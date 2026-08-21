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

func TestStaleMemoryBindings(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_stale_mem.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-stale-mem"
	if err = db.RegisterProject(database, projectID, "Stale Mem Project", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Create an existing file
	existingFile := filepath.Join(tempDir, "pkg", "live.go")
	if err = os.MkdirAll(filepath.Dir(existingFile), 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	if err = os.WriteFile(existingFile, []byte("package pkg\n"), 0644); err != nil {
		t.Fatalf("failed to write live.go: %v", err)
	}

	// Insert memories: one pointing to existing file, one pointing to missing file
	_, err = database.Exec(`
		INSERT INTO memories (id, project_id, category, what, why, learned, where_path) VALUES
		('mem-live', ?, 'decision', 'use pkg', 'speed', 'fast', 'pkg/live.go'),
		('mem-stale', ?, 'decision', 'old logic', 'legacy', 'none', 'pkg/deleted.go');
	`, projectID, projectID)
	if err != nil {
		t.Fatalf("failed to insert test memories: %v", err)
	}

	// Test DetectStaleMemoryBindings
	staleIDs, err := DetectStaleMemoryBindings(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("failed DetectStaleMemoryBindings: %v", err)
	}
	if len(staleIDs) != 1 || staleIDs[0] != "mem-stale" {
		t.Fatalf("expected ['mem-stale'], got %v", staleIDs)
	}

	// Test DiagnoseGraph reports stale memory binding
	report, err := DiagnoseGraph(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("failed DiagnoseGraph: %v", err)
	}
	if report.StaleMemoryBindings != 1 {
		t.Errorf("expected 1 stale memory binding, got %d", report.StaleMemoryBindings)
	}
	if report.IsHealthy {
		t.Errorf("expected graph report to be marked unhealthy due to stale memory binding")
	}
}
