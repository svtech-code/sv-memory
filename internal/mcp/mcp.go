package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/svtech/sv-memory/internal/config"
	"github.com/svtech/sv-memory/internal/db"
	"github.com/svtech/sv-memory/internal/graph"
	"github.com/svtech/sv-memory/internal/memory"
)

// debugEnabled reports whether SV_MEMORY_DEBUG env var is set to a truthy value.
// Used to emit stderr timing logs without polluting the MCP stdio JSON channel.
func debugEnabled() bool {
	v := os.Getenv("SV_MEMORY_DEBUG")
	if v == "" {
		return false
	}
	if b, err := strconv.ParseBool(v); err == nil {
		return b
	}
	return true
}

// debugLog writes a timing line to stderr (safe: stderr is a separate channel
// from MCP JSON-RPC which uses stdout). Skipped entirely on the hot path when
// SV_MEMORY_DEBUG is unset.
func debugLog(format string, args ...interface{}) {
	if !debugEnabled() {
		return
	}
	fmt.Fprintf(os.Stderr, "[sv-memory] "+format+"\n", args...)
}

// StartServer starts the MCP server using stdio transport.
// Reads use the pool's Reader so concurrent tool calls scale; writes (save)
// go through the Writer to keep SQLite serialized under WAL.
func StartServer(pool *db.Pool, cfg *config.Config) error {
	// Initialize server
	s := server.NewMCPServer("sv-memory", "1.0.0")

	// Per-server mtime cache so we skip redundant SyncFromGit round-trips when
	// .sv-memory/memories.json hasn't changed since the last pull. The MCP
	// server is long-lived (stdio), so we keep this state on the closure.
	var (
		syncMu       sync.Mutex
		lastSyncMtim time.Time
	)
	syncFile := filepath.Join(cfg.ProjPath, ".sv-memory", "memories.json")

	// maybeSyncFromGit imports memories.json only when its mtime advanced since
	// the last call. Falls back to a full sync the first time (zero mtime) and
	// when the file does not exist (no Team sync configured).
	maybeSyncFromGit := func() {
		syncMu.Lock()
		defer syncMu.Unlock()
		info, err := os.Stat(syncFile)
		if err != nil {
			lastSyncMtim = time.Time{}
			return
		}
		if !info.ModTime().After(lastSyncMtim) {
			return
		}
		start := time.Now()
		if err := memory.SyncFromGit(pool.Writer, cfg.ProjectID, cfg.ProjPath); err != nil {
			fmt.Fprintf(os.Stderr, "[sv-memory] syncFromGit failed: %v\n", err)
			return
		}
		lastSyncMtim = info.ModTime()
		debugLog("syncFromGit pulled in %s", time.Since(start))
	}

	// In-memory graph cache: loads the full project's nodes+edges with two
	// SQL queries then runs BFS in Go-land, avoiding N+1 round-trips per
	// visited node. Invalidated by sv_graph_sync.
	var (
		graphMu    sync.Mutex
		cachedGraph *inMemoryGraph
	)

	getOrLoadGraph := func() (*inMemoryGraph, error) {
		graphMu.Lock()
		defer graphMu.Unlock()
		if cachedGraph != nil {
			return cachedGraph, nil
		}
		var count int
		if err := pool.Reader.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ?", cfg.ProjectID).Scan(&count); err != nil {
			return nil, err
		}
		if count == 0 {
			startBuild := time.Now()
			if err := graph.SyncGraph(pool.Writer, cfg.ProjectID, cfg.ProjPath); err != nil {
				return nil, err
			}
			debugLog("graph_query auto-built graph in %s", time.Since(startBuild))
		}
		g, err := loadFullGraph(pool.Reader, cfg.ProjectID)
		if err != nil {
			return nil, err
		}
		cachedGraph = g
		return g, nil
	}

	// 1. Tool: sv_mem_save
	saveTool := mcp.NewTool("sv_mem_save",
		mcp.WithDescription("Persist a key architectural decision, bug fix, progress journal, or standard guidelines to the project's memory. Supports optional topic_key for upsert semantics (update in place on same project+topic) and session_id for session association."),
		mcp.WithString("category", mcp.Required(), mcp.Description("Category of memory: 'bugfix' | 'architecture' | 'standard' | 'decision' | 'journal' | 'postmortem' | 'discussion' | 'idea' | 'qa'")),
		mcp.WithString("what", mcp.Required(), mcp.Description("Concise description of the decision, standard, or fix")),
		mcp.WithString("why", mcp.Required(), mcp.Description("Detailed reasoning for this choice")),
		mcp.WithString("learned", mcp.Required(), mcp.Description("Rule or key lesson to guide future agents")),
		mcp.WithString("where_path", mcp.Description("Optional file or folder path affected by this memory")),
		mcp.WithString("impact", mcp.Description("Achievements, successes, or what went well")),
		mcp.WithString("errors_faced", mcp.Description("Errors faced, roadblocks, or what went wrong")),
		mcp.WithString("next_steps", mcp.Description("Next actions or pending tasks to continue work")),
		mcp.WithString("topic_key", mcp.Description("Optional stable topic key for upsert semantics. When set, saves to the same project+topic update in place instead of creating a new record. Format: 'category/kebab-case-description'. Use sv_mem_suggest_topic_key to generate one.")),
		mcp.WithString("session_id", mcp.Description("Optional session ID to associate this memory with an active session.")),
	)

	s.AddTool(saveTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		category, err := req.RequireString("category")
		if err != nil {
			return mcp.NewToolResultError("missing required field: category"), nil
		}
		what, err := req.RequireString("what")
		if err != nil {
			return mcp.NewToolResultError("missing required field: what"), nil
		}
		why, err := req.RequireString("why")
		if err != nil {
			return mcp.NewToolResultError("missing required field: why"), nil
		}
		learned, err := req.RequireString("learned")
		if err != nil {
			return mcp.NewToolResultError("missing required field: learned"), nil
		}

		wherePath := req.GetString("where_path", "")
		impact := req.GetString("impact", "")
		errorsFaced := req.GetString("errors_faced", "")
		nextSteps := req.GetString("next_steps", "")
		topicKey := req.GetString("topic_key", "")
		sessionID := req.GetString("session_id", "")

		// Auto-associate with active session if no explicit session_id provided
		if sessionID == "" {
			if active, err := memory.GetActiveSession(pool.Writer, cfg.ProjectID); err == nil && active != nil {
				sessionID = active.ID
			}
		}

		mem := &memory.Memory{
			ProjectID:   cfg.ProjectID,
			Category:    category,
			What:        what,
			Why:         why,
			WherePath:   wherePath,
			Learned:     learned,
			GitBranch:   config.GetGitBranch(cfg.ProjPath),
			GitCommit:   config.GetGitCommit(cfg.ProjPath),
			Author:      config.GetGitAuthor(cfg.ProjPath),
			Impact:      impact,
			ErrorsFaced: errorsFaced,
			NextSteps:   nextSteps,
			TopicKey:    topicKey,
			SessionID:   sessionID,
		}

		startSave := time.Now()
		saved, err := memory.SaveMemory(pool.Writer, mem)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to save memory to SQLite: %v", err)), nil
		}
		debugLog("mem_save SQLite write for id=%s took %s", saved.ID, time.Since(startSave))

		startSync := time.Now()
		if err := memory.SyncToGit(pool.Writer, cfg.ProjectID, cfg.ProjPath); err != nil {
			return mcp.NewToolResultText(fmt.Sprintf("Saved to local SQLite database (ID: %s) but failed Git Sync: %v", saved.ID, err)), nil
		}
		debugLog("mem_save syncToGit for id=%s took %s", saved.ID, time.Since(startSync))

		syncMu.Lock()
		if info, err := os.Stat(syncFile); err == nil {
			lastSyncMtim = info.ModTime()
		}
		syncMu.Unlock()

		// Build contextual response based on what SaveMemory did
		var action string
		if saved.DuplicateCount > 0 {
			action = fmt.Sprintf("duplicate suppressed (count: %d)", saved.DuplicateCount)
		} else if saved.RevisionCount > 1 {
			action = fmt.Sprintf("updated existing topic_key (revision: %d)", saved.RevisionCount)
		} else {
			action = "created"
		}
		return mcp.NewToolResultText(fmt.Sprintf("Successfully %s memory (ID: %s) and synced to Git workspace (.sv-memory/memories.json)", action, saved.ID)), nil
	})

	// 2. Tool: sv_mem_suggest_topic_key
	suggestTool := mcp.NewTool("sv_mem_suggest_topic_key",
		mcp.WithDescription("Suggest a stable topic_key for an evolving topic before saving. The key follows 'category/kebab-case-description' format and enables upsert semantics in sv_mem_save."),
		mcp.WithString("category", mcp.Required(), mcp.Description("Category of memory: 'bugfix' | 'architecture' | 'standard' | 'decision' | 'journal' | 'postmortem' | 'discussion' | 'idea' | 'qa'")),
		mcp.WithString("what", mcp.Required(), mcp.Description("The title or description of the memory")),
	)

	s.AddTool(suggestTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		category, err := req.RequireString("category")
		if err != nil {
			return mcp.NewToolResultError("missing required field: category"), nil
		}
		what, err := req.RequireString("what")
		if err != nil {
			return mcp.NewToolResultError("missing required field: what"), nil
		}
		key := memory.SuggestTopicKey(category, what)
		return mcp.NewToolResultText(fmt.Sprintf("Suggested topic_key: `%s`\nUse this key with sv_mem_save(topic_key=\"%s\") to enable upsert semantics.", key, key)), nil
	})

	// 3. Tool: sv_mem_session_start
	sessionStartTool := mcp.NewTool("sv_mem_session_start",
		mcp.WithDescription("Register the start of a new coding session. Call this at the beginning of a work session to enable session grouping and post-compaction context recovery."),
		mcp.WithString("goal", mcp.Description("Optional goal or objective for this session")),
		mcp.WithString("directory", mcp.Description("Optional working directory (auto-detected from repo if omitted)")),
	)

	s.AddTool(sessionStartTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		goal := req.GetString("goal", "")
		dir := req.GetString("directory", "")
		if dir == "" {
			dir = cfg.ProjPath
		}
		session, err := memory.StartSession(pool.Writer, cfg.ProjectID, goal, dir)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to start session: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Session started (ID: %s). Use sv_mem_save with session_id=\"%s\" to associate memories, then sv_mem_session_end to close.", session.ID, session.ID)), nil
	})

	// 4. Tool: sv_mem_session_end
	sessionEndTool := mcp.NewTool("sv_mem_session_end",
		mcp.WithDescription("End an active coding session with a summary. Call this before finishing a work session to enable context recovery via sv_mem_context."),
		mcp.WithString("session_id", mcp.Required(), mcp.Description("The session ID to end")),
		mcp.WithString("summary", mcp.Description("Optional summary of what was accomplished")),
	)

	s.AddTool(sessionEndTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sessionID, err := req.RequireString("session_id")
		if err != nil {
			return mcp.NewToolResultError("missing required field: session_id"), nil
		}
		summary := req.GetString("summary", "")
		if err := memory.EndSession(pool.Writer, sessionID, summary); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to end session: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Session %s ended successfully.", sessionID)), nil
	})

	// 5. Tool: sv_mem_session_summary
	sessionSummaryTool := mcp.NewTool("sv_mem_session_summary",
		mcp.WithDescription("Save a structured summary for a session. Call before sv_mem_session_end to record goal, discoveries, accomplished work, next steps, and relevant files."),
		mcp.WithString("session_id", mcp.Required(), mcp.Description("Session ID to associate the summary with")),
		mcp.WithString("goal", mcp.Description("Original goal or objective of the session")),
		mcp.WithString("discoveries", mcp.Description("Key discoveries or findings during the session")),
		mcp.WithString("accomplished", mcp.Description("What was accomplished during the session")),
		mcp.WithString("next_steps", mcp.Description("Next steps or pending items")),
		mcp.WithString("files", mcp.Description("Relevant files modified or created")),
	)

	s.AddTool(sessionSummaryTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sessionID, err := req.RequireString("session_id")
		if err != nil {
			return mcp.NewToolResultError("missing required field: session_id"), nil
		}
		goal := req.GetString("goal", "")
		discoveries := req.GetString("discoveries", "")
		accomplished := req.GetString("accomplished", "")
		nextSteps := req.GetString("next_steps", "")
		files := req.GetString("files", "")
		if err := memory.SaveSessionSummary(pool.Writer, sessionID, goal, discoveries, accomplished, nextSteps, files); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to save session summary: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Session summary saved for session %s.", sessionID)), nil
	})

	// 6. Tool: sv_mem_context
	contextTool := mcp.NewTool("sv_mem_context",
		mcp.WithDescription("Recover context from the last completed session. Call this after a compaction or context reset to resume work. Returns the last session's goal, summary, and associated memories."),
		mcp.WithString("limit", mcp.Description("Optional limit of memories to include (default '10')")),
	)

	s.AddTool(contextTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		startQuery := time.Now()
		contextStr, err := memory.GetSessionContext(pool.Reader, cfg.ProjectID)
		debugLog("mem_context took %s", time.Since(startQuery))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get session context: %v", err)), nil
		}
		return mcp.NewToolResultText(contextStr), nil
	})

	// 7. Tool: sv_mem_search
	searchTool := mcp.NewTool("sv_mem_search",
		mcp.WithDescription("Query the historical project decisions, architectural rules, and past bugfixes using keyword/FTS search."),
		mcp.WithString("query", mcp.Required(), mcp.Description("The keyword or phrase to search for")),
		mcp.WithString("category", mcp.Description("Optional category to filter results: 'bugfix' | 'architecture' | 'standard' | 'decision' | 'journal' | 'postmortem' | 'discussion' | 'idea' | 'qa'")),
		mcp.WithString("limit", mcp.Description("Optional limit of results to return (default is '10')")),
	)

	s.AddTool(searchTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query, err := req.RequireString("query")
		if err != nil {
			return mcp.NewToolResultError("missing required field: query"), nil
		}
		category := req.GetString("category", "")
		limitStr := req.GetString("limit", "10")

		limit := 10
		if limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil {
				limit = l
			}
		}

		// Only pull from Git if memories.json's mtime advanced since the last
		// pull. Cheap os.Stat avoids re-reading+parsing the whole file on every
		// search query (was the previous behavior — O(n) cost per call).
		startSync := time.Now()
		maybeSyncFromGit()
		debugLog("mem_search maybeSyncFromGit took %s", time.Since(startSync))

		// Searches are read-only — route to the Reader to scale concurrently
		// with other MCP tool calls (WAL allows N parallel readers).
		startSearch := time.Now()
		memories, err := memory.SearchMemories(pool.Reader, cfg.ProjectID, query, category, limit)
		debugLog("mem_search query=%q category=%q returned %d rows in %s", query, category, len(memories), time.Since(startSearch))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed searching memories: %v", err)), nil
		}

		if len(memories) == 0 {
			return mcp.NewToolResultText("No relevant project memories found matching the query."), nil
		}

		// Format output as clean Markdown for the AI agent
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Found %d relevant project memories:\n\n", len(memories)))
		for _, m := range memories {
			sb.WriteString(fmt.Sprintf("### [%s] %s (ID: %s)\n", strings.ToUpper(m.Category), m.What, m.ID))
			sb.WriteString(fmt.Sprintf("* **Why:** %s\n", m.Why))
			sb.WriteString(fmt.Sprintf("* **Rule / Learned:** %s\n", m.Learned))
			if m.WherePath != "" {
				sb.WriteString(fmt.Sprintf("* **Path:** `%s`\n", m.WherePath))
			}
			if m.GitBranch != "" {
				sb.WriteString(fmt.Sprintf("* **Branch:** `%s`\n", m.GitBranch))
			}
			if m.GitCommit != "" {
				sb.WriteString(fmt.Sprintf("* **Commit:** `%s`\n", m.GitCommit))
			}
			if m.Author != "" {
				sb.WriteString(fmt.Sprintf("* **Author:** `%s`\n", m.Author))
			}
			if m.Impact != "" {
				sb.WriteString(fmt.Sprintf("* **What went well / Impact:** %s\n", m.Impact))
			}
			if m.ErrorsFaced != "" {
				sb.WriteString(fmt.Sprintf("* **Roadblocks / Errors faced:** %s\n", m.ErrorsFaced))
			}
			if m.NextSteps != "" {
				sb.WriteString(fmt.Sprintf("* **Next steps / Pending:** %s\n", m.NextSteps))
			}
			sb.WriteString(fmt.Sprintf("* **Date:** %s\n\n", m.CreatedAt.Format("2006-01-02")))
		}

		return mcp.NewToolResultText(sb.String()), nil
	})

	// 4. Tool: sv_graph_query
	graphQueryTool := mcp.NewTool("sv_graph_query",
		mcp.WithDescription("Retrieve project code structure, connections, imports, and dependencies for a given module, file, or package."),
		mcp.WithString("path_or_node", mcp.Required(), mcp.Description("The file path, package name, or module to inspect")),
		mcp.WithString("depth", mcp.Description("Hop distance depth in the dependency graph (default is '1')")),
	)

	s.AddTool(graphQueryTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		pathOrNode, err := req.RequireString("path_or_node")
		if err != nil {
			return mcp.NewToolResultError("missing required field: path_or_node"), nil
		}
		depthStr := req.GetString("depth", "1")

		depth := 1
		if depthStr != "" {
			if d, err := strconv.Atoi(depthStr); err == nil {
				depth = d
			}
		}

		// Load or retrieve the in-memory graph cache. First call triggers a
		// full load from SQLite (two bulk queries); subsequent calls hit
		// cached Go maps until sv_graph_sync invalidates them.
		startQuery := time.Now()
		g, err := getOrLoadGraph()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to load graph: %v", err)), nil
		}

		subGraph := g.query(pathOrNode, depth)
		debugLog("graph_query path=%q depth=%d returned %d nodes / %d edges in %s", pathOrNode, depth, len(subGraph.Nodes), len(subGraph.Edges), time.Since(startQuery))

		if len(subGraph.Nodes) == 0 {
			return mcp.NewToolResultText(fmt.Sprintf("No nodes found matching '%s' in the project graph.", pathOrNode)), nil
		}

		// Build a markdown response containing node details and a Mermaid diagram representation
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("## Code Sub-Graph for '%s' (Depth: %d)\n\n", pathOrNode, depth))

		sb.WriteString("### Nodes in Sub-graph:\n")
		for _, node := range subGraph.Nodes {
			sb.WriteString(fmt.Sprintf("- **%s** (`%s`): %s\n", node.Label, node.ID, node.Type))
		}
		sb.WriteString("\n")

		if len(subGraph.Edges) > 0 {
			sb.WriteString("### Mermaid Dependency Diagram:\n")
			sb.WriteString("```mermaid\ngraph TD\n")
			for _, edge := range subGraph.Edges {
				// Escape labels for Mermaid
				srcEscaped := escapeMermaid(edge.SourceID)
				tgtEscaped := escapeMermaid(edge.TargetID)

				if edge.RelationType == "imports" {
					sb.WriteString(fmt.Sprintf("    %s -->|imports| %s\n", srcEscaped, tgtEscaped))
				} else {
					sb.WriteString(fmt.Sprintf("    %s -->|%s| %s\n", srcEscaped, edge.RelationType, tgtEscaped))
				}
			}
			sb.WriteString("```\n")
		} else {
			sb.WriteString("*No connections/edges found in this range.*\n")
		}

		return mcp.NewToolResultText(sb.String()), nil
	})

	// 5. Tool: sv_graph_sync
	graphSyncTool := mcp.NewTool("sv_graph_sync",
		mcp.WithDescription("Trigger a full re-scan of the project code directory and refresh the structural dependency graph stored in SQLite."),
	)

	s.AddTool(graphSyncTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		startSync := time.Now()
		err := graph.SyncGraph(pool.Writer, cfg.ProjectID, cfg.ProjPath)
		debugLog("graph_sync rebuild took %s", time.Since(startSync))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to sync graph: %v", err)), nil
		}
		// Invalidate in-memory cache so the next sv_graph_query reloads fresh
		// data from the rebuilt graph tables.
		graphMu.Lock()
		cachedGraph = nil
		graphMu.Unlock()
		return mcp.NewToolResultText("Dependency graph refreshed and synchronized successfully in SQLite."), nil
	})

	// Start standard IO transport server using convenience function
	return server.ServeStdio(s)
}

// subGraph is the result of a BFS traversal returned by the sv_graph_query tool.
type subGraph struct {
	Nodes []*graph.Node
	Edges []*graph.Edge
}

// inMemoryGraph holds the full project graph in Go memory so that BFS
// traversals (sv_graph_query) run without any SQL round-trips once the data
// is loaded. Two bulk queries replace the previous N+1 pattern of one
// SELECT per visited node + one SELECT per node's edges.
type inMemoryGraph struct {
	nodes         map[string]*graph.Node
	edgesBySource map[string][]*graph.Edge
	edgesByTarget map[string][]*graph.Edge
}

// loadFullGraph executes two queries to load all nodes and edges for a project
// into an inMemoryGraph.
func loadFullGraph(db *sql.DB, projectID string) (*inMemoryGraph, error) {
	nodeMap := make(map[string]*graph.Node)
	nRows, err := db.Query("SELECT id, node_type, label, path, metadata FROM graph_nodes WHERE project_id = ?", projectID)
	if err != nil {
		return nil, err
	}
	defer nRows.Close()
	for nRows.Next() {
		var n graph.Node
		var metaStr string
		if err := nRows.Scan(&n.ID, &n.Type, &n.Label, &n.Path, &metaStr); err == nil {
			_ = json.Unmarshal([]byte(metaStr), &n.Metadata)
			nodeMap[n.ID] = &n
		}
	}
	if err := nRows.Err(); err != nil {
		return nil, err
	}

	edgesBySrc := make(map[string][]*graph.Edge)
	edgesByTgt := make(map[string][]*graph.Edge)
	eRows, err := db.Query("SELECT id, source_id, target_id, relation_type FROM graph_edges WHERE project_id = ?", projectID)
	if err != nil {
		return nil, err
	}
	defer eRows.Close()
	for eRows.Next() {
		var e graph.Edge
		if err := eRows.Scan(&e.ID, &e.SourceID, &e.TargetID, &e.RelationType); err == nil {
			edgesBySrc[e.SourceID] = append(edgesBySrc[e.SourceID], &e)
			edgesByTgt[e.TargetID] = append(edgesByTgt[e.TargetID], &e)
		}
	}
	if err := eRows.Err(); err != nil {
		return nil, err
	}

	return &inMemoryGraph{
		nodes:         nodeMap,
		edgesBySource: edgesBySrc,
		edgesByTarget: edgesByTgt,
	}, nil
}

// query performs a BFS traversal over the in-memory graph starting from the
// node matching 'start' (by id, path, or label) and returns all reachable
// nodes and edges within maxDepth hops. Zero SQL calls.
func (g *inMemoryGraph) query(start string, maxDepth int) *subGraph {
	startID := g.findNode(start)
	if startID == "" {
		return &subGraph{}
	}

	type queueItem struct {
		id    string
		depth int
	}

	queue := []queueItem{{id: startID, depth: 0}}
	visited := make(map[string]bool)
	nodeMap := make(map[string]*graph.Node)
	edgeMap := make(map[string]*graph.Edge)

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		if visited[curr.id] {
			continue
		}
		visited[curr.id] = true

		if n, ok := g.nodes[curr.id]; ok {
			nodeMap[curr.id] = n
		}

		if curr.depth >= maxDepth {
			continue
		}

		for _, e := range g.edgesBySource[curr.id] {
			edgeMap[e.ID] = e
			if !visited[e.TargetID] {
				queue = append(queue, queueItem{id: e.TargetID, depth: curr.depth + 1})
			}
		}
		for _, e := range g.edgesByTarget[curr.id] {
			edgeMap[e.ID] = e
			if !visited[e.SourceID] {
				queue = append(queue, queueItem{id: e.SourceID, depth: curr.depth + 1})
			}
		}
	}

	nodes := make([]*graph.Node, 0, len(nodeMap))
	for _, n := range nodeMap {
		nodes = append(nodes, n)
	}
	edges := make([]*graph.Edge, 0, len(edgeMap))
	for _, e := range edgeMap {
		edges = append(edges, e)
	}
	return &subGraph{Nodes: nodes, Edges: edges}
}

// findNode returns the node ID matching by exact id, path, or label.
func (g *inMemoryGraph) findNode(start string) string {
	for id, n := range g.nodes {
		if id == start || n.Path == start || n.Label == start {
			return id
		}
	}
	return ""
}

func escapeMermaid(s string) string {
	// Replaces path slash/special chars with underscores or quotes for Mermaid
	s = strings.ReplaceAll(s, "\\", "/")
	return fmt.Sprintf(`"%s"`, s)
}
