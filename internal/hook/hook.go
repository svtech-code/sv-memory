package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/svtech-code/sv-memory/internal/protocol"
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
	PlatformCodex       Platform = "codex"
	PlatformAntigravity Platform = "antigravity"
	PlatformOpenCode    Platform = "opencode"
)

var supportedPlatforms = []Platform{PlatformClaudeCode, PlatformCodex, PlatformAntigravity, PlatformOpenCode}

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
		case PlatformCodex:
			r.Files, r.Err = e.installCodex()
		case PlatformAntigravity:
			r.Files, r.Err = e.installAntigravity()
		case PlatformOpenCode:
			r.Files, r.Err = e.installOpenCodeSkill()
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
		case PlatformCodex:
			r.Files, r.Err = e.uninstallCodex()
		case PlatformAntigravity:
			r.Files, r.Err = e.uninstallAntigravity()
		case PlatformOpenCode:
			r.Files, r.Err = e.uninstallOpenCodeSkill()
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
		case PlatformCodex:
			status[p] = e.codexInstalled()
		case PlatformAntigravity:
			status[p] = e.antigravityInstalled()
		case PlatformOpenCode:
			status[p] = e.openCodeSkillInstalled()
		default:
			status[p] = false
		}
	}
	return status
}

// --- Claude Code ---

func (e *HookEngine) claudeHookDir() string {
	return filepath.Join(e.projPath, ".claude", "hooks", "pre_tool_use")
}

func (e *HookEngine) claudeSettingsPath() string {
	return filepath.Join(e.projPath, ".claude", "settings.json")
}

func (e *HookEngine) claudeHookScriptPath() string {
	return filepath.Join(e.claudeHookDir(), "sv-memory.sh")
}

func (e *HookEngine) installClaudeCode() ([]string, error) {
	var created []string

	// 1. Write hook script
	hookDir := e.claudeHookDir()
	if err := os.MkdirAll(hookDir, 0755); err != nil {
		return created, fmt.Errorf("failed to create hook dir %s: %w", hookDir, err)
	}

	scriptPath := e.claudeHookScriptPath()
	scriptContent := hookScript(PlatformClaudeCode, e.mode)
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		return created, fmt.Errorf("failed to write hook script %s: %w", scriptPath, err)
	}
	created = append(created, scriptPath)

	// 2. Update .claude/settings.json
	settingsPath := e.claudeSettingsPath()
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0755); err != nil {
		return created, fmt.Errorf("failed to create settings dir: %w", err)
	}

	settings := make(map[string]interface{})
	if existing, err := os.ReadFile(settingsPath); err == nil {
		_ = json.Unmarshal(existing, &settings)
	}

	hooksRaw, _ := settings["hooks"]
	hooks, _ := hooksRaw.(map[string]interface{})
	if hooks == nil {
		hooks = make(map[string]interface{})
	}

	preToolUseRaw, _ := hooks["preToolUse"]
	preToolUse, _ := preToolUseRaw.(map[string]interface{})
	if preToolUse == nil {
		preToolUse = make(map[string]interface{})
	}

	preToolUse["script"] = scriptPath
	hooks["preToolUse"] = preToolUse
	settings["hooks"] = hooks

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return created, fmt.Errorf("failed to marshal settings: %w", err)
	}
	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		return created, fmt.Errorf("failed to write settings %s: %w", settingsPath, err)
	}
	created = append(created, settingsPath)

	return created, nil
}

func (e *HookEngine) uninstallClaudeCode() ([]string, error) {
	var removed []string

	// 1. Remove hook script
	scriptPath := e.claudeHookScriptPath()
	if err := os.Remove(scriptPath); err != nil && !os.IsNotExist(err) {
		return removed, fmt.Errorf("failed to remove hook script %s: %w", scriptPath, err)
	}
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		removed = append(removed, scriptPath)
	}

	// 2. Remove hook reference from settings
	settingsPath := e.claudeSettingsPath()
	if existing, err := os.ReadFile(settingsPath); err == nil {
		settings := make(map[string]interface{})
		_ = json.Unmarshal(existing, &settings)

		hooksRaw, _ := settings["hooks"]
		hooks, _ := hooksRaw.(map[string]interface{})
		if hooks != nil {
			delete(hooks, "preToolUse")
			if len(hooks) == 0 {
				delete(settings, "hooks")
			} else {
				settings["hooks"] = hooks
			}
		} else {
			delete(settings, "hooks")
		}

		data, _ := json.MarshalIndent(settings, "", "  ")
		_ = os.WriteFile(settingsPath, data, 0644)
		removed = append(removed, settingsPath+" (preToolUse entry removed)")
	}

	return removed, nil
}

func (e *HookEngine) claudeCodeInstalled() bool {
	if _, err := os.Stat(e.claudeHookScriptPath()); os.IsNotExist(err) {
		return false
	}

	settingsPath := e.claudeSettingsPath()
	existing, err := os.ReadFile(settingsPath)
	if err != nil {
		return false
	}

	settings := make(map[string]interface{})
	if err := json.Unmarshal(existing, &settings); err != nil {
		return false
	}

	hooksRaw, _ := settings["hooks"]
	hooks, _ := hooksRaw.(map[string]interface{})
	if hooks == nil {
		return false
	}

	preToolUseRaw, _ := hooks["preToolUse"]
	preToolUse, _ := preToolUseRaw.(map[string]interface{})
	if preToolUse == nil {
		return false
	}

	script, _ := preToolUse["script"].(string)
	return script != "" && filepath.Base(script) == "sv-memory.sh"
}

// --- Codex ---

func (e *HookEngine) codexHooksPath() string {
	return filepath.Join(e.projPath, ".codex", "hooks.json")
}

func (e *HookEngine) installCodex() ([]string, error) {
	var created []string

	hooksPath := e.codexHooksPath()
	hooksDir := filepath.Dir(hooksPath)
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return created, fmt.Errorf("failed to create .codex dir: %w", err)
	}

	// Write a no-op hook config for Codex.
	// On Codex Desktop, PreToolUse with additionalContext breaks Bash calls,
	// so the hook script intentionally does nothing. The real mechanism is AGENTS.md.
	hookConfig := map[string]interface{}{
		"preToolUse": map[string]interface{}{
			"script": filepath.Join(e.projPath, ".codex", "hooks", "sv-memory.sh"),
		},
	}

	// Merge with existing hooks.json if it exists
	var existingData map[string]interface{}
	if b, err := os.ReadFile(hooksPath); err == nil {
		_ = json.Unmarshal(b, &existingData)
	}
	if existingData == nil {
		existingData = hookConfig
	} else {
		for k, v := range hookConfig {
			existingData[k] = v
		}
	}

	data, err := json.MarshalIndent(existingData, "", "  ")
	if err != nil {
		return created, fmt.Errorf("failed to marshal hooks.json: %w", err)
	}
	if err := os.WriteFile(hooksPath, data, 0644); err != nil {
		return created, fmt.Errorf("failed to write hooks.json: %w", err)
	}
	created = append(created, hooksPath)

	// Write the no-op script
	scriptDir := filepath.Join(e.projPath, ".codex", "hooks")
	if err := os.MkdirAll(scriptDir, 0755); err != nil {
		return created, fmt.Errorf("failed to create .codex/hooks dir: %w", err)
	}
	scriptPath := filepath.Join(scriptDir, "sv-memory.sh")
	if err := os.WriteFile(scriptPath, []byte(hookScript(PlatformCodex, e.mode)), 0755); err != nil {
		return created, fmt.Errorf("failed to write codex hook script: %w", err)
	}
	created = append(created, scriptPath)

	return created, nil
}

func (e *HookEngine) uninstallCodex() ([]string, error) {
	var removed []string

	// Remove hooks.json
	hooksPath := e.codexHooksPath()
	if err := os.Remove(hooksPath); err != nil && !os.IsNotExist(err) {
		return removed, fmt.Errorf("failed to remove %s: %w", hooksPath, err)
	}
	if _, err := os.Stat(hooksPath); os.IsNotExist(err) {
		removed = append(removed, hooksPath)
	}

	// Remove the no-op script
	scriptPath := filepath.Join(e.projPath, ".codex", "hooks", "sv-memory.sh")
	if err := os.Remove(scriptPath); err != nil && !os.IsNotExist(err) {
		return removed, fmt.Errorf("failed to remove %s: %w", scriptPath, err)
	}
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		removed = append(removed, scriptPath)
	}

	return removed, nil
}

func (e *HookEngine) codexInstalled() bool {
	hooksPath := e.codexHooksPath()
	if _, err := os.Stat(hooksPath); os.IsNotExist(err) {
		return false
	}
	return true
}

// --- Antigravity CLI (agy) ---

func (e *HookEngine) antigravityHooksPath() string {
	return filepath.Join(e.projPath, ".agents", "hooks.json")
}

func (e *HookEngine) antigravityHookScriptPath() string {
	return filepath.Join(e.projPath, ".agents", "hooks", "sv-memory.sh")
}

func (e *HookEngine) installAntigravity() ([]string, error) {
	var created []string

	// 1. Write hook script
	scriptPath := e.antigravityHookScriptPath()
	scriptDir := filepath.Dir(scriptPath)
	if err := os.MkdirAll(scriptDir, 0755); err != nil {
		return created, fmt.Errorf("failed to create .agents/hooks dir: %w", err)
	}
	scriptContent := hookScript(PlatformAntigravity, e.mode)
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		return created, fmt.Errorf("failed to write agy hook script: %w", err)
	}
	created = append(created, scriptPath)

	// 2. Build hooks.json config
	// agy hooks.json shape: named hook groups with event keys.
	// PreToolUse uses [{matcher, hooks: [{type, command, timeout}]}]
	hooksEntry := map[string]interface{}{
		"sv-memory": map[string]interface{}{
			"enabled": true,
			"PreToolUse": []map[string]interface{}{
				{
					"matcher": "view_file|grep_search|list_dir",
					"hooks": []map[string]interface{}{
						{
							"type":    "command",
							"command": scriptPath,
							"timeout": 5,
						},
					},
				},
			},
		},
	}

	hooksPath := e.antigravityHooksPath()
	hooksDir := filepath.Dir(hooksPath)
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return created, fmt.Errorf("failed to create .agents dir: %w", err)
	}

	// Merge with existing hooks.json if it exists (preserve other named hooks)
	var existingData map[string]interface{}
	if b, err := os.ReadFile(hooksPath); err == nil {
		_ = json.Unmarshal(b, &existingData)
	}
	if existingData == nil {
		existingData = hooksEntry
	} else {
		for k, v := range hooksEntry {
			existingData[k] = v
		}
	}

	data, err := json.MarshalIndent(existingData, "", "  ")
	if err != nil {
		return created, fmt.Errorf("failed to marshal agy hooks.json: %w", err)
	}
	if err := os.WriteFile(hooksPath, data, 0644); err != nil {
		return created, fmt.Errorf("failed to write agy hooks.json: %w", err)
	}
	created = append(created, hooksPath)

	return created, nil
}

func (e *HookEngine) uninstallAntigravity() ([]string, error) {
	var removed []string

	// 1. Remove hook script
	scriptPath := e.antigravityHookScriptPath()
	if err := os.Remove(scriptPath); err != nil && !os.IsNotExist(err) {
		return removed, fmt.Errorf("failed to remove agy hook script: %w", err)
	}
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		removed = append(removed, scriptPath)
	}

	// 2. Remove our entry from hooks.json (preserve other named hooks)
	hooksPath := e.antigravityHooksPath()
	if existing, err := os.ReadFile(hooksPath); err == nil {
		data := make(map[string]interface{})
		_ = json.Unmarshal(existing, &data)

		delete(data, "sv-memory")

		if len(data) == 0 {
			if err := os.Remove(hooksPath); err != nil && !os.IsNotExist(err) {
				return removed, fmt.Errorf("failed to remove agy hooks.json: %w", err)
			}
			if _, err := os.Stat(hooksPath); os.IsNotExist(err) {
				removed = append(removed, hooksPath)
			}
		} else {
			out, _ := json.MarshalIndent(data, "", "  ")
			_ = os.WriteFile(hooksPath, out, 0644)
			removed = append(removed, hooksPath+" (sv-memory entry removed)")
		}
	}

	return removed, nil
}

func (e *HookEngine) antigravityInstalled() bool {
	hooksPath := e.antigravityHooksPath()
	if _, err := os.Stat(hooksPath); os.IsNotExist(err) {
		return false
	}
	scriptPath := e.antigravityHookScriptPath()
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		return false
	}
	existing, err := os.ReadFile(hooksPath)
	if err != nil {
		return false
	}
	data := make(map[string]interface{})
	if err := json.Unmarshal(existing, &data); err != nil {
		return false
	}
	_, hasEntry := data["sv-memory"]
	return hasEntry
}

// --- OpenCode Skill ---
// OpenCode does not support PreToolUse hooks natively. Instead, it uses
// a Skills system (SKILL.md) loaded via the `skill` tool. We install a
// skill that mirrors the nudge instructions from hook scripts.

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

// SupportedPlatforms returns the list of all supported platforms.
func SupportedPlatforms() []Platform {
	return supportedPlatforms
}
