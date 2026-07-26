package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Config holds configuration parameters for sv-memory.
type Config struct {
	DBPath    string
	ProjectID string
	ProjName  string
	ProjPath  string
}

// GetDBPath returns the default global SQLite database path.
func GetDBPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not get user home directory: %w", err)
	}
	dbDir := filepath.Join(home, ".config", "sv-memory")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return "", fmt.Errorf("could not create configuration directory: %w", err)
	}
	return filepath.Join(dbDir, "storage.db"), nil
}

// GetGitRoot executes a git command to find the root of the repository.
func GetGitRoot(cwd string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		// Fallback to current working directory if not a git repo
		abs, errAbs := filepath.Abs(cwd)
		if errAbs != nil {
			return cwd, fmt.Errorf("not a git repository and cannot resolve absolute path: %w", err)
		}
		return abs, nil
	}
	return strings.TrimSpace(string(out)), nil
}

// GenerateProjectID generates a stable hash for a given project path.
func GenerateProjectID(projPath string) string {
	hash := sha256.Sum256([]byte(filepath.Clean(projPath)))
	return hex.EncodeToString(hash[:])[:16] // Keep it a clean 16-character hex string
}

// LoadConfig initializes the configuration for the current project.
func LoadConfig(cwd string) (*Config, error) {
	dbPath, err := GetDBPath()
	if err != nil {
		return nil, err
	}

	gitRoot, err := GetGitRoot(cwd)
	if err != nil {
		return nil, err
	}

	projName := filepath.Base(gitRoot)
	projID := GenerateProjectID(gitRoot)

	return &Config{
		DBPath:    dbPath,
		ProjectID: projID,
		ProjName:  projName,
		ProjPath:  gitRoot,
	}, nil
}

// GetGitBranch returns the current git branch name.
func GetGitBranch(projPath string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = projPath
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// GetGitCommit returns the current short git commit hash.
func GetGitCommit(projPath string) string {
	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	cmd.Dir = projPath
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// GetGitAuthor returns the git configuration user name or email.
func GetGitAuthor(projPath string) string {
	cmd := exec.Command("git", "config", "user.name")
	cmd.Dir = projPath
	out, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		cmdEmail := exec.Command("git", "config", "user.email")
		cmdEmail.Dir = projPath
		outEmail, errEmail := cmdEmail.Output()
		if errEmail != nil {
			return ""
		}
		return strings.TrimSpace(string(outEmail))
	}
	return strings.TrimSpace(string(out))
}

