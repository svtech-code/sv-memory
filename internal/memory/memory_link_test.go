package memory

import (
	"path/filepath"
	"testing"

	"github.com/svtech-code/sv-memory/internal/db"
)

func TestSaveMemoryWiresRationaleEdge(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "save_link_test.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	const projectID = "link-project"
	if err := db.RegisterProject(database, projectID, "Link Project", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Seed a canonical code node the memory will link to.
	if _, err := database.Exec(
		"INSERT INTO graph_nodes (id, project_id, node_type, label, path, metadata) VALUES (?, ?, 'file', ?, ?, '{}')",
		"internal/db/db.go", projectID, "db.go", "internal/db/db.go",
	); err != nil {
		t.Fatalf("failed to seed graph node: %v", err)
	}

	saved, err := SaveMemory(database, &Memory{
		ProjectID: projectID,
		Category:  "decision",
		What:      "Use PostgreSQL for user storage",
		Why:       "Relational queries and scaling",
		Learned:   "Relational model fits user schema",
		WherePath: "internal/db/db.go",
	})
	if err != nil {
		t.Fatalf("failed to save memory: %v", err)
	}

	// Memory must exist as a document node in the graph.
	var nodeType string
	if err := database.QueryRow("SELECT node_type FROM graph_nodes WHERE project_id = ? AND id = ?", projectID, saved.ID).Scan(&nodeType); err != nil {
		t.Fatalf("memory document node not found: %v", err)
	}
	if nodeType != "document" {
		t.Fatalf("expected document node, got %s", nodeType)
	}

	// And a rationale_for edge memory -> code node.
	var relType string
	if err := database.QueryRow(
		"SELECT relation_type FROM graph_edges WHERE project_id = ? AND source_id = ? AND target_id = 'internal/db/db.go'",
		projectID, saved.ID,
	).Scan(&relType); err != nil {
		t.Fatalf("rationale_for edge not found: %v", err)
	}
	if relType != "rationale_for" {
		t.Fatalf("expected rationale_for edge, got %s", relType)
	}
}

func TestUpdateMemoryRelinksChangedWherePath(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "update_link_test.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	const projectID = "link-update-project"
	if err := db.RegisterProject(database, projectID, "Link Update Project", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	for _, p := range []string{"a.go", "b.go"} {
		if _, err := database.Exec(
			"INSERT INTO graph_nodes (id, project_id, node_type, label, path, metadata) VALUES (?, ?, 'file', ?, ?, '{}')",
			p, projectID, p, p,
		); err != nil {
			t.Fatalf("failed to seed graph node %s: %v", p, err)
		}
	}

	saved, err := SaveMemory(database, &Memory{
		ProjectID: projectID,
		Category:  "architecture",
		What:      "Original",
		Why:       "Why",
		Learned:   "Learned",
		WherePath: "a.go",
	})
	if err != nil {
		t.Fatalf("failed to save memory: %v", err)
	}

	newPath := "b.go"
	if _, err := UpdateMemory(database, projectID, saved.ID, MemoryUpdate{WherePath: &newPath}); err != nil {
		t.Fatalf("failed to update memory: %v", err)
	}

	// Re-linked to the new target.
	var relType string
	if err := database.QueryRow(
		"SELECT relation_type FROM graph_edges WHERE project_id = ? AND source_id = ? AND target_id = 'b.go'",
		projectID, saved.ID,
	).Scan(&relType); err != nil {
		t.Fatalf("re-linked rationale_for edge not found: %v", err)
	}
	// And the document node path was updated.
	var nodePath string
	if err := database.QueryRow("SELECT path FROM graph_nodes WHERE project_id = ? AND id = ?", projectID, saved.ID).Scan(&nodePath); err != nil {
		t.Fatalf("memory document node not found: %v", err)
	}
	if nodePath != "b.go" {
		t.Fatalf("expected document node path b.go, got %s", nodePath)
	}
}
