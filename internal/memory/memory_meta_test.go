package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/svtech-code/sv-memory/internal/db"
)

func TestNewID(t *testing.T) {
	if got := len(newID()); got != 16 {
		t.Errorf("newID() length = %d, want 16", got)
	}
	seen := make(map[string]bool, 100000)
	for i := 0; i < 100000; i++ {
		id := newID()
		if seen[id] {
			t.Fatalf("newID() collision detected: %s", id)
		}
		seen[id] = true
	}
}

// TestValidMemoryID covers the conservative id-safety rule used by the import
// paths: only alphanumerics plus '-' and '_' are allowed, with a length bound.
// Path separators and traversal segments must be rejected.
func TestValidMemoryID(t *testing.T) {
	valid := []string{"abc123", "a-b_c", "0123456789abcdef", "A1-b_9"}
	for _, id := range valid {
		if !validMemoryID(id) {
			t.Errorf("expected %q to be a valid memory id", id)
		}
	}

	invalid := []string{"", "../../x", "a/b", "a\\b", "a..b", ".", "..", "has space", "tab\t", "ñoño"}
	for _, id := range invalid {
		if validMemoryID(id) {
			t.Errorf("expected %q to be an invalid memory id", id)
		}
	}

	tooLong := strings.Repeat("a", 65)
	if validMemoryID(tooLong) {
		t.Error("expected an id longer than 64 chars to be rejected")
	}
}

func TestGetTimelineCompact(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-timeline-test")
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

	projectID := "proj-timeline"
	err = db.RegisterProject(database, projectID, "Timeline Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Fixed, strictly increasing creation times so the ordering assertions are
	// deterministic (SaveMemory honors an explicit CreatedAt on a new row).
	base := time.Date(2026, 8, 15, 10, 0, 0, 0, time.Local)
	ids := make(map[string]string, 4)
	for i, what := range []string{"first", "second", "third", "fourth"} {
		m, saveErr := SaveMemory(database, &Memory{
			ID:        "tl-" + what,
			ProjectID: projectID,
			Category:  "journal",
			What:      what,
			CreatedAt: base.Add(time.Duration(i) * time.Minute),
		})
		if saveErr != nil {
			t.Fatalf("save %s: %v", what, saveErr)
		}
		ids[what] = m.ID
	}

	// Central observation: 'second' sits between first and third/fourth.
	obs := ids["second"]

	// before=1 after=1: exactly first before, third after.
	prev, next, err := GetTimelineCompact(database, projectID, obs, 1, 1)
	if err != nil {
		t.Fatalf("GetTimelineCompact: %v", err)
	}
	if len(prev) != 1 || prev[0].ID != ids["first"] {
		t.Errorf("expected exactly first before, got %d items (%+v)", len(prev), prev)
	}
	if len(next) != 1 || next[0].ID != ids["third"] {
		t.Errorf("expected exactly third after, got %d items (%+v)", len(next), next)
	}

	// before=2 after=2: only 'first' precedes second; both third and fourth
	// follow. The central observation is never included.
	prev, next, err = GetTimelineCompact(database, projectID, obs, 2, 2)
	if err != nil {
		t.Fatalf("GetTimelineCompact wide: %v", err)
	}
	if len(prev) != 1 || prev[0].ID != ids["first"] {
		t.Errorf("expected 1 item before, got %d (%+v)", len(prev), prev)
	}
	if len(next) != 2 || next[0].ID != ids["third"] || next[1].ID != ids["fourth"] {
		t.Errorf("expected third, fourth after, got %d (%+v)", len(next), next)
	}
	for _, item := range append(prev, next...) {
		if item.ID == obs {
			t.Errorf("central observation leaked into the timeline")
		}
	}

	// Soft-deleted memories must not appear in the timeline.
	if err = DeleteMemory(database, projectID, ids["first"], false); err != nil {
		t.Fatalf("soft-delete first: %v", err)
	}
	prev, _, err = GetTimelineCompact(database, projectID, obs, 5, 0)
	if err != nil {
		t.Fatalf("GetTimelineCompact after delete: %v", err)
	}
	if len(prev) != 0 {
		t.Errorf("expected no items before after soft-delete, got %d", len(prev))
	}

	// Unknown observation is an error.
	if _, _, err = GetTimelineCompact(database, projectID, "does-not-exist", 2, 2); err == nil {
		t.Error("expected error for unknown observation id, got nil")
	}
}

func TestCompareMemoriesTruncatesReason(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-compare-reason")
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

	m1, err := SaveMemory(database, &Memory{ProjectID: projectID, Category: "decision", What: "A", Why: "x", Learned: "l"})
	if err != nil {
		t.Fatalf("SaveMemory error: %v", err)
	}
	m2, err := SaveMemory(database, &Memory{ProjectID: projectID, Category: "decision", What: "B", Why: "y", Learned: "l"})
	if err != nil {
		t.Fatalf("SaveMemory error: %v", err)
	}

	longReason := strings.Repeat("b", 300)
	if _, err = database.Exec(
		"INSERT INTO memory_relations (id, project_id, source_id, target_id, relation_type, reason, status) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"rel-1", projectID, m1.ID, m2.ID, "relates_to", longReason, "judged",
	); err != nil {
		t.Fatalf("insert relation: %v", err)
	}

	out, err := CompareMemories(database, projectID, m1.ID, m2.ID)
	if err != nil {
		t.Fatalf("CompareMemories: %v", err)
	}
	if strings.Contains(out, longReason) {
		t.Fatalf("expected reason to be truncated in CompareMemories output, but full reason was present")
	}
}
