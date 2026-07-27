package protocol

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInjectProtocol_CreateNew(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "protocol-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	injected, err := InjectProtocol(tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(injected) != 1 || injected[0] != "AGENTS.md" {
		t.Errorf("expected [AGENTS.md], got: %v", injected)
	}

	content, err := os.ReadFile(filepath.Join(tempDir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("failed to read created AGENTS.md: %v", err)
	}

	if !strings.Contains(string(content), "<!-- SV-MEMORY:START -->") {
		t.Error("created AGENTS.md does not contain the start comment tag")
	}
}

func TestInjectProtocol_UpdateExisting(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "protocol-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a pre-existing target file
	targetFile := filepath.Join(tempDir, ".cursorrules")
	initialContent := "## User Custom Rules\n"
	err = os.WriteFile(targetFile, []byte(initialContent), 0644)
	if err != nil {
		t.Fatalf("failed to write initial file: %v", err)
	}

	injected, err := InjectProtocol(tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// It should inject into .cursorrules, and NOT create AGENTS.md since foundAny = true
	if len(injected) != 1 || injected[0] != ".cursorrules" {
		t.Errorf("expected [.cursorrules], got: %v", injected)
	}

	content, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("failed to read updated file: %v", err)
	}

	if !strings.HasPrefix(string(content), "## User Custom Rules\n") {
		t.Error("original content was corrupted or removed")
	}

	if !strings.Contains(string(content), "<!-- SV-MEMORY:START -->") {
		t.Error("updated file does not contain the injected protocol block")
	}

	// Verify idempotency: calling it again should result in no changes
	injected2, err := InjectProtocol(tempDir)
	if err != nil {
		t.Fatalf("unexpected error on second run: %v", err)
	}
	if len(injected2) != 0 {
		t.Errorf("expected no changes on second run, got: %v", injected2)
	}
}
