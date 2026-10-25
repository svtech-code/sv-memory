package mcp

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/svtech-code/sv-memory/internal/graph"
	"github.com/svtech-code/sv-memory/internal/memory"
)

// renderPreflight renders a pre-flight verdict as compact markdown: the overall
// verdict line plus one line per surfaced rule. Token-efficient by design — the
// agent reviews the rules and drills down with sv_mem_get only when needed.
func renderPreflight(r *memory.PreflightResult) string {
	if r == nil || len(r.Issues) == 0 {
		return fmt.Sprintf("**Pre-flight: `%s`** — no conflicting rules found.", memory.PreflightPass)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "**Pre-flight: `%s`**\n", r.Verdict)
	if r.Verdict == memory.PreflightBlock {
		sb.WriteString("The proposal contradicts a pinned invariant/rule:\n")
	} else {
		sb.WriteString("The proposal overlaps existing decisions/standards — review before proceeding:\n")
	}
	for _, it := range r.Issues {
		fmt.Fprintf(&sb, "- [%s] **%s** (ID: %s, sim %.0f%%, %s)\n",
			strings.ToUpper(it.Category), it.What, it.MemoryID, it.Similarity*100,
			strings.ToLower(it.Severity))
	}
	sb.WriteString("\n*Drill down with `sv_mem_get(id=\"<id>\")` or `sv_mem_judge` to record a relation.*\n")
	return sb.String()
}

// resolveChangeByIDOrSlug looks a change up by ID and falls back to slug,
// preserving the historic error wording for each stage.
func resolveChangeByIDOrSlug(reader *sql.DB, projectID, changeID string) (*memory.Change, error) {
	c, err := memory.GetChange(reader, projectID, changeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get change: %v", err)
	}
	if c != nil {
		return c, nil
	}
	c, err = memory.GetChangeBySlug(reader, projectID, changeID)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve change: %v", err)
	}
	return c, nil
}

// handleProposeSpec creates a spec change in the draft state, optionally stores
// OpenSpec-style delta requirements, wires the capability into the graph, and
// runs a pre-flight check against the project's rules/invariants before any
// code is written.
func (s *Server) handleProposeSpec(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	slug, err := req.RequireString("slug")
	if err != nil {
		return mcp.NewToolResultError("missing required field: slug"), nil
	}
	title, err := req.RequireString("title")
	if err != nil {
		return mcp.NewToolResultError("missing required field: title"), nil
	}
	what := req.GetString("what", "")
	goal := req.GetString("goal", "")
	wherePath := req.GetString("where_path", "")
	design := req.GetString("design", "")
	tasks := req.GetString("tasks", "")
	requirements := req.GetString("requirements", "")
	capabilityPath := req.GetString("capability_path", "")

	c, err := memory.CreateChange(s.pool.Writer, s.cfg.ProjectID, slug, title, what, goal, wherePath, design, tasks)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to create change: %v", err)), nil
	}

	// Override the default capability (slug) when a distinct one is provided.
	if capabilityPath != "" {
		if c, err = memory.SetChangeCapabilityPath(s.pool.Writer, s.cfg.ProjectID, c.ID, capabilityPath); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to set capability path: %v", err)), nil
		}
	}

	// Advance the lifecycle draft -> proposed once the change is fully formed,
	// so ListChangesByStatus("proposed") and the mirror reflect the state the
	// AGENTS.md cycle documents.
	if c, err = memory.UpdateChangeStatus(s.pool.Writer, s.cfg.ProjectID, c.ID, memory.ChangeStatusProposed); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to mark change proposed: %v", err)), nil
	}

	reqSummary, err := s.storeChangeRequirements(c, requirements)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to store requirements: %v", err)), nil
	}
	s.scheduleSync()

	// Wire the capability into the knowledge graph (spec node + implements edge
	// to the affected code path) so context packs surface it for this path.
	_ = graph.EnsureSpecCapabilityEdges(s.pool.Writer, s.cfg.ProjectID, graph.SpecCapabilityRef{
		ChangeID:       c.ID,
		CapabilityPath: c.CapabilityPath,
		WherePath:      c.WherePath,
	})

	// Pre-flight check against rules/invariants (deterministic, zero LLM cost).
	preflight, pfErr := memory.PreflightCheck(s.pool.Reader, s.cfg.ProjectID, title, what, s.cfg.Settings.ConflictThreshold)
	s.usedSpecFlow.Store(true)

	return s.respond(req, renderProposedChange(c, reqSummary, preflight, pfErr)), nil
}

// storeChangeRequirements replaces the change's delta requirements and returns
// a per-delta summary ("2 added", "1 modified", ...).
func (s *Server) storeChangeRequirements(c *memory.Change, requirements string) ([]string, error) {
	if requirements == "" {
		return nil, nil
	}
	deltas := memory.ParseSpecDeltas(requirements)
	if err := memory.ReplaceChangeRequirements(s.pool.Writer, s.cfg.ProjectID, c.ID, c.CapabilityPath, deltas); err != nil {
		return nil, err
	}
	var summary []string
	for _, d := range deltas {
		summary = append(summary, fmt.Sprintf("%d %s", len(d.Requirements), strings.ToLower(d.Op)))
	}
	return summary, nil
}

func renderProposedChange(c *memory.Change, reqSummary []string, preflight *memory.PreflightResult, pfErr error) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Change proposed: `%s`\n\n", c.Slug)
	fmt.Fprintf(&sb, "- **ID:** `%s`\n- **Status:** `%s`\n", c.ID, c.Status)
	if c.WherePath != "" {
		fmt.Fprintf(&sb, "- **Affects:** `%s`\n", c.WherePath)
	}
	if c.CapabilityPath != "" {
		fmt.Fprintf(&sb, "- **Capability:** `%s`\n", c.CapabilityPath)
	}
	if len(reqSummary) > 0 {
		fmt.Fprintf(&sb, "- **Requirements:** %s\n", strings.Join(reqSummary, ", "))
	}
	if c.Tasks != "" {
		if prog := memory.ParseTaskProgress(c.Tasks); prog.Total > 0 {
			fmt.Fprintf(&sb, "- **Tasks:** %s\n", prog.Summary)
		}
	}
	if pfErr == nil {
		sb.WriteString("\n" + renderPreflight(preflight))
	} else {
		fmt.Fprintf(&sb, "\n_Pre-flight check unavailable: %v_\n", pfErr)
	}
	return sb.String()
}

// handleUpdateSpec updates an existing change proposal (tasks, design, what,
// goal, where_path, capability_path, requirements). It allows AI agents to
// update checklist progress (- [x]), refine design/tasks during the Apply phase,
// and keep the spec mirror synchronized.
func (s *Server) handleUpdateSpec(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	changeID, err := req.RequireString("change_id")
	if err != nil {
		return mcp.NewToolResultError("missing required field: change_id"), nil
	}

	c, err := resolveChangeByIDOrSlug(s.pool.Reader, s.cfg.ProjectID, changeID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if c == nil {
		return mcp.NewToolResultError(fmt.Sprintf("change %q not found", changeID)), nil
	}

	upd, updatedFields := collectChangeUpdate(req)

	if len(updatedFields) > 0 {
		c, err = memory.UpdateChange(s.pool.Writer, s.cfg.ProjectID, c.ID, upd)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to update change: %v", err)), nil
		}
	}

	// Update delta requirements if provided
	if reqs := req.GetString("requirements", ""); reqs != "" {
		deltas := memory.ParseSpecDeltas(reqs)
		if err = memory.ReplaceChangeRequirements(s.pool.Writer, s.cfg.ProjectID, c.ID, c.CapabilityPath, deltas); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to update requirements: %v", err)), nil
		}
		updatedFields = append(updatedFields, "requirements")
	}

	s.scheduleSync()
	return s.respond(req, renderUpdatedChange(c, updatedFields)), nil
}

// collectChangeUpdate gathers the optional fields present in an update request
// and the list of their names, in the historic order.
func collectChangeUpdate(req mcp.CallToolRequest) (memory.ChangeUpdate, []string) {
	upd := memory.ChangeUpdate{}
	var updatedFields []string
	if title := req.GetString("title", ""); title != "" {
		upd.Title = &title
		updatedFields = append(updatedFields, "title")
	}
	if what := req.GetString("what", ""); what != "" {
		upd.What = &what
		updatedFields = append(updatedFields, "what")
	}
	if goal := req.GetString("goal", ""); goal != "" {
		upd.Goal = &goal
		updatedFields = append(updatedFields, "goal")
	}
	if wherePath := req.GetString("where_path", ""); wherePath != "" {
		upd.WherePath = &wherePath
		updatedFields = append(updatedFields, "where_path")
	}
	if capabilityPath := req.GetString("capability_path", ""); capabilityPath != "" {
		upd.CapabilityPath = &capabilityPath
		updatedFields = append(updatedFields, "capability_path")
	}
	if design := req.GetString("design", ""); design != "" {
		upd.Design = &design
		updatedFields = append(updatedFields, "design")
	}
	if tasks := req.GetString("tasks", ""); tasks != "" {
		upd.Tasks = &tasks
		updatedFields = append(updatedFields, "tasks")
	}
	return upd, updatedFields
}

func renderUpdatedChange(c *memory.Change, updatedFields []string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Change updated: `%s`\n\n", c.Slug)
	fmt.Fprintf(&sb, "- **ID:** `%s`\n- **Status:** `%s`\n", c.ID, c.Status)
	if len(updatedFields) > 0 {
		fmt.Fprintf(&sb, "- **Updated Fields:** %s\n", strings.Join(updatedFields, ", "))
	}
	if c.Tasks != "" {
		if prog := memory.ParseTaskProgress(c.Tasks); prog.Total > 0 {
			fmt.Fprintf(&sb, "- **Task Progress:** %s\n", prog.Summary)
		}
	}
	return sb.String()
}

// handleValidateDecision re-checks a change's proposal against the project's
// rules and invariants after edits. Semantic validation is opt-in: with
// semantic=true a single batched agent call re-ranks the deterministic
// candidates by meaning (fail-open to the deterministic verdict when the agent
// is unavailable).
func (s *Server) handleValidateDecision(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	changeID, err := req.RequireString("change_id")
	if err != nil {
		return mcp.NewToolResultError("missing required field: change_id"), nil
	}
	semantic := req.GetString("semantic", "") == "true"
	agent := req.GetString("semantic_agent", "")

	c, err := memory.GetChange(s.pool.Reader, s.cfg.ProjectID, changeID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load change: %v", err)), nil
	}
	if c == nil {
		return mcp.NewToolResultText(fmt.Sprintf("Change `%s` not found in this project.", changeID)), nil
	}
	if c.Status == memory.ChangeStatusArchived || c.Status == memory.ChangeStatusRejected {
		return mcp.NewToolResultText(fmt.Sprintf("Change `%s` is %s — validation is only meaningful for active proposals.", c.Slug, c.Status)), nil
	}

	preflight, err := s.runPreflight(ctx, semantic, agent, c)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to validate decision: %v", err)), nil
	}

	// Validate the change's delta requirements against the current capability
	// state (RFC 2119 presence + MODIFIED scenario drops). Best-effort.
	reqIssues, reqErr := memory.ValidateChangeRequirements(s.pool.Reader, s.cfg.ProjectID, c.ID)
	if reqErr != nil {
		debugLog("requirements validation unavailable for %s: %v", c.ID, reqErr)
	}

	return s.respond(req, renderValidation(c, semantic, preflight, reqIssues)+s.advanceToValidated(c, preflight)), nil
}

// runPreflight runs the deterministic or semantic pre-flight check.
func (s *Server) runPreflight(ctx context.Context, semantic bool, agent string, c *memory.Change) (*memory.PreflightResult, error) {
	if semantic {
		return memory.SemanticPreflight(ctx, s.pool.Reader, s.cfg.ProjectID, c.Title, c.What, agent, s.cfg.Settings.ConflictThreshold)
	}
	return memory.PreflightCheck(s.pool.Reader, s.cfg.ProjectID, c.Title, c.What, s.cfg.Settings.ConflictThreshold)
}

func renderValidation(c *memory.Change, semantic bool, preflight *memory.PreflightResult, reqIssues []memory.RequirementIssue) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Validation: `%s`\n\n", c.Slug)
	if semantic {
		sb.WriteString("*Semantic re-ranking enabled — verdict may reflect the agent's judgment.*\n\n")
	}
	sb.WriteString(renderPreflight(preflight))
	if len(reqIssues) > 0 {
		sb.WriteString("\n### Requirements validation\n")
		for _, it := range reqIssues {
			fmt.Fprintf(&sb, "- **%s:** %s\n", strings.ToUpper(it.Level), it.Message)
		}
		sb.WriteString("\n*Fix warnings in the delta requirements (edit the mirror at `.sv-memory/specs/changes/" + c.Slug + ".md` and run `sv-memory specs import " + c.Slug + "`).*\n")
	}
	if c.Tasks != "" {
		if prog := memory.ParseTaskProgress(c.Tasks); prog.Total > 0 {
			fmt.Fprintf(&sb, "\n### Tasks progress: %s\n", prog.Summary)
		}
	}
	sb.WriteString("\n\nTo proceed once the proposal is sound, run `sv_commit_spec change_id=\"" + c.ID + "\"`.")
	return sb.String()
}

// advanceToValidated moves a sound proposal (PASS/WARN) to the validated state.
// A BLOCK keeps the change proposed so the conflict must be resolved first;
// terminal states are never re-stamped. Returns the status note or "".
func (s *Server) advanceToValidated(c *memory.Change, preflight *memory.PreflightResult) string {
	if preflight.Verdict == memory.PreflightBlock {
		return ""
	}
	switch c.Status {
	case memory.ChangeStatusApplied, memory.ChangeStatusArchived, memory.ChangeStatusRejected:
		return ""
	}
	if _, tErr := memory.UpdateChangeStatus(s.pool.Writer, s.cfg.ProjectID, c.ID, memory.ChangeStatusValidated); tErr != nil {
		return ""
	}
	return "\n_Status updated to `validated`._"
}

// handleSpecList lists active spec changes with their status, title, task
// progress, and capability. Mirrors `openspec list` for MCP consumers — the
// primary way agents discover what is pending without a raw CLI call.
const maxDefaultAppliedSpecs = 5

func (s *Server) handleSpecList(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.usedSpecFlow.Store(true)
	status := req.GetString("status", "")

	changes, err := memory.ListChangesByStatus(s.pool.Reader, s.cfg.ProjectID, status)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list changes: %v", err)), nil
	}

	display, hiddenApplied := selectSpecChanges(changes, status)
	if len(display) == 0 {
		return mcp.NewToolResultText("No active changes in this project. Create one with `sv_propose_spec`."), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Active spec changes (%d)\n\n", len(display))
	fmt.Fprintf(&sb, "%-12s %-24s %-14s %s\n", "STATUS", "SLUG", "TASKS", "TITLE")

	for _, c := range display {
		tasksSummary := "-"
		if prog := memory.ParseTaskProgress(c.Tasks); prog.Total > 0 {
			tasksSummary = prog.Summary
		}
		fmt.Fprintf(&sb, "%-12s %-24s %-14s %s\n",
			c.Status, c.Slug, tasksSummary, memory.TruncateText(c.Title, 50))
	}
	if hiddenApplied > 0 {
		fmt.Fprintf(&sb, "\n*(+%d older applied changes omitted for token economy — pass `status=\"applied\"` to list all)*\n", hiddenApplied)
	}
	sb.WriteString("\nUse `sv_spec_get(change_id=\"<slug>\")` to inspect a specific change.")
	return s.respond(req, sb.String()), nil
}

// selectSpecChanges picks the changes to display: pending (draft/proposed/
// validated) plus a bounded tail of applied ones when no status filter is set.
func selectSpecChanges(changes []*memory.Change, status string) ([]*memory.Change, int) {
	if status != "" {
		return changes, 0
	}
	var pending, applied []*memory.Change
	for _, c := range changes {
		switch c.Status {
		case memory.ChangeStatusDraft, memory.ChangeStatusProposed, memory.ChangeStatusValidated:
			pending = append(pending, c)
		case memory.ChangeStatusApplied:
			applied = append(applied, c)
		}
	}
	display := append([]*memory.Change{}, pending...)
	if len(applied) > maxDefaultAppliedSpecs {
		display = append(display, applied[:maxDefaultAppliedSpecs]...)
		return display, len(applied) - maxDefaultAppliedSpecs
	}
	return append(display, applied...), 0
}

// handleSpecGet returns a full change record: proposal, goal, design, tasks
// (with checkbox progress), and rendered delta requirements. Mirrors
// `openspec show` for MCP consumers.
func (s *Server) handleSpecGet(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.usedSpecFlow.Store(true)
	changeID, err := req.RequireString("change_id")
	if err != nil {
		return mcp.NewToolResultError("missing required field: change_id"), nil
	}

	c, err := resolveChangeByIDOrSlug(s.pool.Reader, s.cfg.ProjectID, changeID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if c == nil {
		return mcp.NewToolResultError(fmt.Sprintf("change %q not found", changeID)), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## `%s` — %s\n\n", c.Slug, c.Title)
	fmt.Fprintf(&sb, "- **ID:** `%s`\n- **Status:** `%s`\n", c.ID, c.Status)
	if c.WherePath != "" {
		fmt.Fprintf(&sb, "- **Affects:** `%s`\n", c.WherePath)
	}
	if c.CapabilityPath != "" {
		fmt.Fprintf(&sb, "- **Capability:** `%s`\n", c.CapabilityPath)
	}
	if prog := memory.ParseTaskProgress(c.Tasks); prog.Total > 0 {
		fmt.Fprintf(&sb, "- **Tasks:** %s\n", prog.Summary)
	}

	if c.What != "" {
		fmt.Fprintf(&sb, "\n### Proposal\n\n%s\n", c.What)
	}
	if c.Goal != "" {
		fmt.Fprintf(&sb, "\n### Goal\n\n%s\n", c.Goal)
	}
	if c.Design != "" {
		fmt.Fprintf(&sb, "\n### Design\n\n%s\n", c.Design)
	}
	if c.Tasks != "" {
		fmt.Fprintf(&sb, "\n### Tasks\n\n%s\n", c.Tasks)
	}

	deltas, dErr := memory.LoadChangeDeltas(s.pool.Reader, s.cfg.ProjectID, c.ID)
	if dErr == nil && len(deltas) > 0 {
		sb.WriteString("\n### Delta requirements\n\n")
		sb.WriteString(memory.DeltasToMarkdown(deltas))
		sb.WriteString("\n")
	}
	return s.respond(req, sb.String()), nil
}

// handleCommitSpec promotes a validated change into a durable decision/standard
// memory, wires its rationale_for edge to the affected code path, and stamps
// the change as applied. A BLOCK verdict is honored: the commit is rejected
// unless force=true is passed (the agent must explicitly override an invariant).
func (s *Server) handleCommitSpec(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	changeID, err := req.RequireString("change_id")
	if err != nil {
		return mcp.NewToolResultError("missing required field: change_id"), nil
	}
	force := req.GetString("force", "") == "true"
	category := req.GetString("category", "decision")

	c, err := memory.GetChange(s.pool.Writer, s.cfg.ProjectID, changeID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load change: %v", err)), nil
	}
	if c == nil {
		return mcp.NewToolResultText(fmt.Sprintf("Change `%s` not found in this project.", changeID)), nil
	}
	if c.Status == memory.ChangeStatusArchived || c.Status == memory.ChangeStatusRejected {
		return mcp.NewToolResultText(fmt.Sprintf("Change `%s` is %s — nothing to commit.", c.Slug, c.Status)), nil
	}
	if c.Status == memory.ChangeStatusApplied {
		return mcp.NewToolResultText(fmt.Sprintf("Change `%s` is already applied. A committed decision already exists.", c.Slug)), nil
	}

	// Pre-flight gate: a BLOCK requires an explicit force override.
	preflight, pfErr := memory.PreflightCheck(s.pool.Writer, s.cfg.ProjectID, c.Title, c.What, s.cfg.Settings.ConflictThreshold)
	if pfErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to run pre-flight gate: %v", pfErr)), nil
	}
	if preflight.Verdict == memory.PreflightBlock && !force {
		return mcp.NewToolResultText(renderCommitBlocked(preflight)), nil
	}

	mem := s.buildDecisionMemory(c, category)

	// Load the delta requirements for the atomic commit. Zero deltas is a
	// valid commit (a decision without requirements).
	deltas, dErr := memory.LoadChangeDeltas(s.pool.Writer, s.cfg.ProjectID, c.ID)
	if dErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load change requirements: %v", dErr)), nil
	}

	// Commit the authoritative state atomically: decision memory + change link
	// + capability delta merge + lifecycle stamp all land (or none do). On a
	// partial failure (e.g. ADDED of an existing requirement) nothing is
	// persisted, so the delta can be fixed and the commit retried cleanly.
	saved, err := memory.CommitChangeAtomic(s.pool.Writer, mem, c.ID, c.CapabilityPath, deltas, memory.ChangeStatusApplied)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to commit spec: %v — fix the delta and re-run sv_validate_decision", err)), nil
	}

	// Graph wiring and conflict judgments are derived, best-effort concerns
	// that run AFTER the authoritative transaction commits, so a graph hiccup
	// never rolls back the commit.
	s.wireCommittedChange(c, saved)
	s.recordCommitConflicts(c, saved, preflight)
	s.scheduleSync()

	return s.respond(req, renderCommittedChange(c, saved, deltas, preflight)), nil
}

// buildDecisionMemory assembles the durable decision/standard memory from the
// committed change and the cached git metadata.
func (s *Server) buildDecisionMemory(c *memory.Change, category string) *memory.Memory {
	gitMeta := s.cachedGitMetadata()
	what := c.Title
	if c.What != "" {
		what = c.Title + ": " + c.What
	}
	learned := c.Design
	if learned == "" {
		learned = "Committed via the spec-driven decision engine from change " + c.Slug
	}
	mem := &memory.Memory{
		ProjectID: s.cfg.ProjectID,
		Category:  category,
		What:      what,
		Why:       c.Goal,
		WherePath: c.WherePath,
		Learned:   learned,
		GitBranch: gitMeta.branch,
		GitCommit: gitMeta.commit,
		Author:    gitMeta.author,
		TopicKey:  "decision/" + c.Slug,
	}
	if mem.Why == "" {
		mem.Why = "No explicit goal recorded for the change; see change " + c.Slug
	}
	return mem
}

// wireCommittedChange links the committed decision memory to its capability and
// affected code path in the knowledge graph (best-effort).
func (s *Server) wireCommittedChange(c *memory.Change, saved *memory.Memory) {
	_ = graph.EnsureMemoryRationaleEdge(s.pool.Writer, s.cfg.ProjectID, graph.MemoryRationaleRef{
		ID:        saved.ID,
		Category:  saved.Category,
		What:      saved.What,
		WherePath: saved.WherePath,
	})
	_ = graph.LinkDecisionToCapability(s.pool.Writer, s.cfg.ProjectID, saved.ID, c.CapabilityPath)
	_ = graph.EnsureSpecCapabilityEdges(s.pool.Writer, s.cfg.ProjectID, graph.SpecCapabilityRef{
		ChangeID:       c.ID,
		CapabilityPath: c.CapabilityPath,
		WherePath:      c.WherePath,
	})
}

// recordCommitConflicts records conflicts_with relations for rules the
// proposal overlaps, so pending-conflict surfacing (Auto-Boot hint +
// sv_mem_conflicts) sees them.
func (s *Server) recordCommitConflicts(c *memory.Change, saved *memory.Memory, preflight *memory.PreflightResult) {
	if preflight.Verdict == memory.PreflightPass {
		return
	}
	for _, it := range preflight.Issues {
		if it.MemoryID == saved.ID {
			continue
		}
		if _, jErr := memory.SaveJudgment(s.pool.Writer, s.cfg.ProjectID, saved.ID, it.MemoryID,
			"conflicts_with", "Pre-flight flagged similarity during commit of change "+c.Slug, "agent"); jErr != nil {
			debugLog("failed to record conflicts_with for %s: %v", it.MemoryID, jErr)
		}
	}
}

func renderCommitBlocked(preflight *memory.PreflightResult) string {
	var sb strings.Builder
	sb.WriteString("## Commit blocked by pre-flight check\n\n")
	sb.WriteString(renderPreflight(preflight))
	sb.WriteString("\n\nOverride the invariant explicitly by passing `force=\"true\"` if you have confirmed the rule no longer applies (and update/archive the conflicting rule).")
	return sb.String()
}

func renderCommittedChange(c *memory.Change, saved *memory.Memory, deltas []memory.Delta, preflight *memory.PreflightResult) string {
	mergedReqs := 0
	for _, d := range deltas {
		mergedReqs += len(d.Requirements)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Committed: `%s`\n\n", c.Slug)
	fmt.Fprintf(&sb, "- **Decision memory:** `%s` (ID: %s)\n", saved.ID, saved.ID)
	fmt.Fprintf(&sb, "- **Status:** `applied`\n")
	if mergedReqs > 0 {
		fmt.Fprintf(&sb, "- **Capability:** `%s` — %d requirement(s) merged into the current state\n", c.CapabilityPath, mergedReqs)
	}
	if preflight.Verdict == memory.PreflightWarn {
		sb.WriteString("\n_Committed with a WARN — review the flagged rules with `sv_mem_judge` if they should be superseded._\n")
	}
	return sb.String()
}
