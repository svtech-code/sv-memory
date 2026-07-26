package graph

import (
	"testing"
)

func TestDetectCommunitiesAndCentrality(t *testing.T) {
	// Create a simple test graph:
	// A -> B -> C
	// D -> E -> F
	// B -> E (bridge edge)
	g := &InMemoryGraph{
		Nodes: map[string]*Node{
			"A": {ID: "A", Label: "A.go", Type: "file"},
			"B": {ID: "B", Label: "B.go", Type: "file"},
			"C": {ID: "C", Label: "C.go", Type: "file"},
			"D": {ID: "D", Label: "D.go", Type: "file"},
			"E": {ID: "E", Label: "E.go", Type: "file"},
			"F": {ID: "F", Label: "F.go", Type: "file"},
		},
		EdgesBySource: map[string][]*Edge{
			"A": {{SourceID: "A", TargetID: "B", RelationType: "imports"}},
			"B": {
				{SourceID: "B", TargetID: "C", RelationType: "imports"},
				{SourceID: "B", TargetID: "E", RelationType: "calls"},
			},
			"D": {{SourceID: "D", TargetID: "E", RelationType: "imports"}},
			"E": {{SourceID: "E", TargetID: "F", RelationType: "imports"}},
		},
		EdgesByTarget: map[string][]*Edge{
			"B": {{SourceID: "A", TargetID: "B", RelationType: "imports"}},
			"C": {{SourceID: "B", TargetID: "C", RelationType: "imports"}},
			"E": {
				{SourceID: "B", TargetID: "E", RelationType: "calls"},
				{SourceID: "D", TargetID: "E", RelationType: "imports"},
			},
			"F": {{SourceID: "E", TargetID: "F", RelationType: "imports"}},
		},
		FanIn: map[string]int{
			"B": 1, "C": 1, "E": 2, "F": 1,
		},
		FanOut: map[string]int{
			"A": 1, "B": 2, "D": 1, "E": 1,
		},
	}

	comms := g.DetectCommunities()
	if len(comms) != 6 {
		t.Errorf("expected 6 community assignments, got %d", len(comms))
	}

	// Betweenness centrality check
	bc := g.BetweennessCentrality()
	if len(bc) != 6 {
		t.Errorf("expected 6 centrality scores, got %d", len(bc))
	}

	// B and E are bridge nodes, so their BC should be higher than leaf nodes like A, C, D, F
	if bc["B"] <= bc["A"] {
		t.Errorf("expected node B (bridge) to have higher centrality than A, got B=%f, A=%f", bc["B"], bc["A"])
	}
	if bc["E"] <= bc["F"] {
		t.Errorf("expected node E (bridge) to have higher centrality than F, got E=%f, F=%f", bc["E"], bc["F"])
	}
}
