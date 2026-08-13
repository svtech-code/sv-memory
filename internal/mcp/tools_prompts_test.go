package mcp

import (
	"context"
	"strings"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"

	"github.com/svtech-code/sv-memory/internal/db"
	"github.com/svtech-code/sv-memory/internal/memory"
)

func TestCapturePromptHandler(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	server := NewServer(pool, cfg)
	captureTool := server.GetTool("sv_mem_capture_prompt")
	if captureTool == nil {
		t.Fatal("sv_mem_capture_prompt tool not registered")
	}

	ctx := context.Background()
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_mem_capture_prompt"
	req.Params.Arguments = map[string]any{
		"content": "Explain how the graph communities are computed.",
	}

	res, err := captureTool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("sv_mem_capture_prompt failed: %v", err)
	}
	if res.IsError {
		t.Fatalf("sv_mem_capture_prompt returned error: %v", res.Content)
	}

	out := textContent(res.Content[0])
	if !strings.Contains(out, "User prompt captured") {
		t.Errorf("expected capture message, got: %s", out)
	}

	// Prompt is persisted and counted by stats.
	stats, err := memory.GetStats(pool.Reader, cfg.ProjectID)
	if err != nil {
		t.Fatalf("failed to get stats: %v", err)
	}
	if stats.TotalPrompts != 1 {
		t.Errorf("expected 1 prompt in stats, got %d", stats.TotalPrompts)
	}
}

func TestCapturePromptHandlerRequiresContent(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	server := NewServer(pool, cfg)
	captureTool := server.GetTool("sv_mem_capture_prompt")

	ctx := context.Background()
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_mem_capture_prompt"
	req.Params.Arguments = map[string]any{}

	res, err := captureTool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("handler failed: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected error for missing content")
	}
	if !strings.Contains(textContent(res.Content[0]), "missing required field") {
		t.Errorf("unexpected error text: %s", textContent(res.Content[0]))
	}
}

func TestMergeProjectsHandler(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	// Register a second project to merge from (distinct path for the unique
	// constraint on projects.path).
	otherID := "other-proj"
	if err := db.RegisterProject(pool.Writer, otherID, "Other", tempDir+"/other"); err != nil {
		t.Fatalf("failed to register other project: %v", err)
	}

	// Seed one memory in the source project.
	if _, err := memory.SaveMemory(pool.Writer, &memory.Memory{
		ProjectID: otherID,
		Category:  "journal",
		What:      "Source observation",
		Why:       "reason",
		Learned:   "lesson",
	}); err != nil {
		t.Fatalf("failed to seed source memory: %v", err)
	}

	server := NewServer(pool, cfg)
	mergeTool := server.GetTool("sv_mem_merge_projects")
	if mergeTool == nil {
		t.Fatal("sv_mem_merge_projects tool not registered")
	}

	ctx := context.Background()
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_mem_merge_projects"
	req.Params.Arguments = map[string]any{
		"from": otherID,
		"to":   cfg.ProjectID,
	}

	res, err := mergeTool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("sv_mem_merge_projects failed: %v", err)
	}
	if res.IsError {
		t.Fatalf("sv_mem_merge_projects returned error: %v", res.Content)
	}

	out := textContent(res.Content[0])
	if !strings.Contains(out, "moved 1 memories") {
		t.Errorf("expected 1 memory moved, got: %s", out)
	}

	// Source project should be gone.
	projects, err := memory.ListProjects(pool.Reader)
	if err != nil {
		t.Fatalf("failed to list projects: %v", err)
	}
	for _, p := range projects {
		if p.ID == otherID {
			t.Error("expected source project to be deleted after merge")
		}
	}

	// Target project now owns the memory.
	stats, err := memory.GetStats(pool.Reader, cfg.ProjectID)
	if err != nil {
		t.Fatalf("failed to get stats: %v", err)
	}
	if stats.TotalMemories != 1 {
		t.Errorf("expected 1 memory in target after merge, got %d", stats.TotalMemories)
	}
}

func TestMergeProjectsHandlerRejectsSameProject(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	server := NewServer(pool, cfg)
	mergeTool := server.GetTool("sv_mem_merge_projects")

	ctx := context.Background()
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_mem_merge_projects"
	req.Params.Arguments = map[string]any{
		"from": cfg.ProjectID,
		"to":   cfg.ProjectID,
	}

	res, err := mergeTool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("handler failed: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected error for from == to")
	}
	if !strings.Contains(textContent(res.Content[0]), "must be different") {
		t.Errorf("unexpected error text: %s", textContent(res.Content[0]))
	}
}
