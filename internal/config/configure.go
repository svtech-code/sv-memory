package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// TargetTool represents an editor or CLI tool compatible with sv-memory.
type TargetTool struct {
	Name      string
	Type      string // "editor" | "cli"
	ID        string // "cursor" | "vscode" | "zed" | "windsurf" | "claude-code" | "opencode" | "codex" | "antigravity"
	Auto      bool   // True if we can configure it automatically, False if manual instructions are needed
	ConfigPath string
}

// resolveConfigPath dynamically locates configuration files based on multiple candidate paths.
func resolveConfigPath(toolID string) string {
	home, _ := os.UserHomeDir()
	var candidates []string

	switch toolID {
	case "zed":
		candidates = []string{
			filepath.Join(home, ".config", "zed", "settings.json"),
			filepath.Join(home, "Library", "Application Support", "Zed", "settings.json"),
		}
	case "opencode":
		candidates = []string{
			filepath.Join(home, ".config", "opencode", "opencode.json"),
		}
	case "antigravity":
		candidates = []string{
			filepath.Join(home, ".gemini", "config", "mcp_config.json"),
			filepath.Join(home, ".gemini", "antigravity-cli", "mcp_config.json"),
			filepath.Join(home, ".config", "antigravity-cli", "mcp_config.json"),
			filepath.Join(home, ".antigravity-cli", "mcp_config.json"),
		}
	case "codex":
		candidates = []string{
			filepath.Join(home, ".codex", "config.toml"),
		}
	default:
		return ""
	}

	// 1. Return the first file candidate that already exists
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	// 2. Return path where the parent directory exists
	for _, path := range candidates {
		dir := filepath.Dir(path)
		if _, err := os.Stat(dir); err == nil {
			return path
		}
	}

	// 3. Fallback to default first candidate path
	if len(candidates) > 0 {
		return candidates[0]
	}
	return ""
}

// GetPredefinedEditors returns the manual + auto list of editors.
func GetPredefinedEditors() []TargetTool {
	return []TargetTool{
		{Name: "Cursor", Type: "editor", ID: "cursor", Auto: false},
		{Name: "VS Code", Type: "editor", ID: "vscode", Auto: false},
		{Name: "Zed Editor", Type: "editor", ID: "zed", Auto: true, ConfigPath: resolveConfigPath("zed")},
		{Name: "Windsurf", Type: "editor", ID: "windsurf", Auto: false},
	}
}

// GetPredefinedCLIs returns the manual + auto list of CLIs.
func GetPredefinedCLIs() []TargetTool {
	return []TargetTool{
		{Name: "Claude Code", Type: "cli", ID: "claude-code", Auto: true},
		{Name: "OpenCode", Type: "cli", ID: "opencode", Auto: true, ConfigPath: resolveConfigPath("opencode")},
		{Name: "Codex", Type: "cli", ID: "codex", Auto: true, ConfigPath: resolveConfigPath("codex")},
		{Name: "Antigravity CLI (agy)", Type: "cli", ID: "antigravity", Auto: true, ConfigPath: resolveConfigPath("antigravity")},
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

	case "antigravity":
		err := configureClaude(tool.ConfigPath, execPath)
		if err != nil {
			return false, "", err
		}
		return true, "Configurado automáticamente en " + tool.ConfigPath, nil

	case "opencode":
		err := configureOpenCode(tool.ConfigPath, execPath)
		if err != nil {
			return false, "", err
		}
		return true, "Configurado automáticamente en " + tool.ConfigPath, nil

	case "codex":
		err := configureCodex(tool.ConfigPath, execPath)
		if err != nil {
			return false, "", err
		}
		return true, "Configurado automáticamente en " + tool.ConfigPath, nil

	case "claude-code":
		// Check if 'claude' command is available in PATH
		_, lookErr := exec.LookPath("claude")
		if lookErr != nil {
			// Not installed globally or not in PATH, output manual fallback instructions
			msg := fmt.Sprintf("Claude Code no se detectó en tu PATH. Agrégalo manualmente ejecutando:\n      claude mcp add sv-memory --scope user --transport stdio -- %s mcp", execPath)
			return false, msg, nil
		}

		// Run the command
		cmd := exec.Command("claude", "mcp", "add", "sv-memory", "--scope", "user", "--transport", "stdio", "--", execPath, "mcp")
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

func configureOpenCode(configPath string, execPath string) error {
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

	mcpRaw, exists := data["mcp"]
	var mcp map[string]interface{}
	if exists {
		if val, ok := mcpRaw.(map[string]interface{}); ok {
			mcp = val
		}
	}
	if mcp == nil {
		mcp = make(map[string]interface{})
	}

	mcp["sv-memory"] = map[string]interface{}{
		"type":    "local",
		"command": []string{execPath, "mcp"},
		"enabled": true,
	}

	data["mcp"] = mcp

	newData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, newData, 0644)
}

// ShowBanner prints the SV Tech styled ASCII logo and version details.
func ShowBanner() {
	cHex := "\x1b[38;2;0;176;194m"
	reset := "\x1b[39m"

	logo := []string{
		"  ███████╗██╗   ██╗    ████████╗███████╗ ██████╗██╗  ██╗",
		"  ██╔════╝██║   ██║    ╚══██╔══╝██╔════╝██╔════╝██║  ██║",
		"  ███████╗██║   ██║       ██║   █████╗  ██║     ███████║",
		"  ╚════██║╚██╗ ██╔╝       ██║   ██╔══╝  ██║     ██╔══██║",
		"  ███████║ ╚████╔╝        ██║   ███████╗╚██████╗██║  ██║",
		"  ╚══════╝  ╚═══╝         ╚═╝   ╚══════╝ ╚═════╝╚═╝  ╚═╝",
	}

	boxW := 66
	contentW := boxW - 2

	topEdge := "╔" + strings.Repeat("═", contentW) + "╗"
	bottomEdge := "╚" + strings.Repeat("═", contentW) + "╝"
	emptyLine := "║" + strings.Repeat(" ", contentW) + "║"

	fmt.Println(cHex + topEdge + reset)
	fmt.Println(cHex + emptyLine + reset)

	for _, line := range logo {
		fmt.Printf("%s║  %s      ║%s\n", cHex, line, reset)
	}

	fmt.Println(cHex + emptyLine + reset)

	// Center lines
	printCenter := func(text string) {
		pad := contentW - len(text)
		left := pad / 2
		right := pad - left
		fmt.Printf("%s║%s%s%s║%s\n", cHex, strings.Repeat(" ", left), text, strings.Repeat(" ", right), reset)
	}

	printCenter("Sv Memory  v1.0.0")
	printCenter("Context Memory & Code Graph Builder")
	printCenter("Prevent context amnesia in your workspace")

	fmt.Println(cHex + emptyLine + reset)
	fmt.Println(cHex + bottomEdge + reset)
	fmt.Println()
}

func configureCodex(configPath string, execPath string) error {
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	var content string
	if _, err := os.Stat(configPath); err == nil {
		b, err := os.ReadFile(configPath)
		if err != nil {
			return err
		}
		content = string(b)
	}

	// Check if [mcp_servers.sv-memory] block already exists
	re := regexp.MustCompile(`(?s)\[mcp_servers\.sv-memory\].*?(\n\s*\n|\n*$)`)
	newBlock := fmt.Sprintf("[mcp_servers.sv-memory]\ncommand = %q\nargs = [\"mcp\"]", execPath)

	if re.MatchString(content) {
		content = re.ReplaceAllString(content, newBlock+"\n")
	} else {
		if len(content) > 0 && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += "\n" + newBlock + "\n"
	}

	return os.WriteFile(configPath, []byte(content), 0644)
}
