package mcp

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/spf13/viper"

	"github.com/svtech-code/sv-memory/internal/memory"
)

func (s *Server) handleSearch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query, err := req.RequireString("query")
	if err != nil {
		return mcp.NewToolResultError("missing required field: query"), nil
	}
	if len(query) > 256 {
		query = query[:256]
	}
	category := req.GetString("category", "")
	pathFilter := req.GetString("path", "")
	limitStr := req.GetString("limit", "10")
	offsetStr := req.GetString("offset", "0")

	limit := 10
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		}
	}
	if limit < 1 {
		limit = 1
	} else if limit > 50 {
		limit = 50
	}

	offset := 0
	if offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}
	if offset < 0 {
		offset = 0
	} else if offset > 1000 {
		offset = 1000
	}

	startSync := time.Now()
	s.maybeSyncFromGit()
	debugLog("mem_search maybeSyncFromGit took %s", time.Since(startSync))

	startSearch := time.Now()
	results, err := memory.SearchMemoriesCompactScoped(s.pool.Reader, s.cfg.ProjectID, query, category, pathFilter, limit, offset)
	debugLog("mem_search query=%q category=%q offset=%d returned %d rows in %s", query, category, offset, len(results), time.Since(startSearch))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed searching memories: %v", err)), nil
	}

	if len(results) == 0 {
		return mcp.NewToolResultText("No relevant project memories found matching the query."), nil
	}

	// Compact table output — progressive disclosure: one row per result with
	// the essentials. Agent drills down with sv_mem_get for full content.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d relevant project memories (use `sv_mem_get` for full content, `sv_mem_timeline` for context):\n\n", len(results)))
	sb.WriteString("| # | ID | Category | Title | Topic (rev) | Date | Score |\n")
	sb.WriteString("|---|----|----------|-------|-------------|------|-------|\n")
	for i, r := range results {
		title := r.What
		if r.DuplicateCount > 0 {
			title += fmt.Sprintf(" (dup: %d)", r.DuplicateCount)
		}
		topic := "-"
		if r.TopicKey != "" {
			topic = fmt.Sprintf("%s (rev %d)", r.TopicKey, r.RevisionCount)
		}
		score := "-"
		if r.Score != 0 {
			score = fmt.Sprintf("%.2f", r.Score)
		}
		sb.WriteString(fmt.Sprintf("| %d | %s | %s | %s | %s | %s | %s |\n",
			i+1, r.ID, strings.ToUpper(r.Category), escapeTableCell(title), escapeTableCell(topic), r.CreatedAt.Format("2006-01-02"), score))
	}
	// Token estimate for the response
	responseText := sb.String()
	estTokens := len(responseText) / 4
	sb.WriteString(fmt.Sprintf("\n*Response: ~%d tokens*", estTokens))

	return mcp.NewToolResultText(sb.String()), nil
}

// escapeTableCell sanitizes a string for safe embedding in a markdown table
// cell: pipes (which would break the row) are escaped and newlines collapsed.
func escapeTableCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.ReplaceAll(s, "\n", " ")
}

func (s *Server) handleGet(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

	mem, err := memory.GetMemory(s.pool.Reader, s.cfg.ProjectID, id)
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
}

func (s *Server) handleTimeline(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

	prev, next, err := memory.GetTimelineCompact(s.pool.Reader, s.cfg.ProjectID, obsID, before, after)
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
}

func (s *Server) handleCompare(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id1, err := req.RequireString("id1")
	if err != nil {
		return mcp.NewToolResultError("missing required field: id1"), nil
	}
	id2, err := req.RequireString("id2")
	if err != nil {
		return mcp.NewToolResultError("missing required field: id2"), nil
	}
	comparison, err := memory.CompareMemories(s.pool.Reader, s.cfg.ProjectID, id1, id2)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to compare memories: %v", err)), nil
	}
	return mcp.NewToolResultText(comparison), nil
}

func (s *Server) handleReview(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	reviewLimit := viper.GetInt("default_review_limit")
	items, err := memory.ReviewMemories(s.pool.Reader, s.cfg.ProjectID, reviewLimit)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to review memories: %v", err)), nil
	}

	stats, errStats := memory.ConflictStats(s.pool.Reader, s.cfg.ProjectID)
	pendingConflicts := 0
	if errStats == nil {
		pendingConflicts = stats["pending"]
	}

	if len(items) == 0 && pendingConflicts == 0 {
		return mcp.NewToolResultText("No memories found for review."), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Memory Review — %d memories\n", len(items)))
	if pendingConflicts > 0 {
		sb.WriteString(fmt.Sprintf("**⚠ Potential Conflicts:** There are %d pending memory conflict(s). Run 'sv-memory conflicts list' to review.\n\n", pendingConflicts))
	} else {
		sb.WriteString("\n")
	}

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
}
