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
		if active, err := memory.GetActiveSession(s.pool.Writer, s.cfg.ProjectID); err == nil && active != nil {
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

	// Conflict surfacing: check for similar titles after save
	candidates, err := memory.FindSimilarMemories(s.pool.Reader, s.cfg.ProjectID, what, 5, 0.85)
	if err == nil && len(candidates) > 0 {
		response += "\n\n**Similar memories detected (consider reviewing with sv_mem_judge if these are conflicts):**\n"
		for _, c := range candidates {
			if c.ID != saved.ID {
				response += fmt.Sprintf("- [%s] **%s** (ID: %s, similarity: %.0f%%)\n",
					strings.ToUpper(c.Category), c.What, c.ID, c.Similarity*100)
			}
		}
	}

	return mcp.NewToolResultText(response), nil
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
	if active, err := memory.GetActiveSession(s.pool.Writer, s.cfg.ProjectID); err == nil && active != nil {
		sessionID = active.ID
	}

	mem, err := memory.CapturePassive(s.pool.Writer, s.cfg.ProjectID, what, why, sessionID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to capture passive observation: %v", err)), nil
	}
	s.scheduleSync()
	return mcp.NewToolResultText(fmt.Sprintf("Passive observation captured (ID: %s, category: journal). Use sv_mem_get(id=\"%s\") for details.", mem.ID, mem.ID)), nil
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
