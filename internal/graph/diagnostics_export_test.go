package graph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/svtech-code/sv-memory/internal/db"
	"github.com/svtech-code/sv-memory/internal/graph/extractor"
)

func TestMDSemanticExtractor(t *testing.T) {
	ext := extractor.NewTreeSitterExtractor()
	content := []byte(`# Architecture Overview

This document describes the clean architecture.

## Database Layer

- NOTE: Use SQLite WAL mode for concurrency.

| Table | Purpose |
| --- | --- |
| memories | Store architectural decisions |

` + "```go\nfunc InitDB() {}\n```" + `
`)

	symbols, imports, err := ext.Extract(content, "docs/arch.md", ".md")
	if err != nil {
		t.Fatalf("failed to extract md semantic: %v", err)
	}

	if len(symbols) == 0 {
		t.Fatalf("expected symbols from md, got 0")
	}

	foundSection := false
	foundCodeBlock := false

	for _, s := range symbols {
		if s.Type == "section" && s.Name == "Architecture Overview" {
			foundSection = true
		}
		if s.Type == "code_block" && s.Name == "go" {
			foundCodeBlock = true
		}
	}

	if !foundSection {
		t.Errorf("expected to find section symbol 'Architecture Overview'")
	}
	if !foundCodeBlock {
		t.Errorf("expected to find code_block symbol")
	}
	_ = imports
}

func TestDiagnoseAndExport(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_graph_diag.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-diag"
	if regErr := db.RegisterProject(database, projectID, "Diag Project", tempDir); regErr != nil {
		t.Fatalf("failed to register project: %v", regErr)
	}

	// Insert dummy nodes & edges
	_, err = database.Exec(`
		INSERT INTO graph_nodes (project_id, id, node_type, label, path) VALUES
		(?, 'node-1', 'file', 'node1.go', 'node1.go'),
		(?, 'node-2', 'file', 'node2.go', 'node2.go');
		INSERT INTO graph_edges (id, project_id, source_id, target_id, relation_type) VALUES
		('e1', ?, 'node-1', 'node-2', 'CALLS');
	`, projectID, projectID, projectID)
	if err != nil {
		t.Fatalf("failed to insert dummy nodes: %v", err)
	}

	// 1. Test DiagnoseGraph
	report, err := DiagnoseGraph(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("failed DiagnoseGraph: %v", err)
	}
	if report.TotalNodes != 2 || report.TotalEdges != 1 {
		t.Errorf("unexpected node/edge counts: nodes=%d, edges=%d", report.TotalNodes, report.TotalEdges)
	}

	// 2. Test ExportObsidianVault
	obsidianDir := filepath.Join(tempDir, "obsidian_vault")
	if exportErr := ExportObsidianVault(database, projectID, obsidianDir); exportErr != nil {
		t.Fatalf("failed ExportObsidianVault: %v", exportErr)
	}
	if _, statErr := os.Stat(filepath.Join(obsidianDir, "nodes")); os.IsNotExist(statErr) {
		t.Errorf("expected obsidian nodes folder to exist")
	}

	// 3. Test ExportCypher
	cypherPath := filepath.Join(tempDir, "graph.cypher")
	if cypherErr := ExportCypher(database, projectID, cypherPath); cypherErr != nil {
		t.Fatalf("failed ExportCypher: %v", cypherErr)
	}
	cypherContent, err := os.ReadFile(cypherPath)
	if err != nil {
		t.Fatalf("failed reading cypher output: %v", err)
	}
	if len(cypherContent) == 0 {
		t.Errorf("expected non-empty cypher script")
	}
}

func TestDetectBridgeNodes(t *testing.T) {
	g := &InMemoryGraph{
		Nodes: map[string]*Node{
			"a": {ID: "a", Label: "A"},
			"b": {ID: "b", Label: "B"},
			"c": {ID: "c", Label: "C"},
			"d": {ID: "d", Label: "D"},
		},
		EdgesBySource: map[string][]*Edge{
			"b": {{SourceID: "b", TargetID: "a"}, {SourceID: "b", TargetID: "c"}},
			"c": {{SourceID: "c", TargetID: "d"}},
		},
		EdgesByTarget: map[string][]*Edge{
			"a": {{SourceID: "b", TargetID: "a"}},
			"c": {{SourceID: "b", TargetID: "c"}},
			"d": {{SourceID: "c", TargetID: "d"}},
		},
		FanIn:  map[string]int{"a": 1, "c": 1, "d": 1},
		FanOut: map[string]int{"b": 2, "c": 1},
	}

	bridges := g.DetectBridgeNodes()
	// Should not panic and return bridge nodes slice
	_ = bridges
}
