package config

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// repoDetectionTimeout bounds the best-effort public/private probe so init never
// blocks on a slow network or a hung gh process.
const repoDetectionTimeout = 5 * time.Second

const (
	gitignoreLocalComment   = "# sv-memory (local-only / public repo mode)"
	gitignorePrivateComment = "# sv-memory (private team git-sync mode: ignore local runtime state)"
	gitignoreRuntimeEntry   = ".sv-memory/context-injection-enabled"
)

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

// HasGitOrGitignore reports whether projPath has a .git entry or an existing .gitignore file.
func HasGitOrGitignore(projPath string) bool {
	if _, err := os.Stat(filepath.Join(projPath, ".git")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(projPath, ".gitignore")); err == nil {
		return true
	}
	return false
}

// isBlanketStoreIgnoreLine reports whether a trimmed .gitignore line ignores the entire .sv-memory directory.
func isBlanketStoreIgnoreLine(trimmed string) bool {
	switch trimmed {
	case ".sv-memory", ".sv-memory/", "/.sv-memory", "/.sv-memory/", ".sv-memory/*":
		return true
	default:
		return false
	}
}

// isOpenSpecIgnoreLine reports whether a trimmed .gitignore line ignores the openspec mirror directory.
func isOpenSpecIgnoreLine(trimmed string) bool {
	switch trimmed {
	case "openspec", "openspec/", "/openspec", "/openspec/", "openspec/*":
		return true
	default:
		return false
	}
}

// isRuntimeIgnoreLine reports whether a trimmed .gitignore line ignores the local context-injection toggle file.
func isRuntimeIgnoreLine(trimmed string) bool {
	switch trimmed {
	case ".sv-memory/context-injection-enabled", "/.sv-memory/context-injection-enabled":
		return true
	default:
		return false
	}
}

// EnsureGitignoreStore reconciles .gitignore in projPath according to ignoreStore:
//   - When ignoreStore is true (git_sync_enabled=false, default local/public mode):
//     ensures `.sv-memory/` and `openspec/` are present in `.gitignore` so `.sv-memory/specs/`,
//     `openspec/`, and memory chunks are not accidentally committed.
//   - When ignoreStore is false (git_sync_enabled=true, private team mode):
//     removes any blanket `.sv-memory/` and `openspec/` ignore entries from `.gitignore` and
//     ensures `.sv-memory/context-injection-enabled` is ignored.
//
// It returns true when `.gitignore` was created or modified.
func EnsureGitignoreStore(projPath string, ignoreStore bool) (bool, error) {
	gitignorePath := filepath.Join(projPath, ".gitignore")
	raw, err := os.ReadFile(gitignorePath)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	original := string(raw)
	var lines []string
	if len(original) > 0 {
		lines = strings.Split(strings.ReplaceAll(original, "\r\n", "\n"), "\n")
	}

	if ignoreStore {
		hasBlanket := false
		hasOpenSpec := false
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if isBlanketStoreIgnoreLine(trimmed) {
				hasBlanket = true
			}
			if isOpenSpecIgnoreLine(trimmed) {
				hasOpenSpec = true
			}
		}
		if hasBlanket && hasOpenSpec {
			return false, nil
		}

		// Filter out private-mode runtime-only ignore lines before adding missing local/public entries
		filtered := make([]string, 0, len(lines)+4)
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == gitignorePrivateComment || isRuntimeIgnoreLine(trimmed) {
				continue
			}
			if !hasBlanket && trimmed == gitignoreLocalComment {
				continue
			}
			filtered = append(filtered, line)
		}
		filtered = trimTrailingEmptyLines(filtered)
		if !hasBlanket {
			if len(filtered) > 0 {
				filtered = append(filtered, "")
			}
			filtered = append(filtered, gitignoreLocalComment, ".sv-memory/")
		}
		if !hasOpenSpec {
			filtered = append(filtered, "openspec/")
		}
		filtered = append(filtered, "")
		updated := strings.Join(filtered, "\n")
		if updated == original {
			return false, nil
		}
		return true, os.WriteFile(gitignorePath, []byte(updated), 0644)
	}

	// ignoreStore == false (private team git-sync mode):
	// Remove blanket .sv-memory/ and openspec/ ignore rules and ensure .sv-memory/context-injection-enabled is ignored.
	filtered := make([]string, 0, len(lines)+3)
	hasRuntimeIgnore := false
	removedAny := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == gitignoreLocalComment || isBlanketStoreIgnoreLine(trimmed) || isOpenSpecIgnoreLine(trimmed) {
			removedAny = true
			continue
		}
		if isRuntimeIgnoreLine(trimmed) {
			hasRuntimeIgnore = true
		}
		filtered = append(filtered, line)
	}

	if !removedAny && hasRuntimeIgnore {
		return false, nil
	}

	if !hasRuntimeIgnore {
		filtered = trimTrailingEmptyLines(filtered)
		if len(filtered) > 0 {
			filtered = append(filtered, "")
		}
		filtered = append(filtered, gitignorePrivateComment, gitignoreRuntimeEntry, "")
	} else {
		filtered = trimTrailingEmptyLines(filtered)
		filtered = append(filtered, "")
	}

	updated := strings.Join(filtered, "\n")
	if updated == original {
		return false, nil
	}
	return true, os.WriteFile(gitignorePath, []byte(updated), 0644)
}

func trimTrailingEmptyLines(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
