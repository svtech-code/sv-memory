package security

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Regex patterns and their replacements for detecting and redacting common secrets.
var redactors = []struct {
	pattern     *regexp.Regexp
	replacement string
}{
	// OpenAI API keys (legacy and new sk-proj-* format)
	{regexp.MustCompile(`(?i)sk-[a-zA-Z0-9-_]{40,}`), "[REDACTED_SECRET]"},

	// Anthropic API keys (legacy and new sk-ant-api03-* format)
	{regexp.MustCompile(`(?i)sk-ant-[a-zA-Z0-9-_]{30,}`), "[REDACTED_SECRET]"},

	// Google Gemini API keys
	{regexp.MustCompile(`AIzaSy[a-zA-Z0-9-_]{33}`), "[REDACTED_SECRET]"},

	// JWT Tokens
	{regexp.MustCompile(`eyJ[a-zA-Z0-9-_]+\.eyJ[a-zA-Z0-9-_]+\.[a-zA-Z0-9-_]+`), "[REDACTED_SECRET]"},

	// Private Keys (RSA, EC, Generic)
	{regexp.MustCompile(`(?s)-----BEGIN [A-Z ]+ PRIVATE KEY-----.*?-----END [A-Z ]+ PRIVATE KEY-----`), "[REDACTED_SECRET]"},

	// Database Connection Strings (redacts only the password, preserving scheme, user, and host)
	{regexp.MustCompile(`(?i)(postgres|mysql|mongodb|redis|amqp|amqps|sqlite|mssql):\/\/([^:]+):([^@]+)@`), `$1://$2:[REDACTED_SECRET]@`},

	// Common generic tokens/keys assignments with quotes (preserves original spacing and quotes)
	{regexp.MustCompile(`(?i)(password|passwd|secret|token|api_key|apikey|private_key)(\s*[:=]\s*)(['"])([a-zA-Z0-9-_]{12,})(['"])`), `$1$2$3[REDACTED_SECRET]$5`},

	// Common generic tokens/keys assignments without quotes (covers export VAR=value, key: value, etc.)
	{regexp.MustCompile(`(?i)(password|passwd|secret|token|api_key|apikey|private_key)(\s*[:=]\s*)([a-zA-Z0-9-_]{12,})`), `$1$2[REDACTED_SECRET]`},
}

// SanitizeText scans the input string and redacts any detected secrets.
func SanitizeText(input string) string {
	if input == "" {
		return ""
	}

	sanitized := input
	for _, r := range redactors {
		sanitized = r.pattern.ReplaceAllString(sanitized, r.replacement)
	}

	return sanitized
}

// ValidateWritePath checks that userPath, when resolved against projPath,
// stays within projPath. Returns the cleaned absolute path or an error.
func ValidateWritePath(projPath, userPath string) (string, error) {
	if filepath.IsAbs(userPath) {
		return "", fmt.Errorf("absolute path not allowed: %q", userPath)
	}
	absPath, err := filepath.Abs(filepath.Join(projPath, userPath))
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	projAbs, err := filepath.Abs(projPath)
	if err != nil {
		return "", fmt.Errorf("invalid project path: %w", err)
	}
	// Use filepath.Rel to detect path traversal: if the relative path starts
	// with "..", the resolved path escapes the project directory.
	rel, err := filepath.Rel(projAbs, absPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path %q escapes project directory", userPath)
	}
	projReal, err := filepath.EvalSymlinks(projAbs)
	if err != nil {
		return "", fmt.Errorf("failed to resolve project path: %w", err)
	}

	// Validate that the resolved real path stays inside the project. If the
	// target does not exist yet (the common case for a file about to be
	// written), resolve the deepest existing ancestor instead: a symlinked
	// parent directory can otherwise redirect the write outside the project.
	checkContained := func(p string) error {
		real, rErr := filepath.EvalSymlinks(p)
		if rErr != nil {
			return fmt.Errorf("failed to resolve symlinks: %w", rErr)
		}
		symRel, relErr := filepath.Rel(projReal, real)
		if relErr != nil || strings.HasPrefix(symRel, "..") {
			return fmt.Errorf("path %q escapes project directory via symlink", userPath)
		}
		return nil
	}

	probe := absPath
	for {
		if _, sErr := os.Lstat(probe); sErr == nil {
			if cErr := checkContained(probe); cErr != nil {
				return "", cErr
			}
			return absPath, nil
		} else if !os.IsNotExist(sErr) {
			return "", fmt.Errorf("failed to stat path: %w", sErr)
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", fmt.Errorf("failed to resolve symlinks for %q", userPath)
		}
		probe = parent
	}
}

// SanitizeSQLitePathFilter escapes SQL LIKE wildcards (% and _) in path filter input.
func SanitizeSQLitePathFilter(input string) string {
	input = strings.ReplaceAll(input, "\\", "\\\\")
	input = strings.ReplaceAll(input, "%", "\\%")
	input = strings.ReplaceAll(input, "_", "\\_")
	return input
}
