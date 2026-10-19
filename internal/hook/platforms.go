package hook

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/svtech-code/sv-memory/internal/config"
	"github.com/svtech-code/sv-memory/internal/protocol"
)

// --- OpenCode Skill ---
// OpenCode does not support PreToolUse hooks natively. Instead, it uses a
// Skills system (SKILL.md) loaded via the `skill` tool, and it natively injects
// the MCP server's `instructions` into the system prompt (V1 and V2). We install
// the skill and inject the AGENTS.md protocol; no TypeScript plugin is needed.

func (e *HookEngine) openCodeSkillDir() string {
	return filepath.Join(e.projPath, ".opencode", "skills", "sv-memory")
}

func (e *HookEngine) openCodeSkillPath() string {
	return filepath.Join(e.openCodeSkillDir(), "SKILL.md")
}

func (e *HookEngine) installOpenCodeSkill() ([]string, error) {
	var created []string
	skillDir := e.openCodeSkillDir()
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return created, fmt.Errorf("failed to create .opencode/skills/sv-memory dir: %w", err)
	}
	skillPath := e.openCodeSkillPath()
	content := hookScript(PlatformOpenCode, e.mode)
	if err := os.WriteFile(skillPath, []byte(content), 0644); err != nil {
		return created, fmt.Errorf("failed to write opencode skill: %w", err)
	}
	created = append(created, skillPath)

	// Also inject sv-memory protocol rules into AGENTS.md so that OpenCode
	// always has the instructions in context without requiring @skill.
	injected, err := protocol.InjectProtocol(e.projPath)
	if err != nil {
		return created, fmt.Errorf("failed to inject protocol rules into AGENTS.md: %w", err)
	}
	for _, f := range injected {
		created = append(created, f+" (protocol rules injected)")
	}

	return created, nil
}

func (e *HookEngine) uninstallOpenCodeSkill() ([]string, error) {
	var removed []string
	skillDir := e.openCodeSkillDir()
	if err := os.RemoveAll(skillDir); err != nil && !os.IsNotExist(err) {
		return removed, fmt.Errorf("failed to remove opencode skill dir: %w", err)
	}
	if _, err := os.Stat(skillDir); os.IsNotExist(err) {
		removed = append(removed, skillDir)
	}
	return removed, nil
}

func (e *HookEngine) openCodeSkillInstalled() bool {
	_, err := os.Stat(e.openCodeSkillPath())
	return err == nil
}

// --- Git Post-Commit Hook ---

func (e *HookEngine) gitHookPath() string {
	gitPath := filepath.Join(e.projPath, ".git")
	if data, err := os.ReadFile(gitPath); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "gitdir:") {
				targetDir := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
				if !filepath.IsAbs(targetDir) {
					targetDir = filepath.Join(e.projPath, targetDir)
				}
				return filepath.Join(targetDir, "hooks", "post-commit")
			}
		}
	}
	return filepath.Join(e.projPath, ".git", "hooks", "post-commit")
}

func (e *HookEngine) installGit() ([]string, error) {
	var created []string

	hookPath := e.gitHookPath()
	hooksDir := filepath.Dir(hookPath)
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return created, fmt.Errorf("failed to create .git/hooks dir: %w", err)
	}

	content := gitPostCommitScript()
	if content == "" {
		return created, fmt.Errorf("missing git post-commit template")
	}

	if err := os.WriteFile(hookPath, []byte(content), 0755); err != nil {
		return created, fmt.Errorf("failed to write git post-commit hook: %w", err)
	}
	created = append(created, hookPath)
	return created, nil
}

func (e *HookEngine) uninstallGit() ([]string, error) {
	var removed []string
	hookPath := e.gitHookPath()
	if err := os.Remove(hookPath); err != nil && !os.IsNotExist(err) {
		return removed, fmt.Errorf("failed to remove git post-commit hook: %w", err)
	}
	if _, err := os.Stat(hookPath); os.IsNotExist(err) {
		removed = append(removed, hookPath)
	}
	return removed, nil
}

func (e *HookEngine) gitInstalled() bool {
	data, err := os.ReadFile(e.gitHookPath())
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "sv-memory")
}

// --- Cursor ---

func (e *HookEngine) cursorMCPPath() string {
	return filepath.Join(e.projPath, ".cursor", "mcp.json")
}

func (e *HookEngine) installCursor() ([]string, error) {
	var created []string
	execPath, err := os.Executable()
	if err != nil {
		return created, err
	}
	execPath = filepath.Clean(execPath)

	path, err := config.ConfigureCursor(e.projPath, execPath)
	if err != nil {
		return created, fmt.Errorf("cursor config failed: %w", err)
	}
	created = append(created, path)

	injected, err := protocol.InjectProtocol(e.projPath)
	if err != nil {
		return created, fmt.Errorf("protocol injection failed: %w", err)
	}
	for _, f := range injected {
		created = append(created, f+" (protocol rules injected)")
	}

	return created, nil
}

func (e *HookEngine) uninstallCursor() ([]string, error) {
	var removed []string
	path := e.cursorMCPPath()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return removed, fmt.Errorf("failed to remove %s: %w", path, err)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		removed = append(removed, path)
	}
	return removed, nil
}

func (e *HookEngine) cursorInstalled() bool {
	_, err := os.Stat(e.cursorMCPPath())
	return err == nil
}

// SupportedPlatforms returns the list of all supported platforms.
func SupportedPlatforms() []Platform {
	return supportedPlatforms
}
