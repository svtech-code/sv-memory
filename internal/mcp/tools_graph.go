package mcp

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/svtech-code/sv-memory/internal/db"
	"github.com/svtech-code/sv-memory/internal/graph"
	"github.com/svtech-code/sv-memory/internal/security"
)

//nolint:gocyclo // BFS query renders many report branches; refactor later
func (s *Server) handleGraphQuery(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	pathOrNode, err := req.RequireString("path_or_node")
	if err != nil {
		return mcp.NewToolResultError("missing required field: path_or_node"), nil
	}
	depthStr := req.GetString("depth", "1")
	depth := 1
	if depthStr != "" {
		if d, convErr := strconv.Atoi(depthStr); convErr == nil {
			depth = d
		}
	}
	if depth < 1 {
		depth = 1
	} else if depth > 5 {
		depth = 5
	}

	relationType := req.GetString("relation_type", "")
	direction := req.GetString("direction", "out")
	tokenBudget := resolveTokenBudget(req.GetString("token_budget", ""))

	// Load or retrieve the in-memory graph cache.
	startQuery := time.Now()
	g, err := s.getOrLoadGraph()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load graph: %v", err)), nil
	}

	hubThreshold := g.ComputeHubThreshold()
	subGraph := g.Query(pathOrNode, depth, relationType, direction, hubThreshold)
	debugLog("graph_query path=%q depth=%d hubThresh=%d returned %d nodes / %d edges in %s", pathOrNode, depth, hubThreshold, len(subGraph.Nodes), len(subGraph.Edges), time.Since(startQuery))

	if len(subGraph.Nodes) == 0 {
		return mcp.NewToolResultText(fmt.Sprintf("No nodes found matching '%s' in the project graph.", pathOrNode)), nil
	}

	commColors := []string{
		"#ECECFF", // light purple
		"#FFF0F5", // lavender blush
		"#F0FFF0", // honeydew
		"#F5F5DC", // beige
		"#FFF8DC", // cornsilk
		"#F0F8FF", // alice blue
		"#FDF5E6", // old lace
		"#FFF5EE", // seashell
		"#F5FFFA", // mint cream
		"#F0FFFF", // azure
	}

	commLabels := computeCommLabels(g)

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Code Sub-Graph for '%s' (Depth: %d)\n\n", pathOrNode, depth)

	sb.WriteString("### Nodes in Sub-graph:\n")
	for _, node := range subGraph.Nodes {
		cID := graph.NodeCommunityID(node)
		bc := graph.NodeBetweennessCentrality(node)
		commStr := commLabelStr(cID, commLabels)
		fmt.Fprintf(&sb, "- **%s** (`%s`): %s (fan-in: %d, fan-out: %d, community: %s, BC: %.2f)\n",
			node.Label, node.ID, node.Type, g.FanIn[node.ID], g.FanOut[node.ID], commStr, bc)
	}
	sb.WriteString("\n")

	// Identify potential God Nodes (degree > 10 or high betweenness centrality)
	var godNodes []string
	for _, node := range subGraph.Nodes {
		bc := graph.NodeBetweennessCentrality(node)
		if g.FanIn[node.ID] > 10 || g.FanOut[node.ID] > 10 || bc > 50.0 {
			godNodes = append(godNodes, node.ID)
		}
	}
	if len(godNodes) > 0 {
		sb.WriteString("### Potential God Nodes:\n")
		for _, id := range godNodes {
			bc := graph.NodeBetweennessCentrality(g.Nodes[id])
			fmt.Fprintf(&sb, "- **%s** (fan-in: %d, fan-out: %d, BC: %.2f)\n", g.Nodes[id].Label, g.FanIn[id], g.FanOut[id], bc)
		}
		sb.WriteString("\n")
	}

	if len(subGraph.Edges) > 0 {
		sb.WriteString("### Mermaid Dependency Diagram:\n")
		sb.WriteString("```mermaid\ngraph TD\n")
		for _, edge := range subGraph.Edges {
			srcEscaped := escapeMermaid(edge.SourceID)
			tgtEscaped := escapeMermaid(edge.TargetID)

			if edge.RelationType == "imports" {
				fmt.Fprintf(&sb, "    %s -->|imports| %s\n", srcEscaped, tgtEscaped)
			} else {
				fmt.Fprintf(&sb, "    %s -->|%s| %s\n", srcEscaped, edge.RelationType, tgtEscaped)
			}
		}

		for _, node := range subGraph.Nodes {
			cID := graph.NodeCommunityID(node)
			if cID > 0 {
				nodeEscaped := escapeMermaid(node.ID)
				color := commColors[(cID-1)%len(commColors)]
				fmt.Fprintf(&sb, "    style %s fill:%s,stroke:#333,stroke-width:1px\n", nodeEscaped, color)
			}
		}

		sb.WriteString("```\n")

		// Confidence breakdown
		var extracted, inferred, ambiguous int
		for _, edge := range subGraph.Edges {
			switch edge.Confidence {
			case "EXTRACTED":
				extracted++
			case "INFERRED":
				inferred++
			case "AMBIGUOUS":
				ambiguous++
			default:
				extracted++
			}
		}
		total := extracted + inferred + ambiguous
		if total > 0 {
			sb.WriteString("\n### Edge Confidence Breakdown:\n")
			fmt.Fprintf(&sb, "- **EXTRACTED:** %d (%d%%) — explicit in source\n", extracted, extracted*100/total)
			fmt.Fprintf(&sb, "- **INFERRED:** %d (%d%%) — derived by resolution\n", inferred, inferred*100/total)
			if ambiguous > 0 {
				fmt.Fprintf(&sb, "- **AMBIGUOUS:** %d (%d%%) — uncertain\n", ambiguous, ambiguous*100/total)
			}
		}
	} else {
		sb.WriteString("*No connections/edges found in this range.*\n")
	}

	benchmark := tokenBenchmark(subGraph.Nodes, sb.Len()/4)
	if benchmark != "" {
		sb.WriteString("\n" + benchmark + "\n")
	}

	responseText := sb.String()
	if tokenBudget > 0 && len(responseText) > tokenBudget*4 {
		totalNodes := len(subGraph.Nodes)
		totalEdges := len(subGraph.Edges)
		// Truncate to budget, ensure we don't cut mid-line
		maxChars := tokenBudget * 4
		truncated := responseText[:maxChars]
		// Find last newline to avoid cutting mid-line
		if lastNewline := strings.LastIndex(truncated, "\n"); lastNewline > 0 {
			truncated = truncated[:lastNewline]
		}
		responseText = fmt.Sprintf(
			"[!] TRUNCATED: showing ~%d chars (~%d tokens) of estimated %d total. The graph has %d nodes and %d edges. Narrow your query with depth, relation_type, direction, or increase token_budget.\n\n%s",
			maxChars, tokenBudget, len(responseText)/4, totalNodes, totalEdges, truncated)
	}

	return mcp.NewToolResultText(responseText), nil
}

func (s *Server) handleGraphPath(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	source, err := req.RequireString("source")
	if err != nil {
		return mcp.NewToolResultError("missing required field: source"), nil
	}
	target, err := req.RequireString("target")
	if err != nil {
		return mcp.NewToolResultError("missing required field: target"), nil
	}
	maxHopsStr := req.GetString("max_hops", "10")
	maxHops := 10
	if d, convErr := strconv.Atoi(maxHopsStr); convErr == nil {
		maxHops = d
	}

	g, err := s.getOrLoadGraph()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load graph: %v", err)), nil
	}

	// Find start and end nodes based on input (fuzzy match)
	startNode := g.FindNode(source)
	endNode := g.FindNode(target)

	if startNode == "" || endNode == "" {
		return mcp.NewToolResultText("Could not find start or end node in the graph."), nil
	}

	path := g.ShortestPath(startNode, endNode, maxHops)

	if len(path) == 0 {
		return mcp.NewToolResultText(fmt.Sprintf("No path found between %s and %s.", startNode, endNode)), nil
	}

	var pathParts []string
	for i := 0; i < len(path); i++ {
		node := g.Nodes[path[i]]
		label := path[i]
		if node != nil {
			label = node.Label
		}
		pathParts = append(pathParts, fmt.Sprintf("`%s`", label))

		if i+1 < len(path) {
			// Find edge between this node and next
			edgeInfo := ""
			for _, e := range g.EdgesBySource[path[i]] {
				if e.TargetID == path[i+1] {
					conf := e.Confidence
					if conf == "" {
						conf = "EXTRACTED"
					}
					edgeInfo = fmt.Sprintf(" --[%s %s]--> ", e.RelationType, conf)
					break
				}
			}
			if edgeInfo == "" {
				edgeInfo = " --> "
			}
			pathParts = append(pathParts, edgeInfo)
		}
	}

	return mcp.NewToolResultText(fmt.Sprintf("Shortest path (%d hops):\n%s", len(path)-1, strings.Join(pathParts, ""))), nil
}

func (s *Server) handleGraphSync(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	startSync := time.Now()
	synced, err := graph.SyncGraphIfStale(s.pool.Writer, s.cfg.ProjectID, s.cfg.ProjPath)
	debugLog("graph_sync rebuild took %s", time.Since(startSync))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to sync graph: %v", err)), nil
	}

	// Invalidate in-memory cache so the next sv_graph_query reloads fresh
	// data from the rebuilt graph tables. Communities and betweenness
	// centrality are computed lazily by sv_graph_explain / sv_graph_god_nodes
	// when they are missing, so sync itself stays cheap.
	graph.GlobalGraphCache.Invalidate(s.cfg.ProjectID)
	if !synced {
		return mcp.NewToolResultText("Dependency graph is already up to date (no file changes detected)."), nil
	}
	s.relinkMemoryRationales()
	return mcp.NewToolResultText("Dependency graph refreshed and synchronized successfully in SQLite."), nil
}

//nolint:gocyclo // rich node explanation renderer; refactor later
func (s *Server) handleGraphExplain(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeName, err := req.RequireString("node")
	if err != nil {
		return mcp.NewToolResultError("missing required field: node"), nil
	}

	g, err := s.getOrLoadGraph()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load graph: %v", err)), nil
	}

	nID := g.FindNode(nodeName)
	if nID == "" {
		return mcp.NewToolResultText(fmt.Sprintf("Could not find node matching '%s' in the graph.", nodeName)), nil
	}

	node := g.Nodes[nID]

	// Lazy-calculate communities and centrality if missing
	var hasBC bool
	if node.Metadata != nil {
		_, hasBC = node.Metadata["betweenness_centrality"]
	}
	if !hasBC {
		debugLog("betweenness_centrality missing, calculating communities and centrality dynamically...")
		s.computeCentralityIfMissing()
		g, _ = s.getOrLoadGraph()
		node = g.Nodes[nID]
	}

	commLabels := computeCommLabels(g)
	cID := graph.NodeCommunityID(node)
	bc := graph.NodeBetweennessCentrality(node)
	fanIn := g.FanIn[nID]
	fanOut := g.FanOut[nID]

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Structural Explanation of Node: `%s`\n\n", node.Label)
	fmt.Fprintf(&sb, "- **Node ID:** `%s`\n", node.ID)
	fmt.Fprintf(&sb, "- **Type:** `%s`\n", node.Type)
	if node.Path != "" {
		fmt.Fprintf(&sb, "- **Path:** `%s`\n", node.Path)
	}

	if node.Metadata != nil {
		if lang, ok := node.Metadata["language"]; ok {
			fmt.Fprintf(&sb, "- **Language:** `%v`\n", lang)
		}
		if loc, ok := node.Metadata["loc"]; ok {
			fmt.Fprintf(&sb, "- **Lines of Code (LOC):** `%v`\n", loc)
		}
	}

	sb.WriteString("\n### 📊 Network Metrics:\n")
	fmt.Fprintf(&sb, "- **Community:** `%s`\n", commLabelStr(cID, commLabels))
	fmt.Fprintf(&sb, "- **Betweenness Centrality (BC):** `%.2f`\n", bc)
	fmt.Fprintf(&sb, "- **Fan-In (Dependents):** `%d`\n", fanIn)
	fmt.Fprintf(&sb, "- **Fan-Out (Dependencies):** `%d`\n", fanOut)

	// God node evaluation
	isGod := false
	reasons := []string{}
	if fanIn > 10 {
		isGod = true
		reasons = append(reasons, fmt.Sprintf("High Fan-In (%d dependents)", fanIn))
	}
	if fanOut > 10 {
		isGod = true
		reasons = append(reasons, fmt.Sprintf("High Fan-Out (%d dependencies)", fanOut))
	}
	if bc > 50.0 {
		isGod = true
		reasons = append(reasons, fmt.Sprintf("High Betweenness Centrality (%.2f) - acts as a structural bridge", bc))
	}

	sb.WriteString("\n### 🧠 Architectural Role:\n")
	if isGod {
		sb.WriteString("⚠️ **Potential God Node/Hub:** This node plays a central role in the codebase due to:\n")
		for _, r := range reasons {
			fmt.Fprintf(&sb, "  - %s\n", r)
		}
		sb.WriteString("Refactoring this node could have significant ripple effects. Consider splitting its responsibilities.\n")
	} else if fanIn == 0 && fanOut > 0 {
		sb.WriteString("🟢 **Entry Point / Controller:** This node has dependencies but no local dependents. It is likely an entry point or top-level controller.\n")
	} else if fanIn > 0 && fanOut == 0 {
		sb.WriteString("🟢 **Leaf Node / Utility:** This node is depended upon by others but has no dependencies. It represents a low-level utility or core data model.\n")
	} else if fanIn > 0 && fanOut > 0 {
		sb.WriteString("🟢 **Intermediate Component:** This node acts as an intermediary, receiving calls/imports and delegating to low-level utilities.\n")
	} else {
		sb.WriteString("🟢 **Isolated Node:** This node currently has no connections in the dependency graph.\n")
	}

	// Actionable suggestions (E1): turn the metrics above into concrete next
	// steps the agent can take without re-deriving them.
	sb.WriteString("\n### 💡 Actionable Suggestions:\n")
	switch {
	case isGod:
		sb.WriteString("- **Refactor risk:** 🔴 **HIGH** (hub node)\n")
		sb.WriteString("- Before changing this node, map ripple effects: run `sv_graph_query` on the key dependents/dependencies listed above and `sv_graph_path` to trace worst-case cascades.\n")
		sb.WriteString("- Consider splitting its responsibilities along community boundaries (see community in Network Metrics).\n")
	case fanIn > 5 || fanOut > 8:
		sb.WriteString("- **Refactor risk:** 🟠 **MEDIUM** (above-average connectivity)\n")
		sb.WriteString("- Prefer additive changes (new exported function/type) over signature changes to limit breakage.\n")
	default:
		sb.WriteString("- **Refactor risk:** 🟢 **LOW**\n")
	}
	if fanIn > 0 && fanOut == 0 {
		sb.WriteString("- Low-level utility: changing its signature may break every dependent listed above — review them first.\n")
	}
	if fanIn == 0 && fanOut > 0 {
		sb.WriteString("- Likely an entry point: renaming is safe, but verify launch/CLI references before doing it.\n")
	}
	if fanOut > fanIn {
		sb.WriteString("- Depends on more modules than depend on it: upstream changes could cascade into this node.\n")
	}
	if bc > 50.0 {
		sb.WriteString("- High betweenness: this node bridges communities. Removing it could disconnect otherwise-unrelated parts of the codebase.\n")
	}

	// Neighbors
	sb.WriteString("\n### 🔗 Immediate Neighbors:\n")
	var dependents []string
	var rationales []string
	for _, e := range g.EdgesByTarget[nID] {
		if e.RelationType == "rationale_for" {
			srcNode := g.Nodes[e.SourceID]
			if srcNode != nil {
				if isMemory, _ := srcNode.Metadata["memory"].(bool); isMemory {
					// Memory-backed rationale: the source is a saved sv_mem_save
					// decision, not a code comment. Surface it as a decision link.
					rationales = append(rationales, fmt.Sprintf("- **Memory/Decision:** `%s` (ID: `%s`)\n  → run `sv_mem_get(id=\"%s\")` for full context",
						srcNode.Label, srcNode.ID, srcNode.ID))
					continue
				}
				lineVal := 0
				if srcNode.Metadata != nil {
					if l, ok := srcNode.Metadata["line"]; ok {
						if lf, ok := l.(float64); ok {
							lineVal = int(lf)
						} else if li, ok := l.(int); ok {
							lineVal = li
						}
					}
				}
				rationales = append(rationales, fmt.Sprintf("- **Line %d:** `%s`", lineVal, srcNode.Label))
			}
		} else {
			srcNode := g.Nodes[e.SourceID]
			if srcNode != nil {
				conf := e.Confidence
				if conf == "" {
					conf = "EXTRACTED"
				}
				dependents = append(dependents, fmt.Sprintf("- `%s` (relation: `%s`, confidence: `%s`)", srcNode.Label, e.RelationType, conf))
			}
		}
	}

	if len(dependents) > 0 {
		sb.WriteString("**Dependents (Who imports/calls this):**\n")
		for _, dep := range dependents {
			sb.WriteString(dep + "\n")
		}
	} else {
		sb.WriteString("**Dependents:** None.\n")
	}

	if len(g.EdgesBySource[nID]) > 0 {
		sb.WriteString("\n**Dependencies (What this imports/calls):**\n")
		for _, e := range g.EdgesBySource[nID] {
			tgtNode := g.Nodes[e.TargetID]
			if tgtNode != nil {
				conf := e.Confidence
				if conf == "" {
					conf = "EXTRACTED"
				}
				fmt.Fprintf(&sb, "- `%s` (relation: `%s`, confidence: `%s`)\n", tgtNode.Label, e.RelationType, conf)
			}
		}
	} else {
		sb.WriteString("\n**Dependencies:** None.\n")
	}

	if len(rationales) > 0 {
		sb.WriteString("\n### 💡 Code Rationales (Empirical Decisions in Comments):\n")
		for _, r := range rationales {
			sb.WriteString(r + "\n")
		}
	}

	// Token benchmark
	allNodes := []*graph.Node{node}
	for _, e := range g.EdgesBySource[nID] {
		if n, ok := g.Nodes[e.TargetID]; ok {
			allNodes = append(allNodes, n)
		}
	}
	for _, e := range g.EdgesByTarget[nID] {
		if n, ok := g.Nodes[e.SourceID]; ok {
			allNodes = append(allNodes, n)
		}
	}
	benchmark := tokenBenchmark(allNodes, sb.Len()/4)
	if benchmark != "" {
		sb.WriteString("\n" + benchmark + "\n")
	}

	sb.WriteString("\n### ❓ Suggested Questions to ask Antigravity about this node:\n")
	fmt.Fprintf(&sb, "1. \"What is the primary responsibility of `%s`?\"\n", node.Label)
	fmt.Fprintf(&sb, "2. \"Are there any architectural patterns or guidelines used inside `%s`?\"\n", node.Label)
	if isGod {
		fmt.Fprintf(&sb, "3. \"How can we refactor or break down the God node `%s` into smaller modules?\"\n", node.Label)
	} else if fanIn > 0 {
		fmt.Fprintf(&sb, "3. \"Who are the main dependents of `%s` and how would a change here affect them?\"\n", node.Label)
	} else {
		fmt.Fprintf(&sb, "3. \"Show me the source code implementation details of `%s`.\"\n", node.Label)
	}

	return s.respond(req, sb.String()), nil
}

func (s *Server) handleGodNodes(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	topNStr := req.GetString("top_n", "10")
	topN := 10
	if d, err := strconv.Atoi(topNStr); err == nil && d > 0 {
		topN = d
	}
	if topN > 100 {
		topN = 100
	}

	g, err := s.getOrLoadGraph()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load graph: %v", err)), nil
	}

	// Lazy-calculate communities and centrality if missing
	hasBC := false
	for _, node := range g.Nodes {
		if node.Metadata != nil {
			if _, ok := node.Metadata["betweenness_centrality"]; ok {
				hasBC = true
				break
			}
		}
	}
	if !hasBC {
		debugLog("betweenness_centrality missing, calculating communities and centrality...")
		s.computeCentralityIfMissing()
	}
	type rankedNode struct {
		id     string
		node   *graph.Node
		degree int
		bc     float64
		comm   int
	}
	var ranked []rankedNode
	for id, n := range g.Nodes {
		deg := g.FanIn[id] + g.FanOut[id]
		ranked = append(ranked, rankedNode{
			id:     id,
			node:   n,
			degree: deg,
			bc:     graph.NodeBetweennessCentrality(n),
			comm:   graph.NodeCommunityID(n),
		})
	}
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].degree > ranked[j].degree
	})
	if topN > len(ranked) {
		topN = len(ranked)
	}
	ranked = ranked[:topN]

	commLabels := computeCommLabels(g)

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Top %d God Nodes (Most Connected)\n\n", topN)
	sb.WriteString("Ranked by total degree (fan-in + fan-out). High-degree nodes are architectural hubs.\n\n")
	sb.WriteString("| Rank | Label | Type | Degree | Fan-In | Fan-Out | BC | Community |\n")
	sb.WriteString("|------|-------|------|--------|--------|---------|----|-----------|\n")
	for i, r := range ranked {
		commStr := strconv.Itoa(r.comm)
		if r.comm == 0 {
			commStr = "none"
		} else if label, ok := commLabels[r.comm]; ok {
			commStr = fmt.Sprintf("%s (%d)", label, r.comm)
		}
		fmt.Fprintf(&sb, "| %d | **%s** | `%s` | %d | %d | %d | %.2f | %s |\n",
			i+1, r.node.Label, r.node.Type, r.degree, g.FanIn[r.id], g.FanOut[r.id], r.bc, commStr)
	}
	sb.WriteString("\n*Use `sv_graph_explain` on any node for deeper analysis.*\n")

	var allNodes []*graph.Node
	for _, r := range ranked {
		allNodes = append(allNodes, r.node)
	}
	benchmark := tokenBenchmark(allNodes, sb.Len()/4)
	if benchmark != "" {
		sb.WriteString("\n" + benchmark + "\n")
	}

	return s.respond(req, sb.String()), nil
}

func (s *Server) handleSurprisingConnections(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	startLoad := time.Now()
	g, err := s.getOrLoadGraph()
	debugLog("graph_load took %s", time.Since(startLoad))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load graph: %v", err)), nil
	}

	limitStr := req.GetString("limit", "10")
	limit := 10
	if d, err := strconv.Atoi(limitStr); err == nil && d > 0 {
		limit = d
	}
	if limit > 50 {
		limit = 50
	}

	if g.BetweennessCentrality() == nil {
		return mcp.NewToolResultError("centrality data not available"), nil
	}

	centrality := g.BetweennessCentrality()
	communities := g.ExtractCommunities()
	if len(communities) == 0 {
		return mcp.NewToolResultText("No community data found. Run community detection first."), nil
	}

	conns := g.FindSurprisingConnections(communities, centrality, limit)

	if len(conns) == 0 {
		return mcp.NewToolResultText("No surprising cross-community connections found."), nil
	}

	commLabels := computeCommLabels(g, centrality)

	// Rank the top connection once so it can be highlighted in a summary line
	// before the full table (E3: bridge-score formatting for the LLM).
	type scoredConn struct {
		idx   int
		score float64
		src   string
		tgt   string
		etype string
		srcC  string
		dstC  string
	}
	rowsFormatted := make([]scoredConn, 0, len(conns))
	for i, c := range conns {
		rowsFormatted = append(rowsFormatted, scoredConn{
			idx:   i + 1,
			score: c.SurpriseScore,
			src:   c.SourceLabel,
			tgt:   c.TargetLabel,
			etype: c.EdgeType,
			srcC:  commLabelStr(c.SrcCommunity, commLabels),
			dstC:  commLabelStr(c.DstCommunity, commLabels),
		})
	}
	best := rowsFormatted[0]
	for _, r := range rowsFormatted[1:] {
		if r.score > best.score {
			best = r
		}
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Surprising Connections (Top %d)\n\n", len(conns))
	fmt.Fprintf(&sb, "**%d cross-community bridge(s)** linking otherwise separate parts of the codebase.\n\n", len(conns))
	fmt.Fprintf(&sb, "🔥 **Most surprising bridge:** `%s` → `%s` (`%s`, score **%.2f**) between **%s** and **%s**.\n",
		best.src, best.tgt, best.etype, best.score, best.srcC, best.dstC)
	sb.WriteString("Drill down with `sv_graph_path(source=\"<node>\", target=\"<node>\")` to trace the full dependency chain.\n\n")

	sb.WriteString("| Rank | Source | Target | Edge Type | Surprise Score | Communities |\n")
	sb.WriteString("|------|--------|--------|-----------|----------------|-------------|\n")
	for _, r := range rowsFormatted {
		fmt.Fprintf(&sb, "| %d | **%s** | **%s** | `%s` | %.2f | %s ↔ %s |\n",
			r.idx, r.src, r.tgt, r.etype, r.score, r.srcC, r.dstC)
	}

	sb.WriteString("\n*Higher surprise score means a more unexpected bridge between communities.*\n")
	return s.respond(req, sb.String()), nil
}

func (s *Server) handleGraphViz(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	g, err := s.getOrLoadGraph()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load graph: %v", err)), nil
	}

	output := req.GetString("output", "graph.html")
	output, err = security.ValidateWritePath(s.cfg.ProjPath, output)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid output path: %v", err)), nil
	}

	comms := g.LeidenDetectCommunities()
	centrality := g.BetweennessCentrality()
	commLabels := g.DetectCommunityLabels(comms, centrality)

	var buf strings.Builder
	if err := g.ExportHTML(&buf, comms, commLabels); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to generate HTML: %v", err)), nil
	}

	if err := os.WriteFile(output, []byte(buf.String()), 0644); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to write %s: %v", output, err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Graph visualization exported to %s (%d bytes)", output, buf.Len())), nil
}

func (s *Server) handleGraphMerge(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	projA, err := req.RequireString("project_a")
	if err != nil {
		return mcp.NewToolResultError("missing required field: project_a"), nil
	}
	projB, err := req.RequireString("project_b")
	if err != nil {
		return mcp.NewToolResultError("missing required field: project_b"), nil
	}
	output := req.GetString("output", "")
	if output != "" {
		output, err = security.ValidateWritePath(s.cfg.ProjPath, output)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid output path: %v", err)), nil
		}
	}

	mergeDB, err := db.InitDB(s.cfg.DBPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to open DB: %v", err)), nil
	}
	defer mergeDB.Close()

	ga, err := graph.LoadFullGraph(mergeDB, projA)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load project %s: %v", projA, err)), nil
	}
	gb, err := graph.LoadFullGraph(mergeDB, projB)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load project %s: %v", projB, err)), nil
	}

	merged := ga.Merge(gb)
	jsonStr := merged.SerializeJSON()

	if output != "" {
		if err := os.WriteFile(output, []byte(jsonStr), 0644); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to write %s: %v", output, err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Merged graph exported to %s (%d nodes, %d edges)", output, len(merged.Nodes), len(merged.EdgesBySource))), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Merged graph: %d nodes, %d edges\n\n```json\n%s\n```",
		len(merged.Nodes), len(merged.EdgesBySource), jsonStr)), nil
}

func escapeMermaid(s string) string {
	s = strings.ReplaceAll(s, "\\", "/")
	return fmt.Sprintf(`"%s"`, s)
}

// computeCommLabels extracts community labels from the graph by finding the
// most central node in each community and using its label as the community name.
// Pass a pre-computed centrality map if available to avoid re-computation.
func computeCommLabels(g *graph.InMemoryGraph, centrality ...map[string]float64) map[int]string {
	communities := g.ExtractCommunities()
	if len(communities) == 0 {
		return nil
	}
	var bc map[string]float64
	if len(centrality) > 0 && centrality[0] != nil {
		bc = centrality[0]
	} else {
		bc = g.BetweennessCentrality()
	}
	return g.DetectCommunityLabels(communities, bc)
}

// tokenBenchmark estimates token savings vs reading raw files.
func tokenBenchmark(nodes []*graph.Node, responseTokens int) string {
	totalLOC := 0
	for _, n := range nodes {
		if n.Metadata == nil {
			totalLOC += 50
			continue
		}
		loc, ok := n.Metadata["loc"]
		if !ok {
			totalLOC += 50
			continue
		}
		switch v := loc.(type) {
		case float64:
			totalLOC += int(v)
		case int:
			totalLOC += v
		case int64:
			totalLOC += int(v)
		default:
			totalLOC += 50
		}
	}
	rawTokens := totalLOC * 4
	if rawTokens <= 0 || responseTokens <= 0 {
		return ""
	}
	savings := float64(rawTokens) / float64(responseTokens)
	return fmt.Sprintf("*Token savings: ~%.0fx vs reading raw files (%d tokens vs %d tokens)*",
		savings, rawTokens, responseTokens)
}

// commLabelStr returns a formatted community string: "Label (ID N)" or "none".
func commLabelStr(commID int, labels map[int]string) string {
	if commID == 0 {
		return "none"
	}
	if label, ok := labels[commID]; ok {
		return fmt.Sprintf("%s (ID %d)", label, commID)
	}
	return fmt.Sprintf("community_%d", commID)
}
