package memory

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/svtech-code/sv-memory/internal/db"
)

func setupFeaturesTest(t *testing.T) (string, *sql.DB, string) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "sv-mem-features")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	dbPath := filepath.Join(tempDir, "features.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	projectID := "features-proj"
	if err := db.RegisterProject(database, projectID, "Features", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}
	t.Cleanup(func() {
		database.Close()
		os.RemoveAll(tempDir)
	})
	return tempDir, database, projectID
}

func TestSaveMemorySetsReviewAfterByCategory(t *testing.T) {
	_, dbc, projectID := setupFeaturesTest(t)
	m, err := SaveMemory(dbc, &Memory{
		ID: "rev-1", ProjectID: projectID, Category: "decision",
		What: "Adopt Postgres", Why: "relational", Learned: "use sql", CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("save failed: %v", err)
	}
	if m.ReviewAfter.IsZero() {
		t.Fatal("expected review_after to be set on save")
	}
	expected := decayReviewAfter("decision")
	if m.ReviewAfter.Sub(m.CreatedAt) < expected-time.Hour {
		t.Fatalf("review_after should be ~%s after created_at, got %s", expected, m.ReviewAfter.Sub(m.CreatedAt))
	}
}

func TestReviewMemoriesFlagsDueReview(t *testing.T) {
	_, dbc, projectID := setupFeaturesTest(t)
	if _, err := SaveMemory(dbc, &Memory{
		ID: "rev-due", ProjectID: projectID, Category: "standard",
		What: "Standard to revalidate", Why: "policy", Learned: "review", CreatedAt: time.Now(),
		ReviewAfter: time.Now().Add(-24 * time.Hour),
	}); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	items, err := ReviewMemories(dbc, projectID, 10)
	if err != nil {
		t.Fatalf("review failed: %v", err)
	}
	found := false
	for _, it := range items {
		if it.Memory.ID == "rev-due" {
			found = true
			if !it.NeedsReview {
				t.Error("expected NeedsReview=true for a standard past its 12-month review window")
			}
			if !strings.Contains(it.Reason, "due for review") {
				t.Errorf("expected 'due for review' reason, got %q", it.Reason)
			}
		}
	}
	if !found {
		t.Fatal("expected rev-due memory in review results")
	}
}

func TestMatchModeAnyBroadensRecall(t *testing.T) {
	_, dbc, projectID := setupFeaturesTest(t)
	mems := []*Memory{
		{ID: "mm-1", Category: "decision", What: "auth service uses JWT", Why: "security", Learned: "ok"},
		{ID: "mm-2", Category: "decision", What: "cache uses redis for sessions", Why: "perf", Learned: "ok"},
	}
	for _, m := range mems {
		m.ProjectID = projectID
		m.CreatedAt = time.Now()
		if _, err := SaveMemory(dbc, m); err != nil {
			t.Fatalf("save %s failed: %v", m.ID, err)
		}
	}
	all, err := SearchMemoriesCompactScoped(dbc, projectID, "auth redis", "", "", "all", 10, 0)
	if err != nil {
		t.Fatalf("all-mode search failed: %v", err)
	}
	anyMode, err := SearchMemoriesCompactScoped(dbc, projectID, "auth redis", "", "", "any", 10, 0)
	if err != nil {
		t.Fatalf("any-mode search failed: %v", err)
	}
	if len(anyMode) <= len(all) {
		t.Errorf("expected any-mode to return at least as many results as all-mode (any=%d, all=%d)", len(anyMode), len(all))
	}
	if len(anyMode) == 0 {
		t.Fatal("expected any-mode to return results")
	}
}

func TestPinUnpinMemory(t *testing.T) {
	_, dbc, projectID := setupFeaturesTest(t)
	if _, err := SaveMemory(dbc, &Memory{
		ID: "pin-1", ProjectID: projectID, Category: "decision",
		What: "Key architecture decision", Why: "important", Learned: "keep", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	if err := PinMemory(dbc, projectID, "pin-1"); err != nil {
		t.Fatalf("pin failed: %v", err)
	}
	pinned, err := SearchPinnedMemories(dbc, projectID, 10)
	if err != nil {
		t.Fatalf("pinned search failed: %v", err)
	}
	if len(pinned) != 1 || pinned[0].ID != "pin-1" {
		t.Fatalf("expected pin-1 pinned, got %v", pinned)
	}

	if err = UnpinMemory(dbc, projectID, "pin-1"); err != nil {
		t.Fatalf("unpin failed: %v", err)
	}
	pinned, err = SearchPinnedMemories(dbc, projectID, 10)
	if err != nil {
		t.Fatalf("pinned search failed: %v", err)
	}
	if len(pinned) != 0 {
		t.Fatalf("expected no pinned after unpin, got %d", len(pinned))
	}

	if err := PinMemory(dbc, projectID, "does-not-exist"); err == nil {
		t.Fatal("expected error pinning a non-existent memory")
	}
}
