package hook

import (
	"fmt"
	"os"
	"path/filepath"
)

// Mode controls hook strictness.
type Mode string

const (
	ModeSoft   Mode = "soft"
	ModeStrict Mode = "strict"
)

// Platform identifies an AI coding assistant platform.
type Platform string

const (
	PlatformClaudeCode  Platform = "claude-code"
	PlatformAntigravity Platform = "antigravity"
	PlatformCursor      Platform = "cursor"
	PlatformOpenCode    Platform = "opencode"
	PlatformGit         Platform = "git"
)

var supportedPlatforms = []Platform{
	PlatformClaudeCode,
	PlatformAntigravity,
	PlatformCursor,
	PlatformOpenCode,
	PlatformGit,
}

// HookEngine manages PreToolUse hook installation for AI assistants.
type HookEngine struct {
	projPath string
	mode     Mode
}

// New creates a HookEngine for the given project directory and mode.
func New(projPath string, mode Mode) *HookEngine {
	if mode == "" {
		mode = ModeSoft
	}
	return &HookEngine{projPath: projPath, mode: mode}
}

// InstallResult describes what was installed for a single platform.
type InstallResult struct {
	Platform Platform `json:"platform"`
	Files    []string `json:"files"`
	Err      error    `json:"-"`
}

// contextInjectionMarkerPath returns the marker file whose presence enables the
// silent context-injection block in the Claude Code hook scripts. Created by
// `sv-memory hooks install --context-injection` (opt-in, default off). It is a
// filesystem marker rather than a config key so the hook script can check it
// without spawning the sv-memory binary just to read configuration.
func (e *HookEngine) contextInjectionMarkerPath() string {
	return filepath.Join(e.projPath, ".sv-memory", "context-injection-enabled")
}

// ContextInjectionEnabled reports whether the silent context-injection marker
// is present for this project.
func (e *HookEngine) ContextInjectionEnabled() bool {
	_, err := os.Stat(e.contextInjectionMarkerPath())
	return err == nil
}

// SetContextInjection creates (enabled) or removes (disabled) the marker that
// activates the silent context-injection block in the Claude Code hook scripts.
// It never fails when the directory cannot be created — a best-effort toggle.
func (e *HookEngine) SetContextInjection(enabled bool) error {
	marker := e.contextInjectionMarkerPath()
	if !enabled {
		if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove context-injection marker: %w", err)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(marker), 0755); err != nil {
		return fmt.Errorf("failed to create .sv-memory dir for marker: %w", err)
	}
	if err := os.WriteFile(marker, []byte("enabled\n"), 0644); err != nil {
		return fmt.Errorf("failed to write context-injection marker: %w", err)
	}
	return nil
}

// Install installs PreToolUse hooks for the given platforms.
// If platforms is empty, it installs for all supported platforms.
func (e *HookEngine) Install(platforms []Platform) []InstallResult {
	if len(platforms) == 0 {
		platforms = supportedPlatforms
	}

	var results []InstallResult
	for _, p := range platforms {
		r := InstallResult{Platform: p}
		switch p {
		case PlatformClaudeCode:
			r.Files, r.Err = e.installClaudeCode()
		case PlatformAntigravity:
			r.Files, r.Err = e.installAntigravity()
		case PlatformCursor:
			r.Files, r.Err = e.installCursor()
		case PlatformOpenCode:
			r.Files, r.Err = e.installOpenCodeSkill()
		case PlatformGit:
			r.Files, r.Err = e.installGit()
		default:
			r.Err = fmt.Errorf("unsupported platform: %s", p)
		}
		results = append(results, r)
	}
	return results
}

// Uninstall removes PreToolUse hooks for the given platforms.
func (e *HookEngine) Uninstall(platforms []Platform) []InstallResult {
	if len(platforms) == 0 {
		platforms = supportedPlatforms
	}

	var results []InstallResult
	for _, p := range platforms {
		r := InstallResult{Platform: p}
		switch p {
		case PlatformClaudeCode:
			r.Files, r.Err = e.uninstallClaudeCode()
		case PlatformAntigravity:
			r.Files, r.Err = e.uninstallAntigravity()
		case PlatformCursor:
			r.Files, r.Err = e.uninstallCursor()
		case PlatformOpenCode:
			r.Files, r.Err = e.uninstallOpenCodeSkill()
		case PlatformGit:
			r.Files, r.Err = e.uninstallGit()
		default:
			r.Err = fmt.Errorf("unsupported platform: %s", p)
		}
		results = append(results, r)
	}
	return results
}

// Status returns a map of platform -> installed (true/false).
func (e *HookEngine) Status(platforms []Platform) map[Platform]bool {
	if len(platforms) == 0 {
		platforms = supportedPlatforms
	}

	status := make(map[Platform]bool)
	for _, p := range platforms {
		switch p {
		case PlatformClaudeCode:
			status[p] = e.claudeCodeInstalled()
		case PlatformAntigravity:
			status[p] = e.antigravityInstalled()
		case PlatformCursor:
			status[p] = e.cursorInstalled()
		case PlatformOpenCode:
			status[p] = e.openCodeSkillInstalled()
		case PlatformGit:
			status[p] = e.gitInstalled()
		default:
			status[p] = false
		}
	}
	return status
}
