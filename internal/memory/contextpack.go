package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/svtech-code/sv-memory/internal/graph"
	"github.com/svtech-code/sv-memory/internal/graph/schema"
	"github.com/svtech-code/sv-memory/internal/security"
)

// ContextNode is a compact summary of a graph node's structural role used by
// the context pack. It avoids loading the full graph so the pack is cheap
// enough for a CLI call from a PreToolUse hook.
type ContextNode struct {
	ID          string
	Label       string
	Type        string
	Path        string
	StartLine   int
	CommunityID int
	FanIn       int
	FanOut      int
	Degree      int
}

// ContextMemory is a memory linked to the target path either through its
// where_path or through a rationale_for edge in the code graph. The Why field
// is left untruncated here; RenderContextPack caps it per configured chars.
type ContextMemory struct {
	ID        string
	Category  string
	What      string
	Why       string
	WherePath string
	Source    string // "where_path" or "rationale_for"
}

// ContextNeighbor is a direct dependent or dependency of the target node.
type ContextNeighbor struct {
	ID           string
	Label        string
	RelationType string
	Confidence   string
}

// ContextChange is a compact summary of an active spec change affecting the
// target path, surfaced by the context pack when include_changes is set. Only
// the fields needed for the agent to decide whether to review it are carried.
type ContextChange struct {
	ID     string
	Slug   string
	Status string
	Title  string
}

// ContextCapability is a capability (a 'spec' graph node) that the target path
// implements, surfaced by the context pack with a bounded requirement summary
// so the agent sees the applicable contract without reading the full spec.
type ContextCapability struct {
	CapabilityPath   string
	RequirementCount int
	Requirements     []string
}

// ContextPack is the fused, token-efficient context for a code path: the
// node's structural role, surgical source code snippet, transitive blast radius,
// plus the memories that explain why the code is the way it is (decisions/standards/bugfixes linked to that path).
type ContextPack struct {
	Node         *ContextNode
	Snippet      string
	SnippetLine  int
	BlastRadius  []graph.BlastRadiusNode
	Memories     []ContextMemory
	Dependents   []ContextNeighbor
	Dependencies []ContextNeighbor
	Changes      []ContextChange
	Capabilities []ContextCapability
}

// ResolveContextNode finds the graph node matching query, trying exact id,
// exact path, exact label, then fuzzy path/label matches. Returns nil when no
// node matches. This is the proprietary bridge between the code graph and the
// memory store: given a path the agent is about to touch, it resolves the node
// so linked memories can be pulled with one bounded call.
func ResolveContextNode(db *sql.DB, projectID, query string) (*ContextNode, error) {
	pattern := "%" + sanitizePathFilter(query) + "%"
	var id, label, nodeType, path, metadata string
	err := db.QueryRow(`
		SELECT id, label, node_type, path, COALESCE(metadata, '')
		FROM graph_nodes
		WHERE project_id = ?
		  AND (id = ? OR path = ? OR label = ? OR path LIKE ? ESCAPE '\' OR label LIKE ? ESCAPE '\')
		ORDER BY CASE
			WHEN id = ? THEN 0
			WHEN path = ? THEN 1
			WHEN label = ? THEN 2
			WHEN path LIKE ? ESCAPE '\' THEN 3
			ELSE 4
		END
		LIMIT 1`,
		projectID, query, query, query, pattern, pattern,
		query, query, query, pattern).Scan(&id, &label, &nodeType, &path, &metadata)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed resolving graph node: %w", err)
	}

	var fanIn, fanOut int
	err = db.QueryRow(`
		SELECT
			(SELECT COUNT(*) FROM graph_edges WHERE project_id = ? AND target_id = ?) AS fan_in,
			(SELECT COUNT(*) FROM graph_edges WHERE project_id = ? AND source_id = ?) AS fan_out`,
		projectID, id, projectID, id).Scan(&fanIn, &fanOut)
	if err != nil {
		return nil, fmt.Errorf("failed computing node degree: %w", err)
	}

	node := &ContextNode{
		ID:     id,
		Label:  label,
		Type:   nodeType,
		Path:   path,
		FanIn:  fanIn,
		FanOut: fanOut,
		Degree: fanIn + fanOut,
	}
	if metadata != "" {
		node.CommunityID = parseCommunityID(metadata)
		node.StartLine = parseStartLine(metadata)
	}
	return node, nil
}

// GetContextPack builds the compact context pack for a code path. maxMemories
// caps how many linked memories are returned (0 = default 5). When includeChanges
// is true, active spec changes whose where_path matches the query are appended
// so the agent sees in-flight proposals before touching the path. The graph may
// be empty (never synced); in that case a pack with no node but path-scoped
// memories is still returned so the call never hard-fails.
func GetContextPack(db *sql.DB, projectID, query string, maxMemories int, includeChanges bool) (*ContextPack, error) {
	if maxMemories <= 0 {
		maxMemories = 5
	}
	if maxMemories > 20 {
		maxMemories = 20
	}

	// 0. Auto-freshness check: refresh graph incrementally if project files changed on disk.
	var projPath string
	_ = db.QueryRow("SELECT path FROM projects WHERE id = ?", projectID).Scan(&projPath)
	if projPath == "" {
		projPath, _ = os.Getwd()
	}
	if projPath != "" {
		_, _ = graph.SyncGraphIfHasChanges(db, projectID, projPath)
	}

	node, err := ResolveContextNode(db, projectID, query)
	if err != nil {
		return nil, err
	}

	pack := &ContextPack{Node: node}

	// 1. Extract surgical source code snippet and blast radius for the resolved node if available.
	if node != nil {
		if node.Path != "" && projPath != "" {
			pack.Snippet, pack.SnippetLine = extractSurgicalSnippet(projPath, node, maxSnippetLines)
		}
		pack.BlastRadius, _ = graph.CalculateBlastRadius(db, projectID, node.ID, 3, 10)
	}

	seen := map[string]bool{}
	var mems []ContextMemory

	// 1. Memories whose where_path matches the query (path-scoped recall).
	pattern := "%" + sanitizePathFilter(query) + "%"
	pathRows, err := db.Query(`
		SELECT id, category, what, COALESCE(why, ''), COALESCE(where_path, '')
		FROM memories
		WHERE project_id = ? AND deleted_at IS NULL
		  AND (where_path LIKE ? ESCAPE '\' OR where_path = ?)
		ORDER BY created_at DESC
		LIMIT ?`, projectID, pattern, query, maxMemories)
	if err != nil {
		return nil, fmt.Errorf("failed querying path memories: %w", err)
	}
	for pathRows.Next() {
		var id, category, what, why, wherePath string
		if scanErr := pathRows.Scan(&id, &category, &what, &why, &wherePath); scanErr == nil {
			if !seen[id] {
				seen[id] = true
				mems = append(mems, ContextMemory{ID: id, Category: category, What: what, Why: why, WherePath: wherePath, Source: "where_path"})
			}
		}
	}
	pathRows.Close()
	if rowsErr := pathRows.Err(); rowsErr != nil {
		return nil, rowsErr
	}

	// 2. Memories linked through a rationale_for edge to the resolved node
	// (memory document node -> code node). This is the graph-native recall.
	if node != nil {
		edgeRows, err := db.Query(`
			SELECT m.id, m.category, m.what, COALESCE(m.why, ''), COALESCE(m.where_path, '')
			FROM graph_edges e
			JOIN memories m ON m.id = e.source_id
			WHERE e.project_id = ? AND e.relation_type = 'rationale_for' AND e.target_id = ?
			  AND m.deleted_at IS NULL
			ORDER BY m.created_at DESC
			LIMIT ?`, projectID, node.ID, maxMemories)
		if err != nil {
			return nil, fmt.Errorf("failed querying rationale memories: %w", err)
		}
		for edgeRows.Next() {
			var id, category, what, why, wherePath string
			if scanErr := edgeRows.Scan(&id, &category, &what, &why, &wherePath); scanErr == nil {
				if !seen[id] {
					seen[id] = true
					mems = append(mems, ContextMemory{ID: id, Category: category, What: what, Why: why, WherePath: wherePath, Source: "rationale_for"})
				}
			}
		}
		edgeRows.Close()
		if rowsErr := edgeRows.Err(); rowsErr != nil {
			return nil, rowsErr
		}

		// 3. Direct neighbors (dependents/dependencies), excluding rationale_for
		// edges so memory document nodes don't appear as code dependents.
		pack.Dependents, err = contextNeighbors(db, projectID, node.ID, "target_id", 5)
		if err != nil {
			return nil, err
		}
		pack.Dependencies, err = contextNeighbors(db, projectID, node.ID, "source_id", 5)
		if err != nil {
			return nil, err
		}

		// 4. Capabilities this node implements (spec nodes reached via
		// 'implements' edges), each with a bounded requirement summary.
		pack.Capabilities, err = capabilitiesForNode(db, projectID, node.ID)
		if err != nil {
			return nil, err
		}
	}

	if len(mems) > maxMemories {
		mems = mems[:maxMemories]
	}
	pack.Memories = mems

	if includeChanges {
		changes, err := changesForPath(db, projectID, query)
		if err == nil {
			pack.Changes = changes
		}
	}
	return pack, nil
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
		SELECT id, slug, status, title
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
		if scanErr := rows.Scan(&c.ID, &c.Slug, &c.Status, &c.Title); scanErr == nil {
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

// CommunityPathSet returns the set of graph node paths structurally related to
// node: every node in the same persisted community (community_id metadata) when
// available, otherwise the node's own directory prefix. This is the graph-aware
// recall used by the graph_boost search expansion: a module search surfaces
// memories for the whole community, not just the exact path.
func CommunityPathSet(db *sql.DB, projectID string, node *ContextNode) (map[string]bool, error) {
	set := map[string]bool{}
	if node == nil {
		return set, nil
	}
	if node.Path != "" {
		set[node.Path] = true
	}
	if node.CommunityID > 0 {
		rows, err := db.Query(`SELECT path, COALESCE(metadata, '') FROM graph_nodes WHERE project_id = ? AND node_type != 'document'`, projectID)
		if err != nil {
			return set, fmt.Errorf("failed querying community nodes: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var p, meta string
			if scanErr := rows.Scan(&p, &meta); scanErr == nil && p != "" && parseCommunityID(meta) == node.CommunityID {
				set[p] = true
			}
		}
		return set, rows.Err()
	}

	// Fallback before communities are computed: same-directory nodes are a
	// cheap structural proxy for the module.
	dir := path.Dir(node.Path)
	if dir != "." && dir != "" && dir != "/" {
		rows, err := db.Query(`SELECT path FROM graph_nodes WHERE project_id = ? AND node_type != 'document' AND path LIKE ? ESCAPE '\'`, projectID, dir+"/%")
		if err != nil {
			return set, fmt.Errorf("failed querying directory nodes: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var p string
			if scanErr := rows.Scan(&p); scanErr == nil && p != "" {
				set[p] = true
			}
		}
		return set, rows.Err()
	}
	return set, nil
}

// parseCommunityID extracts the persisted community_id from a graph node's
// metadata JSON. Returns 0 when absent or malformed.
func parseCommunityID(metadata string) int {
	if metadata == "" {
		return 0
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(metadata), &m); err != nil {
		return 0
	}
	switch v := m["community_id"].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	}
	return 0
}

// maxSnippetLines is the maximum number of source lines included in a surgical snippet.
const maxSnippetLines = 60

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
func parseStartLine(metadata string) int {
	if metadata == "" {
		return 0
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(metadata), &m); err != nil {
		return 0
	}
	if lineVal, ok := m["line"]; ok {
		switch v := lineVal.(type) {
		case float64:
			return int(v)
		case int:
			return v
		case int64:
			return int(v)
		}
	}
	return 0
}

// RenderContextPack renders a context pack as compact markdown text. Each
// memory's why is capped at whyChars to keep the pack token-efficient; the
// agent drills down with sv_mem_get only when needed.
func RenderContextPack(p *ContextPack, whyChars int) string {
	var sb strings.Builder

	if p.Node != nil {
		n := p.Node
		fmt.Fprintf(&sb, "## Context Pack: `%s` (`%s`)\n", n.Label, n.ID)
		fmt.Fprintf(&sb, "- **Type:** `%s`", n.Type)
		if n.Path != "" {
			fmt.Fprintf(&sb, " · **Path:** `%s`", n.Path)
		}
		sb.WriteString("\n")
		fmt.Fprintf(&sb, "- **Fan-in (dependents):** %d · **Fan-out (dependencies):** %d · **Degree:** %d\n", n.FanIn, n.FanOut, n.Degree)
		community := "none"
		if n.CommunityID > 0 {
			community = fmt.Sprintf("ID %d", n.CommunityID)
		}
		fmt.Fprintf(&sb, "- **Community:** %s\n", community)
		if n.Degree > 10 {
			sb.WriteString("- **Hub:** ⚠️ high connectivity — changes ripple; use `sv_graph_explain` before refactoring\n")
		}
	}

	if p.Snippet != "" {
		ext := "text"
		if p.Node != nil && p.Node.Path != "" {
			cleanExt := strings.TrimPrefix(filepath.Ext(p.Node.Path), ".")
			if cleanExt != "" {
				ext = cleanExt
			}
		}
		fmt.Fprintf(&sb, "\n### Source Code Snippet (L%d):\n```%s\n%s\n```\n", p.SnippetLine, ext, p.Snippet)
	}

	if len(p.Memories) > 0 {
		sb.WriteString("\n### Linked memories (why this code is the way it is):\n")
		for _, m := range p.Memories {
			fmt.Fprintf(&sb, "- **[%s] %s** (ID: %s)\n", strings.ToUpper(m.Category), m.What, m.ID)
			if m.Why != "" {
				fmt.Fprintf(&sb, "  *Why:* %s\n", TruncateText(security.SanitizeText(m.Why), whyChars))
			}
			if m.Source == "rationale_for" {
				sb.WriteString("  *linked via code graph (rationale_for)*\n")
			}
		}
		sb.WriteString("\n*Drill down with `sv_mem_get(id=\"<id>\")` for full content.*\n")
	}

	if len(p.Dependents) > 0 {
		sb.WriteString("\n### Direct dependents (who imports/calls this):\n")
		for _, d := range p.Dependents {
			fmt.Fprintf(&sb, "- `%s` (%s, %s)\n", d.Label, d.RelationType, strings.ToLower(d.Confidence))
		}
	}
	if len(p.BlastRadius) > 0 {
		sb.WriteString("\n### 💥 Transitive blast radius (affected upstream consumers):\n")
		for _, b := range p.BlastRadius {
			hubTag := ""
			if b.IsHub {
				hubTag = " ⚠️ Hub"
			}
			fmt.Fprintf(&sb, "- `%s` (hop %d, %s)%s\n", b.ID, b.Depth, b.RelationType, hubTag)
		}
	}
	if len(p.Dependencies) > 0 {
		sb.WriteString("\n### Direct dependencies (what this imports/calls):\n")
		for _, d := range p.Dependencies {
			fmt.Fprintf(&sb, "- `%s` (%s, %s)\n", d.Label, d.RelationType, strings.ToLower(d.Confidence))
		}
	}

	if len(p.Changes) > 0 {
		sb.WriteString("\n### Active changes affecting this path:\n")
		for _, c := range p.Changes {
			fmt.Fprintf(&sb, "- **[%s] %s** (`%s`, slug `%s`)\n", strings.ToUpper(c.Status), c.Title, c.ID, c.Slug)
		}
		sb.WriteString("\n*Review with `sv_validate_decision(change_id=\"<id>\")` before modifying this code.*\n")
	}

	if len(p.Capabilities) > 0 {
		sb.WriteString("\n### Capabilities implemented here:\n")
		for _, cap := range p.Capabilities {
			fmt.Fprintf(&sb, "- **%s** (%d requirement%s)", cap.CapabilityPath, cap.RequirementCount, plural(cap.RequirementCount))
			if len(cap.Requirements) > 0 {
				sb.WriteString(": " + strings.Join(cap.Requirements, ", "))
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n*Full spec: `.sv-memory/specs/capabilities/<cap>/spec.md` — drill down with `sv_mem_context_pack` or `sv_graph_query`.*\n")
	}

	if p.Node == nil && len(p.Memories) == 0 && len(p.Changes) == 0 && len(p.Capabilities) == 0 {
		return "No context found: no matching graph node and no path-scoped memories for the given path."
	}
	return strings.TrimSpace(sb.String())
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
