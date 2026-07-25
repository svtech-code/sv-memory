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
