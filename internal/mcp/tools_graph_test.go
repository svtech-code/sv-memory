package mcp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"

	"github.com/svtech-code/sv-memory/internal/graph"
)

func TestGraphExplainHandler(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	writeMockCodeFiles(t, tempDir)

	server := NewServer(pool, cfg)
	explainTool := server.GetTool("sv_graph_explain")
	if explainTool == nil {
		t.Fatal("sv_graph_explain tool not registered")
	}
	ctx := context.Background()

	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_graph_explain"
	req.Params.Arguments = map[string]any{
		"node": "index.js",
	}

	res, err := explainTool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("sv_graph_explain failed: %v", err)
	}
	out := textContent(res.Content[0])

	assertions := map[string]string{
		"Structural Explanation": "section header",
		"index.js":               "resolved node label",
		"Network Metrics":        "metrics section",
		"Fan-In":                 "fan-in metric",
		"Fan-Out":                "fan-out metric",
		"Neighbors":              "neighbors section",
	}
	for want, desc := range assertions {
		if !strings.Contains(out, want) {
			t.Errorf("sv_graph_explain missing %s (expected %q), got:\n%s", desc, want, out)
		}
	}
}

func TestGraphExplainNodeNotFound(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	writeMockCodeFiles(t, tempDir)

	server := NewServer(pool, cfg)
	explainTool := server.GetTool("sv_graph_explain")
	ctx := context.Background()

	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_graph_explain"
	req.Params.Arguments = map[string]any{
		"node": "does-not-exist.go",
	}

	res, err := explainTool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("sv_graph_explain failed: %v", err)
	}
	out := textContent(res.Content[0])
	if !strings.Contains(out, "Could not find node") {
		t.Errorf("expected not-found message, got: %s", out)
	}
}

func TestGraphGodNodesHandler(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	writeMockCodeFiles(t, tempDir)

	server := NewServer(pool, cfg)
	godNodesTool := server.GetTool("sv_graph_god_nodes")
	if godNodesTool == nil {
		t.Fatal("sv_graph_god_nodes tool not registered")
	}
	ctx := context.Background()

	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_graph_god_nodes"
	req.Params.Arguments = map[string]any{
		"top_n": "5",
	}

	res, err := godNodesTool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("sv_graph_god_nodes failed: %v", err)
	}
	out := textContent(res.Content[0])

	// index.js is the only hub (it imports utils and Button), so it must rank.
	if !strings.Contains(out, "God Nodes") {
		t.Errorf("expected god nodes table header, got:\n%s", out)
	}
	if !strings.Contains(out, "index.js") {
		t.Errorf("expected index.js ranked as top node, got:\n%s", out)
	}
	if !strings.Contains(out, "| Rank |") {
		t.Errorf("expected markdown table header, got:\n%s", out)
	}
}

func TestGraphQueryCompactDefaultAndMermaidOptIn(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	writeMockCodeFiles(t, tempDir)

	server := NewServer(pool, cfg)
	queryTool := server.GetTool("sv_graph_query")
	if queryTool == nil {
		t.Fatal("sv_graph_query tool not registered")
	}
	ctx := context.Background()

	run := func(args map[string]any) string {
		t.Helper()
		req := mcpgo.CallToolRequest{}
		req.Params.Name = "sv_graph_query"
		req.Params.Arguments = args
		res, err := queryTool.Handler(ctx, req)
		if err != nil {
			t.Fatalf("sv_graph_query failed: %v", err)
		}
		return textContent(res.Content[0])
	}

	// Default: compact textual edge list, no Mermaid block, breakdown kept.
	out := run(map[string]any{"path_or_node": "index.js", "depth": "1"})
	if !strings.Contains(out, "→[imports]→") {
		t.Errorf("default output should contain the textual edge list, got:\n%s", out)
	}
	if strings.Contains(out, "```mermaid") {
		t.Errorf("default output should NOT contain a Mermaid diagram, got:\n%s", out)
	}
	if !strings.Contains(out, "Edge Confidence Breakdown") {
		t.Errorf("default output should keep the confidence breakdown, got:\n%s", out)
	}

	// Opt-in: Mermaid diagram replaces the textual edge list.
	outMermaid := run(map[string]any{"path_or_node": "index.js", "depth": "1", "mermaid": "true"})
	if !strings.Contains(outMermaid, "```mermaid") {
		t.Errorf("mermaid=true should render the Mermaid diagram, got:\n%s", outMermaid)
	}
	if strings.Contains(outMermaid, "→[imports]→") {
		t.Errorf("mermaid=true should not emit the textual edge list, got:\n%s", outMermaid)
	}
}

func TestEscapeMermaid(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain", in: "index.js", want: `"index.js"`},
		{name: "backslash", in: `a\b`, want: `"a/b"`},
		{name: "spaces", in: "my file.go", want: `"my file.go"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := escapeMermaid(tt.in); got != tt.want {
				t.Errorf("escapeMermaid(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestCommLabelStr(t *testing.T) {
	tests := []struct {
		name   string
		commID int
		labels map[int]string
		want   string
	}{
		{name: "zero community", commID: 0, labels: nil, want: "none"},
		{name: "with label", commID: 2, labels: map[int]string{2: "core"}, want: "core (ID 2)"},
		{name: "unknown community", commID: 3, labels: nil, want: "community_3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := commLabelStr(tt.commID, tt.labels); got != tt.want {
				t.Errorf("commLabelStr(%d) = %q, want %q", tt.commID, got, tt.want)
			}
		})
	}
}

func TestTokenBenchmark(t *testing.T) {
	t.Run("empty nodes returns empty", func(t *testing.T) {
		if got := tokenBenchmark(nil, 50); got != "" {
			t.Errorf("expected empty benchmark for no nodes, got: %q", got)
		}
	})

	t.Run("estimates savings from loc metadata", func(t *testing.T) {
		nodes := []*graph.Node{
			{ID: "a", Metadata: map[string]interface{}{"loc": float64(100)}},
		}
		got := tokenBenchmark(nodes, 50)
		if !strings.Contains(got, "Token savings") || !strings.Contains(got, "400 tokens") {
			t.Errorf("unexpected benchmark output: %q", got)
		}
	})

	t.Run("defaults to 50 loc without metadata", func(t *testing.T) {
		nodes := []*graph.Node{{ID: "a"}}
		got := tokenBenchmark(nodes, 25)
		// 50 LOC * 4 = 200 raw tokens vs 25 response tokens.
		if !strings.Contains(got, "Token savings") || !strings.Contains(got, "200 tokens") {
			t.Errorf("unexpected benchmark output: %q", got)
		}
	})

	t.Run("zero response tokens returns empty", func(t *testing.T) {
		nodes := []*graph.Node{{ID: "a", Metadata: map[string]interface{}{"loc": float64(100)}}}
		if got := tokenBenchmark(nodes, 0); got != "" {
			t.Errorf("expected empty benchmark for zero response tokens, got: %q", got)
		}
	})
}

func TestGraphDiffHandler(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	// Initialize git repo in tempDir
	cmd := exec.Command("git", "init")
	cmd.Dir = tempDir
	_ = cmd.Run()
	cmd = exec.Command("git", "config", "user.name", "Tester")
	cmd.Dir = tempDir
	_ = cmd.Run()
	cmd = exec.Command("git", "config", "user.email", "tester@example.com")
	cmd.Dir = tempDir
	_ = cmd.Run()

	file1 := filepath.Join(tempDir, "core.go")
	_ = os.WriteFile(file1, []byte("package main\n\nfunc OldFeature() {}\n"), 0644)
	cmd = exec.Command("git", "add", "core.go")
	cmd.Dir = tempDir
	_ = cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "init")
	cmd.Dir = tempDir
	_ = cmd.Run()

	// Add new function in working tree
	_ = os.WriteFile(file1, []byte("package main\n\nfunc OldFeature() {}\nfunc NewFeature() {}\n"), 0644)

	server := NewServer(pool, cfg)
	diffTool := server.GetTool("sv_graph_diff")
	if diffTool == nil {
		t.Fatal("sv_graph_diff tool not registered")
	}

	ctx := context.Background()
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_graph_diff"
	req.Params.Arguments = map[string]any{"base_ref": "HEAD"}

	res, err := diffTool.Handler(ctx, req)
	if err != nil || res.IsError {
		t.Fatalf("sv_graph_diff failed: err=%v, res=%v", err, res)
	}

	text := textContent(res.Content[0])
	if !strings.Contains(text, "Structural Graph Diff vs `HEAD`") {
		t.Errorf("expected header, got: %s", text)
	}
	if !strings.Contains(text, "NewFeature") {
		t.Errorf("expected NewFeature in diff, got: %s", text)
	}
}
