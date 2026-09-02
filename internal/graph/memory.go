package graph

import (
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
)

// InMemoryGraph holds the full project graph in Go memory so that BFS
// traversals run without any SQL round-trips once the data
// is loaded.
type InMemoryGraph struct {
	Nodes         map[string]*Node
	EdgesBySource map[string][]*Edge
	EdgesByTarget map[string][]*Edge
	FanIn         map[string]int
	FanOut        map[string]int
	HubThreshold  int
}

func (g *InMemoryGraph) ComputeHubThreshold() int {
	if g.HubThreshold > 0 {
		return g.HubThreshold
	}
	if len(g.Nodes) == 0 {
		g.HubThreshold = 50
		return g.HubThreshold
	}
	degrees := make([]int, 0, len(g.Nodes))
	for id := range g.Nodes {
		degrees = append(degrees, g.FanIn[id]+g.FanOut[id])
	}
	sort.Ints(degrees)
	// p99 threshold, minimum 50
	p99Idx := len(degrees) * 99 / 100
	if p99Idx >= len(degrees) {
		p99Idx = len(degrees) - 1
	}
	th := degrees[p99Idx]
	if th < 50 {
		th = 50
	}
	g.HubThreshold = th
	return th
}

// DegreeNode describes a node ranked by total degree (fan-in + fan-out).
type DegreeNode struct {
	ID     string
	Label  string
	Type   string
	Degree int
}

// TopDegreeNodes returns the top n code nodes by total degree (fan-in +
// fan-out) using a single aggregate query, without loading the full graph or
// computing centrality. Document nodes (.md) and external package nodes
// (pkg:*) are excluded so the result is a list of actionable project code hubs
// rather than stdlib/third-party imports with a high fan-in. Used by the
// Auto-Boot session bundle to show architectural hotspots cheaply at session
// start.
func TopDegreeNodes(db *sql.DB, projectID string, n int) ([]DegreeNode, error) {
	if n <= 0 {
		n = 3
	}
	rows, err := db.Query(`
		SELECT n.id, n.label, n.node_type,
			COALESCE(fi.cnt, 0) + COALESCE(fo.cnt, 0) AS degree
		FROM (
			SELECT id, label, node_type
			FROM graph_nodes
			WHERE project_id = ? AND node_type NOT IN ('document', 'package')
		) n
		LEFT JOIN (
			SELECT source_id, COUNT(*) AS cnt FROM graph_edges WHERE project_id = ? GROUP BY source_id
		) fi ON fi.source_id = n.id
		LEFT JOIN (
			SELECT target_id, COUNT(*) AS cnt FROM graph_edges WHERE project_id = ? GROUP BY target_id
		) fo ON fo.target_id = n.id
		ORDER BY degree DESC
		LIMIT ?`, projectID, projectID, projectID, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DegreeNode
	for rows.Next() {
		var d DegreeNode
		if err := rows.Scan(&d.ID, &d.Label, &d.Type, &d.Degree); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// LoadFullGraph executes two queries to load all nodes and edges for a project
// into an InMemoryGraph.
func LoadFullGraph(db *sql.DB, projectID string) (*InMemoryGraph, error) {
	nodeMap := make(map[string]*Node)
	nRows, err := db.Query("SELECT id, node_type, label, path, metadata FROM graph_nodes WHERE project_id = ?", projectID)
	if err != nil {
		return nil, err
	}
	defer nRows.Close()
	for nRows.Next() {
		var n Node
		var metaStr string
		if scanErr := nRows.Scan(&n.ID, &n.Type, &n.Label, &n.Path, &metaStr); scanErr == nil {
			_ = json.Unmarshal([]byte(metaStr), &n.Metadata)
			nodeMap[n.ID] = &n
		}
	}
	if rowsErr := nRows.Err(); rowsErr != nil {
		return nil, rowsErr
	}

	edgesBySrc := make(map[string][]*Edge)
	edgesByTgt := make(map[string][]*Edge)
	fanIn := make(map[string]int)
	fanOut := make(map[string]int)

	eRows, err := db.Query("SELECT id, source_id, target_id, relation_type, confidence, source_location FROM graph_edges WHERE project_id = ?", projectID)
	if err != nil {
		return nil, err
	}
	defer eRows.Close()
	for eRows.Next() {
		var e Edge
		if err := eRows.Scan(&e.ID, &e.SourceID, &e.TargetID, &e.RelationType, &e.Confidence, &e.SourceLocation); err == nil {
			edgesBySrc[e.SourceID] = append(edgesBySrc[e.SourceID], &e)
			edgesByTgt[e.TargetID] = append(edgesByTgt[e.TargetID], &e)
			fanOut[e.SourceID]++
			fanIn[e.TargetID]++
		}
	}
	if err := eRows.Err(); err != nil {
		return nil, err
	}

	return &InMemoryGraph{
		Nodes:         nodeMap,
		EdgesBySource: edgesBySrc,
		EdgesByTarget: edgesByTgt,
		FanIn:         fanIn,
		FanOut:        fanOut,
	}, nil
}

// ShortestPath finds the shortest path between start and end nodes.
func (g *InMemoryGraph) ShortestPath(start, end string, maxHops int) []string {
	if _, ok := g.Nodes[start]; !ok {
		return nil
	}
	if _, ok := g.Nodes[end]; !ok {
		return nil
	}

	type queueItem struct {
		id   string
		hops int
	}

	queue := []queueItem{{id: start}}
	parent := map[string]string{start: ""}

	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]
		curr := item.id

		if curr == end {
			path := []string{}
			for curr != "" {
				path = append([]string{curr}, path...)
				curr = parent[curr]
			}
			return path
		}

		if len(parent) > 10000 {
			break // Safety limit
		}
		if maxHops > 0 && item.hops >= maxHops {
			continue
		}

		for _, edge := range g.EdgesBySource[curr] {
			if _, visited := parent[edge.TargetID]; !visited {
				parent[edge.TargetID] = curr
				queue = append(queue, queueItem{id: edge.TargetID, hops: item.hops + 1})
			}
		}
	}
	return nil
}

// SubGraph is the result of a BFS traversal.
type SubGraph struct {
	Nodes []*Node
	Edges []*Edge
}

// Query performs a BFS traversal to find all nodes and edges within maxDepth,
// filtered by optional relation type and direction ('in', 'out', 'all').
// If hubThreshold > 0, nodes with total degree >= hubThreshold are not expanded through.
func (g *InMemoryGraph) Query(start string, maxDepth int, relationType string, direction string, hubThreshold int) *SubGraph {
	startID := g.FindNode(start)
	if startID == "" {
		return &SubGraph{}
	}

	type queueItem struct {
		id    string
		depth int
	}

	queue := []queueItem{{id: startID, depth: 0}}
	visitedNodes := map[string]*Node{startID: g.Nodes[startID]}
	visitedEdges := map[string]*Edge{}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		if curr.depth >= maxDepth {
			continue
		}

		// Hub suppression: don't expand through nodes that exceed the threshold
		if hubThreshold > 0 {
			deg := g.FanIn[curr.id] + g.FanOut[curr.id]
			if curr.depth > 0 && deg >= hubThreshold {
				continue
			}
		}

		// Explore outgoing edges if direction is 'out' or 'all'
		if direction == "out" || direction == "all" {
			for _, edge := range g.EdgesBySource[curr.id] {
				if relationType != "" && edge.RelationType != relationType {
					continue
				}
				visitedEdges[edge.ID] = edge
				if _, visited := visitedNodes[edge.TargetID]; !visited {
					visitedNodes[edge.TargetID] = g.Nodes[edge.TargetID]
					queue = append(queue, queueItem{id: edge.TargetID, depth: curr.depth + 1})
				}
			}
		}

		// Explore incoming edges if direction is 'in' or 'all'
		if direction == "in" || direction == "all" {
			for _, edge := range g.EdgesByTarget[curr.id] {
				if relationType != "" && edge.RelationType != relationType {
					continue
				}
				visitedEdges[edge.ID] = edge
				if _, visited := visitedNodes[edge.SourceID]; !visited {
					visitedNodes[edge.SourceID] = g.Nodes[edge.SourceID]
					queue = append(queue, queueItem{id: edge.SourceID, depth: curr.depth + 1})
				}
			}
		}
	}

	var nodes []*Node
	for _, n := range visitedNodes {
		nodes = append(nodes, n)
	}
	var edges []*Edge
	for _, e := range visitedEdges {
		edges = append(edges, e)
	}
	return &SubGraph{Nodes: nodes, Edges: edges}
}

// NodeBetweennessCentrality extracts the betweenness_centrality value from a node's metadata.
func NodeBetweennessCentrality(n *Node) float64 {
	if n == nil || n.Metadata == nil {
		return 0.0
	}
	val, ok := n.Metadata["betweenness_centrality"]
	if !ok {
		return 0.0
	}
	switch v := val.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	}
	return 0.0
}

// NodeCommunityID extracts the community_id value from a node's metadata.
func NodeCommunityID(n *Node) int {
	if n == nil || n.Metadata == nil {
		return 0
	}
	val, ok := n.Metadata["community_id"]
	if !ok {
		return 0
	}
	switch v := val.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	}
	return 0
}

// FindNode performs a deterministic fuzzy match to find a node ID, prioritizing
// exact matches (ID, then path, then label) and, within a tie, the shortest ID
// (most specific) with an alphabetical fallback. Go's map iteration order is
// randomized, so without this ordering a query that matches several nodes would
// resolve to a different node on every call, producing inconsistent context for
// sv_graph_query / sv_graph_path / sv_graph_explain.
func (g *InMemoryGraph) FindNode(start string) string {
	if _, ok := g.Nodes[start]; ok {
		return start
	}

	bestID := ""
	bestRank := 0
	for id, n := range g.Nodes {
		var rank int
		switch {
		case n.Path == start:
			rank = 4
		case n.Label == start:
			rank = 3
		case strings.Contains(id, start) || strings.Contains(n.Label, start):
			rank = 2
		}
		if rank == 0 {
			continue
		}
		if rank > bestRank ||
			(rank == bestRank && (len(id) < len(bestID) || (len(id) == len(bestID) && id < bestID))) {
			bestID = id
			bestRank = rank
		}
	}
	return bestID
}

// SearchResult describes a node matched by SearchNodes.
type SearchResult struct {
	ID        string
	Label     string
	Type      string
	Path      string
	FanIn     int
	FanOut    int
	Degree    int
	Community int
	Rank      int
}

// SearchNodes returns every node whose id, label, or path contains the query,
// ranked deterministically (exact path > exact label > substring; ties broken
// by shortest id then alphabetical) and bounded by limit. Unlike FindNode,
// which returns a single best match for navigation, SearchNodes is the
// discovery path: agents that do not know the exact symbol can list all
// matching code nodes with their network metrics. The optional nodeType filter
// restricts matches (file/function/class/etc).
func (g *InMemoryGraph) SearchNodes(query, nodeType string, limit int) []SearchResult {
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	if query == "" {
		return nil
	}

	var out []SearchResult
	for id, n := range g.Nodes {
		if nodeType != "" && n.Type != nodeType {
			continue
		}
		var rank int
		switch {
		case n.Path == query:
			rank = 4
		case n.Label == query:
			rank = 3
		case strings.Contains(id, query) || strings.Contains(n.Label, query) || strings.Contains(n.Path, query):
			rank = 2
		}
		if rank == 0 {
			continue
		}
		degree := g.FanIn[id] + g.FanOut[id]
		out = append(out, SearchResult{
			ID:        id,
			Label:     n.Label,
			Type:      n.Type,
			Path:      n.Path,
			FanIn:     g.FanIn[id],
			FanOut:    g.FanOut[id],
			Degree:    degree,
			Community: NodeCommunityID(n),
			Rank:      rank,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Rank != out[j].Rank {
			return out[i].Rank > out[j].Rank
		}
		if out[i].Degree != out[j].Degree {
			return out[i].Degree > out[j].Degree
		}
		if len(out[i].ID) != len(out[j].ID) {
			return len(out[i].ID) < len(out[j].ID)
		}
		return out[i].ID < out[j].ID
	})

	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
