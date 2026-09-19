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
