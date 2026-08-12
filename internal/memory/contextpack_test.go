package memory

import (
	"os"
	"path/filepath"
	"strconv"
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

func TestCommunityPathSetAndSearchByPaths(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "boost.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-boost"
	if regErr := db.RegisterProject(database, projectID, "Boost Proj", tempDir); regErr != nil {
		t.Fatalf("failed to register project: %v", regErr)
	}

	// Three files; a.go and b.go belong to community 7, c.go to community 9.
	for _, f := range []string{"a.go", "b.go", "c.go"} {
		if err = os.WriteFile(filepath.Join(tempDir, f), []byte("package main\nfunc "+f+"_fn(){}\n"), 0644); err != nil {
			t.Fatalf("failed writing %s: %v", f, err)
		}
	}
	if err = graph.SyncGraph(database, projectID, tempDir); err != nil {
		t.Fatalf("failed syncing graph: %v", err)
	}
	setCommunity := func(id string, comm int) {
		t.Helper()
		meta := `{"community_id": ` + strconv.Itoa(comm) + `}`
		if _, execErr := database.Exec("UPDATE graph_nodes SET metadata = ? WHERE project_id = ? AND id = ?", meta, projectID, id); execErr != nil {
			t.Fatalf("failed setting community for %s: %v", id, execErr)
		}
	}
	setCommunity("a.go", 7)
	setCommunity("b.go", 7)
	setCommunity("c.go", 9)

	node, err := ResolveContextNode(database, projectID, "a.go")
	if err != nil || node == nil {
		t.Fatalf("failed resolving a.go: %v", err)
	}
	set, err := CommunityPathSet(database, projectID, node)
	if err != nil {
		t.Fatalf("failed CommunityPathSet: %v", err)
	}
	if !set["a.go"] || !set["b.go"] {
		t.Errorf("expected community set to include a.go and b.go, got %v", set)
	}
	if set["c.go"] {
		t.Errorf("expected c.go (community 9) NOT in set, got %v", set)
	}

	// Save one memory per file; the community search must surface a.go + b.go.
	saveMem := func(id, what, where string) {
		t.Helper()
		if _, sErr := SaveMemory(database, &Memory{
			ID:        id,
			ProjectID: projectID,
			Category:  "decision",
			What:      what,
			Why:       "why " + what,
			Learned:   "learned",
			WherePath: where,
		}); sErr != nil {
			t.Fatalf("failed saving %s: %v", id, sErr)
		}
	}
	saveMem("boost-a", "A module decision", "a.go")
	saveMem("boost-b", "B module decision", "b.go")
	saveMem("boost-c", "C module decision", "c.go")

	// Search with pathFilter=a.go expanded to community paths [a.go, b.go].
	paths := []string{"a.go", "b.go"}
	results, err := SearchMemoriesByPaths(database, projectID, "", "", "all", "a.go", paths, 10, 0)
	if err != nil {
		t.Fatalf("failed SearchMemoriesByPaths: %v", err)
	}
	found := map[string]bool{}
	for _, r := range results {
		found[r.ID] = true
	}
	if !found["boost-a"] || !found["boost-b"] {
		t.Errorf("expected community search to include a.go and b.go memories, got %v", found)
	}
	if found["boost-c"] {
		t.Errorf("expected c.go memory excluded, got %v", found)
	}
}
