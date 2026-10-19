package mcp

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/svtech-code/sv-memory/internal/memory"
)

// judgeReasonMaxChars caps the rationale stored on a memory_relations row
// (sv_mem_judge), matching Engram's 200-char reasoning cap on mem_compare so
// verdict annotations stay token-efficient in search results.
const judgeReasonMaxChars = 200

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

	// Token discipline (Engram mem_compare.reasoning parity): the rationale is
	// capped at judgeReasonMaxChars so long explanations cannot bloat the
	// memory_relations payload that later search results annotate.
	reason = memory.TruncateText(reason, judgeReasonMaxChars)

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
		return s.conflictsList(req)
	case "scan":
		return s.conflictsScan(ctx, req)
	case "ignore":
		return s.conflictsIgnore(req)
	default:
		return mcp.NewToolResultError(fmt.Sprintf("invalid action: %s", action)), nil
	}
}

func (s *Server) conflictsList(req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	list, err := memory.ListConflicts(s.pool.Reader, s.cfg.ProjectID, req.GetString("status", ""))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list conflicts: %v", err)), nil
	}
	if len(list) == 0 {
		return mcp.NewToolResultText("No conflicts found matching criteria."), nil
	}
	var sb strings.Builder
	sb.WriteString("## Surfaced Conflicts\n\n")
	for _, c := range list {
		fmt.Fprintf(&sb, "- **ID:** %s | **Status:** %s | **Score:** %.2f\n  - A: %s\n  - B: %s\n",
			c.ID, c.Status, c.Score, c.SourceWhat, c.TargetWhat)
	}
	return mcp.NewToolResultText(sb.String()), nil
}

func (s *Server) conflictsScan(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	apply := req.GetString("apply", "false") == "true"
	found, err := memory.ScanConflicts(s.pool.Writer, s.cfg.ProjectID, apply, 100, s.scanThreshold(req))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to scan conflicts: %v", err)), nil
	}

	if req.GetString("semantic", "false") != "true" {
		return mcp.NewToolResultText(renderScanResult(found, apply)), nil
	}
	return s.judgeConflictsSemantically(ctx, req, found, apply)
}

// scanThreshold resolves the similarity threshold from the request, falling
// back to the configured default.
func (s *Server) scanThreshold(req mcp.CallToolRequest) float64 {
	threshold := s.cfg.Settings.ConflictThreshold
	if thresholdStr := req.GetString("threshold", ""); thresholdStr != "" {
		if f, err := strconv.ParseFloat(thresholdStr, 64); err == nil {
			threshold = f
		}
	}
	return threshold
}

// renderScanResult formats the deterministic (keyword) scan output.
func renderScanResult(found []*memory.MemoryRelation, apply bool) string {
	if len(found) == 0 {
		return "No potential conflicts detected."
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d potential conflict(s):\n\n", len(found))
	for _, c := range found {
		fmt.Fprintf(&sb, "- **ID:** %s | **Score:** %.2f\n  - A: %s\n  - B: %s\n",
			c.ID, c.Score, c.SourceWhat, c.TargetWhat)
	}
	if apply {
		sb.WriteString("\nConflicts successfully saved to database (status: pending).")
	} else {
		sb.WriteString("\nRun with apply=true to persist these conflicts to database.")
	}
	return sb.String()
}

// judgeConflictsSemantically runs the LLM-judging workflow over the scanned
// candidate pairs.
func (s *Server) judgeConflictsSemantically(ctx context.Context, req mcp.CallToolRequest, found []*memory.MemoryRelation, apply bool) (*mcp.CallToolResult, error) {
	agent := memory.ResolveSemanticAgent(req.GetString("agent", ""))
	maxSemantic, _ := strconv.Atoi(req.GetString("max_semantic", "0"))
	concurrency, _ := strconv.Atoi(req.GetString("concurrency", "3"))
	if concurrency <= 0 {
		concurrency = 3
	}

	result, err := memory.JudgeConflictCandidates(ctx, s.pool.Writer, s.cfg.ProjectID, found, agent, maxSemantic, concurrency, apply)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to run semantic judgments: %v", err)), nil
	}
	if len(result.Verdicts) == 0 {
		return mcp.NewToolResultText("No candidate conflicts to judge semantically. Run a scan first (apply=true) to surface candidates."), nil
	}
	return mcp.NewToolResultText(renderSemanticJudgments(result, agent, apply)), nil
}

// renderSemanticJudgments formats the semantic-judging verdicts and their tally.
func renderSemanticJudgments(result *memory.SemanticConflictScanResult, agent string, apply bool) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Semantic judgments for %d candidate(s) using agent '%s':\n\n", len(result.Verdicts), agent)
	for _, v := range result.Verdicts {
		sb.WriteString(renderVerdictLine(v))
	}
	if result.Failed > 0 {
		fmt.Fprintf(&sb, "\n⚠️  %d judgment(s) failed and were left pending for retry.\n", result.Failed)
	}
	if apply {
		fmt.Fprintf(&sb, "\nPersisted %d judged and %d ignored relation(s) (judged_by: llm).", result.Judged, result.Ignored)
	} else {
		sb.WriteString("\nRun with apply=true to persist these judgments.")
	}
	return sb.String()
}

func renderVerdictLine(v *memory.SemanticVerdict) string {
	if v.Error != "" {
		return fmt.Sprintf("- ❌ **%s** ↔ **%s**: %s\n", v.SourceID, v.TargetID, v.Error)
	}
	if v.Relation == memory.SemanticNone {
		return fmt.Sprintf("- ➖ **NONE** %s ↔ %s (score %.2f) — %s\n", v.SourceID, v.TargetID, v.Score, v.Reason)
	}
	return fmt.Sprintf("- 🔀 **%s** %s ↔ %s (score %.2f) — %s\n", strings.ToUpper(v.Relation), v.SourceID, v.TargetID, v.Score, v.Reason)
}

func (s *Server) conflictsIgnore(req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	relID, err := req.RequireString("relation_id")
	if err != nil {
		return mcp.NewToolResultError("missing required field: relation_id for ignore action"), nil
	}
	if err = memory.IgnoreConflict(s.pool.Writer, s.cfg.ProjectID, relID); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to ignore conflict: %v", err)), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Conflict relation %s marked as ignored.", relID)), nil
}
