package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/svtech-code/sv-memory/internal/graph"
)

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
		return mcp.NewToolResultText(fmt.Sprintf("Could not find node matching '%s' in the graph.\nDid you mean to discover matches? Call sv_graph_search(query=\"%s\").", nodeName, nodeName)), nil
	}

	node := g.Nodes[nID]

	// Lazy-calculate communities and centrality if missing
	if node.Metadata == nil {
		g, node = s.refreshCentralityAndNode(nID)
	} else if _, hasBC := node.Metadata["betweenness_centrality"]; !hasBC {
		g, node = s.refreshCentralityAndNode(nID)
	}

	commLabels := computeCommLabels(g)
	cID := graph.NodeCommunityID(node)
	bc := graph.NodeBetweennessCentrality(node)
	fanIn := g.FanIn[nID]
	fanOut := g.FanOut[nID]

	isGod, reasons := godNodeReasons(fanIn, fanOut, bc)

	var sb strings.Builder
	explainNodeHeader(&sb, node)
	explainNetworkMetrics(&sb, cID, commLabels, bc, fanIn, fanOut)
	explainArchitecturalRole(&sb, isGod, reasons, fanIn, fanOut)
	explainSuggestions(&sb, isGod, fanIn, fanOut, bc)

	dependents, rationales := collectNeighbors(g, nID)
	renderDependents(&sb, dependents)
	renderDependencies(&sb, g, nID)
	renderRationales(&sb, rationales)
	writeNodeBenchmark(&sb, g, nID)
	explainSuggestedQuestions(&sb, node, isGod, fanIn)

	return s.respond(req, sb.String()), nil
}

// refreshCentralityAndNode lazily computes communities/centrality and reloads
// both the graph and the node so the local copy carries fresh metrics.
func (s *Server) refreshCentralityAndNode(nID string) (*graph.InMemoryGraph, *graph.Node) {
	debugLog("betweenness_centrality missing, calculating communities and centrality dynamically...")
	s.computeCentralityIfMissing()
	g, _ := s.getOrLoadGraph()
	return g, g.Nodes[nID]
}

// godNodeReasons reports whether a node is a God Node/hub and the reasons why,
// based on fan-in, fan-out, and betweenness centrality thresholds.
func godNodeReasons(fanIn, fanOut int, bc float64) (bool, []string) {
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
	return isGod, reasons
}

func explainNodeHeader(sb *strings.Builder, node *graph.Node) {
	fmt.Fprintf(sb, "## Structural Explanation of Node: `%s`\n\n", node.Label)
	fmt.Fprintf(sb, "- **Node ID:** `%s`\n", node.ID)
	fmt.Fprintf(sb, "- **Type:** `%s`\n", node.Type)
	if node.Path != "" {
		fmt.Fprintf(sb, "- **Path:** `%s`\n", node.Path)
	}
	if node.Metadata != nil {
		if lang, ok := node.Metadata["language"]; ok {
			fmt.Fprintf(sb, "- **Language:** `%v`\n", lang)
		}
		if loc, ok := node.Metadata["loc"]; ok {
			fmt.Fprintf(sb, "- **Lines of Code (LOC):** `%v`\n", loc)
		}
	}
}

func explainNetworkMetrics(sb *strings.Builder, cID int, commLabels map[int]string, bc float64, fanIn, fanOut int) {
	sb.WriteString("\n### 📊 Network Metrics:\n")
	fmt.Fprintf(sb, "- **Community:** `%s`\n", commLabelStr(cID, commLabels))
	fmt.Fprintf(sb, "- **Betweenness Centrality (BC):** `%.2f`\n", bc)
	fmt.Fprintf(sb, "- **Fan-In (Dependents):** `%d`\n", fanIn)
	fmt.Fprintf(sb, "- **Fan-Out (Dependencies):** `%d`\n", fanOut)
}

func explainArchitecturalRole(sb *strings.Builder, isGod bool, reasons []string, fanIn, fanOut int) {
	sb.WriteString("\n### 🧠 Architectural Role:\n")
	switch {
	case isGod:
		sb.WriteString("⚠️ **Potential God Node/Hub:** This node plays a central role in the codebase due to:\n")
		for _, r := range reasons {
			fmt.Fprintf(sb, "  - %s\n", r)
		}
		sb.WriteString("Refactoring this node could have significant ripple effects. Consider splitting its responsibilities.\n")
	case fanIn == 0 && fanOut > 0:
		sb.WriteString("🟢 **Entry Point / Controller:** This node has dependencies but no local dependents. It is likely an entry point or top-level controller.\n")
	case fanIn > 0 && fanOut == 0:
		sb.WriteString("🟢 **Leaf Node / Utility:** This node is depended upon by others but has no dependencies. It represents a low-level utility or core data model.\n")
	case fanIn > 0 && fanOut > 0:
		sb.WriteString("🟢 **Intermediate Component:** This node acts as an intermediary, receiving calls/imports and delegating to low-level utilities.\n")
	default:
		sb.WriteString("🟢 **Isolated Node:** This node currently has no connections in the dependency graph.\n")
	}
}

// explainSuggestions turns the metrics into concrete next steps the agent can
// take without re-deriving them.
func explainSuggestions(sb *strings.Builder, isGod bool, fanIn, fanOut int, bc float64) {
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
}

// collectNeighbors splits a node's inbound edges into dependents and code
// rationales (comment/memory-backed inbound rationale_for edges).
func collectNeighbors(g *graph.InMemoryGraph, nID string) (dependents, rationales []string) {
	for _, e := range g.EdgesByTarget[nID] {
		srcNode := g.Nodes[e.SourceID]
		if srcNode == nil {
			continue
		}
		if e.RelationType == "rationale_for" {
			rationales = append(rationales, rationaleEntry(srcNode))
			continue
		}
		dependents = append(dependents, fmt.Sprintf("- `%s` (relation: `%s`, confidence: `%s`)", srcNode.Label, e.RelationType, confidenceOrExtracted(e.Confidence)))
	}
	return dependents, rationales
}

// rationaleEntry renders an inbound rationale_for source as either a
// memory/decision link or a source-comment line reference.
func rationaleEntry(srcNode *graph.Node) string {
	if isMemory, _ := srcNode.Metadata["memory"].(bool); isMemory {
		// Memory-backed rationale: the source is a saved sv_mem_save
		// decision, not a code comment. Surface it as a decision link.
		return fmt.Sprintf("- **Memory/Decision:** `%s` (ID: `%s`)\n  → run `sv_mem_get(id=\"%s\")` for full context",
			srcNode.Label, srcNode.ID, srcNode.ID)
	}
	return fmt.Sprintf("- **Line %d:** `%s`", nodeLine(srcNode), srcNode.Label)
}

// nodeLine extracts the integer "line" metadata from a node, tolerating both
// float64 (JSON) and int encodings, defaulting to 0.
func nodeLine(n *graph.Node) int {
	if n.Metadata == nil {
		return 0
	}
	l, ok := n.Metadata["line"]
	if !ok {
		return 0
	}
	if lf, ok := l.(float64); ok {
		return int(lf)
	}
	if li, ok := l.(int); ok {
		return li
	}
	return 0
}

func renderDependents(sb *strings.Builder, dependents []string) {
	sb.WriteString("\n### 🔗 Immediate Neighbors:\n")
	if len(dependents) > 0 {
		sb.WriteString("**Dependents (Who imports/calls this):**\n")
		for _, dep := range dependents {
			sb.WriteString(dep + "\n")
		}
	} else {
		sb.WriteString("**Dependents:** None.\n")
	}
}

func renderDependencies(sb *strings.Builder, g *graph.InMemoryGraph, nID string) {
	edges := g.EdgesBySource[nID]
	if len(edges) == 0 {
		sb.WriteString("\n**Dependencies:** None.\n")
		return
	}
	sb.WriteString("\n**Dependencies (What this imports/calls):**\n")
	for _, e := range edges {
		tgtNode := g.Nodes[e.TargetID]
		if tgtNode != nil {
			fmt.Fprintf(sb, "- `%s` (relation: `%s`, confidence: `%s`)\n", tgtNode.Label, e.RelationType, confidenceOrExtracted(e.Confidence))
		}
	}
}

func renderRationales(sb *strings.Builder, rationales []string) {
	if len(rationales) == 0 {
		return
	}
	sb.WriteString("\n### 💡 Code Rationales (Empirical Decisions in Comments):\n")
	for _, r := range rationales {
		sb.WriteString(r + "\n")
	}
}

// writeNodeBenchmark appends the token-savings estimate for the node and its
// immediate neighbors.
func writeNodeBenchmark(sb *strings.Builder, g *graph.InMemoryGraph, nID string) {
	allNodes := []*graph.Node{g.Nodes[nID]}
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
	if benchmark := tokenBenchmark(allNodes, sb.Len()/4); benchmark != "" {
		sb.WriteString("\n" + benchmark + "\n")
	}
}

func explainSuggestedQuestions(sb *strings.Builder, node *graph.Node, isGod bool, fanIn int) {
	sb.WriteString("\n### ❓ Suggested Questions to ask Antigravity about this node:\n")
	fmt.Fprintf(sb, "1. \"What is the primary responsibility of `%s`?\"\n", node.Label)
	fmt.Fprintf(sb, "2. \"Are there any architectural patterns or guidelines used inside `%s`?\"\n", node.Label)
	switch {
	case isGod:
		fmt.Fprintf(sb, "3. \"How can we refactor or break down the God node `%s` into smaller modules?\"\n", node.Label)
	case fanIn > 0:
		fmt.Fprintf(sb, "3. \"Who are the main dependents of `%s` and how would a change here affect them?\"\n", node.Label)
	default:
		fmt.Fprintf(sb, "3. \"Show me the source code implementation details of `%s`.\"\n", node.Label)
	}
}
