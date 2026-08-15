package security

import (
	"os"
	"path/filepath"
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
		{
			name:     "OpenAI sk-proj Key",
			input:    "api_key: sk-proj-A1B2C3D4E5F6G7H8I9J0K1L2M3N4O5P6Q7R8S9T0U1V2W3X4",
			expected: "api_key: [REDACTED_SECRET]",
			mustFind: "[REDACTED_SECRET]",
		},
		{
			name:     "Anthropic sk-ant-api03 Key",
			input:    "anthropic_key = sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789-_ABCDEF",
			expected: "anthropic_key = [REDACTED_SECRET]",
			mustFind: "[REDACTED_SECRET]",
		},
		{
			name:     "Export Secret Unquoted",
			input:    "export SECRET_TOKEN=mySecretPassword123",
			expected: "export SECRET_TOKEN=[REDACTED_SECRET]",
			mustFind: "[REDACTED_SECRET]",
		},
		{
			name:     "Generic Token Single Quotes",
			input:    "api_key: 'abcdef1234567890'",
			expected: "api_key: '[REDACTED_SECRET]'",
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

func TestValidateWritePath(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("subdir allowed", func(t *testing.T) {
		got, err := ValidateWritePath(tmpDir, "sub/file.txt")
		if err != nil {
			t.Fatal(err)
		}
		exp := filepath.Join(tmpDir, "sub/file.txt")
		if got != exp {
			t.Errorf("expected %q, got %q", exp, got)
		}
	})

	t.Run("empty path allowed", func(t *testing.T) {
		got, err := ValidateWritePath(tmpDir, "")
		if err != nil {
			t.Fatal(err)
		}
		if got != tmpDir {
			t.Errorf("expected %q, got %q", tmpDir, got)
		}
	})

	t.Run("dot allowed", func(t *testing.T) {
		got, err := ValidateWritePath(tmpDir, ".")
		if err != nil {
			t.Fatal(err)
		}
		if got != tmpDir {
			t.Errorf("expected %q, got %q", tmpDir, got)
		}
	})

	t.Run("rejects parent traversal", func(t *testing.T) {
		_, err := ValidateWritePath(tmpDir, "../etc/passwd")
		if err == nil {
			t.Fatal("expected error for path traversal")
		}
	})

	t.Run("rejects deep parent traversal", func(t *testing.T) {
		_, err := ValidateWritePath(tmpDir, "a/../../etc/passwd")
		if err == nil {
			t.Fatal("expected error for deep traversal")
		}
	})

	t.Run("rejects absolute path outside project", func(t *testing.T) {
		// Use a path that is absolute on every OS (a Unix-style "/etc/passwd"
		// is not absolute on Windows and would resolve inside the project).
		_, err := ValidateWritePath(tmpDir, filepath.Join(os.TempDir(), "sv-memory-escape", "passwd"))
		if err == nil {
			t.Fatal("expected error for absolute path outside project")
		}
	})

	t.Run("rejects symlink escape", func(t *testing.T) {
		// Point the symlink at a REAL external directory containing the target
		// file, so EvalSymlinks resolves and the escape is detected on every OS.
		// A dangling target makes EvalSymlinks fail with IsNotExist and silently
		// skips the symlink check on Windows.
		outside := filepath.Join(os.TempDir(), "sv-memory-escape-target")
		if err := os.MkdirAll(outside, 0755); err != nil {
			t.Fatalf("mkdir outside: %v", err)
		}
		defer os.RemoveAll(outside)
		if err := os.WriteFile(filepath.Join(outside, "passwd"), []byte("x"), 0644); err != nil {
			t.Fatalf("write outside target: %v", err)
		}

		link := filepath.Join(tmpDir, "link")
		if err := os.Symlink(outside, link); err != nil {
			t.Skip("symlink not supported:", err)
		}
		_, err := ValidateWritePath(tmpDir, "link/passwd")
		if err == nil {
			t.Fatal("expected error for symlink escape")
		}
	})

	t.Run("rejects symlink escape for non-existent target", func(t *testing.T) {
		// A dangling final component (the target file does not exist yet, the
		// common write case) must still be caught when a parent directory is a
		// symlink pointing outside the project.
		outside := filepath.Join(os.TempDir(), "sv-memory-escape-new")
		if err := os.MkdirAll(outside, 0755); err != nil {
			t.Fatalf("mkdir outside: %v", err)
		}
		defer os.RemoveAll(outside)

		link := filepath.Join(tmpDir, "link2")
		if err := os.Symlink(outside, link); err != nil {
			t.Skip("symlink not supported:", err)
		}
		_, err := ValidateWritePath(tmpDir, "link2/newfile.txt")
		if err == nil {
			t.Fatal("expected error for symlink escape with non-existent target")
		}
	})
}

func TestSanitizeSQLitePathFilter(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"plain string", "src/main.go", "src/main.go"},
		{"escapes percent", "100%", "100\\%"},
		{"escapes underscore", "my_file", "my\\_file"},
		{"escapes backslash", "test\\path", "test\\\\path"},
		{"escapes all", "a_b%c\\d", "a\\_b\\%c\\\\d"},
		{"empty", "", ""},
		{"no special chars", "simple", "simple"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeSQLitePathFilter(tc.input)
			if got != tc.expected {
				t.Errorf("SanitizeSQLitePathFilter(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}
