package hook

import (
	"embed"
	"fmt"
)

//go:embed scripts/claude-code-soft.sh scripts/claude-code-strict.sh scripts/codex-noop.sh
var hookScriptsFS embed.FS

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
	default:
		return ""
	}

	data, err := hookScriptsFS.ReadFile(filename)
	if err != nil {
		return ""
	}
	return string(data)
}

func HookScriptContent(platform Platform, mode Mode) (string, error) {
	content := hookScript(platform, mode)
	if content == "" {
		return "", fmt.Errorf("no hook template for platform %s mode %s", platform, mode)
	}
	return content, nil
}
