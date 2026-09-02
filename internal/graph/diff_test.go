package graph

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/svtech-code/sv-memory/internal/db"
)

func TestComputeGraphDiff(t *testing.T) {
	tempDir := t.TempDir()

	// Helper to run git in tempDir
	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = tempDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v, output: %s", args, err, out)
		}
	}

	// 1. Initialize git repository
	git("init")
	git("config", "user.name", "Tester")
	git("config", "user.email", "tester@example.com")

	// Initial file
	file1 := filepath.Join(tempDir, "service.go")
	initialCode := `package main

import "fmt"

func ExistingFunc() {
	fmt.Println("hello")
}

func ToBeDeleted() {
	fmt.Println("delete me")
}
`
	if err := os.WriteFile(file1, []byte(initialCode), 0644); err != nil {
		t.Fatalf("failed writing initial file: %v", err)
	}

	git("add", "service.go")
	git("commit", "-m", "initial commit")

	// 2. Modify working tree:
	// - Modify ExistingFunc line/content
	// - Remove ToBeDeleted
	// - Add NewFunc
	// - Add new file util.go
	modifiedCode := `package main

import (
	"fmt"
	"strings"
)

func ExistingFunc() {
	fmt.Println("hello modified")
}

func NewFunc() string {
	return strings.ToUpper("new")
}
`
	if err := os.WriteFile(file1, []byte(modifiedCode), 0644); err != nil {
		t.Fatalf("failed updating service.go: %v", err)
	}

	file2 := filepath.Join(tempDir, "util.go")
	utilCode := `package main

func Helper() bool {
	return true
}
`
	if err := os.WriteFile(file2, []byte(utilCode), 0644); err != nil {
		t.Fatalf("failed creating util.go: %v", err)
	}

	// 3. Compute graph diff against HEAD
	database, err := db.InitDB(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatalf("failed init db: %v", err)
	}
	defer database.Close()

	projectID := "test-diff-proj"
	report, err := ComputeGraphDiff(database, projectID, tempDir, "HEAD", true)
	if err != nil {
		t.Fatalf("ComputeGraphDiff failed: %v", err)
	}

	if report.ChangedFilesCount != 2 {
		t.Errorf("expected 2 changed files, got %d", report.ChangedFilesCount)
	}

	// Check added symbols: NewFunc and Helper
	addedNames := make(map[string]bool)
	for _, s := range report.AddedSymbols {
		addedNames[s.Name] = true
	}
	if !addedNames["NewFunc"] {
		t.Errorf("expected NewFunc in added symbols, got: %+v", report.AddedSymbols)
	}
	if !addedNames["Helper"] {
		t.Errorf("expected Helper in added symbols, got: %+v", report.AddedSymbols)
	}

	// Check removed symbols: ToBeDeleted
	removedNames := make(map[string]bool)
	for _, s := range report.RemovedSymbols {
		removedNames[s.Name] = true
	}
	if !removedNames["ToBeDeleted"] {
		t.Errorf("expected ToBeDeleted in removed symbols, got: %+v", report.RemovedSymbols)
	}

	// Check added dependencies: "strings"
	addedImps := make(map[string]bool)
	for _, d := range report.AddedDependencies {
		addedImps[d.Target] = true
	}
	if !addedImps["strings"] {
		t.Errorf("expected strings in added dependencies, got: %+v", report.AddedDependencies)
	}

	// 4. Test render output
	rendered := RenderGraphDiffReport(report)
	if !strings.Contains(rendered, "Structural Graph Diff vs `HEAD`") {
		t.Errorf("expected header in rendered markdown, got: %s", rendered)
	}
	if !strings.Contains(rendered, "NewFunc") || !strings.Contains(rendered, "ToBeDeleted") {
		t.Errorf("expected symbol names in rendered markdown, got: %s", rendered)
	}
}
