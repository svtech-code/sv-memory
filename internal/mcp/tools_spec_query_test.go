package mcp

import (
	"context"
	"strings"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/svtech-code/sv-memory/internal/memory"
)

func TestUpdateSpecHandler(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	server := NewServer(pool, cfg)
	propose := server.GetTool("sv_propose_spec")
	update := server.GetTool("sv_update_spec")
	if propose == nil || update == nil {
		t.Fatal("sv_propose_spec or sv_update_spec tool not registered")
	}

	ctx := context.Background()

	// 1. Propose change
	propReq := mcpgo.CallToolRequest{}
	propReq.Params.Name = "sv_propose_spec"
	propReq.Params.Arguments = map[string]any{
		"slug":  "add-oauth-flow",
		"title": "Add OAuth Flow",
		"what":  "Implement OAuth2 login",
		"tasks": "- [ ] 1. Setup client\n- [ ] 2. Add callback handler",
	}
	res, err := propose.Handler(ctx, propReq)
	if err != nil || res.IsError {
		t.Fatalf("propose failed: err=%v, res=%v", err, res)
	}

	// 2. Update tasks (mark task 1 completed) by slug
	updReq := mcpgo.CallToolRequest{}
	updReq.Params.Name = "sv_update_spec"
	updReq.Params.Arguments = map[string]any{
		"change_id": "add-oauth-flow",
		"tasks":     "- [x] 1. Setup client\n- [ ] 2. Add callback handler",
		"design":    "Use standard oauth2 library with PKCE",
	}
	updRes, err := update.Handler(ctx, updReq)
	if err != nil || updRes.IsError {
		t.Fatalf("update failed: err=%v, res=%v", err, updRes)
	}
	text := textContent(updRes.Content[0])
	if !strings.Contains(text, "Change updated") {
		t.Errorf("expected 'Change updated' in response, got: %s", text)
	}
	if !strings.Contains(text, "1/2 (50%)") {
		t.Errorf("expected task progress summary '1/2 (50%%)', got: %s", text)
	}

	// Verify database state
	c, err := memory.GetChangeBySlug(pool.Reader, cfg.ProjectID, "add-oauth-flow")
	if err != nil || c == nil {
		t.Fatalf("failed to retrieve updated change: %v", err)
	}
	if c.Design != "Use standard oauth2 library with PKCE" {
		t.Errorf("expected updated design, got: %q", c.Design)
	}
	if !strings.Contains(c.Tasks, "- [x] 1. Setup client") {
		t.Errorf("expected updated tasks in DB, got: %q", c.Tasks)
	}
}

func TestSpecListHandler(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	server := NewServer(pool, cfg)
	tool := server.GetTool("sv_spec_list")
	if tool == nil {
		t.Fatal("sv_spec_list tool not registered")
	}

	ctx := context.Background()

	// Empty state
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_spec_list"
	res, err := tool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("sv_spec_list failed: %v", err)
	}
	text := textContent(res.Content[0])
	if !strings.Contains(text, "No active changes") {
		t.Errorf("expected 'No active changes', got: %s", text)
	}

	// Create a change with tasks
	propReq := mcpgo.CallToolRequest{}
	propReq.Params.Name = "sv_propose_spec"
	propReq.Params.Arguments = map[string]any{
		"slug":  "test-list",
		"title": "Test List Feature",
		"what":  "Verify sv_spec_list returns changes with task progress",
		"tasks": "- [x] 1. Done\n- [ ] 2. Pending",
	}
	_, propErr := server.GetTool("sv_propose_spec").Handler(ctx, propReq)
	if propErr != nil {
		t.Fatalf("propose failed: %v", propErr)
	}

	// List should now show the change
	res, err = tool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("sv_spec_list failed: %v", err)
	}
	text = textContent(res.Content[0])
	if !strings.Contains(text, "test-list") {
		t.Errorf("expected slug in list, got: %s", text)
	}
	if !strings.Contains(text, "1/2") {
		t.Errorf("expected task progress 1/2, got: %s", text)
	}

	// Create 7 applied changes to verify the default cap (max 5 applied shown when status is omitted)
	for i := 1; i <= 7; i++ {
		slug := "applied-change-" + string(rune('0'+i))
		c, cErr := memory.CreateChange(pool.Writer, cfg.ProjectID, slug, "Applied Title", "what", "", "", "", "")
		if cErr != nil {
			t.Fatalf("CreateChange %s failed: %v", slug, cErr)
		}
		if _, cErr = memory.UpdateChangeStatus(pool.Writer, cfg.ProjectID, c.ID, memory.ChangeStatusApplied); cErr != nil {
			t.Fatalf("UpdateChangeStatus %s failed: %v", slug, cErr)
		}
	}

	res, err = tool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("sv_spec_list with applied cap failed: %v", err)
	}
	text = textContent(res.Content[0])
	if !strings.Contains(text, "+2 older applied changes omitted") {
		t.Errorf("expected omission notice for +2 older applied changes, got:\n%s", text)
	}

	// Explicit status="applied" must list all 7 applied changes
	reqApplied := mcpgo.CallToolRequest{}
	reqApplied.Params.Name = "sv_spec_list"
	reqApplied.Params.Arguments = map[string]any{"status": "applied"}
	resApplied, err := tool.Handler(ctx, reqApplied)
	if err != nil {
		t.Fatalf("sv_spec_list(status=applied) failed: %v", err)
	}
	textApplied := textContent(resApplied.Content[0])
	if !strings.Contains(textApplied, "Active spec changes (7)") {
		t.Errorf("expected all 7 applied changes when status='applied', got:\n%s", textApplied)
	}
}

func TestSpecGetHandler(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	server := NewServer(pool, cfg)
	tool := server.GetTool("sv_spec_get")
	if tool == nil {
		t.Fatal("sv_spec_get tool not registered")
	}

	ctx := context.Background()

	// Create a change with full content
	propReq := mcpgo.CallToolRequest{}
	propReq.Params.Name = "sv_propose_spec"
	propReq.Params.Arguments = map[string]any{
		"slug":         "get-test-change",
		"title":        "Get Test Change",
		"what":         "Verify sv_spec_get returns full change record",
		"goal":         "Ensure completeness",
		"where_path":   "internal/test/",
		"tasks":        "- [ ] 1. First task\n- [ ] 2. Second task",
		"design":       "Simple test design",
		"requirements": "## ADDED Requirements\n\n### Requirement: Test\nThe system SHALL verify spec_get works.\n\n#### Scenario: Get change\n- WHEN sv_spec_get is called\n- THEN full record is returned\n",
	}
	_, propErr := server.GetTool("sv_propose_spec").Handler(ctx, propReq)
	if propErr != nil {
		t.Fatalf("propose failed: %v", propErr)
	}

	// Get by slug
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_spec_get"
	req.Params.Arguments = map[string]any{"change_id": "get-test-change"}
	res, err := tool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("sv_spec_get failed: %v", err)
	}
	text := textContent(res.Content[0])

	if !strings.Contains(text, "get-test-change") {
		t.Errorf("expected slug in response, got: %s", text)
	}
	if !strings.Contains(text, "Verify sv_spec_get returns full change record") {
		t.Errorf("expected 'what' (proposal) in response, got: %s", text)
	}
	if !strings.Contains(text, "Ensure completeness") {
		t.Errorf("expected goal in response, got: %s", text)
	}
	if !strings.Contains(text, "Simple test design") {
		t.Errorf("expected design in response, got: %s", text)
	}
	if !strings.Contains(text, "0/2") {
		t.Errorf("expected task progress 0/2, got: %s", text)
	}
	if !strings.Contains(text, "ADDED Requirements") {
		t.Errorf("expected delta requirements in response, got: %s", text)
	}
	if !strings.Contains(text, "The system SHALL verify spec_get works") {
		t.Errorf("expected requirement body in response, got: %s", text)
	}

	// Not found
	req.Params.Arguments = map[string]any{"change_id": "nonexistent"}
	res, err = tool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("sv_spec_get failed: %v", err)
	}
	if !res.IsError {
		t.Error("expected error for nonexistent change")
	}
}
