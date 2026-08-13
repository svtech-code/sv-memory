package mcp

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/svtech-code/sv-memory/internal/memory"
)

// handleCapturePrompt stores the user's prompt as a local observation attached
// to the active (or explicit) session, mirroring Engram's mem_save_prompt. The
// prompt is not git-synced in this phase; it lives in the local SQLite store so
// future sessions can recover the user's intent after compaction via
// sv_mem_context and sv_mem_stats.
func (s *Server) handleCapturePrompt(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	content, err := req.RequireString("content")
	if err != nil {
		return mcp.NewToolResultError("missing required field: content"), nil
	}
	sessionID := req.GetString("session_id", "")

	// Auto-associate with the active session when no explicit session is given.
	if sessionID == "" {
		if active, actErr := memory.GetActiveSession(s.pool.Reader, s.cfg.ProjectID); actErr == nil && active != nil {
			sessionID = active.ID
		}
	}

	prompt, err := memory.SavePrompt(s.pool.Writer, s.cfg.ProjectID, sessionID, content)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to capture user prompt: %v", err)), nil
	}

	msg := fmt.Sprintf("User prompt captured (ID: %s).", prompt.ID)
	if sessionID != "" {
		msg += fmt.Sprintf(" Associated with session `%s`.", sessionID)
	} else {
		msg += " No active session — run `sv_mem_session_start` to group prompts with a session."
	}
	msg += " It is recoverable via `sv_mem_context` and counted by `sv_mem_stats`."
	return mcp.NewToolResultText(msg), nil
}

// handleMergeProjects moves all memories, sessions, relations, and graph data
// from one project into another, then deletes the source project. Mirrors
// Engram's mem_merge_projects (admin) and the `sv-memory projects consolidate`
// CLI. Both projects are resolved by ID.
func (s *Server) handleMergeProjects(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	from, err := req.RequireString("from")
	if err != nil {
		return mcp.NewToolResultError("missing required field: from"), nil
	}
	to, err := req.RequireString("to")
	if err != nil {
		return mcp.NewToolResultError("missing required field: to"), nil
	}
	if from == to {
		return mcp.NewToolResultError("source and target project must be different"), nil
	}

	movedMemories, movedSessions, err := memory.ConsolidateProjects(s.pool.Writer, from, to)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to merge projects: %v", err)), nil
	}
	s.scheduleSync()
	return mcp.NewToolResultText(fmt.Sprintf(
		"Projects merged: moved %d memories and %d sessions from `%s` to `%s`. Source project `%s` was deleted.",
		movedMemories, movedSessions, from, to, from)), nil
}
