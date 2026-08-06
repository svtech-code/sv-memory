package memory

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/svtech-code/sv-memory/internal/security"
)

func StartSession(db *sql.DB, projectID, goal, directory string) (*Session, error) {
	id := newID()
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
		mems, searchErr := SearchMemories(db, projectID, "", "", 5)
		if searchErr != nil {
			return "", searchErr
		}
		if len(mems) == 0 {
			return "No previous session context found for this project.", nil
		}
		var sb strings.Builder
		sb.WriteString("No recorded sessions. Most recent memories:\n\n")
		for _, m := range mems {
			fmt.Fprintf(&sb, "- [%s] **%s** (ID: %s, %s)\n",
				strings.ToUpper(m.Category), m.What, m.ID, m.CreatedAt.Format("2006-01-02"))
		}
		return sb.String(), nil
	}

	var sb strings.Builder
	sb.WriteString("## Previous Session Context\n\n")
	fmt.Fprintf(&sb, "**Session ID:** %s\n", session.ID)
	fmt.Fprintf(&sb, "**Started:** %s\n", session.StartedAt.Format("2006-01-02 15:04"))
	if !session.EndedAt.IsZero() {
		fmt.Fprintf(&sb, "**Ended:** %s\n", session.EndedAt.Format("2006-01-02 15:04"))
	}
	if session.Goal != "" {
		fmt.Fprintf(&sb, "**Goal:** %s\n", session.Goal)
	}
	if session.Summary != "" {
		fmt.Fprintf(&sb, "**Summary:** %s\n", session.Summary)
	}

	mems, err := SearchMemoriesBySessionCompact(db, projectID, session.ID, 10)
	if err != nil {
		return "", err
	}
	if len(mems) > 0 {
		fmt.Fprintf(&sb, "\n**Memories saved (%d):**\n", len(mems))
		for _, m := range mems {
			fmt.Fprintf(&sb, "- [%s] **%s** (ID: %s)\n",
				strings.ToUpper(m.Category), m.What, m.ID)
		}
	}
	return sb.String(), nil
}

func GetAutoBootBundle(db *sql.DB, projectID string) (string, error) {
	var sb strings.Builder
	sb.WriteString("### 🚀 Auto-Boot Context Bundle\n\n")

	// Collect IDs already shown in the previous-session section so the
	// per-category sections below don't repeat them (dedup).
	shown := map[string]bool{}
	sessCtx, err := GetSessionContext(db, projectID)
	if err == nil && sessCtx != "" && !strings.HasPrefix(sessCtx, "No previous session") {
		sb.WriteString(sessCtx)
		sb.WriteString("\n\n")
		if last, lErr := GetLastSession(db, projectID); lErr == nil && last != nil {
			if mems, mErr := SearchMemoriesBySessionCompact(db, projectID, last.ID, 10); mErr == nil {
				for _, m := range mems {
					shown[m.ID] = true
				}
			}
		}
	}

	writeBundleSection(&sb, db, projectID, "Key Architectural Decisions", `
		SELECT id, category, what, why
		FROM memories
		WHERE project_id = ? AND category IN ('architecture', 'decision') AND deleted_at IS NULL
		ORDER BY created_at DESC LIMIT 3`, true, shown)

	writeBundleSection(&sb, db, projectID, "Standards & Conventions", `
		SELECT id, category, what, why
		FROM memories
		WHERE project_id = ? AND category = 'standard' AND deleted_at IS NULL
		ORDER BY created_at DESC LIMIT 2`, false, shown)

	writeBundleSection(&sb, db, projectID, "Recent Work & Known Issues", `
		SELECT id, category, what, why
		FROM memories
		WHERE project_id = ? AND category IN ('bugfix', 'journal') AND deleted_at IS NULL
		ORDER BY created_at DESC LIMIT 2`, false, shown)

	return strings.TrimSpace(sb.String()), nil
}

// bundleWhyChars caps the rationale shown per memory in the Auto-Boot bundle
// when withWhy is true, so long rationales don't bloat the session-start
// response. The agent can drill down with sv_mem_get for full content.
const bundleWhyChars = 300

// writeBundleSection appends a titled, compact list of memories to the
// Auto-Boot bundle. When withWhy is false only the title is shown to keep
// the bundle token-efficient; the agent can drill down with sv_mem_get.
// IDs in exclude are skipped to avoid repeating session-listed memories.
func writeBundleSection(sb *strings.Builder, db *sql.DB, projectID, title, query string, withWhy bool, exclude map[string]bool) {
	rows, err := db.Query(query, projectID)
	if err != nil {
		return
	}
	defer rows.Close()
	var items []string
	for rows.Next() {
		var id, cat, what, why string
		if err := rows.Scan(&id, &cat, &what, &why); err != nil {
			continue
		}
		if exclude[id] {
			continue
		}
		if withWhy {
			why = truncateText(why, bundleWhyChars)
			items = append(items, fmt.Sprintf("- **[%s] %s** (ID: %s)\n  *Why:* %s", strings.ToUpper(cat), what, id, why))
		} else {
			items = append(items, fmt.Sprintf("- **[%s] %s** (ID: %s)", strings.ToUpper(cat), what, id))
		}
	}
	if len(items) > 0 {
		sb.WriteString("**" + title + ":**\n")
		sb.WriteString(strings.Join(items, "\n"))
		sb.WriteString("\n\n")
	}
}
