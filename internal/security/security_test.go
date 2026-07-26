package security

import (
	"strings"
	"testing"
)

func TestSanitizeText(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		mustFind string
	}{
		{
			name:     "OpenAI Key",
			input:    "My key is sk-A1B2C3D4E5F6G7H8I9J0K1L2M3N4O5P6Q7R8S9T0U1V2W3X4",
			expected: "My key is [REDACTED_SECRET]",
			mustFind: "[REDACTED_SECRET]",
		},
		{
			name:     "Anthropic Key",
			input:    "anthropic key: sk-ant-sid01-abcdefghijklmnopqrstuvwxyz0123456789-_ABCDEF",
			expected: "anthropic key: [REDACTED_SECRET]",
			mustFind: "[REDACTED_SECRET]",
		},
		{
			name:     "Gemini Key",
			input:    "gemini: AIzaSyD1234567890abcdefghijklmnopqrstuv",
			expected: "gemini: [REDACTED_SECRET]",
			mustFind: "[REDACTED_SECRET]",
		},
		{
			name:     "Private Key Block",
			input:    "--- text ---\n-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA0y...\n-----END RSA PRIVATE KEY-----\n--- end ---",
			expected: "--- text ---\n[REDACTED_SECRET]\n--- end ---",
			mustFind: "[REDACTED_SECRET]",
		},
		{
			name:     "Database Connection String Credentials",
			input:    "DB_URL=postgres://admin:superSecretPassword123@localhost:5432/mydb",
			expected: "DB_URL=postgres://admin:[REDACTED_SECRET]@localhost:5432/mydb",
			mustFind: "[REDACTED_SECRET]",
		},
		{
			name:     "Generic Token Assignment",
			input:    "config.api_token = \"abcdef1234567890\"",
			expected: "config.api_token = \"[REDACTED_SECRET]\"",
			mustFind: "[REDACTED_SECRET]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			output := SanitizeText(tc.input)
			if tc.expected != "" && output != tc.expected {
				t.Errorf("expected %q, but got: %q", tc.expected, output)
			}
			if !strings.Contains(output, tc.mustFind) {
				t.Errorf("expected output to contain %q, but got: %q", tc.mustFind, output)
			}
			
			// Verify the original sensitive values are NOT present anymore
			if tc.name == "OpenAI Key" && strings.Contains(output, "sk-A1") {
				t.Error("OpenAI key value was not fully redacted")
			}
			if tc.name == "Database Connection String Credentials" && strings.Contains(output, "superSecretPassword123") {
				t.Error("DB password was not redacted")
			}
		})
	}
}
