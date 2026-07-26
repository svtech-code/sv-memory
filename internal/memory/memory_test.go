package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/svtech/sv-memory/internal/db"
)

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

	err = SaveMemory(database, mem1)
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
		if err := SaveMemory(database, m); err != nil {
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
