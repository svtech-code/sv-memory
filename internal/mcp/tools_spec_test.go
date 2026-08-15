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

func TestProposeSpecHandler(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	server := NewServer(pool, cfg)
	tool := server.GetTool("sv_propose_spec")
	if tool == nil {
		t.Fatal("sv_propose_spec tool not registered")
	}

	ctx := context.Background()
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_propose_spec"
	req.Params.Arguments = map[string]any{
		"slug":       "implement-session-auth",
		"title":      "Implement session-based auth",
		"what":       "Replace JWT stateless with server-side sessions",
		"where_path": "internal/auth/",
	}

	res, err := tool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("sv_propose_spec failed: %v", err)
	}
	if res.IsError {
		t.Fatalf("sv_propose_spec returned error: %v", res.Content)
	}
	text := textContent(res.Content[0])
	if !strings.Contains(text, "implement-session-auth") {
		t.Errorf("expected slug in response, got: %s", text)
	}
	if !strings.Contains(text, "Pre-flight") {
		t.Errorf("expected pre-flight section, got: %s", text)
	}

	// The change must be persisted with draft status.
	changes, err := memory.ListChangesByStatus(pool.Reader, cfg.ProjectID, memory.ChangeStatusDraft)
	if err != nil {
		t.Fatalf("failed to list changes: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected 1 draft change, got %d", len(changes))
	}
	if changes[0].Slug != "implement-session-auth" {
		t.Errorf("expected slug, got %s", changes[0].Slug)
	}
}

func TestProposeSpecHandlerValidation(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	server := NewServer(pool, cfg)
	tool := server.GetTool("sv_propose_spec")

	ctx := context.Background()
	// Missing slug.
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_propose_spec"
	req.Params.Arguments = map[string]any{"title": "Title"}
	res, err := tool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !res.IsError {
		t.Error("expected error for missing slug")
	}
}

func TestProposeSpecDuplicateSlug(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	server := NewServer(pool, cfg)
	tool := server.GetTool("sv_propose_spec")

	ctx := context.Background()
	mk := func() mcpgo.CallToolRequest {
		r := mcpgo.CallToolRequest{}
		r.Params.Name = "sv_propose_spec"
		r.Params.Arguments = map[string]any{"slug": "dup", "title": "T"}
		return r
	}
	if res, err := tool.Handler(ctx, mk()); err != nil {
		t.Fatalf("first propose failed: %v", err)
	} else if res.IsError {
		t.Fatalf("first propose errored: %v", res.Content)
	}
	res, err := tool.Handler(ctx, mk())
	if err != nil {
		t.Fatalf("second propose returned error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected error result for duplicate slug")
	}
	msg := textContent(res.Content[0])
	if !strings.Contains(msg, "already exists") {
		t.Errorf("expected duplicate slug message, got: %s", msg)
	}
}

func TestValidateDecisionHandler(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	// Seed a pinned invariant that the proposal contradicts.
	saveRuleForTest(t, pool, cfg, "architecture", "Stateless JWT authentication with server-side sessions is a pinned invariant", true)

	server := NewServer(pool, cfg)
	propose := server.GetTool("sv_propose_spec")
	validate := server.GetTool("sv_validate_decision")
	if propose == nil || validate == nil {
		t.Fatal("spec tools not registered")
	}

	ctx := context.Background()
	propReq := mcpgo.CallToolRequest{}
	propReq.Params.Name = "sv_propose_spec"
	propReq.Params.Arguments = map[string]any{
		"slug":  "session-auth",
		"title": "Session auth",
		"what":  "Authentication using stateless JWT with server-side sessions",
	}
	propRes, err := propose.Handler(ctx, propReq)
	if err != nil {
		t.Fatalf("propose failed: %v", err)
	}
	if propRes.IsError {
		t.Fatalf("propose errored: %v", propRes.Content)
	}

	changes, err := memory.ListChangesByStatus(pool.Reader, cfg.ProjectID, memory.ChangeStatusDraft)
	if err != nil || len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d (err=%v)", len(changes), err)
	}

	valReq := mcpgo.CallToolRequest{}
	valReq.Params.Name = "sv_validate_decision"
	valReq.Params.Arguments = map[string]any{"change_id": changes[0].ID}
	valRes, err := validate.Handler(ctx, valReq)
	if err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	if valRes.IsError {
		t.Fatalf("validate errored: %v", valRes.Content)
	}
	text := textContent(valRes.Content[0])
	if !strings.Contains(text, "BLOCK") {
		t.Errorf("expected BLOCK verdict for pinned invariant, got: %s", text)
	}
}

func TestValidateDecisionMissingChange(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	server := NewServer(pool, cfg)
	validate := server.GetTool("sv_validate_decision")

	ctx := context.Background()
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_validate_decision"
	req.Params.Arguments = map[string]any{"change_id": "nonexistent"}
	res, err := validate.Handler(ctx, req)
	if err != nil {
		t.Fatalf("validate returned error: %v", err)
	}
	if res.IsError {
		t.Fatal("expected non-error for missing change")
	}
	if !strings.Contains(textContent(res.Content[0]), "not found") {
		t.Errorf("expected not-found message, got: %s", textContent(res.Content[0]))
	}
}

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

	changes, err := memory.ListChangesByStatus(pool.Reader, cfg.ProjectID, memory.ChangeStatusDraft)
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

	changes, err := memory.ListChangesByStatus(pool.Reader, cfg.ProjectID, memory.ChangeStatusDraft)
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

	// The change must still be draft (not applied) — the gate held.
	drafts, err := memory.ListChangesByStatus(pool.Reader, cfg.ProjectID, memory.ChangeStatusDraft)
	if err != nil || len(drafts) != 1 {
		t.Fatalf("expected change to stay draft, got %d (err=%v)", len(drafts), err)
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

	changes, err := memory.ListChangesByStatus(pool.Reader, cfg.ProjectID, memory.ChangeStatusDraft)
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

func TestContextPackIncludeChanges(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	server := NewServer(pool, cfg)
	tool := server.GetTool("sv_mem_context_pack")
	if tool == nil {
		t.Fatal("sv_mem_context_pack tool not registered")
	}

	// Seed a change affecting a known path.
	_, err := memory.CreateChange(pool.Writer, cfg.ProjectID, "touch-main", "Touch main", "", "", "main.go", "", "")
	if err != nil {
		t.Fatalf("failed to create change: %v", err)
	}

	ctx := context.Background()

	// Without include_changes, no active-changes section.
	reqNo := mcpgo.CallToolRequest{}
	reqNo.Params.Name = "sv_mem_context_pack"
	reqNo.Params.Arguments = map[string]any{"path": "main.go"}
	resNo, err := tool.Handler(ctx, reqNo)
	if err != nil {
		t.Fatalf("context pack failed: %v", err)
	}
	if strings.Contains(textContent(resNo.Content[0]), "Active changes") {
		t.Error("did not expect active-changes section without include_changes")
	}

	// With include_changes=true, the section appears.
	reqYes := mcpgo.CallToolRequest{}
	reqYes.Params.Name = "sv_mem_context_pack"
	reqYes.Params.Arguments = map[string]any{"path": "main.go", "include_changes": "true"}
	resYes, err := tool.Handler(ctx, reqYes)
	if err != nil {
		t.Fatalf("context pack (include) failed: %v", err)
	}
	text := textContent(resYes.Content[0])
	if !strings.Contains(text, "Active changes") {
		t.Errorf("expected active-changes section, got: %s", text)
	}
	if !strings.Contains(text, "touch-main") {
		t.Errorf("expected change slug in response, got: %s", text)
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
