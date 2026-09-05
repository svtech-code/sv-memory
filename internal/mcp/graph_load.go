package mcp

import (
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/svtech-code/sv-memory/internal/graph"
	"github.com/svtech-code/sv-memory/internal/memory"
)

// computeCentralityIfMissing recalculates communities and betweenness
// centrality when they are missing from the graph metadata.
// The caller should invalidate the cache and reload the graph afterwards
// via GlobalGraphCache.Invalidate + getOrLoadGraph().
func (s *Server) computeCentralityIfMissing() {
	_ = graph.UpdateCommunitiesAndCentrality(s.pool.Writer, s.cfg.ProjectID)
	graph.GlobalGraphCache.Invalidate(s.cfg.ProjectID)
}

// getOrLoadGraph returns the in-memory graph for the active project, refreshing
// it lazily only when files changed on disk. When the file watcher is active and
// no changes have been detected (dirty == false), the O(n) DetectStaleFiles walk
// is skipped entirely — the watcher keeps the graph synced in background so the
// cache is already fresh. Falls back to the lazy DetectStaleFiles path when the
// watcher is disabled or reports pending changes.
func (s *Server) getOrLoadGraph() (*graph.InMemoryGraph, error) {
	// When the file watcher is active and reports no pending changes, the
	// background sync has already kept the graph and cache up to date. Skip
	// the O(n) DetectStaleFiles walk entirely.
	if globalFileWatcher != nil && !globalFileWatcher.IsDirty() {
		if cached, ok := graph.GlobalGraphCache.Get(s.pool.Reader, s.cfg.ProjectID); ok {
			return cached, nil
		}
	}

	// Watcher disabled, or dirty — fall back to the lazy sync path.
	startStale := time.Now()
	if synced, err := graph.SyncGraphIfStale(s.pool.Writer, s.cfg.ProjectID, s.cfg.ProjPath); err != nil {
		return nil, err
	} else if synced {
		debugLog("graph lazy sync ran in %s", time.Since(startStale))
	}

	if cached, ok := graph.GlobalGraphCache.Get(s.pool.Reader, s.cfg.ProjectID); ok {
		return cached, nil
	}

	s.graphMu.Lock()
	defer s.graphMu.Unlock()

	// Double-check after lock
	if cached, ok := graph.GlobalGraphCache.Get(s.pool.Reader, s.cfg.ProjectID); ok {
		return cached, nil
	}

	var count int
	if err := s.pool.Reader.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ?", s.cfg.ProjectID).Scan(&count); err != nil {
		return nil, err
	}
	if count == 0 {
		startBuild := time.Now()
		if err := graph.SyncGraph(s.pool.Writer, s.cfg.ProjectID, s.cfg.ProjPath); err != nil {
			return nil, err
		}
		debugLog("graph_query auto-built graph in %s", time.Since(startBuild))
		s.relinkMemoryRationales()
	}
	g, err := graph.LoadFullGraph(s.pool.Reader, s.cfg.ProjectID)
	if err != nil {
		return nil, err
	}

	var fileCount sql.NullInt64
	var maxMtime sql.NullInt64
	_ = s.pool.Reader.QueryRow("SELECT COUNT(*), COALESCE(MAX(mtime_ms), 0) FROM graph_files_meta WHERE project_id = ?", s.cfg.ProjectID).Scan(&fileCount, &maxMtime)
	graph.GlobalGraphCache.Put(s.cfg.ProjectID, g, int(fileCount.Int64), maxMtime.Int64)
	return g, nil
}

// relinkMemoryRationales re-creates the rationale_for edges between saved
// memories (where_path) and their code nodes. Called after a full graph
// rebuild, which wipes the graph nodes/edges tables. Best-effort.
func (s *Server) relinkMemoryRationales() {
	refs, err := memory.ActiveMemoryRationaleRefs(s.pool.Writer, s.cfg.ProjectID)
	if err != nil || len(refs) == 0 {
		return
	}
	if err := graph.RelinkMemoryRationaleEdges(s.pool.Writer, s.cfg.ProjectID, refs); err != nil {
		fmt.Fprintf(os.Stderr, "[sv-memory] relink rationale edges failed: %v\n", err)
	}
}

// relinkSpecCapabilities re-creates the spec capability nodes and their
// implements edges after a full graph rebuild. Best-effort.
func (s *Server) relinkSpecCapabilities() {
	refs, err := memory.ActiveSpecCapabilityRefs(s.pool.Writer, s.cfg.ProjectID)
	if err != nil || len(refs) == 0 {
		return
	}
	if err := graph.RelinkSpecCapabilityEdges(s.pool.Writer, s.cfg.ProjectID, refs); err != nil {
		fmt.Fprintf(os.Stderr, "[sv-memory] relink spec capability edges failed: %v\n", err)
	}
}
