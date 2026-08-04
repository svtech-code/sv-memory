package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/svtech-code/sv-memory/internal/memory"
)

func (s *Server) handleSessionStart(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	goal := req.GetString("goal", "")
	dir := req.GetString("directory", "")
	if dir == "" {
		dir = s.cfg.ProjPath
	}
	session, err := memory.StartSession(s.pool.Writer, s.cfg.ProjectID, goal, dir)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to start session: %v", err)), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Session started (ID: %s). Use sv_mem_save with session_id=\"%s\" to associate memories, then sv_mem_session_end to close.\n\n", session.ID, session.ID)

	autoBundle, bundleErr := memory.GetAutoBootBundle(s.pool.Reader, s.cfg.ProjectID)
	if bundleErr == nil && autoBundle != "" {
		sb.WriteString(autoBundle)
	}

	return mcp.NewToolResultText(sb.String()), nil
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
	contextStr, err := memory.GetSessionContext(s.pool.Reader, s.cfg.ProjectID)
	debugLog("mem_context took %s", time.Since(startQuery))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get session context: %v", err)), nil
	}
	return mcp.NewToolResultText(contextStr), nil
}

func (s *Server) handleCompact(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	report, err := memory.CompactMemories(s.pool.Writer, s.cfg.ProjectID)
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
	fmt.Fprintf(&sb, "**Total memories:** %d\n", stats.TotalMemories)
	fmt.Fprintf(&sb, "**Deleted memories:** %d\n", stats.DeletedMemories)
	fmt.Fprintf(&sb, "**Recent (24h):** %d\n", stats.Recent24h)
	fmt.Fprintf(&sb, "**Total sessions:** %d\n", stats.TotalSessions)
	fmt.Fprintf(&sb, "**Active sessions:** %d\n", stats.ActiveSessions)
	fmt.Fprintf(&sb, "**Total relations:** %d\n", stats.TotalRelations)
	if len(stats.ByCategory) > 0 {
		sb.WriteString("\n**By category:**\n")
		for cat, count := range stats.ByCategory {
			fmt.Fprintf(&sb, "- %s: **%d**\n", cat, count)
		}
	}
	fmt.Fprintf(&sb, "\n*Response: ~%d tokens*", sb.Len()/4)
	return mcp.NewToolResultText(sb.String()), nil
}

func (s *Server) handleCurrentProject(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var projName, projPath string
	if err := s.pool.Reader.QueryRow("SELECT name, path FROM projects WHERE id=?", s.cfg.ProjectID).Scan(&projName, &projPath); err != nil {
		return mcp.NewToolResultText(fmt.Sprintf("**Current project:** %s (ID: `%s`)", s.cfg.ProjName, s.cfg.ProjectID)), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("**Current project:** %s (ID: `%s`, path: `%s`)", projName, s.cfg.ProjectID, projPath)), nil
}
