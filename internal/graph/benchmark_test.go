package graph

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/svtech/sv-memory/internal/db"
)

func BenchmarkBetweennessCentrality(b *testing.B) {
	tempDir, err := os.MkdirTemp("", "sv-mem-bench-graph")
	if err != nil {
		b.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "bench.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		b.Fatalf("failed to init DB: %v", err)
	}
	defer database.Close()

	projectID := "bench-proj"
	err = db.RegisterProject(database, projectID, "Bench Project", tempDir)
	if err != nil {
		b.Fatalf("failed to register project: %v", err)
	}

	files := map[string]string{
		"main.go":     `package main; import "fmt"; func main() { fmt.Println("hello") }`,
		"utils.go":    `package main; func helper() {}`,
		"api.go":      `package main; import "./utils"; func handler() { helper() }`,
		"db.go":       `package main; import "./utils"; func query() { helper() }`,
		"auth.go":     `package main; import "./db"; func login() { query() }`,
		"routes.go":   `package main; import "./auth"; import "./api"; func setup() { login(); handler() }`,
		"config.go":   `package main; func loadConfig() {}`,
		"logger.go":   `package main; import "fmt"; func log() { fmt.Println("log") }`,
		"metrics.go":  `package main; import "./logger"; func track() { log() }`,
		"middleware.go": `package main; import "./auth"; import "./logger"; func mw() { login(); log() }`,
	}

	for name, content := range files {
		err := os.WriteFile(filepath.Join(tempDir, name), []byte(content), 0644)
		if err != nil {
			b.Fatalf("failed writing %s: %v", name, err)
		}
	}

	err = SyncGraphFull(database, projectID, tempDir)
	if err != nil {
		b.Fatalf("failed to sync graph: %v", err)
	}

	g, err := LoadFullGraph(database, projectID)
	if err != nil {
		b.Fatalf("failed to load graph: %v", err)
	}

	edgeCount := 0
	for _, edges := range g.EdgesBySource {
		edgeCount += len(edges)
	}
	b.ReportMetric(float64(len(g.Nodes)), "nodes")
	b.ReportMetric(float64(edgeCount), "edges")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.BetweennessCentrality()
	}
}

func BenchmarkLeidenCommunityDetection(b *testing.B) {
	tempDir, err := os.MkdirTemp("", "sv-mem-bench-leiden")
	if err != nil {
		b.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "bench.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		b.Fatalf("failed to init DB: %v", err)
	}
	defer database.Close()

	projectID := "bench-proj-leiden"
	err = db.RegisterProject(database, projectID, "Bench Leiden", tempDir)
	if err != nil {
		b.Fatalf("failed to register project: %v", err)
	}

	for i := 0; i < 50; i++ {
		content := fmt.Sprintf(`package main; import "./mod%d"; func fn%d() {}`, i%10, i)
		err := os.WriteFile(filepath.Join(tempDir, fmt.Sprintf("file%d.go", i)), []byte(content), 0644)
		if err != nil {
			b.Fatalf("failed writing file%d.go: %v", i, err)
		}
	}

	err = SyncGraphFull(database, projectID, tempDir)
	if err != nil {
		b.Fatalf("failed to sync graph: %v", err)
	}

	g, err := LoadFullGraph(database, projectID)
	if err != nil {
		b.Fatalf("failed to load graph: %v", err)
	}

	edgeCount := 0
	for _, edges := range g.EdgesBySource {
		edgeCount += len(edges)
	}
	b.ReportMetric(float64(len(g.Nodes)), "nodes")
	b.ReportMetric(float64(edgeCount), "edges")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.LeidenDetectCommunities()
	}
}

func BenchmarkGraphLoadFull(b *testing.B) {
	tempDir, err := os.MkdirTemp("", "sv-mem-bench-load")
	if err != nil {
		b.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "bench.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		b.Fatalf("failed to init DB: %v", err)
	}
	defer database.Close()

	projectID := "bench-proj-load"
	err = db.RegisterProject(database, projectID, "Bench Load", tempDir)
	if err != nil {
		b.Fatalf("failed to register project: %v", err)
	}

	for i := 0; i < 500; i++ {
		content := fmt.Sprintf(`package main; import "./mod%d"; func fn%d() {}`, i%20, i)
		err := os.WriteFile(filepath.Join(tempDir, fmt.Sprintf("f%d.go", i)), []byte(content), 0644)
		if err != nil {
			b.Fatalf("failed writing f%d.go: %v", i, err)
		}
	}

	err = SyncGraphFull(database, projectID, tempDir)
	if err != nil {
		b.Fatalf("failed to sync graph: %v", err)
	}

	b.ReportMetric(500, "files")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := LoadFullGraph(database, projectID)
		if err != nil {
			b.Fatalf("failed to load graph: %v", err)
		}
	}
}

func BenchmarkSyncGraphFull(b *testing.B) {
	tempDir, err := os.MkdirTemp("", "sv-mem-bench-syncfull")
	if err != nil {
		b.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "bench.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		b.Fatalf("failed to init DB: %v", err)
	}
	defer database.Close()

	projectID := "bench-syncfull"
	err = db.RegisterProject(database, projectID, "Bench SyncFull", tempDir)
	if err != nil {
		b.Fatalf("failed to register project: %v", err)
	}

	for i := 0; i < 200; i++ {
		content := fmt.Sprintf(`package main; import "./mod%d"; func fn%d() {}`, i%10, i)
		err := os.WriteFile(filepath.Join(tempDir, fmt.Sprintf("f%d.go", i)), []byte(content), 0644)
		if err != nil {
			b.Fatalf("failed writing f%d.go: %v", i, err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Re-create DB to avoid incremental fallback
		os.Remove(dbPath)
		database, err = db.InitDB(dbPath)
		if err != nil {
			b.Fatalf("failed to init DB: %v", err)
		}
		err = db.RegisterProject(database, projectID, "Bench SyncFull", tempDir)
		if err != nil {
			b.Fatalf("failed to register project: %v", err)
		}
		err = SyncGraphFull(database, projectID, tempDir)
		if err != nil {
			b.Fatalf("failed to sync graph: %v", err)
		}
		database.Close()
	}
}

func BenchmarkSyncGraphIncremental(b *testing.B) {
	tempDir, err := os.MkdirTemp("", "sv-mem-bench-incr")
	if err != nil {
		b.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "bench.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		b.Fatalf("failed to init DB: %v", err)
	}

	projectID := "bench-incr"
	err = db.RegisterProject(database, projectID, "Bench Incr", tempDir)
	if err != nil {
		b.Fatalf("failed to register project: %v", err)
	}

	for i := 0; i < 100; i++ {
		content := fmt.Sprintf(`package main; import "./mod%d"; func fn%d() {}`, i%5, i)
		err := os.WriteFile(filepath.Join(tempDir, fmt.Sprintf("f%d.go", i)), []byte(content), 0644)
		if err != nil {
			b.Fatalf("failed writing f%d.go: %v", i, err)
		}
	}

	// Full build first
	err = SyncGraph(database, projectID, tempDir)
	if err != nil {
		b.Fatalf("failed initial sync: %v", err)
	}
	database.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		database, err = db.InitDB(dbPath)
		if err != nil {
			b.Fatalf("failed to init DB: %v", err)
		}

		// Modify one file to trigger incremental
		content := fmt.Sprintf(`package main; import "./mod%d"; func fn%d() {}`, i%5, 9999+i)
		err = os.WriteFile(filepath.Join(tempDir, "f0.go"), []byte(content), 0644)
		if err != nil {
			b.Fatalf("failed writing f0.go: %v", err)
		}

		err = SyncGraph(database, projectID, tempDir)
		if err != nil {
			b.Fatalf("incremental sync failed: %v", err)
		}

		// Restore f0.go for next iteration
		content = fmt.Sprintf(`package main; import "./mod%d"; func fn0() {}`, i%5)
		err = os.WriteFile(filepath.Join(tempDir, "f0.go"), []byte(content), 0644)
		if err != nil {
			b.Fatalf("failed restoring f0.go: %v", err)
		}

		database.Close()
	}
}

func BenchmarkSyncGraphWithMarkdownDeep(b *testing.B) {
	tempDir, err := os.MkdirTemp("", "sv-mem-bench-mddeep")
	if err != nil {
		b.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "bench.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		b.Fatalf("failed to init DB: %v", err)
	}
	defer database.Close()

	projectID := "bench-mddeep"
	err = db.RegisterProject(database, projectID, "Bench MD Deep", tempDir)
	if err != nil {
		b.Fatalf("failed to register project: %v", err)
	}

	for i := 0; i < 20; i++ {
		md := fmt.Sprintf(`# Document %d
## Section A
Content here.

## Section B with Code
More content.

`+"```"+`python
def func_%d():
    pass
`+"```"+`

### Subsection
Even deeper.
`, i, i)
		err := os.WriteFile(filepath.Join(tempDir, fmt.Sprintf("doc%d.md", i)), []byte(md), 0644)
		if err != nil {
			b.Fatalf("failed writing doc%d.md: %v", i, err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		os.Remove(dbPath)
		database, err = db.InitDB(dbPath)
		if err != nil {
			b.Fatalf("failed to init DB: %v", err)
		}
		err = db.RegisterProject(database, projectID, "Bench MD Deep", tempDir)
		if err != nil {
			b.Fatalf("failed to register project: %v", err)
		}
		err = SyncGraphFull(database, projectID, tempDir)
		if err != nil {
			b.Fatalf("failed to sync graph: %v", err)
		}
		database.Close()
	}
}

func BenchmarkSyncGraphWithSQLSchema(b *testing.B) {
	tempDir, err := os.MkdirTemp("", "sv-mem-bench-sql")
	if err != nil {
		b.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "bench.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		b.Fatalf("failed to init DB: %v", err)
	}
	defer database.Close()

	projectID := "bench-sql"
	err = db.RegisterProject(database, projectID, "Bench SQL", tempDir)
	if err != nil {
		b.Fatalf("failed to register project: %v", err)
	}

	for i := 0; i < 10; i++ {
		sql := fmt.Sprintf(`
CREATE TABLE users_%d (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    email TEXT NOT NULL UNIQUE
);

CREATE TABLE posts_%d (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL,
    body TEXT,
    user_id INTEGER NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users_%d(id)
);

CREATE INDEX idx_posts_%d_user_id ON posts_%d(user_id);

CREATE VIEW active_users_%d AS SELECT * FROM users_%d WHERE active = 1;

CREATE TYPE priority_%d AS ENUM ('low', 'medium', 'high');
`, i, i, i, i, i, i, i, i)
		err := os.WriteFile(filepath.Join(tempDir, fmt.Sprintf("schema_%d.sql", i)), []byte(sql), 0644)
		if err != nil {
			b.Fatalf("failed writing schema_%d.sql: %v", i, err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		os.Remove(dbPath)
		database, err = db.InitDB(dbPath)
		if err != nil {
			b.Fatalf("failed to init DB: %v", err)
		}
		err = db.RegisterProject(database, projectID, "Bench SQL", tempDir)
		if err != nil {
			b.Fatalf("failed to register project: %v", err)
		}
		err = SyncGraphFull(database, projectID, tempDir)
		if err != nil {
			b.Fatalf("failed to sync graph: %v", err)
		}
		database.Close()
	}
}

func BenchmarkDetectCommunities(b *testing.B) {
	tempDir, err := os.MkdirTemp("", "sv-mem-bench-comm")
	if err != nil {
		b.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "bench.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		b.Fatalf("failed to init DB: %v", err)
	}
	defer database.Close()

	projectID := "bench-comm"
	err = db.RegisterProject(database, projectID, "Bench Comm", tempDir)
	if err != nil {
		b.Fatalf("failed to register project: %v", err)
	}

	for i := 0; i < 200; i++ {
		dep := (i + 1) % (i/20*20 + 20)
		if dep == i {
			dep = (i + 1) % 200
		}
		content := fmt.Sprintf(`package main; import "./f%d"; func fn%d() {}`, dep, i)
		err := os.WriteFile(filepath.Join(tempDir, fmt.Sprintf("f%d.go", i)), []byte(content), 0644)
		if err != nil {
			b.Fatalf("failed writing f%d.go: %v", i, err)
		}
	}

	err = SyncGraphFull(database, projectID, tempDir)
	if err != nil {
		b.Fatalf("failed to sync graph: %v", err)
	}

	g, err := LoadFullGraph(database, projectID)
	if err != nil {
		b.Fatalf("failed to load graph: %v", err)
	}

	edgeCount := 0
	for _, edges := range g.EdgesBySource {
		edgeCount += len(edges)
	}
	b.ReportMetric(float64(len(g.Nodes)), "nodes")
	b.ReportMetric(float64(edgeCount), "edges")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.DetectCommunities()
	}
}

// BenchmarkGraphCacheGet measures the end-to-end cache lookup path (in-memory
// hit + mtime validation query) for a graph cached in RAM.
func BenchmarkGraphCacheGet(b *testing.B) {
	tempDir, err := os.MkdirTemp("", "sv-mem-bench-cache")
	if err != nil {
		b.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "bench.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		b.Fatalf("failed to init DB: %v", err)
	}
	defer database.Close()

	projectID := "bench-cache-proj"
	if err := db.RegisterProject(database, projectID, "Cache Bench", tempDir); err != nil {
		b.Fatalf("failed to register project: %v", err)
	}

	// Build a small in-memory graph and cache it with matching file metadata.
	g := &InMemoryGraph{
		Nodes: map[string]*Node{
			"a.go": {ID: "a.go", Type: "file", Label: "a.go"},
			"b.go": {ID: "b.go", Type: "file", Label: "b.go"},
		},
		EdgesBySource: map[string][]*Edge{
			"a.go": {{ID: "a-b", SourceID: "a.go", TargetID: "b.go", RelationType: "imports"}},
		},
		EdgesByTarget: map[string][]*Edge{
			"b.go": {{ID: "a-b", SourceID: "a.go", TargetID: "b.go", RelationType: "imports"}},
		},
		FanIn:  map[string]int{"b.go": 1},
		FanOut: map[string]int{"a.go": 1},
	}
	if _, err := database.Exec("INSERT INTO graph_files_meta (project_id, path, mtime_ms, size) VALUES (?, 'a.go', 1000, 10), (?, 'b.go', 2000, 20)", projectID, projectID); err != nil {
		b.Fatalf("failed inserting graph_files_meta: %v", err)
	}

	cache := NewGraphCache()
	cache.Put(projectID, g, 2, 2000)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := cache.Get(database, projectID); !ok {
			b.Fatal("expected cache hit in benchmark")
		}
	}
}

// BenchmarkGraphQueryBFS measures the BFS dependency query on an in-memory
// graph (the operation served by sv_graph_query after a cache hit).
func BenchmarkGraphQueryBFS(b *testing.B) {
	const nodes = 200
	g := &InMemoryGraph{
		Nodes:          make(map[string]*Node, nodes),
		EdgesBySource:  make(map[string][]*Edge, nodes),
		EdgesByTarget:  make(map[string][]*Edge, nodes),
		FanIn:          make(map[string]int, nodes),
		FanOut:         make(map[string]int, nodes),
	}
	for i := 0; i < nodes; i++ {
		id := fmt.Sprintf("file%d.go", i)
		g.Nodes[id] = &Node{ID: id, Type: "file", Label: id}
		if i > 0 {
			prev := fmt.Sprintf("file%d.go", i-1)
			e := &Edge{ID: prev + "-" + id, SourceID: prev, TargetID: id, RelationType: "imports"}
			g.EdgesBySource[prev] = append(g.EdgesBySource[prev], e)
			g.EdgesByTarget[id] = append(g.EdgesByTarget[id], e)
			g.FanIn[id]++
			g.FanOut[prev]++
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sg := g.Query("file0.go", 5, "imports", "out", 50)
		if sg == nil || len(sg.Nodes) == 0 {
			b.Fatal("expected non-empty subgraph")
		}
	}
}
