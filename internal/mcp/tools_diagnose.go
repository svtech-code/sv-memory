package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/svtech-code/sv-memory/internal/graph"
	"github.com/svtech-code/sv-memory/internal/memory"
)

// handleDiagnose runs read-only health checks on the active project and returns
// a compact Markdown report. It combines the memory/system diagnostics
// (RunDiagnostics: database, tables, FTS5 triggers, project registration,
// write permissions, chunks) with the structural graph health check
// (DiagnoseGraph: dangling edges, orphan nodes, self-loops, missing files).
func (s *Server) handleDiagnose(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var sb strings.Builder
	sb.WriteString("## 🏥 sv-memory Diagnostics\n\n")

	fails := 0
	warns := 0
	for _, r := range memory.RunDiagnostics(s.pool.Reader, s.cfg.ProjectID, s.cfg.ProjPath, s.cfg.DBPath) {
		switch r.Status {
		case "fail":
			fails++
			fmt.Fprintf(&sb, "- ❌ **%s:** %s\n", r.Check, r.Message)
		case "warn":
			warns++
			fmt.Fprintf(&sb, "- ⚠️ **%s:** %s\n", r.Check, r.Message)
		default:
			fmt.Fprintf(&sb, "- ✅ **%s:** %s\n", r.Check, r.Message)
		}
	}

	if report, gErr := graph.DiagnoseGraph(s.pool.Reader, s.cfg.ProjectID, s.cfg.ProjPath); gErr == nil {
		sb.WriteString("\n" + report.String())
	} else {
		fails++
		fmt.Fprintf(&sb, "\n- ❌ **graph_health:** %v\n", gErr)
	}

	switch {
	case fails > 0:
		fmt.Fprintf(&sb, "\n**Result:** ❌ %d check(s) failed, %d warning(s). Run `sv-memory diagnose` for the full report.\n", fails, warns)
	case warns > 0:
		fmt.Fprintf(&sb, "\n**Result:** ⚠️ %d warning(s), no failures.\n", warns)
	default:
		sb.WriteString("\n**Result:** ✅ all checks passed.\n")
	}

	return mcp.NewToolResultText(truncateToTokenBudget(sb.String(), resolveTokenBudget(req, req.GetString("token_budget", "")))), nil
}
