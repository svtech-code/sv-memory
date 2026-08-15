package mcp

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/spf13/viper"

	"github.com/svtech-code/sv-memory/internal/graph"
	"github.com/svtech-code/sv-memory/internal/memory"
)

func (s *Server) handleSessionStart(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Import any team-shared memories pulled via git before assembling the
	// Auto-Boot bundle, so a fresh session always starts from the latest
	// context even without an explicit sv_mem_search.
	s.maybeSyncFromGit()

	goal := req.GetString("goal", "")
	semantic := req.GetString("semantic", "") == "true"
	semanticAgent := req.GetString("semantic_agent", "")
	dir := req.GetString("directory", "")
	if dir == "" {
		dir = s.cfg.ProjPath
	}
	session, err := memory.StartSession(s.pool.Writer, s.cfg.ProjectID, goal, dir)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to start session: %v", err)), nil
	}

	// Reset the session token ledger so the Auto-Boot bundle below becomes the
	// first counted injection of the new session (sv_mem_stats reports it).
	s.ResetSessionTokens()

	var sb strings.Builder
	fmt.Fprintf(&sb, "Session started (ID: %s). Use sv_mem_save with session_id=\"%s\" to associate memories, then sv_mem_session_end to close.\n\n", session.ID, session.ID)

	autoBundle, bundleErr := memory.GetAutoBootBundle(ctx, s.pool.Reader, s.cfg.ProjectID, memory.AutoBootOptions{
		Goal:     goal,
		Semantic: semantic,
		Agent:    semanticAgent,
	})
	if bundleErr == nil && autoBundle != "" {
		sb.WriteString(autoBundle)
	}

	// Graph Hubs (E2): surface the top architectural hotspots cheaply (single
	// aggregate query, no centrality) so the agent can orient without an
	// explicit sv_graph_god_nodes call at session start.
	if hubs, hErr := graph.TopDegreeNodes(s.pool.Reader, s.cfg.ProjectID, 3); hErr == nil && len(hubs) > 0 {
		sb.WriteString("\n\n### 🕸️ Graph Hubs (top connected code nodes):\n")
		for _, h := range hubs {
			fmt.Fprintf(&sb, "- **%s** (`%s`, degree: %d)\n", h.Label, h.ID, h.Degree)
		}
		sb.WriteString("\n*Use `sv_graph_explain` on a hub before refactoring it.*\n")
	}

	// Route through the shared token-budget truncation (respond) so the
	// largest pre-tool payload (Auto-Boot Bundle + Graph Hubs) is bounded by
	// the global max_response_tokens default or a per-call token_budget.
	return s.respond(req, sb.String()), nil
}

func (s *Server) handleSessionEnd(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID, err := req.RequireString("session_id")
	if err != nil {
		return mcp.NewToolResultError("missing required field: session_id"), nil
	}
	summary := req.GetString("summary", "")
	if err := memory.EndSession(s.pool.Writer, sessionID, summary); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to end session: %v", err)), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Session %s ended successfully.", sessionID)), nil
}

func (s *Server) handleSessionSummary(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID, err := req.RequireString("session_id")
	if err != nil {
		return mcp.NewToolResultError("missing required field: session_id"), nil
	}
	goal := req.GetString("goal", "")
	discoveries := req.GetString("discoveries", "")
	accomplished := req.GetString("accomplished", "")
	nextSteps := req.GetString("next_steps", "")
	files := req.GetString("files", "")
	if err := memory.SaveSessionSummary(s.pool.Writer, sessionID, goal, discoveries, accomplished, nextSteps, files); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to save session summary: %v", err)), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Session summary saved for session %s.", sessionID)), nil
}

func (s *Server) handleContext(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	startQuery := time.Now()
	limitStr := req.GetString("limit", "")
	limit := 0
	if l, convErr := strconv.Atoi(limitStr); convErr == nil && l > 0 {
		limit = l
	}
	contextStr, err := memory.GetSessionContext(s.pool.Reader, s.cfg.ProjectID, limit)
	debugLog("mem_context took %s", time.Since(startQuery))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get session context: %v", err)), nil
	}
	return s.respond(req, contextStr), nil
}

func (s *Server) handleCompact(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Route through the incremental path so the manual trigger and the
	// background worker share the same last_compaction_at watermark: after the
	// first full pass, a manual call only re-processes topic keys with new
	// activity instead of re-scanning the entire history on the next tick.
	report, err := memory.CompactMemoriesIncremental(s.pool.Writer, s.cfg.ProjectID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to compact memories: %v", err)), nil
	}
	if report.ProcessedTopics == 0 {
		return mcp.NewToolResultText("No duplicate or multi-revision topic keys required compaction."), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Memory compaction complete!\n- Processed Topics: %d\n- Memories Compacted: %d\n- New Syntheses Created: %d\n- Topic Keys: %s",
		report.ProcessedTopics, report.MemoriesCompacted, report.NewSynthesesCreated, strings.Join(report.TopicKeys, ", "))), nil
}

func (s *Server) handleStats(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	stats, err := memory.GetStats(s.pool.Reader, s.cfg.ProjectID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get stats: %v", err)), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Memory Statistics for %s\n\n", s.cfg.ProjName)

	// Current project info (folded in from the former sv_mem_current_project).
	fmt.Fprintf(&sb, "**Current project:** `%s` (ID: `%s`", s.cfg.ProjName, s.cfg.ProjectID)
	var projPath string
	if err := s.pool.Reader.QueryRow("SELECT path FROM projects WHERE id=?", s.cfg.ProjectID).Scan(&projPath); err == nil {
		fmt.Fprintf(&sb, ", path: `%s`)", projPath)
	} else {
		sb.WriteString(")")
	}
	sb.WriteString("\n")

	fmt.Fprintf(&sb, "**Total memories:** %d\n", stats.TotalMemories)
	fmt.Fprintf(&sb, "**Deleted memories:** %d\n", stats.DeletedMemories)
	fmt.Fprintf(&sb, "**Recent (24h):** %d\n", stats.Recent24h)
	fmt.Fprintf(&sb, "**Total sessions:** %d\n", stats.TotalSessions)
	fmt.Fprintf(&sb, "**Active sessions:** %d\n", stats.ActiveSessions)
	fmt.Fprintf(&sb, "**Total relations:** %d\n", stats.TotalRelations)
	fmt.Fprintf(&sb, "**Total user prompts:** %d\n", stats.TotalPrompts)

	// Session token ledger (Phase D): report how many estimated tokens the
	// Auto-Boot bundle + bulk-returning read tools have injected since the last
	// sv_mem_session_start, so the agent can decide when to compact. Read before
	// respond() so this call's own output is not counted.
	budget := viper.GetInt("max_response_tokens")
	budgetStr := "unlimited"
	if budget > 0 {
		budgetStr = strconv.Itoa(budget)
	}
	fmt.Fprintf(&sb, "**Estimated tokens injected this session:** %d\n", s.SessionEstimatedTokens())
	fmt.Fprintf(&sb, "**Token budget (`max_response_tokens`):** %s ('0' = unlimited)\n", budgetStr)
	if len(stats.ByCategory) > 0 {
		sb.WriteString("\n**By category:**\n")
		for cat, count := range stats.ByCategory {
			fmt.Fprintf(&sb, "- %s: **%d**\n", cat, count)
		}
	}
	return s.respond(req, sb.String()), nil
}
