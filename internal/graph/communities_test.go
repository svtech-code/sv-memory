package graph

import (
	"testing"
)

func TestDetectCommunities(t *testing.T) {
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

	t.Run("DetectCommunities", func(t *testing.T) {
		comms := g.DetectCommunities()
		if len(comms) != 6 {
			t.Errorf("expected 6 community assignments, got %d", len(comms))
		}
	})

	t.Run("LeidenDetectCommunities", func(t *testing.T) {
		comms := g.LeidenDetectCommunities()
		if len(comms) != 6 {
			t.Errorf("expected 6 community assignments, got %d", len(comms))
		}
		// Leiden should group A-B-C together and D-E-F together
		if comms["A"] != comms["B"] || comms["B"] != comms["C"] {
			t.Errorf("expected A-B-C in same community with Leiden, got A=%d B=%d C=%d", comms["A"], comms["B"], comms["C"])
		}
		if comms["D"] != comms["E"] || comms["E"] != comms["F"] {
			t.Errorf("expected D-E-F in same community with Leiden, got D=%d E=%d F=%d", comms["D"], comms["E"], comms["F"])
		}
		// A and D should be in different communities (bridge only through B-E)
		if comms["A"] == comms["D"] {
			t.Errorf("expected A and D in different communities with Leiden")
		}
	})

	t.Run("BetweennessCentrality", func(t *testing.T) {
		bc := g.BetweennessCentrality()
		if len(bc) != 6 {
			t.Errorf("expected 6 centrality scores, got %d", len(bc))
		}

		if bc["B"] <= bc["A"] {
			t.Errorf("expected node B (bridge) to have higher centrality than A, got B=%f, A=%f", bc["B"], bc["A"])
		}
		if bc["E"] <= bc["F"] {
			t.Errorf("expected node E (bridge) to have higher centrality than F, got E=%f, F=%f", bc["E"], bc["F"])
		}
	})
}
