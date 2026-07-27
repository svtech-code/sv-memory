package security

import (
	"regexp"
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
