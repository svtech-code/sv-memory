package memory

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Preflight verdicts: BLOCK means a pinned rule/invariant directly contradicts
// the proposal; WARN means an existing decision/standard overlaps meaningfully
// and should be reviewed before proceeding; PASS means no relevant rule found.
const (
	PreflightPass  = "PASS"
	PreflightWarn  = "WARN"
	PreflightBlock = "BLOCK"
)

// PreflightIssue is a single rule/decision surfaced by the pre-flight check:
// the memory it came from, its category, how similar it is to the proposal,
// whether it is pinned (invariant-like), and the severity assigned.
type PreflightIssue struct {
	MemoryID   string  `json:"memory_id"`
	Category   string  `json:"category"`
	What       string  `json:"what"`
	Similarity float64 `json:"similarity"`
	Pinned     bool    `json:"pinned"`
	Severity   string  `json:"severity"` // PreflightBlock | PreflightWarn
}

// PreflightResult is the compact verdict of a validation: an overall severity
// plus the rules that triggered it. The MCP layer renders it as a few lines,
// keeping the response token-efficient.
type PreflightResult struct {
	Verdict string           `json:"verdict"`
	Issues  []PreflightIssue `json:"issues,omitempty"`
}

// preflightCandidates lists the rule-like memories (standards, decisions,
// architecture) whose FTS5 index matches the proposal tokens, pinned first so
// invariant-like rules surface ahead of ordinary decisions. The FTS5 MATCH
// pre-filters the pool; similarity is re-computed precisely afterwards.
func preflightCandidates(db *sql.DB, projectID, tokensJoined string, limit int) ([]PreflightIssue, error) {
	rows, err := db.Query(`
		SELECT m.id, m.category, m.what, m.pinned
		FROM memories m
		JOIN memories_fts f ON m.rowid = f.rowid
		WHERE m.project_id = ? AND m.deleted_at IS NULL
		  AND m.category IN ('standard', 'decision', 'architecture')
		  AND memories_fts MATCH ?
		ORDER BY m.pinned DESC, rank
		LIMIT ?`, projectID, tokensJoined, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query preflight candidates: %w", err)
	}
	defer rows.Close()

	var out []PreflightIssue
	for rows.Next() {
		var id, category, what string
		var pinned int
		if scanErr := rows.Scan(&id, &category, &what, &pinned); scanErr == nil {
			out = append(out, PreflightIssue{
				MemoryID: id,
				Category: category,
				What:     what,
				Pinned:   pinned == 1,
			})
		}
	}
	return out, rows.Err()
}

// PreflightThreshold returns the Jaccard similarity at or above which an
// existing rule is considered in conflict with a proposal. It follows the
// conflict_threshold config used by the conflict scan, falling back to 0.45.
func PreflightThreshold() float64 {
	if v := viper.GetFloat64("conflict_threshold"); v > 0 {
		return v
	}
	return 0.45
}

// PreflightCheck deterministically validates a proposal (title/what) against
// the project's rules and invariants: standards, decisions, and architecture
// memories. A pinned memory above the similarity threshold is a BLOCK (an
// invariant the agent must not silently violate); a non-pinned overlap is a
// WARN (review before proceeding). The check is pure SQLite + Jaccard, so the
// default path costs zero external LLM calls.
func PreflightCheck(db *sql.DB, projectID, title, what string) (*PreflightResult, error) {
	tokens := tokenizeTitle(title + " " + what)
	if len(tokens) == 0 {
		return &PreflightResult{Verdict: PreflightPass}, nil
	}
	quoted := make([]string, len(tokens))
	for i, t := range tokens {
		quoted[i] = `"` + t + `"`
	}

	candidates, err := preflightCandidates(db, projectID, strings.Join(quoted, " OR "), 30)
	if err != nil {
		return nil, err
	}

	threshold := PreflightThreshold()
	result := &PreflightResult{Verdict: PreflightPass}
	for _, c := range candidates {
		sim := jaccardSimilarity(tokens, tokenizeTitle(c.What))
		if sim < threshold {
			continue
		}
		severity := PreflightWarn
		if c.Pinned {
			severity = PreflightBlock
		}
		c.Similarity = roundTwo(sim)
		c.Severity = severity
		result.Issues = append(result.Issues, c)
		if severity == PreflightBlock {
			result.Verdict = PreflightBlock
		} else if result.Verdict != PreflightBlock {
			result.Verdict = PreflightWarn
		}
	}
	return result, nil
}

// SemanticPreflight re-ranks the deterministic candidates by meaning with the
// configured agent CLI, mirroring the semantic recall / conflict-judge
// infrastructure. It fails open: when the agent is unavailable it returns the
// deterministic verdict unchanged (never degrading a working check).
func SemanticPreflight(ctx context.Context, db *sql.DB, projectID, title, what, agent string) (*PreflightResult, error) {
	det, err := PreflightCheck(db, projectID, title, what)
	if err != nil {
		return nil, err
	}
	if len(det.Issues) == 0 {
		return det, nil
	}
	if agent == "" {
		agent = ResolveSemanticAgent("")
	}

	_, reasons, used := SemanticRecall(ctx, db, projectID, title+" "+what, det.IssuesToCandidates(), agent, len(det.Issues))
	if !used || len(reasons) == 0 {
		return det, nil
	}
	// A semantically-confirmed BLOCK elevates a WARN; a semantic PASS keeps the
	// deterministic WARN as advisory. Semantic confirmation never downgrades a
	// deterministic BLOCK.
	for i := range det.Issues {
		if _, ok := reasons[det.Issues[i].MemoryID]; ok && det.Issues[i].Pinned {
			det.Issues[i].Severity = PreflightBlock
			det.Verdict = PreflightBlock
		}
	}
	return det, nil
}

// IssuesToCandidates adapts preflight issues to the SemanticRecall input shape.
func (r *PreflightResult) IssuesToCandidates() []*MemorySearchResult {
	out := make([]*MemorySearchResult, 0, len(r.Issues))
	for _, it := range r.Issues {
		out = append(out, &MemorySearchResult{ID: it.MemoryID, Category: it.Category, What: it.What})
	}
	return out
}

func roundTwo(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}
