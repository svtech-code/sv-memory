package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/svtech-code/sv-memory/internal/config"
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
	PlatformAntigravity Platform = "antigravity"
	PlatformCursor      Platform = "cursor"
	PlatformWindsurf    Platform = "windsurf"
	PlatformOpenCode    Platform = "opencode"
	PlatformCodex       Platform = "codex"
	PlatformGit         Platform = "git"
)

var supportedPlatforms = []Platform{
	PlatformClaudeCode,
	PlatformAntigravity,
	PlatformCursor,
	PlatformWindsurf,
	PlatformOpenCode,
	PlatformCodex,
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
		case PlatformWindsurf:
			r.Files, r.Err = e.installWindsurf()
		case PlatformOpenCode:
			r.Files, r.Err = e.installOpenCodeSkill()
		case PlatformCodex:
			r.Files, r.Err = e.installCodex()
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
		case PlatformWindsurf:
			r.Files, r.Err = e.uninstallWindsurf()
		case PlatformOpenCode:
			r.Files, r.Err = e.uninstallOpenCodeSkill()
		case PlatformCodex:
			r.Files, r.Err = e.uninstallCodex()
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
		case PlatformWindsurf:
			status[p] = e.windsurfInstalled()
		case PlatformOpenCode:
			status[p] = e.openCodeSkillInstalled()
		case PlatformCodex:
			status[p] = e.codexInstalled()
		case PlatformGit:
			status[p] = e.gitInstalled()
		default:
			status[p] = false
		}
	}
	return status
}

// --- Claude Code ---

// claudeLifecycleEvents maps Claude Code hook events (settings.json keys) to
// the subdirectory under .claude/hooks/ where the sv-memory script is stored.
// The PreToolUse entry additionally carries a matcher so it only fires for file
// read/search tools.
var claudeLifecycleEvents = []struct {
	Event   string
	Dir     string
	Matcher string
}{
	{Event: "PreToolUse", Dir: "pre_tool_use", Matcher: "Read|Glob|Grep"},
	{Event: "SessionStart", Dir: "session_start"},
	{Event: "SessionEnd", Dir: "session_end"},
	{Event: "PreCompact", Dir: "precompact"},
	{Event: "SubagentStop", Dir: "subagent_stop"},
}

func (e *HookEngine) claudeHooksRoot() string {
	return filepath.Join(e.projPath, ".claude", "hooks")
}

func (e *HookEngine) claudeSettingsPath() string {
	return filepath.Join(e.projPath, ".claude", "settings.json")
}

func (e *HookEngine) claudeHookScriptPath(eventDir string) string {
	return filepath.Join(e.claudeHooksRoot(), eventDir, "sv-memory.sh")
}

// mergeHookGroup appends hookEntry to the hook group matching matcher within an
// existing event hook list (Claude Code's settings.json array format), so any
// user-configured hooks for the same event are preserved. When the entry's
// command is already present the list is returned unchanged (idempotent).
func mergeHookGroup(existingRaw interface{}, matcher string, hookEntry map[string]interface{}, command string) []interface{} {
	groups, _ := existingRaw.([]interface{})
	for _, g := range groups {
		group, ok := g.(map[string]interface{})
		if !ok {
			continue
		}
		if m, _ := group["matcher"].(string); m != matcher {
			continue
		}
		entryList, _ := group["hooks"].([]interface{})
		for _, e := range entryList {
			entry, ok := e.(map[string]interface{})
			if ok && entry["command"] == command {
				return groups
			}
		}
		group["hooks"] = append(entryList, hookEntry)
		return groups
	}
	return append(groups, map[string]interface{}{
		"matcher": matcher,
		"hooks":   []interface{}{hookEntry},
	})
}

// removeHookEntries deletes hook entries whose command matches command from the
// given event's hook list, dropping empty groups and the event key itself when
// nothing remains. Hooks from other tools are kept intact.
func removeHookEntries(hooks map[string]interface{}, event, command string) {
	existing, ok := hooks[event]
	if !ok {
		return
	}
	groups, _ := existing.([]interface{})
	var kept []interface{}
	for _, g := range groups {
		group, ok := g.(map[string]interface{})
		if !ok {
			kept = append(kept, g)
			continue
		}
		entryList, _ := group["hooks"].([]interface{})
		var keptEntries []interface{}
		for _, e := range entryList {
			entry, ok := e.(map[string]interface{})
			if ok && entry["command"] == command {
				continue
			}
			keptEntries = append(keptEntries, e)
		}
		if len(keptEntries) == 0 {
			continue
		}
		group["hooks"] = keptEntries
		kept = append(kept, group)
	}
	if len(kept) == 0 {
		delete(hooks, event)
	} else {
		hooks[event] = kept
	}
}

func (e *HookEngine) installClaudeCode() ([]string, error) {
	var created []string

	// 1. Write hook scripts (PreToolUse + lifecycle events).
	for _, ev := range claudeLifecycleEvents {
		scriptPath := e.claudeHookScriptPath(ev.Dir)
		if err := os.MkdirAll(filepath.Dir(scriptPath), 0755); err != nil {
			return created, fmt.Errorf("failed to create hook dir %s: %w", filepath.Dir(scriptPath), err)
		}

		var scriptContent string
		if ev.Event == "PreToolUse" {
			scriptContent = hookScript(PlatformClaudeCode, e.mode)
		} else {
			scriptContent = claudeLifecycleScript(ev.Dir)
		}
		if scriptContent == "" {
			return created, fmt.Errorf("missing hook script template for %s", ev.Event)
		}
		if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
			return created, fmt.Errorf("failed to write hook script %s: %w", scriptPath, err)
		}
		created = append(created, scriptPath)
	}

	// 2. Update .claude/settings.json with the official hooks array format.
	settingsPath := e.claudeSettingsPath()
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0755); err != nil {
		return created, fmt.Errorf("failed to create settings dir: %w", err)
	}

	settings := make(map[string]interface{})
	if existing, err := os.ReadFile(settingsPath); err == nil {
		_ = json.Unmarshal(existing, &settings)
	}

	hooksRaw := settings["hooks"]
	hooks, _ := hooksRaw.(map[string]interface{})
	if hooks == nil {
		hooks = make(map[string]interface{})
	}

	for _, ev := range claudeLifecycleEvents {
		scriptPath := e.claudeHookScriptPath(ev.Dir)
		hookEntry := map[string]interface{}{
			"type":    "command",
			"command": scriptPath,
			"timeout": 10,
		}
		matcher := ev.Matcher
		if matcher == "" {
			matcher = "*"
		}
		// Merge into any existing hook groups so pre-existing user hooks for
		// the same event are preserved rather than overwritten.
		hooks[ev.Event] = mergeHookGroup(hooks[ev.Event], matcher, hookEntry, scriptPath)
	}
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

	// 1. Remove hook scripts.
	for _, ev := range claudeLifecycleEvents {
		scriptPath := e.claudeHookScriptPath(ev.Dir)
		if err := os.Remove(scriptPath); err != nil && !os.IsNotExist(err) {
			return removed, fmt.Errorf("failed to remove hook script %s: %w", scriptPath, err)
		}
		if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
			removed = append(removed, scriptPath)
		}
	}

	// 2. Remove sv-memory hook references from settings (both the current array
	// format keys and the legacy lowercase preToolUse entry).
	settingsPath := e.claudeSettingsPath()
	if existing, err := os.ReadFile(settingsPath); err == nil {
		settings := make(map[string]interface{})
		_ = json.Unmarshal(existing, &settings)

		hooksRaw := settings["hooks"]
		hooks, _ := hooksRaw.(map[string]interface{})
		if hooks != nil {
			// Remove only the sv-memory hook entries, keeping any other
			// user-configured hooks for the same events.
			for _, ev := range claudeLifecycleEvents {
				removeHookEntries(hooks, ev.Event, e.claudeHookScriptPath(ev.Dir))
			}
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
		removed = append(removed, settingsPath+" (sv-memory hook entries removed)")
	}

	return removed, nil
}

func (e *HookEngine) claudeCodeInstalled() bool {
	// All lifecycle scripts must exist for a complete install.
	for _, ev := range claudeLifecycleEvents {
		if _, err := os.Stat(e.claudeHookScriptPath(ev.Dir)); os.IsNotExist(err) {
			return false
		}
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

	hooksRaw := settings["hooks"]
	hooks, _ := hooksRaw.(map[string]interface{})
	if hooks == nil {
		return false
	}

	// Current array format: PreToolUse[0].matcher entry whose hook command
	// references sv-memory.sh. Also accept the legacy flat preToolUse.script.
	if legacy, ok := hooks["preToolUse"].(map[string]interface{}); ok {
		if script, ok := legacy["script"].(string); ok && filepath.Base(script) == "sv-memory.sh" {
			return true
		}
	}

	preToolUseArr, ok := hooks["PreToolUse"].([]interface{})
	if !ok || len(preToolUseArr) == 0 {
		return false
	}
	// Search every hook group for an sv-memory entry so a user hook listed
	// first does not hide the install status.
	for _, g := range preToolUseArr {
		group, ok := g.(map[string]interface{})
		if !ok {
			continue
		}
		hooksList, ok := group["hooks"].([]interface{})
		if !ok {
			continue
		}
		for _, h := range hooksList {
			hookEntry, ok := h.(map[string]interface{})
			if !ok {
				continue
			}
			command, _ := hookEntry["command"].(string)
			if command != "" && filepath.Base(command) == "sv-memory.sh" {
				return true
			}
		}
	}
	return false
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

func (e *HookEngine) antigravitySkillDir() string {
	return filepath.Join(e.projPath, ".agents", "skills", "sv-memory")
}

func (e *HookEngine) antigravitySkillPath() string {
	return filepath.Join(e.antigravitySkillDir(), "SKILL.md")
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

	// 3. Write Antigravity native skill (.agents/skills/sv-memory/SKILL.md)
	skillDir := e.antigravitySkillDir()
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return created, fmt.Errorf("failed to create .agents/skills/sv-memory dir: %w", err)
	}
	skillPath := e.antigravitySkillPath()
	skillContent := antigravitySkillScript()
	if skillContent == "" {
		return created, fmt.Errorf("missing antigravity skill template")
	}
	if err := os.WriteFile(skillPath, []byte(skillContent), 0644); err != nil {
		return created, fmt.Errorf("failed to write antigravity skill: %w", err)
	}
	created = append(created, skillPath)

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

	// 3. Remove skill (.agents/skills/sv-memory/SKILL.md)
	skillPath := e.antigravitySkillPath()
	if err := os.Remove(skillPath); err != nil && !os.IsNotExist(err) {
		return removed, fmt.Errorf("failed to remove agy skill: %w", err)
	}
	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		removed = append(removed, skillPath)
		_ = os.Remove(e.antigravitySkillDir())
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
	skillPath := e.antigravitySkillPath()
	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
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

// --- OpenCode Skill + Plugin ---
// OpenCode does not support PreToolUse hooks natively. Instead, it uses a
// Skills system (SKILL.md) loaded via the `skill` tool plus a native TypeScript
// plugin (sv-memory.ts) that registers the sv_memory_context tool. We install
// both so OpenCode gets the nudge AND a first-class context-pack tool.

func (e *HookEngine) openCodeSkillDir() string {
	return filepath.Join(e.projPath, ".opencode", "skills", "sv-memory")
}

func (e *HookEngine) openCodeSkillPath() string {
	return filepath.Join(e.openCodeSkillDir(), "SKILL.md")
}

func (e *HookEngine) openCodePluginPath() string {
	return filepath.Join(e.projPath, ".opencode", "plugin", "sv-memory.ts")
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

	// Native TypeScript plugin: registers the sv_memory_context tool.
	pluginPath := e.openCodePluginPath()
	if err := os.MkdirAll(filepath.Dir(pluginPath), 0755); err != nil {
		return created, fmt.Errorf("failed to create .opencode/plugin dir: %w", err)
	}
	pluginContent := opencodePluginScript(e.mode)
	if pluginContent == "" {
		return created, fmt.Errorf("missing opencode plugin template")
	}
	if err := os.WriteFile(pluginPath, []byte(pluginContent), 0644); err != nil {
		return created, fmt.Errorf("failed to write opencode plugin: %w", err)
	}
	created = append(created, pluginPath)

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

	pluginPath := e.openCodePluginPath()
	if err := os.Remove(pluginPath); err != nil && !os.IsNotExist(err) {
		return removed, fmt.Errorf("failed to remove opencode plugin: %w", err)
	}
	if _, err := os.Stat(pluginPath); os.IsNotExist(err) {
		removed = append(removed, pluginPath)
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

// --- Windsurf ---

func (e *HookEngine) windsurfMCPPath() string {
	return filepath.Join(e.projPath, ".windsurf", "mcp_config.json")
}

func (e *HookEngine) installWindsurf() ([]string, error) {
	var created []string
	execPath, err := os.Executable()
	if err != nil {
		return created, err
	}
	execPath = filepath.Clean(execPath)

	path, err := config.ConfigureWindsurf(e.projPath, execPath)
	if err != nil {
		return created, fmt.Errorf("windsurf config failed: %w", err)
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

func (e *HookEngine) uninstallWindsurf() ([]string, error) {
	var removed []string
	path := e.windsurfMCPPath()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return removed, fmt.Errorf("failed to remove %s: %w", path, err)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		removed = append(removed, path)
	}
	return removed, nil
}

func (e *HookEngine) windsurfInstalled() bool {
	_, err := os.Stat(e.windsurfMCPPath())
	return err == nil
}

// SupportedPlatforms returns the list of all supported platforms.
func SupportedPlatforms() []Platform {
	return supportedPlatforms
}
