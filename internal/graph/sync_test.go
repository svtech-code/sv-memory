package graph

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/svtech-code/sv-memory/internal/db"
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
	// 1. JS file index.js (imports relative utils.js and external lodash/express)
	indexJS := `
	import utils from './utils';
	const lodash = require('lodash');
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

	// index.js -> pkg:lodash (imports) should exist
	err = database.QueryRow(`
		SELECT COUNT(*) FROM graph_edges 
		WHERE project_id = ? AND source_id = 'index.js' AND target_id = 'pkg:lodash' AND relation_type = 'imports'
	`, projectID).Scan(&edgeExists)
	if err != nil {
		t.Fatalf("failed checking imports edge: %v", err)
	}

	if edgeExists != 1 {
		t.Error("expected dependency edge from index.js to pkg:lodash to exist")
	}

	// Verify confidence column
	var confidence string
	err = database.QueryRow(`
		SELECT confidence FROM graph_edges
		WHERE project_id = ? AND source_id = 'index.js' AND target_id = 'utils.js' AND relation_type = 'imports'
	`, projectID).Scan(&confidence)
	if err != nil {
		t.Fatalf("failed reading confidence: %v", err)
	}
	if confidence != "EXTRACTED" {
		t.Errorf("expected confidence 'EXTRACTED', got %q", confidence)
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

func TestSyncGraphChurnFallback(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-churn-test")
	if err != nil {
		t.Fatalf("failed to create temp workspace: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_churn.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init DB: %v", err)
	}
	defer database.Close()

	projectID := "proj-churn-test"
	err = db.RegisterProject(database, projectID, "Churn Test Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// 1. Create 10 files
	for i := 1; i <= 10; i++ {
		filename := fmt.Sprintf("file_%d.js", i)
		err = os.WriteFile(filepath.Join(tempDir, filename), []byte("export const a = 1;"), 0644)
		if err != nil {
			t.Fatalf("failed writing file: %v", err)
		}
	}

	// First build (full)
	err = SyncGraph(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("first SyncGraph failed: %v", err)
	}

	// Test case A: Low churn (1 file modified of 10 = 10% churn)
	// This should successfully run incremental sync
	err = os.WriteFile(filepath.Join(tempDir, "file_1.js"), []byte("export const a = 2;"), 0644)
	if err != nil {
		t.Fatalf("failed updating file_1: %v", err)
	}

	ok, err := trySyncGraphIncremental(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("trySyncGraphIncremental failed: %v", err)
	}
	if !ok {
		t.Error("expected trySyncGraphIncremental to return true for 10% churn (low churn)")
	}

	// Sync again to update metadata
	err = SyncGraph(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("SyncGraph failed: %v", err)
	}

	// Test case B: High churn (4 files modified/deleted of 10 = 40% churn)
	// This should exceed the 30% threshold and trigger full rebuild (return false)
	err = os.WriteFile(filepath.Join(tempDir, "file_2.js"), []byte("export const a = 3;"), 0644)
	if err != nil {
		t.Fatalf("failed updating file_2: %v", err)
	}
	err = os.WriteFile(filepath.Join(tempDir, "file_3.js"), []byte("export const a = 3;"), 0644)
	if err != nil {
		t.Fatalf("failed updating file_3: %v", err)
	}
	// Delete file_4.js and file_5.js
	err = os.Remove(filepath.Join(tempDir, "file_4.js"))
	if err != nil {
		t.Fatalf("failed deleting file_4: %v", err)
	}
	err = os.Remove(filepath.Join(tempDir, "file_5.js"))
	if err != nil {
		t.Fatalf("failed deleting file_5: %v", err)
	}

	ok, err = trySyncGraphIncremental(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("trySyncGraphIncremental failed: %v", err)
	}
	if ok {
		t.Error("expected trySyncGraphIncremental to return false (fallback to full sync) for 40% churn")
	}

	// Final verification: running full sync should succeed
	err = SyncGraph(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("final SyncGraph failed: %v", err)
	}
}

// TestDetectStaleFiles verifies the cheap mtime/size probe: unchanged files are
// not reported, a modified file and a new file are reported as Changed, and a
// removed file is reported as Deleted.
func TestDetectStaleFiles(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-stale-test")
	if err != nil {
		t.Fatalf("failed to create temp workspace: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_stale.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init DB: %v", err)
	}
	defer database.Close()

	projectID := "prog-stale-test"
	if regErr := db.RegisterProject(database, projectID, "Stale Test", tempDir); regErr != nil {
		t.Fatalf("failed to register project: %v", regErr)
	}

	// a.js unchanged, b.js will be modified.
	os.WriteFile(filepath.Join(tempDir, "a.js"), []byte("export const a = 1;"), 0644)
	os.WriteFile(filepath.Join(tempDir, "b.js"), []byte("export const b = 1;"), 0644)

	if syncErr := SyncGraph(database, projectID, tempDir); syncErr != nil {
		t.Fatalf("initial SyncGraph failed: %v", syncErr)
	}

	// No changes yet → report must be clean.
	stale, err := DetectStaleFiles(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("DetectStaleFiles: %v", err)
	}
	if stale.HasChanges || stale.NeedsFull {
		t.Fatalf("expected clean report right after sync, got changed=%v deleted=%v needsFull=%v", stale.Changed, stale.Deleted, stale.NeedsFull)
	}

	// Modify b.js and add c.js, keep a.js untouched.
	time.Sleep(5 * time.Millisecond)
	os.WriteFile(filepath.Join(tempDir, "b.js"), []byte("export const b = 2;"), 0644)
	os.WriteFile(filepath.Join(tempDir, "c.js"), []byte("export const c = 1;"), 0644)

	stale, err = DetectStaleFiles(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("DetectStaleFiles after changes: %v", err)
	}
	if !stale.HasChanges {
		t.Fatal("expected HasChanges after modifying b.js and adding c.js")
	}
	if !containsStr(stale.Changed, "b.js") {
		t.Errorf("expected b.js in Changed, got %v", stale.Changed)
	}
	if !containsStr(stale.Changed, "c.js") {
		t.Errorf("expected c.js in Changed, got %v", stale.Changed)
	}
	if containsStr(stale.Changed, "a.js") {
		t.Errorf("a.js is unchanged but reported as changed: %v", stale.Changed)
	}

	// Remove a.js → must be reported as Deleted.
	os.Remove(filepath.Join(tempDir, "a.js"))
	stale, err = DetectStaleFiles(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("DetectStaleFiles after deletion: %v", err)
	}
	if !containsStr(stale.Deleted, "a.js") {
		t.Errorf("expected a.js in Deleted, got %v", stale.Deleted)
	}
}

// TestSyncGraphIfStale verifies the lazy refresh path: no-op when clean,
// incremental refresh on change, and the graph reflects the new file.
func TestSyncGraphIfStale(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-ifstale-test")
	if err != nil {
		t.Fatalf("failed to create temp workspace: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_ifstale.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init DB: %v", err)
	}
	defer database.Close()

	projectID := "prog-ifstale-test"
	if regErr := db.RegisterProject(database, projectID, "IfStale Test", tempDir); regErr != nil {
		t.Fatalf("failed to register project: %v", regErr)
	}

	os.WriteFile(filepath.Join(tempDir, "a.js"), []byte(`import b from './b';`), 0644)
	os.WriteFile(filepath.Join(tempDir, "b.js"), []byte("export const b = 1;"), 0644)
	if syncErr := SyncGraph(database, projectID, tempDir); syncErr != nil {
		t.Fatalf("initial SyncGraph failed: %v", syncErr)
	}

	// Clean → SyncGraphIfStale must be a no-op.
	synced, err := SyncGraphIfStale(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("SyncGraphIfStale clean: %v", err)
	}
	if synced {
		t.Error("expected SyncGraphIfStale to be a no-op when nothing changed")
	}

	// Change b.js to import a new file d.js, and add d.js.
	time.Sleep(5 * time.Millisecond)
	os.WriteFile(filepath.Join(tempDir, "b.js"), []byte(`import d from './d'; export const b = 2;`), 0644)
	os.WriteFile(filepath.Join(tempDir, "d.js"), []byte("export const d = 1;"), 0644)

	synced, err = SyncGraphIfStale(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("SyncGraphIfStale changed: %v", err)
	}
	if !synced {
		t.Error("expected SyncGraphIfStale to refresh after a change")
	}

	// d.js must now exist in the graph.
	var count int
	if err := database.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ? AND id = 'd.js'", projectID).Scan(&count); err != nil {
		t.Fatalf("count d.js: %v", err)
	}
	if count != 1 {
		t.Errorf("expected d.js node after lazy refresh, got %d", count)
	}

	// b.js -> d.js edge must exist.
	if err := database.QueryRow("SELECT COUNT(*) FROM graph_edges WHERE project_id = ? AND source_id = 'b.js' AND target_id = 'd.js' AND relation_type = 'imports'", projectID).Scan(&count); err != nil {
		t.Fatalf("count b.js->d.js edge: %v", err)
	}
	if count != 1 {
		t.Errorf("expected b.js->d.js edge after lazy refresh, got %d", count)
	}
}

func containsStr(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// TestSyncGraphIfStalePreservesCallEdges verifies that a lazy incremental sync
// (only a subset of files changed) does not wipe 'calls' edges between
// unchanged files: it used to delete every calls edge and re-extract from a
// partial symbol map, silently dropping cross-file calls that touched
// untouched files.
func TestSyncGraphIfStalePreservesCallEdges(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-stale-calls-test")
	if err != nil {
		t.Fatalf("failed to create temp workspace: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_stale_calls.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init DB: %v", err)
	}
	defer database.Close()

	projectID := "prog-stale-calls-test"
	if regErr := db.RegisterProject(database, projectID, "Stale Calls Test", tempDir); regErr != nil {
		t.Fatalf("failed to register project: %v", regErr)
	}

	// a.py calls func_b which is defined in b.py.
	os.WriteFile(filepath.Join(tempDir, "a.py"), []byte("def func_a():\n    return func_b()\n"), 0644)
	os.WriteFile(filepath.Join(tempDir, "b.py"), []byte("def func_b():\n    return 1\n"), 0644)
	if syncErr := SyncGraph(database, projectID, tempDir); syncErr != nil {
		t.Fatalf("initial SyncGraph failed: %v", syncErr)
	}

	var count int
	if err = database.QueryRow("SELECT COUNT(*) FROM graph_edges WHERE project_id = ? AND source_id = 'a.py:func_a' AND target_id = 'b.py:func_b' AND relation_type = 'calls'", projectID).Scan(&count); err != nil {
		t.Fatalf("count initial calls edge: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected a.py:func_a -> b.py:func_b calls edge after full sync, got %d", count)
	}

	// Add an unrelated file and run the lazy refresh path.
	time.Sleep(5 * time.Millisecond)
	os.WriteFile(filepath.Join(tempDir, "c.py"), []byte("def func_c():\n    return 2\n"), 0644)
	synced, err := SyncGraphIfStale(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("SyncGraphIfStale: %v", err)
	}
	if !synced {
		t.Error("expected SyncGraphIfStale to refresh after adding c.py")
	}

	// The cross-file call edge between the two unchanged files must survive.
	if err = database.QueryRow("SELECT COUNT(*) FROM graph_edges WHERE project_id = ? AND source_id = 'a.py:func_a' AND target_id = 'b.py:func_b' AND relation_type = 'calls'", projectID).Scan(&count); err != nil {
		t.Fatalf("count calls edge after lazy refresh: %v", err)
	}
	if count != 1 {
		t.Errorf("expected a.py:func_a -> b.py:func_b calls edge to survive lazy sync, got %d", count)
	}
}
