package memory

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/svtech-code/sv-memory/internal/security"
)

func StartSession(db *sql.DB, projectID, goal, directory string) (*Session, error) {
	id := uuid.New().String()[:8]
	now := time.Now()
	_, err := db.Exec(
		"INSERT INTO sessions (id, project_id, goal, directory, started_at, status) VALUES (?, ?, ?, ?, ?, 'active')",
		id, projectID, goal, directory, now)
	if err != nil {
		return nil, fmt.Errorf("failed to start session: %w", err)
	}
	return &Session{
		ID:        id,
		ProjectID: projectID,
		Goal:      goal,
		Directory: directory,
		StartedAt: now,
		Status:    "active",
	}, nil
}

func EndSession(db *sql.DB, id, summary string) error {
	result, err := db.Exec(
		"UPDATE sessions SET ended_at = ?, summary = ?, status = 'completed' WHERE id = ? AND status = 'active'",
		time.Now(), summary, id)
	if err != nil {
		return fmt.Errorf("failed to end session: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("session %s not found or already completed", id)
	}
	return nil
}

func SaveSessionSummary(db *sql.DB, id, goal, discoveries, accomplished, nextSteps, files string) error {
	goal = security.SanitizeText(goal)
	discoveries = security.SanitizeText(discoveries)
	accomplished = security.SanitizeText(accomplished)
	nextSteps = security.SanitizeText(nextSteps)
	files = security.SanitizeText(files)
	summary := fmt.Sprintf("Goal: %s\nDiscoveries: %s\nAccomplished: %s\nNext Steps: %s\nFiles: %s",
		goal, discoveries, accomplished, nextSteps, files)
	result, err := db.Exec("UPDATE sessions SET goal = ?, summary = ? WHERE id = ?", goal, summary, id)
	if err != nil {
		return fmt.Errorf("failed to save session summary: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("session %s not found", id)
	}
	return nil
}

func GetSession(db *sql.DB, id string) (*Session, error) {
	row := db.QueryRow("SELECT id, project_id, goal, directory, started_at, ended_at, summary, status FROM sessions WHERE id = ?", id)
	var s Session
	var startedAtStr, endedAtStr string
	var goal, directory, summary sql.NullString
	err := row.Scan(&s.ID, &s.ProjectID, &goal, &directory, &startedAtStr, &endedAtStr, &summary, &s.Status)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}
	s.Goal = goal.String
	s.Directory = directory.String
	s.Summary = summary.String
	if t, err := parseTime(startedAtStr); err == nil {
		s.StartedAt = t
	}
	if endedAtStr != "" {
		if t, err := parseTime(endedAtStr); err == nil {
			s.EndedAt = t
		}
	}
	return &s, nil
}

func GetActiveSession(db *sql.DB, projectID string) (*Session, error) {
	row := db.QueryRow("SELECT id, project_id, goal, directory, started_at, ended_at, summary, status FROM sessions WHERE project_id = ? AND status = 'active' ORDER BY started_at DESC LIMIT 1", projectID)
	var s Session
	var startedAtStr, endedAtStr string
	var goal, directory, summary sql.NullString
	err := row.Scan(&s.ID, &s.ProjectID, &goal, &directory, &startedAtStr, &endedAtStr, &summary, &s.Status)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get active session: %w", err)
	}
	s.Goal = goal.String
	s.Directory = directory.String
	s.Summary = summary.String
	if t, err := parseTime(startedAtStr); err == nil {
		s.StartedAt = t
	}
	return &s, nil
}

func GetLastSession(db *sql.DB, projectID string) (*Session, error) {
	row := db.QueryRow("SELECT id, project_id, goal, directory, started_at, ended_at, summary, status FROM sessions WHERE project_id = ? AND status = 'completed' ORDER BY ended_at DESC LIMIT 1", projectID)
	var s Session
	var startedAtStr, endedAtStr string
	var goal, directory, summary sql.NullString
	err := row.Scan(&s.ID, &s.ProjectID, &goal, &directory, &startedAtStr, &endedAtStr, &summary, &s.Status)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get last session: %w", err)
	}
	s.Goal = goal.String
	s.Directory = directory.String
	s.Summary = summary.String
	if t, err := parseTime(startedAtStr); err == nil {
		s.StartedAt = t
	}
	if endedAtStr != "" {
		if t, err := parseTime(endedAtStr); err == nil {
			s.EndedAt = t
		}
	}
	return &s, nil
}

func GetSessionContext(db *sql.DB, projectID string) (string, error) {
	session, err := GetLastSession(db, projectID)
	if err != nil {
		return "", err
	}
	if session == nil {
		mems, err := SearchMemories(db, projectID, "", "", 5)
		if err != nil {
			return "", err
		}
		if len(mems) == 0 {
			return "No previous session context found for this project.", nil
		}
		var sb strings.Builder
		sb.WriteString("No recorded sessions. Most recent memories:\n\n")
		for _, m := range mems {
			sb.WriteString(fmt.Sprintf("- [%s] **%s** (ID: %s, %s)\n",
				strings.ToUpper(m.Category), m.What, m.ID, m.CreatedAt.Format("2006-01-02")))
		}
		return sb.String(), nil
	}

	var sb strings.Builder
	sb.WriteString("## Previous Session Context\n\n")
	sb.WriteString(fmt.Sprintf("**Session ID:** %s\n", session.ID))
	sb.WriteString(fmt.Sprintf("**Started:** %s\n", session.StartedAt.Format("2006-01-02 15:04")))
	if !session.EndedAt.IsZero() {
		sb.WriteString(fmt.Sprintf("**Ended:** %s\n", session.EndedAt.Format("2006-01-02 15:04")))
	}
	if session.Goal != "" {
		sb.WriteString(fmt.Sprintf("**Goal:** %s\n", session.Goal))
	}
	if session.Summary != "" {
		sb.WriteString(fmt.Sprintf("**Summary:** %s\n", session.Summary))
	}

	mems, err := SearchMemoriesBySessionCompact(db, projectID, session.ID, 10)
	if err != nil {
		return "", err
	}
	if len(mems) > 0 {
		sb.WriteString(fmt.Sprintf("\n**Memories saved (%d):**\n", len(mems)))
		for _, m := range mems {
			sb.WriteString(fmt.Sprintf("- [%s] **%s** (ID: %s)\n",
				strings.ToUpper(m.Category), m.What, m.ID))
		}
	}
	return sb.String(), nil
}

func GetAutoBootBundle(db *sql.DB, projectID string) (string, error) {
	var sb strings.Builder
	sb.WriteString("### 🚀 Auto-Boot Context Bundle\n\n")

	sessCtx, err := GetSessionContext(db, projectID)
	if err == nil && sessCtx != "" && !strings.HasPrefix(sessCtx, "No previous session") {
		sb.WriteString(sessCtx)
		sb.WriteString("\n\n")
	}

	rows, err := db.Query(`
	SELECT id, category, what, why, learned, created_at
	FROM memories
	WHERE project_id = ? AND category IN ('architecture', 'decision') AND deleted_at IS NULL
	ORDER BY created_at DESC LIMIT 3`, projectID)
	if err == nil {
		defer rows.Close()
		var archMems []string
		for rows.Next() {
			var id, cat, what, why, learned, createdAt string
			if scanErr := rows.Scan(&id, &cat, &what, &why, &learned, &createdAt); scanErr == nil {
				archMems = append(archMems, fmt.Sprintf("- **[%s] %s** (ID: %s)\n  *Why:* %s", strings.ToUpper(cat), what, id, why))
			}
		}
		if len(archMems) > 0 {
			sb.WriteString("**Key Architectural Decisions:**\n")
			sb.WriteString(strings.Join(archMems, "\n"))
			sb.WriteString("\n\n")
		}
	}
	return strings.TrimSpace(sb.String()), nil
}
