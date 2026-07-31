// Package perm manages MCP tool permission allow-lists per AI assistant
// platform. It writes the sv-memory tool surface (from mcp.AllTools) into the
// platform's configuration file so the agent can call the tools without
// prompting for approval on every invocation.
package perm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/svtech-code/sv-memory/internal/mcp"
)

// Platform identifies an AI assistant that consumes the sv-memory MCP server.
type Platform string

const (
	// PlatformAntigravity is Google's Antigravity CLI (agy). It supports a
	// static allow-list in ~/.gemini/antigravity-cli/settings.json using the
	// format "mcp(sv-memory/<tool>)".
	PlatformAntigravity Platform = "antigravity"
	// PlatformClaudeCode is Anthropic's Claude Code. It supports a static
	// allow-list in ~/.claude/settings.json using "mcp__sv-memory__<tool>".
	PlatformClaudeCode Platform = "claude-code"
	// PlatformOpenCode uses opencode.json. MCP tools are allowed by default;
	// the manager only verifies the server is registered and enabled.
	PlatformOpenCode Platform = "opencode"
	// PlatformCodex uses ~/.codex/config.toml. Tool approval is interactive,
	// so the manager reports status but cannot write a static allow-list.
	PlatformCodex Platform = "codex"
)

// SupportedPlatforms lists every platform the permission manager understands.
var SupportedPlatforms = []Platform{PlatformAntigravity, PlatformClaudeCode, PlatformOpenCode, PlatformCodex}

// platformInfo carries per-platform metadata used for grant/revoke/status.
type platformInfo struct {
	ID          string
	Name        string
	AllowListed bool
	GrantFormat func(tool string) string
	Prefix      string
}

var platformInfos = map[Platform]platformInfo{
	PlatformAntigravity: {
		ID:          "antigravity",
		Name:        "Antigravity CLI (agy)",
		AllowListed: true,
		GrantFormat: func(tool string) string { return "mcp(sv-memory/" + tool + ")" },
		Prefix:      "mcp(sv-memory/",
	},
	PlatformClaudeCode: {
		ID:          "claude-code",
		Name:        "Claude Code",
		AllowListed: true,
		GrantFormat: func(tool string) string { return "mcp__sv-memory__" + tool },
		Prefix:      "mcp__sv-memory__",
	},
	PlatformOpenCode: {
		ID:          "opencode",
		Name:        "OpenCode",
		AllowListed: false,
	},
	PlatformCodex: {
		ID:          "codex",
		Name:        "Codex",
		AllowListed: false,
	},
}

// Info describes a platform for display purposes.
type Info struct {
	ID          string
	Name        string
	AllowListed bool
	ConfigPath  string
}

// Infos returns display metadata for all supported platforms.
func Infos() []Info {
	var out []Info
	for _, p := range SupportedPlatforms {
		out = append(out, Info{
			ID:          string(p),
			Name:        platformInfos[p].Name,
			AllowListed: platformInfos[p].AllowListed,
			ConfigPath:  resolveSettingsPath(p),
		})
	}
	return out
}

// resolveSettingsPath locates the platform's configuration file that holds the
// permission allow-list. Returns "" for platforms without a file-based list.
func resolveSettingsPath(p Platform) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	var candidates []string
	switch p {
	case PlatformAntigravity:
		candidates = []string{
			filepath.Join(home, ".gemini", "antigravity-cli", "settings.json"),
			filepath.Join(home, ".config", "antigravity-cli", "settings.json"),
			filepath.Join(home, ".antigravity-cli", "settings.json"),
		}
	case PlatformClaudeCode:
		candidates = []string{
			filepath.Join(home, ".claude", "settings.json"),
		}
	default:
		return ""
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	for _, path := range candidates {
		dir := filepath.Dir(path)
		if _, err := os.Stat(dir); err == nil {
			return path
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return ""
}

// Result describes what a Grant or Revoke operation changed.
type Result struct {
	Platform   Platform
	ConfigPath string
	Added      []string
	Removed    []string
	Present    []string
	Skipped    bool
	SkippedMsg string
	DryRun     bool
}

// StatusReport describes the current permission state for a platform.
type StatusReport struct {
	Platform   Platform
	Name       string
	AllowListed bool
	ConfigPath string
	Configured bool
	Granted    []string
	Missing    []string
	Message    string
}

// Grant writes allow-list entries for the given tools into the platform's
// config. If tools is empty, every tool in mcp.AllTools is granted. Merge
// semantics: existing allow entries (including unrelated ones like
// "command(npm run)") are preserved. dryRun reports the change without writing.
func Grant(p Platform, tools []string, dryRun bool) (*Result, error) {
	info := platformInfos[p]
	res := &Result{Platform: p, DryRun: dryRun}

	if !info.AllowListed {
		res.Skipped = true
		res.SkippedMsg = fmt.Sprintf("%s uses interactive approval and has no static allow-list to grant.", info.Name)
		return res, nil
	}

	configPath := resolveSettingsPath(p)
	if configPath == "" {
		return nil, fmt.Errorf("could not resolve configuration path for platform %q", p)
	}
	res.ConfigPath = configPath

	if len(tools) == 0 {
		for _, t := range mcp.AllTools {
			tools = append(tools, t.Name)
		}
	}
	sort.Strings(tools)

	data, err := readSettings(configPath)
	if err != nil {
		return nil, err
	}
	allow := getAllowList(data)

	want := map[string]string{}
	for _, t := range tools {
		want[info.GrantFormat(t)] = t
	}
	existing := map[string]bool{}
	for _, entry := range allow {
		existing[entry] = true
	}

	for entry, tool := range want {
		if existing[entry] {
			res.Present = append(res.Present, tool)
			continue
		}
		res.Added = append(res.Added, tool)
		allow = append(allow, entry)
	}
	sort.Strings(res.Added)
	sort.Strings(res.Present)

	if len(res.Added) == 0 {
		return res, nil
	}

	sort.Strings(allow)
	setAllowList(data, allow)
	if dryRun {
		return res, nil
	}
	if err := writeSettings(configPath, data); err != nil {
		return nil, err
	}
	return res, nil
}

// Revoke removes every sv-memory allow-list entry from the platform's config,
// preserving unrelated entries. dryRun reports the change without writing.
func Revoke(p Platform, dryRun bool) (*Result, error) {
	info := platformInfos[p]
	res := &Result{Platform: p, DryRun: dryRun}

	if !info.AllowListed {
		res.Skipped = true
		res.SkippedMsg = fmt.Sprintf("%s has no static allow-list to revoke.", info.Name)
		return res, nil
	}

	configPath := resolveSettingsPath(p)
	if configPath == "" {
		return nil, fmt.Errorf("could not resolve configuration path for platform %q", p)
	}
	res.ConfigPath = configPath

	data, err := readSettings(configPath)
	if err != nil {
		return nil, err
	}
	allow := getAllowList(data)

	var kept []string
	for _, entry := range allow {
		if strings.HasPrefix(entry, info.Prefix) {
			res.Removed = append(res.Removed, platformToolName(p, entry))
			continue
		}
		kept = append(kept, entry)
	}
	sort.Strings(res.Removed)

	if len(res.Removed) == 0 {
		return res, nil
	}
	if dryRun {
		return res, nil
	}
	setAllowList(data, kept)
	if err := writeSettings(configPath, data); err != nil {
		return nil, err
	}
	return res, nil
}

// Status reports which sv-memory tools are currently granted for a platform
// and which are missing from the allow-list.
func Status(p Platform) (*StatusReport, error) {
	info := platformInfos[p]
	report := &StatusReport{
		Platform:    p,
		Name:        info.Name,
		AllowListed: info.AllowListed,
		ConfigPath:  resolveSettingsPath(p),
	}

	if !info.AllowListed {
		if p == PlatformOpenCode {
			report.Message = "MCP tools are allowed by default in OpenCode; no allow-list is needed. Ensure the sv-memory server is registered and enabled in opencode.json."
		} else {
			report.Message = "Tool approval is interactive for this platform; there is no static allow-list to inspect."
		}
		return report, nil
	}

	if report.ConfigPath == "" {
		report.Message = "Configuration file not found."
		return report, nil
	}
	if _, err := os.Stat(report.ConfigPath); os.IsNotExist(err) {
		report.Message = "Configuration file does not exist yet."
		return report, nil
	}
	report.Configured = true

	data, err := readSettings(report.ConfigPath)
	if err != nil {
		return nil, err
	}
	allow := getAllowList(data)
	granted := map[string]bool{}
	for _, entry := range allow {
		if strings.HasPrefix(entry, info.Prefix) {
			granted[platformToolName(p, entry)] = true
		}
	}

	for _, t := range mcp.AllTools {
		if granted[t.Name] {
			report.Granted = append(report.Granted, t.Name)
		} else {
			report.Missing = append(report.Missing, t.Name)
		}
	}
	sort.Strings(report.Granted)
	sort.Strings(report.Missing)
	return report, nil
}

// platformToolName extracts the bare tool name from a granted allow entry,
// stripping the platform-specific prefix and any trailing suffix (the closing
// paren in agy's "mcp(sv-memory/<tool>)" format).
func platformToolName(p Platform, entry string) string {
	name := strings.TrimPrefix(entry, platformInfos[p].Prefix)
	if p == PlatformAntigravity {
		name = strings.TrimSuffix(name, ")")
	}
	return name
}

// readSettings reads a JSON settings file into a generic map, creating an
// empty structure when the file does not exist yet.
func readSettings(path string) (map[string]interface{}, error) {
	data := map[string]interface{}{}
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return data, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		return data, nil
	}
	if err := json.Unmarshal(content, &data); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	return data, nil
}

// writeSettings writes the settings map as indented JSON, creating parent dirs.
func writeSettings(path string, data map[string]interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0644)
}

// getAllowList extracts the permissions.allow slice from the settings map,
// normalizing []string and []interface{} representations.
func getAllowList(data map[string]interface{}) []string {
	permsRaw, ok := data["permissions"]
	if !ok {
		return nil
	}
	perms, ok := permsRaw.(map[string]interface{})
	if !ok {
		return nil
	}
	allowRaw, ok := perms["allow"]
	if !ok {
		return nil
	}
	switch v := allowRaw.(type) {
	case []string:
		return v
	case []interface{}:
		var out []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// setAllowList writes the allow slice into the settings map, creating the
// permissions section if absent.
func setAllowList(data map[string]interface{}, allow []string) {
	var perms map[string]interface{}
	if raw, ok := data["permissions"].(map[string]interface{}); ok {
		perms = raw
	} else {
		perms = map[string]interface{}{}
		data["permissions"] = perms
	}
	perms["allow"] = allow
}
