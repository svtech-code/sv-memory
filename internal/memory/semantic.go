package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/svtech-code/sv-memory/internal/security"
)

// Semantic relation types returned by the LLM judge.
const (
	SemanticSupersedes    = "supersedes"
	SemanticConflictsWith = "conflicts_with"
	SemanticRelatesTo     = "relates_to"
	SemanticNone          = "none"
)

// SemanticVerdict is the outcome of an LLM judgment over one candidate pair.
type SemanticVerdict struct {
	SourceID string  `json:"source_id"`
	TargetID string  `json:"target_id"`
	Relation string  `json:"relation_type"` // supersedes | conflicts_with | relates_to | none
	Reason   string  `json:"reason"`
	Score    float64 `json:"score,omitempty"`
	Error    string  `json:"error,omitempty"`
}

// semanticJudgeTimeout bounds a single agent call so a hung CLI cannot block a
// scan forever.
const semanticJudgeTimeout = 120 * time.Second

// SemanticRunAgent is the injectable runner that executes the agent CLI. The
// default shells out to the configured agent binary; tests replace it with a
// stub so no real agent is invoked.
var SemanticRunAgent = runAgentCLI

// semanticPrompt builds the strict-JSON prompt shown to the agent for a pair.
// It asks for the full content comparison (not just titles) and a single JSON
// object so the output is machine-parseable.
func semanticPrompt(src, tgt *Memory) string {
	return fmt.Sprintf(`You are a memory-hygiene assistant for a coding-agent memory system.

Decide how the two project memories below relate to each other. Compare their CONTENT (what, why, learned, where_path), not just their titles.

Memory A (id: %s):
- Category: %s
- What: %s
- Why: %s
- Learned: %s
- Where: %s

Memory B (id: %s):
- Category: %s
- What: %s
- Why: %s
- Learned: %s
- Where: %s

Respond with ONLY a single JSON object, no prose, in this exact form:
{"relation_type": "supersedes|conflicts_with|relates_to|none", "reason": "one short sentence"}

Rules:
- "supersedes": A makes B obsolete (e.g. a newer decision replaces an older one). Memory A is the newer/source memory.
- "conflicts_with": A and B contradict each other on the same topic.
- "relates_to": A and B are related but neither supersedes nor conflicts.
- "none": they are unrelated or too different.`,
		src.ID, src.Category, src.What, src.Why, src.Learned, src.WherePath,
		tgt.ID, tgt.Category, tgt.What, tgt.Why, tgt.Learned, tgt.WherePath)
}

// runAgentCLI invokes the agent's CLI for a headless run and returns stdout.
// "claude" runs `claude -p <prompt>` (print mode); "opencode" runs
// `opencode run <prompt>`. Any other value is treated as a custom command line
// with the prompt appended as the last argument.
func runAgentCLI(ctx context.Context, agent, prompt string) (string, error) {
	var name string
	var args []string
	switch strings.ToLower(strings.TrimSpace(agent)) {
	case "claude", "claude-code", "claude_code":
		name = "claude"
		args = []string{"-p", prompt}
	case "opencode", "opencode-cli", "opencode_cli":
		name = "opencode"
		args = []string{"run", prompt}
	default:
		parts := strings.Fields(agent)
		if len(parts) == 0 {
			return "", fmt.Errorf("empty semantic agent command")
		}
		name = parts[0]
		args = append(parts[1:], prompt)
	}

	ctx, cancel := context.WithTimeout(ctx, semanticJudgeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("agent %s failed: %v (stderr: %s)", name, err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// parseSemanticVerdict extracts the strict JSON object from the agent output
// (tolerating prose or markdown fences around it) and validates the relation.
func parseSemanticVerdict(output string) (relation, reason string, err error) {
	start := strings.Index(output, "{")
	end := strings.LastIndex(output, "}")
	if start < 0 || end <= start {
		return "", "", fmt.Errorf("no JSON object found in agent response")
	}
	var v struct {
		RelationType string `json:"relation_type"`
		Reason       string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(output[start:end+1]), &v); err != nil {
		return "", "", fmt.Errorf("failed to parse agent JSON: %w", err)
	}
	relation = strings.ToLower(strings.TrimSpace(v.RelationType))
	reason = strings.TrimSpace(v.Reason)
	valid := map[string]bool{
		SemanticSupersedes: true, SemanticConflictsWith: true,
		SemanticRelatesTo: true, SemanticNone: true,
	}
	if !valid[relation] {
		return "", "", fmt.Errorf("agent returned invalid relation_type %q", relation)
	}
	return relation, reason, nil
}

// SemanticJudgeCandidates runs the configured agent over up to maxSemantic
// candidate pairs with concurrency workers. It returns verdicts without
// persisting anything; callers apply them with ApplySemanticVerdict. Pairs
// whose agent call fails carry a non-empty Error and are left untouched so a
// later run can retry them.
func SemanticJudgeCandidates(ctx context.Context, db *sql.DB, projectID string, candidates []*MemoryRelation, agent string, maxSemantic, concurrency int) ([]*SemanticVerdict, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	if maxSemantic <= 0 || maxSemantic > len(candidates) {
		maxSemantic = len(candidates)
	}
	if concurrency <= 0 {
		concurrency = 3
	}

	verdicts := make([]*SemanticVerdict, maxSemantic)
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i := 0; i < maxSemantic; i++ {
		c := candidates[i]
		src, _ := GetMemory(db, projectID, c.SourceID)
		tgt, _ := GetMemory(db, projectID, c.TargetID)
		if src == nil || tgt == nil {
			verdicts[i] = &SemanticVerdict{
				SourceID: c.SourceID, TargetID: c.TargetID,
				Relation: SemanticNone,
				Reason:   "one or both memories not found",
			}
			continue
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, c *MemoryRelation, src, tgt *Memory) {
			defer wg.Done()
			defer func() { <-sem }()
			v := &SemanticVerdict{SourceID: src.ID, TargetID: tgt.ID, Score: c.Score}
			out, err := SemanticRunAgent(ctx, agent, semanticPrompt(src, tgt))
			if err != nil {
				v.Error = err.Error()
				verdicts[idx] = v
				return
			}
			rel, reason, err := parseSemanticVerdict(out)
			if err != nil {
				v.Error = err.Error()
				verdicts[idx] = v
				return
			}
			v.Relation = rel
			v.Reason = reason
			verdicts[idx] = v
		}(i, c, src, tgt)
	}
	wg.Wait()
	return verdicts, nil
}

// ApplySemanticVerdict persists a semantic verdict for a candidate pair. A
// 'none' verdict records the pair as ignored; any other verdict records a
// judged relation of that type (judged_by='llm'). The still-pending candidate
// between the pair is replaced, so re-judging does not accumulate duplicates.
// Verdicts carrying an Error are skipped (the pair stays pending for retry).
func ApplySemanticVerdict(db *sql.DB, projectID string, v *SemanticVerdict) error {
	if v == nil || v.Error != "" {
		return nil
	}
	reason := security.SanitizeText(v.Reason)
	if _, err := db.Exec(
		"DELETE FROM memory_relations WHERE project_id=? AND source_id=? AND target_id=? AND relation_type='conflicts_with' AND status='pending'",
		projectID, v.SourceID, v.TargetID,
	); err != nil {
		return fmt.Errorf("failed to clear pending candidate: %w", err)
	}

	now := time.Now()
	if v.Relation == SemanticNone {
		_, err := db.Exec(`
			INSERT INTO memory_relations (id, project_id, source_id, target_id, relation_type, status, score, reason, judged_by, created_at)
			VALUES (?, ?, ?, ?, 'conflicts_with', 'ignored', ?, ?, 'llm', ?)`,
			newID(), projectID, v.SourceID, v.TargetID, v.Score, reason, now)
		if err != nil {
			return fmt.Errorf("failed to record ignored verdict: %w", err)
		}
		return nil
	}

	_, err := db.Exec(`
		INSERT INTO memory_relations (id, project_id, source_id, target_id, relation_type, status, score, reason, judged_by, created_at)
		VALUES (?, ?, ?, ?, ?, 'judged', ?, ?, 'llm', ?)`,
		newID(), projectID, v.SourceID, v.TargetID, v.Relation, v.Score, reason, now)
	if err != nil {
		return fmt.Errorf("failed to record semantic verdict: %w", err)
	}
	return nil
}
