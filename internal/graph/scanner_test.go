package graph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureMemoryIgnore(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. First run should create .sv-memoryignore
	created, err := EnsureMemoryIgnore(tmpDir)
	if err != nil {
		t.Fatalf("EnsureMemoryIgnore failed: %v", err)
	}
	if !created {
		t.Errorf("expected created to be true on first run")
	}

	ignorePath := filepath.Join(tmpDir, ".sv-memoryignore")
	content, err := os.ReadFile(ignorePath)
	if err != nil {
		t.Fatalf("failed to read created .sv-memoryignore: %v", err)
	}
	if !strings.Contains(string(content), "node_modules/") {
		t.Errorf("expected content to contain node_modules/, got: %s", string(content))
	}

	// 2. Second run should not overwrite existing file
	customContent := "# Custom ignore\ncustom_dir/\n"
	err = os.WriteFile(ignorePath, []byte(customContent), 0644)
	if err != nil {
		t.Fatalf("failed to write custom content: %v", err)
	}

	createdSecond, err := EnsureMemoryIgnore(tmpDir)
	if err != nil {
		t.Fatalf("EnsureMemoryIgnore failed on second run: %v", err)
	}
	if createdSecond {
		t.Errorf("expected created to be false when file already exists")
	}

	readContent, err := os.ReadFile(ignorePath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(readContent) != customContent {
		t.Errorf("expected existing file to be preserved, got: %s", string(readContent))
	}
}
