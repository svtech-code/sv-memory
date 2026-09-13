package graph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/svtech-code/sv-memory/internal/db"
)

func TestExtractRoutingEdgesCanonicalTarget(t *testing.T) {
	// The edge target_id must be the raw path, not "file:" + path.
	tempDir, err := os.MkdirTemp("", "sv-mem-canonical-test")
	if err != nil {
		t.Fatalf("failed to create temp workspace: %v", err)
	}
	defer os.RemoveAll(tempDir)

	pkgJSON := `{"dependencies": {"next": "^14.0.0"}}`
	if err = os.WriteFile(filepath.Join(tempDir, "package.json"), []byte(pkgJSON), 0644); err != nil {
		t.Fatalf("failed writing package.json: %v", err)
	}

	fileContents := map[string][]byte{
		"pages/index.tsx": []byte("export default function Home() {}"),
	}
	filePkgRoot := map[string]string{"pages/index.tsx": ""}
	nodes := map[string]*Node{}

	routeNodes, edges := extractRoutingEdges(tempDir, fileContents, filePkgRoot, nodes)

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
	if edge.TargetID == "file:pages/index.tsx" {
		t.Error("target_id must not have 'file:' prefix")
	}
}

func TestRoutingEdgesE2ENoFKError(t *testing.T) {
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

	pkgJSON := `{"dependencies": {"next": "^14.0.0", "react": "^18.0.0"}}`
	if err = os.WriteFile(filepath.Join(tempDir, "package.json"), []byte(pkgJSON), 0644); err != nil {
		t.Fatalf("failed writing package.json: %v", err)
	}

	pagesDir := filepath.Join(tempDir, "pages")
	if err = os.MkdirAll(pagesDir, 0755); err != nil {
		t.Fatalf("failed creating pages dir: %v", err)
	}
	pageContent := `export default function Home() { return <div>Home</div> }`
	if err = os.WriteFile(filepath.Join(pagesDir, "index.tsx"), []byte(pageContent), 0644); err != nil {
		t.Fatalf("failed writing pages/index.tsx: %v", err)
	}

	if err = SyncGraph(database, projectID, tempDir); err != nil {
		t.Fatalf("SyncGraph failed (FK error?): %v", err)
	}

	var routeCount int
	if err = database.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ? AND node_type = 'route'", projectID).Scan(&routeCount); err != nil {
		t.Fatalf("failed querying route nodes: %v", err)
	}
	if routeCount != 1 {
		t.Errorf("expected 1 route node, got %d", routeCount)
	}

	var targetID string
	if err = database.QueryRow("SELECT target_id FROM graph_edges WHERE project_id = ? AND relation_type = 'routes' LIMIT 1", projectID).Scan(&targetID); err != nil {
		t.Fatalf("failed querying route edge: %v", err)
	}
	if targetID != "pages/index.tsx" {
		t.Errorf("expected edge target_id 'pages/index.tsx', got %q", targetID)
	}
}

func TestViteNoRouteEdges(t *testing.T) {
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

	pkgJSON := `{"dependencies": {"react": "^18.0.0", "react-dom": "^18.0.0"}, "devDependencies": {"vite": "^5.0.0"}}`
	if err = os.WriteFile(filepath.Join(tempDir, "package.json"), []byte(pkgJSON), 0644); err != nil {
		t.Fatalf("failed writing package.json: %v", err)
	}

	pagesDir := filepath.Join(tempDir, "src", "modules", "admissions", "pages")
	if err = os.MkdirAll(pagesDir, 0755); err != nil {
		t.Fatalf("failed creating pages dir: %v", err)
	}
	pageContent := `export default function AdmissionsTrayPage() { return <div>Tray</div> }`
	if err = os.WriteFile(filepath.Join(pagesDir, "AdmissionsTrayPage.tsx"), []byte(pageContent), 0644); err != nil {
		t.Fatalf("failed writing AdmissionsTrayPage.tsx: %v", err)
	}

	if err = SyncGraph(database, projectID, tempDir); err != nil {
		t.Fatalf("SyncGraph failed: %v", err)
	}

	var routeCount int
	if err = database.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ? AND node_type = 'route'", projectID).Scan(&routeCount); err != nil {
		t.Fatalf("failed querying route nodes: %v", err)
	}
	if routeCount != 0 {
		t.Errorf("expected 0 route nodes for Vite project, got %d", routeCount)
	}
}

func TestDetectPackageFrameworks(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-detect-test")
	if err != nil {
		t.Fatalf("failed to create temp workspace: %v", err)
	}
	defer os.RemoveAll(tempDir)

	nodes := map[string]*Node{
		"next.config.js": {ID: "next.config.js", Type: "file", Path: "next.config.js"},
	}

	pkgJSON := `{"dependencies": {"next": "^14.0.0", "react": "^18.0.0"}}`
	if err = os.WriteFile(filepath.Join(tempDir, "package.json"), []byte(pkgJSON), 0644); err != nil {
		t.Fatalf("failed writing package.json: %v", err)
	}

	fw := detectPackageFrameworks(tempDir, nodes, "")

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

func TestDetectPackageFrameworksNoEvidence(t *testing.T) {
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

	fw := detectPackageFrameworks(tempDir, nodes, "")

	if fw.Next || fw.Nuxt || fw.SvelteKit {
		t.Error("expected no frameworks detected for plain React/Vite project")
	}
}

func TestCodeBasedRoutingAlwaysActive(t *testing.T) {
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
	filePkgRoot := map[string]string{"app.py": ""}
	nodes := map[string]*Node{}

	routeNodes, edges := extractRoutingEdges("", fileContents, filePkgRoot, nodes)

	if len(routeNodes) != 2 {
		t.Errorf("expected 2 route nodes for FastAPI/Flask, got %d", len(routeNodes))
	}
	if len(edges) != 2 {
		t.Errorf("expected 2 route edges, got %d", len(edges))
	}
}

func TestMonorepoRouteIDScoping(t *testing.T) {
	// Two apps with the same route path must produce distinct route nodes.
	tempDir, err := os.MkdirTemp("", "sv-mem-monorepo-test")
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

	projectID := "proj-monorepo-test"
	if err = db.RegisterProject(database, projectID, "Monorepo Test", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Root package.json with next → enables routing repo-wide
	rootPkg := `{"dependencies": {"next": "^14.0.0", "react": "^18.0.0"}}`
	if err = os.WriteFile(filepath.Join(tempDir, "package.json"), []byte(rootPkg), 0644); err != nil {
		t.Fatalf("failed writing root package.json: %v", err)
	}

	// Two apps, each with pages/index.tsx AND its own package.json
	for _, app := range []string{"frontend", "admin"} {
		appDir := filepath.Join(tempDir, app)
		if err = os.MkdirAll(filepath.Join(appDir, "pages"), 0755); err != nil {
			t.Fatalf("failed creating %s/pages: %v", app, err)
		}
		content := "export default function " + app + "Home() {}"
		if err = os.WriteFile(filepath.Join(appDir, "pages", "index.tsx"), []byte(content), 0644); err != nil {
			t.Fatalf("failed writing %s/pages/index.tsx: %v", app, err)
		}
		// Each app has its own package.json with next dep
		appPkg := `{"dependencies": {"next": "^14.0.0"}}`
		if err = os.WriteFile(filepath.Join(appDir, "package.json"), []byte(appPkg), 0644); err != nil {
			t.Fatalf("failed writing %s/package.json: %v", app, err)
		}
	}

	if err = SyncGraph(database, projectID, tempDir); err != nil {
		t.Fatalf("SyncGraph failed: %v", err)
	}

	// Must produce 2 distinct route nodes (not 1 collision)
	var routeCount int
	if err = database.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ? AND node_type = 'route'", projectID).Scan(&routeCount); err != nil {
		t.Fatalf("failed querying route nodes: %v", err)
	}
	if routeCount != 2 {
		t.Errorf("expected 2 distinct route nodes for monorepo, got %d", routeCount)
	}

	// Must produce 2 route edges
	var edgeCount int
	if err = database.QueryRow("SELECT COUNT(*) FROM graph_edges WHERE project_id = ? AND relation_type = 'routes'", projectID).Scan(&edgeCount); err != nil {
		t.Fatalf("failed querying route edges: %v", err)
	}
	if edgeCount != 2 {
		t.Errorf("expected 2 route edges for monorepo, got %d", edgeCount)
	}
}

func TestBulkInsertEdgesFKGuard(t *testing.T) {
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

	if err = os.WriteFile(filepath.Join(tempDir, "index.js"), []byte("export default {}"), 0644); err != nil {
		t.Fatalf("failed writing index.js: %v", err)
	}

	if err = SyncGraph(database, projectID, tempDir); err != nil {
		t.Fatalf("SyncGraph failed: %v", err)
	}

	var count int
	if err = database.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ? AND id = 'index.js'", projectID).Scan(&count); err != nil {
		t.Fatalf("failed querying: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected index.js node, got %d", count)
	}

	_, err = database.Exec(
		"INSERT INTO graph_edges (id, project_id, source_id, target_id, relation_type, confidence) VALUES (?, ?, ?, ?, ?, ?)",
		"bad-edge", projectID, "index.js", "nonexistent-node", "routes", "EXTRACTED",
	)
	if err == nil {
		t.Error("expected FK error for edge to nonexistent node, but insert succeeded")
	}
}
