package security

import (
	"regexp"
)

// Regex patterns and their replacements for detecting and redacting common secrets.
var redactors = []struct {
	pattern     *regexp.Regexp
	replacement string
}{
	// OpenAI API keys
	{regexp.MustCompile(`(?i)sk-[a-zA-Z0-9]{48}`), "[REDACTED_SECRET]"},

	// Anthropic API keys
	{regexp.MustCompile(`(?i)sk-ant-sid[a-zA-Z0-9-_]{40,}`), "[REDACTED_SECRET]"},

	// Google Gemini API keys
	{regexp.MustCompile(`AIzaSy[a-zA-Z0-9-_]{33}`), "[REDACTED_SECRET]"},

	// JWT Tokens
	{regexp.MustCompile(`eyJ[a-zA-Z0-9-_]+\.eyJ[a-zA-Z0-9-_]+\.[a-zA-Z0-9-_]+`), "[REDACTED_SECRET]"},

	// Private Keys (RSA, EC, Generic)
	{regexp.MustCompile(`(?s)-----BEGIN [A-Z ]+ PRIVATE KEY-----.*?-----END [A-Z ]+ PRIVATE KEY-----`), "[REDACTED_SECRET]"},

	// Database Connection Strings (redacts only the password, preserving scheme, user, and host)
	{regexp.MustCompile(`(?i)(postgres|mysql|mongodb|redis|amqp|amqps|sqlite|mssql):\/\/([^:]+):([^@]+)@`), `$1://$2:[REDACTED_SECRET]@`},

	// Common generic tokens/keys assignments (preserves original spacing and separator: '=' or ':')
	{regexp.MustCompile(`(?i)(password|passwd|secret|token|api_key|apikey|private_key)(\s*[:=]\s*)['"]([a-zA-Z0-9-_]{12,})['"]`), `$1$2"[REDACTED_SECRET]"`},
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
