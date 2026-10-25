package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/svtech-code/sv-memory/internal/graph"
	"github.com/svtech-code/sv-memory/internal/memory"
)

func TestSessionStartIncludesGraphHubs(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	// Write two Go files with an import edge so the graph has a real hub.
	if err := os.WriteFile(filepath.Join(tempDir, "main.go"),
		[]byte(`package main; import "./utils"; func main() { helper() }`), 0644); err != nil {
		t.Fatalf("failed writing main.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "utils.go"),
		[]byte(`package main; func helper() {}`), 0644); err != nil {
		t.Fatalf("failed writing utils.go: %v", err)
	}
	if err := graph.SyncGraph(pool.Writer, cfg.ProjectID, cfg.ProjPath); err != nil {
		t.Fatalf("failed syncing graph: %v", err)
	}

	srv := NewServer(pool, cfg)
	ctx := context.Background()
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_mem_session_start"
	req.Params.Arguments = map[string]any{"goal": "session with graph hubs"}
	res, err := srv.GetTool("sv_mem_session_start").Handler(ctx, req)
	if err != nil {
		t.Fatalf("session_start failed: %v", err)
	}
	out := textContent(res.Content[0])
	if !strings.Contains(out, "Session started") {
		t.Fatalf("expected started session, got:\n%s", out)
	}
	if !strings.Contains(out, "Graph Hubs") {
		t.Fatalf("expected Graph Hubs section in session_start bundle, got:\n%s", out)
	}
	if !strings.Contains(out, "main.go") && !strings.Contains(out, "utils.go") {
		t.Fatalf("expected a hub node to be listed, got:\n%s", out)
	}
}

func TestSessionStartHonorsTokenBudget(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	// Seed a decision with a long rationale so the Auto-Boot bundle has
	// enough content to exceed a tiny token budget.
	if _, err := memory.SaveMemory(pool.Writer, &memory.Memory{
		ProjectID: cfg.ProjectID,
		Category:  "decision",
		What:      "Use Postgres",
		Why:       strings.Repeat("very long rationale that would make the Auto-Boot bundle huge ", 20),
		Learned:   "Keep relational data in Postgres",
	}); err != nil {
		t.Fatalf("failed to seed memory: %v", err)
	}

	srv := NewServer(pool, cfg)
	ctx := context.Background()
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_mem_session_start"
	req.Params.Arguments = map[string]any{"token_budget": "1"}
	res, err := srv.GetTool("sv_mem_session_start").Handler(ctx, req)
	if err != nil {
		t.Fatalf("session_start failed: %v", err)
	}
	out := textContent(res.Content[0])
	if !strings.Contains(out, "[!] Response truncated to ~1 tokens") {
		t.Fatalf("expected session_start bundle to be truncated by token_budget, got:\n%s", out)
	}
}

func TestSessionStartAutoClosesStaleSessions(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	// Create a stale session (started 2 hours ago).
	stale, err := memory.StartSession(pool.Writer, cfg.ProjectID, "stale session", "/tmp")
	if err != nil {
		t.Fatalf("StartSession error: %v", err)
	}
	pool.Writer.Exec("UPDATE sessions SET started_at = ? WHERE id = ?", time.Now().Add(-2*time.Hour), stale.ID)

	// Verify stale session exists.
	var beforeCount int
	pool.Reader.QueryRow("SELECT count(*) FROM sessions WHERE project_id = ? AND status = 'active'", cfg.ProjectID).Scan(&beforeCount)
	if beforeCount != 1 {
		t.Fatalf("expected 1 active session before start, got %d", beforeCount)
	}

	// Set stale_session_hours to 1 so the 2-hour-old session is stale.
	cfg.Settings.StaleSessionHours = 1

	// Start a new session — should auto-close the stale one.
	srv := NewServer(pool, cfg)
	ctx := context.Background()
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_mem_session_start"
	req.Params.Arguments = map[string]any{}
	res, err := srv.GetTool("sv_mem_session_start").Handler(ctx, req)
	if err != nil {
		t.Fatalf("session_start failed: %v", err)
	}
	out := textContent(res.Content[0])
	if !strings.Contains(out, "Session started") {
		t.Fatalf("expected started session, got:\n%s", out)
	}

	// Verify stale session is now completed.
	var staleStatus string
	pool.Reader.QueryRow("SELECT status FROM sessions WHERE id = ?", stale.ID).Scan(&staleStatus)
	if staleStatus != "completed" {
		t.Fatalf("expected stale session to be completed, got %s", staleStatus)
	}
}

func TestLazySessionAutoBootAndReuse(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	if _, err := memory.SaveMemory(pool.Writer, &memory.Memory{
		ProjectID: cfg.ProjectID,
		Category:  "decision",
		What:      "Use Go for MCP server",
		Why:       "Single binary portability across all agents",
		Learned:   "Keep logic inside internal/mcp",
	}); err != nil {
		t.Fatalf("failed to seed memory: %v", err)
	}

	srv := NewServer(pool, cfg)
	ctx := context.Background()

	// 1. Model skips sv_mem_session_start and calls sv_mem_search directly.
	reqSearch := mcpgo.CallToolRequest{}
	reqSearch.Params.Name = "sv_mem_search"
	reqSearch.Params.Arguments = map[string]any{"query": "Go"}
	res1, err := srv.GetTool("sv_mem_search").Handler(ctx, reqSearch)
	if err != nil {
		t.Fatalf("sv_mem_search failed: %v", err)
	}
	out1 := textContent(res1.Content[0])
	if !strings.Contains(out1, "[sv-memory Auto-Boot]") {
		t.Fatalf("expected first tool call to include [sv-memory Auto-Boot] banner, got:\n%s", out1)
	}

	active, err := memory.GetActiveSession(pool.Reader, cfg.ProjectID)
	if err != nil || active == nil {
		t.Fatalf("expected active session to be auto-created in SQLite, err=%v", err)
	}

	// 2. Subsequent tool call in the same server instance does NOT repeat the banner.
	res2, err := srv.GetTool("sv_mem_search").Handler(ctx, reqSearch)
	if err != nil {
		t.Fatalf("second sv_mem_search failed: %v", err)
	}
	out2 := textContent(res2.Content[0])
	if strings.Contains(out2, "[sv-memory Auto-Boot]") {
		t.Fatalf("did not expect second tool call to repeat Auto-Boot banner, got:\n%s", out2)
	}

	// 3. If the agent then calls sv_mem_session_start, it reuses the lazy session ID.
	reqStart := mcpgo.CallToolRequest{}
	reqStart.Params.Name = "sv_mem_session_start"
	reqStart.Params.Arguments = map[string]any{"goal": "adopted goal"}
	resStart, err := srv.GetTool("sv_mem_session_start").Handler(ctx, reqStart)
	if err != nil {
		t.Fatalf("sv_mem_session_start failed: %v", err)
	}
	outStart := textContent(resStart.Content[0])
	if !strings.Contains(outStart, active.ID) {
		t.Fatalf("expected sv_mem_session_start to reuse lazy session %s, got:\n%s", active.ID, outStart)
	}

	var activeCount int
	pool.Reader.QueryRow("SELECT count(*) FROM sessions WHERE project_id = ? AND status = 'active'", cfg.ProjectID).Scan(&activeCount)
	if activeCount != 1 {
		t.Fatalf("expected exactly 1 active session (no duplicate), got %d", activeCount)
	}
}

func TestLazySessionAutoBootSkipsWhenSessionAlreadyActive(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	if _, err := memory.SaveMemory(pool.Writer, &memory.Memory{
		ProjectID: cfg.ProjectID,
		Category:  "standard",
		What:      "Hook-managed session",
		Why:       "PreInvocation hook started session before MCP call",
		Learned:   "Do not duplicate active session",
	}); err != nil {
		t.Fatalf("failed to seed memory: %v", err)
	}

	// Simulate a CLI hook (e.g. Antigravity PreInvocation / Claude Code SessionStart)
	// having already started an active session before the MCP server receives a tool call.
	hookSession, err := memory.StartSession(pool.Writer, cfg.ProjectID, "hook goal", tempDir)
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}

	srv := NewServer(pool, cfg)
	ctx := context.Background()
	req := mcpgo.CallToolRequest{}
	req.Params.Name = "sv_mem_search"
	req.Params.Arguments = map[string]any{"query": "Hook"}
	res, err := srv.GetTool("sv_mem_search").Handler(ctx, req)
	if err != nil {
		t.Fatalf("sv_mem_search failed: %v", err)
	}
	out := textContent(res.Content[0])
	if strings.Contains(out, "[sv-memory Auto-Boot]") {
		t.Fatalf("expected no Auto-Boot banner when session %s is already active, got:\n%s", hookSession.ID, out)
	}

	var activeCount int
	pool.Reader.QueryRow("SELECT count(*) FROM sessions WHERE project_id = ? AND status = 'active'", cfg.ProjectID).Scan(&activeCount)
	if activeCount != 1 {
		t.Fatalf("expected 1 active session, got %d", activeCount)
	}
}

func TestAdaptiveCrossToolChainingNudges(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	if err := os.WriteFile(filepath.Join(tempDir, "svc.go"),
		[]byte(`package main; func RunService() {}`), 0644); err != nil {
		t.Fatalf("failed writing svc.go: %v", err)
	}
	if err := graph.SyncGraph(pool.Writer, cfg.ProjectID, cfg.ProjPath); err != nil {
		t.Fatalf("failed syncing graph: %v", err)
	}
	if _, err := memory.SaveMemory(pool.Writer, &memory.Memory{
		ProjectID: cfg.ProjectID,
		Category:  "decision",
		What:      "Service runner pattern",
		Why:       "Keep entrypoint clean",
		Learned:   "Use RunService",
	}); err != nil {
		t.Fatalf("failed seeding memory: %v", err)
	}

	srv := NewServer(pool, cfg)
	ctx := context.Background()

	// 1. sv_mem_session_start nudges toward sv_mem_search and sv_graph_explore.
	reqStart := mcpgo.CallToolRequest{}
	reqStart.Params.Name = "sv_mem_session_start"
	resStart, err := srv.GetTool("sv_mem_session_start").Handler(ctx, reqStart)
	if err != nil {
		t.Fatalf("session_start failed: %v", err)
	}
	outStart := textContent(resStart.Content[0])
	if !strings.Contains(outStart, "sv_mem_search") || !strings.Contains(outStart, "sv_graph_explore") {
		t.Fatalf("expected session_start output to nudge sv_mem_search and sv_graph_explore, got:\n%s", outStart)
	}

	// 2. sv_graph_search before sv_mem_search nudges both sv_graph_explore and sv_mem_search.
	reqGSearch := mcpgo.CallToolRequest{}
	reqGSearch.Params.Name = "sv_graph_search"
	reqGSearch.Params.Arguments = map[string]any{"query": "RunService"}
	resGS, err := srv.GetTool("sv_graph_search").Handler(ctx, reqGSearch)
	if err != nil {
		t.Fatalf("sv_graph_search failed: %v", err)
	}
	outGS := textContent(resGS.Content[0])
	if !strings.Contains(outGS, "sv_graph_explore") || !strings.Contains(outGS, "sv_mem_search") {
		t.Fatalf("expected sv_graph_search to nudge sv_graph_explore and sv_mem_search, got:\n%s", outGS)
	}

	// 3. sv_mem_search before sv_graph_explore nudges sv_mem_get and sv_graph_explore.
	reqMSearch := mcpgo.CallToolRequest{}
	reqMSearch.Params.Name = "sv_mem_search"
	reqMSearch.Params.Arguments = map[string]any{"query": "Service"}
	resMS, err := srv.GetTool("sv_mem_search").Handler(ctx, reqMSearch)
	if err != nil {
		t.Fatalf("sv_mem_search failed: %v", err)
	}
	outMS := textContent(resMS.Content[0])
	if !strings.Contains(outMS, "sv_mem_get") || !strings.Contains(outMS, "sv_graph_explore") {
		t.Fatalf("expected sv_mem_search to nudge sv_mem_get and sv_graph_explore, got:\n%s", outMS)
	}

	// 4. sv_graph_explore after sv_mem_search nudges sv_mem_get and sv_propose_spec.
	reqExplore := mcpgo.CallToolRequest{}
	reqExplore.Params.Name = "sv_graph_explore"
	reqExplore.Params.Arguments = map[string]any{"path": "RunService"}
	resExp, err := srv.GetTool("sv_graph_explore").Handler(ctx, reqExplore)
	if err != nil {
		t.Fatalf("sv_graph_explore failed: %v", err)
	}
	outExp := textContent(resExp.Content[0])
	if !strings.Contains(outExp, "sv_mem_get") || !strings.Contains(outExp, "sv_propose_spec") {
		t.Fatalf("expected sv_graph_explore to nudge sv_mem_get and sv_propose_spec, got:\n%s", outExp)
	}
}

// callToolThroughServer invokes a tool through the MCPServer JSON-RPC path so
// server-wide tool handler middleware (lazy auto-boot) is exercised, unlike
// srv.GetTool(name).Handler which calls the raw handler directly.
func callToolThroughServer(t *testing.T, srv *mcpserver.MCPServer, name string, args map[string]any) *mcpgo.CallToolResult {
	t.Helper()
	raw, err := json.Marshal(mcpgo.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      mcpgo.NewRequestId(int64(1)),
		Request: mcpgo.Request{Method: "tools/call"},
		Params:  mcpgo.CallToolParams{Name: name, Arguments: args},
	})
	if err != nil {
		t.Fatalf("failed to marshal tools/call request: %v", err)
	}
	msg := srv.HandleMessage(context.Background(), raw)
	resp, ok := msg.(mcpgo.JSONRPCResponse)
	if !ok {
		t.Fatalf("expected JSONRPCResponse, got %T (%+v)", msg, msg)
	}
	result, ok := resp.Result.(*mcpgo.CallToolResult)
	if !ok {
		t.Fatalf("expected *CallToolResult, got %T", resp.Result)
	}
	if result.IsError {
		t.Fatalf("%s returned a tool error: %s", name, textContent(result.Content[0]))
	}
	return result
}

// TestLazySessionAutoBootOnRawTool verifies the server-wide middleware triggers
// lazy auto-boot even for handlers that bypass respond (raw text results).
func TestLazySessionAutoBootOnRawTool(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	srv := NewServer(pool, cfg)
	res := callToolThroughServer(t, srv, "sv_mem_capture_passive",
		map[string]any{"what": "first call is not a search", "why": "regression coverage"})
	out := textContent(res.Content[0])
	if !strings.Contains(out, "[sv-memory Auto-Boot]") {
		t.Fatalf("expected raw-result tool to include Auto-Boot banner, got:\n%s", out)
	}

	active, err := memory.GetActiveSession(pool.Reader, cfg.ProjectID)
	if err != nil || active == nil {
		t.Fatalf("expected an auto-created active session, err=%v", err)
	}

	second := callToolThroughServer(t, srv, "sv_mem_search", map[string]any{"query": "none"})
	if out := textContent(second.Content[0]); strings.Contains(out, "[sv-memory Auto-Boot]") {
		t.Fatalf("did not expect the Auto-Boot banner to repeat on a later tool call, got:\n%s", out)
	}
}

// TestLazySessionAdoptionAppliesGoalAndDirectory verifies that an explicit
// sv_mem_session_start adopts the lazily created session and persists the
// requested goal and directory on that same row.
func TestLazySessionAdoptionAppliesGoalAndDirectory(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	srv := NewServer(pool, cfg)
	callToolThroughServer(t, srv, "sv_mem_capture_passive",
		map[string]any{"what": "trigger lazy boot", "why": "adoption test"})

	active, err := memory.GetActiveSession(pool.Reader, cfg.ProjectID)
	if err != nil || active == nil {
		t.Fatalf("expected an auto-created active session, err=%v", err)
	}

	customDir := filepath.Join(tempDir, "custom-workdir")
	res := callToolThroughServer(t, srv, "sv_mem_session_start",
		map[string]any{"goal": "adopted goal", "directory": customDir})
	out := textContent(res.Content[0])
	if !strings.Contains(out, active.ID) {
		t.Fatalf("expected sv_mem_session_start to reuse lazy session %s, got:\n%s", active.ID, out)
	}

	var goal, dir string
	if scanErr := pool.Reader.QueryRow(
		"SELECT goal, directory FROM sessions WHERE id = ?", active.ID).Scan(&goal, &dir); scanErr != nil {
		t.Fatalf("failed reading adopted session: %v", scanErr)
	}
	if goal != "adopted goal" || dir != customDir {
		t.Fatalf("expected goal=%q directory=%q, got goal=%q directory=%q", "adopted goal", customDir, goal, dir)
	}

	var activeCount int
	if scanErr := pool.Reader.QueryRow(
		"SELECT count(*) FROM sessions WHERE project_id = ? AND status = 'active'", cfg.ProjectID).Scan(&activeCount); scanErr != nil {
		t.Fatalf("failed counting active sessions: %v", scanErr)
	}
	if activeCount != 1 {
		t.Fatalf("expected exactly 1 active session, got %d", activeCount)
	}
}
