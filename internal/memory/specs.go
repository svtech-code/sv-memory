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
		case strings.HasPrefix(trimmed, "- **Goal:**"):
			c.Goal = strings.TrimSpace(strings.TrimPrefix(trimmed, "- **Goal:**"))
			c.Goal = strings.Trim(c.Goal, "` ")
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

	// If openspec/changes exists, also synchronize modular OpenSpec files
	openSpecChangesDir := filepath.Join(projPath, "openspec", "changes")
	if fi, sErr := os.Stat(openSpecChangesDir); sErr == nil && fi.IsDir() {
		for _, c := range all {
			if c.Status == ChangeStatusArchived || c.Status == ChangeStatusRejected {
				continue
			}
			modDir := filepath.Join(openSpecChangesDir, c.Slug)
			if mErr := os.MkdirAll(modDir, 0755); mErr == nil {
				_ = writeMirrorFile(filepath.Join(modDir, "proposal.md"), ChangeToMarkdown(c))
				if c.Design != "" {
					_ = writeMirrorFile(filepath.Join(modDir, "design.md"), c.Design)
				}
				if c.Tasks != "" {
					_ = writeMirrorFile(filepath.Join(modDir, "tasks.md"), c.Tasks)
				}
				if deltas, dErr := LoadChangeDeltas(db, projectID, c.ID); dErr == nil && len(deltas) > 0 {
					_ = writeMirrorFile(filepath.Join(modDir, "specs.md"), DeltasToMarkdown(deltas))
				}
			}
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
	if err := atomicWriteFile(path, []byte(body)); err != nil {
		return fmt.Errorf("failed to write spec mirror %s: %w", path, err)
	}
	return nil
}

// ListSpecMirrors returns the sorted list of active change slugs present in the
// mirror directories (.sv-memory/specs/ and openspec/), used by the CLI.
func ListSpecMirrors(projPath string) ([]string, error) {
	slugSet := make(map[string]bool)
	dirsToCheck := []string{
		specChangesDir(projPath),
		filepath.Join(projPath, "openspec", "changes"),
	}

	for _, dir := range dirsToCheck {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				slugSet[e.Name()] = true
			} else if strings.HasSuffix(e.Name(), ".md") {
				slugSet[strings.TrimSuffix(e.Name(), ".md")] = true
			}
		}
	}

	var slugs []string
	for s := range slugSet {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)
	return slugs, nil
}

// FormatTaskProgress parses standard markdown task checkboxes (- [ ] and - [x])
// and returns the completed count, total count, ratio, and human-readable summary.
func FormatTaskProgress(tasks string) (completed, total int, ratio float64, summary string) {
	lines := strings.Split(tasks, "\n")
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "- [x]") || strings.HasPrefix(trimmed, "- [X]") ||
			strings.HasPrefix(trimmed, "* [x]") || strings.HasPrefix(trimmed, "* [X]") {
			completed++
			total++
		} else if strings.HasPrefix(trimmed, "- [ ]") || strings.HasPrefix(trimmed, "* [ ]") {
			total++
		}
	}
	if total == 0 {
		return 0, 0, 0, "no checklist tasks defined"
	}
	ratio = float64(completed) / float64(total)
	pct := int(ratio * 100)
	summary = fmt.Sprintf("%d/%d tasks completed (%d%%)", completed, total, pct)
	return completed, total, ratio, summary
}

// ImportChangeFromMarkdown reads a change's Markdown mirror (single-file or
// modular OpenSpec directory layout with proposal.md, design.md, tasks.md,
// specs.md), parses the human-editable content, and reconciles it back into
// the authoritative DB.
func ImportChangeFromMarkdown(db *sql.DB, projectID, projPath, slug string) (*Change, error) {
	if _, err := validateCapabilityPath(slug); err != nil {
		return nil, fmt.Errorf("invalid change slug: %w", err)
	}

	parsed, deltas, found, err := loadSpecChangeFromMirror(projPath, slug)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}

	existing, err := GetChangeBySlug(db, projectID, slug)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("no change with slug %q exists in the store — create it with sv_propose_spec before importing", slug)
	}

	upd, changed := buildChangeUpdate(existing, parsed)
	if changed {
		if existing, err = UpdateChange(db, projectID, existing.ID, upd); err != nil {
			return nil, err
		}
	}

	if err = ReplaceChangeRequirements(db, projectID, existing.ID, existing.CapabilityPath, deltas); err != nil {
		return nil, err
	}
	return existing, nil
}

func loadSpecChangeFromMirror(projPath, slug string) (*Change, []Delta, bool, error) {
	if parsed, deltas, found := readModularOpenSpecChange(projPath, slug); found {
		return parsed, deltas, true, nil
	}
	return readSingleFileSpecChange(projPath, slug)
}

func readModularOpenSpecChange(projPath, slug string) (*Change, []Delta, bool) {
	modDirs := []string{
		filepath.Join(projPath, "openspec", "changes", slug),
		filepath.Join(specChangesDir(projPath), slug),
	}

	var foundModDir string
	for _, md := range modDirs {
		if fi, err := os.Stat(md); err == nil && fi.IsDir() {
			foundModDir = md
			break
		}
	}
	if foundModDir == "" {
		return nil, nil, false
	}

	parsed := &Change{Slug: slug}
	if pData, err := os.ReadFile(filepath.Join(foundModDir, "proposal.md")); err == nil {
		pChange, pErr := ParseChangeMarkdown(string(pData))
		if pErr == nil {
			parsed.Title = pChange.Title
			parsed.What = pChange.What
			parsed.Goal = pChange.Goal
			parsed.WherePath = pChange.WherePath
			parsed.CapabilityPath = pChange.CapabilityPath
			if parsed.What == "" {
				parsed.What = strings.TrimSpace(string(pData))
			}
		}
	}
	if dData, err := os.ReadFile(filepath.Join(foundModDir, "design.md")); err == nil {
		parsed.Design = strings.TrimSpace(string(dData))
	}
	if tData, err := os.ReadFile(filepath.Join(foundModDir, "tasks.md")); err == nil {
		parsed.Tasks = strings.TrimSpace(string(tData))
	}

	var deltas []Delta
	specsPaths := []string{
		filepath.Join(foundModDir, "specs.md"),
		filepath.Join(foundModDir, "spec.md"),
	}
	for _, sp := range specsPaths {
		if sData, err := os.ReadFile(sp); err == nil {
			deltas = ParseSpecDeltas(string(sData))
			if len(deltas) > 0 {
				break
			}
		}
	}
	return parsed, deltas, true
}

func readSingleFileSpecChange(projPath, slug string) (*Change, []Delta, bool, error) {
	singlePaths := []string{
		filepath.Join(specChangesDir(projPath), slug+".md"),
		filepath.Join(projPath, "openspec", "changes", slug+".md"),
	}
	var data []byte
	var foundPath string
	for _, sp := range singlePaths {
		if d, err := os.ReadFile(sp); err == nil {
			foundPath = sp
			data = d
			break
		}
	}
	if foundPath == "" {
		return nil, nil, false, nil
	}

	content := string(data)
	deltas := ParseSpecDeltas(content)
	changePart := stripDeltaSections(content)
	parsed, err := ParseChangeMarkdown(changePart)
	if err != nil {
		return nil, nil, false, err
	}
	parsed.Slug = slug
	return parsed, deltas, true, nil
}

func buildChangeUpdate(existing, parsed *Change) (ChangeUpdate, bool) {
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
	return upd, changed
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
