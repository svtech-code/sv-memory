package graph

import (
	"encoding/json"
	"sort"
)

func (g *InMemoryGraph) Merge(other *InMemoryGraph) *InMemoryGraph {
	result := &InMemoryGraph{
		Nodes:         make(map[string]*Node),
		EdgesBySource: make(map[string][]*Edge),
		EdgesByTarget: make(map[string][]*Edge),
		FanIn:         make(map[string]int),
		FanOut:        make(map[string]int),
	}

	for id, n := range g.Nodes {
		metaCopy := make(map[string]interface{})
		for k, v := range n.Metadata {
			metaCopy[k] = v
		}
		result.Nodes[id] = &Node{
			ID:       n.ID,
			Type:     n.Type,
			Label:    n.Label,
			Path:     n.Path,
			Metadata: metaCopy,
		}
	}

	for id, n := range other.Nodes {
		if existing, ok := result.Nodes[id]; ok {
			metaCopy := make(map[string]interface{})
			for k, v := range existing.Metadata {
				metaCopy[k] = v
			}
			for k, v := range n.Metadata {
				metaCopy[k] = v
			}
			existing.Metadata = metaCopy
		} else {
			metaCopy := make(map[string]interface{})
			for k, v := range n.Metadata {
				metaCopy[k] = v
			}
			result.Nodes[id] = &Node{
				ID:       n.ID,
				Type:     n.Type,
				Label:    n.Label,
				Path:     n.Path,
				Metadata: metaCopy,
			}
		}
	}

	seenEdges := make(map[string]bool)
	addEdge := func(e *Edge) {
		key := e.SourceID + "|" + e.TargetID + "|" + e.RelationType
		if seenEdges[key] {
			return
		}
		seenEdges[key] = true

		edgeCopy := &Edge{
			ID:             e.ID,
			SourceID:       e.SourceID,
			TargetID:       e.TargetID,
			RelationType:   e.RelationType,
			Confidence:     e.Confidence,
			SourceLocation: e.SourceLocation,
		}

		result.EdgesBySource[e.SourceID] = append(result.EdgesBySource[e.SourceID], edgeCopy)
		result.EdgesByTarget[e.TargetID] = append(result.EdgesByTarget[e.TargetID], edgeCopy)
		result.FanOut[e.SourceID]++
		result.FanIn[e.TargetID]++
	}

	for _, edges := range g.EdgesBySource {
		for _, e := range edges {
			addEdge(e)
		}
	}
	for _, edges := range other.EdgesBySource {
		for _, e := range edges {
			addEdge(e)
		}
	}

	hubThreshold := g.ComputeHubThreshold()
	otherHub := other.ComputeHubThreshold()
	if otherHub > hubThreshold {
		hubThreshold = otherHub
	}
	result.HubThreshold = hubThreshold

	return result
}

// SerializeJSON returns a deterministic JSON serialization of the graph. Node
// IDs are sorted and edge duplicates are dropped so the output is stable across
// runs.
func (g *InMemoryGraph) SerializeJSON() string {
	type mergeEdge struct {
		Source string `json:"source"`
		Target string `json:"target"`
		Type   string `json:"type"`
	}

	type mergeGraph struct {
		Nodes []*Node     `json:"nodes"`
		Edges []mergeEdge `json:"edges"`
	}

	m := mergeGraph{
		Nodes: make([]*Node, 0, len(g.Nodes)),
		Edges: make([]mergeEdge, 0),
	}

	nodeIDs := make([]string, 0, len(g.Nodes))
	for id := range g.Nodes {
		nodeIDs = append(nodeIDs, id)
	}
	sort.Strings(nodeIDs)
	for _, id := range nodeIDs {
		m.Nodes = append(m.Nodes, g.Nodes[id])
	}

	seen := make(map[string]bool)
	for _, edges := range g.EdgesBySource {
		for _, e := range edges {
			key := e.SourceID + "->" + e.TargetID
			if seen[key] {
				continue
			}
			seen[key] = true
			m.Edges = append(m.Edges, mergeEdge{
				Source: e.SourceID,
				Target: e.TargetID,
				Type:   e.RelationType,
			})
		}
	}

	jsonBytes, _ := json.MarshalIndent(m, "", "  ")
	return string(jsonBytes)
}
