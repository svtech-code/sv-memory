package graph

import (
	"database/sql"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
)

type cacheEntry struct {
	graph      *InMemoryGraph
	maxMtimeMs int64
	fileCount  int
	cachedAt   time.Time
}

// GraphCache provides a thread-safe fixed-capacity LRU cache for InMemoryGraph
// with mtime validation. Unlike a plain map, entries are evicted once the
// capacity is exceeded (oldest first), bounding memory for large workspaces.
type GraphCache struct {
	lru *lru.Cache[string, *cacheEntry]
}

// DefaultGraphCacheSize bounds how many project graphs are kept in memory.
const DefaultGraphCacheSize = 32

var GlobalGraphCache = NewGraphCache()

// NewGraphCache creates a fixed-capacity LRU cache. When the cache is full and
// a new project is cached, the least-recently-used entry is evicted.
func NewGraphCache() *GraphCache {
	c, err := lru.NewWithEvict[string, *cacheEntry](DefaultGraphCacheSize, nil)
	if err != nil {
		// NewWithEvict only fails for size < 1; DefaultGraphCacheSize is > 0.
		panic(err)
	}
	return &GraphCache{lru: c}
}

// Len returns the number of cached project graphs.
func (c *GraphCache) Len() int {
	return c.lru.Len()
}

// Get returns the cached InMemoryGraph if the underlying database table has not changed.
// Validates against both the file count and max mtime to detect both deletions
// (which lower max mtime) and modifications/restorations.
func (c *GraphCache) Get(db *sql.DB, projectID string) (*InMemoryGraph, bool) {
	entry, ok := c.lru.Get(projectID)
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

	if currentCount.Int64 != int64(entry.fileCount) || currentMaxMtime.Int64 != entry.maxMtimeMs {
		c.Invalidate(projectID)
		return nil, false
	}

	return entry.graph, true
}

// Put caches the InMemoryGraph alongside the current file count and max mtime.
func (c *GraphCache) Put(projectID string, g *InMemoryGraph, fileCount int, maxMtimeMs int64) {
	c.lru.Add(projectID, &cacheEntry{
		graph:      g,
		fileCount:  fileCount,
		maxMtimeMs: maxMtimeMs,
		cachedAt:   time.Now(),
	})
}

// Invalidate clears the cached entry for a project.
func (c *GraphCache) Invalidate(projectID string) {
	c.lru.Remove(projectID)
}

// Clear flushes all entries in the cache.
func (c *GraphCache) Clear() {
	c.lru.Purge()
}

// Entries returns the project IDs currently held in the cache, most-recently-used first.
func (c *GraphCache) Entries() []string {
	keys := c.lru.Keys()
	out := make([]string, 0, len(keys))
	out = append(out, keys...)
	return out
}

// InvalidateAll clears every cached entry and returns the number removed.
func (c *GraphCache) InvalidateAll() int {
	n := c.Len()
	c.Clear()
	return n
}
