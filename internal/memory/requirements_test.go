package memory

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/svtech-code/sv-memory/internal/db"
)

func TestParseSpecDeltasSections(t *testing.T) {
	md := `# Delta for Auth

## Purpose
Some prose the parser must skip.

## ADDED Requirements

### Requirement: Two-Factor Authentication
The system MUST support TOTP-based two-factor authentication.

#### Scenario: 2FA enrollment
- **GIVEN** a user without 2FA enabled
- **WHEN** the user enables 2FA in settings
- **THEN** a QR code is displayed

#### Scenario: 2FA login
- GIVEN a user with 2FA enabled
- WHEN the user submits valid credentials
- THEN an OTP challenge is presented

## MODIFIED Requirements

### Requirement: Session Expiration
The system SHALL expire sessions after 15 minutes of inactivity.

#### Scenario: Idle timeout
- **GIVEN** an authenticated session
- **WHEN** 15 minutes pass
- **THEN** the session is invalidated

## REMOVED Requirements

### Requirement: Remember Me
Deprecated in favor of 2FA.
`

	deltas := ParseSpecDeltas(md)
	if len(deltas) != 3 {
		t.Fatalf("expected 3 deltas, got %d", len(deltas))
	}
	if deltas[0].Op != DeltaAdded || deltas[1].Op != DeltaModified || deltas[2].Op != DeltaRemoved {
		t.Fatalf("expected [ADDED MODIFIED REMOVED], got %v", deltaOps(deltas))
	}

	added := deltas[0].Requirements
	if len(added) != 1 {
		t.Fatalf("expected 1 added requirement, got %d", len(added))
	}
	r := added[0]
	if r.Name != "Two-Factor Authentication" {
		t.Errorf("expected name %q, got %q", "Two-Factor Authentication", r.Name)
	}
	if !strings.Contains(r.Body, "MUST support") {
		t.Errorf("expected body with MUST, got %q", r.Body)
	}
	if len(r.Scenarios) != 2 {
		t.Fatalf("expected 2 scenarios, got %d", len(r.Scenarios))
	}
	first := r.Scenarios[0]
	if len(first.Steps) != 3 {
		t.Fatalf("expected 3 steps in first scenario, got %d", len(first.Steps))
	}
	if first.Steps[0].Keyword != "GIVEN" || first.Steps[0].Text != "a user without 2FA enabled" {
		t.Errorf("unexpected first step: %+v", first.Steps[0])
	}

	// Bare (non-bold) keyword form in the second scenario.
	if r.Scenarios[1].Steps[0].Keyword != "GIVEN" {
		t.Errorf("expected bare GIVEN keyword, got %q", r.Scenarios[1].Steps[0].Keyword)
	}
}

func TestParseSpecDeltasRenamed(t *testing.T) {
	md := `## RENAMED Requirements

- **FROM:** ` + "`Session Authentication`" + `
- **TO:** ` + "`Auth Session Validation`" + `

- FROM: Legacy Login
- TO: Deprecated Login
`

	deltas := ParseSpecDeltas(md)
	if len(deltas) != 1 || deltas[0].Op != DeltaRenamed {
		t.Fatalf("expected a single RENAMED delta, got %v", deltaOps(deltas))
	}
	reqs := deltas[0].Requirements
	if len(reqs) != 2 {
		t.Fatalf("expected 2 renames, got %d", len(reqs))
	}
	if reqs[0].Name != "Session Authentication" || reqs[0].RenameTo != "Auth Session Validation" {
		t.Errorf("unexpected first rename: %+v", reqs[0])
	}
	if reqs[1].Name != "Legacy Login" || reqs[1].RenameTo != "Deprecated Login" {
		t.Errorf("unexpected second rename: %+v", reqs[1])
	}
}

func TestParseSpecDeltasLenient(t *testing.T) {
	md := `## Unknown Section
### Not a Requirement: ignore

## ADDED Requirements

### Requirement: Has scenarios
Body line.

#### Not a Scenario: skip
- not a step
`

	deltas := ParseSpecDeltas(md)
	if len(deltas) != 1 {
		t.Fatalf("expected 1 delta, got %d", len(deltas))
	}
	if deltas[0].Op != DeltaAdded {
		t.Fatalf("expected ADDED, got %s", deltas[0].Op)
	}
	if len(deltas[0].Requirements) != 1 {
		t.Fatalf("expected 1 requirement, got %d", len(deltas[0].Requirements))
	}
	if deltas[0].Requirements[0].Body != "Body line." {
		t.Errorf("expected body %q, got %q", "Body line.", deltas[0].Requirements[0].Body)
	}
}

func TestDeltasToMarkdownRoundTrip(t *testing.T) {
	original := []Delta{
		{Op: DeltaAdded, Requirements: []Requirement{
			{Name: "Theme Selection", Body: "The app SHALL let users switch themes.", Scenarios: []Scenario{
				{Name: "Toggle", Steps: []ScenarioStep{
					{Keyword: "WHEN", Text: "the user clicks the toggle"},
					{Keyword: "THEN", Text: "the theme switches"},
				}},
			}},
		}},
		{Op: DeltaRenamed, Requirements: []Requirement{
			{Name: "Old Name", RenameTo: "New Name"},
		}},
		{Op: DeltaRemoved, Requirements: []Requirement{
			{Name: "Legacy", Body: "Deprecated."},
		}},
	}

	md := DeltasToMarkdown(original)
	parsed := ParseSpecDeltas(md)
	if !reflect.DeepEqual(parsed, original) {
		t.Errorf("round-trip mismatch.\nrendered:\n%s\ngot: %+v\nwant: %+v", md, parsed, original)
	}
}

func TestExtractRFC2119(t *testing.T) {
	cases := []struct {
		body string
		want []string
	}{
		{"The system SHALL expire sessions after 30 minutes.", []string{"SHALL"}},
		{"The system MUST NOT leak tokens and SHOULD retry.", []string{"MUST NOT", "SHOULD"}},
		{"The system MAY cache results.", []string{"MAY"}},
		{"MUSTACHE is not a keyword.", nil},
		{"No normative language here.", nil},
	}
	for _, tc := range cases {
		got := ExtractRFC2119(tc.body)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ExtractRFC2119(%q) = %v, want %v", tc.body, got, tc.want)
		}
	}
}

func TestDeltaOpValid(t *testing.T) {
	for _, op := range []string{DeltaAdded, DeltaModified, DeltaRemoved, DeltaRenamed} {
		if !DeltaOpValid(op) {
			t.Errorf("expected %q to be valid", op)
		}
	}
	if DeltaOpValid("CHANGED") {
		t.Error("expected CHANGED to be invalid")
	}
}

func TestSpecRequirementsTablesExist(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "spec_req.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	for _, table := range []string{"spec_requirements", "spec_capabilities", "spec_requirements_fts"} {
		var n int
		if err = database.QueryRow(
			"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name = ?", table).Scan(&n); err != nil {
			t.Fatalf("query %s: %v", table, err)
		}
		if n != 1 {
			t.Errorf("expected table %s to exist", table)
		}
	}
}

func deltaOps(deltas []Delta) []string {
	out := make([]string, len(deltas))
	for i, d := range deltas {
		out[i] = d.Op
	}
	return out
}
