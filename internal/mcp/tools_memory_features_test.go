package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/svtech-code/sv-memory/internal/memory"
)

func TestPinUnpinTools(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	srv := NewServer(pool, cfg)
	ctx := context.Background()

	saved, err := memory.SaveMemory(pool.Writer, &memory.Memory{
		ID: "pin-tool-1", ProjectID: cfg.ProjectID, Category: "decision",
		What: "Key decision to pin", Why: "important", Learned: "keep visible", CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("seed save failed: %v", err)
	}

	call := func(name string, args map[string]any) string {
		req := mcpgo.CallToolRequest{}
		req.Params.Name = name
		req.Params.Arguments = args
		res, callErr := srv.GetTool(name).Handler(ctx, req)
		if callErr != nil {
			t.Fatalf("%s failed: %v", name, callErr)
		}
		var out strings.Builder
		for _, c := range res.Content {
			out.WriteString(textContent(c))
		}
		return out.String()
	}

	out := call("sv_mem_pin", map[string]any{"id": saved.ID})
	if !strings.Contains(out, "pinned") {
		t.Fatalf("expected pin confirmation, got %q", out)
	}
	pinned, err := memory.SearchPinnedMemories(pool.Reader, cfg.ProjectID, 10)
	if err != nil {
		t.Fatalf("pinned search failed: %v", err)
	}
	if len(pinned) != 1 || pinned[0].ID != saved.ID {
		t.Fatalf("expected 1 pinned memory, got %d", len(pinned))
	}

	out = call("sv_mem_pin", map[string]any{"id": saved.ID, "action": "unpin"})
	if !strings.Contains(out, "unpinned") {
		t.Fatalf("expected unpin confirmation, got %q", out)
	}
	pinned, err = memory.SearchPinnedMemories(pool.Reader, cfg.ProjectID, 10)
	if err != nil {
		t.Fatalf("pinned search failed: %v", err)
	}
	if len(pinned) != 0 {
		t.Fatalf("expected 0 pinned after unpin, got %d", len(pinned))
	}
}

func TestSearchMatchModeHandler(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	for _, m := range []*memory.Memory{
		{ID: "mm-h-1", Category: "decision", What: "auth uses JWT", Why: "security", Learned: "x"},
		{ID: "mm-h-2", Category: "decision", What: "cache uses redis", Why: "perf", Learned: "x"},
	} {
		m.ProjectID = cfg.ProjectID
		m.CreatedAt = time.Now()
		if _, err := memory.SaveMemory(pool.Writer, m); err != nil {
			t.Fatalf("seed failed: %v", err)
		}
	}

	srv := NewServer(pool, cfg)
	ctx := context.Background()
	call := func(mode string) string {
		req := mcpgo.CallToolRequest{}
		req.Params.Name = "sv_mem_search"
		req.Params.Arguments = map[string]any{"query": "auth redis", "match_mode": mode, "limit": "10"}
		res, err := srv.GetTool("sv_mem_search").Handler(ctx, req)
		if err != nil {
			t.Fatalf("search failed: %v", err)
		}
		var out strings.Builder
		for _, c := range res.Content {
			out.WriteString(textContent(c))
		}
		return out.String()
	}

	allMode := call("all")
	anyMode := call("any")
	if strings.Contains(allMode, "mm-h-2") && strings.Contains(allMode, "mm-h-1") {
		t.Log("note: all-mode matched both in this small corpus")
	}
	if !strings.Contains(anyMode, "mm-h-1") && !strings.Contains(anyMode, "mm-h-2") {
		t.Fatalf("any-mode should return at least one result, got:\n%s", anyMode)
	}
}

func TestConfiguredCharsFallback(t *testing.T) {
	// With no config key set, the compiled-in default is returned so existing
	// truncation behavior is preserved when viper has no value.
	if got := configuredInt("no_such_config_key_anywhere", 123); got != 123 {
		t.Errorf("expected fallback 123, got %d", got)
	}
}

func TestSessionTokenLedger(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	if _, err := memory.SaveMemory(pool.Writer, &memory.Memory{
		ProjectID: cfg.ProjectID,
		Category:  "decision",
		What:      "Use Postgres",
		Why:       strings.Repeat("reasoning for choosing Postgres ", 40),
		Learned:   "Relational data",
	}); err != nil {
		t.Fatalf("failed saving memory: %v", err)
	}

	srv := NewServer(pool, cfg)
	ctx := context.Background()
	call := func(name string, args map[string]any) string {
		t.Helper()
		req := mcpgo.CallToolRequest{}
		req.Params.Name = name
		req.Params.Arguments = args
		res, err := srv.GetTool(name).Handler(ctx, req)
		if err != nil {
			t.Fatalf("%s failed: %v", name, err)
		}
		return textContent(res.Content[0])
	}
	// parseLedger extracts the reported "Estimated tokens injected this session"
	// value from a sv_mem_stats response.
	parseLedger := func(out string) int {
		t.Helper()
		marker := "Estimated tokens injected this session:"
		idx := strings.Index(out, marker)
		if idx < 0 {
			t.Fatalf("missing ledger line in stats:\n%s", out)
		}
		// The rendered line is markdown-bolded: "**...:** 112". Strip any
		// leading "*" so only the digits remain.
		rest := strings.TrimSpace(out[idx+len(marker):])
		rest = strings.Trim(rest, "* ")
		n := 0
		for _, c := range rest {
			if c < '0' || c > '9' {
				break
			}
			n = n*10 + int(c-'0')
		}
		return n
	}

	// Session start resets the ledger, then the Auto-Boot bundle is counted.
	call("sv_mem_session_start", map[string]any{"goal": "ledger test"})
	afterStart := parseLedger(call("sv_mem_stats", map[string]any{}))
	if afterStart <= 0 {
		t.Fatalf("expected Auto-Boot bundle to accrue tokens, got %d", afterStart)
	}

	// A bulk read adds more to the ledger.
	call("sv_mem_search", map[string]any{"query": "Postgres"})
	afterSearch := parseLedger(call("sv_mem_stats", map[string]any{}))
	if afterSearch <= afterStart {
		t.Fatalf("expected search to accrue tokens (start=%d search=%d)", afterStart, afterSearch)
	}
}

func TestSessionEnd_IdempotenceAndNotFound(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	srv := NewServer(pool, cfg)
	ctx := context.Background()

	// 1. Start session
	startReq := mcpgo.CallToolRequest{}
	startReq.Params.Name = "sv_mem_session_start"
	startReq.Params.Arguments = map[string]any{"goal": "Test session idempotence"}
	res, err := srv.GetTool("sv_mem_session_start").Handler(ctx, startReq)
	if err != nil || res.IsError {
		t.Fatalf("sv_mem_session_start failed: err=%v, res=%v", err, res)
	}
	startText := textContent(res.Content[0])
	parts := strings.Split(startText, "ID: ")
	if len(parts) < 2 {
		t.Fatalf("could not extract session ID: %s", startText)
	}
	sessionID := strings.Split(parts[1], ")")[0]

	// 2. First session end on active session -> success
	endReq := mcpgo.CallToolRequest{}
	endReq.Params.Name = "sv_mem_session_end"
	endReq.Params.Arguments = map[string]any{
		"session_id":   sessionID,
		"accomplished": "Finished task A",
	}
	res, err = srv.GetTool("sv_mem_session_end").Handler(ctx, endReq)
	if err != nil {
		t.Fatalf("sv_mem_session_end first call returned error: %v", err)
	}
	if res.IsError {
		t.Fatalf("sv_mem_session_end first call flagged isError: %v", textContent(res.Content[0]))
	}
	endText := textContent(res.Content[0])
	if !strings.Contains(endText, "ended successfully") {
		t.Errorf("expected 'ended successfully' in response, got: %s", endText)
	}

	// 3. Second session end on already completed session -> success (idempotent, isError=false)
	endReq2 := mcpgo.CallToolRequest{}
	endReq2.Params.Name = "sv_mem_session_end"
	endReq2.Params.Arguments = map[string]any{
		"session_id": sessionID,
	}
	res, err = srv.GetTool("sv_mem_session_end").Handler(ctx, endReq2)
	if err != nil {
		t.Fatalf("sv_mem_session_end 2nd call returned error: %v", err)
	}
	if res.IsError {
		t.Fatalf("sv_mem_session_end 2nd call flagged isError: %v", textContent(res.Content[0]))
	}
	endText2 := textContent(res.Content[0])
	if !strings.Contains(endText2, "already completed") {
		t.Errorf("expected 'already completed' in idempotent response, got: %s", endText2)
	}

	// 4. Session end on non-existent session ID -> error (isError=true)
	endReqBad := mcpgo.CallToolRequest{}
	endReqBad.Params.Name = "sv_mem_session_end"
	endReqBad.Params.Arguments = map[string]any{
		"session_id": "non-existent-session-id",
	}
	res, err = srv.GetTool("sv_mem_session_end").Handler(ctx, endReqBad)
	if err != nil {
		t.Fatalf("sv_mem_session_end bad ID returned transport error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected isError=true for non-existent session ID, got success: %v", textContent(res.Content[0]))
	}
	badText := textContent(res.Content[0])
	if !strings.Contains(badText, "session not found") {
		t.Errorf("expected 'session not found' in bad session response, got: %s", badText)
	}
}

func TestCompactTool_AutoAndCheckOnly(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	srv := NewServer(pool, cfg)
	ctx := context.Background()

	// 1. Check-only on clean project -> healthy
	checkReq := mcpgo.CallToolRequest{}
	checkReq.Params.Name = "sv_mem_compact"
	checkReq.Params.Arguments = map[string]any{"check_only": "true"}
	res, err := srv.GetTool("sv_mem_compact").Handler(ctx, checkReq)
	if err != nil || res.IsError {
		t.Fatalf("sv_mem_compact check_only failed: err=%v, res=%v", err, res)
	}
	text := textContent(res.Content[0])
	if !strings.Contains(text, "healthy") {
		t.Errorf("expected 'healthy' in check_only response, got: %s", text)
	}

	// 2. Seed 2 memories under same topic_key
	seedReq := func(id, topic string) {
		r := mcpgo.CallToolRequest{}
		r.Params.Name = "sv_mem_save"
		r.Params.Arguments = map[string]any{
			"category":  "standard",
			"what":      "Rule for " + id,
			"why":       "Reason for " + id,
			"learned":   "Learned " + id,
			"topic_key": topic,
		}
		if _, saveErr := srv.GetTool("sv_mem_save").Handler(ctx, r); saveErr != nil {
			t.Fatalf("seed failed: %v", saveErr)
		}
	}
	seedReq("m1", "standard/naming")
	// Insert second row with same topic_key directly to simulate multi-row fragmentation
	_, err = pool.Writer.Exec(`
		INSERT INTO memories (id, project_id, category, what, why, learned, topic_key, revision_count, created_at)
		VALUES ('m2-dup', ?, 'standard', 'Updated naming rule', 'Better clarity', 'Clarity matters', 'standard/naming', 2, datetime('now'))
	`, cfg.ProjectID)
	if err != nil {
		t.Fatalf("direct insert failed: %v", err)
	}

	// 3. Auto mode with threshold=2 (only 1 fragmented topic -> below threshold, should not compact)
	autoReq := mcpgo.CallToolRequest{}
	autoReq.Params.Name = "sv_mem_compact"
	autoReq.Params.Arguments = map[string]any{
		"auto":      "true",
		"threshold": "2",
	}
	res, err = srv.GetTool("sv_mem_compact").Handler(ctx, autoReq)
	if err != nil || res.IsError {
		t.Fatalf("auto compact below threshold failed: err=%v, res=%v", err, res)
	}
	text = textContent(res.Content[0])
	if !strings.Contains(text, "below threshold") {
		t.Errorf("expected 'below threshold' in auto compact, got: %s", text)
	}

	// 4. Auto mode with threshold=1 -> triggers auto-compaction
	autoReq1 := mcpgo.CallToolRequest{}
	autoReq1.Params.Name = "sv_mem_compact"
	autoReq1.Params.Arguments = map[string]any{
		"auto":      "true",
		"threshold": "1",
	}
	res, err = srv.GetTool("sv_mem_compact").Handler(ctx, autoReq1)
	if err != nil || res.IsError {
		t.Fatalf("auto compact at threshold failed: err=%v, res=%v", err, res)
	}
	text = textContent(res.Content[0])
	if !strings.Contains(text, "Auto-compaction complete") {
		t.Errorf("expected 'Auto-compaction complete', got: %s", text)
	}
}
