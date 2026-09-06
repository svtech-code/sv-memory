package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/svtech-code/sv-memory/internal/config"
	"github.com/svtech-code/sv-memory/internal/db"
	"github.com/svtech-code/sv-memory/internal/memory"
)

func runCLIChdir(t *testing.T, dir string, args ...string) error {
	t.Helper()
	oldCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldCWD)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	resetInitFlags()
	rootCmd.SetArgs(args)
	return rootCmd.Execute()
}

func TestSessionSubcommandsRegistered(t *testing.T) {
	session, _, err := rootCmd.Find([]string{"session"})
	if err != nil {
		t.Fatalf("session command not found: %v", err)
	}
	names := map[string]bool{}
	for _, c := range session.Commands() {
		names[c.Name()] = true
	}
	for _, n := range []string{"start", "summary", "end", "active"} {
		if !names[n] {
			t.Errorf("expected session subcommand %q to be registered", n)
		}
	}

	capture, _, err := rootCmd.Find([]string{"capture"})
	if err != nil {
		t.Fatalf("capture command not found: %v", err)
	}
	capNames := map[string]bool{}
	for _, c := range capture.Commands() {
		capNames[c.Name()] = true
	}
	if !capNames["prompt"] {
		t.Errorf("expected capture subcommand %q to be registered", "prompt")
	}
}

// TestSessionLifecycleCLI exercises the full session lifecycle through the CLI:
// start -> capture prompt (auto-attached to the active session) -> summary ->
// end -> active becomes none. Verifies state through the memory store, matching
// the MCP handlers' behavior so agent hooks/plugins can drive the same flow.
func TestSessionLifecycleCLI(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tempDir, ".git"), 0755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	if err := runCLIChdir(t, tempDir, "init", "--skip-setup"); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Resolve the physical path the CLI will use: withProject() resolves the
	// project via os.Getwd(), which on macOS returns the /private/var form of a
	// t.TempDir() path. Use the same resolution so the project ID matches.
	physical := tempDir
	if old, gErr := os.Getwd(); gErr == nil {
		if err := os.Chdir(tempDir); err == nil {
			if p, wErr := os.Getwd(); wErr == nil {
				physical = p
			}
			_ = os.Chdir(old)
		}
	}

	cfg, err := config.LoadConfig(physical)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	database, err := db.InitDB(cfg.DBPath)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer database.Close()

	// session start
	if err = runCLIChdir(t, tempDir, "session", "start", "--goal", "cli lifecycle test"); err != nil {
		t.Fatalf("session start failed: %v", err)
	}
	active, err := memory.GetActiveSession(database, cfg.ProjectID)
	if err != nil || active == nil {
		t.Fatalf("expected an active session after start, err=%v active=%v", err, active)
	}

	// capture prompt auto-attaches to the active session
	if err = runCLIChdir(t, tempDir, "capture", "prompt", "hello from cli"); err != nil {
		t.Fatalf("capture prompt failed: %v", err)
	}
	prompts, err := memory.RecentPrompts(database, cfg.ProjectID, "", 10)
	if err != nil || len(prompts) != 1 || prompts[0].SessionID != active.ID {
		t.Fatalf("expected 1 prompt attached to active session, got %d (err=%v)", len(prompts), err)
	}

	// session summary
	if err = runCLIChdir(t, tempDir, "session", "summary", active.ID, "--accomplished", "done"); err != nil {
		t.Fatalf("session summary failed: %v", err)
	}

	// session end (auto-detects the active session)
	if err = runCLIChdir(t, tempDir, "session", "end", "--summary", "finished"); err != nil {
		t.Fatalf("session end failed: %v", err)
	}
	active2, err := memory.GetActiveSession(database, cfg.ProjectID)
	if err != nil || active2 != nil {
		t.Fatalf("expected no active session after end, err=%v active=%v", err, active2)
	}
}
