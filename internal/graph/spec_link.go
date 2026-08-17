package graph

import (
	"database/sql"
	"fmt"

	"github.com/svtech-code/sv-memory/internal/graph/schema"
)

// SpecCapabilityRef describes a spec capability that should be linked into the
// code graph as a first-class 'spec' node. ChangeID identifies the change whose
// requirements target the capability; WherePath is the code path the change
// affects (empty for capabilities with no associated code yet).
type SpecCapabilityRef struct {
	ChangeID       string
	CapabilityPath string
	WherePath      string
}

// capabilityNodeID returns the stable graph node id for a capability.
func capabilityNodeID(capabilityPath string) string {
	return "spec:" + capabilityPath
}

// EnsureSpecCapabilityEdges upserts the 'spec' graph node for a capability and
// adds an idempotent 'implements' edge from the canonical code node at
// ref.WherePath to the capability (code implements the capability). The node is
// always created; the code edge is best-effort and skipped when WherePath is
// empty or unmapped, mirroring EnsureMemoryRationaleEdge.
func EnsureSpecCapabilityEdges(db *sql.DB, projectID string, ref SpecCapabilityRef) error {
	if ref.CapabilityPath == "" {
		return nil
	}

	capID := capabilityNodeID(ref.CapabilityPath)
	label := "capability: " + truncateNodeLabel(ref.CapabilityPath)
	meta := map[string]interface{}{
		"spec":           true,
		"capability":     ref.CapabilityPath,
		"related_change": ref.ChangeID,
	}

	if err := upsertGraphNode(db, projectID, capID, schema.NodeTypeSpec, label, ref.CapabilityPath, meta); err != nil {
		return fmt.Errorf("failed to upsert capability node: %w", err)
	}

	where := normalizeNodePath(ref.WherePath)
	if where == "" {
		return nil
	}
	var codeNodeID string
	err := db.QueryRow(
		"SELECT id FROM graph_nodes WHERE project_id = ? AND path = ? AND id = path LIMIT 1",
		projectID, where,
	).Scan(&codeNodeID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}

	edgeID := codeNodeID + "->" + capID + "-implements"
	if err := insertGraphEdge(db, projectID, edgeID, codeNodeID, capID, schema.EdgeImplements, "INFERRED", ""); err != nil {
		return fmt.Errorf("failed to upsert capability implements edge: %w", err)
	}
	return nil
}

// LinkDecisionToCapability wires the 'implements' edge from a committed decision
// memory node (a 'document' node) to the capability whose requirements it
// fulfills. The capability node is upserted first so the edge never dangles.
// Best-effort: a decision without a graph node (e.g. no where_path) is skipped.
func LinkDecisionToCapability(db *sql.DB, projectID, memoryID, capabilityPath string) error {
	if memoryID == "" || capabilityPath == "" {
		return nil
	}
	var srcExists int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM graph_nodes WHERE project_id = ? AND id = ?", projectID, memoryID).Scan(&srcExists); err != nil {
		return err
	}
	if srcExists == 0 {
		return nil
	}
	if err := EnsureSpecCapabilityEdges(db, projectID, SpecCapabilityRef{CapabilityPath: capabilityPath}); err != nil {
		return err
	}
	capID := capabilityNodeID(capabilityPath)
	edgeID := memoryID + "->" + capID + "-implements"
	if err := insertGraphEdge(db, projectID, edgeID, memoryID, capID, schema.EdgeImplements, "INFERRED", ""); err != nil {
		return fmt.Errorf("failed to link decision to capability: %w", err)
	}
	return nil
}

// RelinkSpecCapabilityEdges re-creates the capability nodes and implements
// edges for all provided refs. Used after a full graph rebuild, which wipes the
// graph nodes/edges tables, so capabilities survive a rebuild.
func RelinkSpecCapabilityEdges(db *sql.DB, projectID string, refs []SpecCapabilityRef) error {
	for _, ref := range refs {
		if err := EnsureSpecCapabilityEdges(db, projectID, ref); err != nil {
			return err
		}
	}
	return nil
}
