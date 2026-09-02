package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootCommandCreated(t *testing.T) {
	if rootCmd == nil {
		t.Fatal("rootCmd is nil")
	}
	if rootCmd.Use != "sv-memory" {
		t.Errorf("expected Use 'sv-memory', got %q", rootCmd.Use)
	}
}

func TestSubcommandsRegistered(t *testing.T) {
	cmdNames := make(map[string]bool)
	for _, cmd := range rootCmd.Commands() {
		cmdNames[cmd.Name()] = true
	}

	expected := []string{
		"init", "mcp", "sync", "configure", "diagnose", "stats", "compact",
		"graph", "export", "import", "delete", "projects", "conflicts",
		"obsidian-export", "hooks", "context",
	}

	for _, name := range expected {
		if !cmdNames[name] {
			t.Errorf("expected subcommand %q to be registered on rootCmd", name)
		}
	}
}

func TestGraphSubcommandsRegistered(t *testing.T) {
	graphCmd, _, err := rootCmd.Find([]string{"graph"})
	if err != nil {
		t.Fatalf("graph subcommand not found: %v", err)
	}

	cmdNames := make(map[string]bool)
	for _, cmd := range graphCmd.Commands() {
		cmdNames[cmd.Name()] = true
	}

	expected := []string{"rebuild", "path", "explain", "communities", "wiki", "viz", "merge"}
	for _, name := range expected {
		if !cmdNames[name] {
			t.Errorf("expected graph subcommand %q to be registered", name)
		}
	}
}

func TestHelpOutput(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"--help"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("help execution failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "sv-memory") {
		t.Error("help output should contain 'sv-memory'")
	}
	if !strings.Contains(output, "init") {
		t.Error("help output should list 'init' subcommand")
	}
	if !strings.Contains(output, "mcp") {
		t.Error("help output should list 'mcp' subcommand")
	}
}

func TestCLINoArgs(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("root command with no args failed: %v", err)
	}
}

func TestEachCommandHelpSucceeds(t *testing.T) {
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "completion" || cmd.Name() == "help" {
			continue
		}
		t.Run(cmd.Name(), func(t *testing.T) {
			buf := new(bytes.Buffer)
			rootCmd.SetOut(buf)
			rootCmd.SetArgs([]string{cmd.Name(), "--help"})
			err := rootCmd.Execute()
			if err != nil {
				t.Fatalf("%s --help failed: %v", cmd.Name(), err)
			}
			output := buf.String()
			if !strings.Contains(output, cmd.Short) {
				t.Errorf("%s help text should contain its short description", cmd.Name())
			}
		})
	}
}

func TestEachGraphSubcommandHelpSucceeds(t *testing.T) {
	graphCmd, _, err := rootCmd.Find([]string{"graph"})
	if err != nil {
		t.Fatalf("graph subcommand not found: %v", err)
	}
	for _, sub := range graphCmd.Commands() {
		t.Run(sub.Name(), func(t *testing.T) {
			buf := new(bytes.Buffer)
			rootCmd.SetOut(buf)
			rootCmd.SetArgs([]string{"graph", sub.Name(), "--help"})
			err := rootCmd.Execute()
			if err != nil {
				t.Fatalf("graph %s --help failed: %v", sub.Name(), err)
			}
			output := buf.String()
			if !strings.Contains(output, sub.Short) {
				t.Errorf("graph %s help text should contain its short description", sub.Name())
			}
		})
	}
}
