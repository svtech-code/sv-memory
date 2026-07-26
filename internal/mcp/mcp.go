package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/svtech/sv-memory/internal/config"
	"github.com/svtech/sv-memory/internal/graph"
	"github.com/svtech/sv-memory/internal/memory"
)

// StartServer starts the MCP server using stdio transport.
func StartServer(db *sql.DB, cfg *config.Config) error {
	// Initialize server
	s := server.NewMCPServer("sv-memory", "1.0.0")

	// 1. Tool: sv_mem_save
	saveTool := mcp.NewTool("sv_mem_save",
		mcp.WithDescription("Persist a key architectural decision, bug fix, progress journal, or standard guidelines to the project's memory. This will also update the shared workspace JSON file for Git versioning."),
		mcp.WithString("category", mcp.Required(), mcp.Description("Category of memory: 'bugfix' | 'architecture' | 'standard' | 'decision' | 'journal' | 'postmortem'")),
		mcp.WithString("what", mcp.Required(), mcp.Description("Concise description of the decision, standard, or fix")),
		mcp.WithString("why", mcp.Required(), mcp.Description("Detailed reasoning for this choice")),
		mcp.WithString("learned", mcp.Required(), mcp.Description("Rule or key lesson to guide future agents")),
		mcp.WithString("where_path", mcp.Description("Optional file or folder path affected by this memory")),
		mcp.WithString("impact", mcp.Description("Achievements, successes, or what went well")),
		mcp.WithString("errors_faced", mcp.Description("Errors faced, roadblocks, or what went wrong")),
		mcp.WithString("next_steps", mcp.Description("Next actions or pending tasks to continue work")),
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

		mem := &memory.Memory{
			ID:          uuid.New().String()[:8], // Compact 8-char UUID
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
			CreatedAt:   time.Now(),
		}

		// Save locally in SQLite
		if err := memory.SaveMemory(db, mem); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to save memory to SQLite: %v", err)), nil
		}

		// Sync immediately to Git json file
		if err := memory.SyncToGit(db, cfg.ProjectID, cfg.ProjPath); err != nil {
			return mcp.NewToolResultText(fmt.Sprintf("Saved to local SQLite database (ID: %s) but failed Git Sync: %v", mem.ID, err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Successfully saved decision memory (ID: %s) and synced to Git workspace (.sv-memory/memories.json)", mem.ID)), nil
	})

	// 2. Tool: sv_mem_search
	searchTool := mcp.NewTool("sv_mem_search",
		mcp.WithDescription("Query the historical project decisions, architectural rules, and past bugfixes using keyword/FTS search."),
		mcp.WithString("query", mcp.Required(), mcp.Description("The keyword or phrase to search for")),
		mcp.WithString("category", mcp.Description("Optional category to filter results: 'bugfix' | 'architecture' | 'standard' | 'decision'")),
	)

	s.AddTool(searchTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query, err := req.RequireString("query")
		if err != nil {
			return mcp.NewToolResultError("missing required field: query"), nil
		}
		category := req.GetString("category", "")

		// Sync from Git first to load any new teammate memories
		_ = memory.SyncFromGit(db, cfg.ProjectID, cfg.ProjPath)

		memories, err := memory.SearchMemories(db, cfg.ProjectID, query, category)
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

	// 3. Tool: sv_graph_query
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

		// Check if graph is already populated for this project. If not, auto-build/sync it.
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ?", cfg.ProjectID).Scan(&count)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to check graph status: %v", err)), nil
		}
		if count == 0 {
			if err := graph.SyncGraph(db, cfg.ProjectID, cfg.ProjPath); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("failed to auto-build dependency graph: %v", err)), nil
			}
		}

		subGraph, err := querySubGraph(db, cfg.ProjectID, pathOrNode, depth)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to query graph: %v", err)), nil
		}

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

	// 4. Tool: sv_graph_sync
	graphSyncTool := mcp.NewTool("sv_graph_sync",
		mcp.WithDescription("Trigger a full re-scan of the project code directory and refresh the structural dependency graph stored in SQLite."),
	)

	s.AddTool(graphSyncTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		err := graph.SyncGraph(db, cfg.ProjectID, cfg.ProjPath)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to sync graph: %v", err)), nil
		}
		return mcp.NewToolResultText("Dependency graph refreshed and synchronized successfully in SQLite."), nil
	})

	// Start standard IO transport server using convenience function
	return server.ServeStdio(s)
}

type subGraph struct {
	Nodes []*graph.Node
	Edges []*graph.Edge
}

// querySubGraph performs a BFS traversal on the SQLite DB graph starting at node matching pathOrNode.
func querySubGraph(db *sql.DB, projectID string, start string, maxDepth int) (*subGraph, error) {
	// Find starting node ID
	var startID string
	err := db.QueryRow(`
		SELECT id FROM graph_nodes 
		WHERE project_id = ? AND (id = ? OR path = ? OR label = ?)
		LIMIT 1`, projectID, start, start, start).Scan(&startID)
	if err != nil {
		if err == sql.ErrNoRows {
			// Start node not found
			return &subGraph{Nodes: nil, Edges: nil}, nil
		}
		return nil, err
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

		// Load node
		var node graph.Node
		var metadataStr string
		err := db.QueryRow(`
			SELECT id, node_type, label, path, metadata 
			FROM graph_nodes 
			WHERE project_id = ? AND id = ?`, projectID, curr.id).Scan(&node.ID, &node.Type, &node.Label, &node.Path, &metadataStr)
		if err == nil {
			_ = json.Unmarshal([]byte(metadataStr), &node.Metadata)
			nodeMap[node.ID] = &node
		}

		if curr.depth >= maxDepth {
			continue
		}

		// Find edges where this node is source or target
		rows, err := db.Query(`
			SELECT id, source_id, target_id, relation_type 
			FROM graph_edges 
			WHERE project_id = ? AND (source_id = ? OR target_id = ?)`, projectID, curr.id, curr.id)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		for rows.Next() {
			var edge graph.Edge
			if err := rows.Scan(&edge.ID, &edge.SourceID, &edge.TargetID, &edge.RelationType); err == nil {
				edgeMap[edge.ID] = &edge

				// Push neighbor to queue
				neighbor := edge.TargetID
				if neighbor == curr.id {
					neighbor = edge.SourceID
				}
				if !visited[neighbor] {
					queue = append(queue, queueItem{id: neighbor, depth: curr.depth + 1})
				}
			}
		}
		rows.Close()
	}

	nodes := make([]*graph.Node, 0, len(nodeMap))
	for _, n := range nodeMap {
		nodes = append(nodes, n)
	}

	edges := make([]*graph.Edge, 0, len(edgeMap))
	for _, e := range edgeMap {
		edges = append(edges, e)
	}

	return &subGraph{Nodes: nodes, Edges: edges}, nil
}

func escapeMermaid(s string) string {
	// Replaces path slash/special chars with underscores or quotes for Mermaid
	s = strings.ReplaceAll(s, "\\", "/")
	return fmt.Sprintf(`"%s"`, s)
}
