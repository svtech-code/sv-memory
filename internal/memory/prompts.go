package memory

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/svtech-code/sv-memory/internal/security"
)

// UserPrompt is a captured user prompt attached to a session. Mirrors Engram's
// user_prompts store: prompts are local observations (not git-synced) so future
// sessions can recover the user's intent after compaction.
type UserPrompt struct {
	ID        string
	ProjectID string
	SessionID string
	Content   string
	CreatedAt time.Time
}

// SavePrompt persists a user prompt under the given session (may be empty for
// prompts captured outside an active session). Secrets are redacted before
// write. Returns the stored prompt.
func SavePrompt(db *sql.DB, projectID, sessionID, content string) (*UserPrompt, error) {
	id := newID()
	now := time.Now()
	content = strings.TrimSpace(security.SanitizeText(content))
	if content == "" {
		return nil, fmt.Errorf("prompt content is empty")
	}
	_, err := db.Exec(
		"INSERT INTO user_prompts (id, project_id, session_id, content, created_at) VALUES (?, ?, ?, ?, ?)",
		id, projectID, sessionID, content, now)
	if err != nil {
		return nil, fmt.Errorf("failed to save user prompt: %w", err)
	}
	return &UserPrompt{ID: id, ProjectID: projectID, SessionID: sessionID, Content: content, CreatedAt: now}, nil
}

// RecentPrompts returns the most recent prompts for a project, optionally
// scoped to a single session. Ordered newest-first. Returns at most limit rows.
func RecentPrompts(db *sql.DB, projectID, sessionID string, limit int) ([]*UserPrompt, error) {
	if limit <= 0 {
		limit = 10
	}
	query := `SELECT id, project_id, session_id, content, created_at FROM user_prompts WHERE project_id = ?`
	args := []interface{}{projectID}
	if sessionID != "" {
		query += ` AND session_id = ?`
		args = append(args, sessionID)
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query user prompts: %w", err)
	}
	defer rows.Close()

	var prompts []*UserPrompt
	for rows.Next() {
		var p UserPrompt
		var createdAt string
		if err := rows.Scan(&p.ID, &p.ProjectID, &p.SessionID, &p.Content, &createdAt); err != nil {
			return nil, fmt.Errorf("failed scanning prompt row: %w", err)
		}
		if t, err := parseTime(createdAt); err == nil {
			p.CreatedAt = t
		}
		prompts = append(prompts, &p)
	}
	return prompts, rows.Err()
}

// CountPrompts returns the number of prompts recorded for a project.
func CountPrompts(db *sql.DB, projectID string) (int, error) {
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM user_prompts WHERE project_id = ?", projectID).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count user prompts: %w", err)
	}
	return count, nil
}
