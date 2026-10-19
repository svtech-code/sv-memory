package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// packageSource concatenates the non-test Go sources of the current package so
// guard tests can scan tool registrations/descriptions regardless of which file
// a given split places them in.
func packageSource(t *testing.T) string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("failed to glob package sources: %v", err)
	}
	var sb strings.Builder
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, rerr := os.ReadFile(f)
		if rerr != nil {
			t.Fatalf("failed to read %s: %v", f, rerr)
		}
		sb.Write(b)
		sb.WriteByte('\n')
	}
	return sb.String()
}

func TestDebounceTimerRace(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	server := NewServer(pool, cfg)
	saveTool := server.GetTool("sv_mem_save")
	ctx := context.Background()

	// Concurrently save memories to trigger debounce scheduleSync in parallel.
	// Run with `go test -race` to verify safety.
	var wg sync.WaitGroup
	workers := 10
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func(id int) {
			defer wg.Done()
			req := mcpgo.CallToolRequest{}
			req.Params.Name = "sv_mem_save"
			req.Params.Arguments = map[string]any{
				"category": "journal",
				"what":     fmt.Sprintf("Concurrent task save %d", id),
				"why":      "Testing thread safety",
				"learned":  "Debounce logic should be concurrent safe",
			}
			_, _ = saveTool.Handler(ctx, req)
		}(i)
	}

	wg.Wait()
	// Let debounce timer fire and clean up
	time.Sleep(600 * time.Millisecond)
}

// TestAllToolsMatchesRegisteredTools is the guard test for the single source
// of truth: every tool registered via NewTool/AddTool MUST have a matching
// entry in AllTools (used by the permission manager). It scans the package
// sources to collect the registered names instead of relying on a server
// instance, keeping the test lightweight and independent of DB setup.
func TestAllToolsMatchesRegisteredTools(t *testing.T) {
	src := packageSource(t)

	registered := map[string]bool{}
	for _, line := range strings.Split(src, "\n") {
		idx := strings.Index(line, `mcp.NewTool("`)
		if idx == -1 {
			continue
		}
		rest := line[idx+len(`mcp.NewTool("`):]
		end := strings.Index(rest, `"`)
		if end <= 0 {
			continue
		}
		name := rest[:end]
		if strings.HasPrefix(name, "sv_") || strings.HasPrefix(name, "sv_graph_") {
			registered[name] = true
		}
	}

	if len(registered) == 0 {
		t.Fatal("no registered tools found in package sources — guard test is broken")
	}

	allTools := map[string]bool{}
	seen := map[string]bool{}
	for _, tool := range AllTools {
		if seen[tool.Name] {
			t.Errorf("duplicate entry in AllTools: %s", tool.Name)
		}
		seen[tool.Name] = true
		if tool.Description == "" {
			t.Errorf("AllTools entry %s has an empty description", tool.Name)
		}
		allTools[tool.Name] = true
	}

	for name := range registered {
		if !allTools[name] {
			t.Errorf("registered tool %s is missing from AllTools — add it to keep permissions in sync", name)
		}
	}
	for name := range allTools {
		if !registered[name] {
			t.Errorf("AllTools entry %s is not registered in NewServer — remove it or register it", name)
		}
	}
}

// TestDefaultServerExposesOnlyCoreTools guards the token-economy tool surface:
// the default server (no SV_MEMORY_FULL_TOOLS) must register every Core tool
// and skip the non-core maintenance/admin tools, keeping the per-request tool
// surface advertised to agents small.
func TestDefaultServerExposesOnlyCoreTools(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	srv := NewServer(pool, cfg)
	for _, tool := range AllTools {
		got := srv.GetTool(tool.Name) != nil
		if !tool.Hidden && !got {
			t.Errorf("core tool %s not registered on default server", tool.Name)
		}
		if tool.Hidden && got {
			t.Errorf("hidden tool %s should not be registered on default server (set SV_MEMORY_FULL_TOOLS to enable)", tool.Name)
		}
	}
}

// TestFullToolsModeRegistersAllTools verifies the opt-in escape hatch: with
// SV_MEMORY_FULL_TOOLS=1 every tool in AllTools is registered.
func TestFullToolsModeRegistersAllTools(t *testing.T) {
	t.Setenv("SV_MEMORY_FULL_TOOLS", "1")
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	srv := NewServer(pool, cfg)
	for _, tool := range AllTools {
		if srv.GetTool(tool.Name) == nil {
			t.Errorf("tool %s not registered in full mode", tool.Name)
		}
	}
}

// TestToolDescriptionBudget guards the per-request token surface: the combined
// length of LLM-visible tool descriptions must stay under 9500 chars so the
// tool list does not crowd out model context on every request.
func TestToolDescriptionBudget(t *testing.T) {
	src := packageSource(t)
	re := regexp.MustCompile(`mcp\.WithDescription\("((?:[^"\\]|\\.)*)"\)`)
	total := 0
	for _, m := range re.FindAllStringSubmatch(src, -1) {
		total += len(m[1])
	}
	if total >= 10000 {
		t.Errorf("tool description budget exceeded: %d chars (max 10000) — trim verbose descriptions", total)
	}
}

// TestServerInstructionsGuardsProtocolGuidance verifies that sv-memory's MCP
// initialize response carries server-wide instructions (the memory-first /
// graph-first / save-after-decision / spec-cycle protocol) so spec-compliant
// clients (notably OpenAI Codex) guide their models without per-platform
// plugins or hooks.
func TestServerInstructionsGuardsProtocolGuidance(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	srv := NewServer(pool, cfg)

	raw, err := json.Marshal(mcpgo.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mcpgo.NewRequestId(int64(1)),
		Request: mcpgo.Request{Method: "initialize"},
	})
	if err != nil {
		t.Fatalf("failed to marshal initialize request: %v", err)
	}

	msg := srv.HandleMessage(context.Background(), raw)
	resp, ok := msg.(mcpgo.JSONRPCResponse)
	if !ok {
		t.Fatalf("expected JSONRPCResponse, got %T", msg)
	}

	initResult, ok := resp.Result.(mcpgo.InitializeResult)
	if !ok {
		t.Fatalf("expected InitializeResult, got %T", resp.Result)
	}

	for _, marker := range []string{"sv_mem_search", "sv_graph_explore", "sv_mem_save", "sv_spec_list", "sv_commit_spec", "sv_* suffix"} {
		if !strings.Contains(initResult.Instructions, marker) {
			t.Errorf("initialize instructions missing marker %q", marker)
		}
	}
}
