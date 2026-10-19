package mcp

import (
	"context"
	"strings"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/svtech-code/sv-memory/internal/memory"
)

func TestProposeSpecHandler(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	server := NewServer(pool, cfg)
	tool := server.GetTool("sv_propose_spec")
	if tool == nil {
		t.Fatal("sv_propose_spec tool not registered")
	}

	ctx := context.Background()
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_propose_spec"
	req.Params.Arguments = map[string]any{
		"slug":       "implement-session-auth",
		"title":      "Implement session-based auth",
		"what":       "Replace JWT stateless with server-side sessions",
		"where_path": "internal/auth/",
	}

	res, err := tool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("sv_propose_spec failed: %v", err)
	}
	if res.IsError {
		t.Fatalf("sv_propose_spec returned error: %v", res.Content)
	}
	text := textContent(res.Content[0])
	if !strings.Contains(text, "implement-session-auth") {
		t.Errorf("expected slug in response, got: %s", text)
	}
	if !strings.Contains(text, "Pre-flight") {
		t.Errorf("expected pre-flight section, got: %s", text)
	}

	// The change must be persisted and advanced to proposed status.
	changes, err := memory.ListChangesByStatus(pool.Reader, cfg.ProjectID, memory.ChangeStatusProposed)
	if err != nil {
		t.Fatalf("failed to list changes: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected 1 proposed change, got %d", len(changes))
	}
	if changes[0].Slug != "implement-session-auth" {
		t.Errorf("expected slug, got %s", changes[0].Slug)
	}
}

func TestProposeSpecHandlerValidation(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	server := NewServer(pool, cfg)
	tool := server.GetTool("sv_propose_spec")

	ctx := context.Background()
	// Missing slug.
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_propose_spec"
	req.Params.Arguments = map[string]any{"title": "Title"}
	res, err := tool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !res.IsError {
		t.Error("expected error for missing slug")
	}
}

func TestProposeSpecDuplicateSlug(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	server := NewServer(pool, cfg)
	tool := server.GetTool("sv_propose_spec")

	ctx := context.Background()
	mk := func() mcpgo.CallToolRequest {
		r := mcpgo.CallToolRequest{}
		r.Params.Name = "sv_propose_spec"
		r.Params.Arguments = map[string]any{"slug": "dup", "title": "T"}
		return r
	}
	if res, err := tool.Handler(ctx, mk()); err != nil {
		t.Fatalf("first propose failed: %v", err)
	} else if res.IsError {
		t.Fatalf("first propose errored: %v", res.Content)
	}
	res, err := tool.Handler(ctx, mk())
	if err != nil {
		t.Fatalf("second propose returned error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected error result for duplicate slug")
	}
	msg := textContent(res.Content[0])
	if !strings.Contains(msg, "already exists") {
		t.Errorf("expected duplicate slug message, got: %s", msg)
	}
}

func TestProposeSpecWithRequirements(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	server := NewServer(pool, cfg)
	tool := server.GetTool("sv_propose_spec")
	if tool == nil {
		t.Fatal("sv_propose_spec tool not registered")
	}

	ctx := context.Background()
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_propose_spec"
	req.Params.Arguments = map[string]any{
		"slug":            "add-2fa",
		"title":           "Add 2FA",
		"what":            "Require a second factor during login",
		"where_path":      "internal/auth/",
		"capability_path": "auth",
		"requirements": `## ADDED Requirements

### Requirement: Two-Factor Authentication
The system MUST require a TOTP second factor during login.

#### Scenario: OTP required
- **GIVEN** a user with 2FA enabled
- **WHEN** the user submits valid credentials
- **THEN** an OTP challenge is presented`,
	}

	res, err := tool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("sv_propose_spec failed: %v", err)
	}
	if res.IsError {
		t.Fatalf("sv_propose_spec errored: %v", res.Content)
	}
	text := textContent(res.Content[0])
	if !strings.Contains(text, "- **Capability:** `auth`") || !strings.Contains(text, "1 added") {
		t.Errorf("expected capability and requirement summary in response, got: %s", text)
	}

	changes, err := memory.ListChangesByStatus(pool.Reader, cfg.ProjectID, memory.ChangeStatusProposed)
	if err != nil || len(changes) != 1 {
		t.Fatalf("expected 1 proposed change, got %d (err=%v)", len(changes), err)
	}
	if changes[0].CapabilityPath != "auth" {
		t.Errorf("expected capability path auth, got %q", changes[0].CapabilityPath)
	}
	deltas, err := memory.LoadChangeDeltas(pool.Reader, cfg.ProjectID, changes[0].ID)
	if err != nil {
		t.Fatalf("failed to load deltas: %v", err)
	}
	if len(deltas) != 1 || len(deltas[0].Requirements) != 1 {
		t.Fatalf("expected 1 ADDED requirement, got %+v", deltas)
	}
	if deltas[0].Requirements[0].Name != "Two-Factor Authentication" {
		t.Errorf("expected requirement name, got %q", deltas[0].Requirements[0].Name)
	}
}
