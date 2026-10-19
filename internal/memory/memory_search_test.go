package memory

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/svtech-code/sv-memory/internal/db"
)

func TestFindSimilarMemories(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-similar-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_storage.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-similar"
	err = db.RegisterProject(database, projectID, "Similar Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Save memories with overlapping and distinct titles
	mems := []*Memory{
		{
			ID:        "m-1",
			ProjectID: projectID,
			Category:  "architecture",
			What:      "Use clean architecture in Golang service",
			Why:       "Decouple concerns",
			Learned:   "Keep domain clean",
			CreatedAt: time.Now(),
		},
		{
			ID:        "m-2",
			ProjectID: projectID,
			Category:  "decision",
			What:      "Use hexagonal architecture in Golang backend",
			Why:       "Ports and adapters",
			Learned:   "Separate domain from infra",
			CreatedAt: time.Now(),
		},
		{
			ID:        "m-3",
			ProjectID: projectID,
			Category:  "journal",
			What:      "Fixed memory leak in connection pool",
			Why:       "Connections not closed",
			Learned:   "Check defer close",
			CreatedAt: time.Now(),
		},
		{
			ID:        "m-4",
			ProjectID: projectID,
			Category:  "standard",
			What:      "Linter configuration for Golang",
			Why:       "Consistent style",
			Learned:   "Run linter on commit",
			CreatedAt: time.Now(),
		},
	}
	for _, m := range mems {
		_, err = SaveMemory(database, m)
		if err != nil {
			t.Fatalf("failed saving memory: %v", err)
		}
	}

	tests := []struct {
		name      string
		title     string
		wantMin   int
		wantMax   int
		threshold float64
	}{
		{
			name:      "similar architecture title",
			title:     "Use clean architecture in Golang service",
			wantMin:   1,
			wantMax:   2,
			threshold: 0.85,
		},
		{
			name:      "similar hexagonal title",
			title:     "Use hexagonal architecture in Golang backend",
			wantMin:   1,
			wantMax:   2,
			threshold: 0.85,
		},
		{
			name:      "unique title no match",
			title:     "Setup CI/CD pipeline with GitHub Actions",
			wantMin:   0,
			wantMax:   0,
			threshold: 0.85,
		},
		{
			name:      "low threshold catches more",
			title:     "Golang architecture clean backend",
			wantMin:   1,
			wantMax:   4,
			threshold: 0.50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidates, err := FindSimilarMemories(database, projectID, tt.title, 5, tt.threshold)
			if err != nil {
				t.Fatalf("FindSimilarMemories failed: %v", err)
			}
			if len(candidates) < tt.wantMin || len(candidates) > tt.wantMax {
				t.Errorf("expected %d-%d candidates, got %d", tt.wantMin, tt.wantMax, len(candidates))
			}
			// Check similarity values are within bounds
			for _, c := range candidates {
				if c.Similarity < 0 || c.Similarity > 1.0 {
					t.Errorf("similarity out of range [0,1]: %f", c.Similarity)
				}
				if c.ID == "" {
					t.Error("candidate ID is empty")
				}
			}
		})
	}
}

func TestTokenizeTitle(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"Use clean architecture in Golang service", []string{"clean", "architecture", "golang", "service"}},
		{"Fixed memory leak in connection pool", []string{"fixed", "memory", "leak", "connection", "pool"}},
		{"a an the", nil},
		{"hello world", []string{"hello", "world"}},
		{"", nil},
	}
	for _, tt := range tests {
		got := tokenizeTitle(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("tokenizeTitle(%q) = %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("tokenizeTitle(%q) = %v, want %v", tt.input, got, tt.want)
				break
			}
		}
	}
}

func TestJaccardSimilarity(t *testing.T) {
	tests := []struct {
		a    []string
		b    []string
		want float64
	}{
		{[]string{"a", "b"}, []string{"a", "b"}, 1.0},
		{[]string{"a", "b"}, []string{"a", "c"}, 1.0 / 3.0},
		{[]string{"a"}, []string{"b"}, 0.0},
		{[]string{}, []string{}, 1.0},
		{[]string{"a", "b", "c"}, []string{"a", "b"}, 2.0 / 3.0},
	}
	for _, tt := range tests {
		got := jaccardSimilarity(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("jaccardSimilarity(%v, %v) = %f, want %f", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestSanitizeFTS5Query(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"normal phrase", "sv-memory protocol", `"sv-memory"* "protocol"*`},
		{"column targeting chars", "memory: protocol", `"memory:"* "protocol"*`},
		{"double quotes", `foo "bar"`, `"foo"* "bar"*`},
		{"AND operator", "foo AND bar", `"foo"* "AND"* "bar"*`},
		{"special chars", `-WAL memory (`, `"-WAL"* "memory"* "("`},
		{"multiple spaces", "foo   bar", `"foo"* "bar"*`},
		{"single char token stays exact", "a protocol", `"a" "protocol"*`},
		{"only quotes produce empty query", `""`, ""},
		{"triple quotes produce empty query", `"""`, ""},
		{"mixed quotes and words", `foo """ bar`, `"foo"* "bar"*`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeFTS5Query(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeFTS5Query(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFTS5WithSpecialChars(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-fts-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "test-fts"
	if regErr := db.RegisterProject(database, projectID, "FTS Test", tempDir); regErr != nil {
		t.Fatalf("failed to register project: %v", regErr)
	}

	_, err = SaveMemory(database, &Memory{
		ID: "fts-1", ProjectID: projectID, Category: "test",
		What: "memory save protocol", Why: "testing save with FTS5",
		Learned: "always test save", CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("failed to save memory: %v", err)
	}

	// Queries that previously caused "no such column" or syntax errors
	hostileQueries := []string{
		"sv-memory: rules",
		"memory: protocol",
		`foo "bar"`,
		"-WAL",
		"AND",
		"memory (",
	}
	for _, q := range hostileQueries {
		t.Run("hostile_"+q, func(t *testing.T) {
			results, err := SearchMemoriesCompact(database, projectID, q, "", 10, 0)
			if err != nil {
				t.Fatalf("hostile query %q returned error: %v", q, err)
			}
			// Should not error — may or may not find results
			_ = results
		})
	}
}

func TestFTS5PrefixMatchingAndScore(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-prefix-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "test-prefix"
	if regErr := db.RegisterProject(database, projectID, "Prefix Test", tempDir); regErr != nil {
		t.Fatalf("failed to register project: %v", regErr)
	}

	_, err = SaveMemory(database, &Memory{
		ID: "pfx-1", ProjectID: projectID, Category: "standard",
		What: "component card pattern", Why: "reusable UI components",
		Learned: "always use the card component", CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("failed to save memory: %v", err)
	}
	_, err = SaveMemory(database, &Memory{
		ID: "pfx-2", ProjectID: projectID, Category: "decision",
		What: "avoid component library", Why: "keep deps light",
		Learned: "no heavy frameworks", CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("failed to save memory: %v", err)
	}

	// Singular query must match the plural "components" in pfx-1 via prefix.
	results, err := SearchMemoriesCompact(database, projectID, "component", "", 10, 0)
	if err != nil {
		t.Fatalf("search 'component': %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected prefix match for 'component' to find 'components'")
	}
	if results[0].ID != "pfx-1" {
		t.Errorf("expected pfx-1 first, got %s", results[0].ID)
	}
	if results[0].Score == 0 {
		t.Error("expected a non-zero BM25 score on FTS results")
	}
	if results[0].Score > 0 {
		t.Errorf("expected negative BM25 score (lower is better), got %v", results[0].Score)
	}
}

func TestTriFactorScoreRanking(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-trifactor-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "test-trifactor"
	if err = db.RegisterProject(database, projectID, "TriFactor Test", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// 1. Save unpinned vs pinned memory with similar search terms
	_, err = SaveMemory(database, &Memory{
		ID: "rank-unpinned", ProjectID: projectID, Category: "journal",
		What: "microservice retry policy for payment processing", Why: "network faults",
		Learned: "exponential backoff", CreatedAt: time.Now().Add(-10 * time.Minute),
		Pinned: false,
	})
	if err != nil {
		t.Fatalf("failed to save unpinned memory: %v", err)
	}

	_, err = SaveMemory(database, &Memory{
		ID: "rank-pinned", ProjectID: projectID, Category: "journal",
		What: "microservice retry policy for invoice processing", Why: "network faults",
		Learned: "exponential backoff", CreatedAt: time.Now().Add(-10 * time.Minute),
		Pinned: true,
	})
	if err != nil {
		t.Fatalf("failed to save pinned memory: %v", err)
	}

	// Pinned memory should rank higher (more negative score)
	results, err := SearchMemoriesCompact(database, projectID, "microservice retry policy", "", 10, 0)
	if err != nil {
		t.Fatalf("search 'microservice retry policy': %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}
	if results[0].ID != "rank-pinned" {
		t.Errorf("expected pinned memory first, got %s", results[0].ID)
	}
	if results[0].Score >= results[1].Score {
		t.Errorf("expected pinned memory score (%f) to be lower/more negative than unpinned (%f)", results[0].Score, results[1].Score)
	}
}

func TestSearchQuotesOnlyDoesNotError(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sv-mem-fts-quotes")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "test-fts-quotes"
	if regErr := db.RegisterProject(database, projectID, "FTS Quotes Test", tempDir); regErr != nil {
		t.Fatalf("failed to register project: %v", regErr)
	}

	_, err = SaveMemory(database, &Memory{
		ProjectID: projectID, Category: "test",
		What: "some content", Why: "why", Learned: "learned",
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("failed to save memory: %v", err)
	}

	// Queries made only of quotes sanitize to "" and must not produce an FTS5
	// syntax error from an empty MATCH expression.
	for _, q := range []string{`""`, `"""`, `""" ""`} {
		compact, err := SearchMemoriesCompact(database, projectID, q, "", 10, 0)
		if err != nil {
			t.Fatalf("SearchMemoriesCompact(%q) errored: %v", q, err)
		}
		if len(compact) != 0 {
			t.Errorf("SearchMemoriesCompact(%q) = %d results, want 0", q, len(compact))
		}
		full, err := SearchMemories(database, projectID, q, "", 10)
		if err != nil {
			t.Fatalf("SearchMemories(%q) errored: %v", q, err)
		}
		if len(full) != 0 {
			t.Errorf("SearchMemories(%q) = %d results, want 0", q, len(full))
		}
	}
}
