package graph

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/svtech-code/sv-memory/internal/graph/schema"
)

// MemoryRationaleRef describes an active memory that should be linked to the
// code graph via a rationale_for edge. It intentionally carries only the
// fields needed for the link so the graph package does not depend on memory.
type MemoryRationaleRef struct {
	ID        string
	Category  string
	What      string
	WherePath string
}

// normalizeNodePath cleans a memory where_path into a graph node path: strips
// a leading "./", normalizes backslashes to forward slashes, and drops a
// trailing line/column suffix (e.g. "db.go:42" -> "db.go"). Returns "" for
// paths that cannot be mapped.
func normalizeNodePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimPrefix(p, "./")
	// Strip trailing :line or :line:col markers some tools append.
	if idx := strings.Index(p, ":"); idx > 0 {
		// Only treat it as a line suffix if the tail is purely digits (or
		// digits:digits); otherwise the colon may be part of the path.
		tail := p[idx+1:]
		valid := true
		seen := false
		for _, r := range tail {
			if r == ':' && !seen {
				seen = true
				continue
			}
			if r < '0' || r > '9' {
				valid = false
				break
			}
		}
		if valid && tail != "" {
			p = p[:idx]
		}
	}
	return strings.TrimSpace(p)
}

// EnsureMemoryRationaleEdge links one memory to the canonical code node whose
// path matches ref.WherePath with a rationale_for edge (memory -> code). It
// upserts a 'document' graph node for the memory (id = memory ID) and an
// idempotent edge to the code node. It is best-effort: an empty or unmapped
// where_path, or a graph that has not been built yet, is a silent no-op so a
// memory save never fails because of graph wiring.
func EnsureMemoryRationaleEdge(db *sql.DB, projectID string, ref MemoryRationaleRef) error {
	where := normalizeNodePath(ref.WherePath)
	if ref.ID == "" || where == "" {
		return nil
	}

	// Only link to the canonical node for the path (id == path), i.e. the file,
	// markdown document, SQL, or package node registered by the code scanner.
	// Symbol nodes (functions/classes) share the file path but have composite
	// ids, so linking to the canonical file is deterministic and safe.
	var targetID string
	err := db.QueryRow(
		"SELECT id FROM graph_nodes WHERE project_id = ? AND path = ? AND id = path LIMIT 1",
		projectID, where,
	).Scan(&targetID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}

	label := ref.Category + ": " + truncateNodeLabel(ref.What)
	meta := map[string]interface{}{
		"memory":    true,
		"memory_id": ref.ID,
		"category":  ref.Category,
	}

	if err := upsertGraphNode(db, projectID, ref.ID, schema.NodeTypeDocument, label, where, meta); err != nil {
		return fmt.Errorf("failed to upsert memory rationale node: %w", err)
	}

	edgeID := ref.ID + "->" + targetID + "-rationale_for"
	if err := insertGraphEdge(db, projectID, edgeID, ref.ID, targetID, schema.EdgeRationaleFor, "INFERRED", ""); err != nil {
		return fmt.Errorf("failed to upsert memory rationale edge: %w", err)
	}

	return nil
}

// RelinkMemoryRationaleEdges re-creates the rationale_for edges for all the
// provided memory refs. It is used after a full graph rebuild, which wipes the
// graph nodes/edges tables, so the memory <-> code links survive a rebuild.
func RelinkMemoryRationaleEdges(db *sql.DB, projectID string, refs []MemoryRationaleRef) error {
	for _, ref := range refs {
		if err := EnsureMemoryRationaleEdge(db, projectID, ref); err != nil {
			return err
		}
	}
	return nil
}

// truncateNodeLabel caps the memory node label so graph tables and exports
// stay compact.
func truncateNodeLabel(s string) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= 64 {
		return s
	}
	return string(runes[:64]) + "…"
}
