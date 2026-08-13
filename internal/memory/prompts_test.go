package memory

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/svtech-code/sv-memory/internal/db"
)

func setupPromptsTest(t *testing.T) (string, *sql.DB, string) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "sv-mem-prompts")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	dbPath := filepath.Join(tempDir, "prompts.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	projectID := "prompts-proj"
	if err := db.RegisterProject(database, projectID, "Prompts", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}
	t.Cleanup(func() {
		database.Close()
		os.RemoveAll(tempDir)
	})
	return tempDir, database, projectID
}

func TestSavePromptAndRecentPrompts(t *testing.T) {
	_, dbc, projectID := setupPromptsTest(t)

	p, err := SavePrompt(dbc, projectID, "sess-1", "Refactor the auth middleware to use JWT.")
	if err != nil {
		t.Fatalf("failed to save prompt: %v", err)
	}
	if p.ID == "" {
		t.Error("expected a non-empty prompt ID")
	}
	if !strings.Contains(p.Content, "JWT") {
		t.Error("expected prompt content preserved")
	}

	// Scoped to the session.
	prompts, err := RecentPrompts(dbc, projectID, "sess-1", 10)
	if err != nil {
		t.Fatalf("failed to query prompts: %v", err)
	}
	if len(prompts) != 1 {
		t.Fatalf("expected 1 prompt for sess-1, got %d", len(prompts))
	}

	// Another session's prompt is not returned when scoped.
	if _, err = SavePrompt(dbc, projectID, "sess-2", "Document the CLI flags."); err != nil {
		t.Fatalf("failed to save second prompt: %v", err)
	}
	prompts, err = RecentPrompts(dbc, projectID, "sess-1", 10)
	if err != nil {
		t.Fatalf("failed to query prompts: %v", err)
	}
	if len(prompts) != 1 {
		t.Fatalf("expected still 1 prompt for sess-1, got %d", len(prompts))
	}

	// Unscoped returns newest first.
	prompts, err = RecentPrompts(dbc, projectID, "", 10)
	if err != nil {
		t.Fatalf("failed to query all prompts: %v", err)
	}
	if len(prompts) != 2 {
		t.Fatalf("expected 2 prompts unscoped, got %d", len(prompts))
	}
	if prompts[0].SessionID != "sess-2" {
		t.Errorf("expected newest prompt (sess-2) first, got %s", prompts[0].SessionID)
	}

	// Count reflects both.
	count, err := CountPrompts(dbc, projectID)
	if err != nil {
		t.Fatalf("failed to count prompts: %v", err)
	}
	if count != 2 {
		t.Errorf("expected prompt count 2, got %d", count)
	}
}

func TestSavePromptRejectsEmptyAndSanitizesSecrets(t *testing.T) {
	_, dbc, projectID := setupPromptsTest(t)

	if _, err := SavePrompt(dbc, projectID, "sess-1", "   "); err == nil {
		t.Error("expected empty prompt to be rejected")
	}

	p, err := SavePrompt(dbc, projectID, "sess-1", "Set the API key to sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789-_ABCDEF secret.")
	if err != nil {
		t.Fatalf("failed to save prompt: %v", err)
	}
	if strings.Contains(p.Content, "sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789-_ABCDEF") {
		t.Error("expected prompt secrets to be redacted")
	}
	if !strings.Contains(p.Content, "[REDACTED_SECRET]") {
		t.Error("expected redaction marker in prompt content")
	}
}

func TestStatsIncludePrompts(t *testing.T) {
	_, dbc, projectID := setupPromptsTest(t)

	stats, err := GetStats(dbc, projectID)
	if err != nil {
		t.Fatalf("failed to get stats: %v", err)
	}
	if stats.TotalPrompts != 0 {
		t.Errorf("expected 0 prompts in fresh stats, got %d", stats.TotalPrompts)
	}

	if _, err = SavePrompt(dbc, projectID, "sess-1", "A user prompt."); err != nil {
		t.Fatalf("failed to save prompt: %v", err)
	}
	stats, err = GetStats(dbc, projectID)
	if err != nil {
		t.Fatalf("failed to get stats: %v", err)
	}
	if stats.TotalPrompts != 1 {
		t.Errorf("expected 1 prompt in stats, got %d", stats.TotalPrompts)
	}
}
