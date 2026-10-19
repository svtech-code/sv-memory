package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mustHookScript loads a hook template and fails the test if none exists.
// The exported HookScriptContent wrapper was removed (production uses the
// unexported hookScript directly), so tests call the unexported one.
func mustHookScript(t *testing.T, p Platform, m Mode) string {
	t.Helper()
	content := hookScript(p, m)
	if content == "" {
		t.Fatalf("no hook template for platform %s mode %s", p, m)
	}
	return content
}

func TestHookScriptContent(t *testing.T) {
	content := mustHookScript(t, PlatformClaudeCode, ModeSoft)
	if !strings.Contains(content, "sv-memory") {
		t.Error("claude-code soft script should contain 'sv-memory'")
	}
	if !strings.Contains(content, "exit 0") {
		t.Error("claude-code soft script should have exit 0")
	}
}

func TestHookScriptContentStrict(t *testing.T) {
	content := mustHookScript(t, PlatformClaudeCode, ModeStrict)
	if !strings.Contains(content, "strict") {
		t.Error("claude-code strict script should contain 'strict'")
	}
	if !strings.Contains(content, "FLAG_FILE") {
		t.Error("claude-code strict script should have FLAG_FILE session tracking")
	}
}

func TestClaudeScriptsContainContextInjectionBlock(t *testing.T) {
	for _, mode := range []Mode{ModeSoft, ModeStrict} {
		content := mustHookScript(t, PlatformClaudeCode, mode)
		if !strings.Contains(content, "context-injection-enabled") {
			t.Errorf("claude-code %s script should contain the context-injection block", mode)
		}
		if !strings.Contains(content, "run_with_timeout") {
			t.Errorf("claude-code %s script should have the portable timeout helper", mode)
		}
		if !strings.Contains(content, "--max-memories") {
			t.Errorf("claude-code %s script should call sv-memory context with a bounded --max-memories", mode)
		}
	}
}

func TestContextInjectionMarker(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-hook-marker")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	eng := New(tempDir, ModeSoft)
	if eng.ContextInjectionEnabled() {
		t.Fatal("expected context injection disabled by default")
	}

	if err = eng.SetContextInjection(true); err != nil {
		t.Fatalf("failed enabling context injection: %v", err)
	}
	if !eng.ContextInjectionEnabled() {
		t.Fatal("expected context injection enabled after SetContextInjection(true)")
	}
	marker := filepath.Join(tempDir, ".sv-memory", "context-injection-enabled")
	if _, err = os.Stat(marker); err != nil {
		t.Fatalf("expected marker file at %s: %v", marker, err)
	}

	if err = eng.SetContextInjection(false); err != nil {
		t.Fatalf("failed disabling context injection: %v", err)
	}
	if eng.ContextInjectionEnabled() {
		t.Fatal("expected context injection disabled after SetContextInjection(false)")
	}
}

func TestInstallClaudeCodeSoft(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-hook-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	eng := New(tempDir, ModeSoft)
	results := eng.Install([]Platform{PlatformClaudeCode})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err != nil {
		t.Fatalf("install failed: %v", results[0].Err)
	}
	if len(results[0].Files) == 0 {
		t.Fatal("expected at least 1 file to be created")
	}

	// Verify script exists and is executable
	scriptPath := filepath.Join(tempDir, ".claude", "hooks", "pre_tool_use", "sv-memory.sh")
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		t.Fatalf("hook script not created at %s", scriptPath)
	}

	// Verify settings file was created/updated
	settingsPath := filepath.Join(tempDir, ".claude", "settings.json")
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		t.Fatalf("settings not created at %s", settingsPath)
	}

	// Verify status says installed
	status := eng.Status([]Platform{PlatformClaudeCode})
	if !status[PlatformClaudeCode] {
		t.Error("expected claude-code status to be installed")
	}
}

func TestInstallClaudeCodeStrict(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-hook-strict-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	eng := New(tempDir, ModeStrict)
	results := eng.Install([]Platform{PlatformClaudeCode})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err != nil {
		t.Fatalf("install failed: %v", results[0].Err)
	}

	// Verify strict script content
	scriptPath := filepath.Join(tempDir, ".claude", "hooks", "pre_tool_use", "sv-memory.sh")
	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("failed to read script: %v", err)
	}
	if !strings.Contains(string(content), "FLAG_FILE") {
		t.Error("strict mode script should contain FLAG_FILE")
	}
}

func TestInstallClaudeCodeLifecycleHooks(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-hook-lifecycle-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	eng := New(tempDir, ModeSoft)
	results := eng.Install([]Platform{PlatformClaudeCode})
	if results[0].Err != nil {
		t.Fatalf("install failed: %v", results[0].Err)
	}

	// All lifecycle scripts must exist.
	lifecycleDirs := []string{"session_start", "session_end", "precompact", "subagent_stop", "user_prompt_submit"}
	for _, dir := range lifecycleDirs {
		scriptPath := filepath.Join(tempDir, ".claude", "hooks", dir, "sv-memory.sh")
		if _, err = os.Stat(scriptPath); os.IsNotExist(err) {
			t.Errorf("lifecycle script not created at %s", scriptPath)
		}
	}

	// settings.json must register every event in the array format.
	settingsPath := filepath.Join(tempDir, ".claude", "settings.json")
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("failed to read settings: %v", err)
	}
	var settings map[string]interface{}
	if err = json.Unmarshal(raw, &settings); err != nil {
		t.Fatalf("failed to parse settings: %v", err)
	}
	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		t.Fatal("expected hooks object in settings")
	}
	for _, event := range []string{"PreToolUse", "UserPromptSubmit", "SessionStart", "SessionEnd", "PreCompact", "SubagentStop"} {
		entries, ok := hooks[event].([]interface{})
		if !ok || len(entries) == 0 {
			t.Errorf("expected %s hook entries in settings.json", event)
			continue
		}
		first, ok := entries[0].(map[string]interface{})
		if !ok {
			t.Errorf("expected object entry for %s", event)
			continue
		}
		hooksList, ok := first["hooks"].([]interface{})
		if !ok || len(hooksList) == 0 {
			t.Errorf("expected hooks list for %s", event)
			continue
		}
		cmdEntry, ok := hooksList[0].(map[string]interface{})
		if !ok {
			t.Errorf("expected command entry for %s", event)
			continue
		}
		if filepath.Base(cmdEntry["command"].(string)) != "sv-memory.sh" {
			t.Errorf("%s command should point to sv-memory.sh", event)
		}
	}

	// Status must report installed with the lifecycle hooks present.
	status := eng.Status([]Platform{PlatformClaudeCode})
	if !status[PlatformClaudeCode] {
		t.Error("expected claude-code status to be installed")
	}
}

func TestClaudeCodeHooksPreserveUserHooks(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-hook-merge-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Seed settings.json with a hook belonging to another tool.
	settingsPath := filepath.Join(tempDir, ".claude", "settings.json")
	if err = os.MkdirAll(filepath.Dir(settingsPath), 0755); err != nil {
		t.Fatalf("failed to create settings dir: %v", err)
	}
	initial := map[string]interface{}{
		"hooks": map[string]interface{}{
			"PreToolUse": []interface{}{
				map[string]interface{}{
					"matcher": "Read|Write",
					"hooks": []interface{}{
						map[string]interface{}{"type": "command", "command": "/usr/local/bin/other-tool", "timeout": 3},
					},
				},
			},
		},
	}
	data, _ := json.Marshal(initial)
	if err = os.WriteFile(settingsPath, data, 0644); err != nil {
		t.Fatalf("failed to write settings: %v", err)
	}

	eng := New(tempDir, ModeSoft)
	if results := eng.Install([]Platform{PlatformClaudeCode}); results[0].Err != nil {
		t.Fatalf("install failed: %v", results[0].Err)
	}

	findCmds := func() (foundUser, foundSVM bool) {
		raw, rErr := os.ReadFile(settingsPath)
		if rErr != nil {
			t.Fatalf("failed to read settings: %v", rErr)
		}
		var settings map[string]interface{}
		if jErr := json.Unmarshal(raw, &settings); jErr != nil {
			t.Fatalf("failed to parse settings: %v", jErr)
		}
		hooks, ok := settings["hooks"].(map[string]interface{})
		if !ok {
			t.Fatal("expected hooks object in settings")
		}
		pre, ok := hooks["PreToolUse"].([]interface{})
		if !ok || len(pre) == 0 {
			t.Fatal("expected PreToolUse entries")
		}
		for _, g := range pre {
			group, ok := g.(map[string]interface{})
			if !ok {
				continue
			}
			entries, _ := group["hooks"].([]interface{})
			for _, e := range entries {
				entry, ok := e.(map[string]interface{})
				if !ok {
					continue
				}
				cmd, _ := entry["command"].(string)
				if strings.Contains(cmd, "other-tool") {
					foundUser = true
				}
				if strings.Contains(cmd, "sv-memory.sh") {
					foundSVM = true
				}
			}
		}
		return foundUser, foundSVM
	}

	foundUser, foundSVM := findCmds()
	if !foundUser {
		t.Error("user hook was lost after install")
	}
	if !foundSVM {
		t.Error("sv-memory hook not registered after install")
	}
	if st := eng.Status([]Platform{PlatformClaudeCode}); !st[PlatformClaudeCode] {
		t.Error("expected claude-code status to be installed")
	}

	// Uninstall must remove only the sv-memory entries, keeping the user hook.
	if results := eng.Uninstall([]Platform{PlatformClaudeCode}); results[0].Err != nil {
		t.Fatalf("uninstall failed: %v", results[0].Err)
	}
	foundUser, foundSVM = findCmds()
	if foundSVM {
		t.Error("sv-memory hook still present after uninstall")
	}
	if !foundUser {
		t.Error("user hook was removed by uninstall")
	}
}

func TestUninstallClaudeCode(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-hook-uninstall-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	eng := New(tempDir, ModeSoft)

	// Install first
	installResults := eng.Install([]Platform{PlatformClaudeCode})
	if installResults[0].Err != nil {
		t.Fatalf("install failed: %v", installResults[0].Err)
	}

	// Then uninstall
	uninstallResults := eng.Uninstall([]Platform{PlatformClaudeCode})
	if uninstallResults[0].Err != nil {
		t.Fatalf("uninstall failed: %v", uninstallResults[0].Err)
	}

	// Verify script is gone
	scriptPath := filepath.Join(tempDir, ".claude", "hooks", "pre_tool_use", "sv-memory.sh")
	if _, err := os.Stat(scriptPath); !os.IsNotExist(err) {
		t.Error("hook script should have been removed")
	}

	// Verify status says not installed
	status := eng.Status([]Platform{PlatformClaudeCode})
	if status[PlatformClaudeCode] {
		t.Error("expected claude-code status to be not installed after uninstall")
	}
}

func TestHookScriptContentClaudeCodeStrictIsNudgeOnly(t *testing.T) {
	content := mustHookScript(t, PlatformClaudeCode, ModeStrict)
	if strings.Contains(content, "exit 2") {
		t.Error("claude-code strict script must never block (nudge-only)")
	}
}

func TestClaudeCodeStrictHasWriteNudge(t *testing.T) {
	content := mustHookScript(t, PlatformClaudeCode, ModeStrict)
	if !strings.Contains(content, "Write|Edit") {
		t.Error("claude-code strict script should match Write|Edit tools")
	}
	if !strings.Contains(content, "sv_propose_spec") {
		t.Error("claude-code strict script should nudge toward sv_propose_spec on first write")
	}
	if !strings.Contains(content, "WRITE_FLAG") {
		t.Error("claude-code strict script should track write nudge per session")
	}
}

// TestClaudeCodeSessionStartClearsFlags verifies that the SessionStart hook
// clears per-session strict/read and write-nudge temp flags so the graph-first
// redirect and write nudge fire again in the new session (they previously
// persisted until reboot, causing the redirect to only fire once per machine).
func TestClaudeCodeSessionStartClearsFlags(t *testing.T) {
	data, err := hookScriptsFS.ReadFile("scripts/claude-code-session-start.sh")
	if err != nil {
		t.Fatalf("failed to read session-start script: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "sv_mem_hash") {
		t.Error("session-start script should define sv_mem_hash for flag path computation")
	}
	if !strings.Contains(content, ".sv-memory-strict-") {
		t.Error("session-start script should clear strict/read flag files")
	}
	if !strings.Contains(content, ".sv-memory-write-") {
		t.Error("session-start script should clear write-nudge flag files")
	}
	if !strings.Contains(content, "rm -f") {
		t.Error("session-start script should use rm -f to clear flag files")
	}
}
