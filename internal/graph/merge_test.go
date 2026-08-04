package graph

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInMemoryGraphMergeAndJSON(t *testing.T) {
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

	jsonStr := left.MergeToJSON(right)
	if !strings.Contains(jsonStr, `"nodes"`) || !strings.Contains(jsonStr, `"edges"`) {
		t.Fatalf("MergeToJSON = %s", jsonStr)
	}

	loaded := &InMemoryGraph{
		Nodes:         map[string]*Node{},
		EdgesBySource: map[string][]*Edge{},
		EdgesByTarget: map[string][]*Edge{},
		FanIn:         map[string]int{},
		FanOut:        map[string]int{},
	}
	if err := loaded.MergeFromJSON(jsonStr); err != nil {
		t.Fatalf("MergeFromJSON() error = %v", err)
	}
	if len(loaded.Nodes) != 2 || len(loaded.EdgesBySource["a"]) != 1 {
		t.Errorf("loaded graph = %d nodes/%d edges, want 2/1", len(loaded.Nodes), len(loaded.EdgesBySource["a"]))
	}
	if err := loaded.MergeFromJSON("{"); err == nil {
		t.Error("MergeFromJSON invalid JSON returned nil error")
	}

	var snapshot struct {
		Nodes []Node `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &snapshot); err != nil || len(snapshot.Nodes) != 2 {
		t.Errorf("invalid merge snapshot: err=%v nodes=%d", err, len(snapshot.Nodes))
	}
}
