package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/svtech-code/sv-memory/internal/db"
)

func TestListPruneConsolidateProjects(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-projects-test")
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

	// Register two projects
	err = db.RegisterProject(database, "proj-a", "Project A", filepath.Join(tempDir, "a"))
	if err != nil {
		t.Fatalf("failed to register proj-a: %v", err)
	}
	err = db.RegisterProject(database, "proj-b", "Project B", filepath.Join(tempDir, "b"))
	if err != nil {
		t.Fatalf("failed to register proj-b: %v", err)
	}
	err = db.RegisterProject(database, "proj-c", "Project C", filepath.Join(tempDir, "c"))
	if err != nil {
		t.Fatalf("failed to register proj-c: %v", err)
	}

	// Save a memory in proj-a
	_, err = SaveMemory(database, &Memory{
		ID: "pa-1", ProjectID: "proj-a", Category: "decision",
		What: "Test", Why: "why", Learned: "learned", CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("failed saving memory: %v", err)
	}

	// Test ListProjects
	projects, err := ListProjects(database)
	if err != nil {
		t.Fatalf("ListProjects failed: %v", err)
	}
	if len(projects) != 3 {
		t.Fatalf("expected 3 projects, got %d", len(projects))
	}

	// Check counts
	for _, p := range projects {
		switch p.ID {
		case "proj-a":
			if p.MemoryCount != 1 {
				t.Errorf("proj-a expected 1 memory, got %d", p.MemoryCount)
			}
		case "proj-b":
			if p.MemoryCount != 0 {
				t.Errorf("proj-b expected 0 memories, got %d", p.MemoryCount)
			}
		case "proj-c":
			if p.MemoryCount != 0 {
				t.Errorf("proj-c expected 0 memories, got %d", p.MemoryCount)
			}
		}
	}

	// Test PruneProjects (should remove proj-b and proj-c but not proj-a)
	pruned, err := PruneProjects(database)
	if err != nil {
		t.Fatalf("PruneProjects failed: %v", err)
	}
	if len(pruned) != 2 {
		t.Errorf("expected 2 pruned projects, got %d: %v", len(pruned), pruned)
	}

	// Verify proj-a still exists
	projects, err = ListProjects(database)
	if err != nil {
		t.Fatalf("ListProjects failed: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project after prune, got %d", len(projects))
	}
	if projects[0].ID != "proj-a" {
		t.Errorf("expected proj-a to remain, got %s", projects[0].ID)
	}

	// Create two more projects for consolidate test
	err = db.RegisterProject(database, "proj-x", "Project X", filepath.Join(tempDir, "x"))
	if err != nil {
		t.Fatalf("failed to register proj-x: %v", err)
	}
	err = db.RegisterProject(database, "proj-y", "Project Y", filepath.Join(tempDir, "y"))
	if err != nil {
		t.Fatalf("failed to register proj-y: %v", err)
	}

	// Save a memory in proj-x
	_, err = SaveMemory(database, &Memory{
		ID: "px-1", ProjectID: "proj-x", Category: "decision",
		What: "From X", Why: "why", Learned: "l", CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("failed saving memory in proj-x: %v", err)
	}

	// Test ConsolidateProjects: move from proj-x to proj-a
	mems, sess, err := ConsolidateProjects(database, "proj-x", "proj-a")
	if err != nil {
		t.Fatalf("ConsolidateProjects failed: %v", err)
	}
	if mems != 1 {
		t.Errorf("expected 1 memory moved, got %d", mems)
	}
	if sess != 0 {
		t.Errorf("expected 0 sessions moved, got %d", sess)
	}

	// Verify proj-x is gone and proj-a now has 2 memories
	var count int
	database.QueryRow("SELECT COUNT(*) FROM projects WHERE id='proj-x'").Scan(&count)
	if count != 0 {
		t.Error("expected proj-x to be deleted after consolidation")
	}
	database.QueryRow("SELECT COUNT(*) FROM memories WHERE project_id='proj-a' AND deleted_at IS NULL").Scan(&count)
	if count != 2 {
		t.Errorf("expected proj-a to have 2 memories, got %d", count)
	}
}

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

func TestExportImportJSON(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-export-test")
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

	projectID := "proj-export"
	err = db.RegisterProject(database, projectID, "Export Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Save some memories
	mem1 := &Memory{ID: "e-1", ProjectID: projectID, Category: "architecture", What: "Arch 1", Why: "why1", Learned: "l1", CreatedAt: time.Now()}
	mem2 := &Memory{ID: "e-2", ProjectID: projectID, Category: "decision", What: "Dec 2", Why: "why2", Learned: "l2", CreatedAt: time.Now()}
	if _, saveErr := SaveMemory(database, mem1); saveErr != nil {
		t.Fatalf("failed saving mem1: %v", saveErr)
	}
	if _, saveErr := SaveMemory(database, mem2); saveErr != nil {
		t.Fatalf("failed saving mem2: %v", saveErr)
	}

	// Export to JSON
	exportPath := filepath.Join(tempDir, "export.json")
	n, err := ExportJSON(database, projectID, exportPath)
	if err != nil {
		t.Fatalf("ExportJSON failed: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 exported memories, got %d", n)
	}

	// Delete from DB
	if _, execErr := database.Exec("DELETE FROM memories WHERE project_id = ?", projectID); execErr != nil {
		t.Fatalf("failed clearing memories: %v", execErr)
	}

	var count int
	database.QueryRow("SELECT COUNT(*) FROM memories WHERE project_id = ?", projectID).Scan(&count)
	if count != 0 {
		t.Fatal("expected empty memories table")
	}

	// Import from JSON
	n, err = ImportJSON(database, projectID, exportPath)
	if err != nil {
		t.Fatalf("ImportJSON failed: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 imported memories, got %d", n)
	}

	// Verify
	database.QueryRow("SELECT COUNT(*) FROM memories WHERE project_id = ?", projectID).Scan(&count)
	if count != 2 {
		t.Errorf("expected 2 memories after import, got %d", count)
	}
}

func TestDeleteSessionAndProject(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-delete-test")
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

	projectID := "proj-delete"
	err = db.RegisterProject(database, projectID, "Delete Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Start a session
	session, err := StartSession(database, projectID, "test", tempDir)
	if err != nil {
		t.Fatalf("failed starting session: %v", err)
	}

	// Save a memory associated with the session
	_, err = SaveMemory(database, &Memory{
		ID: "d-1", ProjectID: projectID, Category: "decision",
		What: "Test", Why: "why", Learned: "l",
		SessionID: session.ID, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("failed saving memory: %v", err)
	}

	// Test DeleteSession: should fail because memories are associated
	err = DeleteSession(database, session.ID)
	if err == nil {
		t.Error("expected error deleting session with memories, got nil")
	}

	// Test DeleteProject soft
	err = DeleteProject(database, projectID, false)
	if err != nil {
		t.Fatalf("DeleteProject soft failed: %v", err)
	}

	// Verify project still exists as shell
	var count int
	database.QueryRow("SELECT COUNT(*) FROM projects WHERE id=?", projectID).Scan(&count)
	if count != 1 {
		t.Error("expected project to remain as shell after soft delete")
	}

	// Verify memories are soft-deleted
	database.QueryRow("SELECT COUNT(*) FROM memories WHERE project_id=? AND deleted_at IS NULL", projectID).Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 active memories, got %d", count)
	}

	// Check soft-deleted memories exist
	database.QueryRow("SELECT COUNT(*) FROM memories WHERE project_id=? AND deleted_at IS NOT NULL", projectID).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 soft-deleted memory, got %d", count)
	}

	// Verify sessions are deleted
	database.QueryRow("SELECT COUNT(*) FROM sessions WHERE project_id=?", projectID).Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 sessions after soft delete, got %d", count)
	}

	// Test hard delete with a new project
	err = db.RegisterProject(database, "proj-hard", "Hard Delete Proj", filepath.Join(tempDir, "hard-path"))
	if err != nil {
		t.Fatalf("failed to register proj-hard: %v", err)
	}

	_, err = SaveMemory(database, &Memory{
		ID: "hd-1", ProjectID: "proj-hard", Category: "decision",
		What: "Hard", Why: "why", Learned: "l", CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("failed saving memory in proj-hard: %v", err)
	}

	err = DeleteProject(database, "proj-hard", true)
	if err != nil {
		t.Fatalf("DeleteProject hard failed: %v", err)
	}

	database.QueryRow("SELECT COUNT(*) FROM memories WHERE project_id='proj-hard'").Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 memories after hard delete, got %d", count)
	}
}

func TestMemoryCRUDAndFTS(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-memory-test")
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

	projectID := "proj-test"
	err = db.RegisterProject(database, projectID, "Test Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// 1. Test SaveMemory
	mem1 := &Memory{
		ID:        "mem-1",
		ProjectID: projectID,
		Category:  "bugfix",
		What:      "Fixed a memory leak in connection pooling",
		Why:       "The connections were not being closed inside the defer block",
		WherePath: "internal/db/db.go",
		Learned:   "Always verify connection close defer calls exist",
		CreatedAt: time.Now(),
	}

	_, err = SaveMemory(database, mem1)
	if err != nil {
		t.Fatalf("failed to save memory: %v", err)
	}

	// 2. Test SearchMemories with exact category and search term
	results, err := SearchMemories(database, projectID, "leak", "", 0)
	if err != nil {
		t.Fatalf("failed searching memories: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(results))
	}

	if results[0].ID != mem1.ID {
		t.Errorf("expected found memory ID to match, expected %s, got %s", mem1.ID, results[0].ID)
	}

	// Test SearchMemories with category filter
	resultsCat, err := SearchMemories(database, projectID, "", "bugfix", 0)
	if err != nil {
		t.Fatalf("failed searching memories with category filter: %v", err)
	}
	if len(resultsCat) != 1 {
		t.Errorf("expected 1 result with category filter, got %d", len(resultsCat))
	}

	// Test SearchMemories with wrong keyword
	resultsEmpty, err := SearchMemories(database, projectID, "nonexistentword", "", 0)
	if err != nil {
		t.Fatalf("failed searching: %v", err)
	}
	if len(resultsEmpty) != 0 {
		t.Errorf("expected 0 search results, got %d", len(resultsEmpty))
	}
}

func TestGitSync(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-sync-test")
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

	projectID := "proj-sync-test"
	err = db.RegisterProject(database, projectID, "Sync Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Save a few memories in SQLite
	mems := []*Memory{
		{
			ID:        "m-1",
			ProjectID: projectID,
			Category:  "architecture",
			What:      "Use clean architecture in Golang service",
			Why:       "Decouple controllers from DB schema",
			WherePath: "internal/service",
			Learned:   "Inject repository interface inside use-case controllers",
			CreatedAt: time.Now().Truncate(time.Second),
		},
		{
			ID:        "m-2",
			ProjectID: projectID,
			Category:  "standard",
			What:      "Linter configurations",
			Why:       "Enforce consistent coding style across team",
			WherePath: ".golangci.yml",
			Learned:   "Run golangci-lint check on commit hook",
			CreatedAt: time.Now().Truncate(time.Second),
		},
	}

	for _, m := range mems {
		if _, saveErr := SaveMemory(database, m); saveErr != nil {
			t.Fatalf("failed saving memory: %v", saveErr)
		}
	}

	// 1. Sync to Git (creates JSON file)
	err = SyncToGit(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("failed SyncToGit: %v", err)
	}

	syncFilePath := filepath.Join(tempDir, ".sv-memory", "memories.json")
	if _, statErr := os.Stat(syncFilePath); os.IsNotExist(statErr) {
		t.Fatal("expected .sv-memory/memories.json file to be created")
	}

	// Check JSON file content
	data, err := os.ReadFile(syncFilePath)
	if err != nil {
		t.Fatalf("failed reading sync file: %v", err)
	}

	var readMems []*Memory
	if unmarshalErr := json.Unmarshal(data, &readMems); unmarshalErr != nil {
		t.Fatalf("failed to unmarshal JSON: %v", unmarshalErr)
	}

	if len(readMems) != 2 {
		t.Errorf("expected 2 memories in JSON, got %d", len(readMems))
	}

	// 2. Sync from Git: delete from database first, then pull
	_, err = database.Exec("DELETE FROM memories")
	if err != nil {
		t.Fatalf("failed clearing memories table: %v", err)
	}

	var count int
	err = database.QueryRow("SELECT COUNT(*) FROM memories").Scan(&count)
	if err != nil {
		t.Fatalf("query count err: %v", err)
	}
	if count != 0 {
		t.Fatal("expected database memories table to be empty")
	}

	// Pull from Git JSON
	err = SyncFromGit(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("failed SyncFromGit: %v", err)
	}

	// Verify database is populated again
	err = database.QueryRow("SELECT COUNT(*) FROM memories").Scan(&count)
	if err != nil {
		t.Fatalf("query count err: %v", err)
	}
	if count != 2 {
		t.Errorf("expected database to have 2 memories after sync, got %d", count)
	}
}

func TestFindSimilarMemories(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-similar-test")
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

	projectID := "proj-similar"
	err = db.RegisterProject(database, projectID, "Similar Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Save memories with overlapping and distinct titles
	mems := []*Memory{
		{
			ID:        "m-1",
			ProjectID: projectID,
			Category:  "architecture",
			What:      "Use clean architecture in Golang service",
			Why:       "Decouple concerns",
			Learned:   "Keep domain clean",
			CreatedAt: time.Now(),
		},
		{
			ID:        "m-2",
			ProjectID: projectID,
			Category:  "decision",
			What:      "Use hexagonal architecture in Golang backend",
			Why:       "Ports and adapters",
			Learned:   "Separate domain from infra",
			CreatedAt: time.Now(),
		},
		{
			ID:        "m-3",
			ProjectID: projectID,
			Category:  "journal",
			What:      "Fixed memory leak in connection pool",
			Why:       "Connections not closed",
			Learned:   "Check defer close",
			CreatedAt: time.Now(),
		},
		{
			ID:        "m-4",
			ProjectID: projectID,
			Category:  "standard",
			What:      "Linter configuration for Golang",
			Why:       "Consistent style",
			Learned:   "Run linter on commit",
			CreatedAt: time.Now(),
		},
	}
	for _, m := range mems {
		if _, err := SaveMemory(database, m); err != nil {
			t.Fatalf("failed saving memory: %v", err)
		}
	}

	tests := []struct {
		name      string
		title     string
		wantMin   int
		wantMax   int
		threshold float64
	}{
		{
			name:      "similar architecture title",
			title:     "Use clean architecture in Golang service",
			wantMin:   1,
			wantMax:   2,
			threshold: 0.85,
		},
		{
			name:      "similar hexagonal title",
			title:     "Use hexagonal architecture in Golang backend",
			wantMin:   1,
			wantMax:   2,
			threshold: 0.85,
		},
		{
			name:      "unique title no match",
			title:     "Setup CI/CD pipeline with GitHub Actions",
			wantMin:   0,
			wantMax:   0,
			threshold: 0.85,
		},
		{
			name:      "low threshold catches more",
			title:     "Golang architecture clean backend",
			wantMin:   1,
			wantMax:   4,
			threshold: 0.50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidates, err := FindSimilarMemories(database, projectID, tt.title, 5, tt.threshold)
			if err != nil {
				t.Fatalf("FindSimilarMemories failed: %v", err)
			}
			if len(candidates) < tt.wantMin || len(candidates) > tt.wantMax {
				t.Errorf("expected %d-%d candidates, got %d", tt.wantMin, tt.wantMax, len(candidates))
			}
			// Check similarity values are within bounds
			for _, c := range candidates {
				if c.Similarity < 0 || c.Similarity > 1.0 {
					t.Errorf("similarity out of range [0,1]: %f", c.Similarity)
				}
				if c.ID == "" {
					t.Error("candidate ID is empty")
				}
			}
		})
	}
}

func TestTokenizeTitle(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"Use clean architecture in Golang service", []string{"clean", "architecture", "golang", "service"}},
		{"Fixed memory leak in connection pool", []string{"fixed", "memory", "leak", "connection", "pool"}},
		{"a an the", nil},
		{"hello world", []string{"hello", "world"}},
		{"", nil},
	}
	for _, tt := range tests {
		got := tokenizeTitle(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("tokenizeTitle(%q) = %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("tokenizeTitle(%q) = %v, want %v", tt.input, got, tt.want)
				break
			}
		}
	}
}

func TestJaccardSimilarity(t *testing.T) {
	tests := []struct {
		a    []string
		b    []string
		want float64
	}{
		{[]string{"a", "b"}, []string{"a", "b"}, 1.0},
		{[]string{"a", "b"}, []string{"a", "c"}, 1.0 / 3.0},
		{[]string{"a"}, []string{"b"}, 0.0},
		{[]string{}, []string{}, 1.0},
		{[]string{"a", "b", "c"}, []string{"a", "b"}, 2.0 / 3.0},
	}
	for _, tt := range tests {
		got := jaccardSimilarity(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("jaccardSimilarity(%v, %v) = %f, want %f", tt.a, tt.b, got, tt.want)
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

func TestExportObsidian(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-obsidian-export-test")
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

	projectID := "proj-export"
	err = db.RegisterProject(database, projectID, "Export Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// 1. Save a memory
	mem1 := &Memory{
		ID:        "mem-1234",
		ProjectID: projectID,
		Category:  "architecture",
		What:      "Use clean architecture in Go",
		Why:       "Decoupling controllers",
		WherePath: "main.go",
		Learned:   "Injecting interface controllers",
		CreatedAt: time.Now(),
	}
	if _, saveErr := SaveMemory(database, mem1); saveErr != nil {
		t.Fatalf("failed saving memory: %v", saveErr)
	}

	// 2. Insert mock structural graph nodes
	_, err = database.Exec(`
		INSERT INTO graph_nodes (id, project_id, node_type, label, path, metadata) VALUES 
		('main.go', ?, 'file', 'main.go', 'main.go', '{"language":"go"}'),
		('main.go:callerFunc', ?, 'function', 'callerFunc', 'main.go', '{"line":3}'),
		('main.go:helperFunc', ?, 'function', 'helperFunc', 'main.go', '{"line":7}'),
		('pkg:lodash', ?, 'package', 'lodash', 'lodash', NULL),
		('mem-1234', ?, 'concept', 'Use clean architecture in Go', 'main.go', NULL)
	`, projectID, projectID, projectID, projectID, projectID)
	if err != nil {
		t.Fatalf("failed to insert graph nodes: %v", err)
	}

	// 3. Insert mock structural graph edges
	_, err = database.Exec(`
		INSERT INTO graph_edges (id, project_id, source_id, target_id, relation_type, confidence, source_location) VALUES 
		('edge-1', ?, 'main.go', 'pkg:lodash', 'imports', 'EXTRACTED', NULL),
		('edge-2', ?, 'main.go:callerFunc', 'main.go:helperFunc', 'calls', 'INFERRED', 'L4'),
		('edge-3', ?, 'mem-1234', 'main.go', 'rationale_for', 'EXTRACTED', NULL)
	`, projectID, projectID, projectID)
	if err != nil {
		t.Fatalf("failed to insert graph edges: %v", err)
	}

	// 4. Run ExportObsidian
	outputDir := "my-vault"
	err = ExportObsidian(database, projectID, tempDir, outputDir)
	if err != nil {
		t.Fatalf("ExportObsidian failed: %v", err)
	}

	vaultPath := filepath.Join(tempDir, outputDir)

	// 5. Verify memory note exists
	memFilePath := filepath.Join(vaultPath, "mem-1234.md")
	if _, statErr := os.Stat(memFilePath); os.IsNotExist(statErr) {
		t.Fatalf("expected memory note to exist at %s", memFilePath)
	}
	memContent, err := os.ReadFile(memFilePath)
	if err != nil {
		t.Fatalf("failed reading memory note: %v", err)
	}
	if !strings.Contains(string(memContent), "[[code/main.go|main.go]]") {
		t.Errorf("expected memory note to contain link to code node, got:\n%s", string(memContent))
	}

	// 6. Verify code note exists
	codeFilePath := filepath.Join(vaultPath, "code", "main.go.md")
	if _, statErr := os.Stat(codeFilePath); os.IsNotExist(statErr) {
		t.Fatalf("expected code note to exist at %s", codeFilePath)
	}
	codeContent, err := os.ReadFile(codeFilePath)
	if err != nil {
		t.Fatalf("failed reading code note: %v", err)
	}
	if !strings.Contains(string(codeContent), "[[../code/packages/pkg_lodash|lodash]]") {
		t.Errorf("expected code note to contain package link, got:\n%s", string(codeContent))
	}
	if !strings.Contains(string(codeContent), "[[../code/main.go#helperFunc|helperFunc]]") {
		t.Errorf("expected code note to contain symbol call link, got:\n%s", string(codeContent))
	}
	if !strings.Contains(string(codeContent), "[[../mem-1234|mem-1234]]") {
		t.Errorf("expected code note to contain associated memory backlink, got:\n%s", string(codeContent))
	}

	// 7. Verify package note exists
	pkgFilePath := filepath.Join(vaultPath, "code", "packages", "pkg_lodash.md")
	if _, statErr := os.Stat(pkgFilePath); os.IsNotExist(statErr) {
		t.Fatalf("expected package note to exist at %s", pkgFilePath)
	}
	pkgContent, err := os.ReadFile(pkgFilePath)
	if err != nil {
		t.Fatalf("failed reading package note: %v", err)
	}
	if !strings.Contains(string(pkgContent), "[[../../code/main.go|main.go]]") {
		t.Errorf("expected package note to contain dependents link, got:\n%s", string(pkgContent))
	}
}

func TestAutoBootBundleAndScopedSearch(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_autoboot.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-autoboot"
	err = db.RegisterProject(database, projectID, "AutoBoot Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// 1. Create a session and memory
	sess, err := StartSession(database, projectID, "Refactor database module", tempDir)
	if err != nil {
		t.Fatalf("failed to start session: %v", err)
	}
	mem := &Memory{
		ID:        "mem-boot-1",
		ProjectID: projectID,
		Category:  "architecture",
		What:      "Use SQLite WAL mode",
		Why:       "Improve concurrency",
		WherePath: "internal/db/pool.go",
		Learned:   "WAL mode avoids locks",
		SessionID: sess.ID,
		CreatedAt: time.Now(),
	}
	if _, saveErr := SaveMemory(database, mem); saveErr != nil {
		t.Fatalf("failed to save memory: %v", saveErr)
	}

	// 2. Test GetAutoBootBundle
	bundle, err := GetAutoBootBundle(database, projectID)
	if err != nil {
		t.Fatalf("failed to get autoboot bundle: %v", err)
	}
	if !strings.Contains(bundle, "Use SQLite WAL mode") {
		t.Errorf("expected autoboot bundle to contain architectural decision, got:\n%s", bundle)
	}

	// 3. Test SearchMemoriesCompactScoped with BM25 & path filter
	results, err := SearchMemoriesCompactScoped(database, projectID, "SQLite", "", "internal/db", 10, 0)
	if err != nil {
		t.Fatalf("failed scoped search: %v", err)
	}
	if len(results) != 1 || results[0].ID != "mem-boot-1" {
		t.Errorf("expected 1 result with ID mem-boot-1, got %d results", len(results))
	}

	// Test path filter miss
	noResults, err := SearchMemoriesCompactScoped(database, projectID, "SQLite", "", "internal/mcp", 10, 0)
	if err != nil {
		t.Fatalf("failed scoped search miss: %v", err)
	}
	if len(noResults) != 0 {
		t.Errorf("expected 0 results for non-matching path, got %d", len(noResults))
	}
}

func TestAutoBootBundleFullSessionFlow(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_autoboot_flow.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-autoboot-flow"
	if err := db.RegisterProject(database, projectID, "AutoBoot Flow Proj", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Session 1: full lifecycle with architecture + decision memories.
	sess1, err := StartSession(database, projectID, "Refactor database module", tempDir)
	if err != nil {
		t.Fatalf("failed to start session 1: %v", err)
	}

	// Session 1 memories: these appear in the session context section and must
	// NOT be repeated in the per-category sections (dedup).
	sessMems := []*Memory{
		{ID: "mem-flow-1", ProjectID: projectID, Category: "architecture", What: "Use SQLite WAL mode", Why: "Improve concurrency", WherePath: "internal/db/pool.go", Learned: "WAL avoids writer locks", SessionID: sess1.ID, CreatedAt: time.Now().Add(-4 * time.Hour)},
		{ID: "mem-flow-3", ProjectID: projectID, Category: "decision", What: "Keep JSON over NDJSON", Why: "Backwards compatibility", Learned: "JSON stays for legacy commits", SessionID: sess1.ID, CreatedAt: time.Now().Add(-3 * time.Hour)},
		{ID: "mem-flow-5", ProjectID: projectID, Category: "bugfix", What: "Fix empty FTS5 query crash", Why: "Sanitize empty tokens", SessionID: sess1.ID, CreatedAt: time.Now().Add(-2 * time.Hour)},
	}
	// Extra memories outside the session so the per-category sections have
	// content even after session IDs are deduped.
	extraMems := []*Memory{
		{ID: "mem-extra-1", ProjectID: projectID, Category: "architecture", What: "Adopt Bun as package manager", Why: "Faster installs", CreatedAt: time.Now().Add(-90 * time.Minute)},
		{ID: "mem-extra-2", ProjectID: projectID, Category: "decision", What: "Use typed constants", Why: "Avoid magic strings", CreatedAt: time.Now().Add(-80 * time.Minute)},
		{ID: "mem-extra-3", ProjectID: projectID, Category: "standard", What: "Use Conventional Commits in Spanish", Why: "Team convention", CreatedAt: time.Now().Add(-70 * time.Minute)},
		{ID: "mem-extra-4", ProjectID: projectID, Category: "bugfix", What: "Fix index out of range", Why: "Guard empty slice", CreatedAt: time.Now().Add(-60 * time.Minute)},
	}
	for _, m := range append(sessMems, extraMems...) {
		if _, saveErr := SaveMemory(database, m); saveErr != nil {
			t.Fatalf("failed to save memory %s: %v", m.ID, saveErr)
		}
	}

	if err := SaveSessionSummary(database, sess1.ID, "Refactor database module", "Discovered WAL + Bun", "Wrote session flow", "Add edge case tests", "internal/memory/memory_session.go"); err != nil {
		t.Fatalf("failed to save session summary: %v", err)
	}
	if err := EndSession(database, sess1.ID, "Completed refactor session"); err != nil {
		t.Fatalf("failed to end session 1: %v", err)
	}

	// Session 2: starting a new session should surface session 1 context.
	if _, err := StartSession(database, projectID, "New feature task", tempDir); err != nil {
		t.Fatalf("failed to start session 2: %v", err)
	}

	bundle, err := GetAutoBootBundle(database, projectID)
	if err != nil {
		t.Fatalf("failed to get autoboot bundle: %v", err)
	}

	assertions := map[string]string{
		"## Previous Session Context":    "previous session section header",
		"**Session ID:** " + sess1.ID:     "session 1 ID",
		"**Goal:** Refactor database module": "session 1 goal",
		"**Summary:** Completed refactor session": "session 1 summary",
		"**Memories saved (3):**":         "session 1 memory list",
		"**Key Architectural Decisions:**": "architectural decisions section",
		"Adopt Bun as package manager":    "architecture memory title",
		"Use typed constants":             "decision memory title",
		"*Why:* Faster installs":          "architecture why rationale",
		"**Standards & Conventions:**":    "standards section",
		"Use Conventional Commits in Spanish": "standard memory title",
		"**Recent Work & Known Issues:**": "recent work section",
		"Fix index out of range":          "bugfix memory title",
		"Use SQLite WAL mode":             "session architecture memory listed in session context",
	}
	for want, desc := range assertions {
		if !strings.Contains(bundle, want) {
			t.Errorf("autoboot bundle missing %s (expected substring %q), got:\n%s", desc, want, bundle)
		}
	}

	// Dedup: a memory shown in the previous-session section must not repeat
	// its rationale under Key Architectural Decisions.
	if strings.Contains(bundle, "*Why:* Improve concurrency") {
		t.Errorf("expected session memory deduped from decisions section, got:\n%s", bundle)
	}
}

func TestGetSessionContextEdgeCases(t *testing.T) {
	t.Run("new project without sessions or memories", func(t *testing.T) {
		database, err := db.InitDB(filepath.Join(t.TempDir(), "edge_new.db"))
		if err != nil {
			t.Fatalf("failed to init db: %v", err)
		}
		defer database.Close()

		projectID := "proj-edge-new"
		if err := db.RegisterProject(database, projectID, "Edge New Proj", t.TempDir()); err != nil {
			t.Fatalf("failed to register project: %v", err)
		}

		ctx, err := GetSessionContext(database, projectID)
		if err != nil {
			t.Fatalf("failed GetSessionContext: %v", err)
		}
		if ctx != "No previous session context found for this project." {
			t.Errorf("expected empty-project message, got: %s", ctx)
		}
	})

	t.Run("active session is treated as nonexistent", func(t *testing.T) {
		database, err := db.InitDB(filepath.Join(t.TempDir(), "edge_active.db"))
		if err != nil {
			t.Fatalf("failed to init db: %v", err)
		}
		defer database.Close()

		projectID := "proj-edge-active"
		tempDir := t.TempDir()
		if err := db.RegisterProject(database, projectID, "Edge Active Proj", tempDir); err != nil {
			t.Fatalf("failed to register project: %v", err)
		}

		// An unclosed session must not be surfaced as previous context.
		if _, err := StartSession(database, projectID, "Unfinished work", tempDir); err != nil {
			t.Fatalf("failed to start session: %v", err)
		}
		if _, err := SaveMemory(database, &Memory{
			ProjectID: projectID, Category: "journal", What: "Ongoing notes",
			CreatedAt: time.Now(),
		}); err != nil {
			t.Fatalf("failed to save memory: %v", err)
		}

		ctx, err := GetSessionContext(database, projectID)
		if err != nil {
			t.Fatalf("failed GetSessionContext: %v", err)
		}
		if !strings.Contains(ctx, "No recorded sessions. Most recent memories:") {
			t.Errorf("expected fallback to recent memories for active-only session, got: %s", ctx)
		}
		if !strings.Contains(ctx, "Ongoing notes") {
			t.Errorf("expected recent memory listed in fallback, got: %s", ctx)
		}
	})

	t.Run("bundle omits decisions when none exist", func(t *testing.T) {
		database, err := db.InitDB(filepath.Join(t.TempDir(), "edge_nodecisions.db"))
		if err != nil {
			t.Fatalf("failed to init db: %v", err)
		}
		defer database.Close()

		projectID := "proj-edge-nodecisions"
		tempDir := t.TempDir()
		if err := db.RegisterProject(database, projectID, "Edge No Decisions Proj", tempDir); err != nil {
			t.Fatalf("failed to register project: %v", err)
		}

		sess, err := StartSession(database, projectID, "Journal only session", tempDir)
		if err != nil {
			t.Fatalf("failed to start session: %v", err)
		}
		if _, err := SaveMemory(database, &Memory{
			ProjectID: projectID, Category: "journal", What: "Work log entry",
			SessionID: sess.ID, CreatedAt: time.Now(),
		}); err != nil {
			t.Fatalf("failed to save memory: %v", err)
		}
		if err := EndSession(database, sess.ID, "Finished logging"); err != nil {
			t.Fatalf("failed to end session: %v", err)
		}

		bundle, err := GetAutoBootBundle(database, projectID)
		if err != nil {
			t.Fatalf("failed GetAutoBootBundle: %v", err)
		}
		if !strings.Contains(bundle, "## Previous Session Context") {
			t.Errorf("expected session context in bundle, got:\n%s", bundle)
		}
		if strings.Contains(bundle, "**Key Architectural Decisions:**") {
			t.Errorf("expected no architectural decisions section when only journal memories exist, got:\n%s", bundle)
		}
	})
}

func TestSanitizeFTS5Query(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"normal phrase", "sv-memory protocol", `"sv-memory"* "protocol"*`},
		{"column targeting chars", "memory: protocol", `"memory:"* "protocol"*`},
		{"double quotes", `foo "bar"`, `"foo"* "bar"*`},
		{"AND operator", "foo AND bar", `"foo"* "AND"* "bar"*`},
		{"special chars", `-WAL memory (`, `"-WAL"* "memory"* "("`},
		{"multiple spaces", "foo   bar", `"foo"* "bar"*`},
		{"single char token stays exact", "a protocol", `"a" "protocol"*`},
		{"only quotes produce empty query", `""`, ""},
		{"triple quotes produce empty query", `"""`, ""},
		{"mixed quotes and words", `foo """ bar`, `"foo"* "bar"*`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeFTS5Query(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeFTS5Query(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFTS5WithSpecialChars(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-fts-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "test-fts"
	if regErr := db.RegisterProject(database, projectID, "FTS Test", tempDir); regErr != nil {
		t.Fatalf("failed to register project: %v", regErr)
	}

	_, err = SaveMemory(database, &Memory{
		ID: "fts-1", ProjectID: projectID, Category: "test",
		What: "memory save protocol", Why: "testing save with FTS5",
		Learned: "always test save", CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("failed to save memory: %v", err)
	}

	// Queries that previously caused "no such column" or syntax errors
	hostileQueries := []string{
		"sv-memory: rules",
		"memory: protocol",
		`foo "bar"`,
		"-WAL",
		"AND",
		"memory (",
	}
	for _, q := range hostileQueries {
		t.Run("hostile_"+q, func(t *testing.T) {
			results, err := SearchMemoriesCompact(database, projectID, q, "", 10, 0)
			if err != nil {
				t.Fatalf("hostile query %q returned error: %v", q, err)
			}
			// Should not error — may or may not find results
			_ = results
		})
	}
}

func TestFTS5PrefixMatchingAndScore(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-prefix-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "test-prefix"
	if regErr := db.RegisterProject(database, projectID, "Prefix Test", tempDir); regErr != nil {
		t.Fatalf("failed to register project: %v", regErr)
	}

	_, err = SaveMemory(database, &Memory{
		ID: "pfx-1", ProjectID: projectID, Category: "standard",
		What: "component card pattern", Why: "reusable UI components",
		Learned: "always use the card component", CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("failed to save memory: %v", err)
	}
	_, err = SaveMemory(database, &Memory{
		ID: "pfx-2", ProjectID: projectID, Category: "decision",
		What: "avoid component library", Why: "keep deps light",
		Learned: "no heavy frameworks", CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("failed to save memory: %v", err)
	}

	// Singular query must match the plural "components" in pfx-1 via prefix.
	results, err := SearchMemoriesCompact(database, projectID, "component", "", 10, 0)
	if err != nil {
		t.Fatalf("search 'component': %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected prefix match for 'component' to find 'components'")
	}
	if results[0].ID != "pfx-1" {
		t.Errorf("expected pfx-1 first, got %s", results[0].ID)
	}
	if results[0].Score == 0 {
		t.Error("expected a non-zero BM25 score on FTS results")
	}
	if results[0].Score > 0 {
		t.Errorf("expected negative BM25 score (lower is better), got %v", results[0].Score)
	}
}

func TestNewID(t *testing.T) {
	if got := len(newID()); got != 16 {
		t.Errorf("newID() length = %d, want 16", got)
	}
	seen := make(map[string]bool, 100000)
	for i := 0; i < 100000; i++ {
		id := newID()
		if seen[id] {
			t.Fatalf("newID() collision detected: %s", id)
		}
		seen[id] = true
	}
}

func TestSearchQuotesOnlyDoesNotError(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-fts-quotes")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "test-fts-quotes"
	if regErr := db.RegisterProject(database, projectID, "FTS Quotes Test", tempDir); regErr != nil {
		t.Fatalf("failed to register project: %v", regErr)
	}

	_, err = SaveMemory(database, &Memory{
		ProjectID: projectID, Category: "test",
		What: "some content", Why: "why", Learned: "learned",
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("failed to save memory: %v", err)
	}

	// Queries made only of quotes sanitize to "" and must not produce an FTS5
	// syntax error from an empty MATCH expression.
	for _, q := range []string{`""`, `"""`, `""" ""`} {
		compact, err := SearchMemoriesCompact(database, projectID, q, "", 10, 0)
		if err != nil {
			t.Fatalf("SearchMemoriesCompact(%q) errored: %v", q, err)
		}
		if len(compact) != 0 {
			t.Errorf("SearchMemoriesCompact(%q) = %d results, want 0", q, len(compact))
		}
		full, err := SearchMemories(database, projectID, q, "", 10)
		if err != nil {
			t.Fatalf("SearchMemories(%q) errored: %v", q, err)
		}
		if len(full) != 0 {
			t.Errorf("SearchMemories(%q) = %d results, want 0", q, len(full))
		}
	}
}
