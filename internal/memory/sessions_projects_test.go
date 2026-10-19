package memory

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/svtech-code/sv-memory/internal/db"
)

func TestListPruneConsolidateProjects(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-projects-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_storage.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	// Register two projects
	err = db.RegisterProject(database, "proj-a", "Project A", filepath.Join(tempDir, "a"))
	if err != nil {
		t.Fatalf("failed to register proj-a: %v", err)
	}
	err = db.RegisterProject(database, "proj-b", "Project B", filepath.Join(tempDir, "b"))
	if err != nil {
		t.Fatalf("failed to register proj-b: %v", err)
	}
	err = db.RegisterProject(database, "proj-c", "Project C", filepath.Join(tempDir, "c"))
	if err != nil {
		t.Fatalf("failed to register proj-c: %v", err)
	}

	// Save a memory in proj-a
	_, err = SaveMemory(database, &Memory{
		ID: "pa-1", ProjectID: "proj-a", Category: "decision",
		What: "Test", Why: "why", Learned: "learned", CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("failed saving memory: %v", err)
	}

	// Test ListProjects
	projects, err := ListProjects(database)
	if err != nil {
		t.Fatalf("ListProjects failed: %v", err)
	}
	if len(projects) != 3 {
		t.Fatalf("expected 3 projects, got %d", len(projects))
	}

	// Check counts
	for _, p := range projects {
		switch p.ID {
		case "proj-a":
			if p.MemoryCount != 1 {
				t.Errorf("proj-a expected 1 memory, got %d", p.MemoryCount)
			}
		case "proj-b":
			if p.MemoryCount != 0 {
				t.Errorf("proj-b expected 0 memories, got %d", p.MemoryCount)
			}
		case "proj-c":
			if p.MemoryCount != 0 {
				t.Errorf("proj-c expected 0 memories, got %d", p.MemoryCount)
			}
		}
	}

	// Test PruneProjects (should remove proj-b and proj-c but not proj-a)
	pruned, err := PruneProjects(database)
	if err != nil {
		t.Fatalf("PruneProjects failed: %v", err)
	}
	if len(pruned) != 2 {
		t.Errorf("expected 2 pruned projects, got %d: %v", len(pruned), pruned)
	}

	// Verify proj-a still exists
	projects, err = ListProjects(database)
	if err != nil {
		t.Fatalf("ListProjects failed: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project after prune, got %d", len(projects))
	}
	if projects[0].ID != "proj-a" {
		t.Errorf("expected proj-a to remain, got %s", projects[0].ID)
	}

	// Create two more projects for consolidate test
	err = db.RegisterProject(database, "proj-x", "Project X", filepath.Join(tempDir, "x"))
	if err != nil {
		t.Fatalf("failed to register proj-x: %v", err)
	}
	err = db.RegisterProject(database, "proj-y", "Project Y", filepath.Join(tempDir, "y"))
	if err != nil {
		t.Fatalf("failed to register proj-y: %v", err)
	}

	// Save a memory in proj-x
	_, err = SaveMemory(database, &Memory{
		ID: "px-1", ProjectID: "proj-x", Category: "decision",
		What: "From X", Why: "why", Learned: "l", CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("failed saving memory in proj-x: %v", err)
	}

	// Test ConsolidateProjects: move from proj-x to proj-a
	mems, sess, err := ConsolidateProjects(database, "proj-x", "proj-a")
	if err != nil {
		t.Fatalf("ConsolidateProjects failed: %v", err)
	}
	if mems != 1 {
		t.Errorf("expected 1 memory moved, got %d", mems)
	}
	if sess != 0 {
		t.Errorf("expected 0 sessions moved, got %d", sess)
	}

	// Verify proj-x is gone and proj-a now has 2 memories
	var count int
	database.QueryRow("SELECT COUNT(*) FROM projects WHERE id='proj-x'").Scan(&count)
	if count != 0 {
		t.Error("expected proj-x to be deleted after consolidation")
	}
	database.QueryRow("SELECT COUNT(*) FROM memories WHERE project_id='proj-a' AND deleted_at IS NULL").Scan(&count)
	if count != 2 {
		t.Errorf("expected proj-a to have 2 memories, got %d", count)
	}
}

func TestDeleteSessionAndProject(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-delete-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_storage.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-delete"
	err = db.RegisterProject(database, projectID, "Delete Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Start a session
	session, err := StartSession(database, projectID, "test", tempDir)
	if err != nil {
		t.Fatalf("failed starting session: %v", err)
	}

	// Save a memory associated with the session
	_, err = SaveMemory(database, &Memory{
		ID: "d-1", ProjectID: projectID, Category: "decision",
		What: "Test", Why: "why", Learned: "l",
		SessionID: session.ID, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("failed saving memory: %v", err)
	}

	// Test DeleteSession: should fail because memories are associated
	err = DeleteSession(database, session.ID)
	if err == nil {
		t.Error("expected error deleting session with memories, got nil")
	}

	// Test DeleteProject soft
	err = DeleteProject(database, projectID, false)
	if err != nil {
		t.Fatalf("DeleteProject soft failed: %v", err)
	}

	// Verify project still exists as shell
	var count int
	database.QueryRow("SELECT COUNT(*) FROM projects WHERE id=?", projectID).Scan(&count)
	if count != 1 {
		t.Error("expected project to remain as shell after soft delete")
	}

	// Verify memories are soft-deleted
	database.QueryRow("SELECT COUNT(*) FROM memories WHERE project_id=? AND deleted_at IS NULL", projectID).Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 active memories, got %d", count)
	}

	// Check soft-deleted memories exist
	database.QueryRow("SELECT COUNT(*) FROM memories WHERE project_id=? AND deleted_at IS NOT NULL", projectID).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 soft-deleted memory, got %d", count)
	}

	// Verify sessions are deleted
	database.QueryRow("SELECT COUNT(*) FROM sessions WHERE project_id=?", projectID).Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 sessions after soft delete, got %d", count)
	}

	// Test hard delete with a new project
	err = db.RegisterProject(database, "proj-hard", "Hard Delete Proj", filepath.Join(tempDir, "hard-path"))
	if err != nil {
		t.Fatalf("failed to register proj-hard: %v", err)
	}

	_, err = SaveMemory(database, &Memory{
		ID: "hd-1", ProjectID: "proj-hard", Category: "decision",
		What: "Hard", Why: "why", Learned: "l", CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("failed saving memory in proj-hard: %v", err)
	}

	err = DeleteProject(database, "proj-hard", true)
	if err != nil {
		t.Fatalf("DeleteProject hard failed: %v", err)
	}

	database.QueryRow("SELECT COUNT(*) FROM memories WHERE project_id='proj-hard'").Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 memories after hard delete, got %d", count)
	}
}

// insertOldMemory inserts a memory with an artificially old created_at and
// last_seen_at so the prune cutoff treats it as stale.
func insertOldMemory(t *testing.T, database *sql.DB, projectID, id, cat, what string, daysOld int) {
	t.Helper()
	old := time.Now().Add(-time.Duration(daysOld) * 24 * time.Hour)
	if _, err := database.Exec(`INSERT INTO memories (id, project_id, category, what, why, learned, created_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, id, projectID, cat, what, "why", "learned", old, old); err != nil {
		t.Fatalf("insert old memory %s: %v", id, err)
	}
}

func TestPruneStaleMemories(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "prune.db"))
	if err != nil {
		t.Fatalf("InitDB error: %v", err)
	}
	defer database.Close()

	const projectID = "proj-prune"
	if err = db.RegisterProject(database, projectID, "Prune", tempDir); err != nil {
		t.Fatalf("RegisterProject error: %v", err)
	}

	insertOldMemory(t, database, projectID, "j-stale", "journal", "Old journal", 200)
	insertOldMemory(t, database, projectID, "j-fresh", "journal", "Fresh journal", 1)
	insertOldMemory(t, database, projectID, "d-stale", "decision", "Old decision", 200)
	insertOldMemory(t, database, projectID, "j-pinned", "journal", "Pinned journal", 200)
	if _, err = database.Exec("UPDATE memories SET pinned = 1 WHERE id = 'j-pinned'"); err != nil {
		t.Fatalf("pin memory: %v", err)
	}

	// Dry run: only the stale, unpinned, transient journal is listed; nothing
	// is deleted.
	pruned, err := PruneStaleMemories(database, projectID, 90, "", false)
	if err != nil {
		t.Fatalf("PruneStaleMemories dry run: %v", err)
	}
	if len(pruned) != 1 || pruned[0].ID != "j-stale" {
		t.Fatalf("dry run should list only j-stale, got %+v", pruned)
	}
	var active int
	if err = database.QueryRow("SELECT COUNT(*) FROM memories WHERE project_id = ? AND deleted_at IS NULL", projectID).Scan(&active); err != nil {
		t.Fatalf("count active: %v", err)
	}
	if active != 4 {
		t.Fatalf("dry run must not delete: expected 4 active, got %d", active)
	}

	// Apply: only the stale transient journal is soft-deleted; the fresh
	// journal, the durable decision, and the pinned journal stay.
	pruned, err = PruneStaleMemories(database, projectID, 90, "", true)
	if err != nil {
		t.Fatalf("PruneStaleMemories apply: %v", err)
	}
	if len(pruned) != 1 || pruned[0].ID != "j-stale" {
		t.Fatalf("apply should prune only j-stale, got %+v", pruned)
	}
	var deleted int
	if err = database.QueryRow("SELECT COUNT(*) FROM memories WHERE project_id = ? AND deleted_at IS NOT NULL", projectID).Scan(&deleted); err != nil {
		t.Fatalf("count deleted: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 soft-deleted memory, got %d", deleted)
	}
	for _, id := range []string{"j-fresh", "d-stale", "j-pinned"} {
		var del bool
		if err = database.QueryRow("SELECT deleted_at IS NOT NULL FROM memories WHERE id = ?", id).Scan(&del); err != nil {
			t.Fatalf("check %s: %v", id, err)
		}
		if del {
			t.Errorf("memory %s must not be pruned", id)
		}
	}

	// Category override: an explicitly requested category is pruned too.
	insertOldMemory(t, database, projectID, "b-stale", "bugfix", "Old bugfix", 200)
	pruned, err = PruneStaleMemories(database, projectID, 90, "bugfix", false)
	if err != nil {
		t.Fatalf("PruneStaleMemories override: %v", err)
	}
	if len(pruned) != 1 || pruned[0].ID != "b-stale" {
		t.Fatalf("category override should list b-stale, got %+v", pruned)
	}
}

func TestGetSessionContextEdgeCases(t *testing.T) {
	t.Run("new project without sessions or memories", func(t *testing.T) {
		database, err := db.InitDB(filepath.Join(t.TempDir(), "edge_new.db"))
		if err != nil {
			t.Fatalf("failed to init db: %v", err)
		}
		defer database.Close()

		projectID := "proj-edge-new"
		err = db.RegisterProject(database, projectID, "Edge New Proj", t.TempDir())
		if err != nil {
			t.Fatalf("failed to register project: %v", err)
		}

		ctx, err := GetSessionContext(database, projectID, 0)
		if err != nil {
			t.Fatalf("failed GetSessionContext: %v", err)
		}
		if ctx != "No previous session context found for this project." {
			t.Errorf("expected empty-project message, got: %s", ctx)
		}
	})

	t.Run("active session is treated as nonexistent", func(t *testing.T) {
		database, err := db.InitDB(filepath.Join(t.TempDir(), "edge_active.db"))
		if err != nil {
			t.Fatalf("failed to init db: %v", err)
		}
		defer database.Close()

		projectID := "proj-edge-active"
		tempDir := t.TempDir()
		err = db.RegisterProject(database, projectID, "Edge Active Proj", tempDir)
		if err != nil {
			t.Fatalf("failed to register project: %v", err)
		}

		// An unclosed session must not be surfaced as previous context.
		_, err = StartSession(database, projectID, "Unfinished work", tempDir)
		if err != nil {
			t.Fatalf("failed to start session: %v", err)
		}
		_, err = SaveMemory(database, &Memory{
			ProjectID: projectID, Category: "journal", What: "Ongoing notes",
			CreatedAt: time.Now(),
		})
		if err != nil {
			t.Fatalf("failed to save memory: %v", err)
		}

		ctx, err := GetSessionContext(database, projectID, 0)
		if err != nil {
			t.Fatalf("failed GetSessionContext: %v", err)
		}
		if !strings.Contains(ctx, "No recorded sessions. Most recent memories:") {
			t.Errorf("expected fallback to recent memories for active-only session, got: %s", ctx)
		}
		if !strings.Contains(ctx, "Ongoing notes") {
			t.Errorf("expected recent memory listed in fallback, got: %s", ctx)
		}
	})

	t.Run("bundle omits decisions when none exist", func(t *testing.T) {
		database, err := db.InitDB(filepath.Join(t.TempDir(), "edge_nodecisions.db"))
		if err != nil {
			t.Fatalf("failed to init db: %v", err)
		}
		defer database.Close()

		projectID := "proj-edge-nodecisions"
		tempDir := t.TempDir()
		err = db.RegisterProject(database, projectID, "Edge No Decisions Proj", tempDir)
		if err != nil {
			t.Fatalf("failed to register project: %v", err)
		}

		sess, err := StartSession(database, projectID, "Journal only session", tempDir)
		if err != nil {
			t.Fatalf("failed to start session: %v", err)
		}
		_, err = SaveMemory(database, &Memory{
			ProjectID: projectID, Category: "journal", What: "Work log entry",
			SessionID: sess.ID, CreatedAt: time.Now(),
		})
		if err != nil {
			t.Fatalf("failed to save memory: %v", err)
		}
		err = EndSession(database, sess.ID, "Finished logging")
		if err != nil {
			t.Fatalf("failed to end session: %v", err)
		}

		bundle, err := GetAutoBootBundle(context.Background(), database, projectID, AutoBootOptions{})
		if err != nil {
			t.Fatalf("failed GetAutoBootBundle: %v", err)
		}
		if !strings.Contains(bundle, "## Previous Session Context") {
			t.Errorf("expected session context in bundle, got:\n%s", bundle)
		}
		if strings.Contains(bundle, "**Key Architectural Decisions:**") {
			t.Errorf("expected no architectural decisions section when only journal memories exist, got:\n%s", bundle)
		}
	})
}

func TestEndSession_IdempotentAndNotFound(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-memory-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "test-session-idempotent"
	err = db.RegisterProject(database, projectID, "Test Session Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}
	sess, err := StartSession(database, projectID, "Test session idempotence", tempDir)
	if err != nil {
		t.Fatalf("failed to start session: %v", err)
	}

	// 1. Ending an active session should succeed.
	if err = EndSession(database, sess.ID, "Initial completion summary"); err != nil {
		t.Fatalf("EndSession on active session failed: %v", err)
	}

	// Verify session status and summary
	var status, summary string
	err = database.QueryRow("SELECT status, summary FROM sessions WHERE id = ?", sess.ID).Scan(&status, &summary)
	if err != nil {
		t.Fatalf("failed to query session: %v", err)
	}
	if status != "completed" {
		t.Errorf("expected status 'completed', got %q", status)
	}
	if summary != "Initial completion summary" {
		t.Errorf("expected summary 'Initial completion summary', got %q", summary)
	}

	// 2. Ending an already completed session should return ErrSessionAlreadyCompleted
	// and preserve the existing summary without overwriting.
	err = EndSession(database, sess.ID, "New summary that should not overwrite")
	if err == nil {
		t.Fatal("expected error ending already completed session, got nil")
	}
	if !errors.Is(err, ErrSessionAlreadyCompleted) {
		t.Errorf("expected ErrSessionAlreadyCompleted, got: %v", err)
	}

	// Verify summary was preserved
	err = database.QueryRow("SELECT summary FROM sessions WHERE id = ?", sess.ID).Scan(&summary)
	if err != nil {
		t.Fatalf("failed to query session after 2nd end: %v", err)
	}
	if summary != "Initial completion summary" {
		t.Errorf("expected summary preserved as 'Initial completion summary', got %q", summary)
	}

	// 3. Ending a non-existent session should return ErrSessionNotFound.
	err = EndSession(database, "non-existent-session-id", "Some summary")
	if err == nil {
		t.Fatal("expected error ending non-existent session, got nil")
	}
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got: %v", err)
	}
}

func TestCloseStaleSessions(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-close-stale")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_storage.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "test-project"
	if err = db.RegisterProject(database, projectID, "test-proj", "/tmp"); err != nil {
		t.Fatalf("RegisterProject error: %v", err)
	}

	// Create a session that started 2 hours ago (stale with 1h threshold).
	sess1, err := StartSession(database, projectID, "old session", "/tmp")
	if err != nil {
		t.Fatalf("StartSession error: %v", err)
	}
	database.Exec("UPDATE sessions SET started_at = ? WHERE id = ?", time.Now().Add(-2*time.Hour), sess1.ID)

	// Create a fresh session (not stale).
	sess2, err := StartSession(database, projectID, "fresh session", "/tmp")
	if err != nil {
		t.Fatalf("StartSession error: %v", err)
	}

	// Close stale sessions with 1h threshold.
	closed, err := CloseStaleSessions(database, projectID, 1*time.Hour)
	if err != nil {
		t.Fatalf("CloseStaleSessions error: %v", err)
	}
	if closed != 1 {
		t.Fatalf("expected 1 stale session closed, got %d", closed)
	}

	// Verify old session is completed.
	var status1 string
	database.QueryRow("SELECT status FROM sessions WHERE id = ?", sess1.ID).Scan(&status1)
	if status1 != "completed" {
		t.Fatalf("expected old session to be completed, got %s", status1)
	}

	// Verify fresh session is still active.
	var status2 string
	database.QueryRow("SELECT status FROM sessions WHERE id = ?", sess2.ID).Scan(&status2)
	if status2 != "active" {
		t.Fatalf("expected fresh session to be active, got %s", status2)
	}

	// Idempotent: closing again should return 0.
	closed2, err := CloseStaleSessions(database, projectID, 1*time.Hour)
	if err != nil {
		t.Fatalf("CloseStaleSessions idempotent error: %v", err)
	}
	if closed2 != 0 {
		t.Fatalf("expected 0 on second call, got %d", closed2)
	}
}
