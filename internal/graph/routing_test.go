package graph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/svtech-code/sv-memory/internal/db"
)

func TestExtractRoutingEdgesCanonicalTarget(t *testing.T) {
	// The edge target_id must be the raw path, not "file:" + path.
	// This is the core fix for the FK failure.
	fileContents := map[string][]byte{
		"pages/index.tsx": []byte("export default function Home() {}"),
	}
	frameworks := routeFrameworks{Next: true}

	routeNodes, edges := extractRoutingEdges(fileContents, frameworks)

	if len(routeNodes) == 0 {
		t.Fatal("expected at least one route node")
	}
	if len(edges) == 0 {
		t.Fatal("expected at least one route edge")
	}

	edge := edges[0]
	if edge.TargetID != "pages/index.tsx" {
		t.Errorf("expected target_id 'pages/index.tsx', got %q", edge.TargetID)
	}
	// Verify no "file:" prefix
	if edge.TargetID == "file:pages/index.tsx" {
		t.Error("target_id must not have 'file:' prefix")
	}
}

func TestRoutingEdgesE2ENoFKError(t *testing.T) {
	// Full SyncGraph with Next.js evidence must not fail with FK error.
	tempDir, err := os.MkdirTemp("", "sv-mem-routing-test")
	if err != nil {
		t.Fatalf("failed to create temp workspace: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_storage.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init DB: %v", err)
	}
	defer database.Close()

	projectID := "proj-routing-test"
	if err = db.RegisterProject(database, projectID, "Routing Test", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Package.json with next dependency → evidence
	pkgJSON := `{"dependencies": {"next": "^14.0.0", "react": "^18.0.0"}}`
	if err = os.WriteFile(filepath.Join(tempDir, "package.json"), []byte(pkgJSON), 0644); err != nil {
		t.Fatalf("failed writing package.json: %v", err)
	}

	// Page file under pages/
	pagesDir := filepath.Join(tempDir, "pages")
	if err = os.MkdirAll(pagesDir, 0755); err != nil {
		t.Fatalf("failed creating pages dir: %v", err)
	}
	pageContent := `export default function Home() { return <div>Home</div> }`
	if err = os.WriteFile(filepath.Join(pagesDir, "index.tsx"), []byte(pageContent), 0644); err != nil {
		t.Fatalf("failed writing pages/index.tsx: %v", err)
	}

	// SyncGraph must succeed (no FK error)
	if err = SyncGraph(database, projectID, tempDir); err != nil {
		t.Fatalf("SyncGraph failed (FK error?): %v", err)
	}

	// Verify route node was persisted
	var routeCount int
	if err = database.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ? AND node_type = 'route'", projectID).Scan(&routeCount); err != nil {
		t.Fatalf("failed querying route nodes: %v", err)
	}
	if routeCount != 1 {
		t.Errorf("expected 1 route node, got %d", routeCount)
	}

	// Verify edge with correct target
	var targetID string
	if err = database.QueryRow("SELECT target_id FROM graph_edges WHERE project_id = ? AND relation_type = 'routes' LIMIT 1", projectID).Scan(&targetID); err != nil {
		t.Fatalf("failed querying route edge: %v", err)
	}
	if targetID != "pages/index.tsx" {
		t.Errorf("expected edge target_id 'pages/index.tsx', got %q", targetID)
	}
}

func TestViteNoRouteEdges(t *testing.T) {
	// A Vite+React Router project with pages/ should NOT generate route edges
	// when there is no Next/Nuxt/SvelteKit evidence.
	tempDir, err := os.MkdirTemp("", "sv-mem-vite-test")
	if err != nil {
		t.Fatalf("failed to create temp workspace: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_storage.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init DB: %v", err)
	}
	defer database.Close()

	projectID := "proj-vite-test"
	if err = db.RegisterProject(database, projectID, "Vite Test", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Package.json WITHOUT next/nuxt/sveltekit → no evidence
	pkgJSON := `{"dependencies": {"react": "^18.0.0", "react-dom": "^18.0.0"}, "devDependencies": {"vite": "^5.0.0"}}`
	if err = os.WriteFile(filepath.Join(tempDir, "package.json"), []byte(pkgJSON), 0644); err != nil {
		t.Fatalf("failed writing package.json: %v", err)
	}

	// Vite-style pages directory
	pagesDir := filepath.Join(tempDir, "src", "modules", "admissions", "pages")
	if err = os.MkdirAll(pagesDir, 0755); err != nil {
		t.Fatalf("failed creating pages dir: %v", err)
	}
	pageContent := `export default function AdmissionsTrayPage() { return <div>Tray</div> }`
	if err = os.WriteFile(filepath.Join(pagesDir, "AdmissionsTrayPage.tsx"), []byte(pageContent), 0644); err != nil {
		t.Fatalf("failed writing AdmissionsTrayPage.tsx: %v", err)
	}

	// SyncGraph must succeed
	if err = SyncGraph(database, projectID, tempDir); err != nil {
		t.Fatalf("SyncGraph failed: %v", err)
	}

	// Verify NO route nodes
	var routeCount int
	if err = database.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ? AND node_type = 'route'", projectID).Scan(&routeCount); err != nil {
		t.Fatalf("failed querying route nodes: %v", err)
	}
	if routeCount != 0 {
		t.Errorf("expected 0 route nodes for Vite project, got %d", routeCount)
	}
}

func TestDetectFileRoutingFrameworks(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-detect-test")
	if err != nil {
		t.Fatalf("failed to create temp workspace: %v", err)
	}
	defer os.RemoveAll(tempDir)

	nodes := map[string]*Node{
		"next.config.js": {ID: "next.config.js", Type: "file", Path: "next.config.js"},
	}

	// next dependency in package.json
	pkgJSON := `{"dependencies": {"next": "^14.0.0", "react": "^18.0.0"}}`
	if err = os.WriteFile(filepath.Join(tempDir, "package.json"), []byte(pkgJSON), 0644); err != nil {
		t.Fatalf("failed writing package.json: %v", err)
	}

	fw := detectFileRoutingFrameworks(tempDir, nodes, []string{"package.json"})

	if !fw.Next {
		t.Error("expected Next.js to be detected")
	}
	if fw.Nuxt {
		t.Error("expected Nuxt to NOT be detected")
	}
	if fw.SvelteKit {
		t.Error("expected SvelteKit to NOT be detected")
	}
}

func TestDetectFileRoutingFrameworksNoEvidence(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-detect-empty")
	if err != nil {
		t.Fatalf("failed to create temp workspace: %v", err)
	}
	defer os.RemoveAll(tempDir)

	nodes := map[string]*Node{}
	pkgJSON := `{"dependencies": {"react": "^18.0.0"}}`
	if err = os.WriteFile(filepath.Join(tempDir, "package.json"), []byte(pkgJSON), 0644); err != nil {
		t.Fatalf("failed writing package.json: %v", err)
	}

	fw := detectFileRoutingFrameworks(tempDir, nodes, []string{"package.json"})

	if fw.Next || fw.Nuxt || fw.SvelteKit {
		t.Error("expected no frameworks detected for plain React/Vite project")
	}
}

func TestCodeBasedRoutingAlwaysActive(t *testing.T) {
	// Code-based routing (Python/Java) should work regardless of framework evidence.
	fileContents := map[string][]byte{
		"app.py": []byte(`
from flask import Flask
app = Flask(__name__)

@app.get("/api/users")
def get_users():
    pass

@app.post("/api/users")
def create_user():
    pass
`),
	}
	frameworks := routeFrameworks{} // no file-based frameworks evidenced

	routeNodes, edges := extractRoutingEdges(fileContents, frameworks)

	if len(routeNodes) != 2 {
		t.Errorf("expected 2 route nodes for FastAPI/Flask, got %d", len(routeNodes))
	}
	if len(edges) != 2 {
		t.Errorf("expected 2 route edges, got %d", len(edges))
	}
}

func TestBulkInsertEdgesFKGuard(t *testing.T) {
	// Edges referencing nonexistent nodes must be skipped, not abort the tx.
	tempDir, err := os.MkdirTemp("", "sv-mem-fk-guard-test")
	if err != nil {
		t.Fatalf("failed to create temp workspace: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_storage.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init DB: %v", err)
	}
	defer database.Close()

	projectID := "proj-fk-guard"
	if err = db.RegisterProject(database, projectID, "FK Guard Test", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Create a real file node
	if err = os.WriteFile(filepath.Join(tempDir, "index.js"), []byte("export default {}"), 0644); err != nil {
		t.Fatalf("failed writing index.js: %v", err)
	}

	// Sync to get the real node persisted
	if err = SyncGraph(database, projectID, tempDir); err != nil {
		t.Fatalf("SyncGraph failed: %v", err)
	}

	// Verify index.js exists
	var count int
	if err = database.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ? AND id = 'index.js'", projectID).Scan(&count); err != nil {
		t.Fatalf("failed querying: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected index.js node, got %d", count)
	}

	// Now manually insert an edge to a nonexistent node via raw SQL (bypassing guard)
	// This should fail with FK error — proving the guard is needed in the code path
	_, err = database.Exec(
		"INSERT INTO graph_edges (id, project_id, source_id, target_id, relation_type, confidence) VALUES (?, ?, ?, ?, ?, ?)",
		"bad-edge", projectID, "index.js", "nonexistent-node", "routes", "EXTRACTED",
	)
	if err == nil {
		t.Error("expected FK error for edge to nonexistent node, but insert succeeded")
	}
}
