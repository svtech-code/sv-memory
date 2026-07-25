package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
)

// TargetTool represents an editor or CLI tool compatible with sv-memory.
type TargetTool struct {
	Name      string
	Type      string // "editor" | "cli"
	ID        string // "cursor" | "vscode" | "zed" | "windsurf" | "claude-code" | "opencode" | "codex" | "antigravity"
	Auto      bool   // True if we can configure it automatically, False if manual instructions are needed
	ConfigPath string
}

// GetPredefinedEditors returns the manual + auto list of editors.
func GetPredefinedEditors() []TargetTool {
	home, _ := os.UserHomeDir()
	return []TargetTool{
		{Name: "Cursor", Type: "editor", ID: "cursor", Auto: false},
		{Name: "VS Code", Type: "editor", ID: "vscode", Auto: false},
		{Name: "Zed Editor", Type: "editor", ID: "zed", Auto: true, ConfigPath: filepath.Join(home, ".config", "zed", "settings.json")},
		{Name: "Windsurf", Type: "editor", ID: "windsurf", Auto: false},
	}
}

// GetPredefinedCLIs returns the manual + auto list of CLIs.
func GetPredefinedCLIs() []TargetTool {
	home, _ := os.UserHomeDir()
	return []TargetTool{
		{Name: "Claude Code", Type: "cli", ID: "claude-code", Auto: true},
		{Name: "OpenCode", Type: "cli", ID: "opencode", Auto: true, ConfigPath: filepath.Join(home, ".gemini", "antigravity-cli", "mcp_config.json")},
		{Name: "Codex", Type: "cli", ID: "codex", Auto: false},
		{Name: "Antigravity CLI (agy)", Type: "cli", ID: "antigravity", Auto: true, ConfigPath: filepath.Join(home, ".gemini", "antigravity-cli", "mcp_config.json")},
	}
}

// ConfigResult returns the details of a target configuration execution.
func ConfigureTargetTool(tool TargetTool) (bool, string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return false, "", fmt.Errorf("failed to detect current executable path: %w", err)
	}
	execPath = filepath.Clean(execPath)

	if !tool.Auto {
		// Return manual instructions
		switch tool.ID {
		case "cursor":
			msg := fmt.Sprintf("Abre Cursor -> Features -> MCP -> click en 'Add New MCPServer'.\n      * Name: sv-memory\n      * Type: command\n      * Command: %s mcp", execPath)
			return false, msg, nil
		case "vscode":
			msg := fmt.Sprintf("Abre la configuración del plugin de MCP que utilices (ej. Cline o Roo Code):\n      * Agrega un nuevo servidor con el Comando: %s\n      * Con los Argumentos: [\"mcp\"]", execPath)
			return false, msg, nil
		case "windsurf":
			msg := fmt.Sprintf("Abre los ajustes de Windsurf -> MCP -> click en 'Add New Server'.\n      * Name: sv-memory\n      * Type: command\n      * Command: %s mcp", execPath)
			return false, msg, nil
		case "codex":
			msg := fmt.Sprintf("Configura el plugin de MCP en Codex utilizando:\n      * Comando: %s\n      * Argumentos: [\"mcp\"]", execPath)
			return false, msg, nil
		default:
			return false, "Configuración manual requerida en tu editor.", nil
		}
	}

	// Automatic configurations
	switch tool.ID {
	case "zed":
		err := configureZed(tool.ConfigPath, execPath)
		if err != nil {
			return false, "", err
		}
		return true, "Configurado automáticamente en " + tool.ConfigPath, nil

	case "opencode", "antigravity":
		err := configureClaude(tool.ConfigPath, execPath)
		if err != nil {
			return false, "", err
		}
		return true, "Configurado automáticamente en " + tool.ConfigPath, nil

	case "claude-code":
		// Check if 'claude' command is available in PATH
		_, lookErr := exec.LookPath("claude")
		if lookErr != nil {
			// Not installed globally or not in PATH, output manual fallback instructions
			msg := fmt.Sprintf("Claude Code no se detectó en tu PATH. Agrégalo manualmente ejecutando:\n      claude mcp add sv-memory -- %s mcp", execPath)
			return false, msg, nil
		}

		// Run the command
		cmd := exec.Command("claude", "mcp", "add", "sv-memory", "--", execPath, "mcp")
		output, err := cmd.CombinedOutput()
		if err != nil {
			return false, "", fmt.Errorf("failed executing claude command (%s): %w", string(output), err)
		}
		return true, "Configurado automáticamente mediante comando 'claude mcp add'", nil
	}

	return false, "", fmt.Errorf("unknown auto-configuration ID: %s", tool.ID)
}

func configureClaude(configPath string, execPath string) error {
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	var data map[string]interface{}
	if _, err := os.Stat(configPath); err == nil {
		content, err := os.ReadFile(configPath)
		if err != nil {
			return err
		}
		_ = json.Unmarshal(content, &data)
	}

	if data == nil {
		data = make(map[string]interface{})
	}

	mcpServersRaw, exists := data["mcpServers"]
	var mcpServers map[string]interface{}
	if exists {
		if val, ok := mcpServersRaw.(map[string]interface{}); ok {
			mcpServers = val
		}
	}
	if mcpServers == nil {
		mcpServers = make(map[string]interface{})
	}

	mcpServers["sv-memory"] = map[string]interface{}{
		"command": execPath,
		"args":    []string{"mcp"},
	}

	data["mcpServers"] = mcpServers

	newData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, newData, 0644)
}

func configureZed(configPath string, execPath string) error {
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	var data map[string]interface{}
	if _, err := os.Stat(configPath); err == nil {
		content, err := os.ReadFile(configPath)
		if err != nil {
			return err
		}
		
		cleanContent := stripComments(string(content))
		_ = json.Unmarshal([]byte(cleanContent), &data)
	}

	if data == nil {
		data = make(map[string]interface{})
	}

	mcpServersRaw, exists := data["mcp_servers"]
	var mcpServers map[string]interface{}
	if exists {
		if val, ok := mcpServersRaw.(map[string]interface{}); ok {
			mcpServers = val
		}
	}
	if mcpServers == nil {
		mcpServers = make(map[string]interface{})
	}

	mcpServers["sv-memory"] = map[string]interface{}{
		"command": execPath,
		"args":    []string{"mcp"},
	}

	data["mcp_servers"] = mcpServers

	newData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, newData, 0644)
}

func stripComments(input string) string {
	reLine := regexp.MustCompile(`//.*`)
	input = reLine.ReplaceAllString(input, "")
	reBlock := regexp.MustCompile(`/\*.*?\*/`)
	input = reBlock.ReplaceAllString(input, "")
	return input
}
