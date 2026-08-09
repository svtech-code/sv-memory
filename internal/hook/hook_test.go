package hook

import (
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
}

func TestHookScriptContentOpenCode(t *testing.T) {
	content := mustHookScript(t, PlatformOpenCode, ModeSoft)
	if !strings.Contains(content, "sv_mem_search") {
		t.Error("opencode skill should contain sv_mem_search instructions")
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
	if status[PlatformOpenCode] {
		t.Error("expected opencode to not be installed in empty dir")
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
