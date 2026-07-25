package security

import (
	"regexp"
)

// Regex patterns for detecting common secrets and credentials.
var secretPatterns = []*regexp.Regexp{
	// OpenAI API keys
	regexp.MustCompile(`(?i)sk-[a-zA-Z0-9]{48}`),
	
	// Anthropic API keys
	regexp.MustCompile(`(?i)sk-ant-sid[a-zA-Z0-9-_]{40,}`),
	
	// Google Gemini API keys
	regexp.MustCompile(`AIzaSy[a-zA-Z0-9-_]{33}`),
	
	// JWT Tokens
	regexp.MustCompile(`eyJ[a-zA-Z0-9-_]+\.eyJ[a-zA-Z0-9-_]+\.[a-zA-Z0-9-_]+`),
	
	// Private Keys (RSA, EC, Generic)
	regexp.MustCompile(`(?s)-----BEGIN [A-Z ]+ PRIVATE KEY-----.*?-----END [A-Z ]+ PRIVATE KEY-----`),
	
	// Database Connection Strings
	regexp.MustCompile(`(?i)(postgres|mysql|mongodb|redis|amqp|amqps|sqlite|mssql):\/\/([^:]+):([^@]+)@`),
	
	// Common generic tokens/keys assignments (case insensitive)
	// Matches: token = "xyz...", password: 'abc...', secret: "123..."
	regexp.MustCompile(`(?i)(password|passwd|secret|token|api_key|apikey|private_key)\s*[:=]\s*['"]([a-zA-Z0-9-_]{12,})['"]`),
}

// SanitizeText scans the input string and redacts any detected secrets.
func SanitizeText(input string) string {
	if input == "" {
		return ""
	}

	sanitized := input
	for _, pattern := range secretPatterns {
		// If it's a generic assignment, we want to redact only the value, not the key name.
		// For example, in: token = "secret123", we want: token = "[REDACTED_SECRET]"
		if pattern.NumSubexp() >= 2 {
			sanitized = pattern.ReplaceAllString(sanitized, `$1: "[REDACTED_SECRET]"`)
		} else {
			sanitized = pattern.ReplaceAllString(sanitized, "[REDACTED_SECRET]")
		}
	}

	return sanitized
}
