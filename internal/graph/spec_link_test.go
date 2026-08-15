package graph

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/svtech-code/sv-memory/internal/db"
)

func setupSpecLinkDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "speclink.db"))
	if err != nil {
		t.Fatalf("InitDB error: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	const projectID = "speclink-proj"
	if err := db.RegisterProject(database, projectID, "Spec Link", tempDir); err != nil {
		t.Fatalf("RegisterProject error: %v", err)
	}
	return database, projectID
}

func TestEnsureSpecCapabilityEdges(t *testing.T) {
	database, projectID := setupSpecLinkDB(t)

	// A canonical code node the capability is implemented in.
	if _, err := database.Exec(
		"INSERT INTO graph_nodes (id, project_id, node_type, label, path, metadata) VALUES (?, ?, 'file', ?, ?, '{}')",
		"internal/auth/auth.go", projectID, "auth.go", "internal/auth/auth.go",
	); err != nil {
		t.Fatalf("seed code node: %v", err)
	}

	ref := SpecCapabilityRef{
		ChangeID:       "change1",
		CapabilityPath: "auth",
		WherePath:      "internal/auth/auth.go",
	}
	if err := EnsureSpecCapabilityEdges(database, projectID, ref); err != nil {
		t.Fatalf("EnsureSpecCapabilityEdges error: %v", err)
	}

	// Capability spec node upserted with a stable id and 'spec' type.
	var nodeType, path string
	if err := database.QueryRow("SELECT node_type, path FROM graph_nodes WHERE project_id = ? AND id = ?", projectID, "spec:auth").Scan(&nodeType, &path); err != nil {
		t.Fatalf("capability node not found: %v", err)
	}
	if nodeType != "spec" || path != "auth" {
		t.Fatalf("unexpected capability node: type=%s path=%s", nodeType, path)
	}

	// implements edge: code node -> capability.
	var relType string
	if err := database.QueryRow(
		"SELECT relation_type FROM graph_edges WHERE project_id = ? AND source_id = 'internal/auth/auth.go' AND target_id = 'spec:auth'",
		projectID,
	).Scan(&relType); err != nil {
		t.Fatalf("implements edge not found: %v", err)
	}
	if relType != "implements" {
		t.Fatalf("unexpected relation type: %s", relType)
	}

	// Re-running must be idempotent.
	if err := EnsureSpecCapabilityEdges(database, projectID, ref); err != nil {
		t.Fatalf("re-run EnsureSpecCapabilityEdges error: %v", err)
	}
}

func TestEnsureSpecCapabilityEdgesNoCodeNode(t *testing.T) {
	database, projectID := setupSpecLinkDB(t)

	// Capability without a mapped code path: node created, no edge, no error.
	ref := SpecCapabilityRef{ChangeID: "c1", CapabilityPath: "billing", WherePath: "missing/path.go"}
	if err := EnsureSpecCapabilityEdges(database, projectID, ref); err != nil {
		t.Fatalf("EnsureSpecCapabilityEdges error: %v", err)
	}
	var n int
	if err := database.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ? AND id = 'spec:billing'", projectID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected capability node, got %d", n)
	}
	var edges int
	if err := database.QueryRow(
		"SELECT COUNT(*) FROM graph_edges WHERE project_id = ? AND target_id = 'spec:billing'", projectID).Scan(&edges); err != nil {
		t.Fatalf("count edges: %v", err)
	}
	if edges != 0 {
		t.Fatalf("expected no implements edge without a code node, got %d", edges)
	}
}

func TestLinkDecisionToCapability(t *testing.T) {
	database, projectID := setupSpecLinkDB(t)

	// The decision memory must exist as a graph node first (SaveMemory creates
	// it); LinkDecisionToCapability then adds the implements edge.
	if _, err := database.Exec(
		"INSERT INTO graph_nodes (id, project_id, node_type, label, path, metadata) VALUES (?, ?, 'document', ?, ?, '{}')",
		"decision-mem-1", projectID, "decision-mem-1", "internal/auth/auth.go",
	); err != nil {
		t.Fatalf("seed decision node: %v", err)
	}

	if err := LinkDecisionToCapability(database, projectID, "decision-mem-1", "auth"); err != nil {
		t.Fatalf("LinkDecisionToCapability error: %v", err)
	}
	var relType string
	if err := database.QueryRow(
		"SELECT relation_type FROM graph_edges WHERE project_id = ? AND source_id = 'decision-mem-1' AND target_id = 'spec:auth'",
		projectID,
	).Scan(&relType); err != nil {
		t.Fatalf("decision implements edge not found: %v", err)
	}
	if relType != "implements" {
		t.Fatalf("unexpected relation type: %s", relType)
	}

	// The capability node must exist for the edge to satisfy the FK.
	var n int
	if err := database.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ? AND id = 'spec:auth'", projectID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected capability node for linked decision, got %d", n)
	}

	// A decision without a graph node is skipped (best-effort), not an error.
	if err := LinkDecisionToCapability(database, projectID, "ghost-decision", "auth"); err != nil {
		t.Fatalf("LinkDecisionToCapability for missing node should be a no-op, got %v", err)
	}
}

func TestRelinkSpecCapabilityEdges(t *testing.T) {
	database, projectID := setupSpecLinkDB(t)
	if _, err := database.Exec(
		"INSERT INTO graph_nodes (id, project_id, node_type, label, path, metadata) VALUES (?, ?, 'file', ?, ?, '{}')",
		"internal/auth/auth.go", projectID, "auth.go", "internal/auth/auth.go",
	); err != nil {
		t.Fatalf("seed code node: %v", err)
	}

	refs := []SpecCapabilityRef{
		{ChangeID: "c1", CapabilityPath: "auth", WherePath: "internal/auth/auth.go"},
	}
	if err := RelinkSpecCapabilityEdges(database, projectID, refs); err != nil {
		t.Fatalf("RelinkSpecCapabilityEdges error: %v", err)
	}
	var capEdges int
	if err := database.QueryRow(
		"SELECT COUNT(*) FROM graph_edges WHERE project_id = ? AND relation_type = 'implements' AND target_id = 'spec:auth' AND source_id = 'internal/auth/auth.go'",
		projectID).Scan(&capEdges); err != nil {
		t.Fatalf("count cap edges: %v", err)
	}
	if capEdges != 1 {
		t.Fatalf("expected 1 capability implements edge after relink, got %d", capEdges)
	}
}
