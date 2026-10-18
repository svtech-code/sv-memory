package config

import (
	"os"
	"testing"
)

func TestIsGitHubRemote(t *testing.T) {
	cases := []struct {
		remote string
		want   bool
	}{
		{"git@github.com:svtech-code/sv-memory.git", true},
		{"https://github.com/svtech-code/sv-memory.git", true},
		{"https://GitHub.com/owner/repo", true},
		{"git@gitlab.com:owner/repo.git", false},
		{"https://bitbucket.org/owner/repo", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isGitHubRemote(tc.remote); got != tc.want {
			t.Errorf("isGitHubRemote(%q) = %v, want %v", tc.remote, got, tc.want)
		}
	}
}

// TestRepoVisibilityNonRepo verifies the probe fails open to "unknown" when the
// path is not a git repository (no origin), without requiring gh or network.
func TestRepoVisibilityNonRepo(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-repo-visibility")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	if got := RepoVisibility(tempDir); got != "unknown" {
		t.Errorf("RepoVisibility(non-repo) = %q, want %q", got, "unknown")
	}
}

func TestEnsureGitignoreStoreLifecycle(t *testing.T) {
	tempDir := t.TempDir()

	if HasGitOrGitignore(tempDir) {
		t.Fatalf("expected HasGitOrGitignore to be false on empty dir")
	}

	// Seed an existing .gitignore with unrelated entries
	gitignorePath := tempDir + "/.gitignore"
	if err := os.WriteFile(gitignorePath, []byte("node_modules/\ndist/\n"), 0644); err != nil {
		t.Fatalf("write initial .gitignore: %v", err)
	}
	if !HasGitOrGitignore(tempDir) {
		t.Fatalf("expected HasGitOrGitignore to be true when .gitignore exists")
	}

	// 1. Default local/public mode (ignoreStore = true) adds .sv-memory/ and openspec/
	changed, err := EnsureGitignoreStore(tempDir, true)
	if err != nil {
		t.Fatalf("EnsureGitignoreStore(true): %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true when adding .sv-memory/ to .gitignore")
	}
	raw, _ := os.ReadFile(gitignorePath)
	content := string(raw)
	if !containsLine(content, ".sv-memory/") || !containsLine(content, "openspec/") || !containsLine(content, "node_modules/") {
		t.Fatalf("expected .gitignore to contain .sv-memory/, openspec/, and preserve node_modules/, got:\n%s", content)
	}

	// Idempotent second call in local/public mode
	changed, err = EnsureGitignoreStore(tempDir, true)
	if err != nil || changed {
		t.Fatalf("expected idempotent EnsureGitignoreStore(true) to return changed=false, err=nil; got changed=%v, err=%v", changed, err)
	}

	// 2. Switch to Private Team Git-Sync mode (ignoreStore = false)
	changed, err = EnsureGitignoreStore(tempDir, false)
	if err != nil {
		t.Fatalf("EnsureGitignoreStore(false): %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true when switching to private team git-sync mode")
	}
	raw, _ = os.ReadFile(gitignorePath)
	content = string(raw)
	if containsLine(content, ".sv-memory/") || containsLine(content, "openspec/") {
		t.Fatalf("expected blanket .sv-memory/ and openspec/ to be removed in private team mode, got:\n%s", content)
	}
	if !containsLine(content, ".sv-memory/context-injection-enabled") || !containsLine(content, "node_modules/") {
		t.Fatalf("expected runtime state ignore and preserved node_modules/ in private team mode, got:\n%s", content)
	}

	// Idempotent second call in private team mode
	changed, err = EnsureGitignoreStore(tempDir, false)
	if err != nil || changed {
		t.Fatalf("expected idempotent EnsureGitignoreStore(false) to return changed=false, err=nil; got changed=%v, err=%v", changed, err)
	}

	// 3. Switch back to Local/Public mode (ignoreStore = true)
	changed, err = EnsureGitignoreStore(tempDir, true)
	if err != nil || !changed {
		t.Fatalf("expected switching back to local/public mode to return changed=true, err=nil; got changed=%v, err=%v", changed, err)
	}
	raw, _ = os.ReadFile(gitignorePath)
	content = string(raw)
	if !containsLine(content, ".sv-memory/") || !containsLine(content, "openspec/") || containsLine(content, ".sv-memory/context-injection-enabled") {
		t.Fatalf("expected .sv-memory/ and openspec/ restored and runtime-only line cleaned up, got:\n%s", content)
	}
}

func containsLine(content, want string) bool {
	for _, line := range splitLines(content) {
		if line == want {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
