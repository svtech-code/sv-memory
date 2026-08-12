package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

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

// ContextPack is the fused, token-efficient context for a code path: the
// node's structural role plus the memories that explain why the code is the
// way it is (decisions/standards/bugfixes linked to that path).
type ContextPack struct {
	Node         *ContextNode
	Memories     []ContextMemory
	Dependents   []ContextNeighbor
	Dependencies []ContextNeighbor
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
		var m map[string]interface{}
		if jsonErr := json.Unmarshal([]byte(metadata), &m); jsonErr == nil {
			switch v := m["community_id"].(type) {
			case float64:
				node.CommunityID = int(v)
			case int:
				node.CommunityID = v
			case int64:
				node.CommunityID = int(v)
			}
		}
	}
	return node, nil
}

// GetContextPack builds the compact context pack for a code path. maxMemories
// caps how many linked memories are returned (0 = default 5). The graph may be
// empty (never synced); in that case a pack with no node but path-scoped
// memories is still returned so the call never hard-fails.
func GetContextPack(db *sql.DB, projectID, query string, maxMemories int) (*ContextPack, error) {
	if maxMemories <= 0 {
		maxMemories = 5
	}
	if maxMemories > 20 {
		maxMemories = 20
	}

	node, err := ResolveContextNode(db, projectID, query)
	if err != nil {
		return nil, err
	}

	pack := &ContextPack{Node: node}
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
	}

	if len(mems) > maxMemories {
		mems = mems[:maxMemories]
	}
	pack.Memories = mems
	return pack, nil
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
	if len(p.Dependencies) > 0 {
		sb.WriteString("\n### Direct dependencies (what this imports/calls):\n")
		for _, d := range p.Dependencies {
			fmt.Fprintf(&sb, "- `%s` (%s, %s)\n", d.Label, d.RelationType, strings.ToLower(d.Confidence))
		}
	}

	if p.Node == nil && len(p.Memories) == 0 {
		return "No context found: no matching graph node and no path-scoped memories for the given path."
	}
	return strings.TrimSpace(sb.String())
}
