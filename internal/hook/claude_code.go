package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// claudeLifecycleEvents maps Claude Code hook events (settings.json keys) to
// the subdirectory under .claude/hooks/ where the sv-memory script is stored.
// The PreToolUse entry additionally carries a matcher so it only fires for file
// read/search tools.
var claudeLifecycleEvents = []struct {
	Event   string
	Dir     string
	Matcher string
}{
	{Event: "PreToolUse", Dir: "pre_tool_use", Matcher: "Read|Glob|Grep|Write|Edit"},
	{Event: "UserPromptSubmit", Dir: "user_prompt_submit"},
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

	existing, err := os.ReadFile(e.claudeSettingsPath())
	if err != nil {
		return false
	}
	settings := make(map[string]interface{})
	if err := json.Unmarshal(existing, &settings); err != nil {
		return false
	}

	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		return false
	}
	return claudeLegacyHookPresent(hooks) || claudePreToolUseHookPresent(hooks)
}

// claudeLegacyHookPresent reports whether the legacy flat preToolUse.script
// entry references the sv-memory script.
func claudeLegacyHookPresent(hooks map[string]interface{}) bool {
	legacy, ok := hooks["preToolUse"].(map[string]interface{})
	if !ok {
		return false
	}
	script, ok := legacy["script"].(string)
	return ok && filepath.Base(script) == "sv-memory.sh"
}

// claudePreToolUseHookPresent searches every PreToolUse hook group for an
// sv-memory command so a user hook listed first does not hide the install
// status.
func claudePreToolUseHookPresent(hooks map[string]interface{}) bool {
	groups, ok := hooks["PreToolUse"].([]interface{})
	if !ok {
		return false
	}
	for _, g := range groups {
		group, ok := g.(map[string]interface{})
		if !ok {
			continue
		}
		entries, ok := group["hooks"].([]interface{})
		if !ok {
			continue
		}
		for _, h := range entries {
			entry, ok := h.(map[string]interface{})
			if !ok {
				continue
			}
			command, _ := entry["command"].(string)
			if command != "" && filepath.Base(command) == "sv-memory.sh" {
				return true
			}
		}
	}
	return false
}
