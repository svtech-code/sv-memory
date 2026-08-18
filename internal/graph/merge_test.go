package graph

import (
	"testing"
)

func TestInMemoryGraphMerge(t *testing.T) {
	left := &InMemoryGraph{
		Nodes: map[string]*Node{
			"a": {ID: "a", Type: "file", Metadata: map[string]interface{}{"left": true}},
		},
		EdgesBySource: map[string][]*Edge{
			"a": {{ID: "e1", SourceID: "a", TargetID: "b", RelationType: "imports"}},
		},
		EdgesByTarget: map[string][]*Edge{
			"b": {{ID: "e1", SourceID: "a", TargetID: "b", RelationType: "imports"}},
		},
		FanOut: map[string]int{"a": 1},
		FanIn:  map[string]int{"b": 1},
	}
	right := &InMemoryGraph{
		Nodes: map[string]*Node{
			"a": {ID: "a", Type: "file", Metadata: map[string]interface{}{"right": true}},
			"b": {ID: "b", Type: "file"},
		},
		EdgesBySource: map[string][]*Edge{
			"a": {{ID: "e2", SourceID: "a", TargetID: "b", RelationType: "imports"}},
		},
	}

	merged := left.Merge(right)
	if len(merged.Nodes) != 2 || len(merged.EdgesBySource["a"]) != 1 {
		t.Fatalf("merged graph = %d nodes/%d edges, want 2/1", len(merged.Nodes), len(merged.EdgesBySource["a"]))
	}
	if !merged.Nodes["a"].Metadata["left"].(bool) || !merged.Nodes["a"].Metadata["right"].(bool) {
		t.Error("merge did not preserve metadata from both graphs")
	}
}
