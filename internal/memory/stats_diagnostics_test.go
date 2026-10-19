package memory

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/svtech-code/sv-memory/internal/db"
)

func TestRunDiagnostics(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-diagnose-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_storage.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-diagnose"
	err = db.RegisterProject(database, projectID, "Diagnose Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Run diagnostics on a healthy setup
	results := RunDiagnostics(database, projectID, tempDir, dbPath)
	if len(results) == 0 {
		t.Fatal("expected at least one diagnostic result")
	}

	var passCount, failCount int
	for _, r := range results {
		switch r.Status {
		case "pass":
			passCount++
		case "fail":
			failCount++
			t.Errorf("unexpected failure: [%s] %s — %s", r.Check, r.Message, r.Status)
		}
	}
	if passCount == 0 {
		t.Error("expected at least one pass result")
	}

	// Ensure key checks pass
	checkMap := make(map[string]string)
	for _, r := range results {
		checkMap[r.Check] = r.Status
	}
	expectedPass := []string{
		"database_file", "database_connection",
		"table_projects", "table_memories",
		"table_sessions", "table_memory_relations",
		"trigger_memories_ai", "trigger_memories_ad", "trigger_memories_au",
		"project_registered", "fts5_healthy",
	}
	for _, chk := range expectedPass {
		if checkMap[chk] != "pass" {
			t.Errorf("expected check %s to pass, got %s", chk, checkMap[chk])
		}
	}
}

func TestGetStats(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-stats-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_storage.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-stats"
	err = db.RegisterProject(database, projectID, "Stats Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Empty project stats
	s, err := GetStats(database, projectID)
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}
	if s.TotalMemories != 0 {
		t.Errorf("expected 0 total memories, got %d", s.TotalMemories)
	}
	if len(s.ByCategory) != 0 {
		t.Errorf("expected empty by_category, got %v", s.ByCategory)
	}

	// Save a few memories
	mem1 := &Memory{ID: "s-1", ProjectID: projectID, Category: "architecture", What: "Arch 1", Why: "why1", Learned: "l1", CreatedAt: time.Now()}
	mem2 := &Memory{ID: "s-2", ProjectID: projectID, Category: "architecture", What: "Arch 2", Why: "why2", Learned: "l2", CreatedAt: time.Now()}
	mem3 := &Memory{ID: "s-3", ProjectID: projectID, Category: "bugfix", What: "Bug 1", Why: "why3", Learned: "l3", CreatedAt: time.Now()}

	if _, saveErr := SaveMemory(database, mem1); saveErr != nil {
		t.Fatalf("failed saving mem1: %v", saveErr)
	}
	if _, saveErr := SaveMemory(database, mem2); saveErr != nil {
		t.Fatalf("failed saving mem2: %v", saveErr)
	}
	if _, saveErr := SaveMemory(database, mem3); saveErr != nil {
		t.Fatalf("failed saving mem3: %v", saveErr)
	}

	// Start a session
	_, err = StartSession(database, projectID, "Test session", tempDir)
	if err != nil {
		t.Fatalf("failed starting session: %v", err)
	}

	s, err = GetStats(database, projectID)
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}
	if s.TotalMemories != 3 {
		t.Errorf("expected 3 total memories, got %d", s.TotalMemories)
	}
	if s.ByCategory["architecture"] != 2 {
		t.Errorf("expected 2 architecture, got %d", s.ByCategory["architecture"])
	}
	if s.ByCategory["bugfix"] != 1 {
		t.Errorf("expected 1 bugfix, got %d", s.ByCategory["bugfix"])
	}
	if s.TotalSessions != 1 {
		t.Errorf("expected 1 total session, got %d", s.TotalSessions)
	}
	if s.ActiveSessions != 1 {
		t.Errorf("expected 1 active session, got %d", s.ActiveSessions)
	}
	if s.TotalRelations != 0 {
		t.Errorf("expected 0 relations, got %d", s.TotalRelations)
	}
	if s.Recent24h != 3 {
		t.Errorf("expected 3 recent memories, got %d", s.Recent24h)
	}
}
