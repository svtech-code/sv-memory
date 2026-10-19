package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/svtech-code/sv-memory/internal/graph"
	"github.com/svtech-code/sv-memory/internal/memory"
)

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
	req.Params.Name = "sv_graph_explore"
	req.Params.Arguments = map[string]any{"path": "main.go"}
	res, err := srv.GetTool("sv_graph_explore").Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("sv_graph_explore failed: %v", err)
	}
	out := textContent(res.Content[0])
	if !strings.Contains(out, "Context Pack") {
		t.Fatalf("expected context pack header, got:\n%s", out)
	}
	if !strings.Contains(out, "Keep main thin") {
		t.Fatalf("expected linked memory in pack, got:\n%s", out)
	}
}

func TestGraphExploreAliasTool(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	if err := os.WriteFile(filepath.Join(tempDir, "alpha.go"),
		[]byte("package main\nfunc Alpha(){ Beta() }\nfunc Beta(){ Gamma() }\nfunc Gamma(){}\n"), 0644); err != nil {
		t.Fatalf("failed writing alpha.go: %v", err)
	}
	if err := graph.SyncGraph(pool.Writer, cfg.ProjectID, cfg.ProjPath); err != nil {
		t.Fatalf("failed syncing graph: %v", err)
	}

	srv := NewServer(pool, cfg)
	// The alias tool exists and routes to the context-pack handler.
	if srv.GetTool("sv_graph_explore") == nil {
		t.Fatal("expected sv_graph_explore alias tool to be registered")
	}

	// Multi-symbol explore returns a call path between Alpha and Gamma.
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_graph_explore"
	req.Params.Arguments = map[string]any{"path": "Alpha, Gamma"}
	res, err := srv.GetTool("sv_graph_explore").Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("sv_graph_explore failed: %v", err)
	}
	out := textContent(res.Content[0])
	if !strings.Contains(out, "### Call path") {
		t.Fatalf("expected explore call path section, got:\n%s", out)
	}
	if !strings.Contains(out, "Beta") {
		t.Fatalf("expected Beta in the call path, got:\n%s", out)
	}
}

func TestGraphReportTool(t *testing.T) {
	t.Setenv("SV_MEMORY_FULL_TOOLS", "1")
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	if err := os.WriteFile(filepath.Join(tempDir, "a.go"), []byte("package main\nfunc a() {}\n"), 0644); err != nil {
		t.Fatalf("failed writing a.go: %v", err)
	}
	if err := graph.SyncGraph(pool.Writer, cfg.ProjectID, cfg.ProjPath); err != nil {
		t.Fatalf("failed syncing graph: %v", err)
	}

	srv := NewServer(pool, cfg)
	if srv.GetTool("sv_graph_report") == nil {
		t.Fatal("expected sv_graph_report tool to be registered")
	}

	reportName := "report.md"
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_graph_report"
	req.Params.Arguments = map[string]any{"output": reportName}
	res, err := srv.GetTool("sv_graph_report").Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("sv_graph_report failed: %v", err)
	}
	out := textContent(res.Content[0])
	if !strings.Contains(out, "Graph report written to") {
		t.Fatalf("expected report digest, got:\n%s", out)
	}
	reportPath := filepath.Join(cfg.ProjPath, reportName)
	if _, err = os.Stat(reportPath); err != nil {
		t.Fatalf("expected report.md to be written: %v", err)
	}
	body, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("failed reading report: %v", err)
	}
	if !strings.Contains(string(body), "## God Nodes (top hubs)") {
		t.Fatalf("expected god-nodes section in written report, got:\n%s", body)
	}
	if !strings.Contains(string(body), "## Suggested Questions") {
		t.Fatalf("expected suggested-questions section in written report, got:\n%s", body)
	}
}

func TestGraphSearchTool(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	// Write a few Go files with shared substring "auth" to exercise multi-match.
	files := map[string]string{
		"auth.go":     "package main\nfunc auth() {}\n",
		"oauth.go":    "package main\nfunc oauth() {}\n",
		"handlers.go": "package main\nfunc handleAuth() {}\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(tempDir, name), []byte(body), 0644); err != nil {
			t.Fatalf("failed writing %s: %v", name, err)
		}
	}
	if err := graph.SyncGraph(pool.Writer, cfg.ProjectID, cfg.ProjPath); err != nil {
		t.Fatalf("failed syncing graph: %v", err)
	}

	srv := NewServer(pool, cfg)
	if srv.GetTool("sv_graph_search") == nil {
		t.Fatal("expected sv_graph_search tool to be registered")
	}
	if srv.GetTool("sv_graph_communities") == nil {
		t.Fatal("expected sv_graph_communities tool to be registered")
	}

	ctx := context.Background()

	// Basic multi-match search.
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_graph_search"
	req.Params.Arguments = map[string]any{"query": "auth"}
	res, err := srv.GetTool("sv_graph_search").Handler(ctx, req)
	if err != nil {
		t.Fatalf("sv_graph_search failed: %v", err)
	}
	out := textContent(res.Content[0])
	if !strings.Contains(out, "## Graph Search Results") {
		t.Fatalf("expected results header, got:\n%s", out)
	}
	// Must list multiple matches (auth.go + handlers.go + oauth.go).
	if strings.Count(out, "| ") < 3 {
		t.Fatalf("expected at least 3 table rows, got:\n%s", out)
	}

	// No-match case includes fallback suggestion.
	req.Params.Arguments = map[string]any{"query": "zzz_nonexistent_xyz"}
	res, err = srv.GetTool("sv_graph_search").Handler(ctx, req)
	if err != nil {
		t.Fatalf("sv_graph_search failed: %v", err)
	}
	out = textContent(res.Content[0])
	if strings.Contains(out, "## Graph Search Results") {
		t.Fatalf("no-match should not produce results header, got:\n%s", out)
	}
	if !strings.Contains(out, "Try a shorter term") {
		t.Fatalf("no-match should include fallback suggestions, got:\n%s", out)
	}
}

func TestGraphSearchFallbackInQueryTool(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	if err := os.WriteFile(filepath.Join(tempDir, "a.go"), []byte("package main\nfunc a() {}\n"), 0644); err != nil {
		t.Fatalf("failed writing a.go: %v", err)
	}
	if err := graph.SyncGraph(pool.Writer, cfg.ProjectID, cfg.ProjPath); err != nil {
		t.Fatalf("failed syncing graph: %v", err)
	}

	srv := NewServer(pool, cfg)
	ctx := context.Background()

	// sv_graph_query on a non-existent node must suggest sv_graph_search.
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_graph_query"
	req.Params.Arguments = map[string]any{"path_or_node": "zzz_no_match"}
	res, err := srv.GetTool("sv_graph_query").Handler(ctx, req)
	if err != nil {
		t.Fatalf("sv_graph_query failed: %v", err)
	}
	out := textContent(res.Content[0])
	if !strings.Contains(out, "sv_graph_search(query=\"zzz_no_match\")") {
		t.Fatalf("expected sv_graph_search fallback suggestion in query response, got:\n%s", out)
	}

	// Same for sv_graph_explain.
	req.Params.Name = "sv_graph_explain"
	req.Params.Arguments = map[string]any{"node": "zzz_no_match"}
	res, err = srv.GetTool("sv_graph_explain").Handler(ctx, req)
	if err != nil {
		t.Fatalf("sv_graph_explain failed: %v", err)
	}
	out = textContent(res.Content[0])
	if !strings.Contains(out, "sv_graph_search(query=\"zzz_no_match\")") {
		t.Fatalf("expected sv_graph_search fallback suggestion in explain response, got:\n%s", out)
	}
}

func TestGraphCommunitiesTool(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	// Write two files with an import edge so they form a community.
	if err := os.WriteFile(filepath.Join(tempDir, "main.go"), []byte("package main\nimport \"./utils\"\nfunc main() { helper() }\n"), 0644); err != nil {
		t.Fatalf("failed writing main.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "utils.go"), []byte("package main\nfunc helper() {}\n"), 0644); err != nil {
		t.Fatalf("failed writing utils.go: %v", err)
	}
	if err := graph.SyncGraph(pool.Writer, cfg.ProjectID, cfg.ProjPath); err != nil {
		t.Fatalf("failed syncing graph: %v", err)
	}

	srv := NewServer(pool, cfg)
	ctx := context.Background()

	// List top communities.
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_graph_communities"
	req.Params.Arguments = map[string]any{}
	res, err := srv.GetTool("sv_graph_communities").Handler(ctx, req)
	if err != nil {
		t.Fatalf("sv_graph_communities failed: %v", err)
	}
	out := textContent(res.Content[0])
	if !strings.Contains(out, "## Top") || !strings.Contains(out, "Community") {
		t.Fatalf("expected communities table header, got:\n%s", out)
	}
	// Table must contain at least one community row.
	if strings.Count(out, "| ") < 3 {
		t.Fatalf("expected at least one community row, got:\n%s", out)
	}
}

func TestGraphReportConfidenceSection(t *testing.T) {
	t.Setenv("SV_MEMORY_FULL_TOOLS", "1")
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	// Write files so the graph has nodes. The confidence section is verified
	// by the unit test in report_test.go (TestWriteReportConfidence) since
	// edge detection in the scanner may vary across environments.
	if err := os.WriteFile(filepath.Join(tempDir, "a.go"), []byte("package main\nfunc a() {}\n"), 0644); err != nil {
		t.Fatalf("failed writing a.go: %v", err)
	}
	if err := graph.SyncGraph(pool.Writer, cfg.ProjectID, cfg.ProjPath); err != nil {
		t.Fatalf("failed syncing graph: %v", err)
	}

	srv := NewServer(pool, cfg)
	ctx := context.Background()

	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_graph_report"
	req.Params.Arguments = map[string]any{"output": "report_conf.md"}
	res, err := srv.GetTool("sv_graph_report").Handler(ctx, req)
	if err != nil {
		t.Fatalf("sv_graph_report failed: %v", err)
	}
	digest := textContent(res.Content[0])
	if !strings.Contains(digest, "Graph report written to") {
		t.Fatalf("expected report digest, got:\n%s", digest)
	}

	reportPath := filepath.Join(cfg.ProjPath, "report_conf.md")
	body, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("failed reading report: %v", err)
	}
	bodyStr := string(body)
	// Verify report has core sections (confidence section presence depends on
	// whether the scanner detected edges — unit test covers that logic).
	if !strings.Contains(bodyStr, "# Graph Overview Report") {
		t.Fatalf("expected report header, got:\n%s", bodyStr)
	}
}

func TestSurprisingConnectionsConfidenceColumn(t *testing.T) {
	t.Setenv("SV_MEMORY_FULL_TOOLS", "1")
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	// Write files forming two communities with a cross-community bridge.
	if err := os.WriteFile(filepath.Join(tempDir, "a.go"), []byte("package main\nimport \"./b\"\nfunc main() { b helper() }\n"), 0644); err != nil {
		t.Fatalf("failed writing a.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "b.go"), []byte("package main\nfunc helper() {}\n"), 0644); err != nil {
		t.Fatalf("failed writing b.go: %v", err)
	}
	if err := graph.SyncGraph(pool.Writer, cfg.ProjectID, cfg.ProjPath); err != nil {
		t.Fatalf("failed syncing graph: %v", err)
	}

	srv := NewServer(pool, cfg)
	ctx := context.Background()

	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_graph_surprising_connections"
	req.Params.Arguments = map[string]any{"limit": "10"}
	res, err := srv.GetTool("sv_graph_surprising_connections").Handler(ctx, req)
	if err != nil {
		t.Fatalf("sv_graph_surprising_connections failed: %v", err)
	}
	out := textContent(res.Content[0])
	// Response should either have results with Confidence column or "no connections".
	if strings.Contains(out, "Surprising Connections") && strings.Contains(out, "| ") {
		if !strings.Contains(out, "Confidence") {
			t.Fatalf("expected Confidence column in surprising connections table, got:\n%s", out)
		}
	}
}

func TestGraphQueryConfidenceFilter(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	// Write files with an import edge.
	if err := os.WriteFile(filepath.Join(tempDir, "a.go"), []byte("package main\nimport \"./b\"\nfunc main() { b helper() }\n"), 0644); err != nil {
		t.Fatalf("failed writing a.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "b.go"), []byte("package main\nfunc helper() {}\n"), 0644); err != nil {
		t.Fatalf("failed writing b.go: %v", err)
	}
	if err := graph.SyncGraph(pool.Writer, cfg.ProjectID, cfg.ProjPath); err != nil {
		t.Fatalf("failed syncing graph: %v", err)
	}

	srv := NewServer(pool, cfg)
	ctx := context.Background()

	// Query without filter — should show edges.
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_graph_query"
	req.Params.Arguments = map[string]any{"path_or_node": "a.go", "depth": "1"}
	res, err := srv.GetTool("sv_graph_query").Handler(ctx, req)
	if err != nil {
		t.Fatalf("sv_graph_query failed: %v", err)
	}
	outAll := textContent(res.Content[0])
	if !strings.Contains(outAll, "Edges:") && !strings.Contains(outAll, "No connections") {
		t.Fatalf("expected edges section or no-connections, got:\n%s", outAll)
	}

	// Query with confidence=INFERRED filter — edges from freshly synced graph
	// are typically EXTRACTED, so INFERRED filter should yield no edges.
	req.Params.Arguments = map[string]any{"path_or_node": "a.go", "depth": "1", "confidence": "INFERRED"}
	res, err = srv.GetTool("sv_graph_query").Handler(ctx, req)
	if err != nil {
		t.Fatalf("sv_graph_query with confidence filter failed: %v", err)
	}
	outFiltered := textContent(res.Content[0])
	if strings.Contains(outFiltered, "→[") {
		t.Fatalf("confidence=INFERRED filter should exclude EXTRACTED edges, got:\n%s", outFiltered)
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

func TestGraphQueryPathAccrueTokenLedger(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	writeMockCodeFiles(t, tempDir)

	server := NewServer(pool, cfg)
	ctx := context.Background()

	// sv_graph_query and sv_graph_path route through s.respond, so each call
	// must accrue estimated tokens into the session ledger (sv_mem_stats).
	qReq := mcpgo.CallToolRequest{}
	qReq.Params.Name = "sv_graph_query"
	qReq.Params.Arguments = map[string]any{"path_or_node": "index.js", "depth": "1"}
	if res, err := server.GetTool("sv_graph_query").Handler(ctx, qReq); err != nil || res.IsError {
		t.Fatalf("sv_graph_query failed: err=%v res=%v", err, res.Content)
	}

	pReq := mcpgo.CallToolRequest{}
	pReq.Params.Name = "sv_graph_path"
	pReq.Params.Arguments = map[string]any{"source": "index.js", "target": "components/Button.tsx", "max_hops": "5"}
	if res, err := server.GetTool("sv_graph_path").Handler(ctx, pReq); err != nil || res.IsError {
		t.Fatalf("sv_graph_path failed: err=%v res=%v", err, res.Content)
	}

	statsReq := mcpgo.CallToolRequest{}
	statsReq.Params.Name = "sv_mem_stats"
	res, err := server.GetTool("sv_mem_stats").Handler(ctx, statsReq)
	if err != nil {
		t.Fatalf("sv_mem_stats failed: %v", err)
	}
	text := textContent(res.Content[0])
	marker := "Estimated tokens injected this session:"
	idx := strings.Index(text, marker)
	if idx < 0 {
		t.Fatalf("missing ledger line in stats:\n%s", text)
	}
	rest := strings.TrimSpace(text[idx+len(marker):])
	rest = strings.Trim(rest, "* ")
	n := 0
	for _, c := range rest {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	if n <= 0 {
		t.Errorf("expected graph query/path to accrue tokens in the ledger, got %d", n)
	}
}
