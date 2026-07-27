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

	for i := 0; i < 100; i++ {
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

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := LoadFullGraph(database, projectID)
		if err != nil {
			b.Fatalf("failed to load graph: %v", err)
		}
	}
}
