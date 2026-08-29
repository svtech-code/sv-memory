package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/svtech-code/sv-memory/internal/config"
	"github.com/svtech-code/sv-memory/internal/hook"
)

// TestSetupStatusNoArgs verifies that `sv-memory setup` without arguments runs
// the status table without error (read-only).
func TestSetupStatusNoArgs(t *testing.T) {
	tempDir := t.TempDir()

	oldCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	defer os.Chdir(oldCWD)
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("failed to chdir to temp dir: %v", err)
	}

	if err := setupStatus(); err != nil {
		t.Fatalf("setupStatus failed: %v", err)
	}
}

// TestSetupCursorWritesConfig verifies cursor setup writes the project-local
// .cursor/mcp.json with the sv-memory server entry.
func TestSetupCursorWritesConfig(t *testing.T) {
	tempDir := t.TempDir()
	execPath := "/usr/local/bin/sv-memory"

	path, err := config.ConfigureCursor(tempDir, execPath)
	if err != nil {
		t.Fatalf("ConfigureCursor failed: %v", err)
	}
	if path != filepath.Join(tempDir, ".cursor", "mcp.json") {
		t.Errorf("unexpected path %s", path)
	}
	content := readTestFile(t, path)
	if !strings.Contains(content, `"sv-memory"`) {
		t.Error("expected sv-memory server entry in cursor mcp.json")
	}
	if !statusCursor(tempDir) {
		t.Error("expected cursor status installed after config write")
	}
}

// TestSetupWindsurfWritesConfig verifies the windsurf project-local config.
func TestSetupWindsurfWritesConfig(t *testing.T) {
	tempDir := t.TempDir()
	execPath := "/usr/local/bin/sv-memory"

	path, err := config.ConfigureWindsurf(tempDir, execPath)
	if err != nil {
		t.Fatalf("ConfigureWindsurf failed: %v", err)
	}
	if path != filepath.Join(tempDir, ".windsurf", "mcp_config.json") {
		t.Errorf("unexpected path %s", path)
	}
	content := readTestFile(t, path)
	if !strings.Contains(content, `"sv-memory"`) {
		t.Error("expected sv-memory server entry in windsurf config")
	}
	if !statusWindsurf(tempDir) {
		t.Error("expected windsurf status installed after config write")
	}
}

// TestSetupClaudeCodeWritesHooksAndLifecycle verifies that claude-code setup
// writes the project .mcp.json, the PreToolUse + lifecycle hook scripts, and
// registers them in .claude/settings.json. A fake HOME keeps the real
// ~/.claude/settings.json untouched by permission granting.
func TestSetupClaudeCodeWritesHooksAndLifecycle(t *testing.T) {
	tempDir := t.TempDir()
	fakeHome := t.TempDir()

	oldHome := os.Getenv("HOME")
	oldCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	t.Cleanup(func() {
		os.Setenv("HOME", oldHome)
		os.Chdir(oldCWD)
	})
	os.Setenv("HOME", fakeHome)

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	if err := setupClaudeCode(tempDir, "/usr/local/bin/sv-memory", hook.ModeSoft); err != nil {
		t.Fatalf("setupClaudeCode failed: %v", err)
	}

	// Project .mcp.json written (claude CLI absent in CI).
	if !fileExists(filepath.Join(tempDir, ".mcp.json")) {
		t.Error("expected project .mcp.json to be written")
	}

	// Lifecycle hook scripts.
	for _, dir := range []string{"pre_tool_use", "session_start", "session_end", "precompact", "subagent_stop"} {
		p := filepath.Join(tempDir, ".claude", "hooks", dir, "sv-memory.sh")
		if !fileExists(p) {
			t.Errorf("expected hook script at %s", p)
		}
	}

	// settings.json must exist and register the lifecycle events.
	settings := readTestFile(t, filepath.Join(tempDir, ".claude", "settings.json"))
	for _, event := range []string{"PreToolUse", "SessionStart", "SessionEnd", "PreCompact", "SubagentStop"} {
		if !strings.Contains(settings, event) {
			t.Errorf("expected %s in settings.json", event)
		}
	}

	if !statusClaudeCode(tempDir) {
		t.Error("expected claude-code status installed")
	}
}

// TestSetupOpenCodeWritesPlugin verifies the opencode hook install writes the
// native TS plugin and the skill. MCP config (home-scoped) is not part of this
// unit test so the developer's real opencode.json is never touched.
func TestSetupOpenCodeWritesPlugin(t *testing.T) {
	tempDir := t.TempDir()

	eng := hook.New(tempDir, hook.ModeSoft)
	results := eng.Install([]hook.Platform{hook.PlatformOpenCode})
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("opencode install failed: %v", results)
	}

	if !fileExists(filepath.Join(tempDir, ".opencode", "plugin", "sv-memory.ts")) {
		t.Error("expected opencode plugin sv-memory.ts")
	}
	if !fileExists(filepath.Join(tempDir, ".opencode", "skills", "sv-memory", "SKILL.md")) {
		t.Error("expected opencode skill SKILL.md")
	}
	plugin := readTestFile(t, filepath.Join(tempDir, ".opencode", "plugin", "sv-memory.ts"))
	if !strings.Contains(plugin, "sv_memory_context") {
		t.Error("expected sv_memory_context tool in plugin")
	}
}

// TestSetupAntigravityWritesHooksAndSkill verifies that antigravity setup writes
// the PreToolUse hook, hooks.json, and the native skill in .agents/skills/.
func TestSetupAntigravityWritesHooksAndSkill(t *testing.T) {
	tempDir := t.TempDir()

	eng := hook.New(tempDir, hook.ModeSoft)
	results := eng.Install([]hook.Platform{hook.PlatformAntigravity})
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("antigravity install failed: %v", results)
	}

	if !fileExists(filepath.Join(tempDir, ".agents", "hooks.json")) {
		t.Error("expected .agents/hooks.json")
	}
	if !fileExists(filepath.Join(tempDir, ".agents", "hooks", "sv-memory.sh")) {
		t.Error("expected .agents/hooks/sv-memory.sh")
	}
	skillPath := filepath.Join(tempDir, ".agents", "skills", "sv-memory", "SKILL.md")
	if !fileExists(skillPath) {
		t.Error("expected .agents/skills/sv-memory/SKILL.md")
	}
	skillContent := readTestFile(t, skillPath)
	if !strings.Contains(skillContent, "name: sv-memory") {
		t.Error("expected frontmatter in antigravity skill")
	}
	if !statusAntigravity(tempDir) {
		t.Error("expected antigravity status to be installed")
	}
}

// TestAutoWireProjectAgentsFreshAndExisting verifies autoWireProjectAgents behavior
// for both fresh projects (wires all) and existing projects (wires installed).
func TestAutoWireProjectAgentsFreshAndExisting(t *testing.T) {
	tempDir := t.TempDir()

	oldCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	defer os.Chdir(oldCWD)
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	// 1. Fresh project: wire explicit agent first
	if err := autoWireProjectAgents(tempDir, false, "antigravity"); err != nil {
		t.Fatalf("autoWireProjectAgents explicit failed: %v", err)
	}
	if !statusAntigravity(tempDir) {
		t.Error("expected antigravity to be installed")
	}

	// 2. Existing project with only Antigravity installed: auto-wire without arg reconciles Antigravity
	if err := autoWireProjectAgents(tempDir, false, ""); err != nil {
		t.Fatalf("autoWireProjectAgents reconcile failed: %v", err)
	}
	if !statusAntigravity(tempDir) {
		t.Error("expected antigravity to still be installed")
	}
}

// TestConfigureTargetAgents verifies configureTargetAgents sets up only the targeted agents.
func TestConfigureTargetAgents(t *testing.T) {
	tempDir := t.TempDir()

	oldCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	defer os.Chdir(oldCWD)
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	// Target only cursor
	if err := configureTargetAgents(tempDir, false, []string{"cursor"}); err != nil {
		t.Fatalf("configureTargetAgents failed: %v", err)
	}
	if !statusCursor(tempDir) {
		t.Error("expected cursor to be installed")
	}
	if statusClaudeCode(tempDir) || statusWindsurf(tempDir) || statusAntigravity(tempDir) || statusCodex(tempDir) {
		t.Error("expected other agents not to be installed")
	}
}

// TestInstalledAgentsDetection verifies installedAgents accurately identifies configured agents.
func TestInstalledAgentsDetection(t *testing.T) {
	tempDir := t.TempDir()

	installed := installedAgents(tempDir)
	if len(installed) != 0 {
		t.Errorf("expected 0 installed agents in fresh dir, got %v", installed)
	}

	// Install windsurf
	_, err := config.ConfigureWindsurf(tempDir, "/usr/local/bin/sv-memory")
	if err != nil {
		t.Fatalf("ConfigureWindsurf failed: %v", err)
	}

	installed = installedAgents(tempDir)
	if len(installed) != 1 || installed[0] != "windsurf" {
		t.Errorf("expected [windsurf], got %v", installed)
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func readTestFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("failed to read %s: %v", p, err)
	}
	return string(b)
}
