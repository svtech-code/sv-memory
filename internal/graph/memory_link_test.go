package graph

import (
	"path/filepath"
	"testing"

	"github.com/svtech-code/sv-memory/internal/db"
)

func TestEnsureMemoryRationaleEdge(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "memlink.db"))
	if err != nil {
		t.Fatalf("InitDB error: %v", err)
	}
	defer database.Close()

	const projectID = "memlink-proj"
	if err := db.RegisterProject(database, projectID, "Mem Link", tempDir); err != nil {
		t.Fatalf("RegisterProject error: %v", err)
	}

	// A canonical file node (id == path) the memory will link to.
	if _, err := database.Exec(
		"INSERT INTO graph_nodes (id, project_id, node_type, label, path, metadata) VALUES (?, ?, 'file', ?, ?, '{}')",
		"internal/db/db.go", projectID, "db.go", "internal/db/db.go",
	); err != nil {
		t.Fatalf("seed file node: %v", err)
	}

	ref := MemoryRationaleRef{
		ID:        "mem1",
		Category:  "decision",
		What:      "Use PostgreSQL for user storage",
		WherePath: "internal/db/db.go",
	}
	if err := EnsureMemoryRationaleEdge(database, projectID, ref); err != nil {
		t.Fatalf("EnsureMemoryRationaleEdge error: %v", err)
	}

	// Memory document node upserted with id == memory id.
	var nodeType, path string
	if err := database.QueryRow("SELECT node_type, path FROM graph_nodes WHERE project_id = ? AND id = ?", projectID, "mem1").Scan(&nodeType, &path); err != nil {
		t.Fatalf("memory node not found: %v", err)
	}
	if nodeType != "document" || path != "internal/db/db.go" {
		t.Fatalf("unexpected memory node: type=%s path=%s", nodeType, path)
	}

	// rationale_for edge memory -> code node.
	var relType string
	if err := database.QueryRow(
		"SELECT relation_type FROM graph_edges WHERE project_id = ? AND source_id = 'mem1' AND target_id = 'internal/db/db.go'",
		projectID,
	).Scan(&relType); err != nil {
		t.Fatalf("rationale edge not found: %v", err)
	}
	if relType != "rationale_for" {
		t.Fatalf("unexpected relation type: %s", relType)
	}

	// Idempotent: a second call must not error or duplicate the edge.
	if err := EnsureMemoryRationaleEdge(database, projectID, ref); err != nil {
		t.Fatalf("second EnsureMemoryRationaleEdge error: %v", err)
	}
	var edgeCount int
	if err := database.QueryRow(
		"SELECT COUNT(*) FROM graph_edges WHERE project_id = ? AND source_id = 'mem1' AND target_id = 'internal/db/db.go' AND relation_type = 'rationale_for'",
		projectID,
	).Scan(&edgeCount); err != nil {
		t.Fatalf("count edges: %v", err)
	}
	if edgeCount != 1 {
		t.Fatalf("expected 1 rationale edge, got %d", edgeCount)
	}
}

func TestEnsureMemoryRationaleEdgeNoopWhenNoTarget(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "memlink_missing.db"))
	if err != nil {
		t.Fatalf("InitDB error: %v", err)
	}
	defer database.Close()

	const projectID = "memlink-missing"
	if err := db.RegisterProject(database, projectID, "Mem Link Missing", tempDir); err != nil {
		t.Fatalf("RegisterProject error: %v", err)
	}

	// Empty where_path -> no-op.
	if err := EnsureMemoryRationaleEdge(database, projectID, MemoryRationaleRef{ID: "m1", Category: "decision", What: "x"}); err != nil {
		t.Fatalf("empty where_path should be no-op, got error: %v", err)
	}

	// Known where_path but no graph node for it -> no-op, no error.
	if err := EnsureMemoryRationaleEdge(database, projectID, MemoryRationaleRef{ID: "m1", Category: "decision", What: "x", WherePath: "does/not/exist.go"}); err != nil {
		t.Fatalf("missing target should be no-op, got error: %v", err)
	}
	var n int
	_ = database.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ?", projectID).Scan(&n)
	if n != 0 {
		t.Fatalf("expected no graph nodes created, got %d", n)
	}
}

func TestRelinkMemoryRationaleEdges(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "relink.db"))
	if err != nil {
		t.Fatalf("InitDB error: %v", err)
	}
	defer database.Close()

	const projectID = "relink-proj"
	if err := db.RegisterProject(database, projectID, "Relink", tempDir); err != nil {
		t.Fatalf("RegisterProject error: %v", err)
	}

	for _, p := range []string{"a.go", "b.go"} {
		if _, err := database.Exec(
			"INSERT INTO graph_nodes (id, project_id, node_type, label, path, metadata) VALUES (?, ?, 'file', ?, ?, '{}')",
			p, projectID, p, p,
		); err != nil {
			t.Fatalf("seed node %s: %v", p, err)
		}
	}

	refs := []MemoryRationaleRef{
		{ID: "m1", Category: "architecture", What: "A", WherePath: "a.go"},
		{ID: "m2", Category: "bugfix", What: "B", WherePath: "b.go"},
	}
	if err := RelinkMemoryRationaleEdges(database, projectID, refs); err != nil {
		t.Fatalf("RelinkMemoryRationaleEdges error: %v", err)
	}

	var edges int
	if err := database.QueryRow("SELECT COUNT(*) FROM graph_edges WHERE project_id = ? AND relation_type = 'rationale_for'", projectID).Scan(&edges); err != nil {
		t.Fatalf("count edges: %v", err)
	}
	if edges != 2 {
		t.Fatalf("expected 2 rationale edges, got %d", edges)
	}
}

func TestNormalizeNodePath(t *testing.T) {
	cases := map[string]string{
		"":                        "",
		"   ":                     "",
		"./internal/db/db.go":     "internal/db/db.go",
		"internal\\db\\db.go":     "internal/db/db.go",
		"internal/db/db.go:42":    "internal/db/db.go",
		"internal/db/db.go:42:10": "internal/db/db.go",
		"internal/db/db.go":       "internal/db/db.go",
		"./README.md":             "README.md",
	}
	for in, want := range cases {
		if got := normalizeNodePath(in); got != want {
			t.Errorf("normalizeNodePath(%q) = %q, want %q", in, got, want)
		}
	}
}
