package mcp

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/svtech-code/sv-memory/internal/graph"
)

func (s *Server) handleGraphQuery(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	pathOrNode, err := req.RequireString("path_or_node")
	if err != nil {
		return mcp.NewToolResultError("missing required field: path_or_node"), nil
	}
	depth := clampRange(req.GetString("depth", "1"), 1, 1, 5)
	relationType := req.GetString("relation_type", "")
	direction := req.GetString("direction", "out")
	confidenceFilter := req.GetString("confidence", "")
	mermaidFlag := req.GetString("mermaid", "") == "true"

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
		return mcp.NewToolResultText(fmt.Sprintf("No nodes found matching '%s' in the project graph.\nDid you mean to discover matches? Call sv_graph_search(query=\"%s\").", pathOrNode, pathOrNode)), nil
	}

	commLabels := computeCommLabels(g)

	// Apply optional confidence filter: keep only edges matching the requested
	// confidence level. Nodes remain in the subgraph for context, but only
	// matching edges are rendered.
	if confidenceFilter != "" {
		subGraph.Edges = filterEdgesByConfidence(subGraph.Edges, confidenceFilter)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Code Sub-Graph for '%s' (Depth: %d)\n\n", pathOrNode, depth)
	renderSubGraphNodes(&sb, g, subGraph.Nodes, commLabels)
	renderGodNodes(&sb, g, godNodeIDs(g, subGraph.Nodes))
	renderEdgesSection(&sb, subGraph.Edges, subGraph.Nodes, mermaidFlag)

	// Route through s.respond so the shared token-budget truncation applies
	// and the estimated tokens are accrued into the session token ledger
	// (sv_mem_stats reports them).
	return s.respond(req, sb.String()), nil
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
	maxHops := clampRange(req.GetString("max_hops", strconv.Itoa(graph.DefaultMaxHops)), graph.DefaultMaxHops, 1, 20)

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

	return s.respond(req, fmt.Sprintf("Shortest path (%d hops):\n%s", len(path)-1, renderGraphPath(g, path))), nil
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
	s.relinkSpecCapabilities()
	return mcp.NewToolResultText("Dependency graph refreshed and synchronized successfully in SQLite."), nil
}

// renderGraphPath joins the node labels and edge arrows of a shortest path into
// a single human-readable chain.
func renderGraphPath(g *graph.InMemoryGraph, path []string) string {
	var pathParts []string
	for i := 0; i < len(path); i++ {
		label := path[i]
		if node := g.Nodes[path[i]]; node != nil {
			label = node.Label
		}
		pathParts = append(pathParts, fmt.Sprintf("`%s`", label))
		if i+1 < len(path) {
			pathParts = append(pathParts, edgeArrow(g, path[i], path[i+1]))
		}
	}
	return strings.Join(pathParts, "")
}

// edgeArrow renders the annotated arrow between two consecutive path nodes,
// falling back to a plain arrow when no edge record is found.
func edgeArrow(g *graph.InMemoryGraph, from, to string) string {
	for _, e := range g.EdgesBySource[from] {
		if e.TargetID == to {
			return fmt.Sprintf(" --[%s %s]--> ", e.RelationType, confidenceOrExtracted(e.Confidence))
		}
	}
	return " --> "
}

// filterEdgesByConfidence keeps only edges whose (defaulted) confidence equals
// the requested level.
func filterEdgesByConfidence(edges []*graph.Edge, filter string) []*graph.Edge {
	var filtered []*graph.Edge
	for _, e := range edges {
		if confidenceOrExtracted(e.Confidence) == filter {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

func renderSubGraphNodes(sb *strings.Builder, g *graph.InMemoryGraph, nodes []*graph.Node, commLabels map[int]string) {
	sb.WriteString("### Nodes in Sub-graph:\n")
	for _, node := range nodes {
		cID := graph.NodeCommunityID(node)
		bc := graph.NodeBetweennessCentrality(node)
		commStr := commLabelStr(cID, commLabels)
		fmt.Fprintf(sb, "- **%s** (`%s`): %s (fan-in: %d, fan-out: %d, community: %s, BC: %.2f)\n",
			node.Label, node.ID, node.Type, g.FanIn[node.ID], g.FanOut[node.ID], commStr, bc)
	}
	sb.WriteString("\n")
}

// godNodeIDs identifies potential God Nodes (degree > 10 or high betweenness
// centrality) among the given nodes.
func godNodeIDs(g *graph.InMemoryGraph, nodes []*graph.Node) []string {
	var ids []string
	for _, node := range nodes {
		bc := graph.NodeBetweennessCentrality(node)
		if g.FanIn[node.ID] > 10 || g.FanOut[node.ID] > 10 || bc > 50.0 {
			ids = append(ids, node.ID)
		}
	}
	return ids
}

func renderGodNodes(sb *strings.Builder, g *graph.InMemoryGraph, ids []string) {
	if len(ids) == 0 {
		return
	}
	sb.WriteString("### Potential God Nodes:\n")
	for _, id := range ids {
		bc := graph.NodeBetweennessCentrality(g.Nodes[id])
		fmt.Fprintf(sb, "- **%s** (fan-in: %d, fan-out: %d, BC: %.2f)\n", g.Nodes[id].Label, g.FanIn[id], g.FanOut[id], bc)
	}
	sb.WriteString("\n")
}

// renderEdgesSection renders either a Mermaid diagram or the compact edge list,
// followed by the confidence breakdown, or a no-edges note when empty.
func renderEdgesSection(sb *strings.Builder, edges []*graph.Edge, nodes []*graph.Node, mermaid bool) {
	if len(edges) == 0 {
		sb.WriteString("*No connections/edges found in this range.*\n")
		return
	}
	if mermaid {
		renderMermaidEdges(sb, edges, nodes)
	} else {
		// Default: a compact, LLM-friendly edge list instead of the Mermaid
		// block, which is token-heavy and rarely rendered by an agent.
		sb.WriteString("### Edges:\n")
		for _, edge := range edges {
			fmt.Fprintf(sb, "- `%s` →[%s]→ `%s` (%s)\n",
				edge.SourceID, edge.RelationType, edge.TargetID, edge.Confidence)
		}
		sb.WriteString("\n")
	}
	renderConfidenceBreakdown(sb, edges)
}

func renderMermaidEdges(sb *strings.Builder, edges []*graph.Edge, nodes []*graph.Node) {
	commColors := []string{
		"#ECECFF", "#FFF0F5", "#F0FFF0", "#F5F5DC", "#FFF8DC",
		"#F0F8FF", "#FDF5E6", "#FFF5EE", "#F5FFFA", "#F0FFFF",
	}
	sb.WriteString("### Mermaid Dependency Diagram:\n")
	sb.WriteString("```mermaid\ngraph TD\n")
	for _, edge := range edges {
		srcEscaped := escapeMermaid(edge.SourceID)
		tgtEscaped := escapeMermaid(edge.TargetID)
		if edge.RelationType == "imports" {
			fmt.Fprintf(sb, "    %s -->|imports| %s\n", srcEscaped, tgtEscaped)
		} else {
			fmt.Fprintf(sb, "    %s -->|%s| %s\n", srcEscaped, edge.RelationType, tgtEscaped)
		}
	}
	for _, node := range nodes {
		if cID := graph.NodeCommunityID(node); cID > 0 {
			color := commColors[(cID-1)%len(commColors)]
			fmt.Fprintf(sb, "    style %s fill:%s,stroke:#333,stroke-width:1px\n", escapeMermaid(node.ID), color)
		}
	}
	sb.WriteString("```\n")
}

func renderConfidenceBreakdown(sb *strings.Builder, edges []*graph.Edge) {
	var extracted, inferred, ambiguous int
	for _, edge := range edges {
		switch edge.Confidence {
		case "INFERRED":
			inferred++
		case "AMBIGUOUS":
			ambiguous++
		default:
			extracted++
		}
	}
	total := extracted + inferred + ambiguous
	if total == 0 {
		return
	}
	sb.WriteString("\n### Edge Confidence Breakdown:\n")
	fmt.Fprintf(sb, "- **EXTRACTED:** %d (%d%%) — explicit in source\n", extracted, extracted*100/total)
	fmt.Fprintf(sb, "- **INFERRED:** %d (%d%%) — derived by resolution\n", inferred, inferred*100/total)
	if ambiguous > 0 {
		fmt.Fprintf(sb, "- **AMBIGUOUS:** %d (%d%%) — uncertain\n", ambiguous, ambiguous*100/total)
	}
}

// confidenceOrExtracted defaults an empty edge confidence to EXTRACTED, the
// implicit level for every edge written before confidence was tracked.
func confidenceOrExtracted(conf string) string {
	if conf == "" {
		return "EXTRACTED"
	}
	return conf
}

// ensureCentralityGraph lazily computes communities/centrality when missing and
// reloads the graph so the caller's local copy carries the fresh metrics.
func (s *Server) ensureCentralityGraph(g *graph.InMemoryGraph) (*graph.InMemoryGraph, error) {
	if graphHasCentrality(g) {
		return g, nil
	}
	debugLog("betweenness_centrality missing, calculating communities and centrality...")
	s.computeCentralityIfMissing()
	return s.getOrLoadGraph()
}

// graphHasCentrality reports whether any graph node carries betweenness
// centrality metadata, the signal used to decide whether the lazy
// compute+reload path is needed.
func graphHasCentrality(g *graph.InMemoryGraph) bool {
	for _, node := range g.Nodes {
		if node.Metadata != nil {
			if _, ok := node.Metadata["betweenness_centrality"]; ok {
				return true
			}
		}
	}
	return false
}

// groupCommunityMembers inverts the node->community map into a community->nodes
// map, with each member list sorted for deterministic output.
func groupCommunityMembers(communities map[string]int) map[int][]string {
	members := make(map[int][]string)
	for id, cid := range communities {
		members[cid] = append(members[cid], id)
	}
	for cid := range members {
		sort.Strings(members[cid])
	}
	return members
}

// communityInfo pairs a community id with its member count for ranking.
type communityInfo struct {
	id    int
	count int
}

// typeSuffix renders the optional node_type filter suffix for the no-match
// message ("of type 'file'") or empty when no filter was given.
func typeSuffix(nodeType string) string {
	if nodeType == "" {
		return ""
	}
	return fmt.Sprintf(" of type '%s'", nodeType)
}

// codeOrPlain wraps a value in backticks unless it is empty, to keep table
// cells readable.
func codeOrPlain(s string) string {
	if s == "" {
		return "—"
	}
	return "`" + s + "`"
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

// positiveInt parses a request string into a positive integer, falling back to
// def when empty, invalid, or non-positive.
func positiveInt(raw string, def int) int {
	if d, err := strconv.Atoi(raw); err == nil && d > 0 {
		return d
	}
	return def
}

// clampRange parses raw into an integer defaulting to def and clamps it to
// [min, max].
func clampRange(raw string, def, min, max int) int {
	v := def
	if d, err := strconv.Atoi(raw); err == nil {
		v = d
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
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
