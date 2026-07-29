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

func TestGitignoreMatcher(t *testing.T) {
	t.Run("dir_only_pattern", func(t *testing.T) {
		m := &gitignoreMatcher{}
		m.addPattern("build/")
		// build/ should match the "build" directory
		if !m.match("build", true) {
			t.Error("expected 'build/' pattern to match directory 'build'")
		}
		// build/ should NOT match a file named "build"
		if m.match("build", false) {
			t.Error("expected 'build/' pattern to NOT match file 'build'")
		}
		// build/ should match nested build directory
		if !m.match("src/build", true) {
			t.Error("expected 'build/' pattern to match nested directory 'src/build'")
		}
	})

	t.Run("negated_pattern", func(t *testing.T) {
		m := &gitignoreMatcher{}
		m.addPattern("*.log")
		m.addPattern("!important.log")
		if !m.match("debug.log", false) {
			t.Error("expected '*.log' to match 'debug.log'")
		}
		if m.match("important.log", false) {
			t.Error("expected '!important.log' to negate match")
		}
	})

	t.Run("rooted_pattern", func(t *testing.T) {
		m := &gitignoreMatcher{}
		m.addPattern("/build")
		// /build should match build at root
		if !m.match("build", false) {
			t.Error("expected '/build' to match 'build' at root")
		}
		// /build should NOT match nested build
		if m.match("src/build", false) {
			t.Error("expected '/build' to NOT match 'src/build'")
		}
	})

	t.Run("wildcard_star", func(t *testing.T) {
		m := &gitignoreMatcher{}
		m.addPattern("*.pyc")
		if !m.match("file.pyc", false) {
			t.Error("expected '*.pyc' to match 'file.pyc'")
		}
		if m.match("file.py", false) {
			t.Error("expected '*.pyc' to NOT match 'file.py'")
		}
	})

	t.Run("double_star", func(t *testing.T) {
		m := &gitignoreMatcher{}
		m.addPattern("a/**/z")
		if !m.match("a/z", false) {
			t.Error("expected 'a/**/z' to match 'a/z'")
		}
		if !m.match("a/b/z", false) {
			t.Error("expected 'a/**/z' to match 'a/b/z'")
		}
		if !m.match("a/b/c/z", false) {
			t.Error("expected 'a/**/z' to match 'a/b/c/z'")
		}
	})

	t.Run("loads_both_ignore_files", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := os.WriteFile(filepath.Join(tmpDir, ".sv-memoryignore"), []byte("secret/\n"), 0644)
		if err != nil {
			t.Fatal(err)
		}
		err = os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte("*.o\n"), 0644)
		if err != nil {
			t.Fatal(err)
		}
		gi, err := loadGitignore(tmpDir)
		if err != nil {
			t.Fatalf("loadGitignore failed: %v", err)
		}
		if !gi.match("secret", true) {
			t.Error("expected .sv-memoryignore 'secret/' to match dir 'secret'")
		}
		if !gi.match("main.o", false) {
			t.Error("expected .gitignore '*.o' to match 'main.o'")
		}
	})
}
