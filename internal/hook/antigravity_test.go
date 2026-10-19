package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHookScriptContentAntigravity(t *testing.T) {
	content := mustHookScript(t, PlatformAntigravity, ModeSoft)
	if !strings.Contains(content, "allow") {
		t.Error("antigravity soft script should contain 'allow' decision")
	}
}

func TestHookScriptContentAntigravityStrict(t *testing.T) {
	content := mustHookScript(t, PlatformAntigravity, ModeStrict)
	if !strings.Contains(content, "FLAG_FILE") {
		t.Error("antigravity strict script should have FLAG_FILE per-conversation tracking")
	}
	if !strings.Contains(content, `"decision":"deny"`) {
		t.Error("antigravity strict script should emit a JSON deny decision")
	}
	if !strings.Contains(content, `"decision":"allow"`) {
		t.Error("antigravity strict script should emit a JSON allow decision")
	}
	if strings.Contains(content, "exit 2") {
		t.Error("antigravity strict script must not use the Claude Code exit 2 contract")
	}
	if !strings.Contains(content, "SV_MEMORY_STRICT_DISABLE") {
		t.Error("antigravity strict script should support the SV_MEMORY_STRICT_DISABLE opt-out")
	}
	if !strings.Contains(content, ".sv-memory") {
		t.Error("antigravity strict script should fail open when .sv-memory is absent")
	}
	if !strings.Contains(content, "workspacePaths") {
		t.Error("antigravity strict script should resolve the workspace from workspacePaths")
	}
	if !strings.Contains(content, "conversationId") {
		t.Error("antigravity strict script should scope the redirect per conversation")
	}
}

func TestAntigravityPreInvocationScriptContent(t *testing.T) {
	content := antigravityPreInvocationScript()
	if content == "" {
		t.Fatal("antigravity preinvocation script template is missing")
	}
	if !strings.Contains(content, "injectSteps") || !strings.Contains(content, "ephemeralMessage") {
		t.Error("preinvocation script should emit injectSteps with an ephemeralMessage")
	}
	if !strings.Contains(content, "workspacePaths") {
		t.Error("preinvocation script should read workspacePaths from the payload")
	}
	if !strings.Contains(content, "invocationNum") {
		t.Error("preinvocation script should read invocationNum from the payload")
	}
	if !strings.Contains(content, "session start") {
		t.Error("preinvocation script should auto-start the session on first invocation")
	}
	if !strings.Contains(content, "sv_mem_session_start") {
		t.Error("preinvocation script should tell the model the session is auto-managed")
	}
	if !strings.Contains(content, "specs list") {
		t.Error("preinvocation script should check for active spec changes on later invocations")
	}
	if !strings.Contains(content, "echo '{}'") {
		t.Error("preinvocation script should fail open with an empty JSON object")
	}
	if strings.Contains(content, "exit 2") {
		t.Error("preinvocation script must never block (no exit 2)")
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
	if _, statErr := os.Stat(hooksPath); os.IsNotExist(statErr) {
		t.Fatalf("hooks.json not created at %s", hooksPath)
	}

	scriptPath := filepath.Join(tempDir, ".agents", "hooks", "sv-memory.sh")
	if _, statErr := os.Stat(scriptPath); os.IsNotExist(statErr) {
		t.Fatalf("hook script not created at %s", scriptPath)
	}

	prePath := filepath.Join(tempDir, ".agents", "hooks", "sv-memory-preinvocation.sh")
	if _, statErr := os.Stat(prePath); os.IsNotExist(statErr) {
		t.Fatalf("preinvocation script not created at %s", prePath)
	}

	rawHooks, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatalf("failed to read hooks.json: %v", err)
	}
	if !strings.Contains(string(rawHooks), "PreInvocation") {
		t.Error("hooks.json should register a PreInvocation entry")
	}
	if !strings.Contains(string(rawHooks), "sv-memory-preinvocation.sh") {
		t.Error("hooks.json PreInvocation should point at the preinvocation script")
	}

	skillPath := filepath.Join(tempDir, ".agents", "skills", "sv-memory", "SKILL.md")
	if _, statErr := os.Stat(skillPath); os.IsNotExist(statErr) {
		t.Fatalf("skill file not created at %s", skillPath)
	}

	rulePath := filepath.Join(tempDir, ".agents", "rules", "sv-memory.md")
	if _, statErr := os.Stat(rulePath); os.IsNotExist(statErr) {
		t.Fatalf("always-on rule not created at %s", rulePath)
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

	prePath := filepath.Join(tempDir, ".agents", "hooks", "sv-memory-preinvocation.sh")
	if _, err := os.Stat(prePath); !os.IsNotExist(err) {
		t.Error("preinvocation script should have been removed")
	}

	skillPath := filepath.Join(tempDir, ".agents", "skills", "sv-memory", "SKILL.md")
	if _, err := os.Stat(skillPath); !os.IsNotExist(err) {
		t.Error("skill file should have been removed")
	}

	rulePath := filepath.Join(tempDir, ".agents", "rules", "sv-memory.md")
	if _, err := os.Stat(rulePath); !os.IsNotExist(err) {
		t.Error("always-on rule should have been removed")
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
	if !strings.Contains(content, "sv_graph_explore") {
		t.Error("antigravity skill should emphasize sv_graph_explore")
	}
	if !strings.Contains(content, "Tool names in Antigravity") {
		t.Error("antigravity skill should teach suffix-based tool-name resolution")
	}
}

func TestAntigravityRuleContent(t *testing.T) {
	content := antigravityRuleScript()
	if content == "" {
		t.Fatal("antigravity rule template is missing")
	}
	if !strings.Contains(content, "trigger: always_on") {
		t.Error("antigravity rule should declare the always_on trigger")
	}
	for _, marker := range []string{"sv_graph_explore", "Spec-Driven Decision Cycle", "sv_propose_spec", "sv_commit_spec", "sv_mem_session_start", "match by the `sv_*` suffix"} {
		if !strings.Contains(content, marker) {
			t.Errorf("antigravity rule should mention %s", marker)
		}
	}
	if lines := strings.Count(content, "\n") + 1; lines > 45 {
		t.Errorf("antigravity rule should stay short (got %d lines, want <=45)", lines)
	}
}
