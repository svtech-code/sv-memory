package memory

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/svtech/sv-memory/internal/security"
)

// Memory represents a recorded design decision, bugfix, or coding standard.
type Memory struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Category  string    `json:"category"` // 'bugfix' | 'architecture' | 'standard' | 'decision'
	What      string    `json:"what"`
	Why       string    `json:"why"`
	WherePath string    `json:"where_path,omitempty"`
	Learned   string    `json:"learned"`
	CreatedAt time.Time `json:"created_at"`
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

	query := `
	INSERT INTO memories (id, project_id, category, what, why, where_path, learned, created_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		category = excluded.category,
		what = excluded.what,
		why = excluded.why,
		where_path = excluded.where_path,
		learned = excluded.learned,
		created_at = excluded.created_at;
	`
	createdAt := mem.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}

	_, err := db.Exec(query, mem.ID, mem.ProjectID, mem.Category, mem.What, mem.Why, mem.WherePath, mem.Learned, createdAt)
	if err != nil {
		return fmt.Errorf("failed to save memory: %w", err)
	}
	return nil
}

// SearchMemories queries the database using FTS5 full-text search.
func SearchMemories(db *sql.DB, projectID string, searchTerm string, category string) ([]*Memory, error) {
	var query string
	var args []interface{}

	if searchTerm == "" {
		// No search term: do a regular metadata query
		query = `
		SELECT id, project_id, category, what, why, where_path, learned, created_at
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
		SELECT m.id, m.project_id, m.category, m.what, m.why, m.where_path, m.learned, m.created_at
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

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed searching memories: %w", err)
	}
	defer rows.Close()

	var memories []*Memory
	for rows.Next() {
		var mem Memory
		var createdAtStr string
		err := rows.Scan(&mem.ID, &mem.ProjectID, &mem.Category, &mem.What, &mem.Why, &mem.WherePath, &mem.Learned, &createdAtStr)
		if err != nil {
			return nil, fmt.Errorf("failed scanning memory row: %w", err)
		}
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

// SyncToGit serializes all project memories to a local file in `.sv-memory/memories.json`.
func SyncToGit(db *sql.DB, projectID string, projPath string) error {
	memories, err := SearchMemories(db, projectID, "", "")
	if err != nil {
		return err
	}

	// Create directory if missing
	syncDir := filepath.Join(projPath, ".sv-memory")
	if err := os.MkdirAll(syncDir, 0755); err != nil {
		return fmt.Errorf("failed to create sync directory: %w", err)
	}

	syncFile := filepath.Join(syncDir, "memories.json")
	data, err := json.MarshalIndent(memories, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal memories JSON: %w", err)
	}

	if err := os.WriteFile(syncFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write memories to %s: %w", syncFile, err)
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
	INSERT INTO memories (id, project_id, category, what, why, where_path, learned, created_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		category = excluded.category,
		what = excluded.what,
		why = excluded.why,
		where_path = excluded.where_path,
		learned = excluded.learned,
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
		_, err := stmt.Exec(mem.ID, mem.ProjectID, mem.Category, mem.What, mem.Why, mem.WherePath, mem.Learned, createdAt)
		if err != nil {
			return fmt.Errorf("failed to sync memory %s: %w", mem.ID, err)
		}
	}

	return tx.Commit()
}
