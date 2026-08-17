package memory

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/svtech-code/sv-memory/internal/security"
)

// Change lifecycle states, mirroring the spec-driven decision cycle: a proposal
// is drafted, proposed, and validated against existing invariants before work
// begins; once implemented it is marked applied and optionally archived for
// history (or rejected outright).
const (
	ChangeStatusDraft     = "draft"
	ChangeStatusProposed  = "proposed"
	ChangeStatusValidated = "validated"
	ChangeStatusApplied   = "applied"
	ChangeStatusArchived  = "archived"
	ChangeStatusRejected  = "rejected"
)

// ChangeStatusValid reports whether the given status is a legal lifecycle
// state. Kept in the memory package (and mirrored in the db migration) so the
// domain layer is the single source of truth for the transition vocabulary.
func ChangeStatusValid(status string) bool {
	switch status {
	case ChangeStatusDraft, ChangeStatusProposed, ChangeStatusValidated,
		ChangeStatusApplied, ChangeStatusArchived, ChangeStatusRejected:
		return true
	}
	return false
}

// Change represents a spec-driven proposal in the decision engine: an idea that
// travels through the propose -> validate -> apply -> archive lifecycle and,
// once committed, produces one or more decision/standard memories.
type Change struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"project_id"`
	Slug           string    `json:"slug"`
	Status         string    `json:"status"`
	Title          string    `json:"title"`
	What           string    `json:"what,omitempty"`
	Goal           string    `json:"goal,omitempty"`
	WherePath      string    `json:"where_path,omitempty"`
	CapabilityPath string    `json:"capability_path,omitempty"`
	Design         string    `json:"design,omitempty"`
	Tasks          string    `json:"tasks,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at,omitempty"`
	ArchivedAt     time.Time `json:"archived_at,omitempty"`
}

// CreateChange inserts a new change in the draft state. The slug must be
// non-empty and unique per project; title is required. Free-text fields are
// sanitized (secret redaction) before persistence, matching the memory save
// path.
func CreateChange(db *sql.DB, projectID, slug, title, what, goal, wherePath, design, tasks string) (*Change, error) {
	if projectID == "" {
		return nil, errors.New("change ProjectID cannot be empty")
	}
	if strings.TrimSpace(slug) == "" {
		return nil, errors.New("change slug cannot be empty")
	}
	if strings.TrimSpace(title) == "" {
		return nil, errors.New("change title cannot be empty")
	}
	if err := validateChangeFields(title, what, goal, wherePath, "", design, tasks); err != nil {
		return nil, err
	}

	slug = security.SanitizeText(strings.TrimSpace(slug))
	title = security.SanitizeText(strings.TrimSpace(title))
	what = security.SanitizeText(what)
	goal = security.SanitizeText(goal)
	wherePath = security.SanitizeText(wherePath)
	design = security.SanitizeText(design)
	tasks = security.SanitizeText(tasks)

	// A change targets a single capability, defaulting to its slug.
	capabilityPath := security.SanitizeText(strings.TrimSpace(slug))

	id := newID()
	now := time.Now()
	if _, err := db.Exec(`
		INSERT INTO changes (id, project_id, slug, status, title, what, goal, where_path, capability_path, design, tasks, created_at)
		VALUES (?, ?, ?, 'draft', ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, projectID, slug, title, what, goal, wherePath, capabilityPath, design, tasks, now); err != nil {
		// Surface the UNIQUE(project_id, slug) violation clearly so the caller
		// can offer an upsert/continue instead of a cryptic constraint error.
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, fmt.Errorf("a change with slug %q already exists in this project", slug)
		}
		return nil, fmt.Errorf("failed to create change: %w", err)
	}
	return &Change{
		ID:             id,
		ProjectID:      projectID,
		Slug:           slug,
		Status:         ChangeStatusDraft,
		Title:          title,
		What:           what,
		Goal:           goal,
		WherePath:      wherePath,
		CapabilityPath: capabilityPath,
		Design:         design,
		Tasks:          tasks,
		CreatedAt:      now,
	}, nil
}

// GetChange returns a change by ID within a project, or nil when not found.
func GetChange(db *sql.DB, projectID, id string) (*Change, error) {
	row := db.QueryRow(`
		SELECT id, project_id, slug, status, title, what, goal, where_path, capability_path, design, tasks, created_at, updated_at, archived_at
		FROM changes WHERE project_id = ? AND id = ?`, projectID, id)
	c, err := scanChange(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

// GetChangeBySlug returns a change by its project-unique slug, or nil when not
// found. Used to deduplicate proposals with the same topic before creating.
func GetChangeBySlug(db *sql.DB, projectID, slug string) (*Change, error) {
	row := db.QueryRow(`
		SELECT id, project_id, slug, status, title, what, goal, where_path, capability_path, design, tasks, created_at, updated_at, archived_at
		FROM changes WHERE project_id = ? AND slug = ?`, projectID, slug)
	c, err := scanChange(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

// scanChange scans a full change row. The caller is responsible for mapping
// sql.ErrNoRows to its nil result.
func scanChange(row *sql.Row) (*Change, error) {
	var c Change
	var createdAtStr, updatedAtStr, archivedAtStr sql.NullString
	var what, goal, wherePath, capabilityPath, design, tasks sql.NullString
	if err := row.Scan(&c.ID, &c.ProjectID, &c.Slug, &c.Status, &c.Title,
		&what, &goal, &wherePath, &capabilityPath, &design, &tasks,
		&createdAtStr, &updatedAtStr, &archivedAtStr); err != nil {
		return nil, err
	}
	c.What = what.String
	c.Goal = goal.String
	c.WherePath = wherePath.String
	c.CapabilityPath = capabilityPath.String
	c.Design = design.String
	c.Tasks = tasks.String
	c.CreatedAt = parseTimeOrNow(createdAtStr.String)
	if t, err := parseTime(updatedAtStr.String); err == nil {
		c.UpdatedAt = t
	}
	if t, err := parseTime(archivedAtStr.String); err == nil {
		c.ArchivedAt = t
	}
	return &c, nil
}

// UpdateChangeStatus transitions a change to the given lifecycle state. Only
// legal states are accepted. Entering 'applied' or 'archived' stamps the
// archive timestamp; every transition bumps updated_at. Returns the change
// with its new state, or an error when the change does not exist.
func UpdateChangeStatus(db *sql.DB, projectID, id, status string) (*Change, error) {
	if !ChangeStatusValid(status) {
		return nil, fmt.Errorf("invalid change status %q", status)
	}
	now := time.Now()
	var archivedAt interface{}
	if status == ChangeStatusApplied || status == ChangeStatusArchived {
		archivedAt = now
	}
	res, err := db.Exec(`
		UPDATE changes SET status = ?, updated_at = ?, archived_at = COALESCE(?, archived_at)
		WHERE project_id = ? AND id = ?`, status, now, archivedAt, projectID, id)
	if err != nil {
		return nil, fmt.Errorf("failed to update change status: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, fmt.Errorf("change %s not found in project", id)
	}
	return GetChange(db, projectID, id)
}

// ListChangesByStatus returns the changes of a project, optionally filtered by
// a single lifecycle state, most recently created first.
func ListChangesByStatus(db *sql.DB, projectID, status string) ([]*Change, error) {
	query := `
		SELECT id, project_id, slug, status, title, what, goal, where_path, capability_path, design, tasks, created_at, updated_at, archived_at
		FROM changes WHERE project_id = ?`
	args := []interface{}{projectID}
	if status != "" {
		if !ChangeStatusValid(status) {
			return nil, fmt.Errorf("invalid change status %q", status)
		}
		query += " AND status = ?"
		args = append(args, status)
	}
	query += " ORDER BY created_at DESC"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list changes: %w", err)
	}
	defer rows.Close()

	var changes []*Change
	for rows.Next() {
		var c Change
		var createdAtStr, updatedAtStr, archivedAtStr sql.NullString
		var what, goal, wherePath, capabilityPath, design, tasks sql.NullString
		if err := rows.Scan(&c.ID, &c.ProjectID, &c.Slug, &c.Status, &c.Title,
			&what, &goal, &wherePath, &capabilityPath, &design, &tasks,
			&createdAtStr, &updatedAtStr, &archivedAtStr); err != nil {
			return nil, err
		}
		c.What = what.String
		c.Goal = goal.String
		c.WherePath = wherePath.String
		c.CapabilityPath = capabilityPath.String
		c.Design = design.String
		c.Tasks = tasks.String
		c.CreatedAt = parseTimeOrNow(createdAtStr.String)
		if t, err := parseTime(updatedAtStr.String); err == nil {
			c.UpdatedAt = t
		}
		if t, err := parseTime(archivedAtStr.String); err == nil {
			c.ArchivedAt = t
		}
		changes = append(changes, &c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return changes, nil
}

// SetMemoryChangeID links a memory to the change that produced it (used by the
// decision engine when committing a proposal). Linking is best-effort on the
// change side: an unknown change_id is stored as-is and surfaced later only by
// explicit memory lookup, so a commit never fails because of a stale change.
func SetMemoryChangeID(db *sql.DB, projectID, memoryID, changeID string) error {
	if _, err := db.Exec(
		"UPDATE memories SET change_id = ? WHERE project_id = ? AND id = ? AND deleted_at IS NULL",
		nullString(changeID), projectID, memoryID); err != nil {
		return fmt.Errorf("failed to link memory to change: %w", err)
	}
	return nil
}

// SetChangeCapabilityPath overrides the capability a change targets (defaults
// to its slug at creation). Used by the propose path when the agent specifies a
// distinct capability name. Returns the updated change.
func SetChangeCapabilityPath(db *sql.DB, projectID, id, capabilityPath string) (*Change, error) {
	existing, err := GetChange(db, projectID, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("change %s not found in project", id)
	}
	capabilityPath = security.SanitizeText(strings.TrimSpace(capabilityPath))
	if capabilityPath == "" {
		capabilityPath = existing.Slug
	}
	if _, err := db.Exec(`
		UPDATE changes SET capability_path = ?, updated_at = ?
		WHERE project_id = ? AND id = ?`,
		capabilityPath, time.Now(), projectID, id); err != nil {
		return nil, fmt.Errorf("failed to update change capability: %w", err)
	}
	return GetChange(db, projectID, id)
}

// ChangeStats returns the count of active (not archived/rejected) changes per
// lifecycle state, used by the Auto-Boot bundle to hint at in-flight proposals
// without loading their content. The result is only populated for states that
// have at least one change, so the caller renders a hint with zero cost when
// the store is healthy.
func ChangeStats(db *sql.DB, projectID string) (map[string]int, error) {
	rows, err := db.Query(`
		SELECT status, COUNT(*)
		FROM changes
		WHERE project_id = ? AND status NOT IN ('archived', 'rejected')
		GROUP BY status`, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch change stats: %w", err)
	}
	defer rows.Close()

	stats := map[string]int{
		ChangeStatusDraft:     0,
		ChangeStatusProposed:  0,
		ChangeStatusValidated: 0,
		ChangeStatusApplied:   0,
	}
	for rows.Next() {
		var status string
		var n int
		if scanErr := rows.Scan(&status, &n); scanErr == nil {
			stats[status] = n
		}
	}
	return stats, rows.Err()
}
