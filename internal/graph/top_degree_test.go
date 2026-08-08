package graph

import (
	"path/filepath"
	"testing"

	"github.com/svtech-code/sv-memory/internal/db"
)

func TestTopDegreeNodes(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "top_degree.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init DB: %v", err)
	}
	defer database.Close()

	projectID := "proj-top-degree"
	if err := db.RegisterProject(database, projectID, "Top Degree Proj", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Node setup: two heavily-connected code nodes, one leaf, one document.
	nodes := []struct {
		id    string
		typ   string
		label string
	}{
		{"hub.go", "file", "hub.go"},
		{"core.go", "file", "core.go"},
		{"leaf.go", "file", "leaf.go"},
		{"README.md", "document", "README.md"},
		{"a.go", "file", "a.go"}, {"b.go", "file", "b.go"}, {"c.go", "file", "c.go"},
		{"d.go", "file", "d.go"}, {"e.go", "file", "e.go"},
	}
	for _, n := range nodes {
		if _, err := database.Exec(
			"INSERT INTO graph_nodes (project_id, id, node_type, label, path, metadata) VALUES (?, ?, ?, ?, ?, '{}')",
			projectID, n.id, n.typ, n.label, n.id); err != nil {
			t.Fatalf("failed inserting node %s: %v", n.id, err)
		}
	}

	edges := []struct{ src, tgt string }{
		{"a.go", "hub.go"}, {"b.go", "hub.go"}, {"c.go", "hub.go"},
		{"hub.go", "core.go"},
		{"d.go", "core.go"}, {"e.go", "core.go"},
		{"leaf.go", "core.go"},
		{"README.md", "hub.go"},
	}
	for i, e := range edges {
		id := "e" + string(rune('a'+i))
		if _, err := database.Exec(
			"INSERT INTO graph_edges (id, project_id, source_id, target_id, relation_type, confidence) VALUES (?, ?, ?, ?, 'imports', 'EXTRACTED')",
			id, projectID, e.src, e.tgt); err != nil {
			t.Fatalf("failed inserting edge %s: %v", id, err)
		}
	}

	hubs, err := TopDegreeNodes(database, projectID, 3)
	if err != nil {
		t.Fatalf("TopDegreeNodes failed: %v", err)
	}
	if len(hubs) != 3 {
		t.Fatalf("expected 3 hubs, got %d", len(hubs))
	}

	// hub.go has 3 inbound (a/b/c) + 1 outbound (core) + README inbound = 5;
	// core.go has 1 inbound (hub) + 3 outbound (d/e/leaf) = 4; README.md
	// (document) must be excluded despite its edge; leaf.go has degree 1.
	if hubs[0].Label != "hub.go" || hubs[0].Degree != 5 {
		t.Errorf("expected hub.go first with degree 5, got %v (degree %d)", hubs[0].Label, hubs[0].Degree)
	}
	if hubs[1].Label != "core.go" || hubs[1].Degree != 4 {
		t.Errorf("expected core.go second with degree 4, got %v (degree %d)", hubs[1].Label, hubs[1].Degree)
	}
	for _, h := range hubs {
		if h.Type == "document" {
			t.Errorf("document node %s must be excluded from hubs", h.ID)
		}
	}

	// Empty project → no error, empty result.
	emptyPath := filepath.Join(tempDir, "empty")
	if err := db.RegisterProject(database, "proj-empty", "Empty", emptyPath); err != nil {
		t.Fatalf("failed to register empty project: %v", err)
	}
	empty, err := TopDegreeNodes(database, "proj-empty", 3)
	if err != nil {
		t.Fatalf("TopDegreeNodes empty failed: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("expected 0 hubs for empty project, got %d", len(empty))
	}
}
