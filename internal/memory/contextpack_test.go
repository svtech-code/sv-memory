package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/svtech-code/sv-memory/internal/db"
	"github.com/svtech-code/sv-memory/internal/graph"
)

func TestContextPackResolvesNodeAndMemories(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "ctx.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-ctx"
	if regErr := db.RegisterProject(database, projectID, "Ctx Proj", tempDir); regErr != nil {
		t.Fatalf("failed to register project: %v", regErr)
	}

	// Build a tiny graph: main.go imports utils.go.
	if err = os.WriteFile(filepath.Join(tempDir, "main.go"), []byte("package main\nimport \"proj/utils\"\nfunc main(){ helper() }\n"), 0644); err != nil {
		t.Fatalf("failed writing main.go: %v", err)
	}
	if err = os.WriteFile(filepath.Join(tempDir, "utils.go"), []byte("package main\nfunc helper() {}\n"), 0644); err != nil {
		t.Fatalf("failed writing utils.go: %v", err)
	}
	if err = graph.SyncGraph(database, projectID, tempDir); err != nil {
		t.Fatalf("failed syncing graph: %v", err)
	}

	// A memory saved with where_path main.go links via both where_path and the
	// auto-created rationale_for edge.
	mem, err := SaveMemory(database, &Memory{
		ProjectID: projectID,
		Category:  "decision",
		What:      "Use package-level layout",
		Why:       "Keep entry point thin",
		Learned:   "Prefer small main",
		WherePath: "main.go",
	})
	if err != nil {
		t.Fatalf("failed saving memory: %v", err)
	}

	pack, err := GetContextPack(database, projectID, "main.go", 5)
	if err != nil {
		t.Fatalf("failed GetContextPack: %v", err)
	}
	if pack.Node == nil {
		t.Fatal("expected context pack to resolve main.go node")
	}
	if pack.Node.Label != "main.go" {
		t.Errorf("expected node main.go, got %s", pack.Node.Label)
	}
	if pack.Node.FanOut < 1 {
		t.Errorf("expected main.go to have at least one dependency, got fan-out %d", pack.Node.FanOut)
	}

	// The memory must appear exactly once (where_path + rationale_for dedup).
	count := 0
	for _, m := range pack.Memories {
		if m.ID == mem.ID {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected linked memory deduplicated (once), got %d occurrences: %+v", count, pack.Memories)
	}

	// Render smoke test: title present and why truncated to the cap.
	rendered := RenderContextPack(pack, 10)
	if !strings.Contains(rendered, "Use package-level layout") {
		t.Errorf("expected rendered pack to include memory title, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "[truncated") {
		t.Errorf("expected long why to be truncated by the cap (20 chars > 10), got:\n%s", rendered)
	}
}

func TestResolveContextNodeFuzzyAndMissing(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "ctxf.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-ctxf"
	if regErr := db.RegisterProject(database, projectID, "Ctx F Proj", tempDir); regErr != nil {
		t.Fatalf("failed to register project: %v", regErr)
	}
	if err = os.WriteFile(filepath.Join(tempDir, "utils.go"), []byte("package main\nfunc helper() {}\n"), 0644); err != nil {
		t.Fatalf("failed writing utils.go: %v", err)
	}
	if err = graph.SyncGraph(database, projectID, tempDir); err != nil {
		t.Fatalf("failed syncing graph: %v", err)
	}

	// Fuzzy: "util" resolves to utils.go.
	node, err := ResolveContextNode(database, projectID, "util")
	if err != nil {
		t.Fatalf("failed ResolveContextNode: %v", err)
	}
	if node == nil || node.Label != "utils.go" {
		t.Fatalf("expected fuzzy match to utils.go, got %+v", node)
	}

	// Missing path -> nil, not an error.
	missing, err := ResolveContextNode(database, projectID, "definitely-not-a-file.go")
	if err != nil {
		t.Fatalf("failed ResolveContextNode missing: %v", err)
	}
	if missing != nil {
		t.Fatalf("expected nil for missing node, got %+v", missing)
	}
}

func TestContextPackNoGraphStillReturnsMemories(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "ctxn.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-ctxn"
	if regErr := db.RegisterProject(database, projectID, "Ctx N Proj", tempDir); regErr != nil {
		t.Fatalf("failed to register project: %v", regErr)
	}

	// Memory saved with a where_path but the graph was never synced.
	if _, err = SaveMemory(database, &Memory{
		ProjectID: projectID,
		Category:  "standard",
		What:      "Pin dependencies",
		Why:       "Reproducible builds",
		Learned:   "Commit go.sum",
		WherePath: "go.mod",
	}); err != nil {
		t.Fatalf("failed saving memory: %v", err)
	}

	pack, err := GetContextPack(database, projectID, "go.mod", 5)
	if err != nil {
		t.Fatalf("failed GetContextPack: %v", err)
	}
	if pack.Node != nil {
		t.Fatalf("expected no graph node (graph never synced), got %+v", pack.Node)
	}
	if len(pack.Memories) != 1 || pack.Memories[0].What != "Pin dependencies" {
		t.Fatalf("expected path-scoped memory without a graph, got %+v", pack.Memories)
	}
}
