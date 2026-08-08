package memory

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/svtech-code/sv-memory/internal/db"
)

func TestUpdateMemoryPartialFields(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "update_test.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	const projectID = "upd-project"
	if err := db.RegisterProject(database, projectID, "Update Project", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	saved, err := SaveMemory(database, &Memory{
		ProjectID: projectID,
		Category:  "decision",
		What:      "Use Postgres for user storage",
		Why:       "Relational queries and scaling",
		Learned:   "Relational model fits user schema",
		WherePath: "internal/db/db.go",
		Impact:    "Faster joins",
		NextSteps: "Migrate users table",
	})
	if err != nil {
		t.Fatalf("failed to save memory: %v", err)
	}

	// Partial update: change only 'what' and 'impact'; the rest must be preserved.
	newWhat := "Use Postgres 15 for user storage"
	newImpact := "Faster joins + better tooling"
	updated, err := UpdateMemory(database, projectID, saved.ID, MemoryUpdate{
		What:   &newWhat,
		Impact: &newImpact,
	})
	if err != nil {
		t.Fatalf("failed to update memory: %v", err)
	}

	if updated.What != newWhat {
		t.Fatalf("expected what=%q, got %q", newWhat, updated.What)
	}
	if updated.Impact != newImpact {
		t.Fatalf("expected impact=%q, got %q", newImpact, updated.Impact)
	}
	if updated.Why != "Relational queries and scaling" {
		t.Fatalf("expected why to be preserved, got %q", updated.Why)
	}
	if updated.Learned != "Relational model fits user schema" {
		t.Fatalf("expected learned to be preserved, got %q", updated.Learned)
	}
	if updated.WherePath != "internal/db/db.go" {
		t.Fatalf("expected where_path to be preserved, got %q", updated.WherePath)
	}
	if updated.NextSteps != "Migrate users table" {
		t.Fatalf("expected next_steps to be preserved, got %q", updated.NextSteps)
	}
	if updated.Category != "decision" {
		t.Fatalf("expected category to be preserved, got %q", updated.Category)
	}
	if updated.RevisionCount != saved.RevisionCount+1 {
		t.Fatalf("expected revision_count %d, got %d", saved.RevisionCount+1, updated.RevisionCount)
	}
	if updated.ID != saved.ID {
		t.Fatalf("expected same id, got %s", updated.ID)
	}
}

func TestUpdateMemoryClearsFieldWithEmptyString(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "update_clear_test.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	const projectID = "upd-clear"
	if err := db.RegisterProject(database, projectID, "Clear Project", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	saved, err := SaveMemory(database, &Memory{
		ProjectID: projectID,
		Category:  "bugfix",
		What:      "Fixed N+1 query",
		Why:       "Eager loading added",
		Learned:   "Use eager loading",
		NextSteps: "Benchmark",
	})
	if err != nil {
		t.Fatalf("failed to save memory: %v", err)
	}

	empty := ""
	updated, err := UpdateMemory(database, projectID, saved.ID, MemoryUpdate{NextSteps: &empty})
	if err != nil {
		t.Fatalf("failed to update memory: %v", err)
	}
	if updated.NextSteps != "" {
		t.Fatalf("expected next_steps cleared, got %q", updated.NextSteps)
	}
	if updated.What != "Fixed N+1 query" {
		t.Fatalf("expected what preserved, got %q", updated.What)
	}
}

func TestUpdateMemoryNotFound(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "update_missing_test.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	const projectID = "upd-missing"
	if err := db.RegisterProject(database, projectID, "Missing Project", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	what := "anything"
	if _, err := UpdateMemory(database, projectID, "nope", MemoryUpdate{What: &what}); err == nil {
		t.Fatal("expected error for missing memory, got nil")
	} else if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got %v", err)
	}
}

func TestUpdateMemorySanitizesSecrets(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "update_sanitize_test.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	const projectID = "upd-sanitize"
	if err := db.RegisterProject(database, projectID, "Sanitize Project", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	saved, err := SaveMemory(database, &Memory{
		ProjectID: projectID,
		Category:  "standard",
		What:      "Connection config",
		Why:       "Needs env vars",
		Learned:   "Use env vars",
	})
	if err != nil {
		t.Fatalf("failed to save memory: %v", err)
	}

	leaky := "export DB_PASSWORD=\"hunter2secret\""
	updated, err := UpdateMemory(database, projectID, saved.ID, MemoryUpdate{Learned: &leaky})
	if err != nil {
		t.Fatalf("failed to update memory: %v", err)
	}
	if strings.Contains(updated.Learned, "hunter2secret") {
		t.Fatalf("expected secret to be redacted, got %q", updated.Learned)
	}
}

func TestMarkMemoryReviewed(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "review_test.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	const projectID = "review-project"
	if err := db.RegisterProject(database, projectID, "Review Project", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	saved, err := SaveMemory(database, &Memory{
		ProjectID: projectID,
		Category:  "decision",
		What:      "Architecture decision to revalidate",
		Why:       "Needs periodic sanity check",
		Learned:   "Revalidate every 6 months",
	})
	if err != nil {
		t.Fatalf("failed to save memory: %v", err)
	}

	// Force the review deadline into the past so the memory is due for review.
	if _, err := database.Exec("UPDATE memories SET review_after = ? WHERE id = ?", "2020-01-01 00:00:00", saved.ID); err != nil {
		t.Fatalf("failed to backdate review_after: %v", err)
	}

	items, err := ReviewMemories(database, projectID, 10)
	if err != nil {
		t.Fatalf("failed to review memories: %v", err)
	}
	foundDue := false
	for _, item := range items {
		if item.Memory.ID == saved.ID && item.NeedsReview {
			foundDue = true
			break
		}
	}
	if !foundDue {
		t.Fatal("expected memory to be flagged as needs review before mark_reviewed")
	}

	if err := MarkMemoryReviewed(database, projectID, saved.ID); err != nil {
		t.Fatalf("failed to mark memory reviewed: %v", err)
	}

	got, err := GetMemory(database, projectID, saved.ID)
	if err != nil {
		t.Fatalf("failed to get memory: %v", err)
	}
	if got == nil || got.ReviewAfter.Before(time.Now()) {
		t.Fatalf("expected review_after reset into the future, got %v", got.ReviewAfter)
	}
	if !got.ReviewAfter.After(got.CreatedAt) {
		t.Fatalf("expected new review deadline after created_at, got %v", got.ReviewAfter)
	}

	items, err = ReviewMemories(database, projectID, 10)
	if err != nil {
		t.Fatalf("failed to re-review memories: %v", err)
	}
	for _, item := range items {
		if item.Memory.ID == saved.ID && item.NeedsReview {
			t.Fatal("memory should no longer be flagged as needs review after mark_reviewed")
		}
	}
}

func TestMarkMemoryReviewedNotFound(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "review_missing_test.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	const projectID = "review-missing"
	if err := db.RegisterProject(database, projectID, "Review Missing", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	if err := MarkMemoryReviewed(database, projectID, "nope"); err == nil {
		t.Fatal("expected error for missing memory, got nil")
	}
}
