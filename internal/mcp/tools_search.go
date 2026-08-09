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
	matchMode := req.GetString("match_mode", "all")
	if matchMode != "any" {
		matchMode = "all"
	}
	limitStr := req.GetString("limit", "10")
	offsetStr := req.GetString("offset", "0")

	limit := 10
	if limitStr != "" {
		if l, convErr := strconv.Atoi(limitStr); convErr == nil {
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
		if o, convErr := strconv.Atoi(offsetStr); convErr == nil && o >= 0 {
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
	results, err := memory.SearchMemoriesCompactScoped(s.pool.Reader, s.cfg.ProjectID, query, category, pathFilter, matchMode, limit, offset)
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
	fmt.Fprintf(&sb, "Found %d relevant project memories (use `sv_mem_get` for full content, `sv_mem_timeline` for context):\n\n", len(results))
	sb.WriteString("| # | ID | Category | Title | Topic | Date |\n")
	sb.WriteString("|---|----|----------|-------|-------|------|\n")
	for i, r := range results {
		topic := "-"
		if r.TopicKey != "" {
			topic = r.TopicKey
		}
		fmt.Fprintf(&sb, "| %d | %s | %s | %s | %s | %s |\n",
			i+1, r.ID, strings.ToUpper(r.Category), escapeTableCell(r.What), escapeTableCell(topic), r.CreatedAt.Format("2006-01-02"))
	}

	// Expand the top-1 hit inline so the agent often skips the follow-up
	// sv_mem_get round-trip. Only for fresh searches (offset 0) with a query.
	if offset == 0 && len(results) > 0 {
		if top, gErr := memory.GetMemory(s.pool.Reader, s.cfg.ProjectID, results[0].ID); gErr == nil && top != nil {
			sb.WriteString("\n### Top result (expanded):\n")
			fmt.Fprintf(&sb, "- [%s] **%s** (ID: %s)\n", strings.ToUpper(top.Category), top.What, top.ID)
			if top.Why != "" {
				fmt.Fprintf(&sb, "  *Why:* %s\n", truncateField(top.Why, searchExpandChars))
			}
			if top.Learned != "" {
				fmt.Fprintf(&sb, "  *Learned:* %s\n", truncateField(top.Learned, searchExpandChars))
			}
			if top.WherePath != "" {
				fmt.Fprintf(&sb, "  *Path:* `%s`\n", top.WherePath)
			}
			if top.TopicKey != "" {
				fmt.Fprintf(&sb, "  *Topic:* `%s`\n", top.TopicKey)
			}
		}
	}

	return s.respond(req, sb.String()), nil
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
		if m, convErr := strconv.Atoi(maxCharsStr); convErr == nil && m >= 0 {
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
	fmt.Fprintf(&sb, "### [%s] %s (ID: %s)\n", strings.ToUpper(mem.Category), mem.What, mem.ID)
	fmt.Fprintf(&sb, "* **Why:** %s\n", truncateField(mem.Why, maxChars))
	fmt.Fprintf(&sb, "* **Rule / Learned:** %s\n", truncateField(mem.Learned, maxChars))
	if mem.WherePath != "" {
		fmt.Fprintf(&sb, "* **Path:** `%s`\n", mem.WherePath)
	}
	if mem.TopicKey != "" {
		fmt.Fprintf(&sb, "* **Topic:** `%s` (revision %d)\n", mem.TopicKey, mem.RevisionCount)
	}
	if mem.DuplicateCount > 0 {
		fmt.Fprintf(&sb, "* **Duplicates:** %d\n", mem.DuplicateCount)
	}
	if mem.Pinned {
		sb.WriteString("* **Pinned:** 📌 yes\n")
	}
	if !mem.ReviewAfter.IsZero() {
		fmt.Fprintf(&sb, "* **Review after:** %s\n", mem.ReviewAfter.Format("2006-01-02"))
	}
	if mem.GitBranch != "" {
		fmt.Fprintf(&sb, "* **Branch:** `%s`\n", mem.GitBranch)
	}
	if mem.GitCommit != "" {
		fmt.Fprintf(&sb, "* **Commit:** `%s`\n", mem.GitCommit)
	}
	if mem.Author != "" {
		fmt.Fprintf(&sb, "* **Author:** `%s`\n", mem.Author)
	}
	if mem.Impact != "" {
		fmt.Fprintf(&sb, "* **What went well / Impact:** %s\n", truncateField(mem.Impact, maxChars))
	}
	if mem.ErrorsFaced != "" {
		fmt.Fprintf(&sb, "* **Roadblocks / Errors faced:** %s\n", truncateField(mem.ErrorsFaced, maxChars))
	}
	if mem.NextSteps != "" {
		fmt.Fprintf(&sb, "* **Next steps / Pending:** %s\n", truncateField(mem.NextSteps, maxChars))
	}
	fmt.Fprintf(&sb, "* **Date:** %s\n", mem.CreatedAt.Format("2006-01-02"))
	return s.respond(req, sb.String()), nil
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
	fmt.Fprintf(&sb, "## Timeline around observation `%s`\n\n", obsID)

	// Surface the central observation with a short rationale so the agent
	// usually doesn't need a separate sv_mem_get call for context.
	if central, cerr := memory.GetMemory(s.pool.Reader, s.cfg.ProjectID, obsID); cerr == nil && central != nil {
		fmt.Fprintf(&sb, "### Central observation:\n")
		fmt.Fprintf(&sb, "- [%s] **%s** (ID: %s, %s)\n",
			strings.ToUpper(central.Category), central.What, central.ID, central.CreatedAt.Format("2006-01-02 15:04"))
		if central.Why != "" {
			fmt.Fprintf(&sb, "  *Why:* %s\n", truncateField(central.Why, timelineWhyChars))
		}
		sb.WriteString("\n")
	}

	if len(prev) > 0 {
		sb.WriteString("### Before:\n")
		for _, m := range prev {
			fmt.Fprintf(&sb, "- [%s] **%s** (ID: %s, %s)\n",
				strings.ToUpper(m.Category), m.What, m.ID, m.CreatedAt.Format("2006-01-02 15:04"))
		}
	}
	if len(next) > 0 {
		if len(prev) > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("### After:\n")
		for _, m := range next {
			fmt.Fprintf(&sb, "- [%s] **%s** (ID: %s, %s)\n",
				strings.ToUpper(m.Category), m.What, m.ID, m.CreatedAt.Format("2006-01-02 15:04"))
		}
	}
	if len(prev) == 0 && len(next) == 0 {
		sb.WriteString("No other memories found nearby in time.\n")
	}

	return s.respond(req, sb.String()), nil
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
	action := req.GetString("action", "list")
	if action == "mark_reviewed" {
		id, err := req.RequireString("id")
		if err != nil {
			return mcp.NewToolResultError("missing required field: id (needed for action='mark_reviewed')"), nil
		}
		if err := memory.MarkMemoryReviewed(s.pool.Writer, s.cfg.ProjectID, id); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to mark memory as reviewed: %v", err)), nil
		}
		s.scheduleSync()
		return mcp.NewToolResultText(fmt.Sprintf("Memory %s marked as reviewed. Its policy-review deadline was reset and the change will be synced to Git.", id)), nil
	}

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
	fmt.Fprintf(&sb, "## Memory Review — %d memories\n", len(items))
	if pendingConflicts > 0 {
		fmt.Fprintf(&sb, "**⚠ Potential Conflicts:** There are %d pending memory conflict(s). Run 'sv-memory conflicts list' to review.\n\n", pendingConflicts)
	} else {
		sb.WriteString("\n")
	}

	for _, item := range items {
		fmt.Fprintf(&sb, "### [%s] %s (ID: %s)\n", strings.ToUpper(item.Memory.Category), item.Memory.What, item.Memory.ID)
		fmt.Fprintf(&sb, "* **Status:** %s\n", item.Reason)
		fmt.Fprintf(&sb, "* **Age:** %d days", item.AgeDays)
		if item.LastSeenDays > 0 {
			fmt.Fprintf(&sb, ", last seen %d days ago", item.LastSeenDays)
		}
		sb.WriteString("\n")
		if item.DuplicateCount > 0 {
			fmt.Fprintf(&sb, "* **Duplicates:** %d\n", item.DuplicateCount)
		}
		if item.RevisionCount > 0 {
			fmt.Fprintf(&sb, "* **Revisions:** %d\n", item.RevisionCount)
		}
		if item.RelationCount > 0 {
			fmt.Fprintf(&sb, "* **Relations:** %d\n", item.RelationCount)
		}
		if item.NeedsConsolidation {
			sb.WriteString("* **⚠ Needs consolidation** (many revisions)\n")
		}
		if item.NeedsReview {
			if item.ReviewDueDays > 0 {
				fmt.Fprintf(&sb, "* **⏰ Due for policy review** (%d days overdue)\n", item.ReviewDueDays)
			} else {
				sb.WriteString("* **⏰ Due for policy review**\n")
			}
		}
		sb.WriteString("\n")
	}
	return s.respond(req, sb.String()), nil
}
