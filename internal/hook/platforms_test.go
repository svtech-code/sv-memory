package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHookScriptContentOpenCode(t *testing.T) {
	content := mustHookScript(t, PlatformOpenCode, ModeSoft)
	if !strings.Contains(content, "sv_mem_search") {
		t.Error("opencode skill should contain sv_mem_search instructions")
	}
	if !strings.Contains(content, "Tool names in OpenCode") {
		t.Error("opencode skill should teach the sv-memory_ tool-name prefix")
	}
	if !strings.Contains(content, "sv-memory_sv_graph_explore") {
		t.Error("opencode skill should map sv_graph_explore to sv-memory_sv_graph_explore")
	}
}

func TestHookScriptContentOpenCodeStrict(t *testing.T) {
	content := mustHookScript(t, PlatformOpenCode, ModeStrict)
	if !strings.Contains(content, "sv_mem_search") {
		t.Error("opencode strict skill should contain sv_mem_search instructions")
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
	if status[PlatformAntigravity] {
		t.Error("expected antigravity to not be installed in empty dir")
	}
	if status[PlatformCursor] {
		t.Error("expected cursor to not be installed in empty dir")
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

// TestSkillParityAndSessionNotes guards the canonical skill files against
// content drift: both skills must contain the mandatory workflow sections
// (graph-first, spec cycle, topic keys, progressive disclosure, maintenance),
// list no hidden tools as core, and carry the correct session-lifecycle note
// for their platform.
func TestSkillParityAndSessionNotes(t *testing.T) {
	required := []string{
		"**Graph:**",
		"sv_graph_explore",
		"Spec-Driven Decision Cycle",
		"sv_propose_spec",
		"sv_validate_decision",
		"sv_commit_spec",
		"## Topic Keys",
		"sv_mem_suggest_topic_key",
		"## Progressive Disclosure",
		"## Tool Quick Reference",
		"## Session Lifecycle",
		"## When to save what",
		"## Periodic maintenance",
		"sv_mem_review",
		"sv_mem_compact",
	}

	// Hidden tools must NOT appear as standalone core entries — they belong
	// only in the "(opt-in)" maintenance/admin line.
	hiddenAsCore := []string{
		"sv_mem_diagnose",
		"sv_graph_report",
	}

	type skillCase struct {
		name            string
		content         string
		wantAutoManaged bool // true = platform auto-manages session
	}

	cases := []skillCase{
		{name: "opencode", content: hookScript(PlatformOpenCode, ModeStrict), wantAutoManaged: true},
		{name: "antigravity", content: antigravitySkillScript(), wantAutoManaged: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, marker := range required {
				if !strings.Contains(tc.content, marker) {
					t.Errorf("%s skill missing required marker: %s", tc.name, marker)
				}
			}
			if tc.wantAutoManaged {
				if !strings.Contains(tc.content, "do not call `sv_mem_session_start` again") {
					t.Error("opencode skill must contain auto-managed session note")
				}
			} else {
				if strings.Contains(tc.content, "do not call `sv_mem_session_start` again") {
					t.Errorf("%s skill must NOT contain auto-managed session note (no plugin auto-start)", tc.name)
				}
			}
			for _, tool := range hiddenAsCore {
				// Check that the tool is NOT a standalone bullet in the core
				// Quick Reference section. It IS allowed in the opt-in line.
				corePattern := "- `" + tool + "`"
				optInLine := "opt-in):"
				lines := strings.Split(tc.content, "\n")
				for _, line := range lines {
					if strings.Contains(line, corePattern) && !strings.Contains(line, optInLine) && !strings.Contains(line, "SV_MEMORY_FULL_TOOLS") {
						t.Errorf("%s skill lists hidden tool %s as core (should be opt-in only)", tc.name, tool)
					}
				}
			}
		})
	}
}
