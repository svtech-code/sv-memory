package graph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/svtech-code/sv-memory/internal/db"
)

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
