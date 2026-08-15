package memory

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/svtech-code/sv-memory/internal/security"
)

// specsDir returns the human-visible spec mirror directory inside the project:
// <projPath>/.sv-memory/specs/. It is intentionally inside the repo (git-synced
// alongside the chunk store) so humans and agents can read the change proposals
// as plain Markdown while the SQLite store remains the authoritative source.
func specsDir(projPath string) string {
	return filepath.Join(projPath, ".sv-memory", "specs")
}

// specChangesDir is where active (non-terminal) change mirrors live.
func specChangesDir(projPath string) string {
	return filepath.Join(specsDir(projPath), "changes")
}

// specArchiveDir is where archived/rejected change mirrors are moved, mirroring
// OpenSpec's archive layout with a date prefix for chronological ordering.
func specArchiveDir(projPath string) string {
	return filepath.Join(specsDir(projPath), "archive")
}

// ChangeToMarkdown renders a change as a human-readable Markdown spec mirror.
// The format is deliberately simple and parseable back by ParseChangeMarkdown,
// so a human can edit the mirror and the edit reconciles into the DB on import.
func ChangeToMarkdown(c *Change) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\n", c.Title)
	fmt.Fprintf(&sb, "- **ID:** `%s`\n", c.ID)
	fmt.Fprintf(&sb, "- **Slug:** `%s`\n", c.Slug)
	fmt.Fprintf(&sb, "- **Status:** `%s`\n", c.Status)
	if c.WherePath != "" {
		fmt.Fprintf(&sb, "- **Where:** `%s`\n", c.WherePath)
	}
	if c.CapabilityPath != "" {
		fmt.Fprintf(&sb, "- **Capability:** `%s`\n", c.CapabilityPath)
	}
	fmt.Fprintf(&sb, "- **Created:** %s\n", c.CreatedAt.Format(time.RFC3339))

	if c.What != "" {
		fmt.Fprintf(&sb, "\n## Proposal\n\n%s\n", c.What)
	}
	if c.Goal != "" {
		fmt.Fprintf(&sb, "\n## Goal\n\n%s\n", c.Goal)
	}
	if c.Design != "" {
		fmt.Fprintf(&sb, "\n## Design\n\n%s\n", c.Design)
	}
	if c.Tasks != "" {
		fmt.Fprintf(&sb, "\n## Tasks\n\n%s\n", c.Tasks)
	}
	return strings.TrimSpace(sb.String())
}

// ParseChangeMarkdown reconstructs the editable fields of a change from its
// Markdown mirror. It is lenient: missing sections are left empty and unknown
// statuses are preserved verbatim (the caller validates on update). The
// identity fields (ID, slug, created_at) are not parsed back — they are
// authoritative in the DB and only the human-editable content round-trips.
func ParseChangeMarkdown(content string) (*Change, error) {
	lines := strings.Split(content, "\n")
	c := &Change{}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "# "):
			c.Title = strings.TrimSpace(trimmed[2:])
		case strings.HasPrefix(trimmed, "- **Status:**"):
			c.Status = strings.TrimSpace(strings.TrimPrefix(trimmed, "- **Status:**"))
			c.Status = strings.Trim(c.Status, "` ")
		case strings.HasPrefix(trimmed, "- **Where:**"):
			c.WherePath = strings.TrimSpace(strings.TrimPrefix(trimmed, "- **Where:**"))
			c.WherePath = strings.Trim(c.WherePath, "` ")
		case strings.HasPrefix(trimmed, "- **Capability:**"):
			c.CapabilityPath = strings.TrimSpace(strings.TrimPrefix(trimmed, "- **Capability:**"))
			c.CapabilityPath = strings.Trim(c.CapabilityPath, "` ")
		case trimmed == "## Proposal":
			if v, ok := sectionContent(lines, i+1, "## "); ok {
				c.What = v
			}
		case trimmed == "## Goal":
			if v, ok := sectionContent(lines, i+1, "## "); ok {
				c.Goal = v
			}
		case trimmed == "## Design":
			if v, ok := sectionContent(lines, i+1, "## "); ok {
				c.Design = v
			}
		case trimmed == "## Tasks":
			if v, ok := sectionContent(lines, i+1, "## "); ok {
				c.Tasks = v
			}
		}
	}
	return c, nil
}

// sectionContent collects the lines between the section start and the next
// "## " header (or end of file), returning the joined trimmed content.
func sectionContent(lines []string, start int, stopPrefix string) (string, bool) {
	var body []string
	for _, l := range lines[start:] {
		if strings.HasPrefix(strings.TrimSpace(l), stopPrefix) {
			break
		}
		body = append(body, l)
	}
	v := strings.TrimSpace(strings.Join(body, "\n"))
	return v, v != ""
}

// WriteSpecMirror exports every active change (plus archived/rejected mirrors
// moved under archive/) into the Markdown mirror tree. It is best-effort and
// never fails the sync that triggered it: a mirror write error is logged to
// stderr and the authoritative SQLite store is untouched. The DB remains the
// source of truth; the mirror is a human-readable projection.
func WriteSpecMirror(db *sql.DB, projectID, projPath string) error {
	all, err := ListChangesByStatus(db, projectID, "")
	if err != nil {
		return fmt.Errorf("failed to list changes for mirror: %w", err)
	}

	changesDir := specChangesDir(projPath)
	archiveDir := specArchiveDir(projPath)
	if err = os.MkdirAll(changesDir, 0755); err != nil {
		return fmt.Errorf("failed to create specs/changes dir: %w", err)
	}
	if err = os.MkdirAll(archiveDir, 0755); err != nil {
		return fmt.Errorf("failed to create specs/archive dir: %w", err)
	}

	// Index of live mirror files so orphaned (deleted) changes are removed.
	live := map[string]bool{}

	for _, c := range all {
		body := ChangeToMarkdown(c)
		if deltas, dErr := LoadChangeDeltas(db, projectID, c.ID); dErr == nil && len(deltas) > 0 {
			body += "\n\n" + DeltasToMarkdown(deltas)
		}
		body = security.SanitizeText(body)
		if c.Status == ChangeStatusArchived || c.Status == ChangeStatusRejected {
			// Move to archive with a date prefix for chronological ordering.
			fileName := fmt.Sprintf("%s-%s.md", c.CreatedAt.Format("2006-01-02"), c.Slug)
			if err = writeMirrorFile(filepath.Join(archiveDir, fileName), body); err != nil {
				return err
			}
			continue
		}
		live[c.Slug+".md"] = true
		if err = writeMirrorFile(filepath.Join(changesDir, c.Slug+".md"), body); err != nil {
			return err
		}
	}

	// Remove mirrors of changes that no longer exist (soft-deleted projects /
	// cleaned stores) so the mirror never lags behind the authoritative DB.
	entries, err := os.ReadDir(changesDir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			if !live[e.Name()] {
				_ = os.Remove(filepath.Join(changesDir, e.Name()))
			}
		}
	}

	// Project the materialized current state per capability.
	if err = writeCapabilityMirrors(db, projectID, projPath); err != nil {
		return err
	}
	return nil
}

// writeMirrorFile writes a spec mirror file atomically (tmp + rename).
func writeMirrorFile(path, body string) error {
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(body), 0644); err != nil {
		return fmt.Errorf("failed to write spec mirror %s: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename spec mirror %s: %w", path, err)
	}
	return nil
}

// ImportChangeFromMarkdown reads a change's Markdown mirror, parses the
// human-editable content, and reconciles it back into the authoritative DB.
// Only fields the mirror actually contains are updated; identity fields (ID,
// slug) are never changed. Returns the updated change, or nil when the mirror
// file does not exist. A mirror edit never creates a new change — the slug must
// already exist in the store.
func ImportChangeFromMarkdown(db *sql.DB, projectID, projPath, slug string) (*Change, error) {
	path := filepath.Join(specChangesDir(projPath), slug+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read spec mirror %s: %w", path, err)
	}
	content := string(data)

	// Delta sections are stripped before change-field parsing so ParseChangeMarkdown
	// never absorbs "## ADDED Requirements" content into the Tasks/Design sections.
	deltas := ParseSpecDeltas(content)
	changePart := stripDeltaSections(content)
	parsed, err := ParseChangeMarkdown(changePart)
	if err != nil {
		return nil, err
	}
	parsed.Slug = slug

	existing, err := GetChangeBySlug(db, projectID, slug)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("no change with slug %q exists in the store — create it with sv_propose_spec before importing", slug)
	}

	// Build the reconciled update. Only non-empty parsed fields overwrite.
	upd := ChangeUpdate{}
	if parsed.Title != "" && parsed.Title != existing.Title {
		upd.Title = &parsed.Title
	}
	if parsed.What != "" && parsed.What != existing.What {
		upd.What = &parsed.What
	}
	if parsed.Goal != "" && parsed.Goal != existing.Goal {
		upd.Goal = &parsed.Goal
	}
	if parsed.WherePath != "" && parsed.WherePath != existing.WherePath {
		upd.WherePath = &parsed.WherePath
	}
	if parsed.CapabilityPath != "" && parsed.CapabilityPath != existing.CapabilityPath {
		upd.CapabilityPath = &parsed.CapabilityPath
	}
	if parsed.Design != "" && parsed.Design != existing.Design {
		upd.Design = &parsed.Design
	}
	if parsed.Tasks != "" && parsed.Tasks != existing.Tasks {
		upd.Tasks = &parsed.Tasks
	}

	changed := upd.Title != nil || upd.What != nil || upd.Goal != nil ||
		upd.WherePath != nil || upd.CapabilityPath != nil || upd.Design != nil || upd.Tasks != nil
	if changed {
		if existing, err = UpdateChange(db, projectID, existing.ID, upd); err != nil {
			return nil, err
		}
	}

	// Reconcile the delta requirements. The capability defaults to the change's
	// (updated) value so a human-edited Capability line moves the requirements.
	// Reconcile even with zero deltas: a human removing every delta section
	// clears the change's stored requirements.
	if err = ReplaceChangeRequirements(db, projectID, existing.ID, existing.CapabilityPath, deltas); err != nil {
		return nil, err
	}
	return existing, nil
}

// stripDeltaSections removes everything from the first delta section header
// (## ADDED/MODIFIED/REMOVED/RENAMED Requirements) onwards, leaving only the
// change-level body for ParseChangeMarkdown.
func stripDeltaSections(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if level, text, ok := headerLine(strings.TrimSpace(line)); ok && level == 2 && deltaOpForHeader(text) != "" {
			return strings.Join(lines[:i], "\n")
		}
	}
	return content
}

// ChangeUpdate holds the fields an import can reconcile into a change. A nil
// field keeps the stored value; a non-nil field overwrites it.
type ChangeUpdate struct {
	Title          *string
	What           *string
	Goal           *string
	WherePath      *string
	CapabilityPath *string
	Design         *string
	Tasks          *string
}

// UpdateChange partially updates a change by ID from a reconciled mirror edit.
// Fields in upd overwrite; others are preserved. The updated_at timestamp is
// bumped so the next mirror export reflects the reconciliation.
func UpdateChange(db *sql.DB, projectID, id string, upd ChangeUpdate) (*Change, error) {
	existing, err := GetChange(db, projectID, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("change %s not found in project", id)
	}

	title := existing.Title
	if upd.Title != nil {
		title = security.SanitizeText(*upd.Title)
	}
	what := existing.What
	if upd.What != nil {
		what = security.SanitizeText(*upd.What)
	}
	goal := existing.Goal
	if upd.Goal != nil {
		goal = security.SanitizeText(*upd.Goal)
	}
	wherePath := existing.WherePath
	if upd.WherePath != nil {
		wherePath = security.SanitizeText(*upd.WherePath)
	}
	capabilityPath := existing.CapabilityPath
	if upd.CapabilityPath != nil {
		capabilityPath = security.SanitizeText(*upd.CapabilityPath)
	}
	design := existing.Design
	if upd.Design != nil {
		design = security.SanitizeText(*upd.Design)
	}
	tasks := existing.Tasks
	if upd.Tasks != nil {
		tasks = security.SanitizeText(*upd.Tasks)
	}

	if err := validateChangeFields(title, what, goal, wherePath, capabilityPath, design, tasks); err != nil {
		return nil, err
	}

	if _, err := db.Exec(`
		UPDATE changes SET title = ?, what = ?, goal = ?, where_path = ?, capability_path = ?, design = ?, tasks = ?, updated_at = ?
		WHERE project_id = ? AND id = ?`,
		title, what, goal, wherePath, capabilityPath, design, tasks, time.Now(), projectID, id); err != nil {
		return nil, fmt.Errorf("failed to update change: %w", err)
	}
	return GetChange(db, projectID, id)
}

// ListSpecMirrors returns the sorted list of active change slugs present in the
// mirror directory, used by the CLI to show what can be imported.
func ListSpecMirrors(projPath string) ([]string, error) {
	entries, err := os.ReadDir(specChangesDir(projPath))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var slugs []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		slugs = append(slugs, strings.TrimSuffix(e.Name(), ".md"))
	}
	sort.Strings(slugs)
	return slugs, nil
}
