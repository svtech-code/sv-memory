package mcp

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/svtech-code/sv-memory/internal/memory"
)

// searchParams holds the parsed, clamped inputs of an sv_mem_search call.
type searchParams struct {
	query         string
	category      string
	pathFilter    string
	matchMode     string
	semantic      bool
	semanticAgent string
	limit         int
	offset        int
	searchLimit   int
}

func (s *Server) handleSearch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	p, err := parseSearchParams(req)
	if err != nil {
		return mcp.NewToolResultError("missing required field: query"), nil
	}

	startSync := time.Now()
	s.maybeSyncFromGit()
	debugLog("mem_search maybeSyncFromGit took %s", time.Since(startSync))

	startSearch := time.Now()
	// Graph-aware search (graph_boost, default on): when a path is given, expand
	// recall to the whole graph community of that path so a module search
	// surfaces memories for the entire module, not just the exact file. Falls
	// back to the plain path-filtered search when the graph has no community
	// data or graph_boost is disabled.
	communityPaths := s.searchCommunityPaths(p.pathFilter)
	results, err := s.runMemorySearch(p, communityPaths)
	debugLog("mem_search query=%q category=%q offset=%d graphBoost=%t returned %d rows in %s", p.query, p.category, p.offset, len(communityPaths) > 0, len(results), time.Since(startSearch))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed searching memories: %v", err)), nil
	}

	// Optional semantic re-ranking: one batched agent call filters/re-orders the
	// keyword pool by meaning. Fail-open: the keyword results are kept when the
	// agent is unavailable or its response cannot be parsed.
	semanticUsed := false
	var reasons map[string]string
	if p.semantic && len(results) > 0 {
		results, reasons, semanticUsed = memory.SemanticRecall(ctx, s.pool.Reader, s.cfg.ProjectID, p.query, results, memory.ResolveSemanticAgent(p.semanticAgent), p.limit)
	}

	if len(results) == 0 {
		if semanticUsed {
			return mcp.NewToolResultText("No relevant project memories found matching the query (semantic recall)."), nil
		}
		return mcp.NewToolResultText("No relevant project memories found matching the query."), nil
	}

	// Map each result to its where_path once so community-expanded rows can be
	// annotated without a per-row query.
	whereByID := s.resultWherePaths(results)

	return s.respond(req, s.renderSearchResults(results, whereByID, communityPaths, p.pathFilter, p.offset, p.semantic, semanticUsed, reasons)), nil
}

// parseSearchParams extracts and clamps the sv_mem_search inputs.
func parseSearchParams(req mcp.CallToolRequest) (searchParams, error) {
	query, err := req.RequireString("query")
	if err != nil {
		return searchParams{}, err
	}
	if runes := []rune(query); len(runes) > 256 {
		query = string(runes[:256])
	}
	p := searchParams{
		query:         query,
		category:      req.GetString("category", ""),
		pathFilter:    req.GetString("path", ""),
		matchMode:     req.GetString("match_mode", "hybrid"),
		semantic:      req.GetString("semantic", "") == "true",
		semanticAgent: req.GetString("semantic_agent", ""),
		limit:         clampRange(req.GetString("limit", "10"), 10, 1, 50),
		offset:        clampRange(req.GetString("offset", "0"), 0, 0, 1000),
	}
	if p.matchMode != "all" && p.matchMode != "any" && p.matchMode != "hybrid" {
		p.matchMode = "hybrid"
	}
	// In semantic mode the keyword pass only feeds a candidate pool to the LLM
	// ranker, so fetch up to 3x the limit (capped) and let SemanticRecall trim
	// back down to `limit`. Pagination is driven by the final semantic ranking.
	p.searchLimit = p.limit
	if p.semantic {
		p.searchLimit = p.limit * 3
		if p.searchLimit > memory.SemanticRecallMaxCandidates {
			p.searchLimit = memory.SemanticRecallMaxCandidates
		}
	}
	return p, nil
}

// runMemorySearch executes the keyword search, using the community-expanded
// path set when graph_boost surfaced one.
func (s *Server) runMemorySearch(p searchParams, communityPaths map[string]bool) ([]*memory.MemorySearchResult, error) {
	if len(communityPaths) > 0 {
		paths := make([]string, 0, len(communityPaths))
		for path := range communityPaths {
			paths = append(paths, path)
		}
		return memory.SearchMemoriesByPaths(s.pool.Reader, s.cfg.ProjectID, p.query, p.category, p.matchMode, p.pathFilter, paths, p.searchLimit, p.offset)
	}
	return memory.SearchMemoriesCompactScoped(s.pool.Reader, s.cfg.ProjectID, p.query, p.category, p.pathFilter, p.matchMode, p.searchLimit, p.offset)
}

// renderSearchResults builds the compact table output (progressive disclosure:
// one row per result with the essentials) plus the inline expansion of the top-1
// hit, annotating graph-community rows with a [graph] marker when graph_boost
// expanded the search beyond the exact path.
func (s *Server) renderSearchResults(results []*memory.MemorySearchResult, whereByID map[string]string, communityPaths map[string]bool, pathFilter string, offset int, semanticRequested, semanticUsed bool, reasons map[string]string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d relevant project memories (use `sv_mem_get` for full content, `sv_mem_timeline` for context):\n\n", len(results))
	if semanticUsed {
		sb.WriteString("*Semantic recall: ranked by meaning with the agent CLI.*\n")
	} else if semanticRequested {
		sb.WriteString("*Semantic recall unavailable — showing keyword results.*\n")
	}
	graphRows := renderSearchTable(&sb, results, whereByID, communityPaths, pathFilter, semanticUsed, reasons)
	if graphRows > 0 {
		sb.WriteString("\n`[graph]` = memory for a file in the same graph community as the search path (graph_boost).\n")
	}

	// Expand the top-1 hit inline so the agent often skips the follow-up
	// sv_mem_get round-trip. Only for fresh searches (offset 0) with a query.
	if offset == 0 && len(results) > 0 {
		s.expandTopResult(&sb, results[0].ID)
	}
	sb.WriteString(s.nextStepAfterMemorySearch())
	return sb.String()
}

// renderSearchTable writes the compact result table and returns the number of
// rows annotated with the [graph] marker.
func renderSearchTable(sb *strings.Builder, results []*memory.MemorySearchResult, whereByID map[string]string, communityPaths map[string]bool, pathFilter string, semanticUsed bool, reasons map[string]string) int {
	sb.WriteString("| # | ID | Category | Title | Topic | Date |\n")
	sb.WriteString("|---|----|----------|-------|-------|------|\n")
	graphRows := 0
	for i, r := range results {
		topic := "-"
		if r.TopicKey != "" {
			topic = r.TopicKey
		}
		title := escapeTableCell(r.What)
		if semanticUsed && reasons != nil {
			if reason := reasons[r.ID]; reason != "" {
				title += " — " + escapeTableCell(reason)
			}
		}
		if pathFilter != "" && whereByID[r.ID] != "" && communityPaths[whereByID[r.ID]] && !strings.Contains(whereByID[r.ID], pathFilter) {
			title += " `[graph]`"
			graphRows++
		}
		fmt.Fprintf(sb, "| %d | %s | %s | %s | %s | %s |\n",
			i+1, r.ID, strings.ToUpper(r.Category), title, escapeTableCell(topic), r.CreatedAt.Format("2006-01-02"))
	}
	return graphRows
}

// expandTopResult appends the inline expansion of a single memory.
func (s *Server) expandTopResult(sb *strings.Builder, id string) {
	top, gErr := memory.GetMemory(s.pool.Reader, s.cfg.ProjectID, id)
	if gErr != nil || top == nil {
		return
	}
	expandChars := s.cfg.Settings.SearchExpandChars
	sb.WriteString("\n### Top result (expanded):\n")
	fmt.Fprintf(sb, "- [%s] **%s** (ID: %s)\n", strings.ToUpper(top.Category), top.What, top.ID)
	if top.Why != "" {
		fmt.Fprintf(sb, "  *Why:* %s\n", truncateField(top.Why, expandChars))
	}
	if top.Learned != "" {
		fmt.Fprintf(sb, "  *Learned:* %s\n", truncateField(top.Learned, expandChars))
	}
	if top.WherePath != "" {
		fmt.Fprintf(sb, "  *Path:* `%s`\n", top.WherePath)
	}
	if top.TopicKey != "" {
		fmt.Fprintf(sb, "  *Topic:* `%s`\n", top.TopicKey)
	}
}

// escapeTableCell sanitizes a string for safe embedding in a markdown table
// cell: pipes (which would break the row) are escaped and newlines collapsed.
func escapeTableCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.ReplaceAll(s, "\n", " ")
}

// searchCommunityPaths resolves the graph community of a path filter for the
// graph_boost expansion. Returns an empty set (no expansion) when the feature
// is disabled, the path is empty, the node cannot be resolved, or the community
// set collapses to just the node itself.
func (s *Server) searchCommunityPaths(pathFilter string) map[string]bool {
	if pathFilter == "" || !s.cfg.Settings.GraphBoost {
		return nil
	}
	node, err := memory.ResolveContextNode(s.pool.Reader, s.cfg.ProjectID, pathFilter)
	if err != nil || node == nil {
		return nil
	}
	set, err := memory.CommunityPathSet(s.pool.Reader, s.cfg.ProjectID, node)
	if err != nil || len(set) < 2 {
		return nil
	}
	return set
}

// resultWherePaths loads the where_path for a batch of result IDs in one query
// so the graph_boost annotation needs no per-row round-trip.
func (s *Server) resultWherePaths(results []*memory.MemorySearchResult) map[string]string {
	out := map[string]string{}
	if len(results) == 0 {
		return out
	}
	ph := make([]string, len(results))
	ids := make([]interface{}, len(results))
	for i, r := range results {
		ph[i] = "?"
		ids[i] = r.ID
	}
	query := "SELECT id, COALESCE(where_path, '') FROM memories WHERE project_id = ? AND id IN (" + strings.Join(ph, ", ") + ")"
	args := append([]interface{}{s.cfg.ProjectID}, ids...)
	rows, err := s.pool.Reader.Query(query, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id, wp string
		if scanErr := rows.Scan(&id, &wp); scanErr == nil {
			out[id] = wp
		}
	}
	return out
}

func (s *Server) handleGet(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return mcp.NewToolResultError("missing required field: id"), nil
	}
	maxChars := s.cfg.Settings.MaxFieldChars
	if maxCharsStr := req.GetString("max_chars", ""); maxCharsStr != "" {
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
	renderMemoryFields(&sb, mem, maxChars)
	return s.respond(req, sb.String()), nil
}

// renderMemoryFields writes the full memory detail block (progressive
// disclosure layer 3), truncating long text fields to maxChars.
func renderMemoryFields(sb *strings.Builder, mem *memory.Memory, maxChars int) {
	fmt.Fprintf(sb, "### [%s] %s (ID: %s)\n", strings.ToUpper(mem.Category), mem.What, mem.ID)
	fmt.Fprintf(sb, "* **Why:** %s\n", truncateField(mem.Why, maxChars))
	fmt.Fprintf(sb, "* **Rule / Learned:** %s\n", truncateField(mem.Learned, maxChars))
	if mem.WherePath != "" {
		fmt.Fprintf(sb, "* **Path:** `%s`\n", mem.WherePath)
	}
	if mem.TopicKey != "" {
		fmt.Fprintf(sb, "* **Topic:** `%s` (revision %d)\n", mem.TopicKey, mem.RevisionCount)
	}
	if mem.DuplicateCount > 0 {
		fmt.Fprintf(sb, "* **Duplicates:** %d\n", mem.DuplicateCount)
	}
	if mem.Pinned {
		sb.WriteString("* **Pinned:** 📌 yes\n")
	}
	if !mem.ReviewAfter.IsZero() {
		fmt.Fprintf(sb, "* **Review after:** %s\n", mem.ReviewAfter.Format("2006-01-02"))
	}
	if mem.GitBranch != "" {
		fmt.Fprintf(sb, "* **Branch:** `%s`\n", mem.GitBranch)
	}
	if mem.GitCommit != "" {
		fmt.Fprintf(sb, "* **Commit:** `%s`\n", mem.GitCommit)
	}
	if mem.Author != "" {
		fmt.Fprintf(sb, "* **Author:** `%s`\n", mem.Author)
	}
	if mem.Impact != "" {
		fmt.Fprintf(sb, "* **What went well / Impact:** %s\n", truncateField(mem.Impact, maxChars))
	}
	if mem.ErrorsFaced != "" {
		fmt.Fprintf(sb, "* **Roadblocks / Errors faced:** %s\n", truncateField(mem.ErrorsFaced, maxChars))
	}
	if mem.NextSteps != "" {
		fmt.Fprintf(sb, "* **Next steps / Pending:** %s\n", truncateField(mem.NextSteps, maxChars))
	}
	fmt.Fprintf(sb, "* **Date:** %s\n", mem.CreatedAt.Format("2006-01-02"))
}

func (s *Server) handleTimeline(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	obsID, err := req.RequireString("observation_id")
	if err != nil {
		return mcp.NewToolResultError("missing required field: observation_id"), nil
	}
	before := neighborCount(req.GetString("before", "5"))
	after := neighborCount(req.GetString("after", "5"))

	prev, next, err := memory.GetTimelineCompact(s.pool.Reader, s.cfg.ProjectID, obsID, before, after)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get timeline: %v", err)), nil
	}

	var sb strings.Builder
	renderTimelineCentral(&sb, s.pool.Reader, s.cfg.ProjectID, obsID, s.cfg.Settings.TimelineWhyChars)
	renderTimelineNeighbors(&sb, prev, next)

	return s.respond(req, sb.String()), nil
}

// neighborCount parses the before/after window size, defaulting to 5 and
// clamping to [1, 50].
func neighborCount(raw string) int {
	v, _ := strconv.Atoi(raw)
	if v <= 0 {
		return 5
	}
	if v > 50 {
		return 50
	}
	return v
}

// renderTimelineCentral surfaces the central observation with a short rationale
// so the agent usually doesn't need a separate sv_mem_get call for context.
func renderTimelineCentral(sb *strings.Builder, reader *sql.DB, projectID, obsID string, whyChars int) {
	fmt.Fprintf(sb, "## Timeline around observation `%s`\n\n", obsID)
	central, cerr := memory.GetMemory(reader, projectID, obsID)
	if cerr != nil || central == nil {
		return
	}
	fmt.Fprintf(sb, "### Central observation:\n")
	fmt.Fprintf(sb, "- [%s] **%s** (ID: %s, %s)\n",
		strings.ToUpper(central.Category), central.What, central.ID, central.CreatedAt.Format("2006-01-02 15:04"))
	if central.Why != "" {
		fmt.Fprintf(sb, "  *Why:* %s\n", truncateField(central.Why, whyChars))
	}
	sb.WriteString("\n")
}

func renderTimelineNeighbors(sb *strings.Builder, prev, next []*memory.MemorySearchResult) {
	if len(prev) > 0 {
		sb.WriteString("### Before:\n")
		for _, m := range prev {
			writeTimelineRow(sb, m)
		}
	}
	if len(next) > 0 {
		if len(prev) > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("### After:\n")
		for _, m := range next {
			writeTimelineRow(sb, m)
		}
	}
	if len(prev) == 0 && len(next) == 0 {
		sb.WriteString("No other memories found nearby in time.\n")
	}
}

func writeTimelineRow(sb *strings.Builder, m *memory.MemorySearchResult) {
	fmt.Fprintf(sb, "- [%s] **%s** (ID: %s, %s)\n",
		strings.ToUpper(m.Category), m.What, m.ID, m.CreatedAt.Format("2006-01-02 15:04"))
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
	switch req.GetString("action", "list") {
	case "mark_reviewed":
		return s.reviewMarkReviewed(req)
	case "prune_stale":
		return s.reviewPruneStale(req)
	default:
		return s.reviewList(req)
	}
}

func (s *Server) reviewMarkReviewed(req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

func (s *Server) reviewPruneStale(req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	olderThanDays := s.cfg.Settings.PruneStaleDays
	if dStr := req.GetString("older_than_days", ""); dStr != "" {
		if d, convErr := strconv.Atoi(dStr); convErr == nil && d > 0 {
			olderThanDays = d
		}
	}
	category := req.GetString("category", "")
	apply := req.GetString("apply", "") == "true"

	pruned, err := memory.PruneStaleMemories(s.pool.Writer, s.cfg.ProjectID, olderThanDays, category, apply)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to prune stale memories: %v", err)), nil
	}
	if apply {
		s.scheduleSync()
	}
	if len(pruned) == 0 {
		return mcp.NewToolResultText("No stale transient memories found to prune."), nil
	}
	return mcp.NewToolResultText(renderPruneReport(pruned, apply)), nil
}

// renderPruneReport formats the prune result table and its trailing note.
func renderPruneReport(pruned []*memory.MemorySearchResult, apply bool) string {
	var sb strings.Builder
	if apply {
		fmt.Fprintf(&sb, "Pruned %d stale transient memories (soft-deleted, change synced to Git):\n\n", len(pruned))
	} else {
		fmt.Fprintf(&sb, "Dry run: %d stale transient memories would be pruned (pass apply='true' to delete):\n\n", len(pruned))
	}
	sb.WriteString("| ID | Category | Title | Date |\n")
	sb.WriteString("|---|----------|-------|------|\n")
	for _, m := range pruned {
		fmt.Fprintf(&sb, "| %s | %s | %s | %s |\n", m.ID, strings.ToUpper(m.Category), escapeTableCell(m.What), m.CreatedAt.Format("2006-01-02"))
	}
	sb.WriteString("\n*Prune only touches transient categories (journal, qa, discussion, idea) unless `category` overrides; pinned and durable memories are never removed.*\n")
	return sb.String()
}

func (s *Server) reviewList(req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	items, err := memory.ReviewMemories(s.pool.Reader, s.cfg.ProjectID, s.cfg.Settings.DefaultReviewLimit)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to review memories: %v", err)), nil
	}

	pendingConflicts := pendingConflictCount(s.pool.Reader, s.cfg.ProjectID)
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
		renderReviewItem(&sb, item)
	}
	return s.respond(req, sb.String()), nil
}

// pendingConflictCount returns the number of pending conflict relations,
// defaulting to 0 when the stats query fails.
func pendingConflictCount(reader *sql.DB, projectID string) int {
	stats, err := memory.ConflictStats(reader, projectID)
	if err != nil {
		return 0
	}
	return stats["pending"]
}

func renderReviewItem(sb *strings.Builder, item *memory.MemoryReviewItem) {
	fmt.Fprintf(sb, "### [%s] %s (ID: %s)\n", strings.ToUpper(item.Memory.Category), item.Memory.What, item.Memory.ID)
	fmt.Fprintf(sb, "* **Status:** %s\n", item.Reason)
	fmt.Fprintf(sb, "* **Age:** %d days", item.AgeDays)
	if item.LastSeenDays > 0 {
		fmt.Fprintf(sb, ", last seen %d days ago", item.LastSeenDays)
	}
	sb.WriteString("\n")
	if item.DuplicateCount > 0 {
		fmt.Fprintf(sb, "* **Duplicates:** %d\n", item.DuplicateCount)
	}
	if item.RevisionCount > 0 {
		fmt.Fprintf(sb, "* **Revisions:** %d\n", item.RevisionCount)
	}
	if item.RelationCount > 0 {
		fmt.Fprintf(sb, "* **Relations:** %d\n", item.RelationCount)
	}
	if item.NeedsConsolidation {
		sb.WriteString("* **⚠ Needs consolidation** (many revisions)\n")
	}
	if item.NeedsReview {
		renderReviewDue(sb, item.ReviewDueDays)
	}
	sb.WriteString("\n")
}

func renderReviewDue(sb *strings.Builder, dueDays int) {
	if dueDays > 0 {
		fmt.Fprintf(sb, "* **⏰ Due for policy review** (%d days overdue)\n", dueDays)
		return
	}
	sb.WriteString("* **⏰ Due for policy review**\n")
}
