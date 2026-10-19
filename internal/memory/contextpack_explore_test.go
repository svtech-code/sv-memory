package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/svtech-code/sv-memory/internal/db"
	"github.com/svtech-code/sv-memory/internal/graph"
)

func TestContextPackExploreMultiSymbolSnippets(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "ctx_explore.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-ctx-explore"
	if regErr := db.RegisterProject(database, projectID, "Explore Proj", tempDir); regErr != nil {
		t.Fatalf("failed to register project: %v", regErr)
	}

	// Graph: main.go imports utils.go; utils.go defines helper().
	if err = os.WriteFile(filepath.Join(tempDir, "main.go"), []byte("package main\nimport \"proj/utils\"\nfunc main(){ helper() }\n"), 0644); err != nil {
		t.Fatalf("failed writing main.go: %v", err)
	}
	if err = os.WriteFile(filepath.Join(tempDir, "utils.go"), []byte("package main\nfunc helper() {}\n"), 0644); err != nil {
		t.Fatalf("failed writing utils.go: %v", err)
	}
	if err = graph.SyncGraph(database, projectID, tempDir); err != nil {
		t.Fatalf("failed syncing graph: %v", err)
	}

	// Multi-symbol explore: resolve utils.go + main.go in one call.
	pack, err := GetContextPack(database, projectID, "utils.go, main.go", 5, false)
	if err != nil {
		t.Fatalf("GetContextPack multi failed: %v", err)
	}
	if pack.Node == nil || !strings.Contains(pack.Node.Label, "utils.go") {
		t.Fatalf("expected primary symbol utils.go, got %+v", pack.Node)
	}

	// Multi-symbol explore resolves the secondary symbol into its own snippet
	// (the call path may be absent when the imported package symbol does not
	// resolve to a concrete node, which is the correct resilient behaviour).
	foundMain := false
	for _, s := range pack.ExtraSnippets {
		if strings.Contains(s.Label, "main.go") {
			foundMain = true
			if s.Text == "" {
				t.Errorf("expected secondary snippet body for main.go, got empty")
			}
		}
	}
	if !foundMain {
		t.Errorf("expected main.go among secondary snippets, got: %+v", pack.ExtraSnippets)
	}

	// Render must include the Related symbols section regardless of call path.
	rendered := RenderContextPack(pack, 20)
	if !strings.Contains(rendered, "### Related symbols (source):") {
		t.Errorf("expected render to include Related symbols section, got:\n%s", rendered)
	}
}

func TestContextPackExploreRendersCallPath(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "ctx_explore2.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-ctx-explore2"
	if regErr := db.RegisterProject(database, projectID, "Explore2 Proj", tempDir); regErr != nil {
		t.Fatalf("failed to register project: %v", regErr)
	}
	if err = os.WriteFile(filepath.Join(tempDir, "alpha.go"), []byte("package main\nfunc Alpha(){ Beta() }\nfunc Beta(){ Gamma() }\nfunc Gamma(){}\n"), 0644); err != nil {
		t.Fatalf("failed writing alpha.go: %v", err)
	}
	if err = graph.SyncGraph(database, projectID, tempDir); err != nil {
		t.Fatalf("failed syncing graph: %v", err)
	}

	// In a single file, Alpha->Beta->Gamma are connected by 'calls' edges, so
	// the shortest path between Alpha and Gamma is a real 3-node chain.
	pack, err := GetContextPack(database, projectID, "Alpha, Gamma", 5, false)
	if err != nil {
		t.Fatalf("GetContextPack failed: %v", err)
	}

	if len(pack.CallPath) == 0 {
		t.Fatalf("expected a call path between Alpha and Gamma, got none (nodes: %+v)", pack.Node)
	}
	labels := make([]string, 0, len(pack.CallPath))
	for _, hop := range pack.CallPath {
		labels = append(labels, hop.Label)
	}
	joined := strings.Join(labels, ">")
	if !strings.Contains(joined, "Alpha") || !strings.Contains(joined, "Beta") || !strings.Contains(joined, "Gamma") {
		t.Errorf("expected call path Alpha>Beta>Gamma, got %q", joined)
	}

	rendered := RenderContextPack(pack, 20)
	if !strings.Contains(rendered, "### Call path") {
		t.Errorf("expected render to include Call path section, got:\n%s", rendered)
	}
}

func TestContextPackExploreCallPathEmptyWithoutTwoSymbols(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "ctx_explore1.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-ctx-explore1"
	if regErr := db.RegisterProject(database, projectID, "Explore1 Proj", tempDir); regErr != nil {
		t.Fatalf("failed to register project: %v", regErr)
	}
	if err = os.WriteFile(filepath.Join(tempDir, "main.go"), []byte("package main\nfunc main() {}\n"), 0644); err != nil {
		t.Fatalf("failed writing main.go: %v", err)
	}
	if err = graph.SyncGraph(database, projectID, tempDir); err != nil {
		t.Fatalf("failed syncing graph: %v", err)
	}

	// Single symbol: no call path, no extra snippets — legacy contract preserved.
	pack, err := GetContextPack(database, projectID, "main.go", 5, false)
	if err != nil {
		t.Fatalf("GetContextPack single failed: %v", err)
	}
	if len(pack.CallPath) != 0 {
		t.Errorf("expected no call path for single symbol, got %+v", pack.CallPath)
	}
	if len(pack.ExtraSnippets) != 0 {
		t.Errorf("expected no extra snippets for single symbol, got %+v", pack.ExtraSnippets)
	}
	if pack.Node == nil || !strings.Contains(pack.Node.Label, "main.go") {
		t.Errorf("expected single-symbol resolve, got %+v", pack.Node)
	}
}

func TestContextPackFileSkeletonAndPostCommitFilter(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "ctx_skeleton.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-ctx-skeleton"
	if regErr := db.RegisterProject(database, projectID, "Skeleton Proj", tempDir); regErr != nil {
		t.Fatalf("failed to register project: %v", regErr)
	}

	src := "package main\n\ntype ServiceConfig struct{}\n\nfunc StartServer() {}\nfunc StopServer() {}\n"
	if err = os.WriteFile(filepath.Join(tempDir, "server.go"), []byte(src), 0644); err != nil {
		t.Fatalf("failed writing server.go: %v", err)
	}
	if err = graph.SyncGraph(database, projectID, tempDir); err != nil {
		t.Fatalf("failed syncing graph: %v", err)
	}

	// Save a high-signal decision and a noisy automatic post-commit journal for server.go.
	if _, err = SaveMemory(database, &Memory{
		ProjectID: projectID,
		Category:  "decision",
		What:      "Graceful shutdown on StopServer",
		Why:       "Prevent dropped connections",
		Learned:   "Always drain before closing",
		WherePath: "server.go",
	}); err != nil {
		t.Fatalf("failed saving decision: %v", err)
	}
	if _, err = SaveMemory(database, &Memory{
		ProjectID: projectID,
		Category:  "journal",
		What:      "git commit: chore(lint): format server.go",
		Why:       "automatic post-commit capture for deadbeef1234",
		Learned:   "Committed 1 file(s) to main",
		WherePath: "server.go",
	}); err != nil {
		t.Fatalf("failed saving post-commit journal: %v", err)
	}

	pack, err := GetContextPack(database, projectID, "server.go", 5, false)
	if err != nil {
		t.Fatalf("GetContextPack failed: %v", err)
	}
	if !strings.Contains(pack.Snippet, "// AST Skeleton for server.go") {
		t.Errorf("expected AST Skeleton header for file node, got:\n%s", pack.Snippet)
	}
	if !strings.Contains(pack.Snippet, "StartServer (function)") || !strings.Contains(pack.Snippet, "StopServer (function)") {
		t.Errorf("expected functions in AST Skeleton, got:\n%s", pack.Snippet)
	}

	for _, m := range pack.Memories {
		if strings.Contains(m.Why, "automatic post-commit capture for") {
			t.Errorf("expected automatic post-commit capture journal to be filtered out, got: %+v", m)
		}
	}
	if len(pack.Memories) != 1 || !strings.Contains(pack.Memories[0].What, "Graceful shutdown") {
		t.Errorf("expected only high-signal decision memory, got: %+v", pack.Memories)
	}
}
