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
	memoryCount   int
	lastSyncTime  time.Time
	jsonSyncCount int
}

func chunkedSyncDir(projPath string) string {
	return filepath.Join(projPath, ".sv-memory", "chunks")
}

func countMemories(db *sql.DB, projectID string) (int, error) {
	var n int
	err := db.QueryRow("SELECT COUNT(*) FROM memories WHERE project_id = ? AND deleted_at IS NULL", projectID).Scan(&n)
	return n, err
}

func searchAllMemories(db *sql.DB, projectID string) ([]*Memory, error) {
	rows, err := db.Query(`
		SELECT id, project_id, category, what, why, where_path, learned,
			git_branch, git_commit, author, impact, errors_faced, next_steps,
			session_id, topic_key, revision_count, duplicate_count, last_seen_at, normalized_hash, created_at
		FROM memories WHERE project_id = ? AND deleted_at IS NULL
		ORDER BY created_at ASC
	`, projectID)
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
	rows, err := db.Query(`
		SELECT id, project_id, category, what, why, where_path, learned,
			git_branch, git_commit, author, impact, errors_faced, next_steps,
			session_id, topic_key, revision_count, duplicate_count, last_seen_at, normalized_hash, created_at
		FROM memories WHERE project_id = ? AND deleted_at IS NULL AND (
			created_at > ? OR (last_seen_at IS NOT NULL AND last_seen_at > ?)
		)
		ORDER BY created_at ASC
	`, projectID, lastSync.Format("2006-01-02 15:04:05"), lastSync.Format("2006-01-02 15:04:05"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMemories(rows)
}

func SyncToGitChunked(db *sql.DB, projectID string, projPath string) error {
	chunkDir := chunkedSyncDir(projPath)
	if err := os.MkdirAll(chunkDir, 0755); err != nil {
		return fmt.Errorf("failed to create chunks directory: %w", err)
	}

	memories, err := searchAllMemories(db, projectID)
	if err != nil {
		return err
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

	for _, mem := range memories {
		data, err := json.Marshal(mem)
		if err != nil {
			return fmt.Errorf("failed to marshal chunk %s: %w", mem.ID, err)
		}
		chunkPath := filepath.Join(chunkDir, mem.ID+".json")
		tmpPath := chunkPath + ".tmp"
		if err := os.WriteFile(tmpPath, data, 0644); err != nil {
			return fmt.Errorf("failed to write chunk %s: %w", mem.ID, err)
		}
		if err := os.Rename(tmpPath, chunkPath); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("failed to rename chunk %s: %w", mem.ID, err)
		}
		delete(existingChunks, mem.ID)
	}

	for id := range existingChunks {
		os.Remove(filepath.Join(chunkDir, id+".json"))
	}

	return nil
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

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := memoryInsertConflictQuery()
	stmt, err := tx.Prepare(query)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		chunkPath := filepath.Join(chunkDir, entry.Name())
		data, err := os.ReadFile(chunkPath)
		if err != nil {
			return fmt.Errorf("failed to read chunk %s: %w", entry.Name(), err)
		}
		var mem Memory
		if unmarshalErr := json.Unmarshal(data, &mem); unmarshalErr != nil {
			return fmt.Errorf("failed to parse chunk %s: %w", entry.Name(), unmarshalErr)
		}
		mem.ProjectID = projectID
		createdAt := mem.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now()
		}
		_, err = stmt.Exec(
			mem.ID, mem.ProjectID, mem.Category, mem.What, mem.Why, mem.WherePath, mem.Learned,
			mem.GitBranch, mem.GitCommit, mem.Author, mem.Impact, mem.ErrorsFaced, mem.NextSteps,
			nullString(mem.SessionID), nullString(mem.TopicKey),
			mem.RevisionCount, mem.DuplicateCount,
			nullTime(mem.LastSeenAt), nullString(mem.NormalizedHash),
			nullTime(mem.ReviewAfter), mem.Pinned,
			createdAt)
		if err != nil {
			return fmt.Errorf("failed to import chunk %s: %w", entry.Name(), err)
		}
	}

	return tx.Commit()
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

	// No inserts, updates, or deletions since the last sync: skip all I/O.
	if len(writeSet) == 0 && countNonDeleted(db, projectID, len(full)) {
		return nil
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

	for _, mem := range writeSet {
		data, marshalErr := json.Marshal(mem)
		if marshalErr != nil {
			return fmt.Errorf("failed to marshal chunk %s: %w", mem.ID, marshalErr)
		}
		chunkPath := filepath.Join(chunkDir, mem.ID+".json")
		tmpPath := chunkPath + ".tmp"
		if writeErr := os.WriteFile(tmpPath, data, 0644); writeErr != nil {
			return fmt.Errorf("failed to write chunk %s: %w", mem.ID, writeErr)
		}
		if renameErr := os.Rename(tmpPath, chunkPath); renameErr != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("failed to rename chunk %s: %w", mem.ID, renameErr)
		}
		delete(existingChunks, mem.ID)
	}

	// Remove chunks whose memory no longer exists (soft-deleted or pruned).
	activeIDs := make(map[string]bool, len(full))
	for _, m := range full {
		activeIDs[m.ID] = true
	}
	for id := range existingChunks {
		if !activeIDs[id] {
			os.Remove(filepath.Join(chunkDir, id+".json"))
		}
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
		memoryCount:   len(full),
		lastSyncTime:  time.Now(),
		jsonSyncCount: nextJSON,
	}
	syncCacheMu.Unlock()

	return nil
}

// countNonDeleted reports whether the live memory count matches the loaded
// set, used to skip a sync that has nothing to do. When counts differ the
// caller must proceed (deletions need orphan cleanup).
func countNonDeleted(db *sql.DB, projectID string, expected int) bool {
	n, err := countMemories(db, projectID)
	return err == nil && n == expected
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
		return fmt.Errorf("failed to parse memories JSON: %w", unmarshalErr)
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := memoryInsertConflictQuery()
	stmt, err := tx.Prepare(query)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, mem := range memories {
		mem.ProjectID = projectID
		createdAt := mem.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now()
		}
		_, err := stmt.Exec(
			mem.ID, mem.ProjectID, mem.Category, mem.What, mem.Why, mem.WherePath, mem.Learned,
			mem.GitBranch, mem.GitCommit, mem.Author, mem.Impact, mem.ErrorsFaced, mem.NextSteps,
			nullString(mem.SessionID), nullString(mem.TopicKey),
			mem.RevisionCount, mem.DuplicateCount,
			nullTime(mem.LastSeenAt), nullString(mem.NormalizedHash),
			nullTime(mem.ReviewAfter), mem.Pinned,
			createdAt)
		if err != nil {
			return fmt.Errorf("failed to sync memory %s: %w", mem.ID, err)
		}
	}

	return tx.Commit()
}
