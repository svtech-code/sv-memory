package graph

import (
	"database/sql"
	"sync"
	"time"
)

type cacheEntry struct {
	graph          *InMemoryGraph
	maxMtimeMs     int64
	fileCount      int
	cachedAt       time.Time
}

// GraphCache provides a thread-safe in-memory cache for InMemoryGraph with mtime invalidation.
type GraphCache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry
}

var GlobalGraphCache = NewGraphCache()

func NewGraphCache() *GraphCache {
	return &GraphCache{
		entries: make(map[string]*cacheEntry),
	}
}

// Get returns the cached InMemoryGraph if the underlying database table has not changed.
// Validates against both the file count and max mtime to detect both deletions
// (which lower max mtime) and modifications/restorations.
func (c *GraphCache) Get(db *sql.DB, projectID string) (*InMemoryGraph, bool) {
	c.mu.RLock()
	entry, ok := c.entries[projectID]
	c.mu.RUnlock()

	if !ok || entry == nil {
		return nil, false
	}

	// Validate against both COUNT and MAX(mtime_ms) to detect deletions
	// (which reduce max mtime) or restorations (which may set older mtime).
	var currentCount sql.NullInt64
	var currentMaxMtime sql.NullInt64
	err := db.QueryRow("SELECT COUNT(*), COALESCE(MAX(mtime_ms), 0) FROM graph_files_meta WHERE project_id = ?", projectID).Scan(&currentCount, &currentMaxMtime)
	if err != nil || !currentCount.Valid {
		return entry.graph, true
	}

	if currentCount.Int64 != int64(entry.fileCount) || currentMaxMtime.Int64 > entry.maxMtimeMs {
		c.Invalidate(projectID)
		return nil, false
	}

	return entry.graph, true
}

// Put caches the InMemoryGraph alongside the current file count and max mtime.
func (c *GraphCache) Put(projectID string, g *InMemoryGraph, fileCount int, maxMtimeMs int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[projectID] = &cacheEntry{
		graph:      g,
		fileCount:  fileCount,
		maxMtimeMs: maxMtimeMs,
		cachedAt:   time.Now(),
	}
}

// Invalidate clears the cached entry for a project.
func (c *GraphCache) Invalidate(projectID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, projectID)
}

// Clear flushes all entries in the cache.
func (c *GraphCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*cacheEntry)
}
