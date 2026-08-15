package memory

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/svtech-code/sv-memory/internal/db"
)

func TestCreateChange(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "changes.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-changes"
	if err = db.RegisterProject(database, projectID, "Changes Proj", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	c, err := CreateChange(database, projectID, "implement-session-auth", "Implement session-based auth", "Replace JWT stateless with sessions", "secure auth", "internal/auth/", "", "")
	if err != nil {
		t.Fatalf("failed to create change: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil change")
	}
	if c.Status != ChangeStatusDraft {
		t.Errorf("expected status draft, got %q", c.Status)
	}
	if c.ID == "" {
		t.Error("expected generated id")
	}
	if c.CreatedAt.IsZero() {
		t.Error("expected created_at to be set")
	}

	got, err := GetChange(database, projectID, c.ID)
	if err != nil {
		t.Fatalf("failed to get change: %v", err)
	}
	if got == nil {
		t.Fatal("expected change to exist after create")
	}
	if got.Title != "Implement session-based auth" {
		t.Errorf("expected title, got %q", got.Title)
	}
	if got.What != "Replace JWT stateless with sessions" {
		t.Errorf("expected what, got %q", got.What)
	}
}

func TestCreateChangeDuplicateSlug(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "changes_dup.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-changes-dup"
	if err = db.RegisterProject(database, projectID, "Dup", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	if _, err = CreateChange(database, projectID, "same-slug", "First", "", "", "", "", ""); err != nil {
		t.Fatalf("failed to create first change: %v", err)
	}
	_, err = CreateChange(database, projectID, "same-slug", "Second", "", "", "", "", "")
	if err == nil {
		t.Fatal("expected duplicate slug to be rejected")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected slug conflict message, got %v", err)
	}
}

func TestCreateChangeValidation(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "changes_val.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-changes-val"
	if err = db.RegisterProject(database, projectID, "Val", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	if _, err = CreateChange(database, projectID, "", "title", "", "", "", "", ""); err == nil {
		t.Error("expected empty slug to be rejected")
	}
	if _, err = CreateChange(database, projectID, "slug", "", "", "", "", "", ""); err == nil {
		t.Error("expected empty title to be rejected")
	}
	long := strings.Repeat("x", maxChangeFieldChars+1)
	if _, err = CreateChange(database, projectID, "slug", "title", long, "", "", "", ""); err == nil {
		t.Error("expected oversized 'what' to be rejected")
	}
}

func TestUpdateChangeStatus(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "changes_status.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-changes-status"
	if err = db.RegisterProject(database, projectID, "Status", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	c, err := CreateChange(database, projectID, "slug", "Title", "", "", "", "", "")
	if err != nil {
		t.Fatalf("failed to create change: %v", err)
	}

	transitions := []struct {
		status string
	}{
		{ChangeStatusProposed},
		{ChangeStatusValidated},
		{ChangeStatusApplied},
		{ChangeStatusArchived},
	}
	for _, tr := range transitions {
		updated, uErr := UpdateChangeStatus(database, projectID, c.ID, tr.status)
		if uErr != nil {
			t.Fatalf("failed to transition to %q: %v", tr.status, uErr)
		}
		if updated.Status != tr.status {
			t.Errorf("expected status %q, got %q", tr.status, updated.Status)
		}
		if updated.UpdatedAt.IsZero() {
			t.Error("expected updated_at to be set after transition")
		}
	}
}

func TestUpdateChangeStatusRejectsInvalid(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "changes_invalid.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-changes-invalid"
	if err = db.RegisterProject(database, projectID, "Invalid", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	c, err := CreateChange(database, projectID, "slug", "Title", "", "", "", "", "")
	if err != nil {
		t.Fatalf("failed to create change: %v", err)
	}
	if _, err = UpdateChangeStatus(database, projectID, c.ID, "bogus"); err == nil {
		t.Error("expected invalid status to be rejected")
	}
	if _, err = UpdateChangeStatus(database, projectID, "nonexistent", ChangeStatusProposed); err == nil {
		t.Error("expected unknown change to error")
	}
}

func TestUpdateChangeStatusStampsArchiveAt(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "changes_archive.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-changes-archive"
	if err = db.RegisterProject(database, projectID, "Archive", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	c, err := CreateChange(database, projectID, "slug", "Title", "", "", "", "", "")
	if err != nil {
		t.Fatalf("failed to create change: %v", err)
	}

	updated, err := UpdateChangeStatus(database, projectID, c.ID, ChangeStatusArchived)
	if err != nil {
		t.Fatalf("failed to archive: %v", err)
	}
	if updated.ArchivedAt.IsZero() {
		t.Error("expected archived_at to be stamped on archive")
	}
	if updated.ArchivedAt.Before(updated.CreatedAt) {
		t.Error("archived_at should be after created_at")
	}
}

func TestListChangesByStatus(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "changes_list.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-changes-list"
	if err = db.RegisterProject(database, projectID, "List", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	c1, err := CreateChange(database, projectID, "a", "A", "", "", "", "", "")
	if err != nil {
		t.Fatalf("failed to create change A: %v", err)
	}
	c2, err := CreateChange(database, projectID, "b", "B", "", "", "", "", "")
	if err != nil {
		t.Fatalf("failed to create change B: %v", err)
	}
	if _, err = UpdateChangeStatus(database, projectID, c2.ID, ChangeStatusProposed); err != nil {
		t.Fatalf("failed to propose change B: %v", err)
	}

	all, err := ListChangesByStatus(database, projectID, "")
	if err != nil {
		t.Fatalf("failed to list all changes: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(all))
	}

	proposed, err := ListChangesByStatus(database, projectID, ChangeStatusProposed)
	if err != nil {
		t.Fatalf("failed to list proposed changes: %v", err)
	}
	if len(proposed) != 1 || proposed[0].ID != c2.ID {
		t.Errorf("expected only change B proposed, got %d", len(proposed))
	}

	drafts, err := ListChangesByStatus(database, projectID, ChangeStatusDraft)
	if err != nil {
		t.Fatalf("failed to list draft changes: %v", err)
	}
	if len(drafts) != 1 || drafts[0].ID != c1.ID {
		t.Errorf("expected only change A draft, got %d", len(drafts))
	}

	if _, err = ListChangesByStatus(database, projectID, "bogus"); err == nil {
		t.Error("expected invalid status filter to be rejected")
	}
}

func TestGetChangeBySlug(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "changes_slug.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-changes-slug"
	if err = db.RegisterProject(database, projectID, "Slug", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	c, err := CreateChange(database, projectID, "my-slug", "Title", "", "", "", "", "")
	if err != nil {
		t.Fatalf("failed to create change: %v", err)
	}
	got, err := GetChangeBySlug(database, projectID, "my-slug")
	if err != nil {
		t.Fatalf("failed to get change by slug: %v", err)
	}
	if got == nil || got.ID != c.ID {
		t.Error("expected to find change by slug")
	}
	none, err := GetChangeBySlug(database, projectID, "missing")
	if err != nil {
		t.Fatalf("failed lookup: %v", err)
	}
	if none != nil {
		t.Error("expected nil for unknown slug")
	}
}

func TestSetMemoryChangeID(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "changes_link.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-changes-link"
	if err = db.RegisterProject(database, projectID, "Link", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	c, err := CreateChange(database, projectID, "slug", "Title", "", "", "", "", "")
	if err != nil {
		t.Fatalf("failed to create change: %v", err)
	}
	mem, err := SaveMemory(database, &Memory{
		ProjectID: projectID,
		Category:  "decision",
		What:      "Decision from change",
		Why:       "why",
		Learned:   "learned",
	})
	if err != nil {
		t.Fatalf("failed to save memory: %v", err)
	}

	if err = SetMemoryChangeID(database, projectID, mem.ID, c.ID); err != nil {
		t.Fatalf("failed to link memory to change: %v", err)
	}

	var changeID sql.NullString
	if err = database.QueryRow("SELECT change_id FROM memories WHERE id = ?", mem.ID).Scan(&changeID); err != nil {
		t.Fatalf("failed to read change_id: %v", err)
	}
	if !changeID.Valid || changeID.String != c.ID {
		t.Errorf("expected change_id %q, got %v", c.ID, changeID)
	}
}

func TestChangeLifecycleMigrationIdempotent(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "migrate.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	// The changes table, change_id column, and indexes must exist after init.
	var n int
	if err = database.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='changes'").Scan(&n); err != nil {
		t.Fatalf("failed to query changes table: %v", err)
	}
	if n != 1 {
		t.Error("expected changes table to exist")
	}

	exists := memoryColumnExists(database, "change_id")
	if !exists {
		t.Error("expected change_id column on memories")
	}

	// Re-running migrations (fresh InitDB on the same file) must not error.
	database.Close()
	database2, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to re-init db: %v", err)
	}
	defer database2.Close()

	// The full CRUD still works after a clean re-init.
	projectID := "proj-migrate"
	if err = db.RegisterProject(database2, projectID, "Migrate", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}
	c, err := CreateChange(database2, projectID, "slug", "Title", "", "", "", "", "")
	if err != nil {
		t.Fatalf("failed to create change post-migration: %v", err)
	}
	if c.CreatedAt.IsZero() || time.Since(c.CreatedAt) > time.Hour {
		t.Error("expected created_at near now")
	}
}

// memoryColumnExists reports whether the memories table has the given column,
// using PRAGMA table_info. Kept local because the db package's columnExists is
// unexported.
func memoryColumnExists(database *sql.DB, col string) bool {
	rows, err := database.Query("PRAGMA table_info(memories)")
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name string
		var typeVal string
		var notnull int
		var dfltVal interface{}
		var pk int
		if err := rows.Scan(&cid, &name, &typeVal, &notnull, &dfltVal, &pk); err == nil {
			if name == col {
				return true
			}
		}
	}
	return false
}
