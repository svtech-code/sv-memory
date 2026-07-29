package graph

import (
	"testing"
)

func TestNodeBetweennessCentrality(t *testing.T) {
	t.Run("nil_node", func(t *testing.T) {
		if got := NodeBetweennessCentrality(nil); got != 0.0 {
			t.Errorf("expected 0.0 for nil node, got %f", got)
		}
	})

	t.Run("nil_metadata", func(t *testing.T) {
		n := &Node{ID: "n1"}
		if got := NodeBetweennessCentrality(n); got != 0.0 {
			t.Errorf("expected 0.0 for nil metadata, got %f", got)
		}
	})

	t.Run("missing_key", func(t *testing.T) {
		n := &Node{ID: "n1", Metadata: map[string]interface{}{"foo": "bar"}}
		if got := NodeBetweennessCentrality(n); got != 0.0 {
			t.Errorf("expected 0.0 for missing key, got %f", got)
		}
	})

	t.Run("float64_value", func(t *testing.T) {
		n := &Node{ID: "n1", Metadata: map[string]interface{}{"betweenness_centrality": 42.5}}
		if got := NodeBetweennessCentrality(n); got != 42.5 {
			t.Errorf("expected 42.5, got %f", got)
		}
	})

	t.Run("float32_value", func(t *testing.T) {
		n := &Node{ID: "n1", Metadata: map[string]interface{}{"betweenness_centrality": float32(10.0)}}
		if got := NodeBetweennessCentrality(n); got != 10.0 {
			t.Errorf("expected 10.0, got %f", got)
		}
	})

	t.Run("wrong_type", func(t *testing.T) {
		n := &Node{ID: "n1", Metadata: map[string]interface{}{"betweenness_centrality": "invalid"}}
		if got := NodeBetweennessCentrality(n); got != 0.0 {
			t.Errorf("expected 0.0 for wrong type, got %f", got)
		}
	})

	t.Run("int_value", func(t *testing.T) {
		n := &Node{ID: "n1", Metadata: map[string]interface{}{"betweenness_centrality": 5}}
		if got := NodeBetweennessCentrality(n); got != 0.0 {
			t.Errorf("expected 0.0 for int (not float64/32), got %f", got)
		}
	})
}

func TestNodeCommunityID(t *testing.T) {
	t.Run("nil_node", func(t *testing.T) {
		if got := NodeCommunityID(nil); got != 0 {
			t.Errorf("expected 0 for nil node, got %d", got)
		}
	})

	t.Run("nil_metadata", func(t *testing.T) {
		n := &Node{ID: "n1"}
		if got := NodeCommunityID(n); got != 0 {
			t.Errorf("expected 0 for nil metadata, got %d", got)
		}
	})

	t.Run("missing_key", func(t *testing.T) {
		n := &Node{ID: "n1", Metadata: map[string]interface{}{"foo": "bar"}}
		if got := NodeCommunityID(n); got != 0 {
			t.Errorf("expected 0 for missing key, got %d", got)
		}
	})

	t.Run("float64_value", func(t *testing.T) {
		n := &Node{ID: "n1", Metadata: map[string]interface{}{"community_id": float64(3)}}
		if got := NodeCommunityID(n); got != 3 {
			t.Errorf("expected 3, got %d", got)
		}
	})

	t.Run("float_value_with_fraction", func(t *testing.T) {
		n := &Node{ID: "n1", Metadata: map[string]interface{}{"community_id": float64(7.99)}}
		if got := NodeCommunityID(n); got != 7 {
			t.Errorf("expected 7 (truncated), got %d", got)
		}
	})

	t.Run("int_value", func(t *testing.T) {
		n := &Node{ID: "n1", Metadata: map[string]interface{}{"community_id": 5}}
		if got := NodeCommunityID(n); got != 5 {
			t.Errorf("expected 5, got %d", got)
		}
	})

	t.Run("int64_value", func(t *testing.T) {
		n := &Node{ID: "n1", Metadata: map[string]interface{}{"community_id": int64(42)}}
		if got := NodeCommunityID(n); got != 42 {
			t.Errorf("expected 42, got %d", got)
		}
	})

	t.Run("wrong_type", func(t *testing.T) {
		n := &Node{ID: "n1", Metadata: map[string]interface{}{"community_id": "invalid"}}
		if got := NodeCommunityID(n); got != 0 {
			t.Errorf("expected 0 for wrong type, got %d", got)
		}
	})
}
