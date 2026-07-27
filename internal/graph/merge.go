package graph

import (
	"encoding/json"
	"fmt"
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

func (g *InMemoryGraph) MergeToJSON(other *InMemoryGraph) string {
	merged := g.Merge(other)

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
		Nodes: make([]*Node, 0, len(merged.Nodes)),
		Edges: make([]mergeEdge, 0),
	}

	nodeIDs := make([]string, 0, len(merged.Nodes))
	for id := range merged.Nodes {
		nodeIDs = append(nodeIDs, id)
	}
	sort.Strings(nodeIDs)
	for _, id := range nodeIDs {
		m.Nodes = append(m.Nodes, merged.Nodes[id])
	}

	seen := make(map[string]bool)
	for _, edges := range merged.EdgesBySource {
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

func (g *InMemoryGraph) MergeFromJSON(jsonStr string) error {
	type mergeEdge struct {
		Source string `json:"source"`
		Target string `json:"target"`
		Type   string `json:"type"`
	}
	type mergeGraph struct {
		Nodes []*Node     `json:"nodes"`
		Edges []mergeEdge `json:"edges"`
	}

	var m mergeGraph
	if err := json.Unmarshal([]byte(jsonStr), &m); err != nil {
		return fmt.Errorf("failed to parse graph JSON: %w", err)
	}

	for _, n := range m.Nodes {
		if existing, ok := g.Nodes[n.ID]; ok {
			for k, v := range n.Metadata {
				existing.Metadata[k] = v
			}
		} else {
			if n.Metadata == nil {
				n.Metadata = make(map[string]interface{})
			}
			g.Nodes[n.ID] = n
		}
	}

	seen := make(map[string]bool)
	for _, mEdge := range m.Edges {
		key := mEdge.Source + "|" + mEdge.Target + "|" + mEdge.Type
		if seen[key] {
			continue
		}
		seen[key] = true

		e := &Edge{
			ID:           fmt.Sprintf("%s-%s-%s", mEdge.Source, mEdge.Target, mEdge.Type),
			SourceID:     mEdge.Source,
			TargetID:     mEdge.Target,
			RelationType: mEdge.Type,
			Confidence:   "EXTRACTED",
		}

		g.EdgesBySource[e.SourceID] = append(g.EdgesBySource[e.SourceID], e)
		g.EdgesByTarget[e.TargetID] = append(g.EdgesByTarget[e.TargetID], e)
		g.FanOut[e.SourceID]++
		g.FanIn[e.TargetID]++
	}

	return nil
}
