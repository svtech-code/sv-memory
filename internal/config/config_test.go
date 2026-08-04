package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGenerateProjectID(t *testing.T) {
	path1 := "/path/to/project"
	path2 := "/path/to/project/"
	path3 := "/path/to/other-project"

	id1 := GenerateProjectID(path1)
	id2 := GenerateProjectID(path2)
	id3 := GenerateProjectID(path3)

	if id1 == "" {
		t.Fatal("project ID should not be empty")
	}

	if id1 != id2 {
		t.Errorf("GenerateProjectID should normalize path trailing slashes: expected %s, got %s", id1, id2)
	}

	if id1 == id3 {
		t.Errorf("different paths should produce different project IDs: path1=%s, path3=%s", id1, id3)
	}

	if len(id1) != 16 {
		t.Errorf("expected project ID length to be 16, got %d", len(id1))
	}
}

func TestGetGitRootFallback(t *testing.T) {
	// Test GetGitRoot when not in a Git repository or using a temporary directory
	tempDir, err := os.MkdirTemp("", "sv-mem-config-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	resolved, err := GetGitRoot(tempDir)
	if err != nil {
		t.Fatalf("expected no error from fallback, got: %v", err)
	}

	absTemp, err := filepath.Abs(tempDir)
	if err != nil {
		t.Fatalf("failed to resolve absolute path of temp dir: %v", err)
	}

	if resolved != absTemp {
		t.Errorf("expected resolved path to match absolute temp dir path: expected %s, got %s", absTemp, resolved)
	}
}

func TestLoadConfig(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-load-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cfg, err := LoadConfig(tempDir)
	if err != nil {
		t.Fatalf("expected LoadConfig to succeed: %v", err)
	}

	if cfg.ProjPath == "" {
		t.Error("expected ProjPath to be populated")
	}
	if cfg.ProjectID == "" {
		t.Error("expected ProjectID to be populated")
	}
	if cfg.ProjName == "" {
		t.Error("expected ProjName to be populated")
	}
	if cfg.DBPath == "" {
		t.Error("expected DBPath to be populated")
	}
}

func TestViperConfig(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-viper-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Clean up config file created during test
	defer func() {
		os.RemoveAll(filepath.Join(tempDir, ".sv-memory"))
	}()

	customDBPath := filepath.Join(tempDir, "custom_storage.db")

	// Write config key to local path
	err = WriteConfigKey(tempDir, "default_db_path", customDBPath, true)
	if err != nil {
		t.Fatalf("failed to write config key: %v", err)
	}

	// Load configuration
	cfg, err := LoadConfig(tempDir)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.DBPath != customDBPath {
		t.Errorf("expected DBPath to be %s, got %s", customDBPath, cfg.DBPath)
	}
}

func TestGitMetadata(t *testing.T) {
	tempDir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.name", "Test User"},
		{"config", "user.email", "test@example.com"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = tempDir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v (%s)", args, err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(tempDir, "README.md"), []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "README.md"}, {"commit", "-q", "-m", "test"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = tempDir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v (%s)", args, err, output)
		}
	}

	if branch := GetGitBranch(tempDir); branch == "" {
		t.Error("GetGitBranch() returned an empty branch")
	}
	if commit := GetGitCommit(tempDir); commit == "" {
		t.Error("GetGitCommit() returned an empty commit")
	}
	if author := GetGitAuthor(tempDir); author != "Test User" {
		t.Errorf("GetGitAuthor() = %q, want Test User", author)
	}
}

func TestWriteConfigKeyRequiresProjectForLocalConfig(t *testing.T) {
	if err := WriteConfigKey("", "key", "value", true); err == nil {
		t.Error("WriteConfigKey() expected an error for empty local project path")
	}
}
