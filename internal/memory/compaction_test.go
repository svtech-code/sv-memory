package memory

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/svtech/sv-memory/internal/db"
)

func TestCompactMemories(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_compact.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-compact"
	if err := db.RegisterProject(database, projectID, "Compact Proj", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Case 1: Multiple entries inserted for topic_key "architecture/database-wal"
	_, err = database.Exec(`
		INSERT INTO memories (id, project_id, category, what, why, learned, topic_key, revision_count, created_at)
		VALUES
		('c-1', ?, 'architecture', 'Initial SQLite Setup', 'Store memories locally', 'SQLite is fast', 'architecture/database-wal', 1, datetime('now', '-2 hours')),
		('c-2', ?, 'architecture', 'SQLite WAL Mode', 'Avoid database locks', 'WAL mode enables concurrent reads', 'architecture/database-wal', 2, datetime('now', '-1 hour')),
		('c-3', ?, 'architecture', 'Optimized SQLite WAL & PRAGMAs', 'Maximize throughput', 'Set cache_size -20000 and synchronous NORMAL', 'architecture/database-wal', 3, datetime('now'));
	`, projectID, projectID, projectID)
	if err != nil {
		t.Fatalf("failed inserting test memories: %v", err)
	}

	// Run compaction
	report, err := CompactMemories(database, projectID)
	if err != nil {
		t.Fatalf("failed CompactMemories: %v", err)
	}

	if report.ProcessedTopics != 1 {
		t.Errorf("expected 1 processed topic, got %d", report.ProcessedTopics)
	}
	if report.MemoriesCompacted != 3 {
		t.Errorf("expected 3 memories compacted, got %d", report.MemoriesCompacted)
	}
	if report.NewSynthesesCreated != 1 {
		t.Errorf("expected 1 synthesis created, got %d", report.NewSynthesesCreated)
	}

	// Verify older entries are soft-deleted and 1 active consolidated entry exists
	activeMems, err := SearchMemories(database, projectID, "", "", 10)
	if err != nil {
		t.Fatalf("failed searching active memories post compaction: %v", err)
	}
	if len(activeMems) != 1 {
		t.Fatalf("expected 1 active memory post compaction, got %d", len(activeMems))
	}

	consolidated := activeMems[0]
	if consolidated.TopicKey != "architecture/database-wal" {
		t.Errorf("expected topic key architecture/database-wal, got %s", consolidated.TopicKey)
	}
	if consolidated.RevisionCount < 4 {
		t.Errorf("expected incremented revision count >= 4, got %d", consolidated.RevisionCount)
	}
}

func TestCompactMemoriesPreservesSessionID(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_compact_session.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-compact-session"
	if err := db.RegisterProject(database, projectID, "Compact Session Proj", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Two entries under the same topic_key; only the older one carries a
	// session_id. After compaction the synthesis must keep that session
	// association so Auto-Boot / sv_mem_context can still find it.
	_, err = database.Exec(`
		INSERT INTO memories (id, project_id, category, what, why, learned, topic_key, session_id, revision_count, created_at)
		VALUES
		('s-1', ?, 'decision', 'Use Bun', 'Avoid npm', 'Bun is the package manager', 'decision/use-bun', 'sess-abc', 1, datetime('now', '-2 hours')),
		('s-2', ?, 'decision', 'Use Bun exclusively', 'No npm allowed', 'Always use bun', 'decision/use-bun', NULL, 2, datetime('now'));
	`, projectID, projectID)
	if err != nil {
		t.Fatalf("failed inserting test memories: %v", err)
	}

	report, err := CompactMemories(database, projectID)
	if err != nil {
		t.Fatalf("failed CompactMemories: %v", err)
	}
	if report.NewSynthesesCreated != 1 {
		t.Fatalf("expected 1 synthesis created, got %d", report.NewSynthesesCreated)
	}

	// The synthesis must be found by session context (the Auto-Boot path).
	bySession, err := SearchMemoriesBySessionCompact(database, projectID, "sess-abc", 10)
	if err != nil {
		t.Fatalf("failed searching by session: %v", err)
	}
	if len(bySession) != 1 {
		t.Fatalf("expected 1 memory recoverable via session context after compaction, got %d", len(bySession))
	}
	if bySession[0].What != "Use Bun exclusively" {
		t.Errorf("expected latest 'what' in synthesis, got %s", bySession[0].What)
	}

	// Verify the synthesis actually persisted the session_id column.
	var sessID sql.NullString
	err = database.QueryRow("SELECT session_id FROM memories WHERE id = ?", bySession[0].ID).Scan(&sessID)
	if err != nil {
		t.Fatalf("failed reading synthesis session_id: %v", err)
	}
	if !sessID.Valid || sessID.String != "sess-abc" {
		t.Errorf("expected synthesis session_id sess-abc, got %v", sessID)
	}
}
