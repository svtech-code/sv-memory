package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/svtech-code/sv-memory/internal/db"
)

func TestSaveMemoryTopicUpsertPreservesCreatedAt(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-topic-upsert")
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

	if err = db.RegisterProject(database, "proj-topic", "Topic", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// First save under the topic key.
	first, err := SaveMemory(database, &Memory{
		ProjectID: "proj-topic", Category: "decision",
		What: "Decision A", Why: "why", Learned: "learned", TopicKey: "decision/topic-a",
	})
	if err != nil {
		t.Fatalf("first save failed: %v", err)
	}
	origCreatedAt := first.CreatedAt

	// A topic-key revision must not move created_at forward.
	time.Sleep(5 * time.Millisecond)
	second, err := SaveMemory(database, &Memory{
		ProjectID: "proj-topic", Category: "decision",
		What: "Decision A revised", Why: "why2", Learned: "learned2", TopicKey: "decision/topic-a",
	})
	if err != nil {
		t.Fatalf("second save failed: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("expected topic-key upsert to reuse id %s, got %s", first.ID, second.ID)
	}
	if !second.CreatedAt.Equal(origCreatedAt) {
		t.Errorf("topic-key revision moved created_at: got %v, want %v", second.CreatedAt, origCreatedAt)
	}
	if second.RevisionCount != 2 {
		t.Errorf("expected revision_count 2, got %d", second.RevisionCount)
	}

	// A brand-new memory must not appear stale: last_seen_at is set on insert,
	// so ReviewMemories sees a recent timestamp instead of the zero-time
	// sentinel that used to read back as ~738000 days.
	third, err := SaveMemory(database, &Memory{
		ProjectID: "proj-topic", Category: "bugfix",
		What: "Brand new", Why: "why", Learned: "learned",
	})
	if err != nil {
		t.Fatalf("third save failed: %v", err)
	}
	if third.LastSeenAt.IsZero() {
		t.Error("expected last_seen_at to be set on new insert")
	}
	items, err := ReviewMemories(database, "proj-topic", 50)
	if err != nil {
		t.Fatalf("ReviewMemories failed: %v", err)
	}
	for _, it := range items {
		if it.Memory.ID == third.ID {
			if it.LastSeenDays > 1 {
				t.Errorf("new memory should not appear stale (LastSeenDays=%d)", it.LastSeenDays)
			}
			if strings.Contains(it.Reason, "stale") {
				t.Errorf("new memory flagged stale: %s", it.Reason)
			}
		}
	}
}

func TestSaveJudgmentReplacesDuplicate(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-judge-dedup")
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

	if err = db.RegisterProject(database, "proj-judge", "Judge", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Two memories to relate.
	a, err := SaveMemory(database, &Memory{
		ProjectID: "proj-judge", Category: "decision",
		What: "Use Postgres", Why: "why", Learned: "learned",
	})
	if err != nil {
		t.Fatalf("save memory a: %v", err)
	}
	b, err := SaveMemory(database, &Memory{
		ProjectID: "proj-judge", Category: "decision",
		What: "Use MySQL", Why: "why", Learned: "learned",
	})
	if err != nil {
		t.Fatalf("save memory b: %v", err)
	}

	// Re-judging the same pair must replace the previous relation, not
	// accumulate duplicates. Manual judgments are recorded as 'judged' so they
	// neither inflate the pending-conflict count nor get replaced by a later
	// semantic re-scan (which only touches status='pending' rows).
	var lastRel *MemoryRelation
	for i := 0; i < 2; i++ {
		lastRel, err = SaveJudgment(database, "proj-judge", a.ID, b.ID, "conflicts_with", "changed opinion", "tester")
		if err != nil {
			t.Fatalf("SaveJudgment %d: %v", i, err)
		}
	}

	var count int
	if err = database.QueryRow(
		"SELECT COUNT(*) FROM memory_relations WHERE project_id = ? AND source_id = ? AND target_id = ? AND relation_type = 'conflicts_with'",
		"proj-judge", a.ID, b.ID).Scan(&count); err != nil {
		t.Fatalf("count relations: %v", err)
	}
	if count != 1 {
		t.Errorf("expected exactly 1 relation after re-judging, got %d", count)
	}
	if lastRel.Status != "judged" {
		t.Errorf("expected returned judgment status 'judged', got %q", lastRel.Status)
	}

	var status string
	if err = database.QueryRow(
		"SELECT status FROM memory_relations WHERE project_id = ? AND source_id = ? AND target_id = ? AND relation_type = 'conflicts_with'",
		"proj-judge", a.ID, b.ID).Scan(&status); err != nil {
		t.Fatalf("query persisted status: %v", err)
	}
	if status != "judged" {
		t.Errorf("expected persisted status 'judged', got %q", status)
	}
}

func TestMemoryCRUDAndFTS(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-memory-test")
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

	projectID := "proj-test"
	err = db.RegisterProject(database, projectID, "Test Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// 1. Test SaveMemory
	mem1 := &Memory{
		ID:        "mem-1",
		ProjectID: projectID,
		Category:  "bugfix",
		What:      "Fixed a memory leak in connection pooling",
		Why:       "The connections were not being closed inside the defer block",
		WherePath: "internal/db/db.go",
		Learned:   "Always verify connection close defer calls exist",
		CreatedAt: time.Now(),
	}

	_, err = SaveMemory(database, mem1)
	if err != nil {
		t.Fatalf("failed to save memory: %v", err)
	}

	// 2. Test SearchMemories with exact category and search term
	results, err := SearchMemories(database, projectID, "leak", "", 0)
	if err != nil {
		t.Fatalf("failed searching memories: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(results))
	}

	if results[0].ID != mem1.ID {
		t.Errorf("expected found memory ID to match, expected %s, got %s", mem1.ID, results[0].ID)
	}

	// Test SearchMemories with category filter
	resultsCat, err := SearchMemories(database, projectID, "", "bugfix", 0)
	if err != nil {
		t.Fatalf("failed searching memories with category filter: %v", err)
	}
	if len(resultsCat) != 1 {
		t.Errorf("expected 1 result with category filter, got %d", len(resultsCat))
	}

	// Test SearchMemories with wrong keyword
	resultsEmpty, err := SearchMemories(database, projectID, "nonexistentword", "", 0)
	if err != nil {
		t.Fatalf("failed searching: %v", err)
	}
	if len(resultsEmpty) != 0 {
		t.Errorf("expected 0 search results, got %d", len(resultsEmpty))
	}
}

func TestDeleteMemoryHard(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-harddelete-test")
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

	projectID := "proj-harddel"
	err = db.RegisterProject(database, projectID, "HardDel Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	a, err := SaveMemory(database, &Memory{
		ID: "hd-a", ProjectID: projectID, Category: "decision",
		What: "hard a", Why: "why", Learned: "l", CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("save a: %v", err)
	}
	b, err := SaveMemory(database, &Memory{
		ID: "hd-b", ProjectID: projectID, Category: "decision",
		What: "hard b", Why: "why", Learned: "l", CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("save b: %v", err)
	}

	// A relation points at the memory being deleted; it must cascade away.
	if _, err = SaveJudgment(database, projectID, a.ID, b.ID, "relates_to", "pair", "tester"); err != nil {
		t.Fatalf("SaveJudgment: %v", err)
	}

	// Hard delete removes the row entirely (not a soft delete marker).
	if err = DeleteMemory(database, projectID, a.ID, true); err != nil {
		t.Fatalf("hard delete: %v", err)
	}

	var count int
	err = database.QueryRow("SELECT COUNT(*) FROM memories WHERE project_id = ? AND id = ?", projectID, a.ID).Scan(&count)
	if err != nil {
		t.Fatalf("query deleted row: %v", err)
	}
	if count != 0 {
		t.Errorf("expected hard-deleted memory row gone, got %d", count)
	}
	err = database.QueryRow(
		"SELECT COUNT(*) FROM memory_relations WHERE project_id = ? AND (source_id = ? OR target_id = ?)",
		projectID, a.ID, a.ID).Scan(&count)
	if err != nil {
		t.Fatalf("query relations: %v", err)
	}
	if count != 0 {
		t.Errorf("expected relations to cascade on hard delete, got %d", count)
	}

	// Deleting a nonexistent id reports an error.
	if err = DeleteMemory(database, projectID, "no-such", true); err == nil {
		t.Error("expected error deleting unknown id, got nil")
	}
}
