package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/svtech-code/sv-memory/internal/db"
)

func TestExportImportJSON(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-export-test")
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

	projectID := "proj-export"
	err = db.RegisterProject(database, projectID, "Export Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Save some memories
	mem1 := &Memory{ID: "e-1", ProjectID: projectID, Category: "architecture", What: "Arch 1", Why: "why1", Learned: "l1", CreatedAt: time.Now()}
	mem2 := &Memory{ID: "e-2", ProjectID: projectID, Category: "decision", What: "Dec 2", Why: "why2", Learned: "l2", CreatedAt: time.Now()}
	if _, saveErr := SaveMemory(database, mem1); saveErr != nil {
		t.Fatalf("failed saving mem1: %v", saveErr)
	}
	if _, saveErr := SaveMemory(database, mem2); saveErr != nil {
		t.Fatalf("failed saving mem2: %v", saveErr)
	}

	// Export to JSON
	exportPath := filepath.Join(tempDir, "export.json")
	n, err := ExportJSON(database, projectID, exportPath)
	if err != nil {
		t.Fatalf("ExportJSON failed: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 exported memories, got %d", n)
	}

	// Delete from DB
	if _, execErr := database.Exec("DELETE FROM memories WHERE project_id = ?", projectID); execErr != nil {
		t.Fatalf("failed clearing memories: %v", execErr)
	}

	var count int
	database.QueryRow("SELECT COUNT(*) FROM memories WHERE project_id = ?", projectID).Scan(&count)
	if count != 0 {
		t.Fatal("expected empty memories table")
	}

	// Import from JSON
	n, err = ImportJSON(database, projectID, exportPath)
	if err != nil {
		t.Fatalf("ImportJSON failed: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 imported memories, got %d", n)
	}

	// Verify
	database.QueryRow("SELECT COUNT(*) FROM memories WHERE project_id = ?", projectID).Scan(&count)
	if count != 2 {
		t.Errorf("expected 2 memories after import, got %d", count)
	}
}

func TestGitSync(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-sync-test")
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

	projectID := "proj-sync-test"
	err = db.RegisterProject(database, projectID, "Sync Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Save a few memories in SQLite
	mems := []*Memory{
		{
			ID:        "m-1",
			ProjectID: projectID,
			Category:  "architecture",
			What:      "Use clean architecture in Golang service",
			Why:       "Decouple controllers from DB schema",
			WherePath: "internal/service",
			Learned:   "Inject repository interface inside use-case controllers",
			CreatedAt: time.Now().Truncate(time.Second),
		},
		{
			ID:        "m-2",
			ProjectID: projectID,
			Category:  "standard",
			What:      "Linter configurations",
			Why:       "Enforce consistent coding style across team",
			WherePath: ".golangci.yml",
			Learned:   "Run golangci-lint check on commit hook",
			CreatedAt: time.Now().Truncate(time.Second),
		},
	}

	for _, m := range mems {
		if _, saveErr := SaveMemory(database, m); saveErr != nil {
			t.Fatalf("failed saving memory: %v", saveErr)
		}
	}

	// 1. Sync to Git (creates JSON file)
	err = SyncToGit(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("failed SyncToGit: %v", err)
	}

	syncFilePath := filepath.Join(tempDir, ".sv-memory", "memories.json")
	if _, statErr := os.Stat(syncFilePath); os.IsNotExist(statErr) {
		t.Fatal("expected .sv-memory/memories.json file to be created")
	}

	// Check JSON file content
	data, err := os.ReadFile(syncFilePath)
	if err != nil {
		t.Fatalf("failed reading sync file: %v", err)
	}

	var readMems []*Memory
	if unmarshalErr := json.Unmarshal(data, &readMems); unmarshalErr != nil {
		t.Fatalf("failed to unmarshal JSON: %v", unmarshalErr)
	}

	if len(readMems) != 2 {
		t.Errorf("expected 2 memories in JSON, got %d", len(readMems))
	}

	// 2. Sync from Git: delete from database first, then pull
	_, err = database.Exec("DELETE FROM memories")
	if err != nil {
		t.Fatalf("failed clearing memories table: %v", err)
	}

	var count int
	err = database.QueryRow("SELECT COUNT(*) FROM memories").Scan(&count)
	if err != nil {
		t.Fatalf("query count err: %v", err)
	}
	if count != 0 {
		t.Fatal("expected database memories table to be empty")
	}

	// Pull from Git JSON
	err = SyncFromGit(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("failed SyncFromGit: %v", err)
	}

	// Verify database is populated again
	err = database.QueryRow("SELECT COUNT(*) FROM memories").Scan(&count)
	if err != nil {
		t.Fatalf("query count err: %v", err)
	}
	if count != 2 {
		t.Errorf("expected database to have 2 memories after sync, got %d", count)
	}
}

func TestExportObsidian(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-obsidian-export-test")
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

	projectID := "proj-export"
	err = db.RegisterProject(database, projectID, "Export Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// 1. Save a memory
	mem1 := &Memory{
		ID:        "mem-1234",
		ProjectID: projectID,
		Category:  "architecture",
		What:      "Use clean architecture in Go",
		Why:       "Decoupling controllers",
		WherePath: "main.go",
		Learned:   "Injecting interface controllers",
		CreatedAt: time.Now(),
	}
	if _, saveErr := SaveMemory(database, mem1); saveErr != nil {
		t.Fatalf("failed saving memory: %v", saveErr)
	}

	// 2. Insert mock structural graph nodes
	_, err = database.Exec(`
		INSERT INTO graph_nodes (id, project_id, node_type, label, path, metadata) VALUES 
		('main.go', ?, 'file', 'main.go', 'main.go', '{"language":"go"}'),
		('main.go:callerFunc', ?, 'function', 'callerFunc', 'main.go', '{"line":3}'),
		('main.go:helperFunc', ?, 'function', 'helperFunc', 'main.go', '{"line":7}'),
		('pkg:lodash', ?, 'package', 'lodash', 'lodash', NULL),
		('mem-1234', ?, 'concept', 'Use clean architecture in Go', 'main.go', NULL)
	`, projectID, projectID, projectID, projectID, projectID)
	if err != nil {
		t.Fatalf("failed to insert graph nodes: %v", err)
	}

	// 3. Insert mock structural graph edges
	_, err = database.Exec(`
		INSERT INTO graph_edges (id, project_id, source_id, target_id, relation_type, confidence, source_location) VALUES 
		('edge-1', ?, 'main.go', 'pkg:lodash', 'imports', 'EXTRACTED', NULL),
		('edge-2', ?, 'main.go:callerFunc', 'main.go:helperFunc', 'calls', 'INFERRED', 'L4'),
		('edge-3', ?, 'mem-1234', 'main.go', 'rationale_for', 'EXTRACTED', NULL)
	`, projectID, projectID, projectID)
	if err != nil {
		t.Fatalf("failed to insert graph edges: %v", err)
	}

	// 4. Run ExportObsidian
	outputDir := "my-vault"
	err = ExportObsidian(database, projectID, filepath.Join(tempDir, outputDir))
	if err != nil {
		t.Fatalf("ExportObsidian failed: %v", err)
	}

	vaultPath := filepath.Join(tempDir, outputDir)

	// 5. Verify memory note exists
	memFilePath := filepath.Join(vaultPath, "mem-1234.md")
	if _, statErr := os.Stat(memFilePath); os.IsNotExist(statErr) {
		t.Fatalf("expected memory note to exist at %s", memFilePath)
	}
	memContent, err := os.ReadFile(memFilePath)
	if err != nil {
		t.Fatalf("failed reading memory note: %v", err)
	}
	if !strings.Contains(string(memContent), "[[code/main.go|main.go]]") {
		t.Errorf("expected memory note to contain link to code node, got:\n%s", string(memContent))
	}

	// 6. Verify code note exists
	codeFilePath := filepath.Join(vaultPath, "code", "main.go.md")
	if _, statErr := os.Stat(codeFilePath); os.IsNotExist(statErr) {
		t.Fatalf("expected code note to exist at %s", codeFilePath)
	}
	codeContent, err := os.ReadFile(codeFilePath)
	if err != nil {
		t.Fatalf("failed reading code note: %v", err)
	}
	if !strings.Contains(string(codeContent), "[[../code/packages/pkg_lodash|lodash]]") {
		t.Errorf("expected code note to contain package link, got:\n%s", string(codeContent))
	}
	if !strings.Contains(string(codeContent), "[[../code/main.go#helperFunc|helperFunc]]") {
		t.Errorf("expected code note to contain symbol call link, got:\n%s", string(codeContent))
	}
	if !strings.Contains(string(codeContent), "[[../mem-1234|mem-1234]]") {
		t.Errorf("expected code note to contain associated memory backlink, got:\n%s", string(codeContent))
	}

	// 7. Verify package note exists
	pkgFilePath := filepath.Join(vaultPath, "code", "packages", "pkg_lodash.md")
	if _, statErr := os.Stat(pkgFilePath); os.IsNotExist(statErr) {
		t.Fatalf("expected package note to exist at %s", pkgFilePath)
	}
	pkgContent, err := os.ReadFile(pkgFilePath)
	if err != nil {
		t.Fatalf("failed reading package note: %v", err)
	}
	if !strings.Contains(string(pkgContent), "[[../../code/main.go|main.go]]") {
		t.Errorf("expected package note to contain dependents link, got:\n%s", string(pkgContent))
	}
}

func TestSyncToGitIncremental(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_incremental.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-incremental"
	err = db.RegisterProject(database, projectID, "Incremental Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Baseline: one memory, first (full) sync.
	m1 := &Memory{ID: "inc-1", ProjectID: projectID, Category: "standard", What: "Baseline rule", Why: "Stable", TopicKey: "standard/baseline", CreatedAt: time.Now().Add(-time.Hour)}
	_, err = SaveMemory(database, m1)
	if err != nil {
		t.Fatalf("failed to save baseline: %v", err)
	}
	err = SyncToGit(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("failed baseline SyncToGit: %v", err)
	}

	chunkDir := filepath.Join(tempDir, ".sv-memory", "chunks")
	baseline, err := os.ReadDir(chunkDir)
	if err != nil {
		t.Fatalf("failed reading chunks: %v", err)
	}
	if len(baseline) != 1 {
		t.Fatalf("expected 1 chunk after baseline sync, got %d", len(baseline))
	}
	baseChunk, err := os.ReadFile(filepath.Join(chunkDir, "inc-1.json"))
	if err != nil {
		t.Fatalf("failed reading baseline chunk: %v", err)
	}

	// Update the same memory via its topic_key: memory count is unchanged, so
	// the incremental path runs and only the changed chunk is rewritten.
	_, err = SaveMemory(database, &Memory{
		ProjectID: projectID, Category: "standard", What: "Baseline rule v2", Why: "Evolving",
		TopicKey: "standard/baseline", CreatedAt: time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("failed to update baseline via topic_key: %v", err)
	}
	err = SyncToGit(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("failed incremental SyncToGit: %v", err)
	}

	after, err := os.ReadDir(chunkDir)
	if err != nil {
		t.Fatalf("failed reading chunks after incremental: %v", err)
	}
	if len(after) != 1 {
		t.Fatalf("expected 1 chunk after incremental sync, got %d", len(after))
	}

	// The chunk content must reflect the updated title.
	chunk1, err := os.ReadFile(filepath.Join(chunkDir, "inc-1.json"))
	if err != nil {
		t.Fatalf("failed reading inc-1 chunk: %v", err)
	}
	if !strings.Contains(string(chunk1), "Baseline rule v2") {
		t.Errorf("expected chunk to be rewritten with new title, got: %s", chunk1)
	}
	if string(chunk1) == string(baseChunk) {
		t.Errorf("expected chunk content to change after update")
	}

	// memories.json is only rewritten periodically; a forced flush guarantees
	// it reflects the full updated set.
	err = SyncToGitForceFull(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("failed SyncToGitForceFull: %v", err)
	}
	syncData, err := os.ReadFile(filepath.Join(tempDir, ".sv-memory", "memories.json"))
	if err != nil {
		t.Fatalf("failed reading memories.json: %v", err)
	}
	var all []*Memory
	err = json.Unmarshal(syncData, &all)
	if err != nil {
		t.Fatalf("failed to unmarshal memories.json: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("expected memories.json to contain 1 memory, got %d", len(all))
	}
	if all[0].What != "Baseline rule v2" {
		t.Errorf("expected updated title in memories.json, got %s", all[0].What)
	}
}

func TestSyncToGitRemovesOrphanChunks(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_orphans.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-orphans"
	err = db.RegisterProject(database, projectID, "Orphans Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	for _, id := range []string{"or-1", "or-2"} {
		_, err = SaveMemory(database, &Memory{ID: id, ProjectID: projectID, Category: "standard", What: "Rule " + id, CreatedAt: time.Now()})
		if err != nil {
			t.Fatalf("failed to save %s: %v", id, err)
		}
	}
	err = SyncToGit(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("failed baseline SyncToGit: %v", err)
	}

	// Soft-delete one memory: its chunk must be removed on the next sync even
	// though the underlying row count (with deleted_at) is unchanged.
	err = DeleteMemory(database, projectID, "or-1", false)
	if err != nil {
		t.Fatalf("failed to soft-delete: %v", err)
	}
	err = SyncToGit(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("failed SyncToGit after delete: %v", err)
	}

	chunkDir := filepath.Join(tempDir, ".sv-memory", "chunks")
	entries, err := os.ReadDir(chunkDir)
	if err != nil {
		t.Fatalf("failed reading chunks: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 remaining chunk after soft-delete, got %d", len(entries))
	}
	if entries[0].Name() != "or-2.json" {
		t.Errorf("expected only or-2.json to remain, got %s", entries[0].Name())
	}
}

func TestSyncToGitPeriodicJSON(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_periodic.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-periodic"
	err = db.RegisterProject(database, projectID, "Periodic Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	syncFile := filepath.Join(tempDir, ".sv-memory", "memories.json")

	// First sync writes memories.json (baseline).
	_, err = SaveMemory(database, &Memory{ID: "per-1", ProjectID: projectID, Category: "standard", What: "R1", CreatedAt: time.Now()})
	if err != nil {
		t.Fatalf("failed to save: %v", err)
	}
	err = SyncToGit(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("failed baseline SyncToGit: %v", err)
	}
	baseMod, err := os.Stat(syncFile)
	if err != nil {
		t.Fatalf("memories.json not written on first sync: %v", err)
	}

	// Subsequent incremental syncs must NOT rewrite memories.json until the
	// jsonSyncInterval boundary (10). Each sync adds a new memory so the
	// chunk writes happen but the monolithic file stays untouched.
	for i := 2; i <= 8; i++ {
		_, err = SaveMemory(database, &Memory{ID: fmt.Sprintf("per-%d", i), ProjectID: projectID, Category: "standard", What: "R" + fmt.Sprint(i), CreatedAt: time.Now()})
		if err != nil {
			t.Fatalf("failed to save per-%d: %v", i, err)
		}
		err = SyncToGit(database, projectID, tempDir)
		if err != nil {
			t.Fatalf("failed SyncToGit at %d: %v", i, err)
		}
	}
	afterMod, err := os.Stat(syncFile)
	if err != nil {
		t.Fatalf("memories.json missing after incremental syncs: %v", err)
	}
	if !afterMod.ModTime().Equal(baseMod.ModTime()) {
		t.Errorf("memories.json was rewritten during incremental syncs before the interval boundary")
	}

	// A forced flush always rewrites memories.json.
	err = SyncToGitForceFull(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("failed SyncToGitForceFull: %v", err)
	}
	forcedMod, err := os.Stat(syncFile)
	if err != nil {
		t.Fatalf("memories.json missing after forced flush: %v", err)
	}
	if forcedMod.ModTime().Equal(afterMod.ModTime()) {
		t.Errorf("expected SyncToGitForceFull to rewrite memories.json")
	}

	// The forced full write must contain the full set.
	data, err := os.ReadFile(syncFile)
	if err != nil {
		t.Fatalf("failed reading memories.json: %v", err)
	}
	var all []*Memory
	err = json.Unmarshal(data, &all)
	if err != nil {
		t.Fatalf("failed to unmarshal memories.json: %v", err)
	}
	if len(all) != 8 {
		t.Errorf("expected 8 memories in forced memories.json, got %d", len(all))
	}
}

// TestSyncFromGitSkipsUnparseableChunks verifies that a chunk left with git
// merge conflict markers (same-ID concurrent edit in two clones) no longer
// aborts the whole import: the unparseable chunk is skipped with a warning and
// the remaining valid chunks still arrive.
func TestSyncFromGitSkipsUnparseableChunks(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_skip.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-skip"
	err = db.RegisterProject(database, projectID, "Skip Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	chunkDir := filepath.Join(tempDir, ".sv-memory", "chunks")
	if err = os.MkdirAll(chunkDir, 0755); err != nil {
		t.Fatalf("failed to create chunks dir: %v", err)
	}

	// One healthy chunk + one chunk containing git conflict markers.
	good := &Memory{ID: "ok-1", ProjectID: projectID, Category: "decision", What: "Good", Why: "fine", Learned: "fine", CreatedAt: time.Now()}
	data, err := json.Marshal(good)
	if err != nil {
		t.Fatalf("failed to marshal good chunk: %v", err)
	}
	if err = os.WriteFile(filepath.Join(chunkDir, "ok-1.json"), data, 0644); err != nil {
		t.Fatalf("failed to write good chunk: %v", err)
	}
	conflicted := "<<<<<<< HEAD\n{\"id\":\"bad-1\"}\n=======\n{\"id\":\"bad-1\",\"what\":\"other\"}\n>>>>>>> remote\n"
	if err = os.WriteFile(filepath.Join(chunkDir, "bad-1.json"), []byte(conflicted), 0644); err != nil {
		t.Fatalf("failed to write conflicted chunk: %v", err)
	}

	err = SyncFromGit(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("SyncFromGit should not fail on an unparseable chunk, got: %v", err)
	}

	var count int
	if err = database.QueryRow("SELECT COUNT(*) FROM memories WHERE project_id = ?", projectID).Scan(&count); err != nil {
		t.Fatalf("query count err: %v", err)
	}
	if count != 1 {
		t.Errorf("expected the healthy chunk to import (1 memory), got %d", count)
	}
}

// TestSyncFromGitWarnsOnLastWriterWins verifies that importing a chunk that
// would overwrite a newer local edit (revision rollback) emits a divergence
// warning instead of silently losing the local change.
func TestSyncFromGitWarnsOnLastWriterWins(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_lww.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-lww"
	err = db.RegisterProject(database, projectID, "LWW Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Local edit is newer (revision 5); the git chunk is older (revision 2).
	local := &Memory{ID: "lww-1", ProjectID: projectID, Category: "decision", What: "Local v5", Why: "local", Learned: "local", RevisionCount: 5, CreatedAt: time.Now()}
	_, err = SaveMemory(database, local)
	if err != nil {
		t.Fatalf("failed to save local: %v", err)
	}

	remote := &Memory{ID: "lww-1", ProjectID: projectID, Category: "decision", What: "Remote v2", Why: "remote", Learned: "remote", RevisionCount: 2, CreatedAt: time.Now()}
	data, err := json.Marshal(remote)
	if err != nil {
		t.Fatalf("failed to marshal remote chunk: %v", err)
	}
	chunkDir := filepath.Join(tempDir, ".sv-memory", "chunks")
	if err = os.MkdirAll(chunkDir, 0755); err != nil {
		t.Fatalf("failed to create chunks dir: %v", err)
	}
	if err = os.WriteFile(filepath.Join(chunkDir, "lww-1.json"), data, 0644); err != nil {
		t.Fatalf("failed to write remote chunk: %v", err)
	}

	// Capture stderr to assert the divergence warning is emitted.
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	err = SyncFromGit(database, projectID, tempDir)
	w.Close()
	os.Stderr = old
	if err != nil {
		t.Fatalf("SyncFromGit failed: %v", err)
	}
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])
	if !strings.Contains(output, "overwrites a newer local edit") {
		t.Errorf("expected a last-writer-wins divergence warning on stderr, got: %q", output)
	}

	// The imported row reflects the git chunk (last-writer-wins).
	var what string
	if err = database.QueryRow("SELECT what FROM memories WHERE project_id = ? AND id = 'lww-1'", projectID).Scan(&what); err != nil {
		t.Fatalf("query err: %v", err)
	}
	if what != "Remote v2" {
		t.Errorf("expected last-writer-wins to apply the git chunk, got %q", what)
	}
}

// TestSyncFromGitRedactsSecretsInChunks verifies that a team-shared chunk
// containing a secret in a free-text field is redacted on import, so raw
// credentials never enter the DB through the git-sync path.
func TestSyncFromGitRedactsSecretsInChunks(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_redact.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-redact"
	err = db.RegisterProject(database, projectID, "Redact Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	secret := "sk-proj-RtY8kLs9NpQ2xW7vZ4mC6jH1gB5eA0dFqX3nM2pL9kR8tY7wQ"
	chunk := &Memory{ID: "red-1", ProjectID: projectID, Category: "journal",
		What: "deploy uses " + secret, Why: "token=" + secret, Learned: secret, CreatedAt: time.Now()}
	data, err := json.Marshal(chunk)
	if err != nil {
		t.Fatalf("failed to marshal chunk: %v", err)
	}
	chunkDir := filepath.Join(tempDir, ".sv-memory", "chunks")
	if err = os.MkdirAll(chunkDir, 0755); err != nil {
		t.Fatalf("failed to create chunks dir: %v", err)
	}
	if err = os.WriteFile(filepath.Join(chunkDir, "red-1.json"), data, 0644); err != nil {
		t.Fatalf("failed to write chunk: %v", err)
	}

	if err = SyncFromGit(database, projectID, tempDir); err != nil {
		t.Fatalf("SyncFromGit failed: %v", err)
	}

	var what, why, learned string
	err = database.QueryRow("SELECT what, why, learned FROM memories WHERE project_id = ? AND id = 'red-1'", projectID).
		Scan(&what, &why, &learned)
	if err != nil {
		t.Fatalf("query err: %v", err)
	}
	for name, field := range map[string]string{"what": what, "why": why, "learned": learned} {
		if strings.Contains(field, secret) {
			t.Errorf("raw secret persisted via git chunk in %s: %q", name, field)
		}
	}
	if !strings.Contains(what, "deploy uses") {
		t.Errorf("expected redacted text preserved in what, got %q", what)
	}
}

// TestImportRejectsUnsafeIDs verifies that a chunk/JSON carrying a
// path-traversal id is never persisted: the strict ImportJSON path rejects it
// outright and the lenient git-sync path skips it (the id would otherwise
// become a chunk file name and escape .sv-memory/chunks on the next SyncToGit).
func TestImportRejectsUnsafeIDs(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_unsafe_id.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-unsafe-id"
	if err = db.RegisterProject(database, projectID, "Unsafe ID Proj", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	unsafe := &Memory{ID: "../../../evil", ProjectID: projectID, Category: "journal",
		What: "unsafe", Why: "unsafe", Learned: "unsafe", CreatedAt: time.Now()}
	data, err := json.Marshal(unsafe)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// Strict path (ImportJSON) must abort.
	importPath := filepath.Join(tempDir, "unsafe.json")
	if err = os.WriteFile(importPath, data, 0644); err != nil {
		t.Fatalf("failed to write import file: %v", err)
	}
	if _, err = ImportJSON(database, projectID, importPath); err == nil {
		t.Fatal("expected ImportJSON to reject a path-traversal id")
	}

	// Lenient path (git-sync chunk) must skip the chunk, not import it.
	chunkDir := filepath.Join(tempDir, ".sv-memory", "chunks")
	if err = os.MkdirAll(chunkDir, 0755); err != nil {
		t.Fatalf("failed to create chunks dir: %v", err)
	}
	if err = os.WriteFile(filepath.Join(chunkDir, "unsafe.json"), data, 0644); err != nil {
		t.Fatalf("failed to write chunk: %v", err)
	}
	if err = SyncFromGit(database, projectID, tempDir); err != nil {
		t.Fatalf("SyncFromGit should not fail on an unsafe chunk, got: %v", err)
	}

	var count int
	if err = database.QueryRow("SELECT COUNT(*) FROM memories WHERE project_id = ?", projectID).Scan(&count); err != nil {
		t.Fatalf("failed counting memories: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected unsafe ids to be rejected (0 imported), got %d", count)
	}
}
