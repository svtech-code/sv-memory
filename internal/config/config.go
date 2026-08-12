package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds configuration parameters for sv-memory.
type Config struct {
	DBPath    string
	ProjectID string
	ProjName  string
	ProjPath  string
}

// LoadGlobalAndLocalConfig loads default, global (~/.sv-memory/config.yaml),
// and local (.sv-memory/config.yaml) configuration settings.
func LoadGlobalAndLocalConfig(projPath string) {
	viper.SetConfigType("yaml")

	// Set default configuration values
	viper.SetDefault("default_db_path", "")
	viper.SetDefault("git_sync_enabled", true)
	viper.SetDefault("conflict_threshold", 0.45)
	viper.SetDefault("default_review_limit", 10)
	viper.SetDefault("auto_compaction_enabled", true)
	viper.SetDefault("compaction_interval_minutes", 60)
	viper.SetDefault("max_response_tokens", 4000)
	viper.SetDefault("max_field_chars", 1000)
	viper.SetDefault("search_expand_chars", 300)
	viper.SetDefault("timeline_why_chars", 200)
	viper.SetDefault("bundle_why_chars", 300)
	viper.SetDefault("context_pack_max_memories", 5)
	viper.SetDefault("graph_boost", true)

	// 1. Load global config: ~/.sv-memory/config.yaml
	home, err := os.UserHomeDir()
	if err == nil {
		globalDir := filepath.Join(home, ".sv-memory")
		_ = os.MkdirAll(globalDir, 0755)
		globalPath := filepath.Join(globalDir, "config.yaml")

		if _, errExists := os.Stat(globalPath); errExists == nil {
			viper.SetConfigFile(globalPath)
			_ = viper.ReadInConfig()
		}
	}

	// 2. Merge local config if exists: <projPath>/.sv-memory/config.yaml
	if projPath != "" {
		localPath := filepath.Join(projPath, ".sv-memory", "config.yaml")
		if _, errExists := os.Stat(localPath); errExists == nil {
			localViper := viper.New()
			localViper.SetConfigFile(localPath)
			localViper.SetConfigType("yaml")
			if errRead := localViper.ReadInConfig(); errRead == nil {
				for _, key := range localViper.AllKeys() {
					viper.Set(key, localViper.Get(key))
				}
			}
		}
	}
}

// WriteConfigKey sets and saves a configuration parameter in either the global
// config file or the project-local config file.
func WriteConfigKey(projPath string, key string, val interface{}, local bool) error {
	var configPath string
	if local {
		if projPath == "" {
			return fmt.Errorf("local configuration requires an active project path")
		}
		localDir := filepath.Join(projPath, ".sv-memory")
		_ = os.MkdirAll(localDir, 0755)
		configPath = filepath.Join(localDir, "config.yaml")
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("could not resolve home directory: %w", err)
		}
		globalDir := filepath.Join(home, ".sv-memory")
		_ = os.MkdirAll(globalDir, 0755)
		configPath = filepath.Join(globalDir, "config.yaml")
	}

	// Read existing keys to avoid overwriting unrelated settings
	v := viper.New()
	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")
	if _, err := os.Stat(configPath); err == nil {
		_ = v.ReadInConfig()
	}

	v.Set(key, val)

	// WriteConfig might fail if the file is new, fallback to WriteConfigAs
	if err := v.WriteConfig(); err != nil {
		if errWrite := v.WriteConfigAs(configPath); errWrite != nil {
			return fmt.Errorf("failed to save config to %s: %w", configPath, errWrite)
		}
	}

	return nil
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

// gitCommandTimeout bounds how long any git helper may wait before giving up.
// Git runs on the hot path of every sv_mem_save; a hung git process (e.g. on a
// locked or networked repository) must not block the CLI indefinitely.
const gitCommandTimeout = 5 * time.Second

// runGit executes a git command in projPath with a timeout and returns its
// trimmed stdout. The timeout is applied via CommandContext so a stalled git
// process is cancelled instead of blocking forever.
func runGit(projPath string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = projPath
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// GetGitRoot executes a git command to find the root of the repository.
func GetGitRoot(cwd string) (string, error) {
	out, err := runGit(cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		// Fallback to current working directory if not a git repo
		abs, errAbs := filepath.Abs(cwd)
		if errAbs != nil {
			return cwd, fmt.Errorf("not a git repository and cannot resolve absolute path: %w", err)
		}
		return abs, nil
	}
	return out, nil
}

// GenerateProjectID generates a stable hash for a given project path.
func GenerateProjectID(projPath string) string {
	hash := sha256.Sum256([]byte(filepath.Clean(projPath)))
	return hex.EncodeToString(hash[:])[:16] // Keep it a clean 16-character hex string
}

// LoadConfig initializes the configuration for the current project.
func LoadConfig(cwd string) (*Config, error) {
	gitRoot, err := GetGitRoot(cwd)
	if err != nil {
		return nil, err
	}

	// Load configuration file hierarchy using Viper
	LoadGlobalAndLocalConfig(gitRoot)

	dbPath := viper.GetString("default_db_path")
	if dbPath == "" {
		dbPath, err = GetDBPath()
		if err != nil {
			return nil, err
		}
	} else {
		// Expand home directory if it starts with ~
		if strings.HasPrefix(dbPath, "~") {
			home, errHome := os.UserHomeDir()
			if errHome == nil {
				dbPath = filepath.Join(home, dbPath[1:])
			}
		}
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
	out, err := runGit(projPath, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	return out
}

// GetGitCommit returns the current short git commit hash.
func GetGitCommit(projPath string) string {
	out, err := runGit(projPath, "rev-parse", "--short", "HEAD")
	if err != nil {
		return ""
	}
	return out
}

// GetGitAuthor returns the git configuration user name.
func GetGitAuthor(projPath string) string {
	out, err := runGit(projPath, "config", "user.name")
	if err != nil || out == "" {
		// Fallback: derive the identity email from GIT_AUTHOR_IDENT
		// ("Name <email> timestamp tz") without a second subprocess look-up.
		if ident, identErr := runGit(projPath, "var", "GIT_AUTHOR_IDENT"); identErr == nil {
			if start, end := strings.Index(ident, "<"), strings.Index(ident, ">"); start >= 0 && end > start {
				return ident[start+1 : end]
			}
		}
		return ""
	}
	return out
}

// GetGitMetadata returns the project's current branch, short commit hash, and
// configured author in a single burst. Callers on the hot path (e.g. every
// sv_mem_save) should invoke this once and cache the result instead of calling
// GetGitBranch/GetGitCommit/GetGitAuthor separately, which would spawn up to
// four git subprocesses.
func GetGitMetadata(projPath string) (branch, commit, author string) {
	branch = GetGitBranch(projPath)
	commit = GetGitCommit(projPath)
	author = GetGitAuthor(projPath)
	return
}
