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

type syncCacheEntry struct {
	memoryCount int
	fileMtim    time.Time
}

func chunkedSyncDir(projPath string) string {
	return filepath.Join(projPath, ".sv-memory", "chunks")
}

func countMemories(db *sql.DB, projectID string) (int, error) {
	var n int
	err := db.QueryRow("SELECT COUNT(*) FROM memories WHERE project_id = ?", projectID).Scan(&n)
	return n, err
}

func searchAllMemories(db *sql.DB, projectID string) ([]*Memory, error) {
	rows, err := db.Query(`
		SELECT id, project_id, category, what, why, where_path, learned,
			git_branch, git_commit, author, impact, errors_faced, next_steps,
			session_id, topic_key, revision_count, duplicate_count, last_seen_at, normalized_hash, created_at
		FROM memories WHERE project_id = ?
		ORDER BY created_at ASC
	`, projectID)
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
		data, err := json.MarshalIndent(mem, "", "  ")
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
		if err := json.Unmarshal(data, &mem); err != nil {
			return fmt.Errorf("failed to parse chunk %s: %w", entry.Name(), err)
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
			createdAt)
		if err != nil {
			return fmt.Errorf("failed to import chunk %s: %w", entry.Name(), err)
		}
	}

	return tx.Commit()
}

func SyncToGit(db *sql.DB, projectID string, projPath string) error {
	syncDir := filepath.Join(projPath, ".sv-memory")
	syncFile := filepath.Join(syncDir, "memories.json")
	chunkDir := chunkedSyncDir(projPath)

	count, err := countMemories(db, projectID)
	if err != nil {
		return err
	}

	syncCacheMu.Lock()
	info := lastWriteInfo[projectID]
	currentMtim := time.Time{}
	if fi, statErr := os.Stat(chunkDir); statErr == nil {
		currentMtim = fi.ModTime()
	} else if fi, statErr := os.Stat(syncFile); statErr == nil {
		currentMtim = fi.ModTime()
	}
	if count == info.memoryCount && currentMtim.Equal(info.fileMtim) {
		syncCacheMu.Unlock()
		return nil
	}
	syncCacheMu.Unlock()

	memories, err := searchAllMemories(db, projectID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(syncDir, 0755); err != nil {
		return fmt.Errorf("failed to create sync directory: %w", err)
	}

	if err := os.MkdirAll(chunkDir, 0755); err != nil {
		return fmt.Errorf("failed to create chunks directory: %w", err)
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
		data, err := json.MarshalIndent(mem, "", "  ")
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

	data, err := json.MarshalIndent(memories, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal memories JSON: %w", err)
	}
	tmpFile := syncFile + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp memories file: %w", err)
	}
	if err := os.Rename(tmpFile, syncFile); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	signalPath := chunkDir
	if fi, statErr := os.Stat(signalPath); statErr == nil {
		syncCacheMu.Lock()
		lastWriteInfo[projectID] = syncCacheEntry{
			memoryCount: count,
			fileMtim:    fi.ModTime(),
		}
		syncCacheMu.Unlock()
	} else {
		syncCacheMu.Lock()
		delete(lastWriteInfo, projectID)
		syncCacheMu.Unlock()
	}

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
	if err := json.Unmarshal(data, &memories); err != nil {
		return fmt.Errorf("failed to parse memories JSON: %w", err)
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
			createdAt)
		if err != nil {
			return fmt.Errorf("failed to sync memory %s: %w", mem.ID, err)
		}
	}

	return tx.Commit()
}
