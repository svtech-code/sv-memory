package graph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/svtech-code/sv-memory/internal/db"
)

func TestGenerateGraphReportWritesAllSections(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "report.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init DB: %v", err)
	}
	defer database.Close()

	projectID := "proj-report"
	if err = db.RegisterProject(database, projectID, "Report Proj", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	nodes := []struct {
		id    string
		typ   string
		label string
	}{
		{"hub.go", "file", "hub.go"},
		{"core.go", "file", "core.go"},
		{"leaf.go", "file", "leaf.go"},
		{"a.go", "file", "a.go"}, {"b.go", "file", "b.go"}, {"c.go", "file", "c.go"},
		{"d.go", "file", "d.go"}, {"e.go", "file", "e.go"},
	}
	for _, n := range nodes {
		if _, execErr := database.Exec(
			"INSERT INTO graph_nodes (project_id, id, node_type, label, path, metadata) VALUES (?, ?, ?, ?, ?, '{}')",
			projectID, n.id, n.typ, n.label, n.id); execErr != nil {
			t.Fatalf("failed inserting node %s: %v", n.id, execErr)
		}
	}
	edges := []struct{ src, tgt string }{
		{"a.go", "hub.go"}, {"b.go", "hub.go"}, {"c.go", "hub.go"},
		{"hub.go", "core.go"},
		{"d.go", "core.go"}, {"e.go", "core.go"},
		{"leaf.go", "core.go"},
	}
	for i, e := range edges {
		id := "e" + string(rune('a'+i))
		if _, execErr := database.Exec(
			"INSERT INTO graph_edges (id, project_id, source_id, target_id, relation_type, confidence) VALUES (?, ?, ?, ?, 'imports', 'EXTRACTED')",
			id, projectID, e.src, e.tgt); execErr != nil {
			t.Fatalf("failed inserting edge %s: %v", id, execErr)
		}
	}

	output := filepath.Join(tempDir, "GRAPH_REPORT.md")
	summary, err := GenerateGraphReport(database, projectID, output, ReportOptions{
		ProjName: "Report Proj",
		GodNodes: 3,
	})
	if err != nil {
		t.Fatalf("GenerateGraphReport failed: %v", err)
	}

	if summary.Bytes <= 0 {
		t.Fatalf("expected non-empty report, got %d bytes", summary.Bytes)
	}
	if summary.Nodes != len(nodes) {
		t.Errorf("expected %d nodes in summary, got %d", len(nodes), summary.Nodes)
	}
	if summary.GodNodes != 3 {
		t.Errorf("expected 3 god nodes, got %d", summary.GodNodes)
	}

	body, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("failed reading report: %v", err)
	}
	content := string(body)

	for _, section := range []string{
		"# Graph Overview Report",
		"## God Nodes (top hubs)",
		"## Top Communities",
		"## Surprising Cross-Community Connections",
		"## Suggested Questions",
	} {
		if !strings.Contains(content, section) {
			t.Errorf("report missing section %q", section)
		}
	}

	if !strings.Contains(content, "Report Proj") {
		t.Errorf("report missing project name in header")
	}
	if !strings.Contains(content, "hub.go") || !strings.Contains(content, "core.go") {
		t.Errorf("report missing known hub labels")
	}
}

func TestGenerateGraphReportEmptyProject(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "empty.db"))
	if err != nil {
		t.Fatalf("failed to init DB: %v", err)
	}
	defer database.Close()

	projectID := "proj-empty-report"
	if err = db.RegisterProject(database, projectID, "Empty", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	output := filepath.Join(tempDir, "out", "GRAPH_REPORT.md")
	summary, err := GenerateGraphReport(database, projectID, output, ReportOptions{})
	if err != nil {
		t.Fatalf("GenerateGraphReport empty failed: %v", err)
	}
	if summary.Nodes != 0 {
		t.Errorf("expected 0 nodes for empty project, got %d", summary.Nodes)
	}
	if _, err := os.Stat(output); err != nil {
		t.Errorf("expected report file to be written: %v", err)
	}
}
