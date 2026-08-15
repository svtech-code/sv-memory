package memory

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/svtech-code/sv-memory/internal/db"
)

func TestRunAgentCLICustomCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("echo is a shell builtin on Windows, not a standalone executable")
	}
	// A custom command: "echo" with the prompt appended as the last argument.
	out, err := runAgentCLI(context.Background(), "echo", `{"relation_type":"none"}`)
	if err != nil {
		t.Fatalf("runAgentCLI error: %v", err)
	}
	if !strings.Contains(out, `relation_type`) {
		t.Fatalf("expected prompt echoed back, got %q", out)
	}
}

func TestParseSemanticVerdict(t *testing.T) {
	cases := []struct {
		name    string
		output  string
		wantRel string
		wantErr bool
	}{
		{"plain json", `{"relation_type":"supersedes","reason":"newer decision"}`, SemanticSupersedes, false},
		{"json in fences", "```json\n{\"relation_type\": \"conflicts_with\", \"reason\": \"contradicts\"}\n```", SemanticConflictsWith, false},
		{"prose wrapper", "Here is the result: {\"relation_type\":\"relates_to\",\"reason\":\"related\"}. Done.", SemanticRelatesTo, false},
		{"none", `{"relation_type":"none","reason":"unrelated"}`, SemanticNone, false},
		{"uppercase type", `{"relation_type":"SUPERSEDES","reason":"x"}`, SemanticSupersedes, false},
		{"invalid type", `{"relation_type":"foo","reason":"x"}`, "", true},
		{"no json", "no json here", "", true},
		{"malformed json", `{"relation_type":"supersedes"`, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rel, _, err := parseSemanticVerdict(c.output)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got relation %q", rel)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if rel != c.wantRel {
				t.Fatalf("expected relation %q, got %q", c.wantRel, rel)
			}
		})
	}
}

func TestSemanticPromptIncludesContent(t *testing.T) {
	src := &Memory{ID: "a", Category: "decision", What: "Use Postgres", Why: "relational", Learned: "sql", WherePath: "db.go"}
	tgt := &Memory{ID: "b", Category: "decision", What: "Use Mongo", Why: "doc store", Learned: "json", WherePath: "store.go"}
	p := semanticPrompt(src, tgt)
	for _, want := range []string{"Use Postgres", "Use Mongo", "a", "b", "relational", "doc store"} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt missing %q:\n%s", want, p)
		}
	}
}

func TestSemanticJudgeCandidatesWithFakeRunner(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "semantic.db"))
	if err != nil {
		t.Fatalf("InitDB error: %v", err)
	}
	defer database.Close()

	const projectID = "semantic-proj"
	if err = db.RegisterProject(database, projectID, "Semantic", tempDir); err != nil {
		t.Fatalf("RegisterProject error: %v", err)
	}

	a, err := SaveMemory(database, &Memory{ProjectID: projectID, Category: "decision", What: "Use Postgres", Why: "relational", Learned: "sql"})
	if err != nil {
		t.Fatalf("save a: %v", err)
	}
	b, err := SaveMemory(database, &Memory{ProjectID: projectID, Category: "decision", What: "Use Mongo", Why: "doc store", Learned: "json"})
	if err != nil {
		t.Fatalf("save b: %v", err)
	}

	candidates := []*MemoryRelation{
		{SourceID: a.ID, TargetID: b.ID, Score: 0.5, SourceWhat: a.What, TargetWhat: b.What},
	}

	original := SemanticRunAgent
	SemanticRunAgent = func(ctx context.Context, agent, prompt string) (string, error) {
		return `{"relation_type":"conflicts_with","reason":"contradicting stores"}`, nil
	}
	defer func() { SemanticRunAgent = original }()

	verdicts, err := SemanticJudgeCandidates(context.Background(), database, projectID, candidates, "fake", 0, 2)
	if err != nil {
		t.Fatalf("SemanticJudgeCandidates error: %v", err)
	}
	if len(verdicts) != 1 {
		t.Fatalf("expected 1 verdict, got %d", len(verdicts))
	}
	if verdicts[0].Relation != SemanticConflictsWith {
		t.Fatalf("expected conflicts_with, got %q", verdicts[0].Relation)
	}
	if verdicts[0].Error != "" {
		t.Fatalf("unexpected error: %s", verdicts[0].Error)
	}
}

func TestSemanticJudgeCandidatesMaxAndErrors(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "semantic_max.db"))
	if err != nil {
		t.Fatalf("InitDB error: %v", err)
	}
	defer database.Close()

	const projectID = "semantic-max-proj"
	if err = db.RegisterProject(database, projectID, "Semantic Max", tempDir); err != nil {
		t.Fatalf("RegisterProject error: %v", err)
	}

	ids := make([]string, 0, 3)
	for _, w := range []string{"One thing", "Two things", "Three things"} {
		var m *Memory
		m, err = SaveMemory(database, &Memory{ProjectID: projectID, Category: "idea", What: w, Why: "w", Learned: "l"})
		if err != nil {
			t.Fatalf("save: %v", err)
		}
		ids = append(ids, m.ID)
	}

	var callsMu sync.Mutex
	calls := 0
	original := SemanticRunAgent
	SemanticRunAgent = func(ctx context.Context, agent, prompt string) (string, error) {
		callsMu.Lock()
		calls++
		callsMu.Unlock()
		return `{"relation_type":"none","reason":"no conflict"}`, nil
	}
	defer func() { SemanticRunAgent = original }()

	candidates := []*MemoryRelation{
		{SourceID: ids[0], TargetID: ids[1]},
		{SourceID: ids[1], TargetID: ids[2]},
		{SourceID: ids[0], TargetID: ids[2]},
	}
	// maxSemantic=2 should limit agent calls to 2.
	verdicts, err := SemanticJudgeCandidates(context.Background(), database, projectID, candidates, "fake", 2, 2)
	if err != nil {
		t.Fatalf("SemanticJudgeCandidates error: %v", err)
	}
	if len(verdicts) != 2 {
		t.Fatalf("expected 2 verdicts, got %d", len(verdicts))
	}
	if calls != 2 {
		t.Fatalf("expected 2 agent calls, got %d", calls)
	}

	// A failing runner surfaces the error on the verdict but returns no error.
	SemanticRunAgent = func(ctx context.Context, agent, prompt string) (string, error) {
		return "", context.DeadlineExceeded
	}
	verdicts, err = SemanticJudgeCandidates(context.Background(), database, projectID, candidates[:1], "fake", 0, 1)
	if err != nil {
		t.Fatalf("expected no top-level error, got %v", err)
	}
	if len(verdicts) != 1 || verdicts[0].Error == "" {
		t.Fatalf("expected error on verdict, got %+v", verdicts)
	}
}

func TestApplySemanticVerdict(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "semantic_apply.db"))
	if err != nil {
		t.Fatalf("InitDB error: %v", err)
	}
	defer database.Close()

	const projectID = "semantic-apply-proj"
	if err := db.RegisterProject(database, projectID, "Semantic Apply", tempDir); err != nil {
		t.Fatalf("RegisterProject error: %v", err)
	}

	a, _ := SaveMemory(database, &Memory{ProjectID: projectID, Category: "decision", What: "A", Why: "x", Learned: "l"})
	b, _ := SaveMemory(database, &Memory{ProjectID: projectID, Category: "decision", What: "B", Why: "y", Learned: "l"})

	// supersedes verdict persists a judged relation with judged_by='llm'.
	if err := ApplySemanticVerdict(database, projectID, &SemanticVerdict{
		SourceID: a.ID, TargetID: b.ID, Relation: SemanticSupersedes, Reason: "newer wins", Score: 0.6,
	}); err != nil {
		t.Fatalf("ApplySemanticVerdict supersedes: %v", err)
	}
	var relType, status, judgedBy string
	if err := database.QueryRow(
		"SELECT relation_type, status, judged_by FROM memory_relations WHERE project_id=? AND source_id=? AND target_id=?",
		projectID, a.ID, b.ID,
	).Scan(&relType, &status, &judgedBy); err != nil {
		t.Fatalf("query verdict: %v", err)
	}
	if relType != SemanticSupersedes || status != "judged" || judgedBy != "llm" {
		t.Fatalf("unexpected verdict row: %s/%s/%s", relType, status, judgedBy)
	}

	// 'none' verdict records an ignored relation for the other pair.
	if err := ApplySemanticVerdict(database, projectID, &SemanticVerdict{
		SourceID: b.ID, TargetID: a.ID, Relation: SemanticNone, Reason: "unrelated",
	}); err != nil {
		t.Fatalf("ApplySemanticVerdict none: %v", err)
	}
	var status2 string
	if err := database.QueryRow(
		"SELECT status FROM memory_relations WHERE project_id=? AND source_id=? AND target_id=?",
		projectID, b.ID, a.ID,
	).Scan(&status2); err != nil {
		t.Fatalf("query ignored verdict: %v", err)
	}
	if status2 != "ignored" {
		t.Fatalf("expected ignored, got %s", status2)
	}

	// Error verdicts are skipped entirely.
	if err := ApplySemanticVerdict(database, projectID, &SemanticVerdict{
		SourceID: a.ID, TargetID: b.ID, Relation: SemanticConflictsWith, Reason: "x", Error: "boom",
	}); err != nil {
		t.Fatalf("ApplySemanticVerdict error verdict should be no-op: %v", err)
	}
	var count int
	_ = database.QueryRow("SELECT COUNT(*) FROM memory_relations WHERE project_id=?", projectID).Scan(&count)
	if count != 2 {
		t.Fatalf("expected 2 relations after no-op skip, got %d", count)
	}
}

func TestSemanticRecallPromptBounded(t *testing.T) {
	items := []recallItem{
		{ID: "a", Category: "decision", What: "w", Why: "y", Learned: "l", Where: "x"},
	}
	p := semanticRecallPrompt("how do we handle auth timeouts", items)
	for _, want := range []string{"how do we handle auth timeouts", "a", "decision", "relevant", "QUERY:"} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt missing %q:\n%s", want, p)
		}
	}
}

func TestParseSemanticRecall(t *testing.T) {
	cases := []struct {
		name    string
		output  string
		wantLen int
		wantErr bool
	}{
		{"plain array", `[{"id":"a","relevant":true,"reason":"matches"}]`, 1, false},
		{"array in fences", "```json\n[{\"id\":\"a\",\"relevant\":true},{\"id\":\"b\",\"relevant\":false}]\n```", 2, false},
		{"prose wrapper", "Here you go: [{\"id\":\"a\",\"relevant\":false}] Done.", 1, false},
		{"no array", "no json here", 0, true},
		{"malformed", `[{"id":"a","relevant":true}`, 0, true},
		{"empty id dropped", `[{"id":"","relevant":true},{"id":"b","relevant":true}]`, 1, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := parseSemanticRecall(c.output)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", out)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(out) != c.wantLen {
				t.Fatalf("expected %d results, got %d", c.wantLen, len(out))
			}
		})
	}
}

func TestSemanticRecallRanksAndCaps(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "recall.db"))
	if err != nil {
		t.Fatalf("InitDB error: %v", err)
	}
	defer database.Close()

	const projectID = "recall-proj"
	if err = db.RegisterProject(database, projectID, "Recall", tempDir); err != nil {
		t.Fatalf("RegisterProject error: %v", err)
	}

	// Four memories; the agent will rank C and B as relevant (C first).
	var ids []string
	for _, w := range []string{"Use Postgres", "Use Mongo", "Auth timeout fix", "Logging config"} {
		var m *Memory
		m, err = SaveMemory(database, &Memory{ProjectID: projectID, Category: "decision", What: w, Why: "why " + w, Learned: "learned " + w})
		if err != nil {
			t.Fatalf("save %q: %v", w, err)
		}
		ids = append(ids, m.ID)
	}

	candidates := make([]*MemorySearchResult, 0, len(ids))
	for _, id := range ids {
		candidates = append(candidates, &MemorySearchResult{ID: id})
	}

	original := SemanticRunAgent
	SemanticRunAgent = func(ctx context.Context, agent, prompt string) (string, error) {
		return `[{"id":"` + ids[2] + `","relevant":true,"reason":"auth relates"},` +
			`{"id":"` + ids[1] + `","relevant":true,"reason":"db choice"},` +
			`{"id":"` + ids[0] + `","relevant":false,"reason":"no"},` +
			`{"id":"` + ids[3] + `","relevant":false,"reason":"no"}]`, nil
	}
	defer func() { SemanticRunAgent = original }()

	// limit=1 must keep only the top relevant candidate (C, ordered first).
	results, reasons, used := SemanticRecall(context.Background(), database, projectID, "auth", candidates, "fake", 1)
	if !used {
		t.Fatal("expected semantic ranking to be used")
	}
	if len(results) != 1 || results[0].ID != ids[2] {
		t.Fatalf("expected top candidate %s only, got %+v", ids[2], results)
	}
	if reasons[ids[2]] != "auth relates" {
		t.Fatalf("expected reason for top candidate, got %q", reasons[ids[2]])
	}

	// No cap truncation: both relevant candidates come back in agent order.
	results, reasons, used = SemanticRecall(context.Background(), database, projectID, "auth", candidates, "fake", 10)
	if !used {
		t.Fatal("expected semantic ranking to be used")
	}
	if len(results) != 2 || results[0].ID != ids[2] || results[1].ID != ids[1] {
		t.Fatalf("expected [C, B] order, got %+v", results)
	}
	if _, ok := reasons[ids[0]]; ok {
		t.Fatalf("irrelevant candidate must not have a reason")
	}
}

func TestSemanticRecallFailOpen(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "recall_fail.db"))
	if err != nil {
		t.Fatalf("InitDB error: %v", err)
	}
	defer database.Close()

	const projectID = "recall-fail-proj"
	if err = db.RegisterProject(database, projectID, "Recall Fail", tempDir); err != nil {
		t.Fatalf("RegisterProject error: %v", err)
	}

	var m *Memory
	m, err = SaveMemory(database, &Memory{ProjectID: projectID, Category: "decision", What: "Use Postgres", Why: "w", Learned: "l"})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	candidates := []*MemorySearchResult{{ID: m.ID}}

	// Agent error → keyword candidates unchanged, no reasons, used=false.
	original := SemanticRunAgent
	SemanticRunAgent = func(ctx context.Context, agent, prompt string) (string, error) {
		return "", context.DeadlineExceeded
	}
	results, reasons, used := SemanticRecall(context.Background(), database, projectID, "auth", candidates, "fake", 5)
	if used {
		t.Fatal("expected fail-open when agent errors")
	}
	if len(results) != 1 || results[0].ID != m.ID {
		t.Fatalf("expected original candidate on fail-open, got %+v", results)
	}
	if reasons != nil {
		t.Fatalf("expected nil reasons on fail-open, got %v", reasons)
	}

	// Invalid JSON → same fail-open behavior.
	SemanticRunAgent = func(ctx context.Context, agent, prompt string) (string, error) {
		return "no json here", nil
	}
	results, reasons, used = SemanticRecall(context.Background(), database, projectID, "auth", candidates, "fake", 5)
	if used {
		t.Fatal("expected fail-open on invalid JSON")
	}
	if len(results) != 1 || results[0].ID != m.ID {
		t.Fatalf("expected original candidate on invalid JSON, got %+v", results)
	}
	if reasons != nil {
		t.Fatalf("expected nil reasons on invalid JSON, got %v", reasons)
	}
	SemanticRunAgent = original
}

func TestSemanticRecallTruncatesFields(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "recall_trunc.db"))
	if err != nil {
		t.Fatalf("InitDB error: %v", err)
	}
	defer database.Close()

	const projectID = "recall-trunc-proj"
	if err = db.RegisterProject(database, projectID, "Recall Trunc", tempDir); err != nil {
		t.Fatalf("RegisterProject error: %v", err)
	}

	longWhy := strings.Repeat("y", 2000)
	var m *Memory
	m, err = SaveMemory(database, &Memory{ProjectID: projectID, Category: "decision", What: "Use Postgres", Why: longWhy, Learned: "l"})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	candidates := []*MemorySearchResult{{ID: m.ID}}

	var gotPrompt string
	original := SemanticRunAgent
	SemanticRunAgent = func(ctx context.Context, agent, prompt string) (string, error) {
		gotPrompt = prompt
		return `[{"id":"` + m.ID + `","relevant":true,"reason":"matches"}]`, nil
	}
	defer func() { SemanticRunAgent = original }()

	if _, _, used := SemanticRecall(context.Background(), database, projectID, "postgres", candidates, "fake", 5); !used {
		t.Fatal("expected semantic ranking to be used")
	}
	if strings.Contains(gotPrompt, longWhy) {
		t.Fatalf("prompt must truncate long fields to stay token-bounded")
	}
	if !strings.Contains(gotPrompt, m.What) {
		t.Fatalf("prompt should contain the candidate's what")
	}
}
