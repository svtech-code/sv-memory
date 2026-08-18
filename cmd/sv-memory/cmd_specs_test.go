package main

import (
	"strings"
	"testing"

	"github.com/svtech-code/sv-memory/internal/memory"
)

func TestSpecsSubcommandsRegistered(t *testing.T) {
	specsCmd, _, err := rootCmd.Find([]string{"specs"})
	if err != nil {
		t.Fatalf("specs subcommand not found: %v", err)
	}

	cmdNames := make(map[string]bool)
	for _, cmd := range specsCmd.Commands() {
		cmdNames[cmd.Name()] = true
	}

	expected := []string{"export", "import", "list", "archive"}
	for _, name := range expected {
		if !cmdNames[name] {
			t.Errorf("expected specs subcommand %q to be registered", name)
		}
	}
}

func TestSpecsListTitleTruncation(t *testing.T) {
	if got := memory.TruncateText("short", 60); got != "short" {
		t.Errorf("expected short title unchanged, got %q", got)
	}
	long := strings.Repeat("x", 100)
	got := memory.TruncateText(long, 10)
	if len([]rune(got)) >= len([]rune(long)) {
		t.Errorf("expected truncated title, got %q (runes %d)", got, len([]rune(got)))
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("expected truncation notice, got %q", got)
	}
}

func TestSpecsListTitleTruncationRuneSafe(t *testing.T) {
	// Multi-byte runes must not split UTF-8 sequences.
	long := strings.Repeat("é", 50)
	got := memory.TruncateText(long, 10)
	if len([]rune(got)) >= len([]rune(long)) {
		t.Errorf("expected rune-safe truncation, got %q (runes %d)", got, len([]rune(got)))
	}
}
