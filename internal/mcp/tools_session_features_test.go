package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/spf13/viper"
	"github.com/svtech-code/sv-memory/internal/graph"
	"github.com/svtech-code/sv-memory/internal/memory"
)

func TestSessionStartIncludesGraphHubs(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	// Write two Go files with an import edge so the graph has a real hub.
	if err := os.WriteFile(filepath.Join(tempDir, "main.go"),
		[]byte(`package main; import "./utils"; func main() { helper() }`), 0644); err != nil {
		t.Fatalf("failed writing main.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "utils.go"),
		[]byte(`package main; func helper() {}`), 0644); err != nil {
		t.Fatalf("failed writing utils.go: %v", err)
	}
	if err := graph.SyncGraph(pool.Writer, cfg.ProjectID, cfg.ProjPath); err != nil {
		t.Fatalf("failed syncing graph: %v", err)
	}

	srv := NewServer(pool, cfg)
	ctx := context.Background()
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_mem_session_start"
	req.Params.Arguments = map[string]any{"goal": "session with graph hubs"}
	res, err := srv.GetTool("sv_mem_session_start").Handler(ctx, req)
	if err != nil {
		t.Fatalf("session_start failed: %v", err)
	}
	out := textContent(res.Content[0])
	if !strings.Contains(out, "Session started") {
		t.Fatalf("expected started session, got:\n%s", out)
	}
	if !strings.Contains(out, "Graph Hubs") {
		t.Fatalf("expected Graph Hubs section in session_start bundle, got:\n%s", out)
	}
	if !strings.Contains(out, "main.go") && !strings.Contains(out, "utils.go") {
		t.Fatalf("expected a hub node to be listed, got:\n%s", out)
	}
}

func TestSessionStartHonorsTokenBudget(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	// Seed a decision with a long rationale so the Auto-Boot bundle has
	// enough content to exceed a tiny token budget.
	if _, err := memory.SaveMemory(pool.Writer, &memory.Memory{
		ProjectID: cfg.ProjectID,
		Category:  "decision",
		What:      "Use Postgres",
		Why:       strings.Repeat("very long rationale that would make the Auto-Boot bundle huge ", 20),
		Learned:   "Keep relational data in Postgres",
	}); err != nil {
		t.Fatalf("failed to seed memory: %v", err)
	}

	srv := NewServer(pool, cfg)
	ctx := context.Background()
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_mem_session_start"
	req.Params.Arguments = map[string]any{"token_budget": "1"}
	res, err := srv.GetTool("sv_mem_session_start").Handler(ctx, req)
	if err != nil {
		t.Fatalf("session_start failed: %v", err)
	}
	out := textContent(res.Content[0])
	if !strings.Contains(out, "[!] Response truncated to ~1 tokens") {
		t.Fatalf("expected session_start bundle to be truncated by token_budget, got:\n%s", out)
	}
}

func TestSessionStartAutoClosesStaleSessions(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	// Create a stale session (started 2 hours ago).
	stale, err := memory.StartSession(pool.Writer, cfg.ProjectID, "stale session", "/tmp")
	if err != nil {
		t.Fatalf("StartSession error: %v", err)
	}
	pool.Writer.Exec("UPDATE sessions SET started_at = ? WHERE id = ?", time.Now().Add(-2*time.Hour), stale.ID)

	// Verify stale session exists.
	var beforeCount int
	pool.Reader.QueryRow("SELECT count(*) FROM sessions WHERE project_id = ? AND status = 'active'", cfg.ProjectID).Scan(&beforeCount)
	if beforeCount != 1 {
		t.Fatalf("expected 1 active session before start, got %d", beforeCount)
	}

	// Set stale_session_hours to 1 so the 2-hour-old session is stale.
	viper.Set("stale_session_hours", 1)
	defer viper.Set("stale_session_hours", 0)

	// Start a new session — should auto-close the stale one.
	srv := NewServer(pool, cfg)
	ctx := context.Background()
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_mem_session_start"
	req.Params.Arguments = map[string]any{}
	res, err := srv.GetTool("sv_mem_session_start").Handler(ctx, req)
	if err != nil {
		t.Fatalf("session_start failed: %v", err)
	}
	out := textContent(res.Content[0])
	if !strings.Contains(out, "Session started") {
		t.Fatalf("expected started session, got:\n%s", out)
	}

	// Verify stale session is now completed.
	var staleStatus string
	pool.Reader.QueryRow("SELECT status FROM sessions WHERE id = ?", stale.ID).Scan(&staleStatus)
	if staleStatus != "completed" {
		t.Fatalf("expected stale session to be completed, got %s", staleStatus)
	}
}
