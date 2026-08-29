package hook

import (
	"embed"
	"strings"
)

//go:embed scripts/claude-code-soft.sh scripts/claude-code-strict.sh scripts/claude-code-session-start.sh scripts/claude-code-precompact.sh scripts/claude-code-subagent-stop.sh scripts/claude-code-session-end.sh scripts/codex-noop.sh scripts/antigravity-soft.sh scripts/antigravity-strict.sh scripts/antigravity-skill.md scripts/opencode-skill.md scripts/opencode-plugin.ts scripts/git-post-commit.sh
var hookScriptsFS embed.FS

// gitPostCommitScript returns the embedded Git post-commit hook script source.
func gitPostCommitScript() string {
	data, err := hookScriptsFS.ReadFile("scripts/git-post-commit.sh")
	if err != nil {
		return ""
	}
	return string(data)
}

// claudeLifecycleScript returns the embedded Claude Code lifecycle hook script
// for the given event directory (session_start, precompact, subagent_stop,
// session_end). Event dirs use underscores; script files use hyphens.
func claudeLifecycleScript(eventDir string) string {
	filename := "scripts/claude-code-" + strings.ReplaceAll(eventDir, "_", "-") + ".sh"
	data, err := hookScriptsFS.ReadFile(filename)
	if err != nil {
		return ""
	}
	return string(data)
}

// opencodePluginScript returns the embedded OpenCode TypeScript plugin source.
func opencodePluginScript() string {
	data, err := hookScriptsFS.ReadFile("scripts/opencode-plugin.ts")
	if err != nil {
		return ""
	}
	return string(data)
}

// antigravitySkillScript returns the embedded Antigravity skill source (with YAML frontmatter).
func antigravitySkillScript() string {
	data, err := hookScriptsFS.ReadFile("scripts/antigravity-skill.md")
	if err != nil {
		return ""
	}
	return string(data)
}

func hookScript(platform Platform, mode Mode) string {
	var filename string
	switch platform {
	case PlatformClaudeCode:
		if mode == ModeStrict {
			filename = "scripts/claude-code-strict.sh"
		} else {
			filename = "scripts/claude-code-soft.sh"
		}
	case PlatformCodex:
		filename = "scripts/codex-noop.sh"
	case PlatformAntigravity:
		if mode == ModeStrict {
			filename = "scripts/antigravity-strict.sh"
		} else {
			filename = "scripts/antigravity-soft.sh"
		}
	case PlatformOpenCode:
		filename = "scripts/opencode-skill.md"
	default:
		return ""
	}

	data, err := hookScriptsFS.ReadFile(filename)
	if err != nil {
		return ""
	}
	return string(data)
}
