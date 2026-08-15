package memory

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/svtech-code/sv-memory/internal/db"
)

func newPreflightDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "preflight.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	projectID := "proj-preflight"
	if err = db.RegisterProject(database, projectID, "Preflight", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}
	return database, projectID
}

func saveRule(t *testing.T, database *sql.DB, projectID, category, what string, pinned bool) string {
	t.Helper()
	mem := &Memory{
		ProjectID: projectID,
		Category:  category,
		What:      what,
		Why:       "test rule",
		Learned:   "test",
		Pinned:    pinned,
	}
	saved, err := SaveMemory(database, mem)
	if err != nil {
		t.Fatalf("failed to save rule: %v", err)
	}
	return saved.ID
}

func TestPreflightCheckPass(t *testing.T) {
	database, projectID := newPreflightDB(t)
	saveRule(t, database, projectID, "standard", "Database migrations must be backward compatible", false)

	result, err := PreflightCheck(database, projectID, "Add session-based auth", "Introduce server-side sessions with JWT refresh tokens")
	if err != nil {
		t.Fatalf("failed preflight: %v", err)
	}
	if result.Verdict != PreflightPass {
		t.Errorf("expected PASS, got %s", result.Verdict)
	}
	if len(result.Issues) != 0 {
		t.Errorf("expected no issues, got %d", len(result.Issues))
	}
}

func TestPreflightCheckWarnOnOverlap(t *testing.T) {
	database, projectID := newPreflightDB(t)
	saveRule(t, database, projectID, "standard", "Authentication uses stateless JWT with server-side sessions support", false)

	result, err := PreflightCheck(database, projectID, "Implement session-based auth", "Authentication using stateless JWT with server-side sessions")
	if err != nil {
		t.Fatalf("failed preflight: %v", err)
	}
	if result.Verdict != PreflightWarn {
		t.Errorf("expected WARN for overlapping standard, got %s", result.Verdict)
	}
	if len(result.Issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(result.Issues))
	}
	if result.Issues[0].Severity != PreflightWarn {
		t.Errorf("expected WARN severity, got %s", result.Issues[0].Severity)
	}
}

func TestPreflightCheckBlockOnPinnedInvariant(t *testing.T) {
	database, projectID := newPreflightDB(t)
	ruleID := saveRule(t, database, projectID, "architecture", "Stateless JWT authentication with server-side sessions is a pinned invariant", true)

	result, err := PreflightCheck(database, projectID, "Implement session-based auth", "Authentication using stateless JWT with server-side sessions")
	if err != nil {
		t.Fatalf("failed preflight: %v", err)
	}
	if result.Verdict != PreflightBlock {
		t.Errorf("expected BLOCK for pinned invariant, got %s", result.Verdict)
	}
	if len(result.Issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(result.Issues))
	}
	if result.Issues[0].Severity != PreflightBlock {
		t.Errorf("expected BLOCK severity, got %s", result.Issues[0].Severity)
	}
	if result.Issues[0].MemoryID != ruleID {
		t.Errorf("expected rule %s, got %s", ruleID, result.Issues[0].MemoryID)
	}
}

func TestPreflightCheckBlockDominatesWarn(t *testing.T) {
	database, projectID := newPreflightDB(t)
	saveRule(t, database, projectID, "standard", "Authentication with stateless JWT server-side sessions", false)
	saveRule(t, database, projectID, "standard", "Stateless JWT authentication with server-side sessions is a pinned invariant", true)

	result, err := PreflightCheck(database, projectID, "Session auth", "Authentication using stateless JWT with server-side sessions")
	if err != nil {
		t.Fatalf("failed preflight: %v", err)
	}
	if result.Verdict != PreflightBlock {
		t.Errorf("expected BLOCK to dominate WARN, got %s", result.Verdict)
	}
	blockCount := 0
	for _, it := range result.Issues {
		if it.Severity == PreflightBlock {
			blockCount++
		}
	}
	if blockCount < 1 {
		t.Error("expected at least one BLOCK issue")
	}
}

func TestPreflightCheckIgnoresJournalCategory(t *testing.T) {
	database, projectID := newPreflightDB(t)
	saveRule(t, database, projectID, "journal", "Stateless JWT authentication with server-side sessions notes for later review", false)

	result, err := PreflightCheck(database, projectID, "Implement session-based auth", "Authentication using stateless JWT with server-side sessions")
	if err != nil {
		t.Fatalf("failed preflight: %v", err)
	}
	// Journals are transient and must not act as invariants.
	if result.Verdict != PreflightPass {
		t.Errorf("expected PASS (journal ignored), got %s", result.Verdict)
	}
}

func TestPreflightCheckEmptyTokensPasses(t *testing.T) {
	database, projectID := newPreflightDB(t)
	saveRule(t, database, projectID, "standard", "Some unrelated rule", false)

	result, err := PreflightCheck(database, projectID, "", "")
	if err != nil {
		t.Fatalf("failed preflight: %v", err)
	}
	if result.Verdict != PreflightPass {
		t.Errorf("expected PASS for empty proposal, got %s", result.Verdict)
	}
}

func TestPreflightCheckFilterByProject(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "preflight_proj.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	pA := "proj-preflight-a"
	pB := "proj-preflight-b"
	if err = db.RegisterProject(database, pA, "A", filepath.Join(tempDir, "a")); err != nil {
		t.Fatalf("failed to register A: %v", err)
	}
	if err = db.RegisterProject(database, pB, "B", filepath.Join(tempDir, "b")); err != nil {
		t.Fatalf("failed to register B: %v", err)
	}
	saveRule(t, database, pA, "architecture", "Stateless JWT authentication with server-side sessions is a pinned invariant", true)

	result, err := PreflightCheck(database, pB, "Implement session-based auth", "Authentication using stateless JWT with server-side sessions")
	if err != nil {
		t.Fatalf("failed preflight: %v", err)
	}
	if result.Verdict != PreflightPass {
		t.Errorf("expected PASS in project B (rule lives in A), got %s", result.Verdict)
	}
}

func TestSemanticPreflightFailsOpen(t *testing.T) {
	database, projectID := newPreflightDB(t)
	saveRule(t, database, projectID, "standard", "Authentication uses stateless JWT with server-side sessions support", false)

	original := SemanticRunAgent
	defer func() { SemanticRunAgent = original }()
	SemanticRunAgent = func(ctx context.Context, agent, prompt string) (string, error) {
		return "", errors.New("agent unavailable")
	}

	result, err := SemanticPreflight(context.Background(), database, projectID, "Implement session-based auth", "Authentication using stateless JWT with server-side sessions", "")
	if err != nil {
		t.Fatalf("failed semantic preflight: %v", err)
	}
	// Fail-open: deterministic verdict must be returned unchanged.
	if result.Verdict != PreflightWarn {
		t.Errorf("expected deterministic WARN after fail-open, got %s", result.Verdict)
	}
}

func TestChangeStatsAndHintData(t *testing.T) {
	database, projectID := newPreflightDB(t)

	stats, err := ChangeStats(database, projectID)
	if err != nil {
		t.Fatalf("failed change stats: %v", err)
	}
	if stats[ChangeStatusDraft] != 0 {
		t.Errorf("expected 0 drafts, got %d", stats[ChangeStatusDraft])
	}

	c, err := CreateChange(database, projectID, "slug", "Title", "", "", "", "", "")
	if err != nil {
		t.Fatalf("failed to create change: %v", err)
	}
	if _, err = UpdateChangeStatus(database, projectID, c.ID, ChangeStatusProposed); err != nil {
		t.Fatalf("failed to propose: %v", err)
	}
	if _, err = UpdateChangeStatus(database, projectID, c.ID, ChangeStatusArchived); err != nil {
		t.Fatalf("failed to archive: %v", err)
	}

	stats, err = ChangeStats(database, projectID)
	if err != nil {
		t.Fatalf("failed change stats after archive: %v", err)
	}
	total := stats[ChangeStatusDraft] + stats[ChangeStatusProposed] + stats[ChangeStatusValidated] + stats[ChangeStatusApplied]
	if total != 0 {
		t.Errorf("expected 0 active changes after archive, got %d", total)
	}
	if !strings.Contains("draft proposed validated applied", strings.Join([]string{ChangeStatusDraft, ChangeStatusProposed, ChangeStatusValidated, ChangeStatusApplied}, " ")) {
		t.Fatal("status constants sanity check failed")
	}
}
