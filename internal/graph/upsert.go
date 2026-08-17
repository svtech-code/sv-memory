package graph

import (
	"database/sql"
	"encoding/json"
)

// sqlExecer is the minimal interface shared by *sql.DB and *sql.Tx so the graph
// upsert helpers can run on either a standalone connection or inside the caller's
// transaction without duplicating the INSERT/ON CONFLICT statements.
type sqlExecer interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

// upsertGraphNode is the single implementation of the graph_nodes upsert used by
// the scanner, the memory rationale linker, and the spec capability linker.
// nodeType must come from the schema vocabulary (schema.NodeType*) so the graph
// node types cannot drift apart across call sites.
func upsertGraphNode(exec sqlExecer, projectID, nodeID, nodeType, label, path string, metadata map[string]interface{}) error {
	metaBytes, _ := json.Marshal(metadata)
	metaStr := string(metaBytes)
	if metaStr == "null" {
		metaStr = "{}"
	}
	_, err := exec.Exec(`
		INSERT INTO graph_nodes (id, project_id, node_type, label, path, metadata)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id, project_id) DO UPDATE SET
			node_type = excluded.node_type,
			label = excluded.label,
			path = excluded.path,
			metadata = excluded.metadata
	`, nodeID, projectID, nodeType, label, path, metaStr)
	return err
}

// insertGraphEdge is the single implementation of the idempotent graph_edges
// insert used across the memory/spec linkers. relationType must come from the
// schema vocabulary (schema.Edge*).
func insertGraphEdge(exec sqlExecer, projectID, edgeID, sourceID, targetID, relationType, confidence, sourceLocation string) error {
	_, err := exec.Exec(`
		INSERT INTO graph_edges (id, project_id, source_id, target_id, relation_type, confidence, source_location)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING
	`, edgeID, projectID, sourceID, targetID, relationType, confidence, sourceLocation)
	return err
}
