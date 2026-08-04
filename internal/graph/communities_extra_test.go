package graph

import "testing"

func TestCommunityLabelsInfoAndSurprisingConnections(t *testing.T) {
	g := &InMemoryGraph{
		Nodes: map[string]*Node{
			"a": {ID: "a", Type: "file", Label: "very-long-file-label.go", Metadata: map[string]interface{}{"community_id": float64(1)}},
			"b": {ID: "b", Type: "function", Label: "function b\nextra details", Metadata: map[string]interface{}{"community_id": int64(2)}},
			"c": {ID: "c", Type: "file", Label: "c.go", Metadata: map[string]interface{}{"community_id": 2}},
			"d": {ID: "d", Type: "file", Label: "d.go", Metadata: map[string]interface{}{}},
		},
		EdgesBySource: map[string][]*Edge{
			"a": {{SourceID: "a", TargetID: "b", RelationType: "imports"}},
			"b": {{SourceID: "b", TargetID: "c", RelationType: "calls"}},
		},
		EdgesByTarget: map[string][]*Edge{
			"b": {{SourceID: "a", TargetID: "b", RelationType: "imports"}},
			"c": {{SourceID: "b", TargetID: "c", RelationType: "calls"}},
		},
		FanIn:  map[string]int{"b": 1, "c": 1},
		FanOut: map[string]int{"a": 1, "b": 1},
	}

	communities := g.ExtractCommunities()
	if len(communities) != 3 || communities["a"] != 1 || communities["b"] != 2 || communities["c"] != 2 {
		t.Errorf("ExtractCommunities() = %#v", communities)
	}

	labels := g.DetectCommunityLabels(communities, map[string]float64{"a": 1, "b": 1, "c": 2})
	if labels[1] != "very-long-file-label.go" || labels[2] != "c.go" {
		t.Errorf("DetectCommunityLabels() = %#v", labels)
	}

	info := g.GetCommunityInfo(communities, map[string]float64{"a": 1, "b": 1, "c": 2})
	if info[2].TopNodeID != "c" || info[2].NodeCount != 2 || info[2].AvgCentrality != 1.5 {
		t.Errorf("GetCommunityInfo(2) = %#v", info[2])
	}

	connections := g.FindSurprisingConnections(communities, map[string]float64{"a": 2, "b": 1, "c": 2}, 1)
	if len(connections) != 1 || connections[0].SourceID != "a" || connections[0].TargetID != "b" {
		t.Errorf("FindSurprisingConnections() = %#v", connections)
	}
	if got := g.FindSurprisingConnections(communities, nil, 0); len(got) != 0 {
		t.Errorf("same/zero centrality connections = %#v, want none", got)
	}
}
