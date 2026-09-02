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

	pack, err := GetContextPack(database, projectID, "main.go", 5, false)
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

	pack, err := GetContextPack(database, projectID, "go.mod", 5, false)
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

func TestContextPackExtractsSurgicalSnippet(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "ctx_snippet.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-snippet"
	if regErr := db.RegisterProject(database, projectID, "Snippet Proj", tempDir); regErr != nil {
		t.Fatalf("failed to register project: %v", regErr)
	}

	src := `package service

import "fmt"

type AuthManager struct {
	secret string
}

func (a *AuthManager) Authenticate(token string) bool {
	if token == "" {
		return false
	}
	fmt.Println("Authenticating token...")
	return token == a.secret
}
`
	if err = os.WriteFile(filepath.Join(tempDir, "auth.go"), []byte(src), 0644); err != nil {
		t.Fatalf("failed writing auth.go: %v", err)
	}

	if err = graph.SyncGraph(database, projectID, tempDir); err != nil {
		t.Fatalf("failed syncing graph: %v", err)
	}

	// 1. Context pack for function symbol: auth.go:Authenticate
	pack, err := GetContextPack(database, projectID, "Authenticate", 5, false)
	if err != nil {
		t.Fatalf("failed GetContextPack for Authenticate: %v", err)
	}
	if pack.Node == nil {
		t.Fatal("expected node Authenticate to resolve")
	}
	if pack.Snippet == "" {
		t.Fatalf("expected surgical snippet for Authenticate, got empty")
	}
	if !strings.Contains(pack.Snippet, "func (a *AuthManager) Authenticate") {
		t.Errorf("expected snippet to contain function header, got:\n%s", pack.Snippet)
	}
	if !strings.Contains(pack.Snippet, "fmt.Println") {
		t.Errorf("expected snippet to contain function body, got:\n%s", pack.Snippet)
	}

	rendered := RenderContextPack(pack, 100)
	if !strings.Contains(rendered, "### Source Code Snippet") {
		t.Errorf("expected rendered context pack to have 'Source Code Snippet' section, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "```go") {
		t.Errorf("expected rendered context pack to have '```go' code block, got:\n%s", rendered)
	}
}

func TestContextPackComputesBlastRadius(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "ctx_blast.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-ctx-blast"
	if regErr := db.RegisterProject(database, projectID, "Blast Proj", tempDir); regErr != nil {
		t.Fatalf("failed to register project: %v", regErr)
	}

	// db.go -> service.go -> api.go
	dbSrc := "package main\nfunc QueryDB() {}\n"
	svcSrc := "package main\nfunc ServiceLayer() { QueryDB() }\n"
	apiSrc := "package main\nfunc Endpoint() { ServiceLayer() }\n"

	if err = os.WriteFile(filepath.Join(tempDir, "db.go"), []byte(dbSrc), 0644); err != nil {
		t.Fatalf("failed writing db.go: %v", err)
	}
	if err = os.WriteFile(filepath.Join(tempDir, "service.go"), []byte(svcSrc), 0644); err != nil {
		t.Fatalf("failed writing service.go: %v", err)
	}
	if err = os.WriteFile(filepath.Join(tempDir, "api.go"), []byte(apiSrc), 0644); err != nil {
		t.Fatalf("failed writing api.go: %v", err)
	}

	if err = graph.SyncGraph(database, projectID, tempDir); err != nil {
		t.Fatalf("failed syncing graph: %v", err)
	}

	pack, err := GetContextPack(database, projectID, "QueryDB", 5, false)
	if err != nil {
		t.Fatalf("GetContextPack failed: %v", err)
	}

	if len(pack.BlastRadius) < 2 {
		t.Fatalf("expected at least 2 transitive blast radius nodes, got %d: %+v", len(pack.BlastRadius), pack.BlastRadius)
	}

	rendered := RenderContextPack(pack, 100)
	if !strings.Contains(rendered, "### 💥 Transitive blast radius") {
		t.Errorf("expected rendered context pack to include 'Transitive blast radius' header, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "hop 1") || !strings.Contains(rendered, "hop 2") {
		t.Errorf("expected rendered context pack to show hop depths, got:\n%s", rendered)
	}
}

func TestContextPackAutoSyncsIfStale(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "ctx_stale.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-ctx-stale"
	if regErr := db.RegisterProject(database, projectID, "Stale Proj", tempDir); regErr != nil {
		t.Fatalf("failed to register project: %v", regErr)
	}

	initialSrc := "package main\nfunc InitialFunction() {}\n"
	serviceFile := filepath.Join(tempDir, "service.go")
	if err = os.WriteFile(serviceFile, []byte(initialSrc), 0644); err != nil {
		t.Fatalf("failed writing service.go: %v", err)
	}

	if err = graph.SyncGraph(database, projectID, tempDir); err != nil {
		t.Fatalf("failed syncing graph: %v", err)
	}

	// Now modify service.go to add NewFunction without calling SyncGraph
	updatedSrc := "package main\nfunc InitialFunction() {}\nfunc NewFunction() {\n\tprintln(\"auto fresh\")\n}\n"
	if err = os.WriteFile(serviceFile, []byte(updatedSrc), 0644); err != nil {
		t.Fatalf("failed updating service.go: %v", err)
	}

	// GetContextPack should automatically trigger the staleness probe and resolve NewFunction!
	pack, err := GetContextPack(database, projectID, "NewFunction", 5, false)
	if err != nil {
		t.Fatalf("GetContextPack failed: %v", err)
	}
	if pack.Node == nil {
		t.Fatal("expected NewFunction to be automatically discovered and resolved via staleness probe")
	}
	if pack.Snippet == "" {
		t.Fatal("expected surgical snippet for auto-synced NewFunction")
	}
	if !strings.Contains(pack.Snippet, "func NewFunction()") {
		t.Errorf("expected snippet to contain 'func NewFunction()', got:\n%s", pack.Snippet)
	}
}

func TestContextPackSurfacesTasksProgress(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "ctx_tasks.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-ctx-tasks"
	if regErr := db.RegisterProject(database, projectID, "Tasks Proj", tempDir); regErr != nil {
		t.Fatalf("failed to register project: %v", regErr)
	}

	// Create a change with task checklist affecting internal/auth/
	tasksMarkdown := "- [x] Step 1: Design tokens\n- [ ] Step 2: Implement handler\n- [x] Step 3: Write tests\n- [ ] Step 4: Validate CI\n"
	c, err := CreateChange(database, projectID, "auth-tokens", "Add Token Support", "what", "goal", "internal/auth/", "design", tasksMarkdown)
	if err != nil {
		t.Fatalf("failed creating change: %v", err)
	}

	pack, err := GetContextPack(database, projectID, "internal/auth/", 5, true)
	if err != nil {
		t.Fatalf("GetContextPack failed: %v", err)
	}

	if len(pack.Changes) == 0 {
		t.Fatalf("expected active change to be surfaced for path, got none")
	}
	if pack.Changes[0].ID != c.ID {
		t.Errorf("expected change ID %s, got %s", c.ID, pack.Changes[0].ID)
	}
	if pack.Changes[0].TaskProgress != "2/4 (50%)" {
		t.Errorf("expected TaskProgress '2/4 (50%%)', got %q", pack.Changes[0].TaskProgress)
	}

	rendered := RenderContextPack(pack, 100)
	if !strings.Contains(rendered, "tasks: 2/4 (50%)") {
		t.Errorf("expected rendered context pack to include 'tasks: 2/4 (50%%)', got:\n%s", rendered)
	}
}

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
