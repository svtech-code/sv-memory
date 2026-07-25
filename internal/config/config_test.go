package config

import (
	"os"
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
