package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"

	"github.com/svtech-code/sv-memory/internal/config"
	"github.com/svtech-code/sv-memory/internal/db"
	"github.com/svtech-code/sv-memory/internal/memory"
)

// seedResponseBenchMemories inserts count memories whose Why/Learned fields are
// long enough to exercise the response truncation paths in sv_mem_get/search.
func seedResponseBenchMemories(tb testing.TB, pool *db.Pool, projectID string, count int) []string {
	tb.Helper()
	long := strings.Repeat("This is a deliberately long rationale used to exercise the token-truncation path in sv-memory tool responses. ", 20)
	var ids []string
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("bench-resp-%d", i)
		m := &memory.Memory{
			ID:        id,
			ProjectID: projectID,
			Category:  []string{"architecture", "decision", "bugfix"}[i%3],
			What:      fmt.Sprintf("Architecture decision %d about service boundaries and module layout", i),
			Why:       fmt.Sprintf("%s (iteration %d)", long, i),
			Learned:   fmt.Sprintf("%s (lesson %d)", long, i),
			WherePath: "/src/internal/service_" + fmt.Sprint(i) + ".go",
			CreatedAt: time.Now(),
		}
		if _, err := memory.SaveMemory(pool.Writer, m); err != nil {
			tb.Fatalf("failed seeding memory %d: %v", i, err)
		}
		ids = append(ids, id)
	}
	return ids
}

func BenchmarkToolResponseTokens(b *testing.B) {
	tempDir, err := os.MkdirTemp("", "sv-mcp-bench-resp")
	if err != nil {
		b.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "bench.db")
	pool, err := db.NewDBPool(dbPath)
	if err != nil {
		b.Fatalf("failed to create DBPool: %v", err)
	}
	defer pool.Close()

	cfg := &config.Config{
		DBPath:    dbPath,
		ProjectID: "bench-resp-proj",
		ProjName:  "Bench Response",
		ProjPath:  tempDir,
	}
	if err := db.RegisterProject(pool.Writer, cfg.ProjectID, cfg.ProjName, cfg.ProjPath); err != nil {
		b.Fatalf("failed to register project: %v", err)
	}

	ids := seedResponseBenchMemories(b, pool, cfg.ProjectID, 50)
	srv := NewServer(pool, cfg)
	ctx := context.Background()

	call := func(name string, args map[string]any) string {
		req := mcpgo.CallToolRequest{}
		req.Params.Name = name
		req.Params.Arguments = args
		res, err := srv.GetTool(name).Handler(ctx, req)
		if err != nil {
			b.Fatalf("%s failed: %v", name, err)
		}
		var out strings.Builder
		for _, c := range res.Content {
			out.WriteString(textContent(c))
		}
		return out.String()
	}

	b.Run("Search_default", func(b *testing.B) {
		b.ReportAllocs()
		var total int
		for i := 0; i < b.N; i++ {
			out := call("sv_mem_search", map[string]any{"query": "architecture decision", "limit": "10"})
			total += len(out)
		}
		b.SetBytes(int64(total / b.N))
		b.ReportMetric(float64(total/b.N)/4, "est_tokens")
	})

	b.Run("Get_default_maxchars", func(b *testing.B) {
		b.ReportAllocs()
		var total int
		for i := 0; i < b.N; i++ {
			out := call("sv_mem_get", map[string]any{"id": ids[i%len(ids)]})
			total += len(out)
		}
		b.SetBytes(int64(total / b.N))
		b.ReportMetric(float64(total/b.N)/4, "est_tokens")
	})

	b.Run("Get_explicit_maxchars_300", func(b *testing.B) {
		b.ReportAllocs()
		var total int
		for i := 0; i < b.N; i++ {
			out := call("sv_mem_get", map[string]any{"id": ids[i%len(ids)], "max_chars": "300"})
			total += len(out)
		}
		b.SetBytes(int64(total / b.N))
		b.ReportMetric(float64(total/b.N)/4, "est_tokens")
	})

	b.Run("Timeline_default", func(b *testing.B) {
		b.ReportAllocs()
		var total int
		for i := 0; i < b.N; i++ {
			out := call("sv_mem_timeline", map[string]any{"observation_id": ids[i%len(ids)], "before": "3", "after": "3"})
			total += len(out)
		}
		b.SetBytes(int64(total / b.N))
		b.ReportMetric(float64(total/b.N)/4, "est_tokens")
	})

	b.Run("Context_default", func(b *testing.B) {
		b.ReportAllocs()
		var total int
		for i := 0; i < b.N; i++ {
			out := call("sv_mem_context", map[string]any{})
			total += len(out)
		}
		b.SetBytes(int64(total / b.N))
		b.ReportMetric(float64(total/b.N)/4, "est_tokens")
	})
}
