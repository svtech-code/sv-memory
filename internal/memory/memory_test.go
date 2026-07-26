package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/svtech/sv-memory/internal/db"
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
	if _, err := SaveMemory(database, mem1); err != nil {
		t.Fatalf("failed saving mem1: %v", err)
	}
	if _, err := SaveMemory(database, mem2); err != nil {
		t.Fatalf("failed saving mem2: %v", err)
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
	if _, err := database.Exec("DELETE FROM memories WHERE project_id = ?", projectID); err != nil {
		t.Fatalf("failed clearing memories: %v", err)
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
		if _, err := SaveMemory(database, m); err != nil {
			t.Fatalf("failed saving memory: %v", err)
		}
	}

	// 1. Sync to Git (creates JSON file)
	err = SyncToGit(database, projectID, tempDir)
	if err != nil {
		t.Fatalf("failed SyncToGit: %v", err)
	}

	syncFilePath := filepath.Join(tempDir, ".sv-memory", "memories.json")
	if _, err := os.Stat(syncFilePath); os.IsNotExist(err) {
		t.Fatal("expected .sv-memory/memories.json file to be created")
	}

	// Check JSON file content
	data, err := os.ReadFile(syncFilePath)
	if err != nil {
		t.Fatalf("failed reading sync file: %v", err)
	}

	var readMems []*Memory
	if err := json.Unmarshal(data, &readMems); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
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

	if _, err := SaveMemory(database, mem1); err != nil {
		t.Fatalf("failed saving mem1: %v", err)
	}
	if _, err := SaveMemory(database, mem2); err != nil {
		t.Fatalf("failed saving mem2: %v", err)
	}
	if _, err := SaveMemory(database, mem3); err != nil {
		t.Fatalf("failed saving mem3: %v", err)
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
