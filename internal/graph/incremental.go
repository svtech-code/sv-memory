package graph

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/svtech-code/sv-memory/internal/graph/schema"
	"github.com/svtech-code/sv-memory/internal/security"
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
	callEdges := extractCallEdges(wr.nodes, wr.fileContents)
	if err := bulkInsertEdges(tx, projectID, callEdges); err != nil {
		return err
	}

	// Phase 2b: Extract framework routing (Next.js, SvelteKit, FastAPI, Spring)
	routeNodes, routeEdges := extractRoutingEdges(projPath, wr.fileContents, wr.filePkgRoot, wr.nodes)
	if err := bulkInsertNodes(tx, projectID, routeNodes); err != nil {
		return err
	}
	if err := bulkInsertEdges(tx, projectID, routeEdges); err != nil {
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

	toParse, deleted := classifyFileChanges(wr, oldMeta)

	// If more than 30% of tracked files changed, fall back to full rebuild.
	if churnExceedsThreshold(len(oldMeta), len(toParse)+len(deleted)) {
		return false, nil
	}

	// Lazy incremental syncs (readOnly set) re-parse only the changed files,
	// so merge the persisted function/class nodes for unchanged files back in.
	// This keeps cross-file call resolution working without re-reading the
	// whole project. It must run before the transaction starts so the single
	// writer connection is not held while querying.
	if readOnly != nil {
		if err = mergeExistingSymbols(db, projectID, wr.nodes); err != nil {
			return false, fmt.Errorf("failed merging existing symbols: %w", err)
		}
	}

	tx, err := db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	if err = deleteRemovedFiles(tx, projectID, deleted); err != nil {
		return false, err
	}
	if err = resetChangedFiles(tx, projectID, toParse); err != nil {
		return false, err
	}

	codeToParse, manifestToParse := splitManifests(wr, toParse)
	codeEdges, err := parseAndUpsertCode(tx, projectID, projPath, wr, codeToParse)
	if err != nil {
		return false, err
	}
	if err = insertChangedEdges(tx, projectID, projPath, wr, codeEdges, manifestToParse); err != nil {
		return false, err
	}
	if err = upsertChangedFileMeta(tx, projectID, wr, toParse); err != nil {
		return false, err
	}
	if err = syncCallAndRouteEdges(tx, projectID, projPath, wr, readOnly); err != nil {
		return false, err
	}

	if err = tx.Commit(); err != nil {
		return false, err
	}

	return true, nil
}

// fileChangeSet is the outcome of comparing a fresh scan against the persisted
// file metadata.
type fileChangeSet struct {
	toParse []string
	deleted []string
}

// classifyFileChanges splits the scanned files into those needing (re)parsing
// (new or mtime/size-changed) and those deleted since the last sync.
func classifyFileChanges(wr *walkResult, oldMeta map[string]fileMetaEntry) (toParse, deleted []string) {
	for _, p := range wr.fileList {
		cur := wr.fileMeta[p]
		prev, exists := oldMeta[p]
		if !exists || cur.mtimeMs != prev.mtimeMs || cur.size != prev.size {
			toParse = append(toParse, p)
		}
	}
	for p := range oldMeta {
		if _, stillExists := wr.fileMeta[p]; !stillExists {
			deleted = append(deleted, p)
		}
	}
	return toParse, deleted
}

// churnExceedsThreshold reports whether the changed+deleted ratio is high enough
// to justify a full rebuild instead of an incremental update.
func churnExceedsThreshold(total, churn int) bool {
	return total > 0 && float64(churn)/float64(total) > 0.30
}

// deleteRemovedFiles removes all nodes, edges, and metadata for files that no
// longer exist on disk.
func deleteRemovedFiles(tx *sql.Tx, projectID string, deleted []string) error {
	for _, p := range deleted {
		if _, e := tx.Exec("DELETE FROM graph_edges WHERE project_id = ? AND (source_id = ? OR source_id LIKE ? OR target_id = ? OR target_id LIKE ?)", projectID, p, p+":%", p, p+":%"); e != nil {
			return fmt.Errorf("failed deleting edges for deleted file %s: %w", p, e)
		}
		if _, e := tx.Exec("DELETE FROM graph_nodes WHERE project_id = ? AND (id = ? OR id LIKE ?)", projectID, p, p+":%"); e != nil {
			return fmt.Errorf("failed deleting nodes for deleted file %s: %w", p, e)
		}
		if _, e := tx.Exec("DELETE FROM graph_files_meta WHERE project_id = ? AND path = ?", projectID, p); e != nil {
			return fmt.Errorf("failed removing file meta for %s: %w", p, e)
		}
	}
	return nil
}

// resetChangedFiles clears the source edges and stale child symbols for files
// that are about to be re-parsed.
func resetChangedFiles(tx *sql.Tx, projectID string, toParse []string) error {
	for _, p := range toParse {
		if _, e := tx.Exec("DELETE FROM graph_edges WHERE project_id = ? AND (source_id = ? OR source_id LIKE ?)", projectID, p, p+":%"); e != nil {
			return fmt.Errorf("failed deleting edges for changed file %s: %w", p, e)
		}
		// Remove stale child symbols (function/class nodes) that will be
		// re-created by scanFiles during step 3.
		if _, e := tx.Exec("DELETE FROM graph_nodes WHERE project_id = ? AND id LIKE ?", projectID, p+":%"); e != nil {
			return fmt.Errorf("failed deleting child nodes for changed file %s: %w", p, e)
		}
	}
	return nil
}

// splitManifests partitions the files to parse into code files and manifests.
func splitManifests(wr *walkResult, toParse []string) (code, manifests []string) {
	isManifest := make(map[string]bool, len(wr.manifestFiles))
	for _, mf := range wr.manifestFiles {
		isManifest[mf] = true
	}
	for _, p := range toParse {
		if isManifest[p] {
			manifests = append(manifests, p)
		} else {
			code = append(code, p)
		}
	}
	return code, manifests
}

// parseAndUpsertCode parses the changed code files, upserts their file + child
// symbol nodes, and returns the resulting import edges.
func parseAndUpsertCode(tx *sql.Tx, projectID, projPath string, wr *walkResult, codeToParse []string) ([]*Edge, error) {
	codeEdges := parseFiles(projPath, wr.nodes, codeToParse, wr.fileContents)
	for _, p := range codeToParse {
		if err := upsertNode(tx, projectID, wr.nodes[p]); err != nil {
			return nil, fmt.Errorf("failed upserting node %s: %w", p, err)
		}
		// Also upsert child symbol nodes (functions/classes) for this file.
		prefix := p + ":"
		for id, node := range wr.nodes {
			if strings.HasPrefix(id, prefix) {
				if err := upsertNode(tx, projectID, node); err != nil {
					return nil, fmt.Errorf("failed upserting child node %s: %w", id, err)
				}
			}
		}
	}
	return codeEdges, nil
}

// insertChangedEdges writes the code + manifest edges, upserts package nodes,
// and then writes the "contains" edges.
func insertChangedEdges(tx *sql.Tx, projectID, projPath string, wr *walkResult, codeEdges []*Edge, manifestToParse []string) error {
	edges := append([]*Edge{}, codeEdges...)
	edges = append(edges, parseManifests(projPath, wr.nodes, manifestToParse)...)
	// Upsert package nodes (from both code and manifest parsers).
	for _, node := range wr.nodes {
		if node.Type == schema.NodeTypePackage {
			if err := upsertNode(tx, projectID, node); err != nil {
				return fmt.Errorf("failed upserting package node %s: %w", node.ID, err)
			}
		}
	}
	if err := bulkInsertEdges(tx, projectID, edges); err != nil {
		return err
	}
	// "contains" edges for documents and SQL schemas
	return bulkInsertEdges(tx, projectID, extractContainsEdges(wr.nodes))
}

// upsertChangedFileMeta stores the fresh mtime/size for parsed files.
func upsertChangedFileMeta(tx *sql.Tx, projectID string, wr *walkResult, toParse []string) error {
	for _, p := range toParse {
		m := wr.fileMeta[p]
		if _, e := tx.Exec("INSERT OR REPLACE INTO graph_files_meta (project_id, path, mtime_ms, size) VALUES (?, ?, ?, ?)", projectID, p, m.mtimeMs, m.size); e != nil {
			return fmt.Errorf("failed upserting file meta for %s: %w", p, e)
		}
	}
	return nil
}

// syncCallAndRouteEdges refreshes call and routing edges. A full incremental
// sync rebuilds every call/route edge from the complete symbol map; a lazy sync
// (readOnly set) only inserts edges for the changed files — source-side edges
// were already removed, and edges that do not touch a changed file are
// preserved, so a single-file edit cannot wipe the rest of the calls graph.
func syncCallAndRouteEdges(tx *sql.Tx, projectID, projPath string, wr *walkResult, readOnly map[string]bool) error {
	if readOnly == nil {
		for _, rt := range []string{schema.EdgeCalls, schema.EdgeRoutes} {
			if _, err := tx.Exec("DELETE FROM graph_edges WHERE project_id = ? AND relation_type = ?", projectID, rt); err != nil {
				return fmt.Errorf("failed deleting old %s edges: %w", rt, err)
			}
		}
		if _, err := tx.Exec("DELETE FROM graph_nodes WHERE project_id = ? AND node_type = ?", projectID, schema.NodeTypeRoute); err != nil {
			return fmt.Errorf("failed deleting old routing nodes: %w", err)
		}
	}
	if err := bulkInsertEdges(tx, projectID, extractCallEdges(wr.nodes, wr.fileContents)); err != nil {
		return err
	}
	routeNodes, routeEdges := extractRoutingEdges(projPath, wr.fileContents, wr.filePkgRoot, wr.nodes)
	if err := bulkInsertNodes(tx, projectID, routeNodes); err != nil {
		return err
	}
	return bulkInsertEdges(tx, projectID, routeEdges)
}

// sanitizeNodeForPersist redacts secrets from a node's label and metadata before
// it is written to graph_nodes. Node paths are file paths produced by the
// filesystem walk (not file content) and are kept untouched so path-based
// lookups keep working. Metadata (rationales, SQL defaults/values) and labels
// (markdown headings) derive from scanned file content and can embed secrets,
// so they are redacted here as a final gate.
func sanitizeNodeForPersist(node *Node) {
	node.Label = security.SanitizeText(node.Label)
	if len(node.Metadata) == 0 {
		return
	}
	for k, v := range node.Metadata {
		switch t := v.(type) {
		case string:
			node.Metadata[k] = security.SanitizeText(t)
		case []string:
			for i, s := range t {
				t[i] = security.SanitizeText(s)
			}
			node.Metadata[k] = t
		case []interface{}:
			for i, s := range t {
				if str, ok := s.(string); ok {
					t[i] = security.SanitizeText(str)
				}
			}
			node.Metadata[k] = t
		}
	}
}

func bulkInsertNodes(tx *sql.Tx, projectID string, nodes map[string]*Node) error {
	stmt, err := tx.Prepare("INSERT INTO graph_nodes (id, project_id, node_type, label, path, metadata) VALUES (?, ?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, node := range nodes {
		sanitizeNodeForPersist(node)
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

	// Load valid node IDs for FK validation — prevents abort on synthetic
	// edges whose endpoints don't exist in graph_nodes.
	validIDs, err := loadValidNodeIDs(tx, projectID)
	if err != nil {
		// If we can't load IDs, fall back to unconditional insert (best effort).
		validIDs = nil
	}

	stmt, err := tx.Prepare("INSERT INTO graph_edges (id, project_id, source_id, target_id, relation_type, confidence, source_location) VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING")
	if err != nil {
		return err
	}
	defer stmt.Close()
	var skipped int
	for _, edge := range edges {
		if validIDs != nil {
			if !validIDs[edge.SourceID] || !validIDs[edge.TargetID] {
				skipped++
				continue
			}
		}
		confidence := edge.Confidence
		if confidence == "" {
			confidence = "EXTRACTED"
		}
		if _, e := stmt.Exec(edge.ID, projectID, edge.SourceID, edge.TargetID, edge.RelationType, confidence, edge.SourceLocation); e != nil {
			return fmt.Errorf("failed inserting edge %s: %w", edge.ID, e)
		}
	}
	_ = skipped // available for future diagnostics/logging
	return nil
}

// loadValidNodeIDs returns the set of node IDs for a project within the current
// transaction, used to validate edge endpoints before insertion.
func loadValidNodeIDs(tx *sql.Tx, projectID string) (map[string]bool, error) {
	rows, err := tx.Query("SELECT id FROM graph_nodes WHERE project_id = ?", projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}
	return ids, rows.Err()
}

// mergeExistingSymbols loads previously persisted function/class nodes for
// files that are not already in nodes (i.e. unchanged files during a lazy
// incremental sync) so cross-file call resolution still works when only a
// subset of files is re-parsed. Fresh nodes for changed files are kept.
func mergeExistingSymbols(db *sql.DB, projectID string, nodes map[string]*Node) error {
	rows, err := db.Query("SELECT id, node_type, label, path FROM graph_nodes WHERE project_id = ? AND node_type IN ('"+schema.NodeTypeFunction+"', '"+schema.NodeTypeClass+"')", projectID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, nodeType, label, path string
		if err := rows.Scan(&id, &nodeType, &label, &path); err != nil {
			return err
		}
		if _, exists := nodes[id]; exists {
			continue
		}
		nodes[id] = &Node{
			ID:       id,
			Type:     nodeType,
			Label:    label,
			Path:     path,
			Metadata: map[string]interface{}{},
		}
	}
	return rows.Err()
}

func upsertNode(tx *sql.Tx, projectID string, node *Node) error {
	sanitizeNodeForPersist(node)
	return upsertGraphNode(tx, projectID, node.ID, node.Type, node.Label, node.Path, node.Metadata)
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
			// Normalize legacy backslash paths (pre-ToSlash Windows builds) so
			// incremental comparisons match the forward-slash scan keys.
			meta[filepath.ToSlash(path)] = m
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
