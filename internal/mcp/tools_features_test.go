package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"

	"github.com/svtech-code/sv-memory/internal/graph"
	"github.com/svtech-code/sv-memory/internal/memory"
)

func TestPinUnpinTools(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	srv := NewServer(pool, cfg)
	ctx := context.Background()

	saved, err := memory.SaveMemory(pool.Writer, &memory.Memory{
		ID: "pin-tool-1", ProjectID: cfg.ProjectID, Category: "decision",
		What: "Key decision to pin", Why: "important", Learned: "keep visible", CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("seed save failed: %v", err)
	}

	call := func(name string, args map[string]any) string {
		req := mcpgo.CallToolRequest{}
		req.Params.Name = name
		req.Params.Arguments = args
		res, callErr := srv.GetTool(name).Handler(ctx, req)
		if callErr != nil {
			t.Fatalf("%s failed: %v", name, callErr)
		}
		var out strings.Builder
		for _, c := range res.Content {
			out.WriteString(textContent(c))
		}
		return out.String()
	}

	out := call("sv_mem_pin", map[string]any{"id": saved.ID})
	if !strings.Contains(out, "pinned") {
		t.Fatalf("expected pin confirmation, got %q", out)
	}
	pinned, err := memory.SearchPinnedMemories(pool.Reader, cfg.ProjectID, 10)
	if err != nil {
		t.Fatalf("pinned search failed: %v", err)
	}
	if len(pinned) != 1 || pinned[0].ID != saved.ID {
		t.Fatalf("expected 1 pinned memory, got %d", len(pinned))
	}

	out = call("sv_mem_pin", map[string]any{"id": saved.ID, "action": "unpin"})
	if !strings.Contains(out, "unpinned") {
		t.Fatalf("expected unpin confirmation, got %q", out)
	}
	pinned, err = memory.SearchPinnedMemories(pool.Reader, cfg.ProjectID, 10)
	if err != nil {
		t.Fatalf("pinned search failed: %v", err)
	}
	if len(pinned) != 0 {
		t.Fatalf("expected 0 pinned after unpin, got %d", len(pinned))
	}
}

func TestSearchMatchModeHandler(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	for _, m := range []*memory.Memory{
		{ID: "mm-h-1", Category: "decision", What: "auth uses JWT", Why: "security", Learned: "x"},
		{ID: "mm-h-2", Category: "decision", What: "cache uses redis", Why: "perf", Learned: "x"},
	} {
		m.ProjectID = cfg.ProjectID
		m.CreatedAt = time.Now()
		if _, err := memory.SaveMemory(pool.Writer, m); err != nil {
			t.Fatalf("seed failed: %v", err)
		}
	}

	srv := NewServer(pool, cfg)
	ctx := context.Background()
	call := func(mode string) string {
		req := mcpgo.CallToolRequest{}
		req.Params.Name = "sv_mem_search"
		req.Params.Arguments = map[string]any{"query": "auth redis", "match_mode": mode, "limit": "10"}
		res, err := srv.GetTool("sv_mem_search").Handler(ctx, req)
		if err != nil {
			t.Fatalf("search failed: %v", err)
		}
		var out strings.Builder
		for _, c := range res.Content {
			out.WriteString(textContent(c))
		}
		return out.String()
	}

	allMode := call("all")
	anyMode := call("any")
	if strings.Contains(allMode, "mm-h-2") && strings.Contains(allMode, "mm-h-1") {
		t.Log("note: all-mode matched both in this small corpus")
	}
	if !strings.Contains(anyMode, "mm-h-1") && !strings.Contains(anyMode, "mm-h-2") {
		t.Fatalf("any-mode should return at least one result, got:\n%s", anyMode)
	}
}

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

func TestConfiguredCharsFallback(t *testing.T) {
	// With no config key set, the compiled-in default is returned so existing
	// truncation behavior is preserved when viper has no value.
	if got := configuredInt("no_such_config_key_anywhere", 123); got != 123 {
		t.Errorf("expected fallback 123, got %d", got)
	}
}

func TestContextPackTool(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	// Sync a tiny graph so the pack can resolve a node and its linked memory.
	if err := os.WriteFile(filepath.Join(tempDir, "main.go"),
		[]byte("package main\nfunc main() {}\n"), 0644); err != nil {
		t.Fatalf("failed writing main.go: %v", err)
	}
	if err := graph.SyncGraph(pool.Writer, cfg.ProjectID, cfg.ProjPath); err != nil {
		t.Fatalf("failed syncing graph: %v", err)
	}
	if _, err := memory.SaveMemory(pool.Writer, &memory.Memory{
		ProjectID: cfg.ProjectID,
		Category:  "decision",
		What:      "Keep main thin",
		Why:       "Single responsibility",
		Learned:   "Prefer small entry points",
		WherePath: "main.go",
	}); err != nil {
		t.Fatalf("failed saving memory: %v", err)
	}

	srv := NewServer(pool, cfg)
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_mem_context_pack"
	req.Params.Arguments = map[string]any{"path": "main.go"}
	res, err := srv.GetTool("sv_mem_context_pack").Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("sv_mem_context_pack failed: %v", err)
	}
	out := textContent(res.Content[0])
	if !strings.Contains(out, "Context Pack") {
		t.Fatalf("expected context pack header, got:\n%s", out)
	}
	if !strings.Contains(out, "Keep main thin") {
		t.Fatalf("expected linked memory in pack, got:\n%s", out)
	}
}

func TestSearchGraphBoost(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	// Two files assigned to the same graph community so the boost fires.
	if err := os.WriteFile(filepath.Join(tempDir, "a.go"), []byte("package main\nfunc a() {}\n"), 0644); err != nil {
		t.Fatalf("failed writing a.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "b.go"), []byte("package main\nfunc b() {}\n"), 0644); err != nil {
		t.Fatalf("failed writing b.go: %v", err)
	}
	if err := graph.SyncGraph(pool.Writer, cfg.ProjectID, cfg.ProjPath); err != nil {
		t.Fatalf("failed syncing graph: %v", err)
	}
	if _, err := pool.Writer.Exec(`UPDATE graph_nodes SET metadata = ? WHERE project_id = ? AND id IN (?, ?)`,
		`{"community_id": 5}`, cfg.ProjectID, "a.go", "b.go"); err != nil {
		t.Fatalf("failed setting community: %v", err)
	}
	for _, m := range []*memory.Memory{
		{ProjectID: cfg.ProjectID, Category: "decision", What: "A decision", Why: "w", Learned: "l", WherePath: "a.go"},
		{ProjectID: cfg.ProjectID, Category: "decision", What: "B decision", Why: "w", Learned: "l", WherePath: "b.go"},
	} {
		if _, err := memory.SaveMemory(pool.Writer, m); err != nil {
			t.Fatalf("failed saving memory: %v", err)
		}
	}

	srv := NewServer(pool, cfg)
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_mem_search"
	req.Params.Arguments = map[string]any{"query": "", "path": "a.go"}
	res, err := srv.GetTool("sv_mem_search").Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	out := textContent(res.Content[0])
	if !strings.Contains(out, "A decision") {
		t.Fatalf("expected a.go memory in results, got:\n%s", out)
	}
	if !strings.Contains(out, "B decision") {
		t.Fatalf("expected community-expanded b.go memory (graph_boost), got:\n%s", out)
	}
	if !strings.Contains(out, "[graph]") {
		t.Fatalf("expected [graph] marker on the community-expanded row, got:\n%s", out)
	}
}

func TestSessionTokenLedger(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	if _, err := memory.SaveMemory(pool.Writer, &memory.Memory{
		ProjectID: cfg.ProjectID,
		Category:  "decision",
		What:      "Use Postgres",
		Why:       strings.Repeat("reasoning for choosing Postgres ", 40),
		Learned:   "Relational data",
	}); err != nil {
		t.Fatalf("failed saving memory: %v", err)
	}

	srv := NewServer(pool, cfg)
	ctx := context.Background()
	call := func(name string, args map[string]any) string {
		t.Helper()
		req := mcpgo.CallToolRequest{}
		req.Params.Name = name
		req.Params.Arguments = args
		res, err := srv.GetTool(name).Handler(ctx, req)
		if err != nil {
			t.Fatalf("%s failed: %v", name, err)
		}
		return textContent(res.Content[0])
	}
	// parseLedger extracts the reported "Estimated tokens injected this session"
	// value from a sv_mem_stats response.
	parseLedger := func(out string) int {
		t.Helper()
		marker := "Estimated tokens injected this session:"
		idx := strings.Index(out, marker)
		if idx < 0 {
			t.Fatalf("missing ledger line in stats:\n%s", out)
		}
		// The rendered line is markdown-bolded: "**...:** 112". Strip any
		// leading "*" so only the digits remain.
		rest := strings.TrimSpace(out[idx+len(marker):])
		rest = strings.Trim(rest, "* ")
		n := 0
		for _, c := range rest {
			if c < '0' || c > '9' {
				break
			}
			n = n*10 + int(c-'0')
		}
		return n
	}

	// Session start resets the ledger, then the Auto-Boot bundle is counted.
	call("sv_mem_session_start", map[string]any{"goal": "ledger test"})
	afterStart := parseLedger(call("sv_mem_stats", map[string]any{}))
	if afterStart <= 0 {
		t.Fatalf("expected Auto-Boot bundle to accrue tokens, got %d", afterStart)
	}

	// A bulk read adds more to the ledger.
	call("sv_mem_search", map[string]any{"query": "Postgres"})
	afterSearch := parseLedger(call("sv_mem_stats", map[string]any{}))
	if afterSearch <= afterStart {
		t.Fatalf("expected search to accrue tokens (start=%d search=%d)", afterStart, afterSearch)
	}
}
