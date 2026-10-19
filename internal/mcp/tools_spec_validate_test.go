package mcp

import (
	"context"
	"strings"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/svtech-code/sv-memory/internal/memory"
)

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

	changes, err := memory.ListChangesByStatus(pool.Reader, cfg.ProjectID, memory.ChangeStatusProposed)
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

func TestValidateDecisionReportsRequirements(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	server := NewServer(pool, cfg)
	propose := server.GetTool("sv_propose_spec")
	validate := server.GetTool("sv_validate_decision")

	ctx := context.Background()
	propReq := mcpgo.CallToolRequest{}
	propReq.Params.Name = "sv_propose_spec"
	propReq.Params.Arguments = map[string]any{
		"slug":            "loose-login",
		"title":           "Loose login",
		"what":            "Relax login rules",
		"capability_path": "auth",
		"requirements":    "## ADDED Requirements\n\n### Requirement: Relaxed Login\nNo normative keywords here.",
	}
	if res, err := propose.Handler(ctx, propReq); err != nil || res.IsError {
		t.Fatalf("propose failed: err=%v res=%v", err, res.Content)
	}

	changes, err := memory.ListChangesByStatus(pool.Reader, cfg.ProjectID, memory.ChangeStatusProposed)
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
	if !strings.Contains(textContent(valRes.Content[0]), "RFC 2119") {
		t.Errorf("expected RFC 2119 requirements warning, got: %s", textContent(valRes.Content[0]))
	}
}

func TestContextPackIncludeChanges(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	server := NewServer(pool, cfg)
	tool := server.GetTool("sv_graph_explore")
	if tool == nil {
		t.Fatal("sv_graph_explore tool not registered")
	}

	// Seed a change affecting a known path.
	_, err := memory.CreateChange(pool.Writer, cfg.ProjectID, "touch-main", "Touch main", "", "", "main.go", "", "")
	if err != nil {
		t.Fatalf("failed to create change: %v", err)
	}

	ctx := context.Background()

	// Without include_changes, no active-changes section.
	reqNo := mcpgo.CallToolRequest{}
	reqNo.Params.Name = "sv_graph_explore"
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
	reqYes.Params.Name = "sv_graph_explore"
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
