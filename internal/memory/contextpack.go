package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/svtech-code/sv-memory/internal/graph"
	"github.com/svtech-code/sv-memory/internal/graph/schema"
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
	ID           string
	Slug         string
	Status       string
	Title        string
	TaskProgress string
}

// ContextCapability is a capability (a 'spec' graph node) that the target path
// implements, surfaced by the context pack with a bounded requirement summary
// so the agent sees the applicable contract without reading the full spec.
type ContextCapability struct {
	CapabilityPath   string
	RequirementCount int
	Requirements     []string
}

// ContextSnippet is a surgical source-code excerpt for a secondary symbol
// surfaced by the unified explore call. It lets the agent see the bodies of
// related symbols (callers/callees or a second queried symbol) without a
// separate Read call, mirroring the "source is already read" contract.
type ContextSnippet struct {
	Label     string
	Path      string
	StartLine int
	Text      string
}

// CallPathHop is one step in the shortest dependency path between two queried
// symbols. Direction is always from source to target of a graph edge.
type CallPathHop struct {
	Label        string
	RelationType string
	Confidence   string
}

// ContextPack is the fused, token-efficient context for a code path: the
// node's structural role, surgical source code snippet, transitive blast radius,
// plus the memories that explain why the code is the way it is (decisions/standards/bugfixes linked to that path).
// When the caller passes multiple comma-separated symbols, the pack also carries
// surgical snippets for the secondary symbols and the shortest call path between
// the two most significant ones, turning the pack into a unified "explore" call.
type ContextPack struct {
	Node          *ContextNode
	Snippet       string
	SnippetLine   int
	BlastRadius   []graph.BlastRadiusNode
	Memories      []ContextMemory
	Dependents    []ContextNeighbor
	Dependencies  []ContextNeighbor
	Changes       []ContextChange
	Capabilities  []ContextCapability
	ExtraSnippets []ContextSnippet
	CallPath      []CallPathHop
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

// resolveProjectPath returns the project's workspace path, falling back to the
// current working directory when the project row has none.
func resolveProjectPath(db *sql.DB, projectID string) string {
	var projPath string
	_ = db.QueryRow("SELECT path FROM projects WHERE id = ?", projectID).Scan(&projPath)
	if projPath == "" {
		projPath, _ = os.Getwd()
	}
	return projPath
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
		return communityPaths(db, projectID, node.CommunityID, set)
	}

	// Fallback before communities are computed: same-directory nodes are a
	// cheap structural proxy for the module.
	dir := path.Dir(node.Path)
	if dir != "." && dir != "" && dir != "/" {
		return directoryPaths(db, projectID, dir, set)
	}
	return set, nil
}

// communityPaths adds every node in the given community to set.
func communityPaths(db *sql.DB, projectID string, communityID int, set map[string]bool) (map[string]bool, error) {
	rows, err := db.Query(`SELECT path, COALESCE(metadata, '') FROM graph_nodes WHERE project_id = ? AND node_type != 'document'`, projectID)
	if err != nil {
		return set, fmt.Errorf("failed querying community nodes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p, meta string
		if scanErr := rows.Scan(&p, &meta); scanErr == nil && p != "" && parseCommunityID(meta) == communityID {
			set[p] = true
		}
	}
	return set, rows.Err()
}

// directoryPaths adds every node under dir to set.
func directoryPaths(db *sql.DB, projectID, dir string, set map[string]bool) (map[string]bool, error) {
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

func splitContextQuery(query string) []string {
	parts := strings.Split(query, ",")
	var out []string
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
			if len(out) >= 3 {
				break
			}
		}
	}
	if len(out) == 0 {
		return []string{strings.TrimSpace(query)}
	}
	return out
}

// resolveExploreSymbols is the multi-symbol branch of GetContextPack for a
// unified explore call. It returns the primary symbol (first of the comma list,
// or the query itself for a single symbol), the surgical snippets for secondary
// symbols, and the shortest dependency path between the two most significant
// symbols. A single symbol returns no extras and keeps the legacy single-node
// behaviour.
func resolveExploreSymbols(db *sql.DB, projectID, projPath, query string) (string, []ContextSnippet, []CallPathHop) {
	symbols := splitContextQuery(query)
	if len(symbols) <= 1 {
		return query, nil, nil
	}
	primary := symbols[0]
	var extra []ContextSnippet
	for _, sym := range symbols[1:] {
		extraNode, resErr := ResolveContextNode(db, projectID, strings.TrimSpace(sym))
		if resErr != nil || extraNode == nil || extraNode.Path == "" || projPath == "" {
			continue
		}
		var text string
		var startLine int
		if extraNode.Type == schema.NodeTypeFile {
			text, startLine = extractFileSkeleton(db, projectID, extraNode, projPath)
		} else {
			text, startLine = extractSurgicalSnippet(projPath, extraNode, maxSnippetLines)
		}
		if text == "" {
			continue
		}
		extra = append(extra, ContextSnippet{
			Label:     extraNode.Label,
			Path:      extraNode.Path,
			StartLine: startLine,
			Text:      text,
		})
	}

	// Shortest dependency path between the two most significant symbols.
	var callPath []CallPathHop
	start, end := resolveChainEndpoints(db, projectID, symbols)
	if start != "" && end != "" {
		callPath = computeCallPath(db, projectID, start, end)
	}
	return primary, extra, callPath
}

// resolveChainEndpoints returns the resolved node IDs (best effort) for the
// first two symbols that actually resolve, so computeCallPath has concrete
// endpoints to join.
func resolveChainEndpoints(db *sql.DB, projectID string, symbols []string) (string, string) {
	var ids []string
	for _, sym := range symbols {
		if len(ids) >= 2 {
			break
		}
		n, err := ResolveContextNode(db, projectID, strings.TrimSpace(sym))
		if err != nil || n == nil {
			continue
		}
		ids = append(ids, n.ID)
	}
	if len(ids) < 2 {
		return "", ""
	}
	return ids[0], ids[1]
}

// computeCallPath loads the in-memory graph once and returns the shortest
// dependency path between start and end (from source toward target). It returns
// nil when the graph is empty, a node is missing, or no path exists within the
// hop limit.
func computeCallPath(db *sql.DB, projectID, startID, endID string) []CallPathHop {
	g, err := graph.LoadFullGraph(db, projectID)
	if err != nil || g == nil {
		return nil
	}
	pathIDs := g.ShortestPath(startID, endID, maxCallPathHops)
	if len(pathIDs) < 2 {
		return nil
	}
	var out []CallPathHop
	for i := 0; i < len(pathIDs); i++ {
		label := pathIDs[i]
		if n := g.Nodes[pathIDs[i]]; n != nil && n.Label != "" {
			label = n.Label
		}
		hop := CallPathHop{Label: label}
		if i+1 < len(pathIDs) {
			for _, e := range g.EdgesBySource[pathIDs[i]] {
				if e.TargetID == pathIDs[i+1] {
					hop.RelationType = e.RelationType
					hop.Confidence = e.Confidence
					if hop.Confidence == "" {
						hop.Confidence = "EXTRACTED"
					}
					break
				}
			}
		}
		out = append(out, hop)
	}
	return out
}

// maxCallPathHops bounds the explore call-path computation so the graph BFS
// stays cheap even on large projects.
const maxCallPathHops = 8

// renderCallPath writes the "Call path" section: a compact chain of node labels
// with per-hop relation type and confidence annotations.
