package graph

import (
	"path/filepath"
	"testing"

	"github.com/svtech-code/sv-memory/internal/db"
)

func TestFindNodeDeterministic(t *testing.T) {
	g := &InMemoryGraph{
		Nodes: map[string]*Node{
			"auth.go":                {ID: "auth.go", Label: "auth.go", Path: "auth.go"},
			"src/auth/middleware.py": {ID: "src/auth/middleware.py", Label: "auth_middleware", Path: "src/auth/middleware.py"},
			"src/auth/handler.py":    {ID: "src/auth/handler.py", Label: "auth_handler", Path: "src/auth/handler.py"},
			"oauth.go":               {ID: "oauth.go", Label: "oauth", Path: "oauth.go"},
			"AuthorizeUser":          {ID: "AuthorizeUser", Label: "AuthorizeUser", Path: "src/auth/service.go"},
		},
	}

	// "auth" is a substring of several IDs; the shortest matching ID must win
	// deterministically (auth.go, not oauth.go or the src/auth files).
	got := g.FindNode("auth")
	if got != "auth.go" {
		t.Errorf("FindNode(\"auth\") = %q, want %q (shortest match)", got, "auth.go")
	}
	for i := 0; i < 20; i++ {
		if again := g.FindNode("auth"); again != got {
			t.Errorf("FindNode(\"auth\") unstable: got %q then %q", got, again)
		}
	}

	// Exact ID wins over substring matches.
	if got := g.FindNode("oauth.go"); got != "oauth.go" {
		t.Errorf("FindNode(\"oauth.go\") = %q, want %q", got, "oauth.go")
	}
	// Exact label wins over substring matches.
	if got := g.FindNode("auth_handler"); got != "src/auth/handler.py" {
		t.Errorf("FindNode(\"auth_handler\") = %q, want %q", got, "src/auth/handler.py")
	}
	// No match.
	if got := g.FindNode("nonexistent_xyz"); got != "" {
		t.Errorf("FindNode(\"nonexistent_xyz\") = %q, want \"\"", got)
	}
}

func TestTopDegreeNodesExcludesPackages(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_hubs.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-hubs"
	if err = db.RegisterProject(database, projectID, "Hubs", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// pkg:requests has the highest fan-in (three importers); the file and
	// function nodes are real project code and must surface above it.
	nodes := []struct{ id, typ string }{
		{"src/core.py", "file"},
		{"src/main.py", "file"},
		{"src/utils.py", "file"},
		{"src/core.py:run", "function"},
		{"pkg:requests", "package"},
	}
	for _, n := range nodes {
		if _, err = database.Exec(
			"INSERT INTO graph_nodes (id, project_id, node_type, label, path, metadata) VALUES (?, ?, ?, ?, ?, '{}')",
			n.id, projectID, n.typ, n.id, n.id); err != nil {
			t.Fatalf("insert node %s: %v", n.id, err)
		}
	}

	edges := [][2]string{
		{"src/core.py", "pkg:requests"},
		{"src/main.py", "pkg:requests"},
		{"src/utils.py", "pkg:requests"},
		{"src/core.py", "src/core.py:run"},
	}
	for _, e := range edges {
		if _, err = database.Exec(
			"INSERT INTO graph_edges (id, project_id, source_id, target_id, relation_type, confidence) VALUES (?, ?, ?, ?, 'imports', 'EXTRACTED')",
			e[0]+"->"+e[1], projectID, e[0], e[1]); err != nil {
			t.Fatalf("insert edge %v: %v", e, err)
		}
	}

	hubs, err := TopDegreeNodes(database, projectID, 3)
	if err != nil {
		t.Fatalf("TopDegreeNodes: %v", err)
	}

	for _, h := range hubs {
		if h.ID == "pkg:requests" {
			t.Errorf("external package pkg:requests must not be listed as a hub, got %+v", hubs)
		}
	}
	found := false
	for _, h := range hubs {
		if h.ID == "src/core.py" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected src/core.py in hubs, got %+v", hubs)
	}
}

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

func TestInMemoryGraphTraversal(t *testing.T) {
	g := &InMemoryGraph{
		Nodes: map[string]*Node{
			"a": {ID: "a", Label: "Alpha", Path: "src/a.go"},
			"b": {ID: "b", Label: "Beta", Path: "src/b.go"},
			"c": {ID: "c", Label: "Gamma", Path: "src/c.go"},
		},
		EdgesBySource: map[string][]*Edge{
			"a": {{ID: "e1", SourceID: "a", TargetID: "b", RelationType: "imports"}},
			"b": {{ID: "e2", SourceID: "b", TargetID: "c", RelationType: "calls"}},
		},
		EdgesByTarget: map[string][]*Edge{
			"b": {{ID: "e1", SourceID: "a", TargetID: "b", RelationType: "imports"}},
			"c": {{ID: "e2", SourceID: "b", TargetID: "c", RelationType: "calls"}},
		},
		FanIn:  map[string]int{"b": 1, "c": 1},
		FanOut: map[string]int{"a": 1, "b": 1},
	}

	if got := g.FindNode("b"); got != "b" {
		t.Errorf("FindNode exact match = %q, want b", got)
	}
	if got := g.FindNode("src/c.go"); got != "c" {
		t.Errorf("FindNode path match = %q, want c", got)
	}
	if got := g.FindNode("Gamm"); got != "c" {
		t.Errorf("FindNode fuzzy match = %q, want c", got)
	}
	if got := g.FindNode("missing"); got != "" {
		t.Errorf("FindNode missing = %q, want empty", got)
	}

	if got := g.ShortestPath("a", "c", 2); len(got) != 3 {
		t.Fatalf("ShortestPath = %v, want a->b->c", got)
	}
	if got := g.ShortestPath("a", "c", 1); got != nil {
		t.Errorf("ShortestPath over hop limit = %v, want nil", got)
	}
	if got := g.ShortestPath("missing", "c", 2); got != nil {
		t.Errorf("ShortestPath with missing start = %v, want nil", got)
	}

	if got := g.Query("b", 1, "", "out", 0); len(got.Nodes) != 2 || len(got.Edges) != 1 {
		t.Errorf("out query = %d nodes/%d edges, want 2/1", len(got.Nodes), len(got.Edges))
	}
	if got := g.Query("b", 1, "imports", "in", 0); len(got.Nodes) != 2 || len(got.Edges) != 1 {
		t.Errorf("filtered in query = %d nodes/%d edges, want 2/1", len(got.Nodes), len(got.Edges))
	}
	if got := g.Query("b", 1, "", "all", 0); len(got.Nodes) != 3 || len(got.Edges) != 2 {
		t.Errorf("all query = %d nodes/%d edges, want 3/2", len(got.Nodes), len(got.Edges))
	}
	if got := g.Query("unknown", 1, "", "out", 0); len(got.Nodes) != 0 {
		t.Errorf("unknown query = %d nodes, want 0", len(got.Nodes))
	}
}

func TestComputeHubThreshold(t *testing.T) {
	if got := (&InMemoryGraph{}).ComputeHubThreshold(); got != 50 {
		t.Errorf("empty graph threshold = %d, want 50", got)
	}

	g := &InMemoryGraph{
		Nodes:        map[string]*Node{"a": {}, "b": {}},
		FanIn:        map[string]int{"a": 2},
		FanOut:       map[string]int{"a": 3},
		HubThreshold: 7,
	}
	if got := g.ComputeHubThreshold(); got != 7 {
		t.Errorf("configured threshold = %d, want 7", got)
	}

	g.HubThreshold = 0
	if got := g.ComputeHubThreshold(); got != 50 {
		t.Errorf("minimum threshold = %d, want 50", got)
	}
}

func TestLoadFullGraph(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("db.InitDB() error = %v", err)
	}
	defer database.Close()

	const projectID = "load-graph-project"
	if regErr := db.RegisterProject(database, projectID, "Load Graph", t.TempDir()); regErr != nil {
		t.Fatalf("RegisterProject() error = %v", regErr)
	}
	_, err = database.Exec(`INSERT INTO graph_nodes (project_id, id, node_type, label, path, metadata)
		VALUES (?, 'a', 'file', 'a.go', 'a.go', ?), (?, 'b', 'file', 'b.go', 'b.go', ?)
	`, projectID, `{"language":"go"}`, projectID, `{}`)
	if err != nil {
		t.Fatalf("insert graph nodes error = %v", err)
	}
	_, err = database.Exec(`INSERT INTO graph_edges (id, project_id, source_id, target_id, relation_type, confidence, source_location)
		VALUES ('edge-a-b', ?, 'a', 'b', 'imports', 'EXTRACTED', 'L1')`, projectID)
	if err != nil {
		t.Fatalf("insert graph edge error = %v", err)
	}

	loaded, err := LoadFullGraph(database, projectID)
	if err != nil {
		t.Fatalf("LoadFullGraph() error = %v", err)
	}
	if len(loaded.Nodes) != 2 || len(loaded.EdgesBySource["a"]) != 1 || loaded.FanOut["a"] != 1 || loaded.FanIn["b"] != 1 {
		t.Errorf("loaded graph = nodes:%d edges:%d fanout:%d fanin:%d", len(loaded.Nodes), len(loaded.EdgesBySource["a"]), loaded.FanOut["a"], loaded.FanIn["b"])
	}
	if loaded.Nodes["a"].Metadata["language"] != "go" {
		t.Errorf("loaded metadata = %#v, want language=go", loaded.Nodes["a"].Metadata)
	}
}
