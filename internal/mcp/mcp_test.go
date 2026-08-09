package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/svtech-code/sv-memory/internal/config"
	"github.com/svtech-code/sv-memory/internal/db"
	"github.com/svtech-code/sv-memory/internal/memory"
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

func TestGraphQueryHandler(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	writeMockCodeFiles(t, tempDir)

	server := NewServer(pool, cfg)
	queryTool := server.GetTool("sv_graph_query")
	ctx := context.Background()

	// 1. Query Out direction (default)
	reqQuery := mcpgo.CallToolRequest{}
	reqQuery.Params.Name = "sv_graph_query"
	reqQuery.Params.Arguments = map[string]any{
		"path_or_node": "index.js",
		"depth":        "1",
		"direction":    "out",
	}

	resQuery, err := queryTool.Handler(ctx, reqQuery)
	if err != nil {
		t.Fatalf("sv_graph_query out failed: %v", err)
	}
	textResult1 := textContent(resQuery.Content[0])
	if !strings.Contains(textResult1, "index.js") || !strings.Contains(textResult1, "utils.js") {
		t.Errorf("expected Out sub-graph to contain index.js and utils.js, got: %s", textResult1)
	}

	// 2. Query In direction
	reqQueryIn := mcpgo.CallToolRequest{}
	reqQueryIn.Params.Name = "sv_graph_query"
	reqQueryIn.Params.Arguments = map[string]any{
		"path_or_node": "components/Button.tsx",
		"depth":        "1",
		"direction":    "in",
	}

	resQueryIn, err := queryTool.Handler(ctx, reqQueryIn)
	if err != nil {
		t.Fatalf("sv_graph_query in failed: %v", err)
	}
	textResult2 := textContent(resQueryIn.Content[0])
	if !strings.Contains(textResult2, "index.js") || !strings.Contains(textResult2, "components/Button.tsx") {
		t.Errorf("expected In sub-graph of Button to contain imports/deps from index.js and components/Button.tsx, got: %s", textResult2)
	}
}

func TestGraphPathHandler(t *testing.T) {
	tempDir, pool, cfg := setupTestEnv(t)
	defer cleanupTestEnv(tempDir, pool)

	writeMockCodeFiles(t, tempDir)

	server := NewServer(pool, cfg)
	pathTool := server.GetTool("sv_graph_path")
	ctx := context.Background()

	reqPath := mcpgo.CallToolRequest{}
	reqPath.Params.Name = "sv_graph_path"
	reqPath.Params.Arguments = map[string]any{
		"source":   "index.js",
		"target":   "components/Button.tsx",
		"max_hops": "5",
	}

	resPath, err := pathTool.Handler(ctx, reqPath)
	if err != nil {
		t.Fatalf("sv_graph_path failed: %v", err)
	}
	textResult := textContent(resPath.Content[0])
	if !strings.Contains(textResult, "imports EXTRACTED") || !strings.Contains(textResult, "index.js") || !strings.Contains(textResult, "Button.tsx") {
		t.Errorf("expected path with confidence details, got: %s", textResult)
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

func TestSessionLifecycleAndJudges(t *testing.T) {
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

// TestAllToolsMatchesRegisteredTools is the guard test for the single source
// of truth: every tool registered via NewTool/AddTool in NewServer MUST have a
// matching entry in AllTools (used by the permission manager). It parses the
// mcp.go source to collect the registered names instead of relying on a server
// instance, keeping the test lightweight and independent of DB setup.
func TestAllToolsMatchesRegisteredTools(t *testing.T) {
	src, err := os.ReadFile("mcp.go")
	if err != nil {
		t.Fatalf("failed to read mcp.go: %v", err)
	}

	registered := map[string]bool{}
	for _, line := range strings.Split(string(src), "\n") {
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
		t.Fatal("no registered tools found in mcp.go — guard test is broken")
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
