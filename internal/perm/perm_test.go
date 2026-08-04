package perm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSettingsFile(t *testing.T, content string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "sv-perm-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write settings: %v", err)
	}
	return path
}

// withTempHome points HOME at a fresh temp dir for the duration of the test and
// returns the path, so resolveSettingsPath resolves platform configs under it.
func withTempHome(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "sv-perm-home-*")
	if err != nil {
		t.Fatalf("temp home: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	prevHome := os.Getenv("HOME")
	os.Setenv("HOME", dir)
	t.Cleanup(func() { os.Setenv("HOME", prevHome) })
	return dir
}

func TestGrantPreservesUnrelatedEntries(t *testing.T) {
	path := writeSettingsFile(t, `{
  "permissions": {
    "allow": ["command(npm run)", "mcp(sv-memory/sv_mem_search)"]
  },
  "model": "gemini"
}`)
	data, err := readSettings(path)
	if err != nil {
		t.Fatalf("readSettings: %v", err)
	}
	allow := getAllowList(data)
	want := []string{"command(npm run)", "mcp(sv-memory/sv_mem_search)"}
	if len(allow) != len(want) {
		t.Fatalf("expected %d entries, got %d: %v", len(want), len(allow), allow)
	}
	for i := range want {
		if allow[i] != want[i] {
			t.Errorf("entry %d: expected %q, got %q", i, want[i], allow[i])
		}
	}
}

func TestGrantDryRunDoesNotWrite(t *testing.T) {
	home := withTempHome(t)
	configPath := filepath.Join(home, ".gemini", "antigravity-cli", "settings.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	original := `{"permissions":{"allow":[]}}`
	if err := os.WriteFile(configPath, []byte(original), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	res, err := Grant(PlatformAntigravity, []string{"sv_mem_search"}, true)
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
	if !res.DryRun || len(res.Added) != 1 {
		t.Fatalf("expected 1 added in dry-run, got: %+v", res)
	}
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(content) != original {
		t.Errorf("dry-run should not write to disk, content changed: %s", content)
	}
}

func TestGrantMergeAllTools(t *testing.T) {
	dir, err := os.MkdirTemp("", "sv-perm-home-*")
	if err != nil {
		t.Fatalf("temp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	home := os.Getenv("HOME")
	os.Setenv("HOME", dir)
	defer os.Setenv("HOME", home)

	configPath := filepath.Join(dir, ".gemini", "antigravity-cli", "settings.json")
	if mkErr := os.MkdirAll(filepath.Dir(configPath), 0755); mkErr != nil {
		t.Fatalf("mkdir: %v", mkErr)
	}
	if writeErr := os.WriteFile(configPath, []byte(`{"permissions":{"allow":["command(npm run)"]}}`), 0644); writeErr != nil {
		t.Fatalf("write: %v", writeErr)
	}

	// Grant all tools.
	res, err := Grant(PlatformAntigravity, nil, false)
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
	if res.ConfigPath == "" {
		t.Fatal("expected a resolved config path")
	}
	if len(res.Added) == 0 {
		t.Fatal("expected tools to be added")
	}

	// Re-grant: everything should be present, nothing added.
	res2, err := Grant(PlatformAntigravity, nil, false)
	if err != nil {
		t.Fatalf("regrant: %v", err)
	}
	if len(res2.Added) != 0 || len(res2.Present) != 26 {
		t.Fatalf("expected 26 present / 0 added on re-grant, got: added=%d present=%d", len(res2.Added), len(res2.Present))
	}

	// Verify file contents: unrelated entry preserved + 26 sv-memory entries.
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var data map[string]interface{}
	if err := json.Unmarshal(content, &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	allow := getAllowList(data)
	found := 0
	hasNpm := false
	for _, entry := range allow {
		if strings.HasPrefix(entry, "mcp(sv-memory/") {
			found++
		}
		if entry == "command(npm run)" {
			hasNpm = true
		}
	}
	if found != 26 {
		t.Errorf("expected 26 sv-memory entries, got %d", found)
	}
	if !hasNpm {
		t.Error("unrelated entry 'command(npm run)' was not preserved")
	}
}

func TestRevokeRemovesOnlySVMemory(t *testing.T) {
	home := withTempHome(t)
	configPath := filepath.Join(home, ".gemini", "antigravity-cli", "settings.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(`{
  "permissions": {
    "allow": ["command(npm run)", "mcp(sv-memory/sv_mem_search)", "mcp__sv-memory__sv_mem_get"]
  }
}`), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	res, err := Revoke(PlatformAntigravity, false)
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if len(res.Removed) != 1 {
		t.Fatalf("expected 1 removed for agy, got: %+v", res)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var data map[string]interface{}
	if err := json.Unmarshal(content, &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	allow := getAllowList(data)
	if len(allow) != 2 {
		t.Fatalf("expected 2 entries after revoke, got %d: %v", len(allow), allow)
	}
	for _, entry := range allow {
		if strings.HasPrefix(entry, "mcp(sv-memory/") {
			t.Errorf("agy entry still present after revoke: %s", entry)
		}
	}
}

func TestClaudeCodeFormat(t *testing.T) {
	dir, err := os.MkdirTemp("", "sv-perm-claude-*")
	if err != nil {
		t.Fatalf("temp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	home := os.Getenv("HOME")
	os.Setenv("HOME", dir)
	defer os.Setenv("HOME", home)

	configPath := filepath.Join(dir, ".claude", "settings.json")
	if mkErr := os.MkdirAll(filepath.Dir(configPath), 0755); mkErr != nil {
		t.Fatalf("mkdir: %v", mkErr)
	}
	if writeErr := os.WriteFile(configPath, []byte(`{"permissions":{"allow":[]}}`), 0644); writeErr != nil {
		t.Fatalf("write: %v", writeErr)
	}

	res, err := Grant(PlatformClaudeCode, []string{"sv_mem_search", "sv_mem_save"}, false)
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
	if len(res.Added) != 2 {
		t.Fatalf("expected 2 added, got: %+v", res)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(content), "mcp__sv-memory__sv_mem_search") {
		t.Errorf("expected claude format mcp__sv-memory__sv_mem_search, got: %s", content)
	}
	if strings.Contains(string(content), "mcp(sv-memory/") {
		t.Errorf("claude must not contain agy format: %s", content)
	}
}

func TestNonAllowListedPlatformsSkip(t *testing.T) {
	res, err := Grant(PlatformOpenCode, nil, false)
	if err != nil {
		t.Fatalf("grant opencode: %v", err)
	}
	if !res.Skipped {
		t.Errorf("opencode grant should be skipped, got: %+v", res)
	}

	res2, err := Grant(PlatformCodex, nil, false)
	if err != nil {
		t.Fatalf("grant codex: %v", err)
	}
	if !res2.Skipped {
		t.Errorf("codex grant should be skipped, got: %+v", res2)
	}

	status, err := Status(PlatformOpenCode)
	if err != nil {
		t.Fatalf("status opencode: %v", err)
	}
	if status.AllowListed {
		t.Error("opencode should not be allow-listed")
	}
	if status.Message == "" {
		t.Error("expected an informational message for opencode")
	}
}

func TestStatusReportsGrantedAndMissing(t *testing.T) {
	home := withTempHome(t)
	configPath := filepath.Join(home, ".gemini", "antigravity-cli", "settings.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(`{"permissions":{"allow":["mcp(sv-memory/sv_mem_search)","mcp(sv-memory/sv_mem_get)"]}}`), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	status, err := Status(PlatformAntigravity)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !status.Configured {
		t.Error("expected Configured=true")
	}
	if len(status.Granted) != 2 {
		t.Errorf("expected 2 granted, got %d: %v", len(status.Granted), status.Granted)
	}
	if len(status.Missing) != 24 {
		t.Errorf("expected 24 missing, got %d", len(status.Missing))
	}
	if status.ConfigPath != configPath {
		t.Errorf("expected config path %s, got %s", configPath, status.ConfigPath)
	}
}

func TestGetSetAllowListRoundTrip(t *testing.T) {
	data := map[string]interface{}{}
	setAllowList(data, []string{"a", "b"})
	allow := getAllowList(data)
	if len(allow) != 2 || allow[0] != "a" || allow[1] != "b" {
		t.Fatalf("round-trip failed: %v", allow)
	}
}
