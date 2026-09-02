package memory

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/svtech-code/sv-memory/internal/db"
)

func TestChangeToMarkdownRoundTrip(t *testing.T) {
	c := &Change{
		ID:        "abc123",
		ProjectID: "proj",
		Slug:      "add-csv-export",
		Status:    "proposed",
		Title:     "Add CSV export",
		What:      "Let users export data in CSV format",
		Goal:      "Data portability",
		WherePath: "internal/export/",
		Design:    "Reuse the existing exporter",
		Tasks:     "- [ ] 1.1 Implement export\n- [ ] 1.2 Add tests",
	}

	md := ChangeToMarkdown(c)
	if !strings.Contains(md, "# Add CSV export") {
		t.Errorf("expected title heading, got:\n%s", md)
	}
	if !strings.Contains(md, "- **Status:** `proposed`") {
		t.Errorf("expected status line, got:\n%s", md)
	}
	if !strings.Contains(md, "## Proposal") {
		t.Errorf("expected proposal section, got:\n%s", md)
	}

	parsed, err := ParseChangeMarkdown(md)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	if parsed.Title != c.Title {
		t.Errorf("expected title %q, got %q", c.Title, parsed.Title)
	}
	if parsed.Status != c.Status {
		t.Errorf("expected status %q, got %q", c.Status, parsed.Status)
	}
	if parsed.What != c.What {
		t.Errorf("expected what %q, got %q", c.What, parsed.What)
	}
	if parsed.Goal != c.Goal {
		t.Errorf("expected goal %q, got %q", c.Goal, parsed.Goal)
	}
	if parsed.WherePath != c.WherePath {
		t.Errorf("expected where %q, got %q", c.WherePath, parsed.WherePath)
	}
	if parsed.Design != c.Design {
		t.Errorf("expected design %q, got %q", c.Design, parsed.Design)
	}
	if parsed.Tasks != c.Tasks {
		t.Errorf("expected tasks %q, got %q", c.Tasks, parsed.Tasks)
	}
}

func TestChangeToMarkdownOmitsEmptySections(t *testing.T) {
	c := &Change{
		Slug:   "minimal",
		Status: "draft",
		Title:  "Minimal",
	}
	md := ChangeToMarkdown(c)
	if strings.Contains(md, "## Proposal") {
		t.Error("expected no empty proposal section")
	}
	if strings.Contains(md, "## Tasks") {
		t.Error("expected no empty tasks section")
	}
}

func TestParseChangeMarkdownLenient(t *testing.T) {
	// Human edits: change title and add a design section not originally present.
	md := `# Renamed Proposal

- **ID:** ` + "`keep-me`" + `
- **Slug:** ` + "`whatever`" + `
- **Status:** ` + "`validated`" + `

## Proposal

New behavior description.

## Design

Hand-written design added by a human.
`
	parsed, err := ParseChangeMarkdown(md)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	if parsed.Title != "Renamed Proposal" {
		t.Errorf("expected renamed title, got %q", parsed.Title)
	}
	if parsed.Status != "validated" {
		t.Errorf("expected validated status, got %q", parsed.Status)
	}
	if !strings.Contains(parsed.Design, "Hand-written design") {
		t.Errorf("expected hand-written design, got %q", parsed.Design)
	}
	if parsed.ID != "" {
		t.Errorf("identity fields must not parse back, got %q", parsed.ID)
	}
}

func TestWriteSpecMirrorAndImport(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "specs.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-specs"
	projPath := filepath.Join(tempDir, "repo")
	if err = os.MkdirAll(projPath, 0755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}
	if err = db.RegisterProject(database, projectID, "Specs", projPath); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Create two changes: one active, one archived.
	active, err := CreateChange(database, projectID, "active-change", "Active change", "Body A", "Goal A", "internal/a/", "Design A", "Tasks A")
	if err != nil {
		t.Fatalf("failed to create active change: %v", err)
	}
	archived, err := CreateChange(database, projectID, "archived-change", "Archived change", "Body B", "", "", "", "")
	if err != nil {
		t.Fatalf("failed to create archived change: %v", err)
	}
	if _, err = UpdateChangeStatus(database, projectID, archived.ID, ChangeStatusArchived); err != nil {
		t.Fatalf("failed to archive: %v", err)
	}

	if err = WriteSpecMirror(database, projectID, projPath); err != nil {
		t.Fatalf("failed to write mirror: %v", err)
	}

	// Active change mirror exists under changes/.
	activePath := filepath.Join(projPath, ".sv-memory", "specs", "changes", "active-change.md")
	data, err := os.ReadFile(activePath)
	if err != nil {
		t.Fatalf("expected active mirror file: %v", err)
	}
	if !strings.Contains(string(data), "Body A") {
		t.Errorf("expected active body in mirror, got: %s", data)
	}

	// Archived change mirror moved under archive/ with a date prefix.
	entries, err := os.ReadDir(filepath.Join(projPath, ".sv-memory", "specs", "archive"))
	if err != nil {
		t.Fatalf("expected archive dir: %v", err)
	}
	foundArchived := false
	for _, e := range entries {
		if strings.Contains(e.Name(), "archived-change") {
			foundArchived = true
		}
	}
	if !foundArchived {
		t.Error("expected archived-change mirror under archive/")
	}
	if _, statErr := os.Stat(filepath.Join(projPath, ".sv-memory", "specs", "changes", "archived-change.md")); statErr == nil {
		t.Error("archived change mirror should not remain under changes/")
	}

	// Edit the active mirror and import it back.
	edited := strings.Replace(string(data), "Body A", "Body A (edited by human)", 1)
	edited = strings.Replace(edited, "# Active change", "# Active change (renamed)", 1)
	if err = os.WriteFile(activePath, []byte(edited), 0644); err != nil {
		t.Fatalf("failed to edit mirror: %v", err)
	}

	updated, err := ImportChangeFromMarkdown(database, projectID, projPath, "active-change")
	if err != nil {
		t.Fatalf("failed to import mirror: %v", err)
	}
	if updated == nil {
		t.Fatal("expected updated change")
	}
	if updated.What != "Body A (edited by human)" {
		t.Errorf("expected reconciled what, got %q", updated.What)
	}
	if updated.Title != "Active change (renamed)" {
		t.Errorf("expected reconciled title, got %q", updated.Title)
	}

	// Import of a mirror for a slug with no change in the store must error
	// (a mirror edit cannot create a change — the DB is authoritative).
	phantomPath := filepath.Join(projPath, ".sv-memory", "specs", "changes", "phantom.md")
	if pErr := os.WriteFile(phantomPath, []byte("# Phantom\n\n## Proposal\n\nBody"), 0644); pErr != nil {
		t.Fatalf("failed to write phantom mirror: %v", pErr)
	}
	if _, err = ImportChangeFromMarkdown(database, projectID, projPath, "phantom"); err == nil {
		t.Error("expected error importing a change that does not exist in the store")
	}

	_ = active
}

func TestWriteSpecMirrorRemovesOrphans(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "specs_orphan.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-specs-orphan"
	projPath := filepath.Join(tempDir, "repo")
	if err = os.MkdirAll(projPath, 0755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}
	if err = db.RegisterProject(database, projectID, "Orphan", projPath); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	c, err := CreateChange(database, projectID, "slug-a", "A", "Body", "", "", "", "")
	if err != nil {
		t.Fatalf("failed to create change: %v", err)
	}
	if err = WriteSpecMirror(database, projectID, projPath); err != nil {
		t.Fatalf("failed to write mirror: %v", err)
	}

	// Simulate the change being deleted from the store (hard delete).
	if err = hardDeleteChange(database, projectID, c.ID); err != nil {
		t.Fatalf("failed to delete change: %v", err)
	}
	if err = WriteSpecMirror(database, projectID, projPath); err != nil {
		t.Fatalf("failed to write mirror after delete: %v", err)
	}

	path := filepath.Join(projPath, ".sv-memory", "specs", "changes", "slug-a.md")
	if _, err := os.Stat(path); err == nil {
		t.Error("orphan mirror should have been removed")
	}
}

// hardDeleteChange physically removes a change row so the mirror orphan-cleanup
// path can be tested (the memory package has no exported hard-delete for changes).
func hardDeleteChange(database *sql.DB, projectID, id string) error {
	_, err := database.Exec("DELETE FROM changes WHERE project_id = ? AND id = ?", projectID, id)
	return err
}

func TestListSpecMirrors(t *testing.T) {
	tempDir := t.TempDir()
	projPath := filepath.Join(tempDir, "repo")
	mirrorDir := filepath.Join(projPath, ".sv-memory", "specs", "changes")
	if err := os.MkdirAll(mirrorDir, 0755); err != nil {
		t.Fatalf("failed to create mirror dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mirrorDir, "aaa.md"), []byte("# A"), 0644); err != nil {
		t.Fatalf("failed to write aaa: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mirrorDir, "bbb.md"), []byte("# B"), 0644); err != nil {
		t.Fatalf("failed to write bbb: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mirrorDir, "notes.txt"), []byte("ignore"), 0644); err != nil {
		t.Fatalf("failed to write notes.txt: %v", err)
	}

	slugs, err := ListSpecMirrors(projPath)
	if err != nil {
		t.Fatalf("failed to list mirrors: %v", err)
	}
	if len(slugs) != 2 {
		t.Fatalf("expected 2 slugs, got %d: %v", len(slugs), slugs)
	}
	if slugs[0] != "aaa" || slugs[1] != "bbb" {
		t.Errorf("expected sorted [aaa bbb], got %v", slugs)
	}
}

func TestFormatTaskProgress(t *testing.T) {
	comp, tot, ratio, summary := FormatTaskProgress("")
	if tot != 0 || summary != "no checklist tasks defined" {
		t.Errorf("expected empty progress, got comp=%d tot=%d ratio=%f summary=%q", comp, tot, ratio, summary)
	}

	tasks := `- [x] 1.1 First task
- [X] 1.2 Second task
- [ ] 1.3 Third task
* [x] 1.4 Fourth task
* [ ] 1.5 Fifth task`

	comp, tot, ratio, summary = FormatTaskProgress(tasks)
	if comp != 3 || tot != 5 || ratio != 0.6 {
		t.Errorf("expected 3/5 (60%%), got comp=%d tot=%d ratio=%f summary=%q", comp, tot, ratio, summary)
	}
	if !strings.Contains(summary, "3/5 tasks completed (60%)") {
		t.Errorf("expected summary with percentage, got %q", summary)
	}
}

func TestImportModularOpenSpecDirectory(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-openspec"
	err = db.RegisterProject(database, projectID, "OpenSpec Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// 1. Create initial change via domain layer
	slug := "auth-session"
	c, err := CreateChange(database, projectID, slug, "Old Title", "Old What", "Old Goal", "pkg/auth", "Old Design", "- [ ] Task 1")
	if err != nil {
		t.Fatalf("failed to create change: %v", err)
	}

	// 2. Create modular OpenSpec directory: openspec/changes/auth-session/
	modDir := filepath.Join(tempDir, "openspec", "changes", slug)
	err = os.MkdirAll(modDir, 0755)
	if err != nil {
		t.Fatalf("failed to create openspec modDir: %v", err)
	}

	err = os.WriteFile(filepath.Join(modDir, "proposal.md"), []byte("# Modular Title\n\n- **Where:** `internal/auth/`\n- **Goal:** `Enhanced security`\n\n## Proposal\n\nNew proposal body"), 0644)
	if err != nil {
		t.Fatalf("failed writing proposal.md: %v", err)
	}
	err = os.WriteFile(filepath.Join(modDir, "design.md"), []byte("Token-based session auth design"), 0644)
	if err != nil {
		t.Fatalf("failed writing design.md: %v", err)
	}
	err = os.WriteFile(filepath.Join(modDir, "tasks.md"), []byte("- [x] 1. Implement token generator\n- [ ] 2. Add test suite"), 0644)
	if err != nil {
		t.Fatalf("failed writing tasks.md: %v", err)
	}
	err = os.WriteFile(filepath.Join(modDir, "specs.md"), []byte("## ADDED Requirements\n\n### Requirement: Token Rotation\nThe system SHALL rotate session tokens every hour.\n\n#### Scenario: Expired token\nGIVEN an expired token\nWHEN request arrives\nTHEN reject with 401\n"), 0644)
	if err != nil {
		t.Fatalf("failed writing specs.md: %v", err)
	}

	// 3. Import from modular OpenSpec layout
	imported, err := ImportChangeFromMarkdown(database, projectID, tempDir, slug)
	if err != nil {
		t.Fatalf("ImportChangeFromMarkdown failed on modular dir: %v", err)
	}
	if imported == nil {
		t.Fatal("expected imported change, got nil")
	}

	if imported.Title != "Modular Title" {
		t.Errorf("expected Title %q, got %q", "Modular Title", imported.Title)
	}
	if imported.WherePath != "internal/auth/" {
		t.Errorf("expected WherePath %q, got %q", "internal/auth/", imported.WherePath)
	}
	if imported.Goal != "Enhanced security" {
		t.Errorf("expected Goal %q, got %q", "Enhanced security", imported.Goal)
	}
	if imported.Design != "Token-based session auth design" {
		t.Errorf("expected Design %q, got %q", "Token-based session auth design", imported.Design)
	}
	if !strings.Contains(imported.Tasks, "- [x] 1. Implement token generator") {
		t.Errorf("expected updated tasks, got %q", imported.Tasks)
	}

	// Check delta requirements stored
	deltas, err := LoadChangeDeltas(database, projectID, c.ID)
	if err != nil {
		t.Fatalf("failed loading deltas: %v", err)
	}
	if len(deltas) != 1 || len(deltas[0].Requirements) != 1 {
		t.Fatalf("expected 1 delta with 1 requirement, got: %+v", deltas)
	}
	if deltas[0].Requirements[0].Name != "Token Rotation" {
		t.Errorf("expected Requirement 'Token Rotation', got %q", deltas[0].Requirements[0].Name)
	}
}
