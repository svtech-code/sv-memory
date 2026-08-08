package mcp

import (
	"context"
	"strings"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/svtech-code/sv-memory/internal/memory"
)

func TestConflictsSemanticScanHandler(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	server := NewServer(pool, cfg)
	conflictsTool := server.GetTool("sv_mem_conflicts")
	if conflictsTool == nil {
		t.Fatal("sv_mem_conflicts tool not registered")
	}

	ctx := context.Background()

	// Save two lexically similar memories so the Jaccard scan finds a pair.
	saveTool := server.GetTool("sv_mem_save")
	for _, title := range []string{"User database uses PostgreSQL", "User database uses MongoDB"} {
		req := mcpgo.CallToolRequest{}
		req.Params.Name = "sv_mem_save"
		req.Params.Arguments = map[string]any{
			"category": "decision",
			"what":     title,
			"why":      "storage choice",
			"learned":  "pick one",
		}
		if _, err := saveTool.Handler(ctx, req); err != nil {
			t.Fatalf("save %q failed: %v", title, err)
		}
	}

	// Stub the agent runner so no real CLI is invoked.
	original := memory.SemanticRunAgent
	memory.SemanticRunAgent = func(ctx context.Context, agent, prompt string) (string, error) {
		return `{"relation_type":"conflicts_with","reason":"contradicting database choices"}`, nil
	}
	defer func() { memory.SemanticRunAgent = original }()

	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_mem_conflicts"
	req.Params.Arguments = map[string]any{
		"action":      "scan",
		"semantic":    "true",
		"apply":       "true",
		"agent":       "opencode",
		"concurrency": "2",
	}
	res, err := conflictsTool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("semantic scan failed: %v", err)
	}
	body := textContent(res.Content[0])
	if !strings.Contains(body, "CONFLICTS_WITH") {
		t.Fatalf("expected conflicts_with verdict in response, got: %q", body)
	}
	if !strings.Contains(body, "Persisted 1 judged") {
		t.Fatalf("expected persisted summary, got: %q", body)
	}

	// Verify a judged relation with judged_by='llm' was persisted.
	var relationType, judgedBy string
	err = pool.Writer.QueryRow(
		"SELECT relation_type, judged_by FROM memory_relations WHERE project_id=? AND judged_by='llm' LIMIT 1",
		cfg.ProjectID,
	).Scan(&relationType, &judgedBy)
	if err != nil {
		t.Fatalf("expected judged llm relation, got: %v", err)
	}
	if relationType != "conflicts_with" {
		t.Fatalf("expected conflicts_with relation, got %s", relationType)
	}
}
