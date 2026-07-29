package graph

import (
	"path/filepath"
	"testing"

	"github.com/svtech/sv-memory/internal/db"
)

func TestGraphCache(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_cache.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-cache"
	if err := db.RegisterProject(database, projectID, "Cache Proj", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Insert meta record with mtime 1000
	_, err = database.Exec("INSERT INTO graph_files_meta (project_id, path, mtime_ms, size) VALUES (?, 'file.go', 1000, 100)", projectID)
	if err != nil {
		t.Fatalf("failed inserting graph_files_meta: %v", err)
	}

	cache := NewGraphCache()
	g := &InMemoryGraph{
		Nodes: map[string]*Node{
			"file.go": {ID: "file.go", Label: "file.go"},
		},
	}

	// 1. Put into cache with mtime 1000 and fileCount 1
	cache.Put(projectID, g, 1, 1000)

	// 2. Get from cache - should hit
	cachedG, ok := cache.Get(database, projectID)
	if !ok || cachedG == nil {
		t.Fatalf("expected cache hit, got miss")
	}

	// 3. Update mtime in DB to 2000 (simulate file modified on disk)
	_, _ = database.Exec("UPDATE graph_files_meta SET mtime_ms = 2000 WHERE project_id = ?", projectID)

	// 4. Get from cache - should invalidate and return miss
	invalidatedG, ok := cache.Get(database, projectID)
	if ok || invalidatedG != nil {
		t.Fatalf("expected cache miss due to mtime invalidation, got hit")
	}

	// 5. Re-cache with updated mtime
	cache.Put(projectID, g, 1, 2000)

	// 6. Delete the meta row (simulate file deleted) — cache should miss
	_, _ = database.Exec("DELETE FROM graph_files_meta WHERE project_id = ?", projectID)
	deletedG, ok := cache.Get(database, projectID)
	if ok || deletedG != nil {
		t.Fatalf("expected cache miss after file deletion, got hit")
	}

	// 7. Test manual clear
	cache.Put(projectID, g, 1, 2000)
	cache.Clear()
	if _, ok := cache.Get(database, projectID); ok {
		t.Fatalf("expected cache miss after Clear()")
	}
}
