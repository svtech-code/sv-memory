package memory

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/svtech/sv-memory/internal/security"
)

// Package-level cache so SyncToGit can skip redundant writes when nothing
// changed in SQLite and the json file on disk is already up-to-date. Held
// across the MCP server lifetime (stdio — one process).
var (
	syncCacheMu   sync.Mutex
	lastWriteInfo = map[string]syncCacheEntry{} // keyed by projectID
)

type syncCacheEntry struct {
	memoryCount int
	fileMtim    time.Time // mtime of the JSON file at last write
}

// Memory represents a recorded design decision, bugfix, or coding standard.
type Memory struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	Category    string    `json:"category"` // 'bugfix' | 'architecture' | 'standard' | 'decision' | 'journal' | 'postmortem' | 'discussion' | 'idea' | 'qa'
	What        string    `json:"what"`
	Why         string    `json:"why"`
	WherePath   string    `json:"where_path,omitempty"`
	Learned     string    `json:"learned"`
	GitBranch   string    `json:"git_branch,omitempty"`
	GitCommit   string    `json:"git_commit,omitempty"`
	Author      string    `json:"author,omitempty"`
	Impact      string    `json:"impact,omitempty"`
	ErrorsFaced string    `json:"errors_faced,omitempty"`
	NextSteps   string    `json:"next_steps,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// SaveMemory inserts or replaces a memory record in the database.
func SaveMemory(db *sql.DB, mem *Memory) error {
	if mem.ID == "" {
		return errors.New("memory ID cannot be empty")
	}
	if mem.ProjectID == "" {
		return errors.New("memory ProjectID cannot be empty")
	}

	// Sanitize fields to prevent secret leakages
	mem.What = security.SanitizeText(mem.What)
	mem.Why = security.SanitizeText(mem.Why)
	mem.WherePath = security.SanitizeText(mem.WherePath)
	mem.Learned = security.SanitizeText(mem.Learned)
	mem.GitBranch = security.SanitizeText(mem.GitBranch)
	mem.GitCommit = security.SanitizeText(mem.GitCommit)
	mem.Author = security.SanitizeText(mem.Author)
	mem.Impact = security.SanitizeText(mem.Impact)
	mem.ErrorsFaced = security.SanitizeText(mem.ErrorsFaced)
	mem.NextSteps = security.SanitizeText(mem.NextSteps)

	query := `
	INSERT INTO memories (id, project_id, category, what, why, where_path, learned, git_branch, git_commit, author, impact, errors_faced, next_steps, created_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		category = excluded.category,
		what = excluded.what,
		why = excluded.why,
		where_path = excluded.where_path,
		learned = excluded.learned,
		git_branch = excluded.git_branch,
		git_commit = excluded.git_commit,
		author = excluded.author,
		impact = excluded.impact,
		errors_faced = excluded.errors_faced,
		next_steps = excluded.next_steps,
		created_at = excluded.created_at;
	`
	createdAt := mem.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}

	_, err := db.Exec(query, mem.ID, mem.ProjectID, mem.Category, mem.What, mem.Why, mem.WherePath, mem.Learned, mem.GitBranch, mem.GitCommit, mem.Author, mem.Impact, mem.ErrorsFaced, mem.NextSteps, createdAt)
	if err != nil {
		return fmt.Errorf("failed to save memory: %w", err)
	}
	return nil
}

// SearchMemories queries the database using FTS5 full-text search.
func SearchMemories(db *sql.DB, projectID string, searchTerm string, category string, limit int) ([]*Memory, error) {
	var query string
	var args []interface{}

	if searchTerm == "" {
		// No search term: do a regular metadata query
		query = `
		SELECT id, project_id, category, what, why, where_path, learned, git_branch, git_commit, author, impact, errors_faced, next_steps, created_at
		FROM memories
		WHERE project_id = ?
		`
		args = append(args, projectID)
		if category != "" {
			query += " AND category = ?"
			args = append(args, category)
		}
		query += " ORDER BY created_at DESC"
	} else {
		// Use FTS5 match query joining memories table on rowid
		query = `
		SELECT m.id, m.project_id, m.category, m.what, m.why, m.where_path, m.learned, m.git_branch, m.git_commit, m.author, m.impact, m.errors_faced, m.next_steps, m.created_at
		FROM memories m
		JOIN memories_fts f ON m.rowid = f.rowid
		WHERE m.project_id = ? AND memories_fts MATCH ?
		`
		args = append(args, projectID, searchTerm)
		if category != "" {
			query += " AND m.category = ?"
			args = append(args, category)
		}
		query += " ORDER BY rank"
	}

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed searching memories: %w", err)
	}
	defer rows.Close()

	var memories []*Memory
	for rows.Next() {
		var mem Memory
		var createdAtStr string
		var gitBranch, gitCommit, author, impact, errorsFaced, nextSteps sql.NullString
		err := rows.Scan(&mem.ID, &mem.ProjectID, &mem.Category, &mem.What, &mem.Why, &mem.WherePath, &mem.Learned, &gitBranch, &gitCommit, &author, &impact, &errorsFaced, &nextSteps, &createdAtStr)
		if err != nil {
			return nil, fmt.Errorf("failed scanning memory row: %w", err)
		}
		mem.GitBranch = gitBranch.String
		mem.GitCommit = gitCommit.String
		mem.Author = author.String
		mem.Impact = impact.String
		mem.ErrorsFaced = errorsFaced.String
		mem.NextSteps = nextSteps.String
		t, parseErr := time.Parse(time.RFC3339, createdAtStr)
		if parseErr == nil {
			mem.CreatedAt = t
		} else {
			// Try fallback SQLite standard formats
			t, parseErr = time.Parse("2006-01-02 15:04:05", createdAtStr)
			if parseErr == nil {
				mem.CreatedAt = t
			} else {
				mem.CreatedAt = time.Now()
			}
		}
		memories = append(memories, &mem)
	}

	return memories, nil
}

// countMemories returns the total number of memories for a given project.
func countMemories(db *sql.DB, projectID string) (int, error) {
	var n int
	err := db.QueryRow("SELECT COUNT(*) FROM memories WHERE project_id = ?", projectID).Scan(&n)
	return n, err
}

// SyncToGit serializes all project memories to a local file in `.sv-memory/memories.json`.
// It uses an atomic write (tmp file + rename) so concurrent readers never see a partial file.
// When nothing changed since the last write (same memory count, same file mtime) it skips
// the write entirely to avoid unnecessary I/O and git diffs.
func SyncToGit(db *sql.DB, projectID string, projPath string) error {
	syncDir := filepath.Join(projPath, ".sv-memory")
	syncFile := filepath.Join(syncDir, "memories.json")

	// Quick check: count current memories and compare with cache.
	count, err := countMemories(db, projectID)
	if err != nil {
		return err
	}

	syncCacheMu.Lock()
	info := lastWriteInfo[projectID]
	currentMtim := time.Time{}
	if fi, statErr := os.Stat(syncFile); statErr == nil {
		currentMtim = fi.ModTime()
	}
	if count == info.memoryCount && currentMtim.Equal(info.fileMtim) {
		syncCacheMu.Unlock()
		return nil // nothing changed — skip write
	}
	syncCacheMu.Unlock()

	// Load all memories from SQLite.
	memories, err := SearchMemories(db, projectID, "", "", 0)
	if err != nil {
		return err
	}

	// Create directory if missing.
	if err := os.MkdirAll(syncDir, 0755); err != nil {
		return fmt.Errorf("failed to create sync directory: %w", err)
	}

	data, err := json.MarshalIndent(memories, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal memories JSON: %w", err)
	}

	// Atomic write: write to tmp file then rename (atomic on POSIX / macOS).
	tmpFile := syncFile + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp memories file: %w", err)
	}
	if err := os.Rename(tmpFile, syncFile); err != nil {
		// Clean up temp file on rename failure.
		os.Remove(tmpFile)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	// Update cache with mtime from the file we just wrote.
	if fi, statErr := os.Stat(syncFile); statErr == nil {
		syncCacheMu.Lock()
		lastWriteInfo[projectID] = syncCacheEntry{
			memoryCount: count,
			fileMtim:    fi.ModTime(),
		}
		syncCacheMu.Unlock()
	} else {
		// Should not happen; purge cache so next call retries.
		syncCacheMu.Lock()
		delete(lastWriteInfo, projectID)
		syncCacheMu.Unlock()
	}

	return nil
}

// SyncFromGit imports memories from `.sv-memory/memories.json` if it exists.
func SyncFromGit(db *sql.DB, projectID string, projPath string) error {
	syncFile := filepath.Join(projPath, ".sv-memory", "memories.json")
	if _, err := os.Stat(syncFile); os.IsNotExist(err) {
		// Nothing to sync, that is normal
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

	// Begin transaction
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `
	INSERT INTO memories (id, project_id, category, what, why, where_path, learned, git_branch, git_commit, author, impact, errors_faced, next_steps, created_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		category = excluded.category,
		what = excluded.what,
		why = excluded.why,
		where_path = excluded.where_path,
		learned = excluded.learned,
		git_branch = excluded.git_branch,
		git_commit = excluded.git_commit,
		author = excluded.author,
		impact = excluded.impact,
		errors_faced = excluded.errors_faced,
		next_steps = excluded.next_steps,
		created_at = excluded.created_at;
	`
	stmt, err := tx.Prepare(query)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, mem := range memories {
		// Safety check: force correct projectID in case of copy-paste files
		mem.ProjectID = projectID
		createdAt := mem.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now()
		}
		_, err := stmt.Exec(mem.ID, mem.ProjectID, mem.Category, mem.What, mem.Why, mem.WherePath, mem.Learned, mem.GitBranch, mem.GitCommit, mem.Author, mem.Impact, mem.ErrorsFaced, mem.NextSteps, createdAt)
		if err != nil {
			return fmt.Errorf("failed to sync memory %s: %w", mem.ID, err)
		}
	}

	return tx.Commit()
}
