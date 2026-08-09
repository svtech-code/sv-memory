package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/svtech-code/sv-memory/internal/memory"
)

func (s *Server) handleSave(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	category, err := req.RequireString("category")
	if err != nil {
		return mcp.NewToolResultError("missing required field: category"), nil
	}
	what, err := req.RequireString("what")
	if err != nil {
		return mcp.NewToolResultError("missing required field: what"), nil
	}
	why, err := req.RequireString("why")
	if err != nil {
		return mcp.NewToolResultError("missing required field: why"), nil
	}
	learned, err := req.RequireString("learned")
	if err != nil {
		return mcp.NewToolResultError("missing required field: learned"), nil
	}

	wherePath := req.GetString("where_path", "")
	impact := req.GetString("impact", "")
	errorsFaced := req.GetString("errors_faced", "")
	nextSteps := req.GetString("next_steps", "")
	topicKey := req.GetString("topic_key", "")
	sessionID := req.GetString("session_id", "")

	// Auto-associate with active session if no explicit session_id provided
	if sessionID == "" {
		if active, actErr := memory.GetActiveSession(s.pool.Writer, s.cfg.ProjectID); actErr == nil && active != nil {
			sessionID = active.ID
		}
	}

	// Git metadata is cached for gitCacheTTL to avoid shelling out to git on
	// every save (up to 4 subprocesses per call in the worst case).
	gitMeta := s.cachedGitMetadata()

	mem := &memory.Memory{
		ProjectID:   s.cfg.ProjectID,
		Category:    category,
		What:        what,
		Why:         why,
		WherePath:   wherePath,
		Learned:     learned,
		GitBranch:   gitMeta.branch,
		GitCommit:   gitMeta.commit,
		Author:      gitMeta.author,
		Impact:      impact,
		ErrorsFaced: errorsFaced,
		NextSteps:   nextSteps,
		TopicKey:    topicKey,
		SessionID:   sessionID,
	}

	startSave := time.Now()
	saved, err := memory.SaveMemory(s.pool.Writer, mem)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to save memory to SQLite: %v", err)), nil
	}
	debugLog("mem_save SQLite write for id=%s took %s", saved.ID, time.Since(startSave))

	// Schedule a debounced Git sync (500ms coalescing window).
	// The actual write happens asynchronously; errors go to stderr.
	// SQLite write has already succeeded at this point.
	s.scheduleSync()

	// Build contextual response based on what SaveMemory did
	var action string
	if saved.DuplicateCount > 0 {
		action = fmt.Sprintf("duplicate suppressed (count: %d)", saved.DuplicateCount)
	} else if saved.RevisionCount > 1 {
		action = fmt.Sprintf("updated existing topic_key (revision: %d)", saved.RevisionCount)
	} else {
		action = "created"
	}

	response := fmt.Sprintf("Successfully %s memory (ID: %s) and synced to Git workspace (.sv-memory/memories.json)", action, saved.ID)

	// Conflict surfacing: check for similar titles after save. Duplicate
	// suppressions already know the similar memory, so the extra FTS5 pass is
	// skipped there. The check is time-boxed so a slow search never blocks the
	// save response.
	if saved.DuplicateCount == 0 && saved.RevisionCount <= 1 {
		response += s.similarMemoriesHint(what, saved.ID)
	}

	return mcp.NewToolResultText(response), nil
}

// similarMemoriesHint runs FindSimilarMemories in a goroutine and waits up to
// similarCheckTimeout for the result. If the search exceeds the budget the
// hint is omitted rather than blocking the save response.
func (s *Server) similarMemoriesHint(title, savedID string) string {
	type result struct {
		candidates []*memory.MemoryCandidate
		err        error
	}
	ch := make(chan result, 1)
	go func() {
		candidates, err := memory.FindSimilarMemories(s.pool.Reader, s.cfg.ProjectID, title, 5, 0.85)
		ch <- result{candidates, err}
	}()

	select {
	case r := <-ch:
		if r.err != nil || len(r.candidates) == 0 {
			return ""
		}
		var sb strings.Builder
		sb.WriteString("\n\n**Similar memories detected (call `sv_mem_judge` to record a relation if these are superseded/conflicting):**\n")
		for _, c := range r.candidates {
			if c.ID != savedID {
				fmt.Fprintf(&sb, "- [%s] **%s** (ID: %s, similarity: %.0f%%)\n",
					strings.ToUpper(c.Category), c.What, c.ID, c.Similarity*100)
				fmt.Fprintf(&sb, "  → `sv_mem_judge(source_id=\"%s\", target_id=\"%s\", relation_type=supersedes|conflicts_with|relates_to)`\n",
					savedID, c.ID)
			}
		}
		return sb.String()
	case <-time.After(similarCheckTimeout):
		debugLog("mem_save similar-memories check timed out, omitting hint")
		return ""
	}
}

func (s *Server) handleUpdate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return mcp.NewToolResultError("missing required field: id"), nil
	}

	// Only fields present in the request arguments are updated. An explicitly
	// empty value clears the field; an absent value leaves it untouched.
	args := req.GetArguments()
	strPtr := func(key string) *string {
		v, ok := args[key]
		if !ok {
			return nil
		}
		s, _ := v.(string)
		return &s
	}

	upd := memory.MemoryUpdate{
		What:        strPtr("what"),
		Why:         strPtr("why"),
		Learned:     strPtr("learned"),
		WherePath:   strPtr("where_path"),
		Impact:      strPtr("impact"),
		ErrorsFaced: strPtr("errors_faced"),
		NextSteps:   strPtr("next_steps"),
	}
	if upd.What == nil && upd.Why == nil && upd.Learned == nil && upd.WherePath == nil &&
		upd.Impact == nil && upd.ErrorsFaced == nil && upd.NextSteps == nil {
		return mcp.NewToolResultError("no updatable fields provided (what, why, learned, where_path, impact, errors_faced, next_steps)"), nil
	}

	updated, err := memory.UpdateMemory(s.pool.Writer, s.cfg.ProjectID, id, upd)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to update memory: %v", err)), nil
	}
	s.scheduleSync()

	return mcp.NewToolResultText(fmt.Sprintf("Memory %s updated (revision %d). Changes will be synced to the Git workspace. Use sv_mem_get(id=\"%s\") to verify the new content.", updated.ID, updated.RevisionCount, updated.ID)), nil
}

func (s *Server) handleSuggestTopicKey(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	category, err := req.RequireString("category")
	if err != nil {
		return mcp.NewToolResultError("missing required field: category"), nil
	}
	what, err := req.RequireString("what")
	if err != nil {
		return mcp.NewToolResultError("missing required field: what"), nil
	}
	key := memory.SuggestTopicKey(category, what)
	return mcp.NewToolResultText(fmt.Sprintf("Suggested topic_key: `%s`\nUse this key with sv_mem_save(topic_key=\"%s\") to enable upsert semantics.", key, key)), nil
}

func (s *Server) handleCapturePassive(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	what, err := req.RequireString("what")
	if err != nil {
		return mcp.NewToolResultError("missing required field: what"), nil
	}
	why, err := req.RequireString("why")
	if err != nil {
		return mcp.NewToolResultError("missing required field: why"), nil
	}

	// Auto-associate with active session
	sessionID := ""
	if active, actErr := memory.GetActiveSession(s.pool.Writer, s.cfg.ProjectID); actErr == nil && active != nil {
		sessionID = active.ID
	}

	mem, err := memory.CapturePassive(s.pool.Writer, s.cfg.ProjectID, what, why, sessionID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to capture passive observation: %v", err)), nil
	}
	s.scheduleSync()
	return mcp.NewToolResultText(fmt.Sprintf("Passive observation captured (ID: %s, category: journal). Use sv_mem_get(id=\"%s\") for details.", mem.ID, mem.ID)), nil
}

// handlePin pins or unpins a memory based on the action argument. The pin/unpin
// verbs were consolidated into a single tool to keep the MCP tool surface small;
// action='pin' (default) surfaces the memory first in session context, while
// action='unpin' clears the pinned flag.
func (s *Server) handlePin(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return mcp.NewToolResultError("missing required field: id"), nil
	}
	switch req.GetString("action", "pin") {
	case "unpin":
		if err := memory.UnpinMemory(s.pool.Writer, s.cfg.ProjectID, id); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to unpin memory: %v", err)), nil
		}
		s.scheduleSync()
		return mcp.NewToolResultText(fmt.Sprintf("Memory %s unpinned.", id)), nil
	case "pin":
		if err := memory.PinMemory(s.pool.Writer, s.cfg.ProjectID, id); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to pin memory: %v", err)), nil
		}
		s.scheduleSync()
		return mcp.NewToolResultText(fmt.Sprintf("Memory %s pinned. It will surface first in sv_mem_context.", id)), nil
	default:
		return mcp.NewToolResultError(fmt.Sprintf("invalid action %q (expected 'pin' or 'unpin')", req.GetString("action", ""))), nil
	}
}

func (s *Server) handleDelete(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return mcp.NewToolResultError("missing required field: id"), nil
	}
	hardStr := req.GetString("hard", "false")
	hard := hardStr == "true" || hardStr == "1"

	if err := memory.DeleteMemory(s.pool.Writer, s.cfg.ProjectID, id, hard); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to delete memory: %v", err)), nil
	}
	s.scheduleSync()

	mode := "soft"
	if hard {
		mode = "hard"
	}
	return mcp.NewToolResultText(fmt.Sprintf("Memory %s %s-deleted successfully.", id, mode)), nil
}
