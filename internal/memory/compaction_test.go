package memory

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/svtech-code/sv-memory/internal/db"
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
	if regErr := db.RegisterProject(database, projectID, "Compact Proj", tempDir); regErr != nil {
		t.Fatalf("failed to register project: %v", regErr)
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
	if regErr := db.RegisterProject(database, projectID, "Compact Session Proj", tempDir); regErr != nil {
		t.Fatalf("failed to register project: %v", regErr)
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

	// The Auto-Boot bundle must also surface the synthesized decision so a
	// new session bootstraps from it, not just sv_mem_context lookups.
	bundle, err := GetAutoBootBundle(database, projectID)
	if err != nil {
		t.Fatalf("failed GetAutoBootBundle after compaction: %v", err)
	}
	if !strings.Contains(bundle, "**Key Architectural Decisions:**") {
		t.Errorf("expected bundle to include architectural decisions section after compaction, got:\n%s", bundle)
	}
	if !strings.Contains(bundle, "Use Bun exclusively") {
		t.Errorf("expected bundle to surface synthesized decision after compaction, got:\n%s", bundle)
	}
}

func TestCompactMemoriesSkipsSingleRowHighRevision(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_compact_singlerow.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-compact-single"
	if regErr := db.RegisterProject(database, projectID, "Compact Single Proj", tempDir); regErr != nil {
		t.Fatalf("failed to register project: %v", regErr)
	}

	// A single active row under a topic_key with a high revision count. Its
	// content was overwritten by each upsert, so there is no history to merge.
	// Compaction must NOT synthesize a near-duplicate twin (which previously
	// inflated revision_count on every run).
	_, err = database.Exec(`
		INSERT INTO memories (id, project_id, category, what, why, learned, where_path, topic_key, revision_count, created_at)
		VALUES ('s-1', ?, 'decision', 'Use Bun', 'Avoid npm', 'Bun is the package manager', 'package.json', 'decision/use-bun', 7, datetime('now'))
	`, projectID)
	if err != nil {
		t.Fatalf("failed inserting test memory: %v", err)
	}

	report, err := CompactMemories(database, projectID)
	if err != nil {
		t.Fatalf("failed CompactMemories: %v", err)
	}
	if report.ProcessedTopics != 0 {
		t.Errorf("expected 0 processed topics for a single high-revision row, got %d", report.ProcessedTopics)
	}
	if report.NewSynthesesCreated != 0 {
		t.Errorf("expected 0 syntheses for a single high-revision row, got %d", report.NewSynthesesCreated)
	}

	// The original memory must remain untouched (not soft-deleted).
	activeMems, err := SearchMemories(database, projectID, "", "", 10)
	if err != nil {
		t.Fatalf("failed searching active memories: %v", err)
	}
	if len(activeMems) != 1 || activeMems[0].ID != "s-1" {
		t.Fatalf("expected the single memory to survive intact, got %d memories", len(activeMems))
	}
	if activeMems[0].RevisionCount != 7 {
		t.Errorf("expected revision_count to stay 7, got %d", activeMems[0].RevisionCount)
	}
}

func TestCompactMemoriesIncrementalOnlyProcessesNewTopics(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_compact_incr.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-compact-incr"
	if regErr := db.RegisterProject(database, projectID, "Compact Incr Proj", tempDir); regErr != nil {
		t.Fatalf("failed to register project: %v", regErr)
	}

	// Deterministic control of the watermark: we explicitly set
	// last_compaction_at before each run so the test never depends on the
	// wall clock or the host timezone (SQLite datetime('now') is UTC while
	// Go time.Now() carries a local offset).
	setWatermark := func(wm string) {
		t.Helper()
		if _, execErr := database.Exec(
			"UPDATE projects SET last_compaction_at = ? WHERE id = ?", wm, projectID); execErr != nil {
			t.Fatalf("failed setting watermark %s: %v", wm, execErr)
		}
	}

	seed := func(id, tk, at string, rev int) {
		t.Helper()
		if _, execErr := database.Exec(`
			INSERT INTO memories (id, project_id, category, what, why, learned, topic_key, revision_count, created_at)
			VALUES (?, ?, 'architecture', 'Old title', 'Old why', 'Old learned', ?, ?, ?)
		`, id, projectID, tk, rev, at); execErr != nil {
			t.Fatalf("failed seeding %s: %v", id, execErr)
		}
	}

	// First run: watermark sits before all seeded rows, so both old topic
	// keys (2 rows each) are picked up.
	setWatermark("2026-01-01 00:00:00")
	seed("i-1", "architecture/old-a", "2026-01-02 10:00:00", 1)
	seed("i-2", "architecture/old-a", "2026-01-02 11:00:00", 2)
	seed("i-3", "architecture/old-b", "2026-01-02 10:00:00", 1)
	seed("i-4", "architecture/old-b", "2026-01-02 11:00:00", 2)

	report, err := CompactMemoriesIncremental(database, projectID)
	if err != nil {
		t.Fatalf("first incremental run failed: %v", err)
	}
	if report.ProcessedTopics != 2 {
		t.Fatalf("expected 2 processed topics on first run, got %d", report.ProcessedTopics)
	}

	// Second run: old topics were consolidated to a single row each, so
	// nothing qualifies regardless of the watermark. Reset the watermark to
	// a known value so the run is fully deterministic.
	setWatermark("2026-01-10 00:00:00")
	report, err = CompactMemoriesIncremental(database, projectID)
	if err != nil {
		t.Fatalf("second incremental run failed: %v", err)
	}
	if report.ProcessedTopics != 0 {
		t.Errorf("expected 0 processed topics on idle second run, got %d", report.ProcessedTopics)
	}

	// A brand-new topic key created after the (reset) watermark is picked up.
	setWatermark("2026-01-20 00:00:00")
	seed("i-5", "architecture/new-c", "2026-01-21 10:00:00", 1)
	seed("i-6", "architecture/new-c", "2026-01-21 11:00:00", 2)
	report, err = CompactMemoriesIncremental(database, projectID)
	if err != nil {
		t.Fatalf("third incremental run failed: %v", err)
	}
	if report.ProcessedTopics != 1 {
		t.Errorf("expected 1 new topic processed on third run, got %d", report.ProcessedTopics)
	}
}
