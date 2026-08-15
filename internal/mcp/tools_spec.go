package mcp

import (
	"context"
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

	// Store delta requirements (single capability per change) when provided.
	var reqSummary []string
	if requirements != "" {
		deltas := memory.ParseSpecDeltas(requirements)
		if err = memory.ReplaceChangeRequirements(s.pool.Writer, s.cfg.ProjectID, c.ID, c.CapabilityPath, deltas); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to store requirements: %v", err)), nil
		}
		for _, d := range deltas {
			reqSummary = append(reqSummary, fmt.Sprintf("%d %s", len(d.Requirements), strings.ToLower(d.Op)))
		}
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
	preflight, pfErr := memory.PreflightCheck(s.pool.Reader, s.cfg.ProjectID, title, what)

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Change proposed: `%s`\n\n", c.Slug)
	fmt.Fprintf(&sb, "- **ID:** `%s`\n- **Status:** `%s`\n", c.ID, c.Status)
	if c.WherePath != "" {
		fmt.Fprintf(&sb, "- **Affects:** `%s`\n", c.WherePath)
	}
	fmt.Fprintf(&sb, "- **Capability:** `%s`\n", c.CapabilityPath)
	if len(reqSummary) > 0 {
		fmt.Fprintf(&sb, "- **Requirements:** %s\n", strings.Join(reqSummary, ", "))
	}
	if pfErr == nil {
		sb.WriteString("\n" + renderPreflight(preflight))
	} else {
		fmt.Fprintf(&sb, "\n_Pre-flight check unavailable: %v_\n", pfErr)
	}
	return s.respond(req, sb.String()), nil
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

	var preflight *memory.PreflightResult
	if semantic {
		preflight, err = memory.SemanticPreflight(ctx, s.pool.Reader, s.cfg.ProjectID, c.Title, c.What, agent)
	} else {
		preflight, err = memory.PreflightCheck(s.pool.Reader, s.cfg.ProjectID, c.Title, c.What)
	}
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to validate decision: %v", err)), nil
	}

	// Validate the change's delta requirements against the current capability
	// state (RFC 2119 presence + MODIFIED scenario drops). Best-effort.
	reqIssues, reqErr := memory.ValidateChangeRequirements(s.pool.Reader, s.cfg.ProjectID, c.ID)
	if reqErr != nil {
		debugLog("requirements validation unavailable for %s: %v", c.ID, reqErr)
	}

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
	sb.WriteString("\n\nTo proceed once the proposal is sound, run `sv_commit_spec change_id=\"" + c.ID + "\"`.")
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
	preflight, pfErr := memory.PreflightCheck(s.pool.Writer, s.cfg.ProjectID, c.Title, c.What)
	if pfErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to run pre-flight gate: %v", pfErr)), nil
	}
	if preflight.Verdict == memory.PreflightBlock && !force {
		var sb strings.Builder
		sb.WriteString("## Commit blocked by pre-flight check\n\n")
		sb.WriteString(renderPreflight(preflight))
		sb.WriteString("\n\nOverride the invariant explicitly by passing `force=\"true\"` if you have confirmed the rule no longer applies (and update/archive the conflicting rule).")
		return mcp.NewToolResultText(sb.String()), nil
	}

	// Merge the change's delta requirements into the capability's current state.
	// A merge conflict (ADDED of an existing requirement, RENAMED of a missing
	// one) aborts the commit so the delta can be fixed first.
	var mergedReqs int
	if reqCount, mErr := mergeChangeDeltas(s, c); mErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to merge requirements: %v — fix the delta and re-run sv_validate_decision", mErr)), nil
	} else if reqCount > 0 {
		mergedReqs = reqCount
	}

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
	saved, err := memory.SaveMemory(s.pool.Writer, mem)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to commit decision memory: %v", err)), nil
	}
	if err = memory.SetMemoryChangeID(s.pool.Writer, s.cfg.ProjectID, saved.ID, c.ID); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to link memory to change: %v", err)), nil
	}

	// Wire the committed decision to the capability (implements edge) so the
	// graph connects decision -> capability -> code. Best-effort.
	_ = graph.LinkDecisionToCapability(s.pool.Writer, s.cfg.ProjectID, saved.ID, c.CapabilityPath)
	_ = graph.EnsureSpecCapabilityEdges(s.pool.Writer, s.cfg.ProjectID, graph.SpecCapabilityRef{
		ChangeID:       c.ID,
		CapabilityPath: c.CapabilityPath,
		WherePath:      c.WherePath,
	})

	// Record conflicts_with relations for rules the proposal overlaps, so the
	// pending-conflict surfacing (Auto-Boot hint + sv_mem_conflicts) sees them.
	if preflight.Verdict != memory.PreflightPass {
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

	if _, err = memory.UpdateChangeStatus(s.pool.Writer, s.cfg.ProjectID, c.ID, memory.ChangeStatusApplied); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to mark change applied: %v", err)), nil
	}
	s.scheduleSync()

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
	return s.respond(req, sb.String()), nil
}

// mergeChangeDeltas merges the change's requirements into its capability's
// current state and returns the number of requirement rows merged. Zero deltas
// is a no-op success.
func mergeChangeDeltas(s *Server, c *memory.Change) (int, error) {
	deltas, err := memory.LoadChangeDeltas(s.pool.Writer, s.cfg.ProjectID, c.ID)
	if err != nil {
		return 0, err
	}
	if len(deltas) == 0 {
		return 0, nil
	}
	if err := memory.MergeDeltas(s.pool.Writer, s.cfg.ProjectID, c.CapabilityPath, deltas); err != nil {
		return 0, err
	}
	total := 0
	for _, d := range deltas {
		total += len(d.Requirements)
	}
	return total, nil
}
