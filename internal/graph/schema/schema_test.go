package schema

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestNodeJSONRoundTrip(t *testing.T) {
	original := &Node{
		ID:    "node-1",
		Type:  "function",
		Label: "RunLoop",
		Path:  "internal/app/run.go",
		Metadata: map[string]interface{}{
			"language": "go",
			"loc":      float64(120),
			"line":     float64(42),
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal node: %v", err)
	}

	var decoded Node
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal node: %v", err)
	}

	if !reflect.DeepEqual(original, &decoded) {
		t.Errorf("round-trip mismatch:\noriginal: %+v\ndecoded:  %+v", original, &decoded)
	}
}

func TestNodeJSONOmitsEmptyMetadata(t *testing.T) {
	node := &Node{
		ID:       "node-2",
		Type:     "file",
		Label:    "main.go",
		Path:     "main.go",
		Metadata: nil,
	}

	data, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("failed to marshal node: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal raw json: %v", err)
	}
	if _, ok := raw["metadata"]; ok {
		t.Errorf("expected metadata to be omitted when nil, got: %s", string(data))
	}
}

func TestEdgeJSONRoundTrip(t *testing.T) {
	original := &Edge{
		ID:             "edge-1",
		SourceID:       "node-a",
		TargetID:       "node-b",
		RelationType:   "imports",
		Confidence:     "EXTRACTED",
		SourceLocation: "42",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal edge: %v", err)
	}

	var decoded Edge
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal edge: %v", err)
	}

	if !reflect.DeepEqual(original, &decoded) {
		t.Errorf("round-trip mismatch:\noriginal: %+v\ndecoded:  %+v", original, &decoded)
	}
}

func TestEdgeJSONOmitsEmptySourceLocation(t *testing.T) {
	edge := &Edge{
		ID:           "edge-2",
		SourceID:     "node-a",
		TargetID:     "node-b",
		RelationType: "calls",
		Confidence:   "INFERRED",
	}

	data, err := json.Marshal(edge)
	if err != nil {
		t.Fatalf("failed to marshal edge: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal raw json: %v", err)
	}
	if _, ok := raw["source_location"]; ok {
		t.Errorf("expected source_location to be omitted when empty, got: %s", string(data))
	}
}
