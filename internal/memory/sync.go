package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	syncCacheMu   sync.Mutex
	lastWriteInfo = map[string]syncCacheEntry{}
)

// jsonSyncInterval controls how often the legacy monolithic memories.json is
// rewritten during incremental syncs. Chunks are the primary Git format; the
// monolithic file is only a fallback for setups without a chunks directory,
// so it is rewritten on the first sync, every jsonSyncInterval syncs, and on
// graceful flush (SyncToGitForceFull).
const jsonSyncInterval = 10

type syncCacheEntry struct {
	lastSyncTime  time.Time
	jsonSyncCount int
}

func chunkedSyncDir(projPath string) string {
	return filepath.Join(projPath, ".sv-memory", "chunks")
}

func searchAllMemories(db *sql.DB, projectID string) ([]*Memory, error) {
	rows, err := db.Query("SELECT "+memoryColumns+
		" FROM memories WHERE project_id = ? AND deleted_at IS NULL ORDER BY created_at ASC", projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMemories(rows)
}

// searchChangedMemories returns only the memories inserted or updated after
// lastSync, so the chunked Git sync can rewrite just the changed chunk files
// instead of the whole set. New inserts are found via created_at; topic-key
// updates and duplicate touches advance last_seen_at, which is also checked.
func searchChangedMemories(db *sql.DB, projectID string, lastSync time.Time) ([]*Memory, error) {
	rows, err := db.Query("SELECT "+memoryColumns+
		" FROM memories WHERE project_id = ? AND deleted_at IS NULL AND ("+
		"created_at > ? OR (last_seen_at IS NOT NULL AND last_seen_at > ?)"+
		") ORDER BY created_at ASC",
		projectID, lastSync, lastSync)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMemories(rows)
}

// writeChunkFiles writes each memory in mems to its own {id}.json chunk file
// inside chunkDir using atomic tmp+rename writes. It returns the set of chunk
// IDs that existed on disk before writing, so callers can remove orphans that
// no longer correspond to a live memory. Used by the incremental syncToGit path.
func writeChunkFiles(chunkDir string, mems []*Memory) (map[string]bool, error) {
	if err := os.MkdirAll(chunkDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create chunks directory: %w", err)
	}

	existingChunks := make(map[string]bool)
	entries, err := os.ReadDir(chunkDir)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
				existingChunks[strings.TrimSuffix(e.Name(), ".json")] = true
			}
		}
	}

	for _, mem := range mems {
		data, marshalErr := json.Marshal(mem)
		if marshalErr != nil {
			return nil, fmt.Errorf("failed to marshal chunk %s: %w", mem.ID, marshalErr)
		}
		chunkPath := filepath.Join(chunkDir, mem.ID+".json")
		tmpPath := chunkPath + ".tmp"
		if writeErr := os.WriteFile(tmpPath, data, 0644); writeErr != nil {
			return nil, fmt.Errorf("failed to write chunk %s: %w", mem.ID, writeErr)
		}
		if renameErr := os.Rename(tmpPath, chunkPath); renameErr != nil {
			os.Remove(tmpPath)
			return nil, fmt.Errorf("failed to rename chunk %s: %w", mem.ID, renameErr)
		}
		delete(existingChunks, mem.ID)
	}

	return existingChunks, nil
}

// logSyncWarning writes a sync warning to stderr. In the MCP stdio transport
// stderr is a safe channel (stdout carries JSON-RPC), so these warnings never
// corrupt a tool response.
func logSyncWarning(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "[sv-memory] sync warning: "+format+"\n", args...)
}

// warnIfLocalDiverges emits a warning when importing the incoming (git) chunk
// would overwrite a local version that appears newer or diverged. This surfaces
// the last-writer-wins semantics of the upsert so a pulled chunk that silently
// replaces a local edit does not go unnoticed. It is best-effort: a missing row
// (pure insert) or a query error simply skips the check.
func warnIfLocalDiverges(tx *sql.Tx, projectID string, mem *Memory) {
	var localRev int
	var localHash sql.NullString
	if err := tx.QueryRow(
		"SELECT revision_count, normalized_hash FROM memories WHERE project_id = ? AND id = ?",
		projectID, mem.ID,
	).Scan(&localRev, &localHash); err != nil {
		return
	}
	switch {
	case mem.RevisionCount < localRev:
		logSyncWarning("memory %s: git chunk is revision %d but local DB is %d — pulling it overwrites a newer local edit (last-writer-wins). Check for a lost change.", mem.ID, mem.RevisionCount, localRev)
	case mem.RevisionCount == localRev && localHash.Valid && localHash.String != "" && mem.NormalizedHash != "" && localHash.String != mem.NormalizedHash:
		logSyncWarning("memory %s: local and git versions diverge at revision %d (different content) — the git chunk wins (last-writer-wins). Check for a lost local edit.", mem.ID, localRev)
	}
}

// importMemoriesIntoDB inserts memories into the project using upsert semantics
// inside a single transaction. Every memory is re-bound to projectID, sanitized,
// and given a default created_at when zero. When warn is true, divergences from
// the local DB are logged before the insert. When strict is true an insert
// failure aborts the import; otherwise the failing row is skipped and counted.
// Returns the number of skipped rows.
func importMemoriesIntoDB(db *sql.DB, projectID string, memories []*Memory, warn, strict bool) (int, error) {
	if len(memories) == 0 {
		return 0, nil
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(memoryInsertConflictQuery())
	if err != nil {
		return 0, fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	skipped := 0
	for _, mem := range memories {
		mem.ProjectID = projectID
		sanitizeMemoryFields(mem)
		createdAt := mem.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now()
		}
		if warn {
			warnIfLocalDiverges(tx, projectID, mem)
		}
		if _, err := stmt.Exec(memoryInsertArgs(mem, createdAt)...); err != nil {
			if strict {
				return 0, fmt.Errorf("failed to import memory %s: %w", mem.ID, err)
			}
			logSyncWarning("skipping memory %s: failed to import (%v)", mem.ID, err)
			skipped++
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit import: %w", err)
	}
	return skipped, nil
}

// readChunkMemory reads and parses a single sync chunk. Unparseable content
// (e.g. a file left with git merge conflict markers after a same-ID concurrent
// edit) is skipped with a warning instead of aborting the whole import, so the
// remaining chunks still arrive. Returns nil for skipped chunks.
func readChunkMemory(chunkPath, name string) *Memory {
	data, err := os.ReadFile(chunkPath)
	if err != nil {
		logSyncWarning("skipping %s: failed to read (%v)", name, err)
		return nil
	}
	var mem Memory
	if unmarshalErr := json.Unmarshal(data, &mem); unmarshalErr != nil {
		logSyncWarning("skipping %s: failed to parse (unresolved git merge conflict markers or corrupt JSON) — resolve the file and re-run 'sv-memory sync' (%v)", name, unmarshalErr)
		return nil
	}
	return &mem
}

func SyncFromGitChunked(db *sql.DB, projectID string, projPath string) error {
	chunkDir := chunkedSyncDir(projPath)
	if _, err := os.Stat(chunkDir); os.IsNotExist(err) {
		return SyncFromGit(db, projectID, projPath)
	}

	entries, err := os.ReadDir(chunkDir)
	if err != nil {
		return fmt.Errorf("failed to read chunks directory: %w", err)
	}

	var memories []*Memory
	var skippedParse int
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if mem := readChunkMemory(filepath.Join(chunkDir, entry.Name()), entry.Name()); mem != nil {
			memories = append(memories, mem)
		} else {
			skippedParse++
		}
	}

	skipped, err := importMemoriesIntoDB(db, projectID, memories, true, false)
	if err != nil {
		return err
	}
	if total := skippedParse + skipped; total > 0 {
		logSyncWarning("%d of %d chunk(s) skipped; the rest imported successfully", total, len(entries))
	}
	return nil
}

func SyncToGit(db *sql.DB, projectID string, projPath string) error {
	return syncToGit(db, projectID, projPath, false)
}

// SyncToGitForceFull performs the same incremental chunk sync as SyncToGit but
// also rewrites the legacy monolithic memories.json regardless of the periodic
// interval. It is used on graceful shutdown so the fallback file is always
// up to date.
func SyncToGitForceFull(db *sql.DB, projectID string, projPath string) error {
	return syncToGit(db, projectID, projPath, true)
}

func syncToGit(db *sql.DB, projectID string, projPath string, forceJSON bool) error {
	syncDir := filepath.Join(projPath, ".sv-memory")
	syncFile := filepath.Join(syncDir, "memories.json")
	chunkDir := chunkedSyncDir(projPath)

	syncCacheMu.Lock()
	info := lastWriteInfo[projectID]
	syncCacheMu.Unlock()

	if mkErr := os.MkdirAll(syncDir, 0755); mkErr != nil {
		return fmt.Errorf("failed to create sync directory: %w", mkErr)
	}
	if mkErr := os.MkdirAll(chunkDir, 0755); mkErr != nil {
		return fmt.Errorf("failed to create chunks directory: %w", mkErr)
	}

	// Full set: used for memories.json and to tell live chunks from orphans.
	full, err := searchAllMemories(db, projectID)
	if err != nil {
		return err
	}

	// When a previous sync exists, only rewrite chunks whose memory changed
	// (new insert or last_seen_at advanced). Otherwise write everything.
	writeSet := full
	if !info.lastSyncTime.IsZero() {
		if changed, cErr := searchChangedMemories(db, projectID, info.lastSyncTime); cErr == nil {
			writeSet = changed
		}
	}

	existingChunks, err := writeChunkFiles(chunkDir, writeSet)
	if err != nil {
		return err
	}

	// Chunks whose memory no longer exists (soft-deleted or pruned) must be
	// removed so a deleted memory does not linger in the sync store.
	activeIDs := make(map[string]bool, len(full))
	for _, m := range full {
		activeIDs[m.ID] = true
	}
	removedOrphan := false
	for id := range existingChunks {
		if !activeIDs[id] {
			if rmErr := os.Remove(filepath.Join(chunkDir, id+".json")); rmErr == nil {
				removedOrphan = true
			}
		}
	}

	// No inserts, updates, or deletions since the last sync: skip the rest of
	// the I/O. A forced full flush (SyncToGitForceFull) always proceeds so the
	// monolithic memories.json is guaranteed to be rewritten on graceful
	// shutdown.
	if !forceJSON && len(writeSet) == 0 && !removedOrphan {
		return nil
	}

	// Rewrite the monolithic memories.json on the first sync, on a forced
	// flush, or every jsonSyncInterval syncs. Chunks are always current, so
	// the monolithic file only needs periodic consistency.
	nextJSON := info.jsonSyncCount + 1
	writeJSON := forceJSON || info.lastSyncTime.IsZero() || nextJSON >= jsonSyncInterval
	if writeJSON {
		data, jErr := json.Marshal(full)
		if jErr != nil {
			return fmt.Errorf("failed to marshal memories JSON: %w", jErr)
		}
		tmpFile := syncFile + ".tmp"
		if wErr := os.WriteFile(tmpFile, data, 0644); wErr != nil {
			return fmt.Errorf("failed to write temp memories file: %w", wErr)
		}
		if rErr := os.Rename(tmpFile, syncFile); rErr != nil {
			os.Remove(tmpFile)
			return fmt.Errorf("failed to rename temp file: %w", rErr)
		}
		nextJSON = 0
	}

	syncCacheMu.Lock()
	lastWriteInfo[projectID] = syncCacheEntry{
		lastSyncTime:  time.Now(),
		jsonSyncCount: nextJSON,
	}
	syncCacheMu.Unlock()

	return nil
}

func SyncFromGit(db *sql.DB, projectID string, projPath string) error {
	chunkDir := chunkedSyncDir(projPath)
	if _, err := os.Stat(chunkDir); err == nil {
		return SyncFromGitChunked(db, projectID, projPath)
	}

	syncFile := filepath.Join(projPath, ".sv-memory", "memories.json")
	if _, err := os.Stat(syncFile); os.IsNotExist(err) {
		return nil
	}

	data, err := os.ReadFile(syncFile)
	if err != nil {
		return fmt.Errorf("failed to read memories file %s: %w", syncFile, err)
	}

	var memories []*Memory
	if unmarshalErr := json.Unmarshal(data, &memories); unmarshalErr != nil {
		logSyncWarning("memories.json failed to parse (unresolved git merge conflict markers or corrupt JSON) — resolve the file and re-run 'sv-memory sync' (%v)", unmarshalErr)
		return nil
	}

	skipped, err := importMemoriesIntoDB(db, projectID, memories, true, false)
	if err != nil {
		return err
	}
	if skipped > 0 {
		logSyncWarning("%d of %d memory(ies) skipped; the rest imported successfully", skipped, len(memories))
	}
	return nil
}
