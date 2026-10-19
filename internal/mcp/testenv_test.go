package mcp

import (
	"os"
	"path/filepath"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"

	"github.com/svtech-code/sv-memory/internal/config"
	"github.com/svtech-code/sv-memory/internal/db"
)

func setupTestEnv(t *testing.T) (string, *db.Pool, *config.Config) {
	tempDir, err := os.MkdirTemp("", "sv-mcp-test")
	if err != nil {
		t.Fatalf("failed to create temp workspace: %v", err)
	}

	err = os.MkdirAll(filepath.Join(tempDir, ".sv-memory"), 0755)
	if err != nil {
		t.Fatalf("failed to create .sv-memory: %v", err)
	}

	dbPath := filepath.Join(tempDir, "test_storage.db")
	pool, err := db.NewDBPool(dbPath)
	if err != nil {
		t.Fatalf("failed to create DBPool: %v", err)
	}

	cfg := &config.Config{
		DBPath:    dbPath,
		ProjectID: "mcp-test-proj-id",
		ProjName:  "MCP Test Project",
		ProjPath:  tempDir,
	}

	// Register project
	err = db.RegisterProject(pool.Writer, cfg.ProjectID, cfg.ProjName, cfg.ProjPath)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	return tempDir, pool, cfg
}

func cleanupTestEnv(tempDir string, pool *db.Pool) {
	pool.Close()
	os.RemoveAll(tempDir)
}

func writeMockCodeFiles(t *testing.T, tempDir string) {
	err := os.WriteFile(filepath.Join(tempDir, "index.js"), []byte(`
		import utils from './utils';
		import { test } from "./components/Button";
	`), 0644)
	if err != nil {
		t.Fatalf("failed to write index.js: %v", err)
	}

	err = os.WriteFile(filepath.Join(tempDir, "utils.js"), []byte(`
		export default {};
	`), 0644)
	if err != nil {
		t.Fatalf("failed to write utils.js: %v", err)
	}

	err = os.MkdirAll(filepath.Join(tempDir, "components"), 0755)
	if err != nil {
		t.Fatalf("failed to create components dir: %v", err)
	}

	err = os.WriteFile(filepath.Join(tempDir, "components", "Button.tsx"), []byte(`
		export const test = () => {};
	`), 0644)
	if err != nil {
		t.Fatalf("failed to write Button.tsx: %v", err)
	}
}

func textContent(c mcpgo.Content) string {
	switch v := c.(type) {
	case mcpgo.TextContent:
		return v.Text
	case *mcpgo.TextContent:
		return v.Text
	default:
		return ""
	}
}
