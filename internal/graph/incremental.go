package graph

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// SyncGraph scans the project directory and builds or incrementally updates the
// dependency graph stored in SQLite. It detects changed files via mtime/size
// and avoids reparsing unchanged files. When more than 30 % of tracked files
// have changed, or when no prior metadata exists, it falls back to a full
// rebuild (delete-all + rescan).
func SyncGraph(db *sql.DB, projectID string, projPath string) error {
	return syncGraph(db, projectID, projPath)
}

// SyncGraphFull forces a full rebuild: all existing nodes and edges for the
// project are deleted and re-scanned from disk. Use this for the CLI `graph
// rebuild` command. Called internally as a fallback when the incremental path
// detects too many changes.
func SyncGraphFull(db *sql.DB, projectID string, projPath string) error {
	return syncGraphFull(db, projectID, projPath)
}

// syncGraph dispatches to incremental or full rebuild.
func syncGraph(db *sql.DB, projectID string, projPath string) error {
	ok, err := trySyncGraphIncremental(db, projectID, projPath)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	// Fall back to full rebuild when incremental is not viable (first run or
	// too many changes).
	return syncGraphFull(db, projectID, projPath)
}

func syncGraphFull(db *sql.DB, projectID string, projPath string) error {
	wr, err := scanFiles(projPath)
	if err != nil {
		return err
	}

	edges := parseFiles(projPath, wr.nodes, wr.fileList, wr.fileContents)
	manifestEdges := parseManifests(projPath, wr.nodes, wr.manifestFiles)
	edges = append(edges, manifestEdges...)

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec("DELETE FROM graph_edges WHERE project_id = ?", projectID)
	if err != nil {
		return fmt.Errorf("failed deleting old edges: %w", err)
	}
	_, err = tx.Exec("DELETE FROM graph_nodes WHERE project_id = ?", projectID)
	if err != nil {
		return fmt.Errorf("failed deleting old nodes: %w", err)
	}

	if err := bulkInsertNodes(tx, projectID, wr.nodes); err != nil {
		return err
	}
	if err := bulkInsertEdges(tx, projectID, edges); err != nil {
		return err
	}

	// Phase 1b: "contains" edges for documents and SQL schemas
	containEdges := extractContainsEdges(wr.nodes)
	if err := bulkInsertEdges(tx, projectID, containEdges); err != nil {
		return err
	}

	// Phase 2: Extract function and class calls
	callEdges := extractCallEdges(projPath, wr.nodes, wr.fileContents)
	if err := bulkInsertEdges(tx, projectID, callEdges); err != nil {
		return err
	}

	// Store fresh file metadata for future incremental runs.
	if err := updateFileMeta(tx, projectID, wr.fileMeta); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

// trySyncGraphIncremental attempts a partial graph update using file mtime/size
// comparison. Returns (true, nil) on success, (false, nil) when a full rebuild
// is recommended (no prior meta or too many changes), or (false, err) on
// database error.
func trySyncGraphIncremental(db *sql.DB, projectID string, projPath string) (bool, error) {
	return trySyncGraphIncrementalFiltered(db, projectID, projPath, nil)
}

// trySyncGraphIncrementalFiltered is trySyncGraphIncremental with an optional
// readOnly set. When readOnly is non-nil, only those files are read and parsed
// for symbols; every other supported file is still registered as a file node
// via its mtime/size so import resolution stays intact. This avoids re-reading
// the whole project when only a few files changed.
//
//nolint:gocyclo // incremental sync orchestrates many branches; refactor later
func trySyncGraphIncrementalFiltered(db *sql.DB, projectID string, projPath string, readOnly map[string]bool) (bool, error) {
	wr, err := scanFilesFiltered(projPath, readOnly)
	if err != nil {
		return false, err
	}

	// Load existing file metadata from the DB.
	oldMeta, err := loadFileMeta(db, projectID)
	if err != nil {
		return false, err
	}

	// First run — no prior metadata, do full rebuild.
	if len(oldMeta) == 0 {
		return false, nil
	}

	// Classify files as unchanged, new, changed, or deleted.
	var toParse, deleted []string
	for _, p := range wr.fileList {
		cur := wr.fileMeta[p]
		prev, exists := oldMeta[p]
		if !exists {
			toParse = append(toParse, p)
		} else if cur.mtimeMs != prev.mtimeMs || cur.size != prev.size {
			toParse = append(toParse, p)
		}
	}
	for p := range oldMeta {
		if _, stillExists := wr.fileMeta[p]; !stillExists {
			deleted = append(deleted, p)
		}
	}

	// If more than 30% of tracked files changed, fall back to full rebuild.
	totalTracked := len(oldMeta)
	churn := len(toParse) + len(deleted)
	if totalTracked > 0 && float64(churn)/float64(totalTracked) > 0.30 {
		return false, nil
	}

	tx, err := db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	// 1. Remove deleted files's nodes and edges (including child symbols).
	for _, p := range deleted {
		if _, e := tx.Exec("DELETE FROM graph_edges WHERE project_id = ? AND (source_id = ? OR source_id LIKE ? OR target_id = ? OR target_id LIKE ?)", projectID, p, p+":%", p, p+":%"); e != nil {
			return false, fmt.Errorf("failed deleting edges for deleted file %s: %w", p, e)
		}
		if _, e := tx.Exec("DELETE FROM graph_nodes WHERE project_id = ? AND (id = ? OR id LIKE ?)", projectID, p, p+":%"); e != nil {
			return false, fmt.Errorf("failed deleting nodes for deleted file %s: %w", p, e)
		}
		if _, e := tx.Exec("DELETE FROM graph_files_meta WHERE project_id = ? AND path = ?", projectID, p); e != nil {
			return false, fmt.Errorf("failed removing file meta for %s: %w", p, e)
		}
	}

	// 2. Remove old edges and child nodes for changed files.
	for _, p := range toParse {
		if _, e := tx.Exec("DELETE FROM graph_edges WHERE project_id = ? AND (source_id = ? OR source_id LIKE ?)", projectID, p, p+":%"); e != nil {
			return false, fmt.Errorf("failed deleting edges for changed file %s: %w", p, e)
		}
		// Remove stale child symbols (function/class nodes) that will be
		// re-created by scanFiles during step 3.
		if _, e := tx.Exec("DELETE FROM graph_nodes WHERE project_id = ? AND id LIKE ?", projectID, p+":%"); e != nil {
			return false, fmt.Errorf("failed deleting child nodes for changed file %s: %w", p, e)
		}
	}

	// Separate manifest files from code files before parsing.
	isManifest := make(map[string]bool, len(wr.manifestFiles))
	for _, mf := range wr.manifestFiles {
		isManifest[mf] = true
	}
	var codeToParse []string
	var manifestToParse []string
	for _, p := range toParse {
		if isManifest[p] {
			manifestToParse = append(manifestToParse, p)
		} else {
			codeToParse = append(codeToParse, p)
		}
	}

	// 3. Parse new+changed code files and insert nodes+edges.
	codeEdges := parseFiles(projPath, wr.nodes, codeToParse, wr.fileContents)
	for _, p := range codeToParse {
		if err := upsertNode(tx, projectID, wr.nodes[p]); err != nil {
			return false, fmt.Errorf("failed upserting node %s: %w", p, err)
		}
		// Also upsert child symbol nodes (functions/classes) for this file.
		prefix := p + ":"
		for id, node := range wr.nodes {
			if strings.HasPrefix(id, prefix) {
				if err := upsertNode(tx, projectID, node); err != nil {
					return false, fmt.Errorf("failed upserting child node %s: %w", id, err)
				}
			}
		}
	}
	// Also process changed manifests.
	var edges []*Edge
	edges = append(edges, codeEdges...)
	manifestEdges := parseManifests(projPath, wr.nodes, manifestToParse)
	edges = append(edges, manifestEdges...)
	// Upsert package nodes (from both code and manifest parsers).
	for _, node := range wr.nodes {
		if node.Type == "package" {
			if err := upsertNode(tx, projectID, node); err != nil {
				return false, fmt.Errorf("failed upserting package node %s: %w", node.ID, err)
			}
		}
	}
	if err := bulkInsertEdges(tx, projectID, edges); err != nil {
		return false, err
	}

	// 3b. "contains" edges for documents and SQL schemas
	containEdges := extractContainsEdges(wr.nodes)
	if err := bulkInsertEdges(tx, projectID, containEdges); err != nil {
		return false, err
	}

	// 4. Update file metadata for added/changed files (code + manifests).
	for _, p := range toParse {
		m := wr.fileMeta[p]
		if _, e := tx.Exec("INSERT OR REPLACE INTO graph_files_meta (project_id, path, mtime_ms, size) VALUES (?, ?, ?, ?)", projectID, p, m.mtimeMs, m.size); e != nil {
			return false, fmt.Errorf("failed upserting file meta for %s: %w", p, e)
		}
	}

	// Phase 2: Extract and sync function and class calls (re-create all calls edges)
	if _, err := tx.Exec("DELETE FROM graph_edges WHERE project_id = ? AND relation_type = 'calls'", projectID); err != nil {
		return false, fmt.Errorf("failed deleting old call edges: %w", err)
	}
	callEdges := extractCallEdges(projPath, wr.nodes, wr.fileContents)
	if err := bulkInsertEdges(tx, projectID, callEdges); err != nil {
		return false, err
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}

	return true, nil
}

func bulkInsertNodes(tx *sql.Tx, projectID string, nodes map[string]*Node) error {
	stmt, err := tx.Prepare("INSERT INTO graph_nodes (id, project_id, node_type, label, path, metadata) VALUES (?, ?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, node := range nodes {
		metaBytes, _ := json.Marshal(node.Metadata)
		metaStr := string(metaBytes)
		if metaStr == "null" {
			metaStr = "{}"
		}
		if _, e := stmt.Exec(node.ID, projectID, node.Type, node.Label, node.Path, metaStr); e != nil {
			return fmt.Errorf("failed inserting node %s: %w", node.ID, e)
		}
	}
	return nil
}

func bulkInsertEdges(tx *sql.Tx, projectID string, edges []*Edge) error {
	if len(edges) == 0 {
		return nil
	}
	stmt, err := tx.Prepare("INSERT INTO graph_edges (id, project_id, source_id, target_id, relation_type, confidence, source_location) VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING")
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, edge := range edges {
		confidence := edge.Confidence
		if confidence == "" {
			confidence = "EXTRACTED"
		}
		if _, e := stmt.Exec(edge.ID, projectID, edge.SourceID, edge.TargetID, edge.RelationType, confidence, edge.SourceLocation); e != nil {
			return fmt.Errorf("failed inserting edge %s: %w", edge.ID, e)
		}
	}
	return nil
}

func upsertNode(tx *sql.Tx, projectID string, node *Node) error {
	metaBytes, _ := json.Marshal(node.Metadata)
	metaStr := string(metaBytes)
	if metaStr == "null" {
		metaStr = "{}"
	}
	_, err := tx.Exec(`
		INSERT INTO graph_nodes (id, project_id, node_type, label, path, metadata)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id, project_id) DO UPDATE SET
			node_type = excluded.node_type,
			label = excluded.label,
			path = excluded.path,
			metadata = excluded.metadata
	`, node.ID, projectID, node.Type, node.Label, node.Path, metaStr)
	return err
}

func loadFileMeta(db *sql.DB, projectID string) (map[string]fileMetaEntry, error) {
	rows, err := db.Query("SELECT path, mtime_ms, size FROM graph_files_meta WHERE project_id = ?", projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	meta := make(map[string]fileMetaEntry)
	for rows.Next() {
		var path string
		var m fileMetaEntry
		if err := rows.Scan(&path, &m.mtimeMs, &m.size); err == nil {
			meta[path] = m
		}
	}
	return meta, rows.Err()
}

func updateFileMeta(tx *sql.Tx, projectID string, meta map[string]fileMetaEntry) error {
	if len(meta) == 0 {
		return nil
	}
	for path, m := range meta {
		if _, err := tx.Exec("INSERT OR REPLACE INTO graph_files_meta (project_id, path, mtime_ms, size) VALUES (?, ?, ?, ?)", projectID, path, m.mtimeMs, m.size); err != nil {
			return fmt.Errorf("failed to update file metadata for %s: %w", path, err)
		}
	}
	return nil
}
