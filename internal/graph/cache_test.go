package graph

import (
	"path/filepath"
	"testing"

	"github.com/svtech-code/sv-memory/internal/db"
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
	if regErr := db.RegisterProject(database, projectID, "Cache Proj", tempDir); regErr != nil {
		t.Fatalf("failed to register project: %v", regErr)
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

	// 7. A restored file may have an older mtime while keeping the same count.
	// The cache must not retain the graph from before the restoration.
	_, _ = database.Exec("INSERT INTO graph_files_meta (project_id, path, mtime_ms, size) VALUES (?, 'file.go', 1500, 100)", projectID)
	cache.Put(projectID, g, 1, 2000)
	_, _ = database.Exec("UPDATE graph_files_meta SET mtime_ms = 1000 WHERE project_id = ?", projectID)
	restoredG, ok := cache.Get(database, projectID)
	if ok || restoredG != nil {
		t.Fatalf("expected cache miss after mtime decreased, got hit")
	}

	// 8. Test manual invalidation of a single project.
	cache.Put(projectID, g, 1, 2000)
	cache.Invalidate(projectID)
	if _, ok := cache.Get(database, projectID); ok {
		t.Fatalf("expected cache miss after Invalidate()")
	}
}

// TestGraphCacheLRUEviction verifies that the cache is a real fixed-capacity
// LRU: once capacity is exceeded, the least-recently-used project is evicted.
func TestGraphCacheLRUEviction(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_cache_lru.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	cache := NewGraphCache()
	cache.lru.Resize(3)

	g := &InMemoryGraph{Nodes: map[string]*Node{}}

	// Add 4 projects; with capacity 3 the first one must be evicted.
	for i := 1; i <= 4; i++ {
		pid := "proj-" + string(rune('0'+i))
		cache.Put(pid, g, 1, int64(i))
	}

	if cache.lru.Len() != 3 {
		t.Fatalf("expected cache length 3 after exceeding capacity, got %d", cache.lru.Len())
	}
	if _, ok := cache.lru.Get("proj-1"); ok {
		t.Error("expected proj-1 to be evicted (LRU), but it is still present")
	}
	for _, id := range []string{"proj-2", "proj-3", "proj-4"} {
		if _, ok := cache.lru.Get(id); !ok {
			t.Errorf("expected %s to remain in cache, but it was evicted", id)
		}
	}
}
