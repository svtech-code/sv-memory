package memory

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/svtech/sv-memory/internal/db"
)

var benchCategories = []string{"architecture", "decision", "journal", "bugfix", "discussion"}

func BenchmarkFTSSearch(b *testing.B) {
	for _, memCount := range []int{100, 500, 1000} {
		b.Run(fmt.Sprintf("memories_%d", memCount), func(b *testing.B) {
			tempDir, err := os.MkdirTemp("", "sv-mem-bench-search")
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

			projectID := "bench-search"
			err = db.RegisterProject(database, projectID, "Bench Search", tempDir)
			if err != nil {
				b.Fatalf("failed to register project: %v", err)
			}

			rng := rand.New(rand.NewSource(42))
			topics := []string{"database", "api", "auth", "cache", "deploy", "testing", "logging", "config", "security", "monitoring"}
			words := []string{"fix", "refactor", "migration", "deprecation", "optimization", "decision", "bug", "feature", "upgrade", "rollback"}

			for i := 0; i < memCount; i++ {
				t := topics[rng.Intn(len(topics))]
				w := words[rng.Intn(len(words))]
				_, err := SaveMemory(database, &Memory{
					ID:        fmt.Sprintf("mem-bench-%d", i),
					ProjectID: projectID,
					Category:  benchCategories[rng.Intn(len(benchCategories))],
					What:      fmt.Sprintf("%s %s %d: fix the %s handler to use the new %s pattern", t, w, i, t, w),
					Why:       fmt.Sprintf("The %s module had a %s issue that needed to be addressed with a proper %s approach", t, w, w),
					Learned:   fmt.Sprintf("Always apply the %s pattern when working on %s to avoid %s-related regressions", w, t, t),
					CreatedAt: time.Now(),
				})
				if err != nil {
					b.Fatalf("failed saving memory %d: %v", i, err)
				}
			}

			b.ReportMetric(float64(memCount), "total_memories")

			b.Run("single_word", func(b *testing.B) {
				queries := []string{"database", "api", "deploy", "security"}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					q := queries[i%len(queries)]
					results, err := SearchMemories(database, projectID, q, "", 10)
					if err != nil {
						b.Fatalf("search failed: %v", err)
					}
					_ = results
				}
			})

			b.Run("multi_word", func(b *testing.B) {
				queries := []string{"api auth", "deploy migration", "security fix", "cache optimization"}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					q := queries[i%len(queries)]
					results, err := SearchMemories(database, projectID, q, "", 10)
					if err != nil {
						b.Fatalf("search failed: %v", err)
					}
					_ = results
				}
			})

			b.Run("with_category_filter", func(b *testing.B) {
				queries := []string{"database", "api", "deploy", "security"}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					q := queries[i%len(queries)]
					results, err := SearchMemories(database, projectID, q, "architecture", 10)
					if err != nil {
						b.Fatalf("search failed: %v", err)
					}
					_ = results
				}
			})
		})
	}
}

func BenchmarkSaveMemory(b *testing.B) {
	tempDir, err := os.MkdirTemp("", "sv-mem-bench-save")
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

	projectID := "bench-save"
	err = db.RegisterProject(database, projectID, "Bench Save", tempDir)
	if err != nil {
		b.Fatalf("failed to register project: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := SaveMemory(database, &Memory{
			ID:        fmt.Sprintf("mem-save-bench-%d", i),
			ProjectID: projectID,
			Category:  "decision",
			What:      fmt.Sprintf("Benchmark save iteration %d", i),
			Why:       "Testing save performance",
			Learned:   "Faster saves with WAL mode",
			CreatedAt: time.Now(),
		})
		if err != nil {
			b.Fatalf("save failed at iteration %d: %v", i, err)
		}
	}
}

func BenchmarkProgressiveDisclosure(b *testing.B) {
	for _, memCount := range []int{100, 500} {
		b.Run(fmt.Sprintf("memories_%d", memCount), func(b *testing.B) {
			tempDir, err := os.MkdirTemp("", "sv-mem-bench-progressive")
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

			projectID := "bench-progressive"
			err = db.RegisterProject(database, projectID, "Bench Progressive", tempDir)
			if err != nil {
				b.Fatalf("failed to register project: %v", err)
			}

			rng := rand.New(rand.NewSource(99))
			for i := 0; i < memCount; i++ {
				what := fmt.Sprintf("Architecture decision %d: use module-based %s approach for %s service",
					i, []string{"clean-architecture", "hexagonal", "layered", "event-driven"}[rng.Intn(4)],
					[]string{"payment", "user", "order", "notification", "search"}[rng.Intn(5)])
				why := fmt.Sprintf("We decided on this because the previous monolith approach caused issues with deployment. "+
					"The new pattern separates concerns better, improves testability, and allows independent scaling. "+
					"This was discussed in the architecture review on iteration %d.", i)
				learned := fmt.Sprintf("Always prefer %s for new services. The migration from monolith should be done incrementally. "+
					"Each service must have its own database and API contract. Documentation should be updated accordingly.",
					[]string{"clean-architecture", "hexagonal", "layered", "event-driven"}[rng.Intn(4)])

				_, err := SaveMemory(database, &Memory{
					ID:        fmt.Sprintf("prog-%d", i),
					ProjectID: projectID,
					Category:  "architecture",
					What:      what,
					Why:       why,
					Learned:   learned,
					CreatedAt: time.Now(),
				})
				if err != nil {
					b.Fatalf("failed saving memory %d: %v", i, err)
				}
			}

			b.ReportMetric(float64(memCount), "total_memories")

			b.Run("ProgressiveSearchOnly", func(b *testing.B) {
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					results, err := SearchMemoriesCompact(database, projectID, "architecture", "architecture", 5, 0)
					if err != nil {
						b.Fatalf("compact search failed: %v", err)
					}
					b.SetBytes(int64(len(results)))
				}
			})

			b.Run("ProgressiveSearchThenGet", func(b *testing.B) {
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					results, err := SearchMemoriesCompact(database, projectID, "architecture", "architecture", 5, 0)
					if err != nil {
						b.Fatalf("compact search failed: %v", err)
					}
					for _, r := range results {
						mem, err := GetMemory(database, projectID, r.ID)
						if err != nil {
							b.Fatalf("get memory failed: %v", err)
						}
						_ = mem
					}
				}
			})

			b.Run("RawFullRead", func(b *testing.B) {
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					allMems, err := SearchMemories(database, projectID, "", "", 0)
					if err != nil {
						b.Fatalf("full search failed: %v", err)
					}
					b.SetBytes(int64(len(allMems)))
				}
			})
		})
	}
}
