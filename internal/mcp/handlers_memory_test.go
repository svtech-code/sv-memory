package mcp

import (
	"context"
	"fmt"
	"strings"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"

	"github.com/svtech-code/sv-memory/internal/memory"
)

func TestSaveMemoryHandler(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	server := NewServer(pool, cfg)
	saveTool := server.GetTool("sv_mem_save")
	if saveTool == nil {
		t.Fatal("sv_mem_save tool not registered")
	}

	ctx := context.Background()

	// 1. Basic Create Strategy
	req1 := mcpgo.CallToolRequest{}
	req1.Params.Name = "sv_mem_save"
	req1.Params.Arguments = map[string]any{
		"category": "architecture",
		"what":     "Use PostgreSQL for storing user data",
		"why":      "Requires relational queries and scaling",
		"learned":  "Relational data model fits user schema",
	}

	res1, err := saveTool.Handler(ctx, req1)
	if err != nil {
		t.Fatalf("sv_mem_save failed: %v", err)
	}
	if res1.IsError {
		t.Fatalf("sv_mem_save returned error: %v", res1.Content)
	}

	textResult := textContent(res1.Content[0])
	if !strings.Contains(textResult, "Successfully created memory") {
		t.Errorf("expected created message, got: %s", textResult)
	}

	// 2. Rolling Window Dedup Strategy
	// Saving identical content within 24 hours should increment duplicate count instead of creating a new row
	res2, err := saveTool.Handler(ctx, req1)
	if err != nil {
		t.Fatalf("sv_mem_save dedup call failed: %v", err)
	}
	textResult2 := textContent(res2.Content[0])
	if !strings.Contains(textResult2, "duplicate suppressed") {
		t.Errorf("expected duplicate suppressed message, got: %s", textResult2)
	}

	// 3. Topic Key Upsert Strategy
	req3 := mcpgo.CallToolRequest{}
	req3.Params.Name = "sv_mem_save"
	req3.Params.Arguments = map[string]any{
		"category":  "standard",
		"what":      "Code reviews require approval from 2 peers",
		"why":       "Maintains code quality",
		"learned":   "Double peer approval works best",
		"topic_key": "standard/code-reviews",
	}

	res3, err := saveTool.Handler(ctx, req3)
	if err != nil {
		t.Fatalf("sv_mem_save topic key call failed: %v", err)
	}
	textResult3 := textContent(res3.Content[0])
	if !strings.Contains(textResult3, "Successfully created memory") {
		t.Errorf("expected created message, got: %s", textResult3)
	}

	// Now modify and save with same topic key (should trigger update/upsert)
	req3Update := mcpgo.CallToolRequest{}
	req3Update.Params.Name = "sv_mem_save"
	req3Update.Params.Arguments = map[string]any{
		"category":  "standard",
		"what":      "Code reviews require approval from 1 peer only", // changed
		"why":       "Speeds up delivery",
		"learned":   "Single peer approval speeds up cycles",
		"topic_key": "standard/code-reviews",
	}

	res4, err := saveTool.Handler(ctx, req3Update)
	if err != nil {
		t.Fatalf("sv_mem_save topic key update failed: %v", err)
	}
	textResult4 := textContent(res4.Content[0])
	if !strings.Contains(textResult4, "updated existing topic_key (revision: 2)") {
		t.Errorf("expected updated message with revision 2, got: %s", textResult4)
	}
}

func TestReadAndCompactMemoryHandlers(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	server := NewServer(pool, cfg)
	ctx := context.Background()
	first, err := memory.SaveMemory(pool.Writer, &memory.Memory{
		ProjectID: cfg.ProjectID,
		Category:  "journal",
		What:      "First observation",
		Why:       "First reason",
		Learned:   "First lesson",
	})
	if err != nil {
		t.Fatalf("failed to seed first memory: %v", err)
	}
	_, err = memory.SaveMemory(pool.Writer, &memory.Memory{
		ProjectID: cfg.ProjectID,
		Category:  "decision",
		What:      "Second observation",
		Why:       "Second reason",
		Learned:   "Second lesson",
	})
	if err != nil {
		t.Fatalf("failed to seed second memory: %v", err)
	}

	getTool := server.GetTool("sv_mem_get")
	missingReq := mcpgo.CallToolRequest{}
	missingReq.Params.Name = "sv_mem_get"
	missingReq.Params.Arguments = map[string]any{"id": "missing"}
	missing, err := getTool.Handler(ctx, missingReq)
	if err != nil || !strings.Contains(textContent(missing.Content[0]), "not found") {
		t.Fatalf("missing memory response = %v, err=%v", missing, err)
	}
	foundReq := mcpgo.CallToolRequest{}
	foundReq.Params.Name = "sv_mem_get"
	foundReq.Params.Arguments = map[string]any{"id": first.ID, "max_chars": "4"}
	found, err := getTool.Handler(ctx, foundReq)
	if err != nil || !strings.Contains(textContent(found.Content[0]), first.What[:4]) {
		t.Fatalf("found memory response = %v, err=%v", found, err)
	}

	timelineTool := server.GetTool("sv_mem_timeline")
	timelineReq := mcpgo.CallToolRequest{}
	timelineReq.Params.Name = "sv_mem_timeline"
	timelineReq.Params.Arguments = map[string]any{"observation_id": first.ID}
	timeline, err := timelineTool.Handler(ctx, timelineReq)
	if err != nil || !strings.Contains(textContent(timeline.Content[0]), "Timeline around") {
		t.Fatalf("timeline response = %v, err=%v", timeline, err)
	}
	timelineText := textContent(timeline.Content[0])
	if !strings.Contains(timelineText, "Central observation") || !strings.Contains(timelineText, first.What) {
		t.Errorf("expected central observation with rationale in timeline, got: %s", timelineText)
	}

	compactTool := server.GetTool("sv_mem_compact")
	compactReq := mcpgo.CallToolRequest{}
	compactReq.Params.Name = "sv_mem_compact"
	compactReq.Params.Arguments = map[string]any{}
	compact, err := compactTool.Handler(ctx, compactReq)
	if err != nil || !strings.Contains(textContent(compact.Content[0]), "No duplicate") {
		t.Fatalf("compact response = %v, err=%v", compact, err)
	}
}

func TestSearchMemoryHandler(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	server := NewServer(pool, cfg)
	saveTool := server.GetTool("sv_mem_save")
	searchTool := server.GetTool("sv_mem_search")
	ctx := context.Background()

	// Pre-populate database with memories
	categories := []string{"bugfix", "architecture", "standard"}
	for i, cat := range categories {
		req := mcpgo.CallToolRequest{}
		req.Params.Name = "sv_mem_save"
		req.Params.Arguments = map[string]any{
			"category":  cat,
			"what":      fmt.Sprintf("Observation title for %s number %d", cat, i),
			"why":       "Reason for this observation",
			"learned":   "Key learning points",
			"topic_key": fmt.Sprintf("standard/observation-%d", i),
		}
		_, _ = saveTool.Handler(ctx, req)
	}

	// 1. Search by query
	reqSearch1 := mcpgo.CallToolRequest{}
	reqSearch1.Params.Name = "sv_mem_search"
	reqSearch1.Params.Arguments = map[string]any{
		"query": "Observation",
	}

	resSearch1, err := searchTool.Handler(ctx, reqSearch1)
	if err != nil {
		t.Fatalf("sv_mem_search failed: %v", err)
	}
	textResult1 := textContent(resSearch1.Content[0])
	if !strings.Contains(textResult1, "BUGFIX") || !strings.Contains(textResult1, "ARCHITECTURE") || !strings.Contains(textResult1, "STANDARD") {
		t.Errorf("expected search results to find matching entries, got: %s", textResult1)
	}
	if !strings.Contains(textResult1, "Top result (expanded)") || !strings.Contains(textResult1, "Reason for this observation") {
		t.Errorf("expected expanded top result with rationale, got: %s", textResult1)
	}
	if !strings.Contains(textResult1, "*Topic:*") || !strings.Contains(textResult1, "standard/observation") {
		t.Errorf("expected expanded top result to include topic_key, got: %s", textResult1)
	}

	// 2. Search with Category filter
	reqSearch2 := mcpgo.CallToolRequest{}
	reqSearch2.Params.Name = "sv_mem_search"
	reqSearch2.Params.Arguments = map[string]any{
		"query":    "Observation",
		"category": "bugfix",
	}

	resSearch2, err := searchTool.Handler(ctx, reqSearch2)
	if err != nil {
		t.Fatalf("sv_mem_search with filter failed: %v", err)
	}
	textResult2 := textContent(resSearch2.Content[0])
	if !strings.Contains(textResult2, "BUGFIX") {
		t.Errorf("expected results to contain BUGFIX, got: %s", textResult2)
	}
	if strings.Contains(textResult2, "ARCHITECTURE") {
		t.Errorf("expected results NOT to contain ARCHITECTURE, got: %s", textResult2)
	}

	// 3. Search with limit and offset
	reqSearch3 := mcpgo.CallToolRequest{}
	reqSearch3.Params.Name = "sv_mem_search"
	reqSearch3.Params.Arguments = map[string]any{
		"query":  "Observation",
		"limit":  "1",
		"offset": "1",
	}

	resSearch3, err := searchTool.Handler(ctx, reqSearch3)
	if err != nil {
		t.Fatalf("sv_mem_search limit/offset failed: %v", err)
	}
	textResult3 := textContent(resSearch3.Content[0])
	lines := strings.Split(strings.TrimSpace(textResult3), "\n")
	// Header + 1 result
	if len(lines) < 2 {
		t.Errorf("expected limited search results (at least 2 lines including header), got: %s", textResult3)
	}
}

func TestSearchMemorySemanticHandler(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	server := NewServer(pool, cfg)
	searchTool := server.GetTool("sv_mem_search")
	ctx := context.Background()

	a, err := memory.SaveMemory(pool.Writer, &memory.Memory{
		ProjectID: cfg.ProjectID, Category: "decision",
		What: "Auth timeout retry strategy", Why: "w", Learned: "l",
	})
	if err != nil {
		t.Fatalf("seed a: %v", err)
	}
	b, err := memory.SaveMemory(pool.Writer, &memory.Memory{
		ProjectID: cfg.ProjectID, Category: "standard",
		What: "Session expiration TTL", Why: "w", Learned: "l",
	})
	if err != nil {
		t.Fatalf("seed b: %v", err)
	}

	run := func(args map[string]any) string {
		t.Helper()
		req := mcpgo.CallToolRequest{}
		req.Params.Name = "sv_mem_search"
		req.Params.Arguments = args
		res, err := searchTool.Handler(ctx, req)
		if err != nil {
			t.Fatalf("sv_mem_search failed: %v", err)
		}
		return textContent(res.Content[0])
	}

	original := memory.SemanticRunAgent
	defer func() { memory.SemanticRunAgent = original }()

	// Semantic pass keeps only the memory the agent marks relevant.
	memory.SemanticRunAgent = func(ctx context.Context, agent, prompt string) (string, error) {
		return `[{"id":"` + a.ID + `","relevant":true,"reason":"auth relates"},` +
			`{"id":"` + b.ID + `","relevant":false,"reason":"unrelated"}]`, nil
	}
	out := run(map[string]any{"query": "auth", "semantic": "true"})
	if !strings.Contains(out, "Semantic recall: ranked by meaning") {
		t.Errorf("expected semantic recall note, got: %s", out)
	}
	if !strings.Contains(out, "Auth timeout retry strategy") {
		t.Errorf("expected relevant memory in results, got: %s", out)
	}
	if !strings.Contains(out, "auth relates") {
		t.Errorf("expected relevance reason inline, got: %s", out)
	}
	if strings.Contains(out, "Session expiration TTL") {
		t.Errorf("irrelevant memory should be filtered out by semantic recall, got: %s", out)
	}

	// Fail-open: agent error keeps the keyword results with a note.
	memory.SemanticRunAgent = func(ctx context.Context, agent, prompt string) (string, error) {
		return "", context.DeadlineExceeded
	}
	out = run(map[string]any{"query": "auth", "semantic": "true"})
	if !strings.Contains(out, "Semantic recall unavailable") {
		t.Errorf("expected fail-open note, got: %s", out)
	}
	if !strings.Contains(out, "Auth timeout retry strategy") {
		t.Errorf("expected keyword results on fail-open, got: %s", out)
	}
}

func TestSimilarMemoriesSurfacing(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	server := NewServer(pool, cfg)
	saveTool := server.GetTool("sv_mem_save")
	ctx := context.Background()

	// Save first memory
	req1 := mcpgo.CallToolRequest{}
	req1.Params.Name = "sv_mem_save"
	req1.Params.Arguments = map[string]any{
		"category": "standard",
		"what":     "Use standard naming convention for tests",
		"why":      "Enforces codebase consistency",
		"learned":  "Suffix files with _test.go",
	}
	_, _ = saveTool.Handler(ctx, req1)

	// Save another memory with similar title
	req2 := mcpgo.CallToolRequest{}
	req2.Params.Name = "sv_mem_save"
	req2.Params.Arguments = map[string]any{
		"category": "standard",
		"what":     "Use naming convention for unit tests",
		"why":      "Allows automated scanning",
		"learned":  "Unit tests should end with _test.go",
	}

	res2, err := saveTool.Handler(ctx, req2)
	if err != nil {
		t.Fatalf("sv_mem_save failed: %v", err)
	}

	textResult := textContent(res2.Content[0])
	if !strings.Contains(textResult, "Similar memories detected") {
		t.Errorf("expected similar memories warning to surface, got: %s", textResult)
	}
}

func TestSessionLifecycleAndJudges(t *testing.T) {
	t.Setenv("SV_MEMORY_FULL_TOOLS", "1")
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	server := NewServer(pool, cfg)

	ctx := context.Background()

	// Tools
	startTool := server.GetTool("sv_mem_session_start")
	endTool := server.GetTool("sv_mem_session_end")
	summaryTool := server.GetTool("sv_mem_session_summary")
	contextTool := server.GetTool("sv_mem_context")
	saveTool := server.GetTool("sv_mem_save")
	judgeTool := server.GetTool("sv_mem_judge")
	compareTool := server.GetTool("sv_mem_compare")
	statsTool := server.GetTool("sv_mem_stats")
	deleteTool := server.GetTool("sv_mem_delete")
	passiveTool := server.GetTool("sv_mem_capture_passive")
	suggestTool := server.GetTool("sv_mem_suggest_topic_key")
	reviewTool := server.GetTool("sv_mem_review")
	graphSyncTool := server.GetTool("sv_graph_sync")
	conflictsMCPTool := server.GetTool("sv_mem_conflicts")

	// 1. Session Start
	resStart, err := startTool.Handler(ctx, mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Arguments: map[string]any{
				"goal": "Improve testing",
			},
		},
	})
	if err != nil {
		t.Fatalf("session_start failed: %v", err)
	}
	sessionMsg := textContent(resStart.Content[0])
	if !strings.Contains(sessionMsg, "Session started") {
		t.Fatalf("expected started session, got: %s", sessionMsg)
	}

	// Extract session ID
	parts := strings.Split(sessionMsg, "ID: ")
	if len(parts) < 2 {
		t.Fatalf("could not extract session id from: %s", sessionMsg)
	}
	sessionID := strings.Split(parts[1], ")")[0]

	// 2. Capture passive observation
	resPassive, err := passiveTool.Handler(ctx, mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Arguments: map[string]any{
				"what": "Passive observation log",
				"why":  "Verifying capture tool",
			},
		},
	})
	if err != nil {
		t.Fatalf("capture_passive failed: %v", err)
	}
	if !strings.Contains(textContent(resPassive.Content[0]), "Passive observation captured") {
		t.Errorf("expected passive captured, got: %s", textContent(resPassive.Content[0]))
	}

	// 3. Save memory to link to session
	resSave1, _ := saveTool.Handler(ctx, mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Arguments: map[string]any{
				"category": "architecture",
				"what":     "Decision A",
				"why":      "Reason A",
				"learned":  "Learned A",
			},
		},
	})
	saveMsg1 := textContent(resSave1.Content[0])
	id1 := strings.Split(strings.Split(saveMsg1, "ID: ")[1], ")")[0]

	resSave2, _ := saveTool.Handler(ctx, mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Arguments: map[string]any{
				"category": "architecture",
				"what":     "Decision B",
				"why":      "Reason B",
				"learned":  "Learned B",
			},
		},
	})
	saveMsg2 := textContent(resSave2.Content[0])
	id2 := strings.Split(strings.Split(saveMsg2, "ID: ")[1], ")")[0]

	// 4. Suggest topic key
	resSuggest, err := suggestTool.Handler(ctx, mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Arguments: map[string]any{
				"category": "architecture",
				"what":     "Suggested key memory",
			},
		},
	})
	if err != nil || !strings.Contains(textContent(resSuggest.Content[0]), "Suggested topic_key:") {
		t.Errorf("suggest_topic_key failed: %v", err)
	}

	// 5. Judge memories (Supersedes)
	resJudge, err := judgeTool.Handler(ctx, mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Arguments: map[string]any{
				"source_id":     id2,
				"target_id":     id1,
				"relation_type": "supersedes",
				"reason":        "Decision B overrides Decision A",
			},
		},
	})
	if err != nil {
		t.Fatalf("judge failed: %v", err)
	}
	if !strings.Contains(textContent(resJudge.Content[0]), "Judgment created") {
		t.Errorf("expected judgment created, got: %s", textContent(resJudge.Content[0]))
	}

	// 6. Compare memories
	resCompare, err := compareTool.Handler(ctx, mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Arguments: map[string]any{
				"id1": id1,
				"id2": id2,
			},
		},
	})
	if err != nil || !strings.Contains(textContent(resCompare.Content[0]), "Decision A") {
		t.Errorf("compare failed: %v", err)
	}

	// 7. Stats
	resStats, err := statsTool.Handler(ctx, mcpgo.CallToolRequest{})
	if err != nil || !strings.Contains(textContent(resStats.Content[0]), "Total memories") {
		t.Errorf("stats failed: %v", err)
	}

	// 8. Stats includes current project info (folded from sv_mem_current_project)
	resProj, err := statsTool.Handler(ctx, mcpgo.CallToolRequest{})
	if err != nil || !strings.Contains(textContent(resProj.Content[0]), "Current project") || !strings.Contains(textContent(resProj.Content[0]), cfg.ProjName) {
		t.Errorf("stats should report current project, got: %v", err)
	}

	// 9. Review
	resReview, err := reviewTool.Handler(ctx, mcpgo.CallToolRequest{})
	if err != nil || !strings.Contains(textContent(resReview.Content[0]), "Memory Review") {
		t.Errorf("review failed: %v", err)
	}

	// 10. Session Summary
	_, err = summaryTool.Handler(ctx, mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Arguments: map[string]any{
				"session_id":   sessionID,
				"goal":         "Improve testing",
				"discoveries":  "Discovered that tests work",
				"accomplished": "Wrote some unit tests",
				"next_steps":   "Write more tests",
			},
		},
	})
	if err != nil {
		t.Fatalf("session_summary failed: %v", err)
	}

	// 11. Session End
	_, err = endTool.Handler(ctx, mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Arguments: map[string]any{
				"session_id": sessionID,
				"summary":    "Completed session tasks",
			},
		},
	})
	if err != nil {
		t.Fatalf("session_end failed: %v", err)
	}

	// 12. Context Recovery
	resContext, err := contextTool.Handler(ctx, mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Arguments: map[string]any{
				"limit": "5",
			},
		},
	})
	if err != nil {
		t.Fatalf("session context failed: %v", err)
	}
	contextStr := textContent(resContext.Content[0])
	if !strings.Contains(contextStr, "Improve testing") || !strings.Contains(contextStr, "Completed session tasks") {
		t.Errorf("expected goals and final accomplishments summary in recovered context, got: %s", contextStr)
	}

	// 13. Graph Sync tool
	resGraphSync, err := graphSyncTool.Handler(ctx, mcpgo.CallToolRequest{})
	if err != nil || !strings.Contains(textContent(resGraphSync.Content[0]), "synchronized successfully") {
		t.Errorf("graph_sync failed: %v", err)
	}
	// Inject conflicting memories
	_, _ = saveTool.Handler(ctx, mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Arguments: map[string]any{
				"category": "architecture",
				"what":     "Conflict memory X",
				"why":      "Reason X",
				"learned":  "Learned X",
			},
		},
	})
	_, _ = saveTool.Handler(ctx, mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Arguments: map[string]any{
				"category": "architecture",
				"what":     "Conflict memory Y",
				"why":      "Reason Y",
				"learned":  "Learned Y",
			},
		},
	})

	// 13.a Scan conflicts via MCP
	resConflictsScan, err := conflictsMCPTool.Handler(ctx, mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Arguments: map[string]any{
				"action":    "scan",
				"apply":     "true",
				"threshold": "0.4",
			},
		},
	})
	if err != nil {
		t.Fatalf("mcp conflicts scan failed: %v", err)
	}
	scanMsg := textContent(resConflictsScan.Content[0])
	if !strings.Contains(scanMsg, "potential conflict") {
		t.Errorf("expected potential conflicts to be found, got: %s", scanMsg)
	}

	// Extract relation ID from scan message (e.g. **ID:** e827d81a)
	partsRel := strings.Split(scanMsg, "**ID:** ")
	if len(partsRel) < 2 {
		t.Fatalf("could not extract conflict relation ID from: %s", scanMsg)
	}
	relID := strings.Split(partsRel[1], " ")[0]
	relID = strings.TrimSpace(strings.Split(relID, "|")[0])

	// 13.b List conflicts via MCP
	resConflictsList, err := conflictsMCPTool.Handler(ctx, mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Arguments: map[string]any{
				"action": "list",
				"status": "pending",
			},
		},
	})
	if err != nil {
		t.Fatalf("mcp conflicts list failed: %v", err)
	}
	listMsg := textContent(resConflictsList.Content[0])
	if !strings.Contains(listMsg, relID) {
		t.Errorf("expected relation %s in listed conflicts, got: %s", relID, listMsg)
	}

	// 13.c Ignore conflict via MCP
	resConflictsIgnore, err := conflictsMCPTool.Handler(ctx, mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Arguments: map[string]any{
				"action":      "ignore",
				"relation_id": relID,
			},
		},
	})
	if err != nil {
		t.Fatalf("mcp conflicts ignore failed: %v", err)
	}
	ignoreMsg := textContent(resConflictsIgnore.Content[0])
	if !strings.Contains(ignoreMsg, "marked as ignored") {
		t.Errorf("expected ignore confirmation, got: %s", ignoreMsg)
	}

	// 14. Delete memory
	resDelete, err := deleteTool.Handler(ctx, mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Arguments: map[string]any{
				"id":   id1,
				"hard": "false",
			},
		},
	})
	if err != nil || !strings.Contains(textContent(resDelete.Content[0]), "deleted successfully") {
		t.Errorf("delete failed: %v", err)
	}
}

func TestAutoTopicKeyAndUnifiedSessionEnd(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	server := NewServer(pool, cfg)
	ctx := context.Background()

	startTool := server.GetTool("sv_mem_session_start")
	saveTool := server.GetTool("sv_mem_save")
	endTool := server.GetTool("sv_mem_session_end")
	getTool := server.GetTool("sv_mem_get")

	// 1. Start session without explicit ID handling
	resStart, err := startTool.Handler(ctx, mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Arguments: map[string]any{
				"goal": "Test ergonomics",
			},
		},
	})
	if err != nil {
		t.Fatalf("session_start failed: %v", err)
	}
	if !strings.Contains(textContent(resStart.Content[0]), "Session started") {
		t.Fatalf("unexpected start output: %s", textContent(resStart.Content[0]))
	}

	// 2. Save decision WITHOUT topic_key (should auto-generate topic_key)
	resSave, err := saveTool.Handler(ctx, mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Arguments: map[string]any{
				"category": "decision",
				"what":     "Use DuckDB for analytics",
				"why":      "Fast columnar queries",
				"learned":  "Columnar engines excel at analytical scans",
			},
		},
	})
	if err != nil {
		t.Fatalf("save failed: %v", err)
	}
	saveMsg := textContent(resSave.Content[0])
	if !strings.Contains(saveMsg, "Successfully created memory") {
		t.Fatalf("unexpected save output: %s", saveMsg)
	}

	// Extract memory ID
	parts := strings.Split(saveMsg, "ID: ")
	if len(parts) < 2 {
		t.Fatalf("could not extract memory ID: %s", saveMsg)
	}
	memID := strings.Split(parts[1], ")")[0]

	// Verify topic_key was auto-generated
	resGet, err := getTool.Handler(ctx, mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Arguments: map[string]any{
				"id": memID,
			},
		},
	})
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	getMsg := textContent(resGet.Content[0])
	if !strings.Contains(getMsg, "decision/use-duckdb-for-analytics") {
		t.Errorf("expected auto-derived topic_key in memory, got: %s", getMsg)
	}

	// 3. End session WITHOUT session_id and WITH inline summary
	resEnd, err := endTool.Handler(ctx, mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Arguments: map[string]any{
				"accomplished": "Implemented DuckDB adapter",
				"next_steps":   "Add benchmarks",
			},
		},
	})
	if err != nil {
		t.Fatalf("endTool failed: %v", err)
	}
	endMsg := textContent(resEnd.Content[0])
	if !strings.Contains(endMsg, "ended successfully") {
		t.Errorf("expected session ended message, got: %s", endMsg)
	}
}
