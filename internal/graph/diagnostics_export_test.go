package graph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/svtech-code/sv-memory/internal/db"
	"github.com/svtech-code/sv-memory/internal/graph/extractor"
)

func TestMDSemanticExtractor(t *testing.T) {
	ext := extractor.NewMDSemanticExtractor()
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
	foundTable := false
	foundCodeBlock := false
	foundRationale := false

	for _, s := range symbols {
		if s.Type == "section" && s.Name == "Architecture Overview" {
			foundSection = true
		}
		if s.Type == "table" {
			foundTable = true
		}
		if s.Type == "code_block" && s.Name == "go" {
			foundCodeBlock = true
		}
		if s.Type == "rationale" {
			foundRationale = true
		}
	}

	if !foundSection {
		t.Errorf("expected to find section symbol 'Architecture Overview'")
	}
	if !foundTable {
		t.Errorf("expected to find table symbol")
	}
	if !foundCodeBlock {
		t.Errorf("expected to find code_block symbol")
	}
	if !foundRationale {
		t.Errorf("expected to find rationale symbol")
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
	if err := db.RegisterProject(database, projectID, "Diag Project", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
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
	if err := ExportObsidianVault(database, projectID, obsidianDir); err != nil {
		t.Fatalf("failed ExportObsidianVault: %v", err)
	}
	if _, err := os.Stat(filepath.Join(obsidianDir, "nodes")); os.IsNotExist(err) {
		t.Errorf("expected obsidian nodes folder to exist")
	}

	// 3. Test ExportCypher
	cypherPath := filepath.Join(tempDir, "graph.cypher")
	if err := ExportCypher(database, projectID, cypherPath); err != nil {
		t.Fatalf("failed ExportCypher: %v", err)
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
