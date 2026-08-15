package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
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

// ResolveSemanticAgent returns the agent CLI to use for semantic judging: the
// explicitly configured value wins, then $SV_MEMORY_SEMANTIC_AGENT, then the
// "claude" default.
func ResolveSemanticAgent(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if env := os.Getenv("SV_MEMORY_SEMANTIC_AGENT"); env != "" {
		return env
	}
	return "claude"
}

// SemanticConflictScanResult summarizes a semantic judging pass.
type SemanticConflictScanResult struct {
	Verdicts []*SemanticVerdict
	Judged   int
	Ignored  int
	Failed   int
}

// JudgeConflictCandidates runs the semantic-judging workflow shared by the CLI
// (`conflicts scan --semantic`) and the MCP tool (`sv_mem_conflicts action=scan
// semantic=true`): falls back to still-pending candidates when none were just
// surfaced, runs the agent judgments, and persists them when apply is true.
// Callers format the returned verdicts; an empty Verdicts slice means there was
// nothing to judge.
func JudgeConflictCandidates(ctx context.Context, db *sql.DB, projectID string, candidates []*MemoryRelation, agent string, maxSemantic, concurrency int, apply bool) (*SemanticConflictScanResult, error) {
	if len(candidates) == 0 {
		pending, err := ListConflicts(db, projectID, "pending")
		if err != nil {
			return nil, err
		}
		candidates = pending
	}
	verdicts, err := SemanticJudgeCandidates(ctx, db, projectID, candidates, agent, maxSemantic, concurrency)
	if err != nil {
		return nil, err
	}
	res := &SemanticConflictScanResult{Verdicts: verdicts}
	for _, v := range verdicts {
		if v.Error != "" {
			res.Failed++
			continue
		}
		if v.Relation == SemanticNone {
			res.Ignored++
		} else {
			res.Judged++
		}
		if apply {
			if err := ApplySemanticVerdict(db, projectID, v); err != nil {
				return nil, err
			}
		}
	}
	return res, nil
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

// SemanticRecallResult is the agent's relevance judgment for one candidate.
type SemanticRecallResult struct {
	ID       string `json:"id"`
	Relevant bool   `json:"relevant"`
	Reason   string `json:"reason"`
}

// recallItem is the truncated view of a candidate memory fed to the agent, so
// the prompt stays token-bounded.
type recallItem struct {
	ID       string
	Category string
	What     string
	Why      string
	Learned  string
	Where    string
}

// SemanticRecallMaxCandidates caps how many keyword candidates are sent to the
// agent in a single call. FTS5 already pre-filters the corpus; sending the
// whole result set would blow the prompt budget.
const SemanticRecallMaxCandidates = 30

// semanticRecallFieldChars caps each free-text field per candidate in the prompt.
const semanticRecallFieldChars = 300

// semanticRecallReasonChars caps the stored relevance reason shown to the agent.
const semanticRecallReasonChars = 120

// semanticRecallPrompt builds the strict-JSON ranking prompt. The agent must
// return an entry for every candidate id, flagged relevant or not and ordered
// most-to-least relevant.
func semanticRecallPrompt(query string, items []recallItem) string {
	var sb strings.Builder
	sb.WriteString("You are a memory-recall assistant for a coding-agent memory system.\n\n")
	sb.WriteString("Given the user's query, decide which of the candidate project memories are RELEVANT to it. Compare MEANING, not just keywords: a memory that answers the query in different words still counts as relevant.\n\n")
	fmt.Fprintf(&sb, "QUERY: %s\n\nCANDIDATES:\n", query)
	for i, it := range items {
		fmt.Fprintf(&sb, "- [%d] id=%s category=%s what=%s why=%s learned=%s where=%s\n",
			i, it.ID, it.Category, it.What, it.Why, it.Learned, it.Where)
	}
	sb.WriteString("\nRespond with ONLY a JSON array, no prose, each element in this exact form:\n")
	sb.WriteString(`[{"id": "<candidate id>", "relevant": true|false, "reason": "one short clause"}]`)
	sb.WriteString("\n\nRules:\n")
	sb.WriteString("- Include an entry for EVERY candidate id above (use the exact id string).\n")
	sb.WriteString("- \"relevant\": true only when the memory meaningfully relates to the query.\n")
	sb.WriteString("- Order the array from most to least relevant (most relevant first).\n")
	sb.WriteString("- Keep \"reason\" to one short clause (<= 20 words).\n")
	return sb.String()
}

// parseSemanticRecall extracts the strict JSON array from the agent output
// (tolerating prose or markdown fences around it) and drops entries with empty
// ids.
func parseSemanticRecall(output string) ([]SemanticRecallResult, error) {
	start := strings.Index(output, "[")
	end := strings.LastIndex(output, "]")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON array found in agent response")
	}
	var out []SemanticRecallResult
	if err := json.Unmarshal([]byte(output[start:end+1]), &out); err != nil {
		return nil, fmt.Errorf("failed to parse agent JSON array: %w", err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty JSON array in agent response")
	}
	valid := make([]SemanticRecallResult, 0, len(out))
	for _, r := range out {
		if strings.TrimSpace(r.ID) != "" {
			valid = append(valid, r)
		}
	}
	return valid, nil
}

// SemanticRecall ranks keyword search candidates by semantic relevance using
// the configured agent CLI in a single batched call. It returns the relevant
// subset (in the agent's ordering, capped at limit), a map of id -> relevance
// reason, and whether the LLM ranking was applied. It is fail-open: when the
// agent is unavailable or its response cannot be parsed, the original
// candidates are returned unchanged with an empty reason map and used=false, so
// a keyword search never degrades because of the optional LLM step.
func SemanticRecall(ctx context.Context, db *sql.DB, projectID, query string, candidates []*MemorySearchResult, agent string, limit int) ([]*MemorySearchResult, map[string]string, bool) {
	if len(candidates) == 0 {
		return candidates, nil, false
	}
	if limit <= 0 {
		limit = 5
	}

	items := make([]recallItem, 0, len(candidates))
	byID := make(map[string]*MemorySearchResult, len(candidates))
	for _, c := range candidates {
		mem, err := GetMemory(db, projectID, c.ID)
		if err != nil || mem == nil {
			continue
		}
		byID[c.ID] = c
		items = append(items, recallItem{
			ID:       mem.ID,
			Category: mem.Category,
			What:     security.SanitizeText(mem.What),
			Why:      TruncateText(security.SanitizeText(mem.Why), semanticRecallFieldChars),
			Learned:  TruncateText(security.SanitizeText(mem.Learned), semanticRecallFieldChars),
			Where:    security.SanitizeText(mem.WherePath),
		})
		if len(items) >= SemanticRecallMaxCandidates {
			break
		}
	}
	if len(items) == 0 {
		return candidates, nil, false
	}

	out, err := SemanticRunAgent(ctx, agent, semanticRecallPrompt(query, items))
	if err != nil {
		return candidates, nil, false // fail-open
	}
	ranks, err := parseSemanticRecall(out)
	if err != nil {
		return candidates, nil, false // fail-open
	}

	reasons := make(map[string]string, len(ranks))
	seen := make(map[string]bool, len(ranks))
	var relevantIDs []string
	for _, r := range ranks {
		if !r.Relevant {
			continue
		}
		if _, ok := byID[r.ID]; !ok || seen[r.ID] {
			continue
		}
		seen[r.ID] = true
		relevantIDs = append(relevantIDs, r.ID)
		reasons[r.ID] = TruncateText(strings.TrimSpace(r.Reason), semanticRecallReasonChars)
	}

	if len(relevantIDs) == 0 {
		return nil, reasons, true
	}

	results := make([]*MemorySearchResult, 0, len(relevantIDs))
	for _, id := range relevantIDs {
		results = append(results, byID[id])
		if len(results) >= limit {
			break
		}
	}
	return results, reasons, true
}
