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

func TestHookScriptContentCodex(t *testing.T) {
	content := mustHookScript(t, PlatformCodex, ModeSoft)
	if !strings.Contains(content, "no-op") {
		t.Error("codex script should be a no-op")
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

	// All four lifecycle scripts must exist.
	lifecycleDirs := []string{"session_start", "session_end", "precompact", "subagent_stop"}
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
	for _, event := range []string{"PreToolUse", "SessionStart", "SessionEnd", "PreCompact", "SubagentStop"} {
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

func TestInstallCodex(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-hook-codex-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	eng := New(tempDir, ModeSoft)
	results := eng.Install([]Platform{PlatformCodex})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err != nil {
		t.Fatalf("install failed: %v", results[0].Err)
	}

	// Verify hooks.json exists
	hooksPath := filepath.Join(tempDir, ".codex", "hooks.json")
	if _, err := os.Stat(hooksPath); os.IsNotExist(err) {
		t.Fatalf("hooks.json not created at %s", hooksPath)
	}

	// Verify noop script exists
	scriptPath := filepath.Join(tempDir, ".codex", "hooks", "sv-memory.sh")
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		t.Fatalf("codex hook script not created at %s", scriptPath)
	}

	status := eng.Status([]Platform{PlatformCodex})
	if !status[PlatformCodex] {
		t.Error("expected codex status to be installed")
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

func TestUninstallCodex(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-hook-codex-uninstall")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	eng := New(tempDir, ModeSoft)

	installResults := eng.Install([]Platform{PlatformCodex})
	if installResults[0].Err != nil {
		t.Fatalf("install failed: %v", installResults[0].Err)
	}

	uninstallResults := eng.Uninstall([]Platform{PlatformCodex})
	if uninstallResults[0].Err != nil {
		t.Fatalf("uninstall failed: %v", uninstallResults[0].Err)
	}

	hooksPath := filepath.Join(tempDir, ".codex", "hooks.json")
	if _, err := os.Stat(hooksPath); !os.IsNotExist(err) {
		t.Error("hooks.json should have been removed")
	}
}

func TestHookScriptContentAntigravity(t *testing.T) {
	content := mustHookScript(t, PlatformAntigravity, ModeSoft)
	if !strings.Contains(content, "allow") {
		t.Error("antigravity soft script should contain 'allow' decision")
	}
}

func TestHookScriptContentAntigravityStrict(t *testing.T) {
	content := mustHookScript(t, PlatformAntigravity, ModeStrict)
	if !strings.Contains(content, "FLAG_FILE") {
		t.Error("antigravity strict script should have FLAG_FILE session tracking")
	}
	if !strings.Contains(content, "exit 2") {
		t.Error("antigravity strict script should have exit 2 for blocked tools")
	}
	if !strings.Contains(content, "SV_MEMORY_STRICT_DISABLE") {
		t.Error("antigravity strict script should support the SV_MEMORY_STRICT_DISABLE opt-out")
	}
	if !strings.Contains(content, ".sv-memory") {
		t.Error("antigravity strict script should fail open when .sv-memory is absent")
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

func TestInstallAntigravity(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-hook-agy-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	eng := New(tempDir, ModeSoft)
	results := eng.Install([]Platform{PlatformAntigravity})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err != nil {
		t.Fatalf("install failed: %v", results[0].Err)
	}

	hooksPath := filepath.Join(tempDir, ".agents", "hooks.json")
	if _, err := os.Stat(hooksPath); os.IsNotExist(err) {
		t.Fatalf("hooks.json not created at %s", hooksPath)
	}

	scriptPath := filepath.Join(tempDir, ".agents", "hooks", "sv-memory.sh")
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		t.Fatalf("hook script not created at %s", scriptPath)
	}

	skillPath := filepath.Join(tempDir, ".agents", "skills", "sv-memory", "SKILL.md")
	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		t.Fatalf("skill file not created at %s", skillPath)
	}

	status := eng.Status([]Platform{PlatformAntigravity})
	if !status[PlatformAntigravity] {
		t.Error("expected antigravity status to be installed")
	}
}

func TestUninstallAntigravity(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-hook-agy-uninstall")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	eng := New(tempDir, ModeSoft)

	installResults := eng.Install([]Platform{PlatformAntigravity})
	if installResults[0].Err != nil {
		t.Fatalf("install failed: %v", installResults[0].Err)
	}

	uninstallResults := eng.Uninstall([]Platform{PlatformAntigravity})
	if uninstallResults[0].Err != nil {
		t.Fatalf("uninstall failed: %v", uninstallResults[0].Err)
	}

	hooksPath := filepath.Join(tempDir, ".agents", "hooks.json")
	if _, err := os.Stat(hooksPath); !os.IsNotExist(err) {
		t.Error("hooks.json should have been removed")
	}

	scriptPath := filepath.Join(tempDir, ".agents", "hooks", "sv-memory.sh")
	if _, err := os.Stat(scriptPath); !os.IsNotExist(err) {
		t.Error("hook script should have been removed")
	}

	skillPath := filepath.Join(tempDir, ".agents", "skills", "sv-memory", "SKILL.md")
	if _, err := os.Stat(skillPath); !os.IsNotExist(err) {
		t.Error("skill file should have been removed")
	}
}

func TestAntigravitySkillContent(t *testing.T) {
	content := antigravitySkillScript()
	if !strings.Contains(content, "name: sv-memory") {
		t.Error("antigravity skill should have YAML frontmatter name")
	}
	if !strings.Contains(content, "description:") {
		t.Error("antigravity skill should have YAML frontmatter description")
	}
	if !strings.Contains(content, "sv_mem_context_pack") {
		t.Error("antigravity skill should emphasize sv_mem_context_pack")
	}
}

func TestHookScriptContentOpenCode(t *testing.T) {
	content := mustHookScript(t, PlatformOpenCode, ModeSoft)
	if !strings.Contains(content, "sv_mem_search") {
		t.Error("opencode skill should contain sv_mem_search instructions")
	}
}

func TestHookScriptContentOpenCodeStrict(t *testing.T) {
	content := mustHookScript(t, PlatformOpenCode, ModeStrict)
	if !strings.Contains(content, "sv_mem_search") {
		t.Error("opencode strict skill should contain sv_mem_search instructions")
	}
}

func TestOpenCodePluginStrictHasToolExecuteBefore(t *testing.T) {
	data, err := hookScriptsFS.ReadFile("scripts/opencode-plugin-strict.ts")
	if err != nil {
		t.Fatalf("failed to read strict plugin: %v", err)
	}
	plugin := string(data)
	if !strings.Contains(plugin, "tool.execute.before") {
		t.Error("strict plugin should contain tool.execute.before hook")
	}
	if !strings.Contains(plugin, "SV_MEMORY_STRICT_DISABLE") {
		t.Error("strict plugin should respect SV_MEMORY_STRICT_DISABLE opt-out")
	}
	if !strings.Contains(plugin, "sv_memory_context") {
		t.Error("strict plugin should still register sv_memory_context tool")
	}
}

func TestInstallOpenCodeStrictPlugin(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-hook-oc-strict")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	eng := New(tempDir, ModeStrict)
	results := eng.Install([]Platform{PlatformOpenCode})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err != nil {
		t.Fatalf("install failed: %v", results[0].Err)
	}

	pluginPath := filepath.Join(tempDir, ".opencode", "plugin", "sv-memory.ts")
	data, err := os.ReadFile(pluginPath)
	if err != nil {
		t.Fatalf("failed to read installed plugin: %v", err)
	}
	plugin := string(data)
	if !strings.Contains(plugin, "tool.execute.before") {
		t.Error("installed strict plugin should contain tool.execute.before")
	}
	if !strings.Contains(plugin, "redirected") {
		t.Error("installed strict plugin should track redirected sessions")
	}
}

func TestInstallOpenCodeSoftPluginNoRedirect(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-hook-oc-soft")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	eng := New(tempDir, ModeSoft)
	results := eng.Install([]Platform{PlatformOpenCode})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err != nil {
		t.Fatalf("install failed: %v", results[0].Err)
	}

	pluginPath := filepath.Join(tempDir, ".opencode", "plugin", "sv-memory.ts")
	data, err := os.ReadFile(pluginPath)
	if err != nil {
		t.Fatalf("failed to read installed plugin: %v", err)
	}
	plugin := string(data)
	if strings.Contains(plugin, "tool.execute.before") {
		t.Error("soft plugin should NOT contain tool.execute.before")
	}
}

func TestInstallOpenCodeSkill(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-hook-oc-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	eng := New(tempDir, ModeSoft)
	results := eng.Install([]Platform{PlatformOpenCode})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err != nil {
		t.Fatalf("install failed: %v", results[0].Err)
	}

	skillPath := filepath.Join(tempDir, ".opencode", "skills", "sv-memory", "SKILL.md")
	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		t.Fatalf("SKILL.md not created at %s", skillPath)
	}

	status := eng.Status([]Platform{PlatformOpenCode})
	if !status[PlatformOpenCode] {
		t.Error("expected opencode status to be installed")
	}
}

func TestUninstallOpenCodeSkill(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-hook-oc-uninstall")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	eng := New(tempDir, ModeSoft)

	installResults := eng.Install([]Platform{PlatformOpenCode})
	if installResults[0].Err != nil {
		t.Fatalf("install failed: %v", installResults[0].Err)
	}

	uninstallResults := eng.Uninstall([]Platform{PlatformOpenCode})
	if uninstallResults[0].Err != nil {
		t.Fatalf("uninstall failed: %v", uninstallResults[0].Err)
	}

	skillDir := filepath.Join(tempDir, ".opencode", "skills", "sv-memory")
	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Error("skill dir should have been removed")
	}
}

func TestStatusNotInstalled(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-hook-status-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	eng := New(tempDir, ModeSoft)
	status := eng.Status(nil)

	if status[PlatformClaudeCode] {
		t.Error("expected claude-code to not be installed in empty dir")
	}
	if status[PlatformCodex] {
		t.Error("expected codex to not be installed in empty dir")
	}
	if status[PlatformAntigravity] {
		t.Error("expected antigravity to not be installed in empty dir")
	}
	if status[PlatformCursor] {
		t.Error("expected cursor to not be installed in empty dir")
	}
	if status[PlatformWindsurf] {
		t.Error("expected windsurf to not be installed in empty dir")
	}
	if status[PlatformOpenCode] {
		t.Error("expected opencode to not be installed in empty dir")
	}
	if status[PlatformGit] {
		t.Error("expected git to not be installed in empty dir")
	}
}

func TestInstallCursor(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-hook-cursor-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	eng := New(tempDir, ModeSoft)
	results := eng.Install([]Platform{PlatformCursor})
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("install cursor failed: %v", results)
	}

	if !eng.Status([]Platform{PlatformCursor})[PlatformCursor] {
		t.Error("expected cursor status to be installed")
	}

	uninst := eng.Uninstall([]Platform{PlatformCursor})
	if len(uninst) != 1 || uninst[0].Err != nil {
		t.Fatalf("uninstall cursor failed: %v", uninst)
	}
	if eng.Status([]Platform{PlatformCursor})[PlatformCursor] {
		t.Error("expected cursor status to not be installed after uninstall")
	}
}

func TestInstallWindsurf(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-hook-windsurf-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	eng := New(tempDir, ModeSoft)
	results := eng.Install([]Platform{PlatformWindsurf})
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("install windsurf failed: %v", results)
	}

	if !eng.Status([]Platform{PlatformWindsurf})[PlatformWindsurf] {
		t.Error("expected windsurf status to be installed")
	}

	uninst := eng.Uninstall([]Platform{PlatformWindsurf})
	if len(uninst) != 1 || uninst[0].Err != nil {
		t.Fatalf("uninstall windsurf failed: %v", uninst)
	}
	if eng.Status([]Platform{PlatformWindsurf})[PlatformWindsurf] {
		t.Error("expected windsurf status to not be installed after uninstall")
	}
}

func TestInstallGitHook(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-hook-git-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	eng := New(tempDir, ModeSoft)

	// Install
	results := eng.Install([]Platform{PlatformGit})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err != nil {
		t.Fatalf("install git hook failed: %v", results[0].Err)
	}

	hookPath := filepath.Join(tempDir, ".git", "hooks", "post-commit")
	content, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("failed to read installed git hook: %v", err)
	}
	if !strings.Contains(string(content), "sv-memory") {
		t.Errorf("expected git hook to contain 'sv-memory', got:\n%s", string(content))
	}

	// Status
	status := eng.Status([]Platform{PlatformGit})
	if !status[PlatformGit] {
		t.Error("expected git status to be true")
	}

	// Uninstall
	uninstResults := eng.Uninstall([]Platform{PlatformGit})
	if len(uninstResults) != 1 || uninstResults[0].Err != nil {
		t.Fatalf("uninstall git hook failed: %v", uninstResults)
	}
	if _, err = os.Stat(hookPath); !os.IsNotExist(err) {
		t.Errorf("expected post-commit hook to be removed")
	}
}

func TestInstallAllPlatforms(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-hook-all-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	eng := New(tempDir, ModeSoft)
	results := eng.Install(nil)

	if len(results) != len(supportedPlatforms) {
		t.Errorf("expected %d results, got %d", len(supportedPlatforms), len(results))
	}

	for _, r := range results {
		if r.Err != nil {
			t.Errorf("install %s failed: %v", r.Platform, r.Err)
		}
	}
}

func TestGitHookWorktree(t *testing.T) {
	tempDir := t.TempDir()
	actualGitDir := t.TempDir()

	// Write .git as a file pointing to actualGitDir (worktree / submodule style)
	gitFilePath := filepath.Join(tempDir, ".git")
	err := os.WriteFile(gitFilePath, []byte("gitdir: "+actualGitDir+"\n"), 0644)
	if err != nil {
		t.Fatalf("failed to write .git file: %v", err)
	}

	eng := New(tempDir, ModeSoft)
	results := eng.Install([]Platform{PlatformGit})
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("install git hook failed: %v", results)
	}

	hookPath := filepath.Join(actualGitDir, "hooks", "post-commit")
	if _, err := os.Stat(hookPath); err != nil {
		t.Fatalf("expected git hook in worktree at %s: %v", hookPath, err)
	}
	if !eng.Status([]Platform{PlatformGit})[PlatformGit] {
		t.Error("expected git status true for worktree")
	}
}
