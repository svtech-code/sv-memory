package graph

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ReportOptions bounds the generated GRAPH_REPORT.md. Zero values fall back to
// sensible defaults so the CLI and MCP tool agree on output without callers
// repeating defaults in two places.
type ReportOptions struct {
	ProjName         string // display name for the header
	GodNodes         int    // top god-node count (default 10)
	Communities      int    // top communities with auto-labels (default 10)
	Connections      int    // surprising cross-community connections (default 10)
	CommunityMembers int    // members shown per community (default 8)
}

// ReportSummary is the compact digest returned after writing GRAPH_REPORT.md.
// The CLI prints it; the MCP tool returns it as tool text without the full
// file body, keeping the agent response bounded.
type ReportSummary struct {
	Bytes        int
	Nodes        int
	Edges        int
	Communities  int
	HubThreshold int
	GodNodes     int
	Connections  int
	Title        string
}

// GenerateGraphReport builds the aggregate graph overview into a standalone
// markdown file. It loads the full graph once, then feeds every section
// (god nodes, communities, bridges, suggested questions) from computed
// metrics rather than invented content. Shared by the CLI and the
// sv_graph_report MCP tool.
func GenerateGraphReport(db *sql.DB, projectID string, output string, opts ReportOptions) (ReportSummary, error) {
	if opts.GodNodes <= 0 {
		opts.GodNodes = 10
	}
	if opts.Communities <= 0 {
		opts.Communities = 10
	}
	if opts.Connections <= 0 {
		opts.Connections = 10
	}
	if opts.CommunityMembers <= 0 {
		opts.CommunityMembers = 8
	}

	g, err := LoadFullGraph(db, projectID)
	if err != nil {
		return ReportSummary{}, fmt.Errorf("failed to load graph: %w", err)
	}

	comms := g.LeidenDetectCommunities()
	centrality := g.BetweennessCentrality()
	commLabels := g.DetectCommunityLabels(comms, centrality)

	godNodes, err := TopDegreeNodes(db, projectID, opts.GodNodes)
	if err != nil {
		return ReportSummary{}, fmt.Errorf("failed to compute god nodes: %w", err)
	}

	conns := g.FindSurprisingConnections(comms, centrality, opts.Connections)

	var body strings.Builder
	writeReportHeader(&body, opts, g, commLabels)
	writeReportGodNodes(&body, godNodes)
	writeReportCommunities(&body, g, comms, commLabels, opts)
	writeReportConnections(&body, conns, commLabels)
	writeReportQuestions(&body, godNodes, conns, commLabels)

	if err := writeReportFile(output, body.String()); err != nil {
		return ReportSummary{}, err
	}

	return ReportSummary{
		Bytes:        body.Len(),
		Nodes:        len(g.Nodes),
		Edges:        len(g.EdgesBySource),
		Communities:  len(commLabels),
		HubThreshold: g.ComputeHubThreshold(),
		GodNodes:     len(godNodes),
		Connections:  len(conns),
		Title:        opts.ProjName,
	}, nil
}

func writeReportHeader(b *strings.Builder, opts ReportOptions, g *InMemoryGraph, commLabels map[int]string) {
	fmt.Fprintf(b, "# Graph Overview Report\n\n")
	if opts.ProjName != "" {
		fmt.Fprintf(b, "**Project:** %s\n\n", opts.ProjName)
	}
	fmt.Fprintf(b, "**Generated:** %s\n\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(b, "| Metric | Value |\n|---|---|\n")
	fmt.Fprintf(b, "| Nodes | %d |\n", len(g.Nodes))
	fmt.Fprintf(b, "| Edges | %d |\n", len(g.EdgesBySource))
	fmt.Fprintf(b, "| Communities | %d |\n", len(commLabels))
	fmt.Fprintf(b, "| Hub threshold (p99) | %d |\n", g.ComputeHubThreshold())
	b.WriteString("\n")
	b.WriteString("This report is a standing architectural overview: the most-connected hubs, the community structure, and the bridges between communities. Use it to decide what to explore or refactor next.\n\n")
}

func writeReportGodNodes(b *strings.Builder, godNodes []DegreeNode) {
	b.WriteString("## God Nodes (top hubs)\n\n")
	if len(godNodes) == 0 {
		b.WriteString("_No hub nodes found._\n\n")
		b.WriteString("---\n\n")
		return
	}
	b.WriteString("| Rank | Node | Degree |\n|---|---|---|\n")
	for i, d := range godNodes {
		fmt.Fprintf(b, "| %d | `%s` | %d |\n", i+1, cleanLabel(&Node{Label: d.Label, Type: d.Type}, 50), d.Degree)
	}
	b.WriteString("\n")
	b.WriteString("These nodes consume or are consumed by the most code. Changes here ripple widely — treat them as architectural hotspots.\n\n")
	b.WriteString("---\n\n")
}

func writeReportCommunities(b *strings.Builder, g *InMemoryGraph, comms map[string]int, commLabels map[int]string, opts ReportOptions) {
	b.WriteString("## Top Communities\n\n")
	groups := groupCommunities(g, comms)
	if len(groups) == 0 {
		b.WriteString("_No communities detected._\n\n")
		b.WriteString("---\n\n")
		return
	}

	ids := make([]int, 0, len(groups))
	for cID := range groups {
		ids = append(ids, cID)
	}
	sort.Slice(ids, func(i, j int) bool {
		return len(groups[ids[i]]) > len(groups[ids[j]])
	})
	if len(ids) > opts.Communities {
		ids = ids[:opts.Communities]
	}

	for _, cID := range ids {
		members := groups[cID]
		label := commLabels[cID]
		if label == "" {
			label = fmt.Sprintf("community_%d", cID)
		}
		fmt.Fprintf(b, "### %s (ID %d, size %d)\n\n", label, cID, len(members))
		for i, m := range members {
			if i >= opts.CommunityMembers {
				fmt.Fprintf(b, "- _... and %d more_\n", len(members)-opts.CommunityMembers)
				break
			}
			fmt.Fprintf(b, "- `%s`\n", m)
		}
		b.WriteString("\n")
	}
	b.WriteString("---\n\n")
}

func groupCommunities(g *InMemoryGraph, comms map[string]int) map[int][]string {
	groups := make(map[int][]string)
	for id, node := range g.Nodes {
		cID, ok := comms[id]
		if !ok {
			continue
		}
		label := cleanLabel(node, 50)
		if label == "" {
			label = id
		}
		groups[cID] = append(groups[cID], label)
	}
	return groups
}

func writeReportConnections(b *strings.Builder, conns []SurprisingConnection, commLabels map[int]string) {
	b.WriteString("## Surprising Cross-Community Connections\n\n")
	if len(conns) == 0 {
		b.WriteString("_No cross-community bridges with a surprise score found._\n\n")
		b.WriteString("---\n\n")
		return
	}
	b.WriteString("| Rank | Source | Target | Edge Type | Surprise Score | Communities |\n")
	b.WriteString("|---|---|---|---|---|---|\n")
	for i, c := range conns {
		src := c.SourceLabel
		if src == "" {
			src = c.SourceID
		}
		tgt := c.TargetLabel
		if tgt == "" {
			tgt = c.TargetID
		}
		fmt.Fprintf(b, "| %d | `%s` | `%s` | `%s` | %.2f | %s ↔ %s |\n",
			i+1, src, tgt, c.EdgeType, c.SurpriseScore,
			reportCommLabel(c.SrcCommunity, commLabels), reportCommLabel(c.DstCommunity, commLabels))
	}
	b.WriteString("\n")
	b.WriteString("Higher surprise score means a more unexpected bridge between communities. Trace them with `sv-memory graph path <source> <target>` or `sv_graph_path`.\n\n")
	b.WriteString("---\n\n")
}

func writeReportQuestions(b *strings.Builder, godNodes []DegreeNode, conns []SurprisingConnection, commLabels map[int]string) {
	b.WriteString("## Suggested Questions\n\n")
	b.WriteString("Generated from the structural metrics above to guide exploration:\n\n")
	num := 1
	for _, d := range godNodes {
		if num > 5 {
			break
		}
		fmt.Fprintf(b, "%d. Revise `%s`: what single responsibility is it overloaded with, and how could it be split?\n", num, d.Label)
		num++
	}
	for _, c := range conns {
		if num > 8 {
			break
		}
		src := c.SourceLabel
		if src == "" {
			src = c.SourceID
		}
		tgt := c.TargetLabel
		if tgt == "" {
			tgt = c.TargetID
		}
		fmt.Fprintf(b, "%d. Trace the bridge `%s` → `%s`: why does it cross from **%s** into **%s**?\n",
			num, src, tgt, reportCommLabel(c.SrcCommunity, commLabels), reportCommLabel(c.DstCommunity, commLabels))
		num++
	}
	if num > 1 {
		fmt.Fprintf(b, "%d. What is the biggest blast-radius decision pending? Start with the highest-degree god node using `sv_graph_explain`.\n", num)
	}
	b.WriteString("\n")
}

// reportCommLabel formats a community as "Label (ID N)" or "none"/"community_N".
func reportCommLabel(commID int, labels map[int]string) string {
	if commID == 0 {
		return "none"
	}
	if label, ok := labels[commID]; ok {
		return fmt.Sprintf("%s (ID %d)", label, commID)
	}
	return fmt.Sprintf("community_%d", commID)
}

// writeReportFile persists the generated report, creating parent directories
// if needed so a nested output path does not fail silently.
func writeReportFile(output, body string) error {
	if dir := filepath.Dir(output); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create output directory: %w", err)
		}
	}
	if err := os.WriteFile(output, []byte(body), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", output, err)
	}
	return nil
}
