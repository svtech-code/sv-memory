package graph

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func TestParseSymbols(t *testing.T) {
	tests := []struct {
		name        string
		ext         string
		content     string
		wantFuncs   []string
		wantClasses []string
		wantSects   []string // sections in markdown
		wantTables  []string // tables in SQL
		wantMeta    map[string]interface{}
	}{
		{
			name: "js functions and classes",
			ext:  ".js",
			content: `
import React from 'react';
export function App() { return <App/>; }
function helper() {}
export class UserService { }
class Internal {}
export default function Root() {}
`,
			wantFuncs:   []string{"App", "helper", "Root"},
			wantClasses: []string{"UserService", "Internal"},
		},
		{
			name: "python functions and classes",
			ext:  ".py",
			content: `
import math
def hello():
    pass
async def async_fn():
    pass
class MyClass:
    pass
`,
			wantFuncs:   []string{"hello", "async_fn"},
			wantClasses: []string{"MyClass"},
		},
		{
			name: "go functions and structs",
			ext:  ".go",
			content: `
package main
func main() {}
func Helper() {}
type User struct {
    Name string
}
type internal struct {}
`,
			wantFuncs:   []string{"Helper"},
			wantClasses: []string{"User", "internal"},
		},
		{
			name: "markdown headings and code blocks",
			ext:  ".md",
			content: `# Main Title
## Section One
Some text here.

## Section Two with Code
More text.

` + "```" + `python
def hello():
    pass
` + "```" + `

### Deep Section
Text.
`,
			wantFuncs:   nil,
			wantClasses: nil,
			wantSects:   []string{"Main Title", "Section One", "Section Two with Code", "Deep Section"},
			wantTables:  nil,
		},
		{
			name: "sql tables and views",
			ext:  ".sql",
			content: `
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT
);

CREATE VIEW active_users AS SELECT * FROM users WHERE active = 1;

CREATE INDEX idx_users_email ON users(email);

CREATE TYPE mood AS ENUM ('happy', 'sad', 'neutral');
`,
			wantFuncs:   nil,
			wantClasses: nil,
			wantSects:   nil,
			wantTables:  []string{"users", "active_users", "idx_users_email", "mood"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := []byte(tt.content)
			symbols, _ := parseSymbols("test"+tt.ext, tt.ext, content)

			var gotFuncs, gotClasses, gotSects, gotTables []string
			for _, s := range symbols {
				switch s.Type {
				case "function":
					gotFuncs = append(gotFuncs, s.Label)
				case "class":
					gotClasses = append(gotClasses, s.Label)
				case "section":
					gotSects = append(gotSects, s.Label)
				case "table", "view", "index", "type":
					gotTables = append(gotTables, s.Label)
				}
			}

			if !stringSliceEqual(gotFuncs, tt.wantFuncs) {
				t.Errorf("functions: got %v, want %v", gotFuncs, tt.wantFuncs)
			}
			if !stringSliceEqual(gotClasses, tt.wantClasses) {
				t.Errorf("classes: got %v, want %v", gotClasses, tt.wantClasses)
			}
			if !stringSliceEqual(gotSects, tt.wantSects) {
				t.Errorf("sections: got %v, want %v", gotSects, tt.wantSects)
			}
			if !stringSliceEqual(gotTables, tt.wantTables) {
				t.Errorf("tables: got %v, want %v", gotTables, tt.wantTables)
			}
		})
	}
}

func TestParseManifests(t *testing.T) {
	t.Run("package.json", func(t *testing.T) {
		content := []byte(`{
			"dependencies": { "react": "^18.0.0", "lodash": "^4.0.0" },
			"devDependencies": { "typescript": "^5.0.0" }
		}`)
		deps := parsePackageJSON(content)
		if len(deps) != 3 {
			t.Errorf("expected 3 deps, got %v", deps)
		}
	})

	t.Run("go.mod", func(t *testing.T) {
		content := []byte("module myapp\ngo 1.21\nrequire (\n\tgithub.com/foo/bar v1.0.0\n\tgithub.com/baz/qux v0.5.0\n)\n")
		deps := parseGoMod(content)
		if len(deps) != 2 {
			t.Errorf("expected 2 deps, got %v", deps)
		}
	})

	t.Run("requirements.txt", func(t *testing.T) {
		content := []byte("flask==2.0.0\nrequests>=2.25.0\n# comment\npytest>=7.0\n")
		deps := parseRequirementsTXT(content)
		if len(deps) != 3 {
			t.Errorf("expected 3 deps, got %v", deps)
		}
	})
}

func TestSyncGraphWithManifests(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-manifest-test")
	if err != nil {
		t.Fatalf("failed to create temp workspace: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_manifest.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init DB: %v", err)
	}
	defer database.Close()

	projectID := "proj-manifest-test"
	err = db.RegisterProject(database, projectID, "Manifest Test", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Create a package.json with dependencies
	err = os.WriteFile(filepath.Join(tempDir, "package.json"), []byte(`{
		"dependencies": { "react": "^18.0.0", "lodash": "^4.0.0" }
	}`), 0644)
	if err != nil {
		t.Fatalf("failed writing package.json: %v", err)
	}

	// Create a code file that imports one of them (ensures dedup)
	err = os.WriteFile(filepath.Join(tempDir, "app.js"), []byte(`import React from 'react';`), 0644)
	if err != nil {
		t.Fatalf("failed writing app.js: %v", err)
	}

	err = SyncGraph(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("SyncGraph failed: %v", err)
	}

	// Verify depends_on edges from manifest
	var depCount int
	err = database.QueryRow(`
		SELECT COUNT(*) FROM graph_edges
		WHERE project_id = ? AND source_id = 'package.json' AND relation_type = 'depends_on'
	`, projectID).Scan(&depCount)
	if err != nil {
		t.Fatalf("failed counting depends_on: %v", err)
	}
	if depCount != 2 {
		t.Errorf("expected 2 depends_on edges from package.json, got %d", depCount)
	}

	// Verify confidence is INFERRED for manifest edges
	var confidence string
	err = database.QueryRow(`
		SELECT confidence FROM graph_edges
		WHERE project_id = ? AND source_id = 'package.json' AND target_id = 'pkg:react' AND relation_type = 'depends_on'
	`, projectID).Scan(&confidence)
	if err != nil {
		t.Fatalf("failed reading confidence: %v", err)
	}
	if confidence != "INFERRED" {
		t.Errorf("expected INFERRED confidence, got %q", confidence)
	}

	// Verify pkg:react node exists (dedup - should be created by both parseFiles and parseManifests)
	var nodeCount int
	err = database.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ? AND id = 'pkg:react'", projectID).Scan(&nodeCount)
	if err != nil {
		t.Fatalf("failed counting pkg:react: %v", err)
	}
	if nodeCount != 1 {
		t.Errorf("expected exactly 1 pkg:react node, got %d", nodeCount)
	}

	// ---- INCREMENTAL: add a new dep to package.json ----
	err = os.WriteFile(filepath.Join(tempDir, "package.json"), []byte(`{
		"dependencies": { "react": "^18.0.0", "lodash": "^4.0.0", "vue": "^3.0.0" }
	}`), 0644)
	if err != nil {
		t.Fatalf("failed updating package.json: %v", err)
	}

	err = SyncGraph(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("incremental SyncGraph failed: %v", err)
	}

	// Verify new dep edge exists
	var vueDep int
	err = database.QueryRow(`
		SELECT COUNT(*) FROM graph_edges
		WHERE project_id = ? AND source_id = 'package.json' AND target_id = 'pkg:vue' AND relation_type = 'depends_on'
	`, projectID).Scan(&vueDep)
	if err != nil {
		t.Fatalf("failed checking vue dep: %v", err)
	}
	if vueDep != 1 {
		t.Errorf("expected depends_on edge to pkg:vue after incremental, got %d", vueDep)
	}

	// Verify total is now 3
	err = database.QueryRow(`
		SELECT COUNT(*) FROM graph_edges
		WHERE project_id = ? AND source_id = 'package.json' AND relation_type = 'depends_on'
	`, projectID).Scan(&depCount)
	if err != nil {
		t.Fatalf("failed counting depends_on after incremental: %v", err)
	}
	if depCount != 3 {
		t.Errorf("expected 3 depends_on edges after incremental, got %d", depCount)
	}

	// ---- REMOVE package.json ----
	err = os.Remove(filepath.Join(tempDir, "package.json"))
	if err != nil {
		t.Fatalf("failed removing package.json: %v", err)
	}

	err = SyncGraph(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("SyncGraph after manifest removal failed: %v", err)
	}

	err = database.QueryRow(`
		SELECT COUNT(*) FROM graph_edges
		WHERE project_id = ? AND source_id = 'package.json' AND relation_type = 'depends_on'
	`, projectID).Scan(&depCount)
	if err != nil {
		t.Fatalf("failed counting depends_on after removal: %v", err)
	}
	if depCount != 0 {
		t.Errorf("expected 0 depends_on edges after manifest removal, got %d", depCount)
	}
}

func TestSyncGraphWithSymbols(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-sym-test")
	if err != nil {
		t.Fatalf("failed to create temp workspace: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_sym.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init DB: %v", err)
	}
	defer database.Close()

	projectID := "proj-sym-test"
	err = db.RegisterProject(database, projectID, "Symbol Test", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Create a JS file with functions and classes
	err = os.WriteFile(filepath.Join(tempDir, "app.js"), []byte(`
import React from 'react';
export function App() { return <App/>; }
export class UserService { }
function helper() {}
`), 0644)
	if err != nil {
		t.Fatalf("failed writing app.js: %v", err)
	}

	err = SyncGraph(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("SyncGraph failed: %v", err)
	}

	// Verify file node exists with enriched metadata
	var metaStr string
	err = database.QueryRow("SELECT metadata FROM graph_nodes WHERE project_id = ? AND id = 'app.js'", projectID).Scan(&metaStr)
	if err != nil {
		t.Fatalf("failed querying app.js metadata: %v", err)
	}
	if !strings.Contains(metaStr, `"language":"javascript"`) {
		t.Errorf("expected metadata to contain language javascript, got %s", metaStr)
	}
	if !strings.Contains(metaStr, `"loc":`) {
		t.Errorf("expected metadata to contain loc, got %s", metaStr)
	}

	// Verify function child nodes exist
	var funcCount int
	err = database.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ? AND node_type = 'function'", projectID).Scan(&funcCount)
	if err != nil {
		t.Fatalf("failed counting function nodes: %v", err)
	}
	if funcCount < 2 {
		t.Errorf("expected at least 2 function nodes, got %d", funcCount)
	}

	// Verify specific child node exists
	var nodeType string
	err = database.QueryRow("SELECT node_type FROM graph_nodes WHERE project_id = ? AND id = 'app.js:App'", projectID).Scan(&nodeType)
	if err != nil {
		t.Fatalf("expected 'app.js:App' node to exist: %v", err)
	}
	if nodeType != "function" {
		t.Errorf("expected app.js:App type 'function', got %s", nodeType)
	}

	// ---- INCREMENTAL: add a new function and verify it appears ----
	err = os.WriteFile(filepath.Join(tempDir, "app.js"), []byte(`
import React from 'react';
export function App() { return <App/>; }
export class UserService { }
function helper() {}
function newFunc() {}
`), 0644)
	if err != nil {
		t.Fatalf("failed updating app.js: %v", err)
	}

	err = SyncGraph(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("incremental SyncGraph failed: %v", err)
	}

	var newFuncExists int
	err = database.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ? AND id = 'app.js:newFunc'", projectID).Scan(&newFuncExists)
	if err != nil {
		t.Fatalf("failed checking app.js:newFunc: %v", err)
	}
	if newFuncExists != 1 {
		t.Errorf("expected 'app.js:newFunc' child node after incremental rebuild, got %d", newFuncExists)
	}

	// Verify old symbols still exist
	err = database.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ? AND id = 'app.js:App'", projectID).Scan(&newFuncExists)
	if err != nil {
		t.Fatalf("failed checking app.js:App: %v", err)
	}
	if newFuncExists != 1 {
		t.Errorf("expected 'app.js:App' to survive incremental rebuild, got %d", newFuncExists)
	}
}

func TestSyncGraphWithMemories(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-graph-memories-test")
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

	projectID := "proj-memories-test"
	err = db.RegisterProject(database, projectID, "Memories Test Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Create a mock code file in the workspace
	err = os.WriteFile(filepath.Join(tempDir, "main.go"), []byte("package main\n\nfunc main() {}"), 0644)
	if err != nil {
		t.Fatalf("failed writing main.go: %v", err)
	}

	// Insert a memory in the DB
	_, err = database.Exec(`
		INSERT INTO memories (id, project_id, category, what, why, learned, where_path, revision_count, created_at)
		VALUES ('mem-1234', ?, 'architecture', 'Use WAL in SQLite', 'Concurrency support', 'WAL works great', 'main.go', 1, CURRENT_TIMESTAMP)
	`, projectID)
	if err != nil {
		t.Fatalf("failed to insert mock memory: %v", err)
	}

	// Run full SyncGraph
	err = SyncGraph(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("SyncGraph failed: %v", err)
	}

	// Verify memory node does NOT exist in graph_nodes (memory is not part of the code graph)
	var memCount int
	err = database.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ? AND id = 'mem-1234'", projectID).Scan(&memCount)
	if err != nil {
		t.Fatalf("failed to query memory node count: %v", err)
	}
	if memCount != 0 {
		t.Errorf("expected 0 memory nodes in graph, got %d", memCount)
	}
}

func TestSyncGraphWithCalls(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-graph-calls-test")
	if err != nil {
		t.Fatalf("failed to create temp workspace: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_calls.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init DB: %v", err)
	}
	defer database.Close()

	projectID := "proj-calls-test"
	err = db.RegisterProject(database, projectID, "Calls Test Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Create a mock Go file where one function calls another
	goCode := `package main

func callerFunc() {
	helperFunc()
}

func helperFunc() {
	println("hello")
}
`
	err = os.WriteFile(filepath.Join(tempDir, "main.go"), []byte(goCode), 0644)
	if err != nil {
		t.Fatalf("failed writing main.go: %v", err)
	}

	// Run SyncGraph
	err = SyncGraph(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("SyncGraph failed: %v", err)
	}

	// Verify that a calls edge exists from 'main.go:callerFunc' to 'main.go:helperFunc'
	var sourceID, targetID, relType, sourceLoc string
	err = database.QueryRow(`
		SELECT source_id, target_id, relation_type, source_location 
		FROM graph_edges 
		WHERE project_id = ? AND relation_type = 'calls'
	`, projectID).Scan(&sourceID, &targetID, &relType, &sourceLoc)
	if err != nil {
		t.Fatalf("failed to query calls edge: %v", err)
	}

	if sourceID != "main.go:callerFunc" {
		t.Errorf("expected source_id to be 'main.go:callerFunc', got: %q", sourceID)
	}
	if targetID != "main.go:helperFunc" {
		t.Errorf("expected target_id to be 'main.go:helperFunc', got: %q", targetID)
	}
	if relType != "calls" {
		t.Errorf("expected relation_type to be 'calls', got: %q", relType)
	}
	// The call is on line 4 of main.go (relative to 1-based index)
	if sourceLoc != "L4" {
		t.Errorf("expected source_location to be 'L4', got: %q", sourceLoc)
	}
}

func TestSyncGraphWithMarkdown(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-graph-md-test")
	if err != nil {
		t.Fatalf("failed to create temp workspace: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_md.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init DB: %v", err)
	}
	defer database.Close()

	projectID := "proj-md-test"
	err = db.RegisterProject(database, projectID, "Markdown Test Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Create mock markdown files
	readmeMD := `
# Project README

We have our [[architecture]] specifications.
See also the [specifications link](specs/specification.md).
`
	err = os.WriteFile(filepath.Join(tempDir, "README.md"), []byte(readmeMD), 0644)
	if err != nil {
		t.Fatalf("failed writing README.md: %v", err)
	}

	err = os.WriteFile(filepath.Join(tempDir, "architecture.md"), []byte("# Architecture"), 0644)
	if err != nil {
		t.Fatalf("failed writing architecture.md: %v", err)
	}

	err = os.MkdirAll(filepath.Join(tempDir, "specs"), 0755)
	if err != nil {
		t.Fatalf("failed creating folder: %v", err)
	}

	err = os.WriteFile(filepath.Join(tempDir, "specs", "specification.md"), []byte("# Spec"), 0644)
	if err != nil {
		t.Fatalf("failed writing specification.md: %v", err)
	}

	// Run SyncGraph
	err = SyncGraph(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("SyncGraph failed: %v", err)
	}

	// Verify README.md node exists and has type "document"
	var nodeType string
	err = database.QueryRow("SELECT node_type FROM graph_nodes WHERE project_id = ? AND id = 'README.md'", projectID).Scan(&nodeType)
	if err != nil {
		t.Fatalf("failed to query README.md node: %v", err)
	}
	if nodeType != "document" {
		t.Errorf("expected node_type to be 'document', got: %q", nodeType)
	}

	// Verify edges
	var count int
	err = database.QueryRow(`
		SELECT COUNT(*) FROM graph_edges 
		WHERE project_id = ? AND source_id = 'README.md' AND target_id = 'architecture.md' AND relation_type = 'references'
	`, projectID).Scan(&count)
	if err != nil || count != 1 {
		t.Errorf("expected 1 edge from README.md to architecture.md, got %d (err: %v)", count, err)
	}

	err = database.QueryRow(`
		SELECT COUNT(*) FROM graph_edges 
		WHERE project_id = ? AND source_id = 'README.md' AND target_id = 'specs/specification.md' AND relation_type = 'references'
	`, projectID).Scan(&count)
	if err != nil || count != 1 {
		t.Errorf("expected 1 edge from README.md to specs/specification.md, got %d (err: %v)", count, err)
	}
}

func TestSyncGraphWithRationales(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-graph-rationales-test")
	if err != nil {
		t.Fatalf("failed to create temp workspace: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_rationales.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init DB: %v", err)
	}
	defer database.Close()

	projectID := "proj-rationales-test"
	err = db.RegisterProject(database, projectID, "Rationales Test Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	goCode := `package main

func processData() {
	// WHY: this algorithm requires linear time complexity
	println("processing")
}
`
	err = os.WriteFile(filepath.Join(tempDir, "main.go"), []byte(goCode), 0644)
	if err != nil {
		t.Fatalf("failed writing main.go: %v", err)
	}

	// Run SyncGraph
	err = SyncGraph(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("SyncGraph failed: %v", err)
	}

	// Verify rationale is stored in file node metadata, not as a separate node
	var metaStr string
	err = database.QueryRow(`
		SELECT metadata FROM graph_nodes 
		WHERE project_id = ? AND id = 'main.go'
	`, projectID).Scan(&metaStr)
	if err != nil {
		t.Fatalf("failed querying file node metadata: %v", err)
	}
	if !strings.Contains(metaStr, "WHY: this algorithm requires linear time complexity") {
		t.Errorf("expected rationale in file metadata, got: %s", metaStr)
	}

	// Verify no separate rationale node was created
	var rationalCount int
	err = database.QueryRow(`
		SELECT COUNT(*) FROM graph_nodes 
		WHERE project_id = ? AND node_type = 'rationale'
	`, projectID).Scan(&rationalCount)
	if err != nil {
		t.Fatalf("failed checking rationale node count: %v", err)
	}
	if rationalCount != 0 {
		t.Errorf("expected 0 rationale nodes, got %d", rationalCount)
	}
}

// TestSyncGraphRedactsSecrets verifies that secrets embedded in scanned file
// content (markdown headings, rationale comments) are redacted before being
// persisted to graph_nodes, so they never surface through graph exports.
func TestSyncGraphRedactsSecrets(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-graph-secrets-test")
	if err != nil {
		t.Fatalf("failed to create temp workspace: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_secrets.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init DB: %v", err)
	}
	defer database.Close()

	projectID := "proj-secrets-test"
	err = db.RegisterProject(database, projectID, "Secrets Test Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	secret := "sk-ant-api03-AbCdEfGhIjKlMnOpQrStUvWxYz0123456789"
	mdContent := "# MyDoc\n\n## Configure the client with " + secret + " here\n"
	if err = os.WriteFile(filepath.Join(tempDir, "notes.md"), []byte(mdContent), 0644); err != nil {
		t.Fatalf("failed writing notes.md: %v", err)
	}

	goCode := "package main\n\n// WHY: keep the token " + secret + " private\nfunc main() {}\n"
	if err = os.WriteFile(filepath.Join(tempDir, "main.go"), []byte(goCode), 0644); err != nil {
		t.Fatalf("failed writing main.go: %v", err)
	}

	if err = SyncGraph(database, projectID, tempDir); err != nil {
		t.Fatalf("SyncGraph failed: %v", err)
	}

	// The markdown heading node id and label must not contain the raw secret.
	rows, err := database.Query("SELECT id, label, metadata FROM graph_nodes WHERE project_id = ?", projectID)
	if err != nil {
		t.Fatalf("failed querying graph_nodes: %v", err)
	}
	defer rows.Close()

	checked := 0
	for rows.Next() {
		var id, label, meta string
		if err = rows.Scan(&id, &label, &meta); err != nil {
			t.Fatalf("scan err: %v", err)
		}
		checked++
		for _, field := range []string{id, label, meta} {
			if strings.Contains(field, secret) {
				t.Errorf("raw secret persisted in graph node (id=%q label=%q metadata=%q)", id, label, meta)
			}
		}
	}
	if checked == 0 {
		t.Fatal("expected at least one graph node to check")
	}

	// The file-node metadata must still carry the (redacted) rationale.
	var metaStr string
	if err = database.QueryRow("SELECT metadata FROM graph_nodes WHERE project_id = ? AND id = 'main.go'", projectID).Scan(&metaStr); err != nil {
		t.Fatalf("failed querying main.go metadata: %v", err)
	}
	if !strings.Contains(metaStr, "WHY: keep the token") {
		t.Errorf("expected rationale present (redacted) in file metadata, got: %s", metaStr)
	}
	if strings.Contains(metaStr, secret) {
		t.Errorf("raw secret persisted in file rationale metadata: %s", metaStr)
	}
}

func TestSyncGraphWithMarkdownDeep(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-graph-md-deep-test")
	if err != nil {
		t.Fatalf("failed to create temp workspace: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_md_deep.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init DB: %v", err)
	}
	defer database.Close()

	projectID := "proj-md-deep-test"
	err = db.RegisterProject(database, projectID, "MD Deep Test", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	mdContent := `# Project Docs
## Getting Started
Run \` + "`" + `npm install` + "`" + `.

` + "```" + `go
package main
func main() { println("hello") }
` + "```" + `

## API Reference
Check the [[specs/api.md]] for details.

### Authentication

` + "```" + `mermaid
graph TD; A-->B;
` + "```" + `
`
	err = os.WriteFile(filepath.Join(tempDir, "docs.md"), []byte(mdContent), 0644)
	if err != nil {
		t.Fatalf("failed writing docs.md: %v", err)
	}
	err = os.MkdirAll(filepath.Join(tempDir, "specs"), 0755)
	if err != nil {
		t.Fatalf("failed creating specs dir: %v", err)
	}
	err = os.WriteFile(filepath.Join(tempDir, "specs", "api.md"), []byte("# API"), 0644)
	if err != nil {
		t.Fatalf("failed writing specs/api.md: %v", err)
	}

	err = SyncGraph(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("SyncGraph failed: %v", err)
	}

	// Verify section nodes created
	var sectionCount int
	err = database.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ? AND node_type = 'section'", projectID).Scan(&sectionCount)
	if err != nil {
		t.Fatalf("failed to query section count: %v", err)
	}
	if sectionCount < 3 {
		t.Errorf("expected at least 3 section nodes, got %d", sectionCount)
	}

	// Verify code_block node created
	var codeBlockCount int
	err = database.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ? AND node_type = 'code_block'", projectID).Scan(&codeBlockCount)
	if err != nil {
		t.Fatalf("failed to query code_block count: %v", err)
	}
	if codeBlockCount != 1 {
		t.Errorf("expected 1 code_block node, got %d", codeBlockCount)
	}

	// Verify diagram node created
	var diagramCount int
	err = database.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ? AND node_type = 'diagram'", projectID).Scan(&diagramCount)
	if err != nil {
		t.Fatalf("failed to query diagram count: %v", err)
	}
	if diagramCount != 1 {
		t.Errorf("expected 1 diagram node, got %d", diagramCount)
	}

	// Verify "contains" edges from docs.md to section/code_block/diagram children
	var containsCount int
	err = database.QueryRow("SELECT COUNT(*) FROM graph_edges WHERE project_id = ? AND source_id = 'docs.md' AND relation_type = 'contains'", projectID).Scan(&containsCount)
	if err != nil {
		t.Fatalf("failed to query contains edges: %v", err)
	}
	if containsCount == 0 {
		t.Errorf("expected at least 1 contains edge from docs.md, got 0")
	}

	// Verify wikilink reference edge still works
	var refCount int
	err = database.QueryRow("SELECT COUNT(*) FROM graph_edges WHERE project_id = ? AND source_id = 'docs.md' AND target_id = 'specs/api.md' AND relation_type = 'references'", projectID).Scan(&refCount)
	if err != nil {
		t.Fatalf("failed to query references edge: %v", err)
	}
	if refCount != 1 {
		t.Errorf("expected 1 references edge to specs/api.md, got %d", refCount)
	}

	// Verify language metadata on code_block node
	var metaStr string
	err = database.QueryRow("SELECT metadata FROM graph_nodes WHERE project_id = ? AND node_type = 'code_block' LIMIT 1", projectID).Scan(&metaStr)
	if err != nil {
		t.Fatalf("failed to query code_block metadata: %v", err)
	}
	if !strings.Contains(metaStr, "go") {
		t.Errorf("expected code_block metadata to contain language 'go', got: %s", metaStr)
	}
}

func TestSyncGraphWithSQLSchema(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-sql-test")
	if err != nil {
		t.Fatalf("failed to create temp workspace: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_sql.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init DB: %v", err)
	}
	defer database.Close()

	projectID := "proj-sql-test"
	err = db.RegisterProject(database, projectID, "SQL Schema Test", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	sqlContent := `
-- Users table
CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    email TEXT NOT NULL UNIQUE
);

-- Posts table with FK to users
CREATE TABLE posts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL,
    body TEXT,
    user_id INTEGER NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE INDEX idx_posts_user_id ON posts(user_id);

CREATE VIEW post_summary AS
    SELECT p.id, p.title, u.name AS author
    FROM posts p JOIN users u ON p.user_id = u.id;

CREATE TYPE priority AS ENUM ('low', 'medium', 'high');
`
	err = os.WriteFile(filepath.Join(tempDir, "schema.sql"), []byte(sqlContent), 0644)
	if err != nil {
		t.Fatalf("failed writing schema.sql: %v", err)
	}

	err = SyncGraph(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("SyncGraph failed: %v", err)
	}

	// Verify table nodes
	var tableCount int
	err = database.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ? AND node_type = 'table'", projectID).Scan(&tableCount)
	if err != nil {
		t.Fatalf("failed to query table count: %v", err)
	}
	if tableCount != 2 {
		t.Errorf("expected 2 table nodes, got %d", tableCount)
	}

	// Verify view node
	var viewCount int
	err = database.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ? AND node_type = 'view'", projectID).Scan(&viewCount)
	if err != nil {
		t.Fatalf("failed to query view count: %v", err)
	}
	if viewCount != 1 {
		t.Errorf("expected 1 view node, got %d", viewCount)
	}

	// Verify index node
	var idxCount int
	err = database.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ? AND node_type = 'index' AND label = 'idx_posts_user_id'", projectID).Scan(&idxCount)
	if err != nil {
		t.Fatalf("failed to query index count: %v", err)
	}
	if idxCount != 1 {
		t.Errorf("expected 1 index node 'idx_posts_user_id', got %d", idxCount)
	}

	// Verify type node
	var typeCount int
	err = database.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ? AND node_type = 'type'", projectID).Scan(&typeCount)
	if err != nil {
		t.Fatalf("failed to query type count: %v", err)
	}
	if typeCount != 1 {
		t.Errorf("expected 1 type node, got %d", typeCount)
	}

	// Verify column metadata on users table
	var metaStr string
	err = database.QueryRow("SELECT metadata FROM graph_nodes WHERE project_id = ? AND node_type = 'table' AND label = 'users'", projectID).Scan(&metaStr)
	if err != nil {
		t.Fatalf("failed to query users table metadata: %v", err)
	}
	if !strings.Contains(metaStr, "id") || !strings.Contains(metaStr, "INTEGER") {
		t.Errorf("expected users table metadata to contain column info, got: %s", metaStr)
	}

	// Verify contains edges from schema.sql
	var containsCount int
	err = database.QueryRow("SELECT COUNT(*) FROM graph_edges WHERE project_id = ? AND source_id = 'schema.sql' AND relation_type = 'contains'", projectID).Scan(&containsCount)
	if err != nil {
		t.Fatalf("failed to query contains edges: %v", err)
	}
	if containsCount < 4 {
		t.Errorf("expected at least 4 contains edges from schema.sql, got %d", containsCount)
	}
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[string]int, len(a))
	for _, v := range a {
		m[v]++
	}
	for _, v := range b {
		if m[v] == 0 {
			return false
		}
		m[v]--
	}
	return true
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
