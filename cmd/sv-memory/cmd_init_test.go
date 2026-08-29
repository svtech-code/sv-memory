package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/svtech-code/sv-memory/internal/hook"
)

func resetInitFlags() {
	_ = initCmd.Flags().Set("skip-setup", "false")
	_ = initCmd.Flags().Set("strict", "false")
	_ = initCmd.Flags().Set("agent", "")
	_ = initCmd.Flags().Set("agents", "")
	_ = initCmd.Flags().Set("all", "false")
}

// TestInitWithGitHook verifies that running init on a directory containing .git
// installs the Git post-commit hook automatically.
func TestInitWithGitHook(t *testing.T) {
	tempDir := t.TempDir()

	// Create fake .git directory
	gitDir := filepath.Join(tempDir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatalf("failed to create .git dir: %v", err)
	}

	oldCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	defer os.Chdir(oldCWD)
	if err = os.Chdir(tempDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	resetInitFlags()
	rootCmd.SetArgs([]string{"init", "--skip-setup"})
	if err = rootCmd.Execute(); err != nil {
		t.Fatalf("rootCmd.Execute failed: %v", err)
	}

	// Verify Git hook is installed
	hookPath := filepath.Join(gitDir, "hooks", "post-commit")
	if _, err = os.Stat(hookPath); err != nil {
		t.Fatalf("expected git hook at %s: %v", hookPath, err)
	}

	content, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("failed to read git hook: %v", err)
	}
	if !strings.Contains(string(content), "sv-memory") {
		t.Errorf("expected git hook to contain sv-memory, got: %s", string(content))
	}
}

// TestInitExplicitSingleAgent verifies that --agent flags configures only that agent.
func TestInitExplicitSingleAgent(t *testing.T) {
	tempDir := t.TempDir()

	oldCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	defer os.Chdir(oldCWD)
	if err = os.Chdir(tempDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	resetInitFlags()
	rootCmd.SetArgs([]string{"init", "--agent", "cursor"})
	if err = rootCmd.Execute(); err != nil {
		t.Fatalf("rootCmd.Execute failed: %v", err)
	}

	if !statusCursor(tempDir) {
		t.Error("expected cursor to be installed")
	}
	if statusClaudeCode(tempDir) || statusAntigravity(tempDir) || statusWindsurf(tempDir) {
		t.Error("expected other agents not to be installed")
	}
}

// TestInitExplicitMultipleAgents verifies that --agents flags configures specified agents.
func TestInitExplicitMultipleAgents(t *testing.T) {
	tempDir := t.TempDir()

	oldCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	defer os.Chdir(oldCWD)
	if err = os.Chdir(tempDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	resetInitFlags()
	rootCmd.SetArgs([]string{"init", "--agents", "cursor,windsurf"})
	if err = rootCmd.Execute(); err != nil {
		t.Fatalf("rootCmd.Execute failed: %v", err)
	}

	if !statusCursor(tempDir) {
		t.Error("expected cursor to be installed")
	}
	if !statusWindsurf(tempDir) {
		t.Error("expected windsurf to be installed")
	}
	if statusClaudeCode(tempDir) || statusAntigravity(tempDir) || statusCodex(tempDir) {
		t.Error("expected other agents not to be installed")
	}
}

// TestInitNonInteractiveFreshDir verifies that running init without flags in a non-interactive
// fresh directory does NOT auto-wire all 6 agents.
func TestInitNonInteractiveFreshDir(t *testing.T) {
	tempDir := t.TempDir()

	oldCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	defer os.Chdir(oldCWD)
	if err = os.Chdir(tempDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	resetInitFlags()
	rootCmd.SetArgs([]string{"init"})
	if err = rootCmd.Execute(); err != nil {
		t.Fatalf("rootCmd.Execute failed: %v", err)
	}

	if statusClaudeCode(tempDir) || statusAntigravity(tempDir) || statusCursor(tempDir) ||
		statusWindsurf(tempDir) || statusOpenCode(tempDir) || statusCodex(tempDir) {
		t.Error("expected no AI assistants to be auto-installed on fresh directory without prompt")
	}

	// Verify AGENTS.md was created
	if !fileExists(filepath.Join(tempDir, "AGENTS.md")) {
		t.Error("expected AGENTS.md to be created")
	}
}

// TestInitReconcilesExistingAgents verifies that running init in a non-interactive directory
// with an existing agent reconciles that agent without installing others.
func TestInitReconcilesExistingAgents(t *testing.T) {
	tempDir := t.TempDir()

	oldCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	defer os.Chdir(oldCWD)
	if err = os.Chdir(tempDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	// Pre-install Antigravity
	eng := hook.New(tempDir, hook.ModeSoft)
	res := eng.Install([]hook.Platform{hook.PlatformAntigravity})
	if len(res) != 1 || res[0].Err != nil {
		t.Fatalf("pre-install antigravity failed: %v", res)
	}

	resetInitFlags()
	rootCmd.SetArgs([]string{"init"})
	if err = rootCmd.Execute(); err != nil {
		t.Fatalf("rootCmd.Execute failed: %v", err)
	}

	if !statusAntigravity(tempDir) {
		t.Error("expected antigravity to still be installed")
	}
	if statusClaudeCode(tempDir) || statusCursor(tempDir) || statusWindsurf(tempDir) {
		t.Error("expected unconfigured agents not to be installed")
	}
}
