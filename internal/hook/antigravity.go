package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// --- Antigravity CLI (agy) ---

func (e *HookEngine) antigravityHooksPath() string {
	return filepath.Join(e.projPath, ".agents", "hooks.json")
}

func (e *HookEngine) antigravityHookScriptPath() string {
	return filepath.Join(e.projPath, ".agents", "hooks", "sv-memory.sh")
}

func (e *HookEngine) antigravityPreInvocationScriptPath() string {
	return filepath.Join(e.projPath, ".agents", "hooks", "sv-memory-preinvocation.sh")
}

func (e *HookEngine) antigravitySkillDir() string {
	return filepath.Join(e.projPath, ".agents", "skills", "sv-memory")
}

func (e *HookEngine) antigravitySkillPath() string {
	return filepath.Join(e.antigravitySkillDir(), "SKILL.md")
}

func (e *HookEngine) antigravityRuleDir() string {
	return filepath.Join(e.projPath, ".agents", "rules")
}

func (e *HookEngine) antigravityRulePath() string {
	return filepath.Join(e.antigravityRuleDir(), "sv-memory.md")
}

func (e *HookEngine) installAntigravity() ([]string, error) {
	var created []string

	if err := e.writeAntigravityScripts(&created); err != nil {
		return created, err
	}
	if err := e.writeAntigravityHooksJSON(&created); err != nil {
		return created, err
	}
	if err := e.writeAntigravitySkill(&created); err != nil {
		return created, err
	}
	if err := e.writeAntigravityRule(&created); err != nil {
		return created, err
	}
	return created, nil
}

// writeAntigravityScripts writes the PreToolUse and PreInvocation hook scripts.
func (e *HookEngine) writeAntigravityScripts(created *[]string) error {
	scriptPath := e.antigravityHookScriptPath()
	scriptDir := filepath.Dir(scriptPath)
	if err := os.MkdirAll(scriptDir, 0755); err != nil {
		return fmt.Errorf("failed to create .agents/hooks dir: %w", err)
	}
	scriptContent := hookScript(PlatformAntigravity, e.mode)
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		return fmt.Errorf("failed to write agy hook script: %w", err)
	}
	*created = append(*created, scriptPath)

	// PreInvocation hook script (deterministic auto-boot + nudge).
	prePath := e.antigravityPreInvocationScriptPath()
	preContent := antigravityPreInvocationScript()
	if preContent == "" {
		return fmt.Errorf("missing antigravity preinvocation template")
	}
	if err := os.WriteFile(prePath, []byte(preContent), 0755); err != nil {
		return fmt.Errorf("failed to write agy preinvocation script: %w", err)
	}
	*created = append(*created, prePath)
	return nil
}

// writeAntigravityHooksJSON merges the sv-memory hook group into hooks.json,
// preserving other named hooks.
func (e *HookEngine) writeAntigravityHooksJSON(created *[]string) error {
	scriptPath := e.antigravityHookScriptPath()
	prePath := e.antigravityPreInvocationScriptPath()

	// agy hooks.json shape: named hook groups with event keys.
	// PreToolUse uses [{matcher, hooks: [{type, command, timeout}]}]
	// PreInvocation is flat: a list of handler objects (no matcher wrapper).
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
			"PreInvocation": []map[string]interface{}{
				{
					"type":    "command",
					"command": prePath,
					"timeout": 15,
				},
			},
		},
	}

	hooksPath := e.antigravityHooksPath()
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0755); err != nil {
		return fmt.Errorf("failed to create .agents dir: %w", err)
	}

	// Merge with existing hooks.json if it exists (preserve other named hooks).
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
		return fmt.Errorf("failed to marshal agy hooks.json: %w", err)
	}
	if err := os.WriteFile(hooksPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write agy hooks.json: %w", err)
	}
	*created = append(*created, hooksPath)
	return nil
}

// writeAntigravitySkill writes the native skill at .agents/skills/sv-memory.
func (e *HookEngine) writeAntigravitySkill(created *[]string) error {
	skillDir := e.antigravitySkillDir()
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return fmt.Errorf("failed to create .agents/skills/sv-memory dir: %w", err)
	}
	skillContent := antigravitySkillScript()
	if skillContent == "" {
		return fmt.Errorf("missing antigravity skill template")
	}
	skillPath := e.antigravitySkillPath()
	if err := os.WriteFile(skillPath, []byte(skillContent), 0644); err != nil {
		return fmt.Errorf("failed to write antigravity skill: %w", err)
	}
	*created = append(*created, skillPath)
	return nil
}

// writeAntigravityRule writes the short always-on rule so the graph-first /
// spec-first protocol stays salient for models that dilute the large injected
// AGENTS.md.
func (e *HookEngine) writeAntigravityRule(created *[]string) error {
	ruleDir := e.antigravityRuleDir()
	if err := os.MkdirAll(ruleDir, 0755); err != nil {
		return fmt.Errorf("failed to create .agents/rules dir: %w", err)
	}
	ruleContent := antigravityRuleScript()
	if ruleContent == "" {
		return fmt.Errorf("missing antigravity rule template")
	}
	rulePath := e.antigravityRulePath()
	if err := os.WriteFile(rulePath, []byte(ruleContent), 0644); err != nil {
		return fmt.Errorf("failed to write antigravity rule: %w", err)
	}
	*created = append(*created, rulePath)
	return nil
}

func (e *HookEngine) uninstallAntigravity() ([]string, error) {
	var removed []string

	if err := removeTrackedFile(e.antigravityHookScriptPath(), &removed, "agy hook script"); err != nil {
		return removed, err
	}
	if err := removeTrackedFile(e.antigravityPreInvocationScriptPath(), &removed, "agy preinvocation script"); err != nil {
		return removed, err
	}
	if err := e.removeAntigravityHooksEntry(&removed); err != nil {
		return removed, err
	}
	if err := removeTrackedFile(e.antigravitySkillPath(), &removed, "agy skill"); err != nil {
		return removed, err
	}
	_ = os.Remove(e.antigravitySkillDir())
	if err := removeTrackedFile(e.antigravityRulePath(), &removed, "agy rule"); err != nil {
		return removed, err
	}
	_ = os.Remove(e.antigravityRuleDir())

	return removed, nil
}

// removeAntigravityHooksEntry deletes the sv-memory group from hooks.json,
// removing the file only when no other named hooks remain.
func (e *HookEngine) removeAntigravityHooksEntry(removed *[]string) error {
	hooksPath := e.antigravityHooksPath()
	existing, err := os.ReadFile(hooksPath)
	if err != nil {
		return nil
	}
	data := make(map[string]interface{})
	_ = json.Unmarshal(existing, &data)

	delete(data, "sv-memory")

	if len(data) == 0 {
		return removeTrackedFile(hooksPath, removed, "agy hooks.json")
	}
	out, _ := json.MarshalIndent(data, "", "  ")
	_ = os.WriteFile(hooksPath, out, 0644)
	*removed = append(*removed, hooksPath+" (sv-memory entry removed)")
	return nil
}

func (e *HookEngine) antigravityInstalled() bool {
	paths := []string{
		e.antigravityHooksPath(),
		e.antigravityHookScriptPath(),
		e.antigravityPreInvocationScriptPath(),
		e.antigravitySkillPath(),
		e.antigravityRulePath(),
	}
	for _, path := range paths {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return false
		}
	}

	existing, err := os.ReadFile(e.antigravityHooksPath())
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

// removeTrackedFile removes path and records it in removed when it no longer
// exists. label is used in the error message.
func removeTrackedFile(path string, removed *[]string, label string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove %s: %w", label, err)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		*removed = append(*removed, path)
	}
	return nil
}
