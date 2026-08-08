package mcp

import (
	"context"
	"strings"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

func TestUpdateMemoryHandler(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	server := NewServer(pool, cfg)
	updateTool := server.GetTool("sv_mem_update")
	if updateTool == nil {
		t.Fatal("sv_mem_update tool not registered")
	}

	ctx := context.Background()

	// Create a memory to update.
	saveTool := server.GetTool("sv_mem_save")
	saveReq := mcpgo.CallToolRequest{}
	saveReq.Params.Name = "sv_mem_save"
	saveReq.Params.Arguments = map[string]any{
		"category":   "architecture",
		"what":       "Original decision",
		"why":        "Original reasoning",
		"learned":    "Original lesson",
		"next_steps": "Do something",
	}
	saveRes, err := saveTool.Handler(ctx, saveReq)
	if err != nil {
		t.Fatalf("save failed: %v", err)
	}
	saveText := textContent(saveRes.Content[0])
	id := extractID(t, saveText)

	// Partial update: change 'what' and clear 'next_steps'.
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_mem_update"
	req.Params.Arguments = map[string]any{
		"id":         id,
		"what":       "Revised decision",
		"next_steps": "",
	}
	res, err := updateTool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	body := textContent(res.Content[0])
	if !strings.Contains(body, id) {
		t.Fatalf("expected response to reference memory %s, got %q", id, body)
	}

	// Verify with sv_mem_get.
	getTool := server.GetTool("sv_mem_get")
	getReq := mcpgo.CallToolRequest{}
	getReq.Params.Name = "sv_mem_get"
	getReq.Params.Arguments = map[string]any{"id": id}
	getRes, err := getTool.Handler(ctx, getReq)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	getBody := textContent(getRes.Content[0])
	if !strings.Contains(getBody, "Revised decision") {
		t.Fatalf("expected updated what, got %q", getBody)
	}
	if strings.Contains(getBody, "Do something") {
		t.Fatalf("expected next_steps cleared, got %q", getBody)
	}
	if !strings.Contains(getBody, "Original reasoning") {
		t.Fatalf("expected why preserved, got %q", getBody)
	}
}

func TestUpdateMemoryHandlerRequiresField(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	server := NewServer(pool, cfg)
	updateTool := server.GetTool("sv_mem_update")

	ctx := context.Background()

	// Missing id.
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_mem_update"
	req.Params.Arguments = map[string]any{"what": "x"}
	res, err := updateTool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !strings.Contains(textContent(res.Content[0]), "missing required field: id") {
		t.Fatalf("expected missing id error, got %q", textContent(res.Content[0]))
	}

	// No updatable fields.
	req2 := mcpgo.CallToolRequest{}
	req2.Params.Name = "sv_mem_update"
	req2.Params.Arguments = map[string]any{"id": "abc"}
	res2, err := updateTool.Handler(ctx, req2)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !strings.Contains(textContent(res2.Content[0]), "no updatable fields provided") {
		t.Fatalf("expected no-fields error, got %q", textContent(res2.Content[0]))
	}
}

func TestReviewMarkReviewedHandler(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	server := NewServer(pool, cfg)
	ctx := context.Background()

	// Create a memory and backdate its review deadline.
	saveTool := server.GetTool("sv_mem_save")
	saveReq := mcpgo.CallToolRequest{}
	saveReq.Params.Name = "sv_mem_save"
	saveReq.Params.Arguments = map[string]any{
		"category": "decision",
		"what":     "Decision due for review",
		"why":      "Needs revalidation",
		"learned":  "Revalidate periodically",
	}
	saveRes, err := saveTool.Handler(ctx, saveReq)
	if err != nil {
		t.Fatalf("save failed: %v", err)
	}
	id := extractID(t, textContent(saveRes.Content[0]))

	if _, err := pool.Writer.Exec("UPDATE memories SET review_after = ? WHERE id = ?", "2020-01-01 00:00:00", id); err != nil {
		t.Fatalf("failed to backdate review_after: %v", err)
	}

	// List should surface it as due for review.
	reviewTool := server.GetTool("sv_mem_review")
	listReq := mcpgo.CallToolRequest{}
	listReq.Params.Name = "sv_mem_review"
	listReq.Params.Arguments = map[string]any{"action": "list"}
	listRes, err := reviewTool.Handler(ctx, listReq)
	if err != nil {
		t.Fatalf("review list failed: %v", err)
	}
	if !strings.Contains(textContent(listRes.Content[0]), "policy review") {
		t.Fatalf("expected memory flagged for policy review, got %q", textContent(listRes.Content[0]))
	}

	// Mark reviewed.
	markReq := mcpgo.CallToolRequest{}
	markReq.Params.Name = "sv_mem_review"
	markReq.Params.Arguments = map[string]any{"action": "mark_reviewed", "id": id}
	markRes, err := reviewTool.Handler(ctx, markReq)
	if err != nil {
		t.Fatalf("mark_reviewed failed: %v", err)
	}
	if !strings.Contains(textContent(markRes.Content[0]), "marked as reviewed") {
		t.Fatalf("unexpected mark_reviewed response: %q", textContent(markRes.Content[0]))
	}

	// Verify it is no longer due.
	listRes2, err := reviewTool.Handler(ctx, listReq)
	if err != nil {
		t.Fatalf("review list failed: %v", err)
	}
	if strings.Contains(textContent(listRes2.Content[0]), "policy review") {
		t.Fatalf("expected memory no longer flagged, got %q", textContent(listRes2.Content[0]))
	}
}

func TestDiagnoseHandler(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	server := NewServer(pool, cfg)
	diagTool := server.GetTool("sv_mem_diagnose")
	if diagTool == nil {
		t.Fatal("sv_mem_diagnose tool not registered")
	}

	ctx := context.Background()
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_mem_diagnose"
	req.Params.Arguments = map[string]any{}

	res, err := diagTool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("diagnose failed: %v", err)
	}
	body := textContent(res.Content[0])
	if !strings.Contains(body, "Diagnostics") {
		t.Fatalf("expected diagnostics header, got %q", body)
	}
	if !strings.Contains(body, "database_connection") && !strings.Contains(body, "database_file") {
		t.Fatalf("expected database checks in output, got %q", body)
	}
	if !strings.Contains(body, "Graph Health") {
		t.Fatalf("expected graph health section, got %q", body)
	}
}

// extractID pulls the first memory ID from an sv_mem_save response like
// "Successfully created memory (ID: abcd1234) and synced ...".
func extractID(t *testing.T, s string) string {
	t.Helper()
	idx := strings.Index(s, "ID: ")
	if idx < 0 {
		t.Fatalf("no ID in response: %q", s)
	}
	rest := s[idx+4:]
	end := strings.IndexAny(rest, " )")
	if end < 0 {
		end = len(rest)
	}
	id := rest[:end]
	if id == "" {
		t.Fatalf("empty id in response: %q", s)
	}
	return id
}
