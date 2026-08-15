package memory

import (
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/svtech-code/sv-memory/internal/db"
	"github.com/svtech-code/sv-memory/internal/graph"
)

// setupSpecReqDB registers a project on a fresh in-memory store and returns the
// database, project ID, and repo path for mirror tests.
func setupSpecReqDB(t *testing.T) (*sql.DB, string, string) {
	t.Helper()
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "spec.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	projectID := "proj-spec-req"
	projPath := filepath.Join(tempDir, "repo")
	if err = os.MkdirAll(projPath, 0755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}
	if err = db.RegisterProject(database, projectID, "Spec Req", projPath); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}
	return database, projectID, projPath
}

func TestActiveSpecCapabilityRefs(t *testing.T) {
	database, projectID, _ := setupSpecReqDB(t)

	c, err := CreateChange(database, projectID, "auth-work", "Auth work", "", "", "internal/auth/", "", "")
	if err != nil {
		t.Fatalf("failed to create change: %v", err)
	}
	if _, err = SetChangeCapabilityPath(database, projectID, c.ID, "auth"); err != nil {
		t.Fatalf("failed to set capability: %v", err)
	}
	if err = ReplaceChangeRequirements(database, projectID, c.ID, "auth", []Delta{{Op: DeltaAdded, Requirements: []Requirement{
		{Name: "Login", Body: "The system SHALL log users in."},
	}}}); err != nil {
		t.Fatalf("failed to store requirements: %v", err)
	}
	// Merge so the current state (spec_capabilities) also carries the capability.
	if err = MergeChangeDeltas(database, projectID, c.ID); err != nil {
		t.Fatalf("failed to merge: %v", err)
	}

	refs, err := ActiveSpecCapabilityRefs(database, projectID)
	if err != nil {
		t.Fatalf("failed to list refs: %v", err)
	}
	foundChange := false
	foundCap := false
	for _, ref := range refs {
		if ref.ChangeID == c.ID && ref.CapabilityPath == "auth" && ref.WherePath == "internal/auth/" {
			foundChange = true
		}
		if ref.ChangeID == "" && ref.CapabilityPath == "auth" {
			foundCap = true
		}
	}
	if !foundChange || !foundCap {
		t.Errorf("expected change ref and current-state ref for auth, got %+v", refs)
	}
}

func TestValidateChangeRequirements(t *testing.T) {
	database, projectID, _ := setupSpecReqDB(t)

	// Current capability state with an existing scenario.
	if err := MergeDeltas(database, projectID, "auth", []Delta{{Op: DeltaAdded, Requirements: []Requirement{
		{Name: "Login", Body: "The system SHALL issue a token.", Scenarios: []Scenario{
			{Name: "Valid", Steps: []ScenarioStep{{Keyword: "WHEN", Text: "creds valid"}}},
			{Name: "Invalid", Steps: []ScenarioStep{{Keyword: "WHEN", Text: "creds bad"}}},
		}},
	}}}); err != nil {
		t.Fatalf("failed to seed state: %v", err)
	}

	c, err := CreateChange(database, projectID, "tighten-login", "Tighten login", "", "", "", "", "")
	if err != nil {
		t.Fatalf("failed to create change: %v", err)
	}
	if _, err = SetChangeCapabilityPath(database, projectID, c.ID, "auth"); err != nil {
		t.Fatalf("failed to set capability: %v", err)
	}
	// MODIFIED drops "Invalid" scenario; body has no RFC 2119 keyword.
	if err = ReplaceChangeRequirements(database, projectID, c.ID, "auth", []Delta{{Op: DeltaModified, Requirements: []Requirement{
		{Name: "Login", Body: "Sessions last a while.", Scenarios: []Scenario{
			{Name: "Valid", Steps: []ScenarioStep{{Keyword: "WHEN", Text: "creds valid"}}},
		}},
	}}}); err != nil {
		t.Fatalf("failed to store requirements: %v", err)
	}

	issues, err := ValidateChangeRequirements(database, projectID, c.ID)
	if err != nil {
		t.Fatalf("failed to validate: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues (RFC2119 + dropped scenario), got %d: %+v", len(issues), issues)
	}
	foundDrop := false
	foundRFC := false
	for _, it := range issues {
		if strings.Contains(it.Message, "drops scenario") && strings.Contains(it.Message, "Invalid") {
			foundDrop = true
		}
		if strings.Contains(it.Message, "RFC 2119") {
			foundRFC = true
		}
	}
	if !foundDrop || !foundRFC {
		t.Errorf("expected both validation warnings, got %+v", issues)
	}
}

func TestContextPackSurfacesCapabilities(t *testing.T) {
	database, projectID, _ := setupSpecReqDB(t)

	// A canonical code node for the path.
	if _, err := database.Exec(
		"INSERT INTO graph_nodes (id, project_id, node_type, label, path, metadata) VALUES (?, ?, 'file', ?, ?, '{}')",
		"internal/auth/auth.go", projectID, "auth.go", "internal/auth/auth.go",
	); err != nil {
		t.Fatalf("seed code node: %v", err)
	}

	// Capability state + graph edge (code implements capability).
	if err := MergeDeltas(database, projectID, "auth", []Delta{{Op: DeltaAdded, Requirements: []Requirement{
		{Name: "Login", Body: "The system SHALL log users in."},
		{Name: "Logout", Body: "The system SHALL end sessions."},
	}}}); err != nil {
		t.Fatalf("failed to seed capability: %v", err)
	}
	if err := graph.EnsureSpecCapabilityEdges(database, projectID, graph.SpecCapabilityRef{CapabilityPath: "auth", WherePath: "internal/auth/auth.go"}); err != nil {
		t.Fatalf("failed to wire capability: %v", err)
	}

	pack, err := GetContextPack(database, projectID, "internal/auth/auth.go", 5, false)
	if err != nil {
		t.Fatalf("failed to get context pack: %v", err)
	}
	if len(pack.Capabilities) != 1 {
		t.Fatalf("expected 1 capability surfaced, got %d: %+v", len(pack.Capabilities), pack.Capabilities)
	}
	cap := pack.Capabilities[0]
	if cap.CapabilityPath != "auth" {
		t.Errorf("expected capability auth, got %q", cap.CapabilityPath)
	}
	if cap.RequirementCount != 2 {
		t.Errorf("expected 2 requirements, got %d", cap.RequirementCount)
	}
	if len(cap.Requirements) != 2 {
		t.Errorf("expected 2 requirement names, got %v", cap.Requirements)
	}

	rendered := RenderContextPack(pack, 200)
	if !strings.Contains(rendered, "Capabilities implemented here") || !strings.Contains(rendered, "auth") {
		t.Errorf("expected capabilities section in render, got:\n%s", rendered)
	}
}

func TestReplaceAndLoadChangeRequirements(t *testing.T) {
	database, projectID, _ := setupSpecReqDB(t)
	c, err := CreateChange(database, projectID, "add-2fa", "Add 2FA", "", "", "internal/auth/", "", "")
	if err != nil {
		t.Fatalf("failed to create change: %v", err)
	}

	deltas := []Delta{
		{Op: DeltaAdded, Requirements: []Requirement{
			{Name: "Two-Factor Authentication", Body: "The system MUST support TOTP.", Scenarios: []Scenario{
				{Name: "Enroll", Steps: []ScenarioStep{
					{Keyword: "GIVEN", Text: "a user without 2FA"},
					{Keyword: "WHEN", Text: "the user enables 2FA"},
					{Keyword: "THEN", Text: "a QR code is shown"},
				}},
			}},
		}},
		{Op: DeltaRemoved, Requirements: []Requirement{{Name: "Legacy Login"}}},
	}
	if err = ReplaceChangeRequirements(database, projectID, c.ID, "auth", deltas); err != nil {
		t.Fatalf("failed to replace requirements: %v", err)
	}

	loaded, err := LoadChangeDeltas(database, projectID, c.ID)
	if err != nil {
		t.Fatalf("failed to load deltas: %v", err)
	}
	if !reflect.DeepEqual(loaded, deltas) {
		t.Errorf("delta round-trip mismatch:\ngot  %+v\nwant %+v", loaded, deltas)
	}

	rows, err := ListChangeRequirements(database, projectID, c.ID)
	if err != nil {
		t.Fatalf("failed to list rows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].CapabilityPath != "auth" {
		t.Errorf("expected capability path auth, got %q", rows[0].CapabilityPath)
	}
	if !strings.Contains(rows[0].RFC2119, "MUST") {
		t.Errorf("expected MUST in rfc2119, got %q", rows[0].RFC2119)
	}
	if rows[0].SortOrder >= rows[1].SortOrder {
		t.Errorf("expected ascending sort order, got %d then %d", rows[0].SortOrder, rows[1].SortOrder)
	}
}

func TestMergeDeltasLifecycle(t *testing.T) {
	database, projectID, _ := setupSpecReqDB(t)
	const cap = "auth"

	added := []Delta{{Op: DeltaAdded, Requirements: []Requirement{
		{Name: "Login", Body: "The system SHALL issue a session token.", Scenarios: []Scenario{
			{Name: "Valid", Steps: []ScenarioStep{{Keyword: "GIVEN", Text: "valid creds"}, {Keyword: "THEN", Text: "token issued"}}},
			{Name: "Invalid", Steps: []ScenarioStep{{Keyword: "GIVEN", Text: "bad creds"}, {Keyword: "THEN", Text: "error shown"}}},
		}},
		{Name: "Logout", Body: "The system SHALL end the session."},
	}}}
	if err := MergeDeltas(database, projectID, cap, added); err != nil {
		t.Fatalf("failed to add: %v", err)
	}

	reqs, err := CapabilityRequirements(database, projectID, cap)
	if err != nil {
		t.Fatalf("failed to list: %v", err)
	}
	if len(reqs) != 2 {
		t.Fatalf("expected 2 requirements after ADDED, got %d", len(reqs))
	}
	if len(reqs[0].Scenarios) != 2 {
		t.Errorf("expected 2 scenarios, got %d", len(reqs[0].Scenarios))
	}

	// MODIFIED replaces the whole requirement block: dropping a scenario removes it.
	modified := []Delta{{Op: DeltaModified, Requirements: []Requirement{
		{Name: "Login", Body: "The system SHALL issue a session token after 2FA.", Scenarios: []Scenario{
			{Name: "Valid", Steps: []ScenarioStep{{Keyword: "WHEN", Text: "creds and 2FA are valid"}, {Keyword: "THEN", Text: "token issued"}}},
		}},
	}}}
	if err = MergeDeltas(database, projectID, cap, modified); err != nil {
		t.Fatalf("failed to modify: %v", err)
	}
	reqs, err = CapabilityRequirements(database, projectID, cap)
	if err != nil {
		t.Fatalf("failed to list after modify: %v", err)
	}
	var login *CapabilityRequirement
	for i := range reqs {
		if reqs[i].Requirement == "Login" {
			login = &reqs[i]
		}
	}
	if login == nil {
		t.Fatal("expected Login requirement after MODIFIED")
	}
	if !strings.Contains(login.Body, "after 2FA") {
		t.Errorf("expected replaced body, got %q", login.Body)
	}
	if len(login.Scenarios) != 1 || login.Scenarios[0].Name != "Valid" {
		t.Errorf("MODIFIED must drop scenarios not listed, got %+v", login.Scenarios)
	}

	// RENAMED renames the header; the MODIFIED upsert on the new name works.
	if err = MergeDeltas(database, projectID, cap, []Delta{{Op: DeltaRenamed, Requirements: []Requirement{
		{Name: "Logout", RenameTo: "Sign Out"},
	}}}); err != nil {
		t.Fatalf("failed to rename: %v", err)
	}
	reqs, _ = CapabilityRequirements(database, projectID, cap)
	names := map[string]bool{}
	for _, r := range reqs {
		names[r.Requirement] = true
	}
	if !names["Sign Out"] || names["Logout"] {
		t.Errorf("expected Sign Out after rename, got %v", names)
	}

	// REMOVED deletes the renamed requirement.
	if err = MergeDeltas(database, projectID, cap, []Delta{{Op: DeltaRemoved, Requirements: []Requirement{
		{Name: "Sign Out"},
	}}}); err != nil {
		t.Fatalf("failed to remove: %v", err)
	}
	reqs, _ = CapabilityRequirements(database, projectID, cap)
	if len(reqs) != 1 || reqs[0].Requirement != "Login" {
		t.Errorf("expected only Login after REMOVED, got %+v", reqs)
	}
}

func TestMergeDeltasRenameThenModify(t *testing.T) {
	database, projectID, _ := setupSpecReqDB(t)
	const cap = "auth"
	if err := MergeDeltas(database, projectID, cap, []Delta{{Op: DeltaAdded, Requirements: []Requirement{
		{Name: "Session", Body: "The system SHALL track sessions."},
	}}}); err != nil {
		t.Fatalf("failed to seed: %v", err)
	}

	deltas := []Delta{
		{Op: DeltaRenamed, Requirements: []Requirement{{Name: "Session", RenameTo: "Active Session"}}},
		{Op: DeltaModified, Requirements: []Requirement{
			{Name: "Active Session", Body: "The system SHALL expire sessions after 30 minutes.", Scenarios: []Scenario{
				{Name: "Idle", Steps: []ScenarioStep{{Keyword: "GIVEN", Text: "an idle session"}, {Keyword: "THEN", Text: "it is invalidated"}}},
			}},
		}},
	}
	if err := MergeDeltas(database, projectID, cap, deltas); err != nil {
		t.Fatalf("failed to rename+modify: %v", err)
	}
	reqs, _ := CapabilityRequirements(database, projectID, cap)
	if len(reqs) != 1 || reqs[0].Requirement != "Active Session" {
		t.Fatalf("expected a single Active Session requirement, got %+v", reqs)
	}
	if len(reqs[0].Scenarios) != 1 {
		t.Errorf("expected MODIFIED scenarios to apply to the renamed requirement, got %+v", reqs[0].Scenarios)
	}
}

func TestMergeDeltasConflicts(t *testing.T) {
	database, projectID, _ := setupSpecReqDB(t)
	const cap = "auth"
	if err := MergeDeltas(database, projectID, cap, []Delta{{Op: DeltaAdded, Requirements: []Requirement{
		{Name: "Login", Body: "The system SHALL log users in."},
	}}}); err != nil {
		t.Fatalf("failed to seed: %v", err)
	}

	// ADDED of an existing name must error (use MODIFIED instead).
	if err := MergeDeltas(database, projectID, cap, []Delta{{Op: DeltaAdded, Requirements: []Requirement{
		{Name: "Login", Body: "duplicate"},
	}}}); err == nil {
		t.Error("expected ADDED conflict error for an existing requirement")
	}

	// RENAMED of a missing requirement must error.
	if err := MergeDeltas(database, projectID, cap, []Delta{{Op: DeltaRenamed, Requirements: []Requirement{
		{Name: "Ghost", RenameTo: "Phantom"},
	}}}); err == nil {
		t.Error("expected rename error for a missing requirement")
	}
}

func TestChangeCapabilityPathDefaultAndSetter(t *testing.T) {
	database, projectID, _ := setupSpecReqDB(t)
	c, err := CreateChange(database, projectID, "slug-x", "Title", "", "", "", "", "")
	if err != nil {
		t.Fatalf("failed to create change: %v", err)
	}
	if c.CapabilityPath != "slug-x" {
		t.Errorf("expected default capability path to equal slug, got %q", c.CapabilityPath)
	}
	updated, err := SetChangeCapabilityPath(database, projectID, c.ID, "custom-cap")
	if err != nil {
		t.Fatalf("failed to set capability path: %v", err)
	}
	if updated.CapabilityPath != "custom-cap" {
		t.Errorf("expected custom capability path, got %q", updated.CapabilityPath)
	}
}

func TestWriteCapabilityMirrorAndOrphanRemoval(t *testing.T) {
	database, projectID, projPath := setupSpecReqDB(t)

	if err := MergeDeltas(database, projectID, "auth", []Delta{{Op: DeltaAdded, Requirements: []Requirement{
		{Name: "Login", Body: "The system SHALL log users in.", Scenarios: []Scenario{
			{Name: "Valid", Steps: []ScenarioStep{{Keyword: "WHEN", Text: "creds are valid"}, {Keyword: "THEN", Text: "logged in"}}},
		}},
	}}}); err != nil {
		t.Fatalf("failed to seed capability: %v", err)
	}
	if err := WriteSpecMirror(database, projectID, projPath); err != nil {
		t.Fatalf("failed to write mirror: %v", err)
	}

	specPath := filepath.Join(projPath, ".sv-memory", "specs", "capabilities", "auth", "spec.md")
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("expected capability mirror file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "# auth Specification") || !strings.Contains(content, "### Requirement: Login") {
		t.Errorf("capability mirror content unexpected:\n%s", content)
	}
	if !strings.Contains(content, "#### Scenario: Valid") {
		t.Errorf("expected scenario in capability mirror:\n%s", content)
	}

	// Removing every requirement retires the capability and its mirror dir.
	if err := MergeDeltas(database, projectID, "auth", []Delta{{Op: DeltaRemoved, Requirements: []Requirement{
		{Name: "Login"},
	}}}); err != nil {
		t.Fatalf("failed to remove: %v", err)
	}
	if err := WriteSpecMirror(database, projectID, projPath); err != nil {
		t.Fatalf("failed to rewrite mirror: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projPath, ".sv-memory", "specs", "capabilities", "auth")); err == nil {
		t.Error("expected orphan capability dir to be removed")
	}
}

func TestWriteSpecMirrorWithDeltasAndImport(t *testing.T) {
	database, projectID, projPath := setupSpecReqDB(t)
	c, err := CreateChange(database, projectID, "workflow-rules", "Workflow Rules", "Proposal body", "", "AGENTS.md", "", "")
	if err != nil {
		t.Fatalf("failed to create change: %v", err)
	}
	if _, err = SetChangeCapabilityPath(database, projectID, c.ID, "workflow"); err != nil {
		t.Fatalf("failed to set capability: %v", err)
	}
	if err = ReplaceChangeRequirements(database, projectID, c.ID, "workflow", []Delta{{Op: DeltaAdded, Requirements: []Requirement{
		{Name: "Commit Format", Body: "Commits SHALL follow Conventional Commits.", Scenarios: []Scenario{
			{Name: "Valid commit", Steps: []ScenarioStep{{Keyword: "WHEN", Text: "a commit is authored"}, {Keyword: "THEN", Text: "it uses type(scope)"}}},
		}},
	}}}); err != nil {
		t.Fatalf("failed to store requirements: %v", err)
	}

	if err = WriteSpecMirror(database, projectID, projPath); err != nil {
		t.Fatalf("failed to write mirror: %v", err)
	}
	mirrorPath := filepath.Join(projPath, ".sv-memory", "specs", "changes", "workflow-rules.md")
	data, err := os.ReadFile(mirrorPath)
	if err != nil {
		t.Fatalf("expected change mirror: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "- **Capability:** `workflow`") {
		t.Errorf("expected capability line in mirror:\n%s", content)
	}
	if !strings.Contains(content, "## ADDED Requirements") || !strings.Contains(content, "### Requirement: Commit Format") {
		t.Errorf("expected delta sections in mirror:\n%s", content)
	}

	// Human edits the requirement body; import reconciles it back.
	edited := strings.Replace(content, "Conventional Commits.", "Conventional Commits with scope.", 1)
	if err = os.WriteFile(mirrorPath, []byte(edited), 0644); err != nil {
		t.Fatalf("failed to edit mirror: %v", err)
	}
	if _, err = ImportChangeFromMarkdown(database, projectID, projPath, "workflow-rules"); err != nil {
		t.Fatalf("failed to import mirror: %v", err)
	}
	loaded, err := LoadChangeDeltas(database, projectID, c.ID)
	if err != nil {
		t.Fatalf("failed to load deltas: %v", err)
	}
	if len(loaded) != 1 || len(loaded[0].Requirements) != 1 {
		t.Fatalf("expected 1 delta with 1 requirement, got %+v", loaded)
	}
	if !strings.Contains(loaded[0].Requirements[0].Body, "with scope") {
		t.Errorf("expected reconciled body, got %q", loaded[0].Requirements[0].Body)
	}
}
