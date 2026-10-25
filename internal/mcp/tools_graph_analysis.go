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

func (s *Server) handleGodNodes(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	topN := positiveInt(req.GetString("top_n", strconv.Itoa(graph.DefaultTopCommunities)), graph.DefaultTopCommunities)
	if topN > 100 {
		topN = 100
	}

	g, err := s.getOrLoadGraph()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load graph: %v", err)), nil
	}

	// Lazy-calculate communities and centrality if missing.
	if g, err = s.ensureCentralityGraph(g); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load graph: %v", err)), nil
	}

	ranked := rankNodesByDegree(g)
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
		fmt.Fprintf(&sb, "| %d | **%s** | `%s` | %d | %d | %d | %.2f | %s |\n",
			i+1, r.node.Label, r.node.Type, r.degree, g.FanIn[r.id], g.FanOut[r.id], r.bc, godNodeCommunityStr(r, commLabels))
	}
	sb.WriteString("\n*Use `sv_graph_explain` on any node for deeper analysis.*\n")

	var allNodes []*graph.Node
	for _, r := range ranked {
		allNodes = append(allNodes, r.node)
	}
	if benchmark := tokenBenchmark(allNodes, sb.Len()/4); benchmark != "" {
		sb.WriteString("\n" + benchmark + "\n")
	}

	return s.respond(req, sb.String()), nil
}

// rankedNode is a node decorated with its degree, betweenness centrality, and
// community id for the god-node ranking.
type rankedNode struct {
	id     string
	node   *graph.Node
	degree int
	bc     float64
	comm   int
}

// rankNodesByDegree returns every non-package node sorted by total degree
// (fan-in + fan-out) descending. External package nodes (pkg:*) are imports
// with high fan-in, not architectural hubs, so they are excluded.
func rankNodesByDegree(g *graph.InMemoryGraph) []rankedNode {
	var ranked []rankedNode
	for id, n := range g.Nodes {
		if n.Type == "package" {
			continue
		}
		ranked = append(ranked, rankedNode{
			id:     id,
			node:   n,
			degree: g.FanIn[id] + g.FanOut[id],
			bc:     graph.NodeBetweennessCentrality(n),
			comm:   graph.NodeCommunityID(n),
		})
	}
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].degree > ranked[j].degree
	})
	return ranked
}

func godNodeCommunityStr(r rankedNode, commLabels map[int]string) string {
	if r.comm == 0 {
		return "none"
	}
	if label, ok := commLabels[r.comm]; ok {
		return fmt.Sprintf("%s (%d)", label, r.comm)
	}
	return strconv.Itoa(r.comm)
}

func (s *Server) handleSurprisingConnections(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	startLoad := time.Now()
	g, err := s.getOrLoadGraph()
	debugLog("graph_load took %s", time.Since(startLoad))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load graph: %v", err)), nil
	}

	limit := positiveInt(req.GetString("limit", "10"), 10)
	if limit > 50 {
		limit = 50
	}

	// Lazily compute and reload when betweenness centrality is missing, like
	// the other graph tools, so a freshly-synced graph does not error here.
	if g, err = s.ensureCentralityGraph(g); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load graph: %v", err)), nil
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

	rowsFormatted := formatSurprisingConnections(conns, commLabels)
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

	sb.WriteString("| Rank | Source | Target | Edge Type | Confidence | Surprise Score | Communities |\n")
	sb.WriteString("|------|--------|--------|-----------|------------|----------------|-------------|\n")
	for _, r := range rowsFormatted {
		fmt.Fprintf(&sb, "| %d | **%s** | **%s** | `%s` | `%s` | %.2f | %s ↔ %s |\n",
			r.idx, r.src, r.tgt, r.etype, r.conf, r.score, r.srcC, r.dstC)
	}

	sb.WriteString("\n*Higher surprise score means a more unexpected bridge between communities.*\n")
	return s.respond(req, sb.String()), nil
}

// scoredConn is a pre-formatted surprising connection row.
type scoredConn struct {
	idx   int
	score float64
	src   string
	tgt   string
	etype string
	conf  string
	srcC  string
	dstC  string
}

func formatSurprisingConnections(conns []graph.SurprisingConnection, commLabels map[int]string) []scoredConn {
	rows := make([]scoredConn, 0, len(conns))
	for i, c := range conns {
		rows = append(rows, scoredConn{
			idx:   i + 1,
			score: c.SurpriseScore,
			src:   c.SourceLabel,
			tgt:   c.TargetLabel,
			etype: c.EdgeType,
			conf:  confidenceOrExtracted(c.Confidence),
			srcC:  commLabelStr(c.SrcCommunity, commLabels),
			dstC:  commLabelStr(c.DstCommunity, commLabels),
		})
	}
	return rows
}

func (s *Server) handleGraphCommunities(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	g, err := s.getOrLoadGraph()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load graph: %v", err)), nil
	}

	// Lazily compute and reload communities/centrality when missing, so a
	// freshly-synced graph (no persisted community_id) still works here.
	if g, err = s.ensureCentralityGraph(g); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load graph: %v", err)), nil
	}

	communities := g.ExtractCommunities()
	if len(communities) == 0 {
		return mcp.NewToolResultText("No communities found in the project graph."), nil
	}

	members := groupCommunityMembers(communities)
	commLabels := computeCommLabels(g)

	if commIDStr := req.GetString("community_id", ""); commIDStr != "" {
		return s.renderCommunityDetail(req, g, members, commIDStr, commLabels)
	}
	return s.renderCommunityList(req, g, members, commLabels, positiveInt(req.GetString("top_n", strconv.Itoa(graph.DefaultTopCommunities)), graph.DefaultTopCommunities))
}

// renderCommunityDetail renders a single community's member table.
func (s *Server) renderCommunityDetail(req mcp.CallToolRequest, g *graph.InMemoryGraph, members map[int][]string, commIDStr string, commLabels map[int]string) (*mcp.CallToolResult, error) {
	commID, convErr := strconv.Atoi(commIDStr)
	if convErr != nil || commID <= 0 {
		return mcp.NewToolResultError(fmt.Sprintf("invalid community_id: %s", commIDStr)), nil
	}
	ids, ok := members[commID]
	if !ok {
		return mcp.NewToolResultText(fmt.Sprintf("Community %d not found in the project graph.", commID)), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Community %s\n\n", commLabelStr(commID, commLabels))
	fmt.Fprintf(&sb, "**%d member(s).**\n\n", len(ids))
	sb.WriteString("| # | Node | Type | Degree | Fan-In | Fan-Out |\n")
	sb.WriteString("|---|------|------|--------|--------|---------|\n")
	for i, id := range ids {
		n, ok := g.Nodes[id]
		if !ok {
			continue
		}
		fmt.Fprintf(&sb, "| %d | `%s` | %s | %d | %d | %d |\n",
			i+1, id, codeOrPlain(n.Type), g.FanIn[id]+g.FanOut[id], g.FanIn[id], g.FanOut[id])
	}
	sb.WriteString("\n*Explore any member with `sv_graph_explain(node=\"<node>\")`.*\n")
	return s.respond(req, sb.String()), nil
}

// renderCommunityList renders communities ranked by member count.
func (s *Server) renderCommunityList(req mcp.CallToolRequest, g *graph.InMemoryGraph, members map[int][]string, commLabels map[int]string, topN int) (*mcp.CallToolResult, error) {
	sorted := rankCommunitiesBySize(members)
	if topN > len(sorted) {
		topN = len(sorted)
	}
	sorted = sorted[:topN]

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Top %d Graph Communities\n\n", topN)
	sb.WriteString("Ranked by member count. Communities group code that changes together.\n\n")
	sb.WriteString("| # | Community | Label | Size |\n")
	sb.WriteString("|---|-----------|-------|------|\n")
	for i, c := range sorted {
		fmt.Fprintf(&sb, "| %d | %d | %s | %d |\n", i+1, c.id, commLabelStr(c.id, commLabels), c.count)
	}

	var sampleNodes []*graph.Node
	for i, c := range sorted {
		if i >= 5 {
			break
		}
		if ids := members[c.id]; len(ids) > 0 {
			if n, ok := g.Nodes[ids[0]]; ok {
				sampleNodes = append(sampleNodes, n)
			}
		}
	}
	if benchmark := tokenBenchmark(sampleNodes, sb.Len()/4); benchmark != "" {
		sb.WriteString("\n" + benchmark + "\n")
	}

	sb.WriteString(`To inspect a community's members, call `)
	sb.WriteString("`sv_graph_communities(community_id=\"<id>\")`.\n")

	return s.respond(req, sb.String()), nil
}

// rankCommunitiesBySize sorts communities by member count descending, breaking
// ties by ascending id for determinism.
func rankCommunitiesBySize(members map[int][]string) []communityInfo {
	sorted := make([]communityInfo, 0, len(members))
	for cid, ids := range members {
		sorted = append(sorted, communityInfo{id: cid, count: len(ids)})
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].count != sorted[j].count {
			return sorted[i].count > sorted[j].count
		}
		return sorted[i].id < sorted[j].id
	})
	return sorted
}

func (s *Server) handleGraphSearch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query, err := req.RequireString("query")
	if err != nil {
		return mcp.NewToolResultError("missing required field: query"), nil
	}

	g, err := s.getOrLoadGraph()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load graph: %v", err)), nil
	}

	// Lazily compute and reload communities/centrality when missing, mirroring
	// the other graph tools, so a freshly-synced graph still reports the
	// community of each match.
	if g, err = s.ensureCentralityGraph(g); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load graph: %v", err)), nil
	}

	limit := positiveInt(req.GetString("limit", "10"), 10)
	nodeType := req.GetString("node_type", "")

	results := g.SearchNodes(query, nodeType, limit)

	var sb strings.Builder
	if len(results) == 0 {
		fmt.Fprintf(&sb, "No nodes match '%s'%s in the project graph.\n", query, typeSuffix(nodeType))
		sb.WriteString("Try a shorter term, `sv_graph_god_nodes`, or `sv_graph_report` for an overview.\n")
		return s.respond(req, sb.String()), nil
	}

	commLabels := computeCommLabels(g)

	fmt.Fprintf(&sb, "## Graph Search Results for '%s'\n\n", query)
	fmt.Fprintf(&sb, "%d node(s) match (showing up to %d). Ranked by match precision, then degree.\n\n", len(results), limit)
	sb.WriteString("| # | Node | Type | Path | Degree | Fan-In | Fan-Out | Community |\n")
	sb.WriteString("|---|------|------|------|--------|--------|---------|-----------|\n")
	for i, r := range results {
		fmt.Fprintf(&sb, "| %d | `%s` | %s | `%s` | %d | %d | %d | %s |\n",
			i+1, r.ID, codeOrPlain(r.Type), r.Path, r.Degree, r.FanIn, r.FanOut, commLabelStr(r.Community, commLabels))
	}
	sb.WriteString(s.nextStepAfterGraphSearch())

	var allNodes []*graph.Node
	for _, r := range results {
		if n, ok := g.Nodes[r.ID]; ok {
			allNodes = append(allNodes, n)
		}
	}
	if benchmark := tokenBenchmark(allNodes, sb.Len()/4); benchmark != "" {
		sb.WriteString("\n" + benchmark + "\n")
	}

	return s.respond(req, sb.String()), nil
}
