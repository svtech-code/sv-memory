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
	if err := db.RegisterProject(database, projectID, "Semantic", tempDir); err != nil {
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
	if err := db.RegisterProject(database, projectID, "Semantic Max", tempDir); err != nil {
		t.Fatalf("RegisterProject error: %v", err)
	}

	ids := make([]string, 0, 3)
	for _, w := range []string{"One thing", "Two things", "Three things"} {
		m, err := SaveMemory(database, &Memory{ProjectID: projectID, Category: "idea", What: w, Why: "w", Learned: "l"})
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
