package mcp

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/svtech-code/sv-memory/internal/db"
	"github.com/svtech-code/sv-memory/internal/graph"
	"github.com/svtech-code/sv-memory/internal/security"
)

func (s *Server) handleGraphReport(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	startLoad := time.Now()
	g, err := s.getOrLoadGraph()
	debugLog("graph_load took %s", time.Since(startLoad))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to load graph: %v", err)), nil
	}

	// Lazy-compute and persist centrality when missing, mirroring the other
	// graph tools, so the report renders from up-to-date computed metrics.
	// GenerateGraphReport loads the graph from the DB itself, so g is only
	// consulted as the freshness signal here.
	if !graphHasCentrality(g) {
		s.computeCentralityIfMissing()
	}

	var opts graph.ReportOptions
	opts.GodNodes = positiveInt(req.GetString("god_nodes", "10"), 10)
	opts.Communities = positiveInt(req.GetString("communities", "10"), 10)
	opts.Connections = positiveInt(req.GetString("connections", "10"), 10)

	output := req.GetString("output", "GRAPH_REPORT.md")
	output, err = security.ValidateWritePath(s.cfg.ProjPath, output)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid output path: %v", err)), nil
	}

	summary, err := graph.GenerateGraphReport(s.pool.Reader, s.cfg.ProjectID, output, opts)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to generate graph report: %v", err)), nil
	}

	digest := fmt.Sprintf(
		"Graph report written to `%s` (%d bytes).\n\n- **Nodes:** %d\n- **Edges:** %d\n- **Communities:** %d (hub threshold p99 = %d)\n- **God nodes:** %d\n- **Surprising connections:** %d\n\nCoverage drill-down: `sv_graph_god_nodes`, `sv_graph_surprising_connections`, `sv_graph_explain(node=...)`.",
		output, summary.Bytes, summary.Nodes, summary.Edges, summary.Communities, summary.HubThreshold, summary.GodNodes, summary.Connections)
	return s.respond(req, digest), nil
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

	merged, err := mergeProjectGraphs(mergeDB, projA, projB)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
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

// mergeProjectGraphs loads two project graphs and returns their union merge.
func mergeProjectGraphs(mergeDB *sql.DB, projA, projB string) (*graph.InMemoryGraph, error) {
	ga, err := graph.LoadFullGraph(mergeDB, projA)
	if err != nil {
		return nil, fmt.Errorf("failed to load project %s: %v", projA, err)
	}
	gb, err := graph.LoadFullGraph(mergeDB, projB)
	if err != nil {
		return nil, fmt.Errorf("failed to load project %s: %v", projB, err)
	}
	return ga.Merge(gb), nil
}

func (s *Server) handleGraphDiff(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	baseRef := req.GetString("base_ref", "")
	blastRadius := req.GetString("blast_radius", "true") != "false"

	report, err := graph.ComputeGraphDiff(s.pool.Reader, s.cfg.ProjectID, s.cfg.ProjPath, baseRef, blastRadius)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to compute graph diff: %v", err)), nil
	}

	rendered := graph.RenderGraphDiffReport(report)
	return s.respond(req, rendered), nil
}
