package config

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// repoDetectionTimeout bounds the best-effort public/private probe so init never
// blocks on a slow network or a hung gh process.
const repoDetectionTimeout = 5 * time.Second

// RepoVisibility reports, best-effort, whether the project's origin repository
// is public. It returns "public", "private", or "unknown". It only probes
// GitHub remotes via the gh CLI; anything else (no origin, non-GitHub host, gh
// missing, timeout, or parse error) returns "unknown". It never fails or blocks
// for longer than repoDetectionTimeout.
func RepoVisibility(projPath string) string {
	remote, err := runGit(projPath, "remote", "get-url", "origin")
	if err != nil || !isGitHubRemote(remote) {
		return "unknown"
	}
	gh, err := exec.LookPath("gh")
	if err != nil {
		return "unknown"
	}
	ctx, cancel := context.WithTimeout(context.Background(), repoDetectionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, gh, "repo", "view", "--json", "visibility", "-q", ".visibility")
	cmd.Dir = projPath
	out, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	switch strings.ToLower(strings.TrimSpace(string(out))) {
	case "public":
		return "public"
	case "private", "internal":
		return "private"
	default:
		return "unknown"
	}
}

// isGitHubRemote reports whether a git remote URL points at github.com,
// supporting both SSH (git@github.com:owner/repo.git) and HTTPS forms.
func isGitHubRemote(remote string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(remote)), "github.com")
}
