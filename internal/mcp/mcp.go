package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/svtech/sv-memory/internal/config"
	"github.com/svtech/sv-memory/internal/db"
	"github.com/svtech/sv-memory/internal/graph"
	"github.com/svtech/sv-memory/internal/memory"
)

// debugEnabled reports whether SV_MEMORY_DEBUG env var is set to a truthy value.
// Used to emit stderr timing logs without polluting the MCP stdio JSON channel.
func debugEnabled() bool {
	v := os.Getenv("SV_MEMORY_DEBUG")
	if v == "" {
		return false
	}
	if b, err := strconv.ParseBool(v); err == nil {
		return b
	}
	return true
}

// debugLog writes a timing line to stderr (safe: stderr is a separate channel
// from MCP JSON-RPC which uses stdout). Skipped entirely on the hot path when
// SV_MEMORY_DEBUG is unset.
func debugLog(format string, args ...interface{}) {
	if !debugEnabled() {
		return
	}
	fmt.Fprintf(os.Stderr, "[sv-memory] "+format+"\n", args...)
}

// StartServer starts the MCP server using stdio transport.
// Reads use the pool's Reader so concurrent tool calls scale; writes (save)
// go through the Writer to keep SQLite serialized under WAL.
func StartServer(pool *db.Pool, cfg *config.Config) error {
	// Initialize server
	s := server.NewMCPServer("sv-memory", "1.0.0")

	// Per-server mtime cache so we skip redundant SyncFromGit round-trips when
	// .sv-memory/chunks/ hasn't changed since the last pull. Uses the chunks
	// directory mtime (or legacy memories.json as fallback). The MCP server is
	// long-lived (stdio), so we keep this state on the closure.
	var (
		syncMu       sync.Mutex
		lastSyncMtim time.Time
	)
	syncFile := filepath.Join(cfg.ProjPath, ".sv-memory", "memories.json")
	chunkDir := filepath.Join(cfg.ProjPath, ".sv-memory", "chunks")

	// syncPathStat returns the path + mtime of the signal file/dir to watch:
	// the chunks dir if it exists, otherwise the legacy memories.json.
	syncPathStat := func() (string, time.Time) {
		if fi, err := os.Stat(chunkDir); err == nil {
			return chunkDir, fi.ModTime()
		}
		if fi, err := os.Stat(syncFile); err == nil {
			return syncFile, fi.ModTime()
		}
		return "", time.Time{}
	}

	// maybeSyncFromGit imports shared memories only when their mtime advanced
	// since the last call. Falls back to a full sync the first time (zero mtime).
	maybeSyncFromGit := func() {
		syncMu.Lock()
		defer syncMu.Unlock()
		_, mtim := syncPathStat()
		if mtim.IsZero() {
			lastSyncMtim = time.Time{}
			return
		}
		if !mtim.After(lastSyncMtim) {
			return
		}
		start := time.Now()
		if err := memory.SyncFromGit(pool.Writer, cfg.ProjectID, cfg.ProjPath); err != nil {
			fmt.Fprintf(os.Stderr, "[sv-memory] syncFromGit failed: %v\n", err)
			return
		}
		lastSyncMtim = mtim
		debugLog("syncFromGit pulled in %s", time.Since(start))
	}

	// Debounced Git sync: coalesces multiple rapid sv_mem_save calls into a
	// single .sv-memory/memories.json write. The timer fires 500ms after the
	// last save, so a burst of 5 saves triggers only one write.
	var (
		debounceMu sync.Mutex
		syncTimer  *time.Timer
	)

	// scheduleSync resets the debounce timer. Each call to sv_mem_save invokes
	// this instead of writing to disk immediately.
	scheduleSync := func() {
		debounceMu.Lock()
		defer debounceMu.Unlock()
		if syncTimer != nil {
			syncTimer.Stop()
		}
		syncTimer = time.AfterFunc(500*time.Millisecond, func() {
			startSync := time.Now()
			if err := memory.SyncToGit(pool.Writer, cfg.ProjectID, cfg.ProjPath); err != nil {
				fmt.Fprintf(os.Stderr, "[sv-memory] syncToGit (debounced) failed: %v\n", err)
				return
			}
			debugLog("syncToGit (debounced) took %s", time.Since(startSync))
			syncMu.Lock()
			_, mtim := syncPathStat()
			if !mtim.IsZero() {
				lastSyncMtim = mtim
			}
			syncMu.Unlock()
		})
	}

	// In-memory graph cache: loads the full project's nodes+edges with two
	// SQL queries then runs BFS in Go-land, avoiding N+1 round-trips per
	// visited node. Invalidated by sv_graph_sync.
	var (
		graphMu    sync.Mutex
		cachedGraph *inMemoryGraph
	)

	getOrLoadGraph := func() (*inMemoryGraph, error) {
		graphMu.Lock()
		defer graphMu.Unlock()
		if cachedGraph != nil {
			return cachedGraph, nil
		}
		var count int
		if err := pool.Reader.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ?", cfg.ProjectID).Scan(&count); err != nil {
			return nil, err
		}
		if count == 0 {
			startBuild := time.Now()
			if err := graph.SyncGraph(pool.Writer, cfg.ProjectID, cfg.ProjPath); err != nil {
				return nil, err
			}
			debugLog("graph_query auto-built graph in %s", time.Since(startBuild))
		}
		g, err := loadFullGraph(pool.Reader, cfg.ProjectID)
		if err != nil {
			return nil, err
		}
		cachedGraph = g
		return g, nil
	}

	// 1. Tool: sv_mem_save
	saveTool := mcp.NewTool("sv_mem_save",
		mcp.WithDescription("Persist a key architectural decision, bug fix, progress journal, or standard guidelines to the project's memory. Supports optional topic_key for upsert semantics (update in place on same project+topic) and session_id for session association."),
		mcp.WithString("category", mcp.Required(), mcp.Description("Category of memory: 'bugfix' | 'architecture' | 'standard' | 'decision' | 'journal' | 'postmortem' | 'discussion' | 'idea' | 'qa'")),
		mcp.WithString("what", mcp.Required(), mcp.Description("Concise description of the decision, standard, or fix")),
		mcp.WithString("why", mcp.Required(), mcp.Description("Detailed reasoning for this choice")),
		mcp.WithString("learned", mcp.Required(), mcp.Description("Rule or key lesson to guide future agents")),
		mcp.WithString("where_path", mcp.Description("Optional file or folder path affected by this memory")),
		mcp.WithString("impact", mcp.Description("Achievements, successes, or what went well")),
		mcp.WithString("errors_faced", mcp.Description("Errors faced, roadblocks, or what went wrong")),
		mcp.WithString("next_steps", mcp.Description("Next actions or pending tasks to continue work")),
		mcp.WithString("topic_key", mcp.Description("Optional stable topic key for upsert semantics. When set, saves to the same project+topic update in place instead of creating a new record. Format: 'category/kebab-case-description'. Use sv_mem_suggest_topic_key to generate one.")),
		mcp.WithString("session_id", mcp.Description("Optional session ID to associate this memory with an active session.")),
	)

	s.AddTool(saveTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
			if active, err := memory.GetActiveSession(pool.Writer, cfg.ProjectID); err == nil && active != nil {
				sessionID = active.ID
			}
		}

		mem := &memory.Memory{
			ProjectID:   cfg.ProjectID,
			Category:    category,
			What:        what,
			Why:         why,
			WherePath:   wherePath,
			Learned:     learned,
			GitBranch:   config.GetGitBranch(cfg.ProjPath),
			GitCommit:   config.GetGitCommit(cfg.ProjPath),
			Author:      config.GetGitAuthor(cfg.ProjPath),
			Impact:      impact,
			ErrorsFaced: errorsFaced,
			NextSteps:   nextSteps,
			TopicKey:    topicKey,
			SessionID:   sessionID,
		}

		startSave := time.Now()
		saved, err := memory.SaveMemory(pool.Writer, mem)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to save memory to SQLite: %v", err)), nil
		}
		debugLog("mem_save SQLite write for id=%s took %s", saved.ID, time.Since(startSave))

		// Schedule a debounced Git sync (500ms coalescing window).
		// The actual write happens asynchronously; errors go to stderr.
		// SQLite write has already succeeded at this point.
		scheduleSync()

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
		candidates, err := memory.FindSimilarMemories(pool.Reader, cfg.ProjectID, what, 5, 0.85)
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
	})

	// 2. Tool: sv_mem_suggest_topic_key
	suggestTool := mcp.NewTool("sv_mem_suggest_topic_key",
		mcp.WithDescription("Suggest a stable topic_key for an evolving topic before saving. The key follows 'category/kebab-case-description' format and enables upsert semantics in sv_mem_save."),
		mcp.WithString("category", mcp.Required(), mcp.Description("Category of memory: 'bugfix' | 'architecture' | 'standard' | 'decision' | 'journal' | 'postmortem' | 'discussion' | 'idea' | 'qa'")),
		mcp.WithString("what", mcp.Required(), mcp.Description("The title or description of the memory")),
	)

	s.AddTool(suggestTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
	})

	// 3. Tool: sv_mem_session_start
	sessionStartTool := mcp.NewTool("sv_mem_session_start",
		mcp.WithDescription("Register the start of a new coding session. Call this at the beginning of a work session to enable session grouping and post-compaction context recovery."),
		mcp.WithString("goal", mcp.Description("Optional goal or objective for this session")),
		mcp.WithString("directory", mcp.Description("Optional working directory (auto-detected from repo if omitted)")),
	)

	s.AddTool(sessionStartTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		goal := req.GetString("goal", "")
		dir := req.GetString("directory", "")
		if dir == "" {
			dir = cfg.ProjPath
		}
		session, err := memory.StartSession(pool.Writer, cfg.ProjectID, goal, dir)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to start session: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Session started (ID: %s). Use sv_mem_save with session_id=\"%s\" to associate memories, then sv_mem_session_end to close.", session.ID, session.ID)), nil
	})

	// 4. Tool: sv_mem_session_end
	sessionEndTool := mcp.NewTool("sv_mem_session_end",
		mcp.WithDescription("End an active coding session with a summary. Call this before finishing a work session to enable context recovery via sv_mem_context."),
		mcp.WithString("session_id", mcp.Required(), mcp.Description("The session ID to end")),
		mcp.WithString("summary", mcp.Description("Optional summary of what was accomplished")),
	)

	s.AddTool(sessionEndTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sessionID, err := req.RequireString("session_id")
		if err != nil {
			return mcp.NewToolResultError("missing required field: session_id"), nil
		}
		summary := req.GetString("summary", "")
		if err := memory.EndSession(pool.Writer, sessionID, summary); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to end session: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Session %s ended successfully.", sessionID)), nil
	})

	// 5. Tool: sv_mem_session_summary
	sessionSummaryTool := mcp.NewTool("sv_mem_session_summary",
		mcp.WithDescription("Save a structured summary for a session. Call before sv_mem_session_end to record goal, discoveries, accomplished work, next steps, and relevant files."),
		mcp.WithString("session_id", mcp.Required(), mcp.Description("Session ID to associate the summary with")),
		mcp.WithString("goal", mcp.Description("Original goal or objective of the session")),
		mcp.WithString("discoveries", mcp.Description("Key discoveries or findings during the session")),
		mcp.WithString("accomplished", mcp.Description("What was accomplished during the session")),
		mcp.WithString("next_steps", mcp.Description("Next steps or pending items")),
		mcp.WithString("files", mcp.Description("Relevant files modified or created")),
	)

	s.AddTool(sessionSummaryTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sessionID, err := req.RequireString("session_id")
		if err != nil {
			return mcp.NewToolResultError("missing required field: session_id"), nil
		}
		goal := req.GetString("goal", "")
		discoveries := req.GetString("discoveries", "")
		accomplished := req.GetString("accomplished", "")
		nextSteps := req.GetString("next_steps", "")
		files := req.GetString("files", "")
		if err := memory.SaveSessionSummary(pool.Writer, sessionID, goal, discoveries, accomplished, nextSteps, files); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to save session summary: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Session summary saved for session %s.", sessionID)), nil
	})

	// 6. Tool: sv_mem_context
	contextTool := mcp.NewTool("sv_mem_context",
		mcp.WithDescription("Recover context from the last completed session. Call this after a compaction or context reset to resume work. Returns the last session's goal, summary, and associated memories."),
		mcp.WithString("limit", mcp.Description("Optional limit of memories to include (default '10')")),
	)

	s.AddTool(contextTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		startQuery := time.Now()
		contextStr, err := memory.GetSessionContext(pool.Reader, cfg.ProjectID)
		debugLog("mem_context took %s", time.Since(startQuery))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get session context: %v", err)), nil
		}
		return mcp.NewToolResultText(contextStr), nil
	})

	// 7. Tool: sv_mem_search
	searchTool := mcp.NewTool("sv_mem_search",
		mcp.WithDescription("Search historical project memories using keyword/FTS search. Returns compact results (ID, category, title, date, topic_key). Use sv_mem_get to retrieve full content of a specific memory, or sv_mem_timeline for chronological context around it."),
		mcp.WithString("query", mcp.Required(), mcp.Description("The keyword or phrase to search for")),
		mcp.WithString("category", mcp.Description("Optional category to filter results: 'bugfix' | 'architecture' | 'standard' | 'decision' | 'journal' | 'postmortem' | 'discussion' | 'idea' | 'qa'")),
		mcp.WithString("limit", mcp.Description("Optional limit of results to return (default is '10')")),
		mcp.WithString("offset", mcp.Description("Optional offset for pagination (default is '0')")),
	)

	s.AddTool(searchTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query, err := req.RequireString("query")
		if err != nil {
			return mcp.NewToolResultError("missing required field: query"), nil
		}
		category := req.GetString("category", "")
		limitStr := req.GetString("limit", "10")
		offsetStr := req.GetString("offset", "0")

		limit := 10
		if limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil {
				limit = l
			}
		}
		offset := 0
		if offsetStr != "" {
			if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
				offset = o
			}
		}

		startSync := time.Now()
		maybeSyncFromGit()
		debugLog("mem_search maybeSyncFromGit took %s", time.Since(startSync))

		startSearch := time.Now()
		results, err := memory.SearchMemoriesCompact(pool.Reader, cfg.ProjectID, query, category, limit, offset)
		debugLog("mem_search query=%q category=%q offset=%d returned %d rows in %s", query, category, offset, len(results), time.Since(startSearch))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed searching memories: %v", err)), nil
		}

		if len(results) == 0 {
			return mcp.NewToolResultText("No relevant project memories found matching the query."), nil
		}

		// Compact output — progressive disclosure: just IDs, titles, and metadata.
		// Agent drills down with sv_mem_get for full content.
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Found %d relevant project memories (use `sv_mem_get` for full content, `sv_mem_timeline` for context):\n\n", len(results)))
		for _, r := range results {
			sb.WriteString(fmt.Sprintf("### [%s] %s (ID: %s)\n", strings.ToUpper(r.Category), r.What, r.ID))
			if r.TopicKey != "" {
				sb.WriteString(fmt.Sprintf("* **Topic:** `%s` (revision %d)\n", r.TopicKey, r.RevisionCount))
			}
			if r.DuplicateCount > 0 {
				sb.WriteString(fmt.Sprintf("* **Duplicates:** %d\n", r.DuplicateCount))
			}
			sb.WriteString(fmt.Sprintf("* **Date:** %s\n", r.CreatedAt.Format("2006-01-02")))
		}
		// Token estimate for the response
		responseText := sb.String()
		estTokens := len(responseText) / 4
		sb.WriteString(fmt.Sprintf("\n*Response: ~%d tokens*", estTokens))

		return mcp.NewToolResultText(sb.String()), nil
	})

	// maxFieldChars is the default maximum character count per text field in
	// sv_mem_get responses. When a field exceeds this limit it is truncated
	// with a "[truncated N chars]" suffix to keep token consumption bounded.
	const maxFieldChars = 2000

	// truncateField shortens a string to maxChars and appends a truncation
	// notice when the original length exceeds the limit.
	truncateField := func(s string, maxChars int) string {
		if maxChars <= 0 || len(s) <= maxChars {
			return s
		}
		return s[:maxChars] + fmt.Sprintf("... [truncated %d chars]", len(s)-maxChars)
	}

	// 8. Tool: sv_mem_get
	getTool := mcp.NewTool("sv_mem_get",
		mcp.WithDescription("Retrieve the full content of a specific memory by its ID. This is the third layer of progressive disclosure: use after sv_mem_search to inspect a memory in detail. Long text fields are truncated beyond max_chars to limit tokens."),
		mcp.WithString("id", mcp.Required(), mcp.Description("The memory ID to retrieve")),
		mcp.WithString("max_chars", mcp.Description("Optional max characters per text field (default '2000', '0' = unlimited)")),
	)

	s.AddTool(getTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := req.RequireString("id")
		if err != nil {
			return mcp.NewToolResultError("missing required field: id"), nil
		}
		maxChars := maxFieldChars
		maxCharsStr := req.GetString("max_chars", "")
		if maxCharsStr != "" {
			if m, err := strconv.Atoi(maxCharsStr); err == nil && m >= 0 {
				maxChars = m
			}
		}

		mem, err := memory.GetMemory(pool.Reader, cfg.ProjectID, id)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get memory: %v", err)), nil
		}
		if mem == nil {
			return mcp.NewToolResultText(fmt.Sprintf("Memory with ID %s not found in the current project.", id)), nil
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("### [%s] %s (ID: %s)\n", strings.ToUpper(mem.Category), mem.What, mem.ID))
		sb.WriteString(fmt.Sprintf("* **Why:** %s\n", truncateField(mem.Why, maxChars)))
		sb.WriteString(fmt.Sprintf("* **Rule / Learned:** %s\n", truncateField(mem.Learned, maxChars)))
		if mem.WherePath != "" {
			sb.WriteString(fmt.Sprintf("* **Path:** `%s`\n", mem.WherePath))
		}
		if mem.TopicKey != "" {
			sb.WriteString(fmt.Sprintf("* **Topic:** `%s` (revision %d)\n", mem.TopicKey, mem.RevisionCount))
		}
		if mem.DuplicateCount > 0 {
			sb.WriteString(fmt.Sprintf("* **Duplicates:** %d\n", mem.DuplicateCount))
		}
		if mem.GitBranch != "" {
			sb.WriteString(fmt.Sprintf("* **Branch:** `%s`\n", mem.GitBranch))
		}
		if mem.GitCommit != "" {
			sb.WriteString(fmt.Sprintf("* **Commit:** `%s`\n", mem.GitCommit))
		}
		if mem.Author != "" {
			sb.WriteString(fmt.Sprintf("* **Author:** `%s`\n", mem.Author))
		}
		if mem.Impact != "" {
			sb.WriteString(fmt.Sprintf("* **What went well / Impact:** %s\n", truncateField(mem.Impact, maxChars)))
		}
		if mem.ErrorsFaced != "" {
			sb.WriteString(fmt.Sprintf("* **Roadblocks / Errors faced:** %s\n", truncateField(mem.ErrorsFaced, maxChars)))
		}
		if mem.NextSteps != "" {
			sb.WriteString(fmt.Sprintf("* **Next steps / Pending:** %s\n", truncateField(mem.NextSteps, maxChars)))
		}
		sb.WriteString(fmt.Sprintf("* **Date:** %s\n", mem.CreatedAt.Format("2006-01-02")))
		// Token estimate
		responseText := sb.String()
		estTokens := len(responseText) / 4
		sb.WriteString(fmt.Sprintf("* **Estimated tokens:** ~%d\n", estTokens))
		return mcp.NewToolResultText(sb.String()), nil
	})

	// 9. Tool: sv_mem_timeline
	timelineTool := mcp.NewTool("sv_mem_timeline",
		mcp.WithDescription("Get chronological context around a specific memory observation. Shows what happened before and after it in the session. This is the second layer of progressive disclosure: use after sv_mem_search to understand the context of a result."),
		mcp.WithString("observation_id", mcp.Required(), mcp.Description("The observation ID to center the timeline around")),
		mcp.WithString("before", mcp.Description("Number of memories to show before (default '5')")),
		mcp.WithString("after", mcp.Description("Number of memories to show after (default '5')")),
	)

	s.AddTool(timelineTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		obsID, err := req.RequireString("observation_id")
		if err != nil {
			return mcp.NewToolResultError("missing required field: observation_id"), nil
		}
		beforeStr := req.GetString("before", "5")
		afterStr := req.GetString("after", "5")
		before, _ := strconv.Atoi(beforeStr)
		after, _ := strconv.Atoi(afterStr)
		if before <= 0 {
			before = 5
		}
		if after <= 0 {
			after = 5
		}

		prev, next, err := memory.GetTimeline(pool.Reader, cfg.ProjectID, obsID, before, after)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get timeline: %v", err)), nil
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("## Timeline around observation `%s`\n\n", obsID))

		if len(prev) > 0 {
			sb.WriteString("### Before:\n")
			for _, m := range prev {
				sb.WriteString(fmt.Sprintf("- [%s] **%s** (ID: %s, %s)\n",
					strings.ToUpper(m.Category), m.What, m.ID, m.CreatedAt.Format("2006-01-02 15:04")))
			}
		}
		if len(next) > 0 {
			if len(prev) > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString("### After:\n")
			for _, m := range next {
				sb.WriteString(fmt.Sprintf("- [%s] **%s** (ID: %s, %s)\n",
					strings.ToUpper(m.Category), m.What, m.ID, m.CreatedAt.Format("2006-01-02 15:04")))
			}
		}
		if len(prev) == 0 && len(next) == 0 {
			sb.WriteString("No other memories found nearby in time.\n")
		}

		return mcp.NewToolResultText(sb.String()), nil
	})

	// 10. Tool: sv_mem_judge
	judgeTool := mcp.NewTool("sv_mem_judge",
		mcp.WithDescription("Create a relation/judgment between two memories. Use this when a new decision supersedes, conflicts with, or relates to a previous one. Helps maintain coherence over time."),
		mcp.WithString("source_id", mcp.Required(), mcp.Description("The ID of the first (newer/source) memory")),
		mcp.WithString("target_id", mcp.Required(), mcp.Description("The ID of the second (older/target) memory")),
		mcp.WithString("relation_type", mcp.Required(), mcp.Description("Type of relation: 'supersedes' | 'conflicts_with' | 'relates_to'")),
		mcp.WithString("reason", mcp.Description("Optional explanation for the judgment")),
		mcp.WithString("judged_by", mcp.Description("Optional identifier of who judged (default: 'agent')")),
	)

	s.AddTool(judgeTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sourceID, err := req.RequireString("source_id")
		if err != nil {
			return mcp.NewToolResultError("missing required field: source_id"), nil
		}
		targetID, err := req.RequireString("target_id")
		if err != nil {
			return mcp.NewToolResultError("missing required field: target_id"), nil
		}
		relType, err := req.RequireString("relation_type")
		if err != nil {
			return mcp.NewToolResultError("missing required field: relation_type"), nil
		}
		reason := req.GetString("reason", "")
		judgedBy := req.GetString("judged_by", "agent")

		validTypes := map[string]bool{"supersedes": true, "conflicts_with": true, "relates_to": true}
		if !validTypes[relType] {
			return mcp.NewToolResultError("invalid relation_type: must be 'supersedes', 'conflicts_with', or 'relates_to'"), nil
		}

		rel, err := memory.SaveJudgment(pool.Writer, cfg.ProjectID, sourceID, targetID, relType, reason, judgedBy)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to save judgment: %v", err)), nil
		}
		scheduleSync()
		return mcp.NewToolResultText(fmt.Sprintf("Judgment created: `%s` %s → `%s` (ID: %s)\nReason: %s", sourceID, relType, targetID, rel.ID, rel.Reason)), nil
	})

	// 11. Tool: sv_mem_compare
	compareTool := mcp.NewTool("sv_mem_compare",
		mcp.WithDescription("Compare two memories side by side in a Markdown table. Use to quickly spot contradictions or similarities before judging."),
		mcp.WithString("id1", mcp.Required(), mcp.Description("The ID of the first memory")),
		mcp.WithString("id2", mcp.Required(), mcp.Description("The ID of the second memory")),
	)

	s.AddTool(compareTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id1, err := req.RequireString("id1")
		if err != nil {
			return mcp.NewToolResultError("missing required field: id1"), nil
		}
		id2, err := req.RequireString("id2")
		if err != nil {
			return mcp.NewToolResultError("missing required field: id2"), nil
		}
		comparison, err := memory.CompareMemories(pool.Reader, cfg.ProjectID, id1, id2)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to compare memories: %v", err)), nil
		}
		return mcp.NewToolResultText(comparison), nil
	})

	// 12. Tool: sv_mem_review
	reviewTool := mcp.NewTool("sv_mem_review",
		mcp.WithDescription("List memories that may need attention: old, stale, high duplicates, or candidates for consolidation. Useful for high-level maintenance."),
	)

	s.AddTool(reviewTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		items, err := memory.ReviewMemories(pool.Reader, cfg.ProjectID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to review memories: %v", err)), nil
		}
		if len(items) == 0 {
			return mcp.NewToolResultText("No memories found for review."), nil
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("## Memory Review — %d memories\n\n", len(items)))
		for _, item := range items {
			sb.WriteString(fmt.Sprintf("### [%s] %s (ID: %s)\n", strings.ToUpper(item.Memory.Category), item.Memory.What, item.Memory.ID))
			sb.WriteString(fmt.Sprintf("* **Status:** %s\n", item.Reason))
			sb.WriteString(fmt.Sprintf("* **Age:** %d days", item.AgeDays))
			if item.LastSeenDays > 0 {
				sb.WriteString(fmt.Sprintf(", last seen %d days ago", item.LastSeenDays))
			}
			sb.WriteString("\n")
			if item.DuplicateCount > 0 {
				sb.WriteString(fmt.Sprintf("* **Duplicates:** %d\n", item.DuplicateCount))
			}
			if item.RevisionCount > 0 {
				sb.WriteString(fmt.Sprintf("* **Revisions:** %d\n", item.RevisionCount))
			}
			if item.RelationCount > 0 {
				sb.WriteString(fmt.Sprintf("* **Relations:** %d\n", item.RelationCount))
			}
			if item.NeedsConsolidation {
				sb.WriteString("* **⚠ Needs consolidation** (many revisions)\n")
			}
			sb.WriteString("\n")
		}
		estTokens := sb.Len() / 4
		sb.WriteString(fmt.Sprintf("*Response: ~%d tokens*\n", estTokens))
		return mcp.NewToolResultText(sb.String()), nil
	})

	// 13. Tool: sv_mem_stats
	statsTool := mcp.NewTool("sv_mem_stats",
		mcp.WithDescription("Get aggregate memory statistics for the current project: total memories, per-category breakdown, session counts, recent activity, and relation count."),
	)

	s.AddTool(statsTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		s, err := memory.GetStats(pool.Reader, cfg.ProjectID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get stats: %v", err)), nil
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("## Memory Statistics for %s\n\n", cfg.ProjName))
		sb.WriteString(fmt.Sprintf("**Total memories:** %d\n", s.TotalMemories))
		sb.WriteString(fmt.Sprintf("**Deleted memories:** %d\n", s.DeletedMemories))
		sb.WriteString(fmt.Sprintf("**Recent (24h):** %d\n", s.Recent24h))
		sb.WriteString(fmt.Sprintf("**Total sessions:** %d\n", s.TotalSessions))
		sb.WriteString(fmt.Sprintf("**Active sessions:** %d\n", s.ActiveSessions))
		sb.WriteString(fmt.Sprintf("**Total relations:** %d\n", s.TotalRelations))
		if len(s.ByCategory) > 0 {
			sb.WriteString("\n**By category:**\n")
			for cat, count := range s.ByCategory {
				sb.WriteString(fmt.Sprintf("- %s: **%d**\n", cat, count))
			}
		}
		sb.WriteString(fmt.Sprintf("\n*Response: ~%d tokens*", sb.Len()/4))
		return mcp.NewToolResultText(sb.String()), nil
	})

	// 14. Tool: sv_mem_delete
	deleteTool := mcp.NewTool("sv_mem_delete",
		mcp.WithDescription("Delete a memory. Soft delete (default) marks it as deleted but preserves it in the database for potential recovery. Hard delete removes it permanently."),
		mcp.WithString("id", mcp.Required(), mcp.Description("The memory ID to delete")),
		mcp.WithString("hard", mcp.Description("Set to 'true' for permanent hard delete (default: 'false' = soft delete)")),
	)

	s.AddTool(deleteTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := req.RequireString("id")
		if err != nil {
			return mcp.NewToolResultError("missing required field: id"), nil
		}
		hardStr := req.GetString("hard", "false")
		hard := hardStr == "true" || hardStr == "1"

		if err := memory.DeleteMemory(pool.Writer, cfg.ProjectID, id, hard); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to delete memory: %v", err)), nil
		}
		scheduleSync()

		mode := "soft"
		if hard {
			mode = "hard"
		}
		return mcp.NewToolResultText(fmt.Sprintf("Memory %s %s-deleted successfully.", id, mode)), nil
	})

	// 15. Tool: sv_mem_capture_passive
	captureTool := mcp.NewTool("sv_mem_capture_passive",
		mcp.WithDescription("Save a lightweight passive observation (e.g. 'modified file X', 'test Y failed'). Unlike sv_mem_save, this requires no explicit decision — it logs context automatically. Category is set to 'journal'."),
		mcp.WithString("what", mcp.Required(), mcp.Description("Brief description of what happened")),
		mcp.WithString("why", mcp.Required(), mcp.Description("Context or reason for the observation")),
	)

	s.AddTool(captureTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
		if active, err := memory.GetActiveSession(pool.Writer, cfg.ProjectID); err == nil && active != nil {
			sessionID = active.ID
		}

		mem, err := memory.CapturePassive(pool.Writer, cfg.ProjectID, what, why, sessionID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to capture passive observation: %v", err)), nil
		}
		scheduleSync()
		return mcp.NewToolResultText(fmt.Sprintf("Passive observation captured (ID: %s, category: journal). Use sv_mem_get(id=\"%s\") for details.", mem.ID, mem.ID)), nil
	})

	// 16. Tool: sv_graph_query
	graphQueryTool := mcp.NewTool("sv_graph_query",
		mcp.WithDescription("Retrieve project code structure, connections, imports, and dependencies for a given module, file, or package."),
		mcp.WithString("path_or_node", mcp.Required(), mcp.Description("The file path, package name, or module to inspect")),
		mcp.WithString("depth", mcp.Description("Hop distance depth in the dependency graph (default is '1')")),
	)

	s.AddTool(graphQueryTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		pathOrNode, err := req.RequireString("path_or_node")
		if err != nil {
			return mcp.NewToolResultError("missing required field: path_or_node"), nil
		}
		depthStr := req.GetString("depth", "1")

		depth := 1
		if depthStr != "" {
			if d, err := strconv.Atoi(depthStr); err == nil {
				depth = d
			}
		}

		// Load or retrieve the in-memory graph cache. First call triggers a
		// full load from SQLite (two bulk queries); subsequent calls hit
		// cached Go maps until sv_graph_sync invalidates them.
		startQuery := time.Now()
		g, err := getOrLoadGraph()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to load graph: %v", err)), nil
		}

		subGraph := g.query(pathOrNode, depth)
		debugLog("graph_query path=%q depth=%d returned %d nodes / %d edges in %s", pathOrNode, depth, len(subGraph.Nodes), len(subGraph.Edges), time.Since(startQuery))

		if len(subGraph.Nodes) == 0 {
			return mcp.NewToolResultText(fmt.Sprintf("No nodes found matching '%s' in the project graph.", pathOrNode)), nil
		}

		// Build a markdown response containing node details and a Mermaid diagram representation
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("## Code Sub-Graph for '%s' (Depth: %d)\n\n", pathOrNode, depth))

		sb.WriteString("### Nodes in Sub-graph:\n")
		for _, node := range subGraph.Nodes {
			sb.WriteString(fmt.Sprintf("- **%s** (`%s`): %s\n", node.Label, node.ID, node.Type))
		}
		sb.WriteString("\n")

		if len(subGraph.Edges) > 0 {
			sb.WriteString("### Mermaid Dependency Diagram:\n")
			sb.WriteString("```mermaid\ngraph TD\n")
			for _, edge := range subGraph.Edges {
				// Escape labels for Mermaid
				srcEscaped := escapeMermaid(edge.SourceID)
				tgtEscaped := escapeMermaid(edge.TargetID)

				if edge.RelationType == "imports" {
					sb.WriteString(fmt.Sprintf("    %s -->|imports| %s\n", srcEscaped, tgtEscaped))
				} else {
					sb.WriteString(fmt.Sprintf("    %s -->|%s| %s\n", srcEscaped, edge.RelationType, tgtEscaped))
				}
			}
			sb.WriteString("```\n")
		} else {
			sb.WriteString("*No connections/edges found in this range.*\n")
		}

		return mcp.NewToolResultText(sb.String()), nil
	})

	// 17. Tool: sv_graph_sync
	graphSyncTool := mcp.NewTool("sv_graph_sync",
		mcp.WithDescription("Trigger a full re-scan of the project code directory and refresh the structural dependency graph stored in SQLite."),
	)

	s.AddTool(graphSyncTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		startSync := time.Now()
		err := graph.SyncGraph(pool.Writer, cfg.ProjectID, cfg.ProjPath)
		debugLog("graph_sync rebuild took %s", time.Since(startSync))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to sync graph: %v", err)), nil
		}
		// Invalidate in-memory cache so the next sv_graph_query reloads fresh
		// data from the rebuilt graph tables.
		graphMu.Lock()
		cachedGraph = nil
		graphMu.Unlock()
		return mcp.NewToolResultText("Dependency graph refreshed and synchronized successfully in SQLite."), nil
	})

	// Start standard IO transport server using convenience function
	return server.ServeStdio(s)
}

// subGraph is the result of a BFS traversal returned by the sv_graph_query tool.
type subGraph struct {
	Nodes []*graph.Node
	Edges []*graph.Edge
}

// inMemoryGraph holds the full project graph in Go memory so that BFS
// traversals (sv_graph_query) run without any SQL round-trips once the data
// is loaded. Two bulk queries replace the previous N+1 pattern of one
// SELECT per visited node + one SELECT per node's edges.
type inMemoryGraph struct {
	nodes         map[string]*graph.Node
	edgesBySource map[string][]*graph.Edge
	edgesByTarget map[string][]*graph.Edge
}

// loadFullGraph executes two queries to load all nodes and edges for a project
// into an inMemoryGraph.
func loadFullGraph(db *sql.DB, projectID string) (*inMemoryGraph, error) {
	nodeMap := make(map[string]*graph.Node)
	nRows, err := db.Query("SELECT id, node_type, label, path, metadata FROM graph_nodes WHERE project_id = ?", projectID)
	if err != nil {
		return nil, err
	}
	defer nRows.Close()
	for nRows.Next() {
		var n graph.Node
		var metaStr string
		if err := nRows.Scan(&n.ID, &n.Type, &n.Label, &n.Path, &metaStr); err == nil {
			_ = json.Unmarshal([]byte(metaStr), &n.Metadata)
			nodeMap[n.ID] = &n
		}
	}
	if err := nRows.Err(); err != nil {
		return nil, err
	}

	edgesBySrc := make(map[string][]*graph.Edge)
	edgesByTgt := make(map[string][]*graph.Edge)
	eRows, err := db.Query("SELECT id, source_id, target_id, relation_type FROM graph_edges WHERE project_id = ?", projectID)
	if err != nil {
		return nil, err
	}
	defer eRows.Close()
	for eRows.Next() {
		var e graph.Edge
		if err := eRows.Scan(&e.ID, &e.SourceID, &e.TargetID, &e.RelationType); err == nil {
			edgesBySrc[e.SourceID] = append(edgesBySrc[e.SourceID], &e)
			edgesByTgt[e.TargetID] = append(edgesByTgt[e.TargetID], &e)
		}
	}
	if err := eRows.Err(); err != nil {
		return nil, err
	}

	return &inMemoryGraph{
		nodes:         nodeMap,
		edgesBySource: edgesBySrc,
		edgesByTarget: edgesByTgt,
	}, nil
}

// query performs a BFS traversal over the in-memory graph starting from the
// node matching 'start' (by id, path, or label) and returns all reachable
// nodes and edges within maxDepth hops. Zero SQL calls.
func (g *inMemoryGraph) query(start string, maxDepth int) *subGraph {
	startID := g.findNode(start)
	if startID == "" {
		return &subGraph{}
	}

	type queueItem struct {
		id    string
		depth int
	}

	queue := []queueItem{{id: startID, depth: 0}}
	visited := make(map[string]bool)
	nodeMap := make(map[string]*graph.Node)
	edgeMap := make(map[string]*graph.Edge)

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		if visited[curr.id] {
			continue
		}
		visited[curr.id] = true

		if n, ok := g.nodes[curr.id]; ok {
			nodeMap[curr.id] = n
		}

		if curr.depth >= maxDepth {
			continue
		}

		for _, e := range g.edgesBySource[curr.id] {
			edgeMap[e.ID] = e
			if !visited[e.TargetID] {
				queue = append(queue, queueItem{id: e.TargetID, depth: curr.depth + 1})
			}
		}
		for _, e := range g.edgesByTarget[curr.id] {
			edgeMap[e.ID] = e
			if !visited[e.SourceID] {
				queue = append(queue, queueItem{id: e.SourceID, depth: curr.depth + 1})
			}
		}
	}

	nodes := make([]*graph.Node, 0, len(nodeMap))
	for _, n := range nodeMap {
		nodes = append(nodes, n)
	}
	edges := make([]*graph.Edge, 0, len(edgeMap))
	for _, e := range edgeMap {
		edges = append(edges, e)
	}
	return &subGraph{Nodes: nodes, Edges: edges}
}

// findNode returns the node ID matching by exact id, path, or label.
func (g *inMemoryGraph) findNode(start string) string {
	for id, n := range g.nodes {
		if id == start || n.Path == start || n.Label == start {
			return id
		}
	}
	return ""
}

func escapeMermaid(s string) string {
	// Replaces path slash/special chars with underscores or quotes for Mermaid
	s = strings.ReplaceAll(s, "\\", "/")
	return fmt.Sprintf(`"%s"`, s)
}
