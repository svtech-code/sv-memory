package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/svtech-code/sv-memory/internal/db"
	"github.com/svtech-code/sv-memory/internal/graph"
)

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
