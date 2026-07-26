package graph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/svtech/sv-memory/internal/db"
)

func TestSyncGraph(t *testing.T) {
	// Create a temporary project workspace
	tempDir, err := os.MkdirTemp("", "sv-mem-graph-test")
	if err != nil {
		t.Fatalf("failed to create temp workspace: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_storage.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init DB: %v", err)
	}
	defer database.Close()

	projectID := "proj-graph-test"
	err = db.RegisterProject(database, projectID, "Graph Test Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Create mock code files in the workspace
	// 1. JS file index.js (imports relative utils.js and external path/fs)
	indexJS := `
	import utils from './utils';
	const path = require('path');
	import { test } from "./components/Button";
	`
	err = os.WriteFile(filepath.Join(tempDir, "index.js"), []byte(indexJS), 0644)
	if err != nil {
		t.Fatalf("failed writing index.js: %v", err)
	}

	// 2. JS file utils.js (no imports)
	err = os.WriteFile(filepath.Join(tempDir, "utils.js"), []byte("export default {}"), 0644)
	if err != nil {
		t.Fatalf("failed writing utils.js: %v", err)
	}

	// 3. JS file components/Button.tsx (relative import and external react)
	err = os.MkdirAll(filepath.Join(tempDir, "components"), 0755)
	if err != nil {
		t.Fatalf("failed to create folder components: %v", err)
	}

	buttonTSX := `
	import React from 'react';
	`
	err = os.WriteFile(filepath.Join(tempDir, "components", "Button.tsx"), []byte(buttonTSX), 0644)
	if err != nil {
		t.Fatalf("failed writing Button.tsx: %v", err)
	}

	// 4. Python file main.py (imports external math)
	mainPy := `
import math
from utils import helper
	`
	err = os.WriteFile(filepath.Join(tempDir, "main.py"), []byte(mainPy), 0644)
	if err != nil {
		t.Fatalf("failed writing main.py: %v", err)
	}

	// Walk tree and Sync graph
	err = SyncGraph(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("failed scanning and syncing graph: %v", err)
	}

	// Check graph nodes
	var nodeCount int
	err = database.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ?", projectID).Scan(&nodeCount)
	if err != nil {
		t.Fatalf("failed querying nodes count: %v", err)
	}

	// Nodes should include: index.js, utils.js, components/Button.tsx, main.py
	// External packages should include: pkg:path, pkg:react, pkg:math
	// (Note: from utils import helper might not resolve to utils.js because in Python the import name is 'utils',
	//  which doesn't start with '.' and is treated as an external package unless direct matched).
	// Let's verify we have at least 4 internal nodes
	if nodeCount < 4 {
		t.Errorf("expected at least 4 nodes in graph database, got %d", nodeCount)
	}

	// Query individual nodes
	var nodeType string
	err = database.QueryRow("SELECT node_type FROM graph_nodes WHERE project_id = ? AND id = 'index.js'", projectID).Scan(&nodeType)
	if err != nil {
		t.Fatalf("expected node index.js to exist in DB: %v", err)
	}
	if nodeType != "file" {
		t.Errorf("expected index.js node type to be 'file', got %s", nodeType)
	}

	// Check if pkg:react node exists
	var count int
	err = database.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ? AND id = 'pkg:react'", projectID).Scan(&count)
	if err != nil {
		t.Fatalf("failed checking package node react: %v", err)
	}
	if count != 1 {
		t.Error("expected external package node 'pkg:react' to be registered")
	}

	// Check graph edges
	// index.js -> utils.js (imports) should exist
	var edgeExists int
	err = database.QueryRow(`
		SELECT COUNT(*) FROM graph_edges 
		WHERE project_id = ? AND source_id = 'index.js' AND target_id = 'utils.js' AND relation_type = 'imports'
	`, projectID).Scan(&edgeExists)
	if err != nil {
		t.Fatalf("failed checking imports edge: %v", err)
	}

	if edgeExists != 1 {
		t.Error("expected dependency edge from index.js to utils.js to exist")
	}

	// index.js -> pkg:path (imports) should exist
	err = database.QueryRow(`
		SELECT COUNT(*) FROM graph_edges 
		WHERE project_id = ? AND source_id = 'index.js' AND target_id = 'pkg:path' AND relation_type = 'imports'
	`, projectID).Scan(&edgeExists)
	if err != nil {
		t.Fatalf("failed checking imports edge: %v", err)
	}

	if edgeExists != 1 {
		t.Error("expected dependency edge from index.js to pkg:path to exist")
	}
}

func TestSyncGraphIncremental(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-incr-test")
	if err != nil {
		t.Fatalf("failed to create temp workspace: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_incr.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init DB: %v", err)
	}
	defer database.Close()

	projectID := "prog-incr-test"
	err = db.RegisterProject(database, projectID, "Incremental Test", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// ---- FIRST BUILD ----
	// a.js imports b.js and react
	err = os.WriteFile(filepath.Join(tempDir, "a.js"), []byte(`import b from './b'; import React from 'react';`), 0644)
	if err != nil {
		t.Fatalf("failed writing a.js: %v", err)
	}
	err = os.WriteFile(filepath.Join(tempDir, "b.js"), []byte(`export const x = 1;`), 0644)
	if err != nil {
		t.Fatalf("failed writing b.js: %v", err)
	}

	err = SyncGraph(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("first SyncGraph failed: %v", err)
	}

	// Verify nodes from first build
	var nodeCount int
	err = database.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ?", projectID).Scan(&nodeCount)
	if err != nil {
		t.Fatalf("failed counting nodes: %v", err)
	}
	if nodeCount < 3 {
		t.Errorf("expected at least 3 nodes (a.js, b.js, pkg:react), got %d", nodeCount)
	}

	// Verify a.js->b.js edge
	var edgeCount int
	err = database.QueryRow(`
		SELECT COUNT(*) FROM graph_edges
		WHERE project_id = ? AND source_id = 'a.js' AND target_id = 'b.js' AND relation_type = 'imports'
	`, projectID).Scan(&edgeCount)
	if err != nil {
		t.Fatalf("failed checking a.js->b.js edge: %v", err)
	}
	if edgeCount != 1 {
		t.Errorf("expected a.js->b.js edge after first build, got %d", edgeCount)
	}

	// Verify a.js->pkg:react edge
	err = database.QueryRow(`
		SELECT COUNT(*) FROM graph_edges
		WHERE project_id = ? AND source_id = 'a.js' AND target_id = 'pkg:react' AND relation_type = 'imports'
	`, projectID).Scan(&edgeCount)
	if err != nil {
		t.Fatalf("failed checking a.js->pkg:react edge: %v", err)
	}
	if edgeCount != 1 {
		t.Errorf("expected a.js->pkg:react edge after first build, got %d", edgeCount)
	}

	// ---- INCREMENTAL BUILD ----
	// Modify a.js to also import 'vue' (a new external dep not seen before)
	// This tests: pkg nodes created by parseFiles are upserted to the DB
	err = os.WriteFile(filepath.Join(tempDir, "a.js"), []byte(`import b from './b'; import React from 'react'; import Vue from 'vue';`), 0644)
	if err != nil {
		t.Fatalf("failed updating a.js: %v", err)
	}

	err = SyncGraph(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("incremental SyncGraph failed: %v", err)
	}

	// Verify pkg:vue node was created (the actual bug fix)
	var vueNodeExists int
	err = database.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ? AND id = 'pkg:vue'", projectID).Scan(&vueNodeExists)
	if err != nil {
		t.Fatalf("failed checking pkg:vue node: %v", err)
	}
	if vueNodeExists != 1 {
		t.Errorf("expected pkg:vue node to exist after incremental rebuild, got %d", vueNodeExists)
	}

	// Verify a.js->pkg:vue edge was created
	err = database.QueryRow(`
		SELECT COUNT(*) FROM graph_edges
		WHERE project_id = ? AND source_id = 'a.js' AND target_id = 'pkg:vue' AND relation_type = 'imports'
	`, projectID).Scan(&edgeCount)
	if err != nil {
		t.Fatalf("failed checking a.js->pkg:vue edge: %v", err)
	}
	if edgeCount != 1 {
		t.Errorf("expected a.js->pkg:vue edge after incremental build, got %d", edgeCount)
	}

	// Verify old edges are preserved
	err = database.QueryRow(`
		SELECT COUNT(*) FROM graph_edges
		WHERE project_id = ? AND source_id = 'a.js' AND target_id = 'b.js' AND relation_type = 'imports'
	`, projectID).Scan(&edgeCount)
	if err != nil {
		t.Fatalf("failed checking a.js->b.js after incremental: %v", err)
	}
	if edgeCount != 1 {
		t.Errorf("expected a.js->b.js edge to survive incremental, got %d", edgeCount)
	}

	err = database.QueryRow(`
		SELECT COUNT(*) FROM graph_edges
		WHERE project_id = ? AND source_id = 'a.js' AND target_id = 'pkg:react' AND relation_type = 'imports'
	`, projectID).Scan(&edgeCount)
	if err != nil {
		t.Fatalf("failed checking a.js->pkg:react after incremental: %v", err)
	}
	if edgeCount != 1 {
		t.Errorf("expected a.js->pkg:react edge to survive incremental, got %d", edgeCount)
	}

	// Verify b.js is unchanged (still has no edges)
	var bEdges int
	err = database.QueryRow(`
		SELECT COUNT(*) FROM graph_edges
		WHERE project_id = ? AND source_id = 'b.js'
	`, projectID).Scan(&bEdges)
	if err != nil {
		t.Fatalf("failed checking b.js edges: %v", err)
	}
	if bEdges != 0 {
		t.Errorf("expected b.js to have 0 outgoing edges, got %d", bEdges)
	}
}
