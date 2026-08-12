package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/svtech-code/sv-memory/internal/config"
	"github.com/svtech-code/sv-memory/internal/hook"
	"github.com/svtech-code/sv-memory/internal/mcp"
	"github.com/svtech-code/sv-memory/internal/perm"
	"github.com/svtech-code/sv-memory/internal/protocol"
)

// setupAgents lists every agent the unified `setup` command supports, matching
// Engram's `engram setup <agent>` integration surface.
var setupAgents = []string{"claude-code", "opencode", "cursor", "windsurf", "antigravity", "codex"}

// setupAgentWiring wires a single agent end-to-end: MCP config, hooks/skills +
// native plugin files, protocol injection, and MCP tool permissions.
func setupAgentWiring(agent string, strict bool) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	execPath, err := os.Executable()
	if err != nil {
		return err
	}
	execPath = filepath.Clean(execPath)

	mode := hook.ModeSoft
	if strict {
		mode = hook.ModeStrict
	}

	switch agent {
	case "claude-code":
		return setupClaudeCode(cwd, execPath, mode)
	case "opencode":
		return setupOpenCode(cwd, execPath, mode)
	case "cursor":
		return setupCursor(cwd, execPath)
	case "windsurf":
		return setupWindsurf(cwd, execPath)
	case "antigravity":
		return setupAntigravity(cwd, execPath, mode)
	case "codex":
		return setupCodex(cwd, execPath, mode)
	default:
		return fmt.Errorf("unsupported agent %q (supported: %v)", agent, setupAgents)
	}
}

func setupClaudeCode(cwd, execPath string, mode hook.Mode) error {
	// MCP config: `claude mcp add` when the CLI is available; otherwise write a
	// project-local .mcp.json so the server is still registered.
	if !commandAvailable("claude") {
		if err := writeProjectMCPJSON(cwd, execPath); err != nil {
			return err
		}
		fmt.Println("✅ claude-code: wrote project .mcp.json (claude CLI not found on PATH).")
	} else {
		fmt.Printf("ℹ️  claude-code: register the MCP server with:\n      claude mcp add sv-memory --scope user --transport stdio -- %s mcp\n", execPath)
	}

	// Hooks: PreToolUse + lifecycle (SessionStart, SessionEnd, PreCompact,
	// SubagentStop) written to .claude/hooks/ and registered in settings.json.
	eng := hook.New(cwd, mode)
	results := eng.Install([]hook.Platform{hook.PlatformClaudeCode})
	if err := reportHookResults("claude-code", results); err != nil {
		return err
	}

	if err := injectProtocol(cwd); err != nil {
		return err
	}

	if err := grantAllPerms(perm.PlatformClaudeCode); err != nil {
		return err
	}

	fmt.Println("✅ claude-code setup complete. Restart Claude Code to activate.")
	return nil
}

func setupOpenCode(cwd, execPath string, mode hook.Mode) error {
	// MCP config via opencode.json (resolve the auto-config path from the
	// predefined CLI list so the file lands in ~/.config/opencode/).
	tool := config.TargetTool{Name: "OpenCode", Type: "cli", ID: "opencode", Auto: true, ConfigPath: predefinedCLI("opencode")}
	if isAuto, msg, err := config.ConfigureTargetTool(tool); err != nil {
		return fmt.Errorf("opencode MCP config failed: %w", err)
	} else if isAuto {
		fmt.Printf("✅ opencode MCP: %s\n", msg)
	}

	// Skill + native TS plugin + protocol injection.
	eng := hook.New(cwd, mode)
	results := eng.Install([]hook.Platform{hook.PlatformOpenCode})
	if err := reportHookResults("opencode", results); err != nil {
		return err
	}

	fmt.Println("✅ opencode setup complete. Restart OpenCode to activate.")
	return nil
}

func setupCursor(cwd, execPath string) error {
	path, err := config.ConfigureCursor(cwd, execPath)
	if err != nil {
		return fmt.Errorf("cursor MCP config failed: %w", err)
	}
	fmt.Printf("✅ cursor: wrote %s\n", path)
	if err := injectProtocol(cwd); err != nil {
		return err
	}
	fmt.Println("✅ cursor setup complete. Restart Cursor to activate.")
	return nil
}

func setupWindsurf(cwd, execPath string) error {
	path, err := config.ConfigureWindsurf(cwd, execPath)
	if err != nil {
		return fmt.Errorf("windsurf MCP config failed: %w", err)
	}
	fmt.Printf("✅ windsurf: wrote %s\n", path)
	if err := injectProtocol(cwd); err != nil {
		return err
	}
	fmt.Println("✅ windsurf setup complete. Restart Windsurf to activate.")
	return nil
}

// predefinedCLI returns the auto-config path for the given tool ID, or "".
func predefinedCLI(id string) string {
	for _, t := range config.GetPredefinedCLIs() {
		if t.ID == id {
			return t.ConfigPath
		}
	}
	return ""
}

func setupAntigravity(cwd, execPath string, mode hook.Mode) error {
	tool := config.TargetTool{Name: "Antigravity CLI (agy)", Type: "cli", ID: "antigravity", Auto: true, ConfigPath: predefinedCLI("antigravity")}
	if isAuto, msg, err := config.ConfigureTargetTool(tool); err != nil {
		return fmt.Errorf("antigravity MCP config failed: %w", err)
	} else if isAuto {
		fmt.Printf("✅ antigravity MCP: %s\n", msg)
	}

	eng := hook.New(cwd, mode)
	results := eng.Install([]hook.Platform{hook.PlatformAntigravity})
	if err := reportHookResults("antigravity", results); err != nil {
		return err
	}

	if err := injectProtocol(cwd); err != nil {
		return err
	}

	if err := grantAllPerms(perm.PlatformAntigravity); err != nil {
		return err
	}

	fmt.Println("✅ antigravity setup complete. Restart Antigravity CLI to activate.")
	return nil
}

func setupCodex(cwd, execPath string, mode hook.Mode) error {
	tool := config.TargetTool{Name: "Codex", Type: "cli", ID: "codex", Auto: true, ConfigPath: predefinedCLI("codex")}
	if isAuto, msg, err := config.ConfigureTargetTool(tool); err != nil {
		return fmt.Errorf("codex MCP config failed: %w", err)
	} else if isAuto {
		fmt.Printf("✅ codex MCP: %s\n", msg)
	}

	eng := hook.New(cwd, mode)
	results := eng.Install([]hook.Platform{hook.PlatformCodex})
	if err := reportHookResults("codex", results); err != nil {
		return err
	}

	if err := injectProtocol(cwd); err != nil {
		return err
	}

	fmt.Println("✅ codex setup complete. Restart Codex to activate.")
	return nil
}

// setupStatus prints the installation status for every supported agent.
func setupStatus() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	fmt.Println("=== sv-memory Setup Status ===")
	fmt.Println()

	statuses := []struct {
		name      string
		installed bool
	}{
		{"claude-code", statusClaudeCode(cwd)},
		{"opencode", statusOpenCode(cwd)},
		{"cursor", statusCursor(cwd)},
		{"windsurf", statusWindsurf(cwd)},
		{"antigravity", statusAntigravity(cwd)},
		{"codex", statusCodex(cwd)},
	}
	for _, s := range statuses {
		if s.installed {
			fmt.Printf("  ✅ %-14s installed\n", s.name)
		} else {
			fmt.Printf("  ❌ %-14s not installed\n", s.name)
		}
	}
	return nil
}

func statusClaudeCode(cwd string) bool {
	return hook.New(cwd, hook.ModeSoft).Status([]hook.Platform{hook.PlatformClaudeCode})[hook.PlatformClaudeCode]
}

func statusOpenCode(cwd string) bool {
	return hook.New(cwd, hook.ModeSoft).Status([]hook.Platform{hook.PlatformOpenCode})[hook.PlatformOpenCode]
}

func statusCursor(cwd string) bool {
	_, err := os.Stat(filepath.Join(cwd, ".cursor", "mcp.json"))
	return err == nil
}

func statusWindsurf(cwd string) bool {
	_, err := os.Stat(filepath.Join(cwd, ".windsurf", "mcp_config.json"))
	return err == nil
}

func statusAntigravity(cwd string) bool {
	return hook.New(cwd, hook.ModeSoft).Status([]hook.Platform{hook.PlatformAntigravity})[hook.PlatformAntigravity]
}

func statusCodex(cwd string) bool {
	return hook.New(cwd, hook.ModeSoft).Status([]hook.Platform{hook.PlatformCodex})[hook.PlatformCodex]
}

var setupCmd = &cobra.Command{
	Use:   "setup [agent]",
	Short: "Wire sv-memory into an AI assistant (MCP config + hooks/plugins + permissions)",
	Long: `Wire sv-memory into an AI assistant (MCP config + hooks/plugins + permissions).
One-shot agent integration: MCP server config + hooks/skills/plugins + protocol
injection (AGENTS.md) + MCP tool permissions. Run without arguments to show the
setup status for every supported agent. Use 'setup <agent>' to install a single
agent or 'setup --all' for every agent.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		strict, _ := cmd.Flags().GetBool("strict")
		all, _ := cmd.Flags().GetBool("all")

		if len(args) == 0 && !all {
			return setupStatus()
		}

		agents := setupAgents
		if len(args) == 1 {
			agents = []string{args[0]}
		}

		anyErr := false
		for _, a := range agents {
			if err := setupAgentWiring(a, strict); err != nil {
				fmt.Printf("❌ %s: %v\n", a, err)
				anyErr = true
				continue
			}
		}
		if anyErr {
			return fmt.Errorf("one or more agent setups failed")
		}
		return nil
	},
}

func init() {
	setupCmd.Flags().Bool("strict", false, "Install strict hooks (block first raw read on Antigravity)")
	setupCmd.Flags().Bool("all", false, "Install for every supported agent")
}

func reportHookResults(agent string, results []hook.InstallResult) error {
	for _, r := range results {
		if r.Err != nil {
			return fmt.Errorf("%s hooks failed: %w", agent, r.Err)
		}
		for _, f := range r.Files {
			fmt.Printf("✅ %s: %s\n", agent, f)
		}
	}
	return nil
}

func injectProtocol(projPath string) error {
	injected, err := protocol.InjectProtocol(projPath)
	if err != nil {
		return fmt.Errorf("protocol injection failed: %w", err)
	}
	for _, f := range injected {
		fmt.Printf("✅ protocol rules injected into: %s\n", f)
	}
	return nil
}

func grantAllPerms(p perm.Platform) error {
	names := make([]string, 0, len(mcp.AllTools))
	for _, t := range mcp.AllTools {
		names = append(names, t.Name)
	}
	sort.Strings(names)

	res, err := perm.Grant(p, names, false)
	if err != nil {
		return fmt.Errorf("permission grant failed: %w", err)
	}
	if res.Skipped {
		fmt.Printf("ℹ️  %s: %s\n", p, res.SkippedMsg)
		return nil
	}
	fmt.Printf("✅ %s: %d tool permission(s) granted (%d already present)\n", p, len(res.Added), len(res.Present))
	return nil
}

// writeProjectMCPJSON writes a project-local .mcp.json (Claude Code reads it
// from the project root) merging any existing servers.
func writeProjectMCPJSON(projPath, execPath string) error {
	return config.WriteJSONMCPServers(filepath.Join(projPath, ".mcp.json"), execPath)
}

func commandAvailable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
