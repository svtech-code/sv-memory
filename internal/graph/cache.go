package graph

import (
	"database/sql"
	"sync"
	"time"
)

type cacheEntry struct {
	graph      *InMemoryGraph
	maxMtimeMs int64
	cachedAt   time.Time
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

// Get returns the cached InMemoryGraph if the underlying database table mtime has not changed.
func (c *GraphCache) Get(db *sql.DB, projectID string) (*InMemoryGraph, bool) {
	c.mu.RLock()
	entry, ok := c.entries[projectID]
	c.mu.RUnlock()

	if !ok || entry == nil {
		return nil, false
	}

	// Fast mtime validation against graph_files_meta table
	var currentMaxMtime sql.NullInt64
	err := db.QueryRow("SELECT MAX(mtime_ms) FROM graph_files_meta WHERE project_id = ?", projectID).Scan(&currentMaxMtime)
	if err != nil || !currentMaxMtime.Valid {
		return entry.graph, true
	}

	if currentMaxMtime.Int64 > entry.maxMtimeMs {
		c.Invalidate(projectID)
		return nil, false
	}

	return entry.graph, true
}

// Put caches the InMemoryGraph alongside the current max file mtime.
func (c *GraphCache) Put(projectID string, g *InMemoryGraph, maxMtimeMs int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[projectID] = &cacheEntry{
		graph:      g,
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
