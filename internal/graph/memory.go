package graph

import (
	"database/sql"
	"encoding/json"
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
		if err := nRows.Scan(&n.ID, &n.Type, &n.Label, &n.Path, &metaStr); err == nil {
			_ = json.Unmarshal([]byte(metaStr), &n.Metadata)
			nodeMap[n.ID] = &n
		}
	}
	if err := nRows.Err(); err != nil {
		return nil, err
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

	queue := []string{start}
	parent := map[string]string{start: ""}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

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

		for _, edge := range g.EdgesBySource[curr] {
			if _, visited := parent[edge.TargetID]; !visited {
				parent[edge.TargetID] = curr
				queue = append(queue, edge.TargetID)
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
func (g *InMemoryGraph) Query(start string, maxDepth int, relationType string, direction string) *SubGraph {
	startID := g.findNode(start)
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

// findNode performs a fuzzy match to find a node ID, prioritizing exact matches.
func (g *InMemoryGraph) findNode(start string) string {
	if _, ok := g.Nodes[start]; ok {
		return start
	}
	// Try finding by path first
	for id, n := range g.Nodes {
		if n.Path == start {
			return id
		}
	}
	// Fuzzy match
	for id, n := range g.Nodes {
		if strings.Contains(id, start) || strings.Contains(n.Label, start) {
			return id
		}
	}
	return ""
}
