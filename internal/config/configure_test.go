package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestStripComments(t *testing.T) {
	input := `{
		// This is a comment
		"key": "value", /* block comment */
		"other": "val"
	}`

	cleaned := stripComments(input)
	var data map[string]interface{}
	err := json.Unmarshal([]byte(cleaned), &data)
	if err != nil {
		t.Fatalf("failed to parse cleaned JSON: %v", err)
	}

	if data["key"] != "value" {
		t.Errorf("expected key to be value, got %v", data["key"])
	}
	if data["other"] != "val" {
		t.Errorf("expected other to be val, got %v", data["other"])
	}
}

func TestConfigureClaude(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-claude-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	configPath := filepath.Join(tempDir, "claude_desktop_config.json")
	execPath := "/usr/local/bin/sv-memory"

	// Test 1: File does not exist (should create it)
	err = configureClaude(configPath, execPath)
	if err != nil {
		t.Fatalf("expected configureClaude to succeed on empty file: %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read created config: %v", err)
	}

	var data map[string]interface{}
	_ = json.Unmarshal(content, &data)

	mcpServers, exists := data["mcpServers"].(map[string]interface{})
	if !exists {
		t.Fatal("expected mcpServers key to exist in config")
	}

	svMem, exists := mcpServers["sv-memory"].(map[string]interface{})
	if !exists {
		t.Fatal("expected sv-memory server entry to exist")
	}

	if svMem["command"] != execPath {
		t.Errorf("expected command path to be %s, got %v", execPath, svMem["command"])
	}

	// Test 2: File exists with other config (should preserve and merge)
	existingContent := `{
		"mcpServers": {
			"other-server": {
				"command": "node",
				"args": ["other.js"]
			}
		}
	}`
	err = os.WriteFile(configPath, []byte(existingContent), 0644)
	if err != nil {
		t.Fatalf("failed writing mockup config: %v", err)
	}

	err = configureClaude(configPath, execPath)
	if err != nil {
		t.Fatalf("expected configureClaude to succeed: %v", err)
	}

	content, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}

	var mergedData map[string]interface{}
	_ = json.Unmarshal(content, &mergedData)

	mergedMCPServers := mergedData["mcpServers"].(map[string]interface{})
	if len(mergedMCPServers) != 2 {
		t.Errorf("expected 2 servers registered, got %d", len(mergedMCPServers))
	}

	if _, exists := mergedMCPServers["other-server"]; !exists {
		t.Error("expected original 'other-server' configuration to be preserved")
	}
	if _, exists := mergedMCPServers["sv-memory"]; !exists {
		t.Error("expected 'sv-memory' configuration to be injected")
	}
}

func TestConfigureCodex(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-codex-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	configPath := filepath.Join(tempDir, "config.toml")
	execPath := "/usr/local/bin/sv-memory"

	// Test 1: File does not exist
	err = configureCodex(configPath, execPath)
	if err != nil {
		t.Fatalf("expected configureCodex to succeed on empty file: %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read created config: %v", err)
	}

	expectedStr := "[mcp_servers.sv-memory]\ncommand = \"/usr/local/bin/sv-memory\"\nargs = [\"mcp\"]\n"
	if string(content) != "\n"+expectedStr {
		t.Errorf("expected:\n%q\ngot:\n%q", "\n"+expectedStr, string(content))
	}

	// Test 2: File exists (should update block)
	existingContent := `[other_block]
option = true

[mcp_servers.sv-memory]
command = "/old/path"
args = ["mcp"]
`
	err = os.WriteFile(configPath, []byte(existingContent), 0644)
	if err != nil {
		t.Fatalf("failed writing mockup config: %v", err)
	}

	err = configureCodex(configPath, execPath)
	if err != nil {
		t.Fatalf("expected configureCodex to succeed: %v", err)
	}

	content, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}

	expectedUpdated := `[other_block]
option = true

[mcp_servers.sv-memory]
command = "/usr/local/bin/sv-memory"
args = ["mcp"]
`
	if string(content) != expectedUpdated {
		t.Errorf("expected:\n%q\ngot:\n%q", expectedUpdated, string(content))
	}
}
