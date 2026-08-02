package mcp

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/spf13/viper"

	"github.com/svtech-code/sv-memory/internal/memory"
)

func (s *Server) handleJudge(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

	rel, err := memory.SaveJudgment(s.pool.Writer, s.cfg.ProjectID, sourceID, targetID, relType, reason, judgedBy)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to save judgment: %v", err)), nil
	}
	s.scheduleSync()
	return mcp.NewToolResultText(fmt.Sprintf("Judgment created: `%s` %s → `%s` (ID: %s)\nReason: %s", sourceID, relType, targetID, rel.ID, rel.Reason)), nil
}

func (s *Server) handleConflicts(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	action, err := req.RequireString("action")
	if err != nil {
		return mcp.NewToolResultError("missing required field: action"), nil
	}

	switch action {
	case "list":
		status := req.GetString("status", "")
		list, err := memory.ListConflicts(s.pool.Reader, s.cfg.ProjectID, status)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to list conflicts: %v", err)), nil
		}
		if len(list) == 0 {
			return mcp.NewToolResultText("No conflicts found matching criteria."), nil
		}
		var sb strings.Builder
		sb.WriteString("## Surfaced Conflicts\n\n")
		for _, c := range list {
			sb.WriteString(fmt.Sprintf("- **ID:** %s | **Status:** %s | **Score:** %.2f\n  - A: %s\n  - B: %s\n",
				c.ID, c.Status, c.Score, c.SourceWhat, c.TargetWhat))
		}
		return mcp.NewToolResultText(sb.String()), nil

	case "scan":
		applyStr := req.GetString("apply", "false")
		apply := applyStr == "true"
		thresholdStr := req.GetString("threshold", "")
		threshold := viper.GetFloat64("conflict_threshold")
		if thresholdStr != "" {
			if f, err := strconv.ParseFloat(thresholdStr, 64); err == nil {
				threshold = f
			}
		}
		found, err := memory.ScanConflicts(s.pool.Writer, s.cfg.ProjectID, apply, 100, threshold)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to scan conflicts: %v", err)), nil
		}
		if len(found) == 0 {
			return mcp.NewToolResultText("No potential conflicts detected."), nil
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Found %d potential conflict(s):\n\n", len(found)))
		for _, c := range found {
			sb.WriteString(fmt.Sprintf("- **ID:** %s | **Score:** %.2f\n  - A: %s\n  - B: %s\n",
				c.ID, c.Score, c.SourceWhat, c.TargetWhat))
		}
		if apply {
			sb.WriteString("\nConflicts successfully saved to database (status: pending).")
		} else {
			sb.WriteString("\nRun with apply=true to persist these conflicts to database.")
		}
		return mcp.NewToolResultText(sb.String()), nil

	case "ignore":
		relID, err := req.RequireString("relation_id")
		if err != nil {
			return mcp.NewToolResultError("missing required field: relation_id for ignore action"), nil
		}
		err = memory.IgnoreConflict(s.pool.Writer, s.cfg.ProjectID, relID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to ignore conflict: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Conflict relation %s marked as ignored.", relID)), nil

	default:
		return mcp.NewToolResultError(fmt.Sprintf("invalid action: %s", action)), nil
	}
}
