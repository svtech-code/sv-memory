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
	TotalNodes          int      `json:"total_nodes"`
	TotalEdges          int      `json:"total_edges"`
	DanglingEdges       int      `json:"dangling_edges"`
	OrphanNodes         int      `json:"orphan_nodes"`
	SelfLoops           int      `json:"self_loops"`
	MissingFiles        int      `json:"missing_files"`
	StaleMemoryBindings int      `json:"stale_memory_bindings,omitempty"`
	IssuesSummary       []string `json:"issues_summary"`
	IsHealthy           bool     `json:"is_healthy"`
}

// DiagnoseGraph performs a health check on the SQLite graph tables for a project.
func DiagnoseGraph(db *sql.DB, projectID, projPath string) (*GraphDiagnosticReport, error) {
	report := &GraphDiagnosticReport{IsHealthy: true}

	if err := report.countNodesAndEdges(db, projectID); err != nil {
		return nil, err
	}
	report.checkSelfLoops(db, projectID)
	report.checkDanglingEdges(db, projectID)
	report.checkOrphanNodes(db, projectID)
	if projPath != "" {
		report.checkMissingFiles(db, projectID, projPath)
		report.checkStaleMemoryBindings(db, projectID, projPath)
	}
	return report, nil
}

func (r *GraphDiagnosticReport) countNodesAndEdges(db *sql.DB, projectID string) error {
	if err := db.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ?", projectID).Scan(&r.TotalNodes); err != nil {
		return fmt.Errorf("failed to count graph_nodes: %w", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM graph_edges WHERE project_id = ?", projectID).Scan(&r.TotalEdges); err != nil {
		return fmt.Errorf("failed to count graph_edges: %w", err)
	}
	return nil
}

// flag marks the report unhealthy and records an issue.
func (r *GraphDiagnosticReport) flag(issue string) {
	r.IsHealthy = false
	r.IssuesSummary = append(r.IssuesSummary, issue)
}

func (r *GraphDiagnosticReport) checkSelfLoops(db *sql.DB, projectID string) {
	if err := db.QueryRow("SELECT COUNT(*) FROM graph_edges WHERE project_id = ? AND source_id = target_id", projectID).Scan(&r.SelfLoops); err == nil && r.SelfLoops > 0 {
		r.flag(fmt.Sprintf("Found %d self-loop edges.", r.SelfLoops))
	}
}

func (r *GraphDiagnosticReport) checkDanglingEdges(db *sql.DB, projectID string) {
	const danglingQuery = `
	SELECT COUNT(*) FROM graph_edges e
	WHERE e.project_id = ? AND (
		NOT EXISTS (SELECT 1 FROM graph_nodes n WHERE n.project_id = e.project_id AND n.id = e.source_id)
		OR
		NOT EXISTS (SELECT 1 FROM graph_nodes n WHERE n.project_id = e.project_id AND n.id = e.target_id)
	)`
	if err := db.QueryRow(danglingQuery, projectID).Scan(&r.DanglingEdges); err == nil && r.DanglingEdges > 0 {
		r.flag(fmt.Sprintf("Found %d dangling edges pointing to non-existent nodes.", r.DanglingEdges))
	}
}

func (r *GraphDiagnosticReport) checkOrphanNodes(db *sql.DB, projectID string) {
	const orphanQuery = `
	SELECT COUNT(*) FROM graph_nodes n
	WHERE n.project_id = ? AND (
		NOT EXISTS (SELECT 1 FROM graph_edges e WHERE e.project_id = n.project_id AND (e.source_id = n.id OR e.target_id = n.id))
	)`
	if err := db.QueryRow(orphanQuery, projectID).Scan(&r.OrphanNodes); err == nil && r.OrphanNodes > 0 {
		// Orphan nodes are informational and do not affect overall health.
		r.IssuesSummary = append(r.IssuesSummary, fmt.Sprintf("Found %d orphan nodes with no connections.", r.OrphanNodes))
	}
}

func (r *GraphDiagnosticReport) checkMissingFiles(db *sql.DB, projectID, projPath string) {
	query := "SELECT path FROM graph_nodes WHERE project_id = ? AND node_type IN ('" + schema.NodeTypeFile + "', '" + schema.NodeTypeDocument + "', '" + schema.NodeTypeSQL + "')"
	rows, err := db.Query(query, projectID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var relPath string
		if scanErr := rows.Scan(&relPath); scanErr != nil {
			continue
		}
		if _, statErr := os.Stat(filepath.Join(projPath, relPath)); os.IsNotExist(statErr) {
			r.MissingFiles++
		}
	}
	if r.MissingFiles > 0 {
		r.flag(fmt.Sprintf("Found %d file nodes pointing to missing files on disk.", r.MissingFiles))
	}
}

func (r *GraphDiagnosticReport) checkStaleMemoryBindings(db *sql.DB, projectID, projPath string) {
	rows, err := db.Query("SELECT id, where_path FROM memories WHERE project_id = ? AND where_path IS NOT NULL AND where_path != '' AND deleted_at IS NULL", projectID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var memID, wherePath string
		if scanErr := rows.Scan(&memID, &wherePath); scanErr != nil {
			continue
		}
		if wherePathHasMissing(wherePath, projPath) {
			r.StaleMemoryBindings++
		}
	}
	if r.StaleMemoryBindings > 0 {
		r.flag(fmt.Sprintf("Found %d memories bound to missing files on disk (stale where_path).", r.StaleMemoryBindings))
	}
}

// wherePathHasMissing reports whether any comma-separated path in wherePath does
// not exist under projPath.
func wherePathHasMissing(wherePath, projPath string) bool {
	for _, p := range strings.Split(wherePath, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, statErr := os.Stat(filepath.Join(projPath, filepath.FromSlash(p))); os.IsNotExist(statErr) {
			return true
		}
	}
	return false
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
	if r.StaleMemoryBindings > 0 {
		fmt.Fprintf(&sb, "- **Stale Memory Bindings:** %d\n", r.StaleMemoryBindings)
	}

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
