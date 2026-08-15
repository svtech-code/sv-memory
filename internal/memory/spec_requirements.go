package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/svtech-code/sv-memory/internal/security"
)

// RequirementRow is a persisted spec_requirements row (a delta entry for one
// change). Scenarios and RFC 2119 keywords are stored as JSON strings.
type RequirementRow struct {
	ID             string
	ProjectID      string
	ChangeID       string
	CapabilityPath string
	DeltaOp        string
	Requirement    string
	RenameTo       string
	Body           string
	RFC2119        string
	Scenarios      string
	SortOrder      int
	CreatedAt      time.Time
}

// CapabilityRequirement is a row of spec_capabilities: the materialized current
// state of one requirement in a capability.
type CapabilityRequirement struct {
	ID             string
	ProjectID      string
	CapabilityPath string
	Requirement    string
	Body           string
	RFC2119        []string
	Scenarios      []Scenario
	UpdatedAt      time.Time
}

// validateCapabilityPath normalizes and bounds a capability path, rejecting
// absolute or traversal paths so the capability mirror can never escape
// .sv-memory/specs/capabilities/.
func validateCapabilityPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("capability path cannot be empty")
	}
	if len(p) > 200 {
		p = p[:200]
	}
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, "\\") {
		return "", fmt.Errorf("capability path must be relative: %q", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", fmt.Errorf("unsafe capability path %q", p)
		}
	}
	return p, nil
}

// ReplaceChangeRequirements replaces every delta row of a change with the
// parsed deltas, in a transaction. Used by the mirror import reconciliation and
// by the propose path when requirements are provided.
func ReplaceChangeRequirements(db *sql.DB, projectID, changeID, capabilityPath string, deltas []Delta) error {
	capabilityPath, err := validateCapabilityPath(capabilityPath)
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		"DELETE FROM spec_requirements WHERE project_id = ? AND change_id = ?", projectID, changeID); err != nil {
		return fmt.Errorf("failed to clear change requirements: %w", err)
	}

	now := time.Now()
	sortOrder := 0
	for _, d := range deltas {
		if !DeltaOpValid(d.Op) {
			return fmt.Errorf("invalid delta op %q", d.Op)
		}
		for _, r := range d.Requirements {
			name := security.SanitizeText(strings.TrimSpace(r.Name))
			if name == "" {
				return fmt.Errorf("requirement name cannot be empty")
			}
			body := security.SanitizeText(r.Body)
			renameTo := security.SanitizeText(strings.TrimSpace(r.RenameTo))
			if _, err := tx.Exec(`
				INSERT INTO spec_requirements (id, project_id, change_id, capability_path, delta_op, requirement, rename_to, body, rfc2119, scenarios, sort_order, created_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				newID(), projectID, changeID, capabilityPath, d.Op, name, nullString(renameTo), nullString(body),
				marshalRFC2119(ExtractRFC2119(body)), marshalScenarios(r.Scenarios), sortOrder, now); err != nil {
				return fmt.Errorf("failed to insert requirement %q: %w", name, err)
			}
			sortOrder++
		}
	}
	return tx.Commit()
}

// ListChangeRequirements returns the persisted delta rows of a change in parse
// order. Best-effort scanning ignores rows that fail to scan rather than
// failing the whole list.
func ListChangeRequirements(db *sql.DB, projectID, changeID string) ([]RequirementRow, error) {
	rows, err := db.Query(`
		SELECT id, project_id, change_id, capability_path, delta_op, requirement, COALESCE(rename_to, ''), COALESCE(body, ''), COALESCE(rfc2119, ''), COALESCE(scenarios, ''), sort_order, created_at
		FROM spec_requirements
		WHERE project_id = ? AND change_id = ?
		ORDER BY sort_order ASC`, projectID, changeID)
	if err != nil {
		return nil, fmt.Errorf("failed to list change requirements: %w", err)
	}
	defer rows.Close()

	var out []RequirementRow
	for rows.Next() {
		var r RequirementRow
		var createdAt string
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.ChangeID, &r.CapabilityPath, &r.DeltaOp,
			&r.Requirement, &r.RenameTo, &r.Body, &r.RFC2119, &r.Scenarios, &r.SortOrder, &createdAt); err != nil {
			return nil, err
		}
		r.CreatedAt = parseTimeOrNow(createdAt)
		out = append(out, r)
	}
	return out, rows.Err()
}

// LoadChangeDeltas reconstructs the ordered []Delta of a change from its
// persisted rows, grouping rows by op in parse order.
func LoadChangeDeltas(db *sql.DB, projectID, changeID string) ([]Delta, error) {
	rows, err := ListChangeRequirements(db, projectID, changeID)
	if err != nil {
		return nil, err
	}
	var deltas []Delta
	for _, r := range rows {
		idx := -1
		for i := range deltas {
			if deltas[i].Op == r.DeltaOp {
				idx = i
				break
			}
		}
		if idx < 0 {
			deltas = append(deltas, Delta{Op: r.DeltaOp})
			idx = len(deltas) - 1
		}
		deltas[idx].Requirements = append(deltas[idx].Requirements, Requirement{
			Name:      r.Requirement,
			RenameTo:  r.RenameTo,
			Body:      r.Body,
			Scenarios: unmarshalScenarios(r.Scenarios),
		})
	}
	return deltas, nil
}

// MergeDeltas applies deltas to the materialized current state of a capability
// (spec_capabilities). Semantics follow OpenSpec: RENAMED first (so a MODIFIED
// naming the new header resolves), then ADDED (appends, erroring on a name that
// already exists), MODIFIED (replaces the whole requirement block), REMOVED
// (deletes, lenient on an already-absent requirement).
func MergeDeltas(db *sql.DB, projectID, capabilityPath string, deltas []Delta) error {
	capabilityPath, err := validateCapabilityPath(capabilityPath)
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback()

	now := time.Now()
	for _, op := range []string{DeltaRenamed, DeltaAdded, DeltaModified, DeltaRemoved} {
		for _, d := range deltas {
			if d.Op != op {
				continue
			}
			for _, r := range d.Requirements {
				name := security.SanitizeText(strings.TrimSpace(r.Name))
				body := security.SanitizeText(r.Body)
				if err := mergeRequirement(tx, projectID, capabilityPath, op, name, security.SanitizeText(strings.TrimSpace(r.RenameTo)), body, r.Scenarios, now); err != nil {
					return err
				}
			}
		}
	}
	return tx.Commit()
}

func mergeRequirement(tx *sql.Tx, projectID, capabilityPath, op, name, renameTo, body string, scenarios []Scenario, now time.Time) error {
	switch op {
	case DeltaRenamed:
		if name == "" || renameTo == "" || name == renameTo {
			return fmt.Errorf("invalid rename %q -> %q", name, renameTo)
		}
		res, err := tx.Exec(`
			UPDATE spec_capabilities SET requirement = ?, updated_at = ?
			WHERE project_id = ? AND capability_path = ? AND requirement = ?`,
			renameTo, now, projectID, capabilityPath, name)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return fmt.Errorf("cannot rename %q: a requirement %q already exists in capability %q", name, renameTo, capabilityPath)
			}
			return fmt.Errorf("failed to rename requirement %q: %w", name, err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("cannot rename %q: no such requirement in capability %q", name, capabilityPath)
		}
	case DeltaAdded:
		if name == "" {
			return fmt.Errorf("added requirement name cannot be empty")
		}
		var count int
		if err := tx.QueryRow(`
			SELECT COUNT(*) FROM spec_capabilities
			WHERE project_id = ? AND capability_path = ? AND requirement = ?`,
			projectID, capabilityPath, name).Scan(&count); err != nil {
			return fmt.Errorf("failed to check requirement existence: %w", err)
		}
		if count > 0 {
			return fmt.Errorf("requirement %q already exists in capability %q — use MODIFIED", name, capabilityPath)
		}
		if _, err := tx.Exec(`
			INSERT INTO spec_capabilities (id, project_id, capability_path, requirement, body, rfc2119, scenarios, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			newID(), projectID, capabilityPath, name, nullString(body),
			marshalRFC2119(ExtractRFC2119(body)), marshalScenarios(scenarios), now); err != nil {
			return fmt.Errorf("failed to add requirement %q: %w", name, err)
		}
	case DeltaModified:
		if name == "" {
			return fmt.Errorf("modified requirement name cannot be empty")
		}
		if _, err := tx.Exec(`
			INSERT INTO spec_capabilities (id, project_id, capability_path, requirement, body, rfc2119, scenarios, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(project_id, capability_path, requirement) DO UPDATE SET
				body = excluded.body,
				rfc2119 = excluded.rfc2119,
				scenarios = excluded.scenarios,
				updated_at = excluded.updated_at`,
			newID(), projectID, capabilityPath, name, nullString(body),
			marshalRFC2119(ExtractRFC2119(body)), marshalScenarios(scenarios), now); err != nil {
			return fmt.Errorf("failed to modify requirement %q: %w", name, err)
		}
	case DeltaRemoved:
		if _, err := tx.Exec(`
			DELETE FROM spec_capabilities
			WHERE project_id = ? AND capability_path = ? AND requirement = ?`,
			projectID, capabilityPath, name); err != nil {
			return fmt.Errorf("failed to remove requirement %q: %w", name, err)
		}
	}
	return nil
}

// MergeChangeDeltas loads a change's deltas and merges them into its
// capability's current state. Used by the commit path.
func MergeChangeDeltas(db *sql.DB, projectID, changeID string) error {
	c, err := GetChange(db, projectID, changeID)
	if err != nil {
		return err
	}
	if c == nil {
		return fmt.Errorf("change %s not found in project", changeID)
	}
	deltas, err := LoadChangeDeltas(db, projectID, changeID)
	if err != nil {
		return err
	}
	if len(deltas) == 0 {
		return nil
	}
	return MergeDeltas(db, projectID, c.CapabilityPath, deltas)
}

// ListCapabilities returns the distinct capability paths present in the current
// state, ordered.
func ListCapabilities(db *sql.DB, projectID string) ([]string, error) {
	rows, err := db.Query(`
		SELECT DISTINCT capability_path FROM spec_capabilities
		WHERE project_id = ? ORDER BY capability_path`, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to list capabilities: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CapabilityRequirements returns the current-state rows of a capability.
func CapabilityRequirements(db *sql.DB, projectID, capabilityPath string) ([]CapabilityRequirement, error) {
	rows, err := db.Query(`
		SELECT id, project_id, capability_path, requirement, COALESCE(body, ''), COALESCE(rfc2119, ''), COALESCE(scenarios, ''), updated_at
		FROM spec_capabilities
		WHERE project_id = ? AND capability_path = ?
		ORDER BY requirement`, projectID, capabilityPath)
	if err != nil {
		return nil, fmt.Errorf("failed to list capability requirements: %w", err)
	}
	defer rows.Close()

	var out []CapabilityRequirement
	for rows.Next() {
		var r CapabilityRequirement
		var rfc2119, scenarios, updatedAt string
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.CapabilityPath, &r.Requirement,
			&r.Body, &rfc2119, &scenarios, &updatedAt); err != nil {
			return nil, err
		}
		r.RFC2119 = unmarshalRFC2119(rfc2119)
		r.Scenarios = unmarshalScenarios(scenarios)
		r.UpdatedAt = parseTimeOrNow(updatedAt)
		out = append(out, r)
	}
	return out, rows.Err()
}

// writeCapabilityMirrors projects the materialized current state of every
// capability under .sv-memory/specs/capabilities/<cap>/spec.md, and removes
// orphaned capability directories whose capability no longer exists.
func writeCapabilityMirrors(db *sql.DB, projectID, projPath string) error {
	capsDir := filepath.Join(specsDir(projPath), "capabilities")
	if err := os.MkdirAll(capsDir, 0755); err != nil {
		return fmt.Errorf("failed to create capabilities dir: %w", err)
	}

	caps, err := ListCapabilities(db, projectID)
	if err != nil {
		return err
	}
	live := map[string]bool{}
	for _, cap := range caps {
		var reqs []CapabilityRequirement
		if reqs, err = CapabilityRequirements(db, projectID, cap); err != nil {
			return err
		}
		var path string
		if path, err = capabilityMirrorPath(projPath, cap); err != nil {
			return err
		}
		if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return fmt.Errorf("failed to create capability mirror dir: %w", err)
		}
		body := CapabilityToMarkdown(cap, reqs)
		if err = writeMirrorFile(path, security.SanitizeText(body)); err != nil {
			return err
		}
		live[cap] = true
	}

	entries, err := os.ReadDir(capsDir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if !capabilityHasLivePath(e.Name(), live) {
			_ = os.RemoveAll(filepath.Join(capsDir, e.Name()))
		}
	}
	return nil
}

// capabilityHasLivePath reports whether name is a capability or a parent
// directory of one (for nested paths such as "cli/validate").
func capabilityHasLivePath(name string, live map[string]bool) bool {
	if live[name] {
		return true
	}
	for c := range live {
		if strings.HasPrefix(c, name+"/") {
			return true
		}
	}
	return false
}

// capabilityMirrorPath returns the mirror path for a capability, rejecting
// unsafe paths before any file operation.
func capabilityMirrorPath(projPath, capabilityPath string) (string, error) {
	cap, err := validateCapabilityPath(capabilityPath)
	if err != nil {
		return "", err
	}
	return filepath.Join(specsDir(projPath), "capabilities", cap, "spec.md"), nil
}

// CapabilityToMarkdown renders a capability's current state as an
// OpenSpec-style main spec (## Requirements + requirements with scenarios).
func CapabilityToMarkdown(capabilityPath string, reqs []CapabilityRequirement) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s Specification\n\n", capabilityPath)
	sb.WriteString("## Requirements\n")
	for _, r := range reqs {
		fmt.Fprintf(&sb, "\n### Requirement: %s\n", r.Requirement)
		if r.Body != "" {
			sb.WriteString(r.Body + "\n")
		}
		for _, sc := range r.Scenarios {
			fmt.Fprintf(&sb, "\n#### Scenario: %s\n", sc.Name)
			for _, st := range sc.Steps {
				fmt.Fprintf(&sb, "- **%s** %s\n", st.Keyword, st.Text)
			}
		}
	}
	return strings.TrimSpace(sb.String())
}

func marshalScenarios(scenarios []Scenario) string {
	if len(scenarios) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(scenarios)
	return string(b)
}

func unmarshalScenarios(s string) []Scenario {
	if s == "" {
		return nil
	}
	var out []Scenario
	if err := json.Unmarshal([]byte(s), &out); err != nil || len(out) == 0 {
		return nil
	}
	return out
}

func marshalRFC2119(keywords []string) string {
	if len(keywords) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(keywords)
	return string(b)
}

func unmarshalRFC2119(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil || len(out) == 0 {
		return nil
	}
	return out
}
