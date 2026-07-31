package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitDBAndRegisterProject(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-db-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_storage.db")

	// 1. Test database initialization
	database, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to initialize test database: %v", err)
	}
	defer database.Close()

	// Verify tables are created
	rows, err := database.Query("SELECT name FROM sqlite_master WHERE type='table'")
	if err != nil {
		t.Fatalf("failed to query sqlite_master: %v", err)
	}
	defer rows.Close()

	tables := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil {
			tables[name] = true
		}
	}

	requiredTables := []string{"projects", "memories", "graph_nodes", "graph_edges"}
	for _, table := range requiredTables {
		if !tables[table] {
			t.Errorf("expected table %s to exist in SQLite database", table)
		}
	}

	// 2. Test RegisterProject
	projectID := "test-project-123"
	projectName := "test-project"
	projectPath := "/tmp/test-project"

	err = RegisterProject(database, projectID, projectName, projectPath)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Verify details
	var count int
	err = database.QueryRow("SELECT COUNT(*) FROM projects WHERE id = ?", projectID).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query registered project count: %v", err)
	}

	if count != 1 {
		t.Errorf("expected project count to be 1, got %d", count)
	}

	var registeredPath string
	err = database.QueryRow("SELECT path FROM projects WHERE id = ?", projectID).Scan(&registeredPath)
	if err != nil {
		t.Fatalf("failed to query registered project path: %v", err)
	}

	if registeredPath != projectPath {
		t.Errorf("expected path to be %s, got %s", projectPath, registeredPath)
	}
}

func TestForeignKeyConstraint(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-fk-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_storage.db")
	database, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to initialize db: %v", err)
	}
	defer database.Close()

	// Try inserting a memory with a project ID that does not exist in projects table
	_, err = database.Exec(`
		INSERT INTO memories (id, project_id, category, what, why, learned)
		VALUES ('mem-1', 'non-existent-project', 'bugfix', 'what', 'why', 'learned')
	`)

	if err == nil {
		t.Error("expected foreign key constraint violation error, but query succeeded")
	} else {
		// Verify foreign key error message (normally containing 'FOREIGN KEY constraint failed')
		errStr := err.Error()
		if !contains(errStr, "FOREIGN KEY") {
			t.Errorf("expected foreign key constraint error, got: %v", err)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || s[:len(substr)] == substr || len(s) > len(substr) && (s[1:] == substr || contains(s[1:], substr)))
}

// TestMigrateGraphEdgesCompositePK verifies that a database created with the
// legacy single-column id PRIMARY KEY is rebuilt into the composite
// (project_id, id) PK, and that edges from two projects with identical
// relative paths no longer collide.
func TestMigrateGraphEdgesCompositePK(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-edge-migrate")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_storage.db")
	database, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	// Simulate a legacy DB: rebuild graph_edges with the old single-column PK.
	if _, err := database.Exec("DROP TABLE IF EXISTS graph_edges"); err != nil {
		t.Fatalf("drop legacy edges: %v", err)
	}
	if _, err := database.Exec(`
		CREATE TABLE graph_edges (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			source_id TEXT NOT NULL,
			target_id TEXT NOT NULL,
			relation_type TEXT NOT NULL,
			confidence TEXT NOT NULL DEFAULT 'EXTRACTED',
			source_location TEXT
		)`); err != nil {
		t.Fatalf("create legacy edges: %v", err)
	}

	// Register two projects and matching nodes so FKs hold.
	for _, p := range []struct{ id, path string }{
		{"proj-A", "/proj/A"},
		{"proj-B", "/proj/B"},
	} {
		if err := RegisterProject(database, p.id, p.path, p.path); err != nil {
			t.Fatalf("register %s: %v", p.id, err)
		}
		for _, n := range []struct{ id, label, path string }{
			{"main.go", "main", "/proj/main.go"},
			{"utils.go", "utils", "/proj/utils.go"},
		} {
			if _, err := database.Exec(
				"INSERT OR IGNORE INTO graph_nodes (id, project_id, node_type, label, path, metadata) VALUES (?, ?, 'file', ?, ?, '{}')",
				n.id, p.id, n.label, n.path); err != nil {
				t.Fatalf("insert node: %v", err)
			}
		}
	}

	// Under the legacy schema only the first project's edge survives because
	// the id (main.go-utils.go-imports) collides across projects: inserting
	// proj-B's edge violates the global UNIQUE/PK on id.
	edgeID := "main.go-utils.go-imports"
	if _, err := database.Exec(`
		INSERT INTO graph_edges (id, project_id, source_id, target_id, relation_type, confidence)
		VALUES (?, 'proj-A', 'main.go', 'utils.go', 'imports', 'EXTRACTED')`, edgeID); err != nil {
		t.Fatalf("insert legacy edge for proj-A: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO graph_edges (id, project_id, source_id, target_id, relation_type, confidence)
		VALUES (?, 'proj-B', 'main.go', 'utils.go', 'imports', 'EXTRACTED')`, edgeID); err == nil {
		t.Error("expected legacy schema to reject the colliding edge id for proj-B")
	}

	var legacyCount int
	if err := database.QueryRow("SELECT COUNT(*) FROM graph_edges").Scan(&legacyCount); err != nil {
		t.Fatalf("count legacy edges: %v", err)
	}
	if legacyCount != 1 {
		t.Fatalf("expected 1 legacy edge (second project dropped), got %d", legacyCount)
	}

	// Now run the migration and re-verify PK shape + insert without collision.
	if err := migrateGraphEdgesCompositePK(database); err != nil {
		t.Fatalf("migrateGraphEdgesCompositePK: %v", err)
	}

	pk, err := tablePKInfo(database, "graph_edges")
	if err != nil {
		t.Fatalf("tablePKInfo: %v", err)
	}
	if len(pk) != 2 {
		t.Fatalf("expected a composite PK of 2 columns, got %v", pk)
	}
	// Both columns must participate in the PK regardless of PRAGMA ordering.
	pkSet := map[string]bool{pk[0]: true, pk[1]: true}
	if !pkSet["project_id"] || !pkSet["id"] {
		t.Fatalf("expected composite PK over [project_id, id], got %v", pk)
	}

	// Inserting the colliding edge for proj-B must now succeed.
	if _, err := database.Exec(`
		INSERT INTO graph_edges (id, project_id, source_id, target_id, relation_type, confidence)
		VALUES (?, 'proj-B', 'main.go', 'utils.go', 'imports', 'EXTRACTED')`, edgeID); err != nil {
		t.Fatalf("insert edge after migration: %v", err)
	}

	var countA, countB int
	if err := database.QueryRow("SELECT COUNT(*) FROM graph_edges WHERE project_id='proj-A'").Scan(&countA); err != nil {
		t.Fatalf("count A: %v", err)
	}
	if err := database.QueryRow("SELECT COUNT(*) FROM graph_edges WHERE project_id='proj-B'").Scan(&countB); err != nil {
		t.Fatalf("count B: %v", err)
	}
	if countA != 1 || countB != 1 {
		t.Fatalf("expected 1 edge per project after migration, got A=%d B=%d", countA, countB)
	}

	// Re-running the migration must be a no-op (idempotent).
	if err := migrateGraphEdgesCompositePK(database); err != nil {
		t.Fatalf("re-run migration: %v", err)
	}
}
