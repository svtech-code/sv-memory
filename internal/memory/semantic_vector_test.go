package memory

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/svtech-code/sv-memory/internal/db"
)

func TestExtractSubwordsAndCosineSimilarity(t *testing.T) {
	vec1 := extractSubwords("jwt token authentication")
	vec2 := extractSubwords("jwt auth tokens")
	vec3 := extractSubwords("unrelated database migration")

	sim12 := CosineSimilarity(vec1, vec2)
	sim13 := CosineSimilarity(vec1, vec3)

	if sim12 <= 0.3 {
		t.Errorf("expected high similarity between related auth terms, got %f", sim12)
	}
	if sim13 >= 0.2 {
		t.Errorf("expected low similarity between auth and database, got %f", sim13)
	}

	// Empty vectors
	if simEmpty := CosineSimilarity(nil, vec1); simEmpty != 0.0 {
		t.Errorf("expected 0.0 for nil vector, got %f", simEmpty)
	}
}

func TestSearchMemoriesHybridSemanticFallback(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-hybrid-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_hybrid.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-hybrid"
	if err = db.RegisterProject(database, projectID, "Hybrid Test", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Save a memory about authentication tokens
	_, err = SaveMemory(database, &Memory{
		ID: "mem-auth", ProjectID: projectID, Category: "standard",
		What: "jwt authentication tokens with sha256 signing",
		Why:  "secure session handling", Learned: "stateless tokens",
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("failed to save memory: %v", err)
	}

	// Save another memory about sqlite WAL
	_, err = SaveMemory(database, &Memory{
		ID: "mem-db", ProjectID: projectID, Category: "decision",
		What: "sqlite write-ahead-logging wal mode for concurrency",
		Why:  "reduce contention", Learned: "wal checkpointing",
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("failed to save memory: %v", err)
	}

	// 1. Strict "all" mode misses because "credentials" is not in the text
	allResults, err := SearchMemoriesCompactScoped(database, projectID, "jwt authentication credentials", "", "", "all", 5, 0)
	if err != nil {
		t.Fatalf("SearchMemoriesCompactScoped all failed: %v", err)
	}
	if len(allResults) != 0 {
		t.Fatalf("expected strict all mode to return 0 results, got %d", len(allResults))
	}

	// 2. Hybrid search surfaces mem-auth via subword vector cosine similarity expansion
	hybridResults, err := SearchMemoriesCompactScoped(database, projectID, "jwt authentication credentials", "", "", "hybrid", 5, 0)
	if err != nil {
		t.Fatalf("SearchMemoriesCompactScoped hybrid failed: %v", err)
	}

	if len(hybridResults) == 0 {
		t.Fatal("expected hybrid search to find mem-auth despite partial keyword variation")
	}
	if hybridResults[0].ID != "mem-auth" {
		t.Errorf("expected top result to be mem-auth, got %s", hybridResults[0].ID)
	}
}
