package graph

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/svtech-code/sv-memory/internal/graph/schema"
)

// GraphDiagnosticReport summarizes integrity issues found in the structural code graph.
type GraphDiagnosticReport struct {
	TotalNodes    int      `json:"total_nodes"`
	TotalEdges    int      `json:"total_edges"`
	DanglingEdges int      `json:"dangling_edges"`
	OrphanNodes   int      `json:"orphan_nodes"`
	SelfLoops     int      `json:"self_loops"`
	MissingFiles  int      `json:"missing_files"`
	IssuesSummary []string `json:"issues_summary"`
	IsHealthy     bool     `json:"is_healthy"`
}

// DiagnoseGraph performs a health check on the SQLite graph tables for a project.
func DiagnoseGraph(db *sql.DB, projectID, projPath string) (*GraphDiagnosticReport, error) {
	report := &GraphDiagnosticReport{
		IsHealthy: true,
	}

	// 1. Total nodes and total edges
	if err := db.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ?", projectID).Scan(&report.TotalNodes); err != nil {
		return nil, fmt.Errorf("failed to count graph_nodes: %w", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM graph_edges WHERE project_id = ?", projectID).Scan(&report.TotalEdges); err != nil {
		return nil, fmt.Errorf("failed to count graph_edges: %w", err)
	}

	// 2. Self loops (source_id == target_id)
	if err := db.QueryRow("SELECT COUNT(*) FROM graph_edges WHERE project_id = ? AND source_id = target_id", projectID).Scan(&report.SelfLoops); err == nil && report.SelfLoops > 0 {
		report.IsHealthy = false
		report.IssuesSummary = append(report.IssuesSummary, fmt.Sprintf("Found %d self-loop edges.", report.SelfLoops))
	}

	// 3. Dangling edges (source or target node not in graph_nodes)
	danglingQuery := `
	SELECT COUNT(*) FROM graph_edges e
	WHERE e.project_id = ? AND (
		NOT EXISTS (SELECT 1 FROM graph_nodes n WHERE n.project_id = e.project_id AND n.id = e.source_id)
		OR
		NOT EXISTS (SELECT 1 FROM graph_nodes n WHERE n.project_id = e.project_id AND n.id = e.target_id)
	)`
	if err := db.QueryRow(danglingQuery, projectID).Scan(&report.DanglingEdges); err == nil && report.DanglingEdges > 0 {
		report.IsHealthy = false
		report.IssuesSummary = append(report.IssuesSummary, fmt.Sprintf("Found %d dangling edges pointing to non-existent nodes.", report.DanglingEdges))
	}

	// 4. Orphan nodes (nodes with no incoming or outgoing edges)
	orphanQuery := `
	SELECT COUNT(*) FROM graph_nodes n
	WHERE n.project_id = ? AND (
		NOT EXISTS (SELECT 1 FROM graph_edges e WHERE e.project_id = n.project_id AND (e.source_id = n.id OR e.target_id = n.id))
	)`
	if err := db.QueryRow(orphanQuery, projectID).Scan(&report.OrphanNodes); err == nil && report.OrphanNodes > 0 {
		report.IssuesSummary = append(report.IssuesSummary, fmt.Sprintf("Found %d orphan nodes with no connections.", report.OrphanNodes))
	}

	// 5. Missing physical files (file nodes pointing to missing disk files)
	if projPath != "" {
		rows, err := db.Query("SELECT path FROM graph_nodes WHERE project_id = ? AND node_type IN ('"+schema.NodeTypeFile+"', '"+schema.NodeTypeDocument+"', '"+schema.NodeTypeSQL+"')", projectID)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var relPath string
				if scanErr := rows.Scan(&relPath); scanErr == nil {
					absPath := filepath.Join(projPath, relPath)
					if _, statErr := os.Stat(absPath); os.IsNotExist(statErr) {
						report.MissingFiles++
					}
				}
			}
			if report.MissingFiles > 0 {
				report.IsHealthy = false
				report.IssuesSummary = append(report.IssuesSummary, fmt.Sprintf("Found %d file nodes pointing to missing files on disk.", report.MissingFiles))
			}
		}
	}

	return report, nil
}

// String returns a clean Markdown formatted report.
func (r *GraphDiagnosticReport) String() string {
	var sb strings.Builder
	sb.WriteString("### 🏥 Graph Health Diagnostics\n\n")
	fmt.Fprintf(&sb, "- **Total Nodes:** %d\n", r.TotalNodes)
	fmt.Fprintf(&sb, "- **Total Edges:** %d\n", r.TotalEdges)
	fmt.Fprintf(&sb, "- **Dangling Edges:** %d\n", r.DanglingEdges)
	fmt.Fprintf(&sb, "- **Orphan Nodes:** %d\n", r.OrphanNodes)
	fmt.Fprintf(&sb, "- **Self Loops:** %d\n", r.SelfLoops)
	fmt.Fprintf(&sb, "- **Missing Files:** %d\n", r.MissingFiles)

	if r.IsHealthy {
		sb.WriteString("\n**Status:** ✅ Graph is healthy! No integrity issues found.\n")
	} else {
		sb.WriteString("\n**Status:** ⚠️ Graph health warnings detected:\n")
		for _, issue := range r.IssuesSummary {
			fmt.Fprintf(&sb, "  - %s\n", issue)
		}
	}
	return sb.String()
}
