package mcp

import (
	"context"
	"strings"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

func TestGraphQueryHandler(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	writeMockCodeFiles(t, tempDir)

	server := NewServer(pool, cfg)
	queryTool := server.GetTool("sv_graph_query")
	ctx := context.Background()

	// 1. Query Out direction (default)
	reqQuery := mcpgo.CallToolRequest{}
	reqQuery.Params.Name = "sv_graph_query"
	reqQuery.Params.Arguments = map[string]any{
		"path_or_node": "index.js",
		"depth":        "1",
		"direction":    "out",
	}

	resQuery, err := queryTool.Handler(ctx, reqQuery)
	if err != nil {
		t.Fatalf("sv_graph_query out failed: %v", err)
	}
	textResult1 := textContent(resQuery.Content[0])
	if !strings.Contains(textResult1, "index.js") || !strings.Contains(textResult1, "utils.js") {
		t.Errorf("expected Out sub-graph to contain index.js and utils.js, got: %s", textResult1)
	}

	// 2. Query In direction
	reqQueryIn := mcpgo.CallToolRequest{}
	reqQueryIn.Params.Name = "sv_graph_query"
	reqQueryIn.Params.Arguments = map[string]any{
		"path_or_node": "components/Button.tsx",
		"depth":        "1",
		"direction":    "in",
	}

	resQueryIn, err := queryTool.Handler(ctx, reqQueryIn)
	if err != nil {
		t.Fatalf("sv_graph_query in failed: %v", err)
	}
	textResult2 := textContent(resQueryIn.Content[0])
	if !strings.Contains(textResult2, "index.js") || !strings.Contains(textResult2, "components/Button.tsx") {
		t.Errorf("expected In sub-graph of Button to contain imports/deps from index.js and components/Button.tsx, got: %s", textResult2)
	}
}

func TestGraphPathHandler(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	writeMockCodeFiles(t, tempDir)

	server := NewServer(pool, cfg)
	pathTool := server.GetTool("sv_graph_path")
	ctx := context.Background()

	reqPath := mcpgo.CallToolRequest{}
	reqPath.Params.Name = "sv_graph_path"
	reqPath.Params.Arguments = map[string]any{
		"source":   "index.js",
		"target":   "components/Button.tsx",
		"max_hops": "5",
	}

	resPath, err := pathTool.Handler(ctx, reqPath)
	if err != nil {
		t.Fatalf("sv_graph_path failed: %v", err)
	}
	textResult := textContent(resPath.Content[0])
	if !strings.Contains(textResult, "imports EXTRACTED") || !strings.Contains(textResult, "index.js") || !strings.Contains(textResult, "Button.tsx") {
		t.Errorf("expected path with confidence details, got: %s", textResult)
	}
}
