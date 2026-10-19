package memory

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/svtech-code/sv-memory/internal/db"
)

// TestIsUniqueViolation verifies that constraint detection is driven by the
// driver's typed result code, not by the error message text. It exercises a
// real SQLite UNIQUE violation, a wrapped form of the same error, and a
// non-UNIQUE constraint violation that must not be misclassified.
func TestIsUniqueViolation(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "constraint.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	if _, err = database.Exec(`CREATE TABLE t (id TEXT PRIMARY KEY, slug TEXT UNIQUE)`); err != nil {
		t.Fatalf("failed to create table: %v", err)
	}
	if _, err = database.Exec(`INSERT INTO t (id, slug) VALUES ('1', 'a')`); err != nil {
		t.Fatalf("failed to seed row: %v", err)
	}

	_, err = database.Exec(`INSERT INTO t (id, slug) VALUES ('2', 'a')`)
	if err == nil {
		t.Fatal("expected a UNIQUE violation")
	}
	if !isUniqueViolation(err) {
		t.Errorf("expected isUniqueViolation=true for a real UNIQUE violation, got err=%v", err)
	}
	if !isUniqueViolation(fmt.Errorf("create change: %w", err)) {
		t.Error("expected isUniqueViolation=true for a wrapped UNIQUE violation")
	}

	// A NOT NULL violation is a constraint error but not a UNIQUE one: the
	// helper must not treat every constraint error as a slug collision.
	if _, err = database.Exec(`CREATE TABLE u (id TEXT PRIMARY KEY, v TEXT NOT NULL)`); err != nil {
		t.Fatalf("failed to create table u: %v", err)
	}
	_, err = database.Exec(`INSERT INTO u (id, v) VALUES ('1', NULL)`)
	if err == nil {
		t.Fatal("expected a NOT NULL violation")
	}
	if isUniqueViolation(err) {
		t.Errorf("expected isUniqueViolation=false for a NOT NULL violation, got err=%v", err)
	}

	if isUniqueViolation(nil) {
		t.Error("expected isUniqueViolation=false for nil")
	}
	if isUniqueViolation(sql.ErrNoRows) {
		t.Error("expected isUniqueViolation=false for an unrelated error")
	}
}
