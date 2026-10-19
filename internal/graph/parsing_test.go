package graph

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/svtech-code/sv-memory/internal/db"
)

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
	// The call is on line 4, col 2 of main.go (relative to 1-based index)
	if sourceLoc != "L4:2" && sourceLoc != "L4" {
		t.Errorf("expected source_location to be 'L4:2' or 'L4', got: %q", sourceLoc)
	}
}

func TestSyncGraphASTCallEdgesExtracted(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-graph-astcalls-test")
	if err != nil {
		t.Fatalf("failed to create temp workspace: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_astcalls.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init DB: %v", err)
	}
	defer database.Close()

	projectID := "proj-astcalls-test"
	err = db.RegisterProject(database, projectID, "AST Calls Test Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Python: helper() defined in one file, called from another (cross-file).
	// The AST path must produce an EXTRACTED edge with a precise L<line>:<col>.
	pythonHelper := `def helper():
    return 42
`
	pythonCaller := `def caller():
    return helper()
`
	err = os.WriteFile(filepath.Join(tempDir, "helper.py"), []byte(pythonHelper), 0644)
	if err != nil {
		t.Fatalf("failed writing helper.py: %v", err)
	}
	err = os.WriteFile(filepath.Join(tempDir, "caller.py"), []byte(pythonCaller), 0644)
	if err != nil {
		t.Fatalf("failed writing caller.py: %v", err)
	}

	err = SyncGraph(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("SyncGraph failed: %v", err)
	}

	var sourceID, targetID, relType, confidence, sourceLoc string
	err = database.QueryRow(`
		SELECT source_id, target_id, relation_type, confidence, source_location
		FROM graph_edges
		WHERE project_id = ? AND relation_type = 'calls'
	`, projectID).Scan(&sourceID, &targetID, &relType, &confidence, &sourceLoc)
	if err != nil {
		t.Fatalf("failed to query calls edge: %v", err)
	}

	if sourceID != "caller.py:caller" {
		t.Errorf("expected source 'caller.py:caller', got %q", sourceID)
	}
	if targetID != "helper.py:helper" {
		t.Errorf("expected target 'helper.py:helper', got %q", targetID)
	}
	if confidence != "EXTRACTED" {
		t.Errorf("expected AST edge confidence EXTRACTED, got %q", confidence)
	}
	// The call is on line 2 of caller.py; the column points at the call node
	// (1-based), which may vary slightly by parser grammar — the line must be
	// exact and the location must carry a column.
	var locLine, locCol int
	if _, err := fmt.Sscanf(sourceLoc, "L%d:%d", &locLine, &locCol); err != nil {
		t.Fatalf("expected source_location L<line>:<col>, got %q", sourceLoc)
	}
	if locLine != 2 {
		t.Errorf("expected call on line 2, got %d", locLine)
	}
	if locCol < 1 {
		t.Errorf("expected 1-based column, got %d", locCol)
	}
}

// TestSyncGraphASTCallTopLevelNoPanic guards against a nil-caller panic in
// extractASTCallEdges: a call at the top level of a file (before the first
// function/class, or in a file with no functions) has no containing caller, so
// the AST resolver used to dereference a nil caller and panic. The top-level
// call must be safely skipped.
func TestSyncGraphASTCallTopLevelNoPanic(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-graph-topcall-test")
	if err != nil {
		t.Fatalf("failed to create temp workspace: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_topcall.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init DB: %v", err)
	}
	defer database.Close()

	projectID := "proj-topcall-test"
	if err = db.RegisterProject(database, projectID, "Top Call Test Proj", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// helper() is defined in its own file; another JS file calls it at the
	// TOP LEVEL (line 1), before any function/class exists in that file. This
	// reproduces the nil-caller path (no symbol has start line <= the call).
	helper := `export function helper() { return 1; }
`
	topLevel := `import { helper } from "./helper.js";
helper();
`
	if err = os.WriteFile(filepath.Join(tempDir, "helper.js"), []byte(helper), 0644); err != nil {
		t.Fatalf("failed writing helper.js: %v", err)
	}
	if err = os.WriteFile(filepath.Join(tempDir, "main.js"), []byte(topLevel), 0644); err != nil {
		t.Fatalf("failed writing main.js: %v", err)
	}

	// Must not panic; the top-level call is skipped (no source caller).
	if err = SyncGraph(database, projectID, tempDir); err != nil {
		t.Fatalf("SyncGraph failed: %v", err)
	}

	// The helper function may still be reachable from an import edge, but there
	// must be no 'calls' edge whose source is the top-level main.js call.
	var count int
	if err = database.QueryRow(
		"SELECT COUNT(*) FROM graph_edges WHERE project_id = ? AND relation_type = 'calls' AND source_id = 'main.js'",
		projectID).Scan(&count); err != nil {
		t.Fatalf("failed counting top-level calls edges: %v", err)
	}
	if count != 0 {
		t.Errorf("expected no calls edge sourced from top-level main.js, got %d", count)
	}
}

func TestSyncGraphMixedGoPythonASTCallEdges(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-graph-mixed-test")
	if err != nil {
		t.Fatalf("failed to create temp workspace: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_mixed.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init DB: %v", err)
	}
	defer database.Close()

	projectID := "proj-mixed-test"
	err = db.RegisterProject(database, projectID, "Mixed Test Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Python file and Go file both use AST call extraction.
	// Both must produce their calls edges with confidence EXTRACTED.
	pythonCode := "def helper():\n    pass\ndef caller():\n    helper()\n"
	goCode := `package main

func helperFunc() {}

func callerFunc() {
	helperFunc()
}
`
	err = os.WriteFile(filepath.Join(tempDir, "a.py"), []byte(pythonCode), 0644)
	if err != nil {
		t.Fatalf("failed writing a.py: %v", err)
	}
	err = os.WriteFile(filepath.Join(tempDir, "main.go"), []byte(goCode), 0644)
	if err != nil {
		t.Fatalf("failed writing main.go: %v", err)
	}

	err = SyncGraph(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("SyncGraph failed: %v", err)
	}

	// The Go calls edge must exist with AST confidence EXTRACTED.
	var goSource, goTarget, goConf, goLoc string
	err = database.QueryRow(`
		SELECT source_id, target_id, confidence, source_location FROM graph_edges
		WHERE project_id = ? AND relation_type = 'calls'
		  AND source_id = 'main.go:callerFunc' AND target_id = 'main.go:helperFunc'
	`, projectID).Scan(&goSource, &goTarget, &goConf, &goLoc)
	if err != nil {
		t.Fatalf("expected Go AST calls edge: %v", err)
	}
	if goSource != "main.go:callerFunc" || goTarget != "main.go:helperFunc" {
		t.Errorf("unexpected Go edge %s -> %s", goSource, goTarget)
	}
	if goConf != "EXTRACTED" {
		t.Errorf("expected Go AST confidence EXTRACTED, got %q", goConf)
	}
	if !strings.HasPrefix(goLoc, "L") {
		t.Errorf("expected Go AST source_location with L<line> prefix, got %q", goLoc)
	}

	// The Python calls edge must exist with AST confidence EXTRACTED.
	var pySource, pyTarget, pyConf string
	err = database.QueryRow(`
		SELECT source_id, target_id, confidence FROM graph_edges
		WHERE project_id = ? AND relation_type = 'calls'
		  AND source_id = 'a.py:caller' AND target_id = 'a.py:helper'
	`, projectID).Scan(&pySource, &pyTarget, &pyConf)
	if err != nil {
		t.Fatalf("expected Python AST calls edge: %v", err)
	}
	if pyConf != "EXTRACTED" {
		t.Errorf("expected Python AST confidence EXTRACTED, got %q", pyConf)
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
