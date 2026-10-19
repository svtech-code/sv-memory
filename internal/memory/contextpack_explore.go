package memory

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/svtech-code/sv-memory/internal/graph"
	"github.com/svtech-code/sv-memory/internal/graph/schema"
	"github.com/svtech-code/sv-memory/internal/security"
)

// GetContextPack builds the compact context pack for a code path. maxMemories
// caps how many linked memories are returned (0 = default 5). When includeChanges
// is true, active spec changes whose where_path matches the query are appended
// so the agent sees in-flight proposals before touching the path. The graph may
// be empty (never synced); in that case a pack with no node but path-scoped
// memories is still returned so the call never hard-fails.
//
// The query may carry multiple comma-separated symbols ("A, B"). In that case
// the resolved primary node receives the full single-symbol treatment below,
// the secondary symbols receive surgical snippets into ExtraSnippets, and the
// shortest dependency path between the two most significant symbols is stored
// in CallPath. A single symbol keeps the exact previous behaviour and output.
func GetContextPack(db *sql.DB, projectID, query string, maxMemories int, includeChanges bool) (*ContextPack, error) {
	if maxMemories <= 0 {
		maxMemories = 5
	}
	if maxMemories > 20 {
		maxMemories = 20
	}

	// Auto-freshness check: refresh graph incrementally if project files changed on disk.
	projPath := resolveProjectPath(db, projectID)
	if projPath != "" {
		_, _ = graph.SyncGraphIfHasChanges(db, projectID, projPath)
	}

	// Split the query into at most 3 symbols when commas are present so the pack
	// turns into a unified explore call (multi-symbol + call path). A single
	// symbol falls through to the exact existing single-node flow.
	primary, extraSnippets, callPath := resolveExploreSymbols(db, projectID, projPath, query)

	node, err := ResolveContextNode(db, projectID, primary)
	if err != nil {
		return nil, err
	}

	pack := &ContextPack{Node: node, ExtraSnippets: extraSnippets, CallPath: callPath}
	applyNodeSnippetAndBlast(db, projectID, projPath, node, pack)

	mems, err := gatherContextMemories(db, projectID, primary, node, maxMemories)
	if err != nil {
		return nil, err
	}
	pack.Memories = mems

	if node != nil {
		// Direct neighbors (dependents/dependencies), excluding rationale_for
		// edges so memory document nodes don't appear as code dependents.
		if pack.Dependents, err = contextNeighbors(db, projectID, node.ID, "target_id", 5); err != nil {
			return nil, err
		}
		if pack.Dependencies, err = contextNeighbors(db, projectID, node.ID, "source_id", 5); err != nil {
			return nil, err
		}
		// Capabilities this node implements (spec nodes reached via
		// 'implements' edges), each with a bounded requirement summary.
		if pack.Capabilities, err = capabilitiesForNode(db, projectID, node.ID); err != nil {
			return nil, err
		}
	}

	if includeChanges {
		if changes, err := changesForPath(db, projectID, primary); err == nil {
			pack.Changes = changes
		}
	}
	return pack, nil
}

// applyNodeSnippetAndBlast fills the snippet and blast-radius fields for the
// resolved node. A nil node leaves both empty.
func applyNodeSnippetAndBlast(db *sql.DB, projectID, projPath string, node *ContextNode, pack *ContextPack) {
	if node == nil {
		return
	}
	if node.Path != "" && projPath != "" {
		if node.Type == schema.NodeTypeFile {
			pack.Snippet, pack.SnippetLine = extractFileSkeleton(db, projectID, node, projPath)
		} else {
			pack.Snippet, pack.SnippetLine = extractSurgicalSnippet(projPath, node, maxSnippetLines)
		}
	}
	pack.BlastRadius, _ = graph.CalculateBlastRadius(db, projectID, node.ID, 3, 10)
}

// gatherContextMemories collects path-scoped and rationale_for memories,
// deduplicating by id (path-scoped first) and capping at maxMemories.
func gatherContextMemories(db *sql.DB, projectID, primary string, node *ContextNode, maxMemories int) ([]ContextMemory, error) {
	pathMems, err := loadPathScopedMemories(db, projectID, primary, maxMemories)
	if err != nil {
		return nil, err
	}
	var rationaleMems []ContextMemory
	if node != nil {
		if rationaleMems, err = loadRationaleMemories(db, projectID, node.ID, maxMemories); err != nil {
			return nil, err
		}
	}

	seen := map[string]bool{}
	var mems []ContextMemory
	for _, m := range append(pathMems, rationaleMems...) {
		if seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		mems = append(mems, m)
	}
	if len(mems) > maxMemories {
		mems = mems[:maxMemories]
	}
	return mems, nil
}

// loadPathScopedMemories loads memories whose where_path matches the query
// (path-scoped recall), excluding automatic post-commit capture logs so they
// don't displace high-signal decisions, bugfixes, or journals.
func loadPathScopedMemories(db *sql.DB, projectID, primary string, maxMemories int) ([]ContextMemory, error) {
	pattern := "%" + sanitizePathFilter(primary) + "%"
	rows, err := db.Query(`
		SELECT id, category, what, COALESCE(why, ''), COALESCE(where_path, '')
		FROM memories
		WHERE project_id = ? AND deleted_at IS NULL
		  AND COALESCE(why, '') NOT LIKE 'automatic post-commit capture for %'
		  AND (where_path LIKE ? ESCAPE '\' OR where_path = ?)
		ORDER BY created_at DESC
		LIMIT ?`, projectID, pattern, primary, maxMemories)
	if err != nil {
		return nil, fmt.Errorf("failed querying path memories: %w", err)
	}
	defer rows.Close()

	return scanContextMemories(rows, "where_path")
}

// loadRationaleMemories loads memories linked through a rationale_for edge to
// the resolved node (memory document node -> code node).
func loadRationaleMemories(db *sql.DB, projectID, nodeID string, maxMemories int) ([]ContextMemory, error) {
	rows, err := db.Query(`
		SELECT m.id, m.category, m.what, COALESCE(m.why, ''), COALESCE(m.where_path, '')
		FROM graph_edges e
		JOIN memories m ON m.id = e.source_id
		WHERE e.project_id = ? AND e.relation_type = 'rationale_for' AND e.target_id = ?
		  AND m.deleted_at IS NULL
		  AND COALESCE(m.why, '') NOT LIKE 'automatic post-commit capture for %'
		ORDER BY m.created_at DESC
		LIMIT ?`, projectID, nodeID, maxMemories)
	if err != nil {
		return nil, fmt.Errorf("failed querying rationale memories: %w", err)
	}
	defer rows.Close()

	return scanContextMemories(rows, "rationale_for")
}

// scanContextMemories reads memory rows into ContextMemory values, tagging the
// recall source. Unreadable rows are skipped.
func scanContextMemories(rows *sql.Rows, source string) ([]ContextMemory, error) {
	var mems []ContextMemory
	for rows.Next() {
		var id, category, what, why, wherePath string
		if scanErr := rows.Scan(&id, &category, &what, &why, &wherePath); scanErr == nil {
			mems = append(mems, ContextMemory{ID: id, Category: category, What: what, Why: why, WherePath: wherePath, Source: source})
		}
	}
	return mems, rows.Err()
}

// capabilitiesForNode lists the capabilities (spec nodes) that node implements,
// i.e. capabilities reachable from node via an 'implements' edge. Each entry is
// bounded: at most maxContextCaps capabilities, each with at most
// maxContextReqNames requirement names and a total requirement count. The
// capability paths are collected first (closing the edge rows) so the per-cap
// count query never nests inside an open result set.
func capabilitiesForNode(db *sql.DB, projectID, nodeID string) ([]ContextCapability, error) {
	rows, err := db.Query(`
		SELECT n.path
		FROM graph_edges e
		JOIN graph_nodes n ON n.id = e.target_id AND n.project_id = e.project_id
		WHERE e.project_id = ? AND e.relation_type = 'implements' AND e.source_id = ?
		  AND n.node_type = 'spec'
		ORDER BY n.path
		LIMIT ?`, projectID, nodeID, maxContextCaps)
	if err != nil {
		return nil, fmt.Errorf("failed querying capabilities: %w", err)
	}
	var caps []ContextCapability
	for rows.Next() {
		var capPath string
		if scanErr := rows.Scan(&capPath); scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		caps = append(caps, ContextCapability{CapabilityPath: capPath})
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range caps {
		var names string
		if err := db.QueryRow(`
			SELECT COUNT(*), COALESCE(GROUP_CONCAT(requirement, ' · '), '')
			FROM spec_capabilities
			WHERE project_id = ? AND capability_path = ?`, projectID, caps[i].CapabilityPath).Scan(&caps[i].RequirementCount, &names); err != nil {
			return nil, err
		}
		parts := strings.Split(names, " · ")
		if len(parts) > maxContextReqNames {
			parts = parts[:maxContextReqNames]
		}
		caps[i].Requirements = parts
	}
	return caps, nil
}

// Bounds for the context-pack capability surfacing, keeping the pack
// token-efficient: the agent reads the full spec mirror only on demand.
const (
	maxContextCaps     = 10
	maxContextReqNames = 5
)

// changesForPath lists active (not archived/rejected) spec changes whose
// where_path matches the query path, most recently created first. Best-effort:
// a where_path mismatch simply yields no changes, never an error for the pack.
func changesForPath(db *sql.DB, projectID, query string) ([]ContextChange, error) {
	pattern := "%" + sanitizePathFilter(query) + "%"
	rows, err := db.Query(`
		SELECT id, slug, status, title, COALESCE(tasks, '')
		FROM changes
		WHERE project_id = ? AND status NOT IN ('archived', 'rejected')
		  AND (where_path LIKE ? ESCAPE '\' OR where_path = ?)
		ORDER BY created_at DESC
		LIMIT 10`, projectID, pattern, query)
	if err != nil {
		return nil, fmt.Errorf("failed querying changes for path: %w", err)
	}
	defer rows.Close()

	var out []ContextChange
	for rows.Next() {
		var c ContextChange
		var tasks string
		if scanErr := rows.Scan(&c.ID, &c.Slug, &c.Status, &c.Title, &tasks); scanErr == nil {
			c.TaskProgress = ParseTaskProgress(tasks).Summary
			out = append(out, c)
		}
	}
	return out, rows.Err()
}

// contextNeighbors lists up to limit direct neighbors of nodeID. When col is
// "target_id" the neighbors are nodes pointing at nodeID (dependents); when
// col is "source_id" they are nodes nodeID points at (dependencies).
func contextNeighbors(db *sql.DB, projectID, nodeID, col string, limit int) ([]ContextNeighbor, error) {
	otherCol := "source_id"
	if col == "source_id" {
		otherCol = "target_id"
	}
	query := fmt.Sprintf(`
		SELECT n.id, n.label, e.relation_type, e.confidence
		FROM graph_edges e
		JOIN graph_nodes n ON n.id = e.%s AND n.project_id = e.project_id
		WHERE e.project_id = ? AND e.%s = ? AND e.relation_type != 'rationale_for'
		LIMIT ?`, otherCol, col)
	rows, err := db.Query(query, projectID, nodeID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed querying neighbors: %w", err)
	}
	defer rows.Close()

	var out []ContextNeighbor
	for rows.Next() {
		var id, label, rel, conf string
		if scanErr := rows.Scan(&id, &label, &rel, &conf); scanErr == nil {
			out = append(out, ContextNeighbor{ID: id, Label: label, RelationType: rel, Confidence: conf})
		}
	}
	return out, rows.Err()
}

// maxSnippetLines is the maximum number of source lines included in a surgical snippet.
const maxSnippetLines = 60

// extractFileSkeleton returns a structured AST skeleton (list of symbols)
// for a file node by querying file-scoped symbol nodes in graph_nodes and any
// outgoing 'contains' edges in the graph.
func extractFileSkeleton(db *sql.DB, projectID string, node *ContextNode, projPath string) (string, int) {
	rows, err := db.Query(`
		SELECT DISTINCT n.label, n.node_type
		FROM graph_nodes n
		WHERE n.project_id = ?
		  AND (
		    (n.path = ? AND n.id != ? AND n.node_type NOT IN ('file', 'document', 'sql', 'route', 'concept', 'spec'))
		    OR n.id IN (
		      SELECT e.target_id
		      FROM graph_edges e
		      WHERE e.project_id = ? AND e.source_id = ? AND e.relation_type = 'contains'
		    )
		  )
		ORDER BY n.label ASC`, projectID, node.Path, node.ID, projectID, node.ID)
	if err != nil {
		return extractSurgicalSnippet(projPath, node, maxSnippetLines)
	}
	defer rows.Close()

	var symbols []string
	for rows.Next() {
		var label, typ string
		if err := rows.Scan(&label, &typ); err == nil {
			symbols = append(symbols, fmt.Sprintf("// - %s (%s)", label, typ))
		}
	}
	if len(symbols) == 0 {
		return extractSurgicalSnippet(projPath, node, maxSnippetLines)
	}

	if len(symbols) > maxSnippetLines {
		symbols = symbols[:maxSnippetLines]
		symbols = append(symbols, "// ... (truncated to prevent token bloat)")
	}

	skeleton := fmt.Sprintf("// AST Skeleton for %s\n// This file exports the following symbols:\n%s", node.Path, strings.Join(symbols, "\n"))
	return skeleton, 1
}

// extractSurgicalSnippet extracts up to maxLines of source code from the workspace for a node.
func extractSurgicalSnippet(projPath string, node *ContextNode, maxLines int) (string, int) {
	if node == nil || node.Path == "" || projPath == "" {
		return "", 0
	}
	if maxLines <= 0 {
		maxLines = maxSnippetLines
	}
	filePath := filepath.Join(projPath, filepath.FromSlash(node.Path))
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", 0
	}

	lines := strings.Split(string(content), "\n")
	if len(lines) == 0 {
		return "", 0
	}

	startLine := node.StartLine
	if startLine <= 0 {
		startLine = 1
	}
	if startLine > len(lines) {
		startLine = 1
	}

	endLine := startLine + maxLines - 1
	truncated := false
	if endLine > len(lines) {
		endLine = len(lines)
	} else if endLine < len(lines) && node.Type != schema.NodeTypeFile {
		truncated = true
	}

	selected := lines[startLine-1 : endLine]
	snippetText := strings.Join(selected, "\n")
	if truncated {
		snippetText += fmt.Sprintf("\n... (%d more lines in %s)", len(lines)-endLine, node.Path)
	}

	return security.SanitizeText(snippetText), startLine
}

// parseStartLine extracts the start line from a graph node's metadata JSON.
