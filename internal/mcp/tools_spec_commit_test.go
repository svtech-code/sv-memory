package mcp

import (
	"context"
	"strings"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/svtech-code/sv-memory/internal/config"
	"github.com/svtech-code/sv-memory/internal/db"
	"github.com/svtech-code/sv-memory/internal/memory"
)

func TestCommitSpecHandler(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	server := NewServer(pool, cfg)
	propose := server.GetTool("sv_propose_spec")
	commit := server.GetTool("sv_commit_spec")
	if propose == nil || commit == nil {
		t.Fatal("spec tools not registered")
	}

	ctx := context.Background()
	propReq := mcpgo.CallToolRequest{}
	propReq.Params.Name = "sv_propose_spec"
	propReq.Params.Arguments = map[string]any{
		"slug":       "add-csv-export",
		"title":      "Add CSV export",
		"what":       "Let users export data in CSV format",
		"where_path": "internal/export/",
	}
	if res, err := propose.Handler(ctx, propReq); err != nil || res.IsError {
		t.Fatalf("propose failed: err=%v res=%v", err, res.Content)
	}

	changes, err := memory.ListChangesByStatus(pool.Reader, cfg.ProjectID, memory.ChangeStatusProposed)
	if err != nil || len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d (err=%v)", len(changes), err)
	}

	comReq := mcpgo.CallToolRequest{}
	comReq.Params.Name = "sv_commit_spec"
	comReq.Params.Arguments = map[string]any{"change_id": changes[0].ID, "category": "decision"}
	comRes, err := commit.Handler(ctx, comReq)
	if err != nil {
		t.Fatalf("commit failed: %v", err)
	}
	if comRes.IsError {
		t.Fatalf("commit errored: %v", comRes.Content)
	}
	text := textContent(comRes.Content[0])
	if !strings.Contains(text, "Committed") {
		t.Errorf("expected commit confirmation, got: %s", text)
	}

	// Change must be applied and a decision memory linked to it.
	applied, err := memory.ListChangesByStatus(pool.Reader, cfg.ProjectID, memory.ChangeStatusApplied)
	if err != nil || len(applied) != 1 {
		t.Fatalf("expected 1 applied change, got %d (err=%v)", len(applied), err)
	}
	if applied[0].ArchivedAt.IsZero() {
		t.Error("expected archived_at stamped on applied change")
	}

	// A decision memory with the topic key must exist and be linked.
	linked, err := memory.SearchMemories(pool.Reader, cfg.ProjectID, "csv", "decision", 5)
	if err != nil || len(linked) != 1 {
		t.Fatalf("expected committed decision memory, got %d (err=%v)", len(linked), err)
	}
	if linked[0].TopicKey != "decision/add-csv-export" {
		t.Errorf("expected topic key decision/add-csv-export, got %s", linked[0].TopicKey)
	}
	var changeID string
	if err = pool.Reader.QueryRow("SELECT COALESCE(change_id,'') FROM memories WHERE id = ?", linked[0].ID).Scan(&changeID); err != nil {
		t.Fatalf("failed to read change_id: %v", err)
	}
	if changeID != changes[0].ID {
		t.Errorf("expected memory linked to change %s, got %s", changes[0].ID, changeID)
	}
}

func TestCommitSpecBlockedByInvariant(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	saveRuleForTest(t, pool, cfg, "architecture", "Stateless JWT authentication with server-side sessions is a pinned invariant", true)

	server := NewServer(pool, cfg)
	propose := server.GetTool("sv_propose_spec")
	commit := server.GetTool("sv_commit_spec")

	ctx := context.Background()
	propReq := mcpgo.CallToolRequest{}
	propReq.Params.Name = "sv_propose_spec"
	propReq.Params.Arguments = map[string]any{
		"slug":  "session-auth",
		"title": "Session auth",
		"what":  "Authentication using stateless JWT with server-side sessions",
	}
	if res, err := propose.Handler(ctx, propReq); err != nil || res.IsError {
		t.Fatalf("propose failed: err=%v res=%v", err, res.Content)
	}

	changes, err := memory.ListChangesByStatus(pool.Reader, cfg.ProjectID, memory.ChangeStatusProposed)
	if err != nil || len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d (err=%v)", len(changes), err)
	}

	comReq := mcpgo.CallToolRequest{}
	comReq.Params.Name = "sv_commit_spec"
	comReq.Params.Arguments = map[string]any{"change_id": changes[0].ID}
	res, err := commit.Handler(ctx, comReq)
	if err != nil {
		t.Fatalf("commit returned error: %v", err)
	}
	if !strings.Contains(textContent(res.Content[0]), "blocked") {
		t.Errorf("expected block message, got: %s", textContent(res.Content[0]))
	}

	// The change must still be proposed (not applied) — the gate held.
	drafts, err := memory.ListChangesByStatus(pool.Reader, cfg.ProjectID, memory.ChangeStatusProposed)
	if err != nil || len(drafts) != 1 {
		t.Fatalf("expected change to stay proposed, got %d (err=%v)", len(drafts), err)
	}
}

func TestCommitSpecForceOverridesBlock(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	saveRuleForTest(t, pool, cfg, "architecture", "Stateless JWT authentication with server-side sessions is a pinned invariant", true)

	server := NewServer(pool, cfg)
	propose := server.GetTool("sv_propose_spec")
	commit := server.GetTool("sv_commit_spec")

	ctx := context.Background()
	propReq := mcpgo.CallToolRequest{}
	propReq.Params.Name = "sv_propose_spec"
	propReq.Params.Arguments = map[string]any{
		"slug":  "session-auth",
		"title": "Session auth",
		"what":  "Authentication using stateless JWT with server-side sessions",
	}
	if res, err := propose.Handler(ctx, propReq); err != nil || res.IsError {
		t.Fatalf("propose failed: err=%v res=%v", err, res.Content)
	}

	changes, err := memory.ListChangesByStatus(pool.Reader, cfg.ProjectID, memory.ChangeStatusProposed)
	if err != nil || len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d (err=%v)", len(changes), err)
	}

	comReq := mcpgo.CallToolRequest{}
	comReq.Params.Name = "sv_commit_spec"
	comReq.Params.Arguments = map[string]any{"change_id": changes[0].ID, "force": "true"}
	res, err := commit.Handler(ctx, comReq)
	if err != nil {
		t.Fatalf("commit returned error: %v", err)
	}
	if res.IsError {
		t.Fatalf("commit errored: %v", res.Content)
	}
	if !strings.Contains(textContent(res.Content[0]), "Committed") {
		t.Errorf("expected commit confirmation with force, got: %s", textContent(res.Content[0]))
	}

	// A conflicts_with relation should have been recorded for the invariant.
	linked, err := memory.SearchMemories(pool.Reader, cfg.ProjectID, "csv", "decision", 5)
	if err != nil {
		t.Fatalf("failed to search decision memory: %v", err)
	}
	// Search by topic to find the committed decision.
	allDecisions, err := memory.SearchMemories(pool.Reader, cfg.ProjectID, "session", "decision", 10)
	if err != nil || len(allDecisions) != 1 {
		t.Fatalf("expected committed decision, got %d (err=%v)", len(allDecisions), err)
	}
	rels, err := memory.GetRelations(pool.Reader, cfg.ProjectID, allDecisions[0].ID)
	if err != nil {
		t.Fatalf("failed to get relations: %v", err)
	}
	foundConflict := false
	for _, r := range rels {
		if r.RelationType == "conflicts_with" {
			foundConflict = true
		}
	}
	if !foundConflict {
		t.Error("expected a conflicts_with relation after force-committing against an invariant")
	}
	_ = linked
}

func TestCommitSpecMissingChange(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	server := NewServer(pool, cfg)
	commit := server.GetTool("sv_commit_spec")

	ctx := context.Background()
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_commit_spec"
	req.Params.Arguments = map[string]any{"change_id": "nonexistent"}
	res, err := commit.Handler(ctx, req)
	if err != nil {
		t.Fatalf("commit returned error: %v", err)
	}
	if !strings.Contains(textContent(res.Content[0]), "not found") {
		t.Errorf("expected not-found message, got: %s", textContent(res.Content[0]))
	}
}

func TestCommitSpecMergesRequirements(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	// Seed a canonical code node so the capability's implements edge resolves.
	if _, err := pool.Writer.Exec(
		"INSERT INTO graph_nodes (id, project_id, node_type, label, path, metadata) VALUES (?, ?, 'file', ?, ?, '{}')",
		"internal/auth/auth.go", cfg.ProjectID, "auth.go", "internal/auth/auth.go",
	); err != nil {
		t.Fatalf("seed code node: %v", err)
	}

	server := NewServer(pool, cfg)
	propose := server.GetTool("sv_propose_spec")
	commit := server.GetTool("sv_commit_spec")

	ctx := context.Background()
	propReq := mcpgo.CallToolRequest{}
	propReq.Params.Name = "sv_propose_spec"
	propReq.Params.Arguments = map[string]any{
		"slug":            "add-2fa",
		"title":           "Add 2FA",
		"what":            "Require a second factor during login",
		"where_path":      "internal/auth/auth.go",
		"capability_path": "auth",
		"requirements":    "## ADDED Requirements\n\n### Requirement: Two-Factor Authentication\nThe system MUST require a TOTP second factor.\n\n#### Scenario: OTP\n- **WHEN** valid credentials\n- **THEN** an OTP challenge is presented",
	}
	if res, err := propose.Handler(ctx, propReq); err != nil || res.IsError {
		t.Fatalf("propose failed: err=%v res=%v", err, res.Content)
	}

	changes, err := memory.ListChangesByStatus(pool.Reader, cfg.ProjectID, memory.ChangeStatusProposed)
	if err != nil || len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d (err=%v)", len(changes), err)
	}

	comReq := mcpgo.CallToolRequest{}
	comReq.Params.Name = "sv_commit_spec"
	comReq.Params.Arguments = map[string]any{"change_id": changes[0].ID}
	comRes, err := commit.Handler(ctx, comReq)
	if err != nil {
		t.Fatalf("commit failed: %v", err)
	}
	if comRes.IsError {
		t.Fatalf("commit errored: %v", comRes.Content)
	}
	text := textContent(comRes.Content[0])
	if !strings.Contains(text, "Committed") || !strings.Contains(text, "merged") {
		t.Errorf("expected commit with merge confirmation, got: %s", text)
	}

	// The requirement must be merged into the capability's current state.
	reqs, err := memory.CapabilityRequirements(pool.Reader, cfg.ProjectID, "auth")
	if err != nil || len(reqs) != 1 {
		t.Fatalf("expected 1 requirement in capability state, got %d (err=%v)", len(reqs), err)
	}
	if reqs[0].Requirement != "Two-Factor Authentication" {
		t.Errorf("expected merged requirement, got %q", reqs[0].Requirement)
	}

	// The change must be applied.
	applied, err := memory.ListChangesByStatus(pool.Reader, cfg.ProjectID, memory.ChangeStatusApplied)
	if err != nil || len(applied) != 1 {
		t.Fatalf("expected 1 applied change, got %d (err=%v)", len(applied), err)
	}

	// The capability node and the code implements edge must exist in the graph.
	var nodeType string
	if err = pool.Reader.QueryRow("SELECT node_type FROM graph_nodes WHERE project_id = ? AND id = 'spec:auth'", cfg.ProjectID).Scan(&nodeType); err != nil {
		t.Fatalf("expected capability node in graph: %v", err)
	}
	if nodeType != "spec" {
		t.Errorf("expected spec node type, got %q", nodeType)
	}
	var edgeCount int
	if err = pool.Reader.QueryRow(
		"SELECT COUNT(*) FROM graph_edges WHERE project_id = ? AND relation_type = 'implements' AND source_id = 'internal/auth/auth.go' AND target_id = 'spec:auth'",
		cfg.ProjectID).Scan(&edgeCount); err != nil {
		t.Fatalf("count implements edge: %v", err)
	}
	if edgeCount != 1 {
		t.Errorf("expected 1 implements edge from code to capability, got %d", edgeCount)
	}

	// The committed decision memory is linked to the capability via implements.
	linked, err := memory.SearchMemories(pool.Reader, cfg.ProjectID, "2fa", "decision", 5)
	if err != nil || len(linked) != 1 {
		t.Fatalf("expected committed decision, got %d (err=%v)", len(linked), err)
	}
	var decEdge int
	if err = pool.Reader.QueryRow(
		"SELECT COUNT(*) FROM graph_edges WHERE project_id = ? AND relation_type = 'implements' AND source_id = ? AND target_id = 'spec:auth'",
		cfg.ProjectID, linked[0].ID).Scan(&decEdge); err != nil {
		t.Fatalf("count decision edge: %v", err)
	}
	if decEdge != 1 {
		t.Errorf("expected decision -> capability implements edge, got %d", decEdge)
	}
}

// saveRuleForTest persists a rule-like memory (used by spec-engine tests).
func saveRuleForTest(t *testing.T, pool *db.Pool, cfg *config.Config, category, what string, pinned bool) {
	t.Helper()
	mem := &memory.Memory{
		ProjectID: cfg.ProjectID,
		Category:  category,
		What:      what,
		Why:       "test rule",
		Learned:   "test",
		Pinned:    pinned,
	}
	saved, err := memory.SaveMemory(pool.Writer, mem)
	if err != nil {
		t.Fatalf("failed to save rule: %v", err)
	}
	_ = saved
}

// TestCommitSpecAtomicOnMergeFailure verifies that a spec commit whose delta
// merge fails (an ADDED requirement that already exists in the capability
// state) leaves NO partial state behind: the decision memory is not saved, the
// capability is not mutated, and the change stays proposed so the delta can be
// fixed and the commit retried cleanly.
func TestCommitSpecAtomicOnMergeFailure(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	// Pre-seed the capability state with a requirement that the change's ADDED
	// delta will collide with.
	if _, err := pool.Writer.Exec(
		"INSERT INTO spec_capabilities (id, project_id, capability_path, requirement, updated_at) VALUES (?, ?, 'auth', 'Existing Req', CURRENT_TIMESTAMP)",
		"seed-req", cfg.ProjectID,
	); err != nil {
		t.Fatalf("seed capability requirement: %v", err)
	}

	server := NewServer(pool, cfg)
	propose := server.GetTool("sv_propose_spec")
	commit := server.GetTool("sv_commit_spec")

	ctx := context.Background()
	propReq := mcpgo.CallToolRequest{}
	propReq.Params.Name = "sv_propose_spec"
	propReq.Params.Arguments = map[string]any{
		"slug":            "collide-req",
		"title":           "Collide req",
		"what":            "Add a requirement that already exists",
		"capability_path": "auth",
		"requirements":    "## ADDED Requirements\n\n### Requirement: Existing Req\nSome body.",
	}
	if res, err := propose.Handler(ctx, propReq); err != nil || res.IsError {
		t.Fatalf("propose failed: err=%v res=%v", err, res.Content)
	}

	changes, err := memory.ListChangesByStatus(pool.Reader, cfg.ProjectID, memory.ChangeStatusProposed)
	if err != nil || len(changes) != 1 {
		t.Fatalf("expected 1 proposed change, got %d (err=%v)", len(changes), err)
	}

	comReq := mcpgo.CallToolRequest{}
	comReq.Params.Name = "sv_commit_spec"
	comReq.Params.Arguments = map[string]any{"change_id": changes[0].ID}
	comRes, err := commit.Handler(ctx, comReq)
	if err != nil {
		t.Fatalf("commit returned error: %v", err)
	}
	if !comRes.IsError {
		t.Fatalf("expected commit to fail on merge conflict, got: %s", textContent(comRes.Content[0]))
	}

	// No decision memory must exist for the change.
	dec, err := memory.SearchMemories(pool.Reader, cfg.ProjectID, "collide", "decision", 5)
	if err != nil {
		t.Fatalf("search decision memory: %v", err)
	}
	if len(dec) != 0 {
		t.Fatalf("expected NO decision memory after failed commit, got %d", len(dec))
	}

	// The capability state must be untouched (only the seed row).
	reqs, err := memory.CapabilityRequirements(pool.Reader, cfg.ProjectID, "auth")
	if err != nil {
		t.Fatalf("load capability requirements: %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected capability untouched (1 seed row), got %d", len(reqs))
	}

	// The change must still be proposed (not applied).
	proposed, err := memory.ListChangesByStatus(pool.Reader, cfg.ProjectID, memory.ChangeStatusProposed)
	if err != nil || len(proposed) != 1 {
		t.Fatalf("expected change still proposed, got %d (err=%v)", len(proposed), err)
	}
}
