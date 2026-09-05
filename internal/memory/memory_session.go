package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/svtech-code/sv-memory/internal/security"
)

func StartSession(db *sql.DB, projectID, goal, directory string) (*Session, error) {
	id := newID()
	now := time.Now()
	_, err := db.Exec(
		"INSERT INTO sessions (id, project_id, goal, directory, started_at, status) VALUES (?, ?, ?, ?, ?, 'active')",
		id, projectID, goal, directory, now)
	if err != nil {
		return nil, fmt.Errorf("failed to start session: %w", err)
	}
	return &Session{
		ID:        id,
		ProjectID: projectID,
		Goal:      goal,
		Directory: directory,
		StartedAt: now,
		Status:    "active",
	}, nil
}

// Sentinel errors for session operations.
var (
	ErrSessionNotFound         = errors.New("session not found")
	ErrSessionAlreadyCompleted = errors.New("session already completed")
)

func EndSession(db *sql.DB, id, summary string) error {
	// Redact secrets the same way SaveSessionSummary does, so a session summary
	// never persists raw credentials that later surface via session context.
	summary = security.SanitizeText(summary)
	result, err := db.Exec(
		"UPDATE sessions SET ended_at = ?, summary = ?, status = 'completed' WHERE id = ? AND status = 'active'",
		time.Now(), summary, id)
	if err != nil {
		return fmt.Errorf("failed to end session: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		var status string
		err = db.QueryRow("SELECT status FROM sessions WHERE id = ?", id).Scan(&status)
		if err == sql.ErrNoRows {
			return fmt.Errorf("%w: %s", ErrSessionNotFound, id)
		}
		if err != nil {
			return fmt.Errorf("failed to check session status: %w", err)
		}
		if status == "completed" {
			return fmt.Errorf("%w: %s", ErrSessionAlreadyCompleted, id)
		}
		return fmt.Errorf("session %s has unexpected status %q", id, status)
	}
	return nil
}

func SaveSessionSummary(db *sql.DB, id, goal, discoveries, accomplished, nextSteps, files string) error {
	goal = security.SanitizeText(goal)
	discoveries = security.SanitizeText(discoveries)
	accomplished = security.SanitizeText(accomplished)
	nextSteps = security.SanitizeText(nextSteps)
	files = security.SanitizeText(files)
	summary := fmt.Sprintf("Goal: %s\nDiscoveries: %s\nAccomplished: %s\nNext Steps: %s\nFiles: %s",
		goal, discoveries, accomplished, nextSteps, files)
	result, err := db.Exec("UPDATE sessions SET goal = ?, summary = ? WHERE id = ?", goal, summary, id)
	if err != nil {
		return fmt.Errorf("failed to save session summary: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("session %s not found", id)
	}
	return nil
}

func GetActiveSession(db *sql.DB, projectID string) (*Session, error) {
	row := db.QueryRow("SELECT id, project_id, goal, directory, started_at, ended_at, summary, status FROM sessions WHERE project_id = ? AND status = 'active' ORDER BY started_at DESC LIMIT 1", projectID)
	var s Session
	var startedAtStr string
	var endedAt sql.NullString
	var goal, directory, summary sql.NullString
	err := row.Scan(&s.ID, &s.ProjectID, &goal, &directory, &startedAtStr, &endedAt, &summary, &s.Status)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get active session: %w", err)
	}
	s.Goal = goal.String
	s.Directory = directory.String
	s.Summary = summary.String
	if t, err := parseTime(startedAtStr); err == nil {
		s.StartedAt = t
	}
	return &s, nil
}

func GetLastSession(db *sql.DB, projectID string) (*Session, error) {
	row := db.QueryRow("SELECT id, project_id, goal, directory, started_at, ended_at, summary, status FROM sessions WHERE project_id = ? AND status = 'completed' ORDER BY ended_at DESC LIMIT 1", projectID)
	var s Session
	var startedAtStr, endedAtStr string
	var goal, directory, summary sql.NullString
	err := row.Scan(&s.ID, &s.ProjectID, &goal, &directory, &startedAtStr, &endedAtStr, &summary, &s.Status)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get last session: %w", err)
	}
	s.Goal = goal.String
	s.Directory = directory.String
	s.Summary = summary.String
	if t, err := parseTime(startedAtStr); err == nil {
		s.StartedAt = t
	}
	if endedAtStr != "" {
		if t, err := parseTime(endedAtStr); err == nil {
			s.EndedAt = t
		}
	}
	return &s, nil
}

func GetSessionContext(db *sql.DB, projectID string, limit int) (string, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	session, err := GetLastSession(db, projectID)
	if err != nil {
		return "", err
	}
	if session == nil {
		mems, searchErr := SearchMemories(db, projectID, "", "", limit)
		if searchErr != nil {
			return "", searchErr
		}
		if len(mems) == 0 {
			return "No previous session context found for this project.", nil
		}
		var sb strings.Builder
		sb.WriteString("No recorded sessions. Most recent memories:\n\n")
		for _, m := range mems {
			fmt.Fprintf(&sb, "- [%s] **%s** (ID: %s, %s)\n",
				strings.ToUpper(m.Category), m.What, m.ID, m.CreatedAt.Format("2006-01-02"))
		}
		return sb.String(), nil
	}

	var sb strings.Builder
	sb.WriteString("## Previous Session Context\n\n")
	fmt.Fprintf(&sb, "**Session ID:** %s\n", session.ID)
	fmt.Fprintf(&sb, "**Started:** %s\n", session.StartedAt.Format("2006-01-02 15:04"))
	if !session.EndedAt.IsZero() {
		fmt.Fprintf(&sb, "**Ended:** %s\n", session.EndedAt.Format("2006-01-02 15:04"))
	}
	if session.Goal != "" {
		fmt.Fprintf(&sb, "**Goal:** %s\n", security.SanitizeText(session.Goal))
	}
	if session.Summary != "" {
		fmt.Fprintf(&sb, "**Summary:** %s\n", security.SanitizeText(session.Summary))
	}

	mems, err := SearchMemoriesBySessionCompact(db, projectID, session.ID, limit)
	if err != nil {
		return "", err
	}
	if len(mems) > 0 {
		fmt.Fprintf(&sb, "\n**Memories saved (%d):**\n", len(mems))
		for _, m := range mems {
			fmt.Fprintf(&sb, "- [%s] **%s** (ID: %s)\n",
				strings.ToUpper(m.Category), m.What, m.ID)
		}
	}

	// Recent user prompts surface the user's intent for the session, so after
	// compaction the agent can recover what was being asked (sv_mem_capture_prompt).
	prompts, perr := RecentPrompts(db, projectID, session.ID, 5)
	if perr == nil && len(prompts) > 0 {
		fmt.Fprintf(&sb, "\n**User prompts (%d):**\n", len(prompts))
		for _, p := range prompts {
			preview := strings.ReplaceAll(p.Content, "\n", " ")
			if len(preview) > 140 {
				preview = preview[:140] + "…"
			}
			fmt.Fprintf(&sb, "- %s\n", security.SanitizeText(preview))
		}
	}

	// Pinned memories surface first so key decisions stay visible regardless
	// of session recency (sv_mem_pin with action='pin' / 'unpin').
	pinned, perr := SearchPinnedMemories(db, projectID, 5)
	if perr == nil && len(pinned) > 0 {
		fmt.Fprintf(&sb, "\n**📌 Pinned memories (%d):**\n", len(pinned))
		for _, m := range pinned {
			fmt.Fprintf(&sb, "- [%s] **%s** (ID: %s)\n",
				strings.ToUpper(m.Category), m.What, m.ID)
		}
	}
	return sb.String(), nil
}

// AutoBootOptions controls how the Auto-Boot context bundle is assembled. When
// Goal is set the per-section candidates are ranked by relevance to it instead
// of pure recency; Semantic opts into an LLM re-rank via the configured agent
// CLI (one batched call, failing open to the deterministic keyword ranking).
type AutoBootOptions struct {
	Goal     string
	Semantic bool
	Agent    string
}

// bundleCandidate is one memory in the Auto-Boot selection pool, tagged with
// its section and ranking inputs.
type bundleCandidate struct {
	id        string
	cat       string
	what      string
	why       string
	pinned    bool
	createdAt time.Time
	section   int
}

// bundleSection describes one Auto-Boot section: its title, the SQL predicate
// on memories, how many candidates to show, and whether the rationale is
// rendered.
type bundleSection struct {
	title   string
	where   string
	withWhy bool
	cap     int
}

var autoBootSections = []bundleSection{
	{title: "Key Architectural Decisions", where: "category IN ('architecture', 'decision')", withWhy: true, cap: 3},
	{title: "Standards & Conventions", where: "category = 'standard'", withWhy: false, cap: 2},
	{title: "Recent Work & Known Issues", where: "category IN ('bugfix', 'journal')", withWhy: false, cap: 2},
	// Postmortems are the most reusable lesson (what went wrong and how to avoid
	// it), so the single most relevant one is surfaced with its rationale.
	{title: "Postmortems & Lessons Learned", where: "category = 'postmortem'", withWhy: true, cap: 1},
	// Recent Q&A notes surface the latest resolved question without its full
	// rationale to keep the bundle compact; the agent drills down if needed.
	{title: "Recent Q&A", where: "category = 'qa'", withWhy: false, cap: 1},
}

// GetAutoBootBundle assembles the session-start context bundle. Without a goal
// the sections keep their recency-based selection (unchanged behavior). With a
// goal the per-section candidates are ranked by relevance to it: pinned first,
// then keyword overlap with the goal, then recency (deterministic, the default).
// When opts.Semantic is set a single batched agent call re-ranks the combined
// pool by meaning and fails open to the deterministic ranking if the agent is
// unavailable.
func GetAutoBootBundle(ctx context.Context, db *sql.DB, projectID string, opts AutoBootOptions) (string, error) {
	var sb strings.Builder
	sb.WriteString("### 🚀 Auto-Boot Context Bundle\n\n")

	// Collect IDs already shown in the previous-session section (and the pinned
	// memories surfaced there) so the per-category sections below don't repeat
	// them (dedup).
	shown := map[string]bool{}
	sessCtx, err := GetSessionContext(db, projectID, 0)
	if err == nil && sessCtx != "" && !strings.HasPrefix(sessCtx, "No previous session") {
		sb.WriteString(sessCtx)
		sb.WriteString("\n\n")
		if last, lErr := GetLastSession(db, projectID); lErr == nil && last != nil {
			if mems, mErr := SearchMemoriesBySessionCompact(db, projectID, last.ID, 10); mErr == nil {
				for _, m := range mems {
					shown[m.ID] = true
				}
			}
		}
		if pinned, pErr := SearchPinnedMemories(db, projectID, 20); pErr == nil {
			for _, m := range pinned {
				shown[m.ID] = true
			}
		}
	}

	// Fetch the candidate pool per section. With a goal the pool is widened so
	// the relevance ranking has material; without a goal it stays at the section
	// cap (pure recency, unchanged behavior).
	bySection := make([][]bundleCandidate, len(autoBootSections))
	var combined []bundleCandidate
	for si, sec := range autoBootSections {
		limit := sec.cap
		if opts.Goal != "" {
			if opts.Semantic {
				// 6 per section × 5 = 30, within SemanticRecallMaxCandidates.
				limit = 6
			} else {
				limit = 15
			}
		}
		rows, err := db.Query(`
			SELECT id, category, what, why, pinned, created_at
			FROM memories
			WHERE project_id = ? AND `+sec.where+` AND deleted_at IS NULL
			ORDER BY created_at DESC LIMIT ?`, projectID, limit)
		if err != nil {
			continue
		}
		for rows.Next() {
			var c bundleCandidate
			var pinned sql.NullInt64
			var createdStr string
			if sErr := rows.Scan(&c.id, &c.cat, &c.what, &c.why, &pinned, &createdStr); sErr != nil {
				continue
			}
			if shown[c.id] {
				continue
			}
			c.pinned = pinned.Valid && pinned.Int64 == 1
			if t, pErr := parseTime(createdStr); pErr == nil {
				c.createdAt = t
			}
			c.section = si
			bySection[si] = append(bySection[si], c)
			if opts.Semantic {
				combined = append(combined, c)
			}
		}
		rows.Close()
	}

	// Select which candidates to show per section.
	var selected [][]bundleCandidate
	var reasons map[string]string
	switch {
	case opts.Goal == "":
		selected = perSectionCaps(bySection, autoBootSections, false, "")
	case opts.Semantic:
		selected, reasons = semanticSelect(ctx, db, projectID, opts.Goal, combined, ResolveSemanticAgent(opts.Agent), autoBootSections)
		if selected == nil {
			selected = perSectionCaps(bySection, autoBootSections, true, opts.Goal) // fail-open → deterministic
		}
	default:
		selected = perSectionCaps(bySection, autoBootSections, true, opts.Goal)
	}

	for si, sec := range autoBootSections {
		items := renderBundleCandidates(selected[si], sec.withWhy, reasons)
		if len(items) > 0 {
			sb.WriteString("**" + sec.title + ":**\n")
			sb.WriteString(strings.Join(items, "\n"))
			sb.WriteString("\n\n")
		}
	}

	// Surface unresolved decision conflicts as context: the agent should know
	// when two memories contradict each other before relying on either. Only
	// emitted when conflicts exist, so the token cost is zero when healthy.
	if stats, err := ConflictStats(db, projectID); err == nil {
		if pending := stats["pending"]; pending > 0 {
			fmt.Fprintf(&sb, "\n**⚠ Pending memory conflicts:** %d — run `sv_mem_conflicts action=scan` to review.\n", pending)
		}
	}

	// Surface in-flight spec changes: proposals the agent is (or should be)
	// working on. Only emitted when a non-terminal change exists, keeping the
	// token cost zero when the store has no active work.
	if stats, err := ChangeStats(db, projectID); err == nil {
		total := stats[ChangeStatusDraft] + stats[ChangeStatusProposed] + stats[ChangeStatusValidated] + stats[ChangeStatusApplied]
		if total > 0 {
			changes, cErr := ListChangesByStatus(db, projectID, "")
			if cErr == nil && len(changes) > 0 {
				sb.WriteString("\n**📋 Active changes:**\n")
				for _, c := range changes {
					tasksLine := ""
					if prog := ParseTaskProgress(c.Tasks); prog.Total > 0 {
						tasksLine = " — " + prog.Summary
					}
					fmt.Fprintf(&sb, "- `%s` [%s] %s%s\n", c.Slug, c.Status, TruncateText(c.Title, 50), tasksLine)
				}
				sb.WriteString("\nUse `sv_spec_list` to see all changes, `sv_spec_get(change_id=\"<slug>\")` to inspect one.\n")
			} else {
				fmt.Fprintf(&sb, "\n**📋 Active changes:** %d\n", total)
			}
		}
	}

	return strings.TrimSpace(sb.String()), nil
}

// bundleWhyChars caps the rationale shown per memory in the Auto-Boot bundle
// when withWhy is true, so long rationales don't bloat the session-start
// response. The agent can drill down with sv_mem_get for full content.
const bundleWhyChars = 300

// bundleWhyCharsLimit returns the configured Auto-Boot rationale cap, falling
// back to bundleWhyChars when the bundle_why_chars config key is unset or
// non-positive. Tunable via ~/.sv-memory/config.yaml without recompiling.
func bundleWhyCharsLimit() int {
	if v := viper.GetInt("bundle_why_chars"); v > 0 {
		return v
	}
	return bundleWhyChars
}

// BundleWhyCharsLimit is the exported form of bundleWhyCharsLimit, used by
// renderers outside the memory package (e.g. the context-pack MCP tool) so the
// why truncation cap stays consistent across the Auto-Boot bundle and packs.
func BundleWhyCharsLimit() int {
	return bundleWhyCharsLimit()
}

// perSectionCaps selects up to each section's cap from its pool. When goalAware
// the pool is ranked by relevance to goal (pinned first, then keyword overlap,
// then recency); otherwise the pool keeps its recency ordering.
func perSectionCaps(bySection [][]bundleCandidate, sections []bundleSection, goalAware bool, goal string) [][]bundleCandidate {
	selected := make([][]bundleCandidate, len(sections))
	for si, sec := range sections {
		pool := bySection[si]
		if goalAware {
			sort.SliceStable(pool, func(i, j int) bool {
				pi, pj := pool[i].pinned, pool[j].pinned
				if pi != pj {
					return pi
				}
				siScore, sjScore := keywordScore(goal, pool[i]), keywordScore(goal, pool[j])
				if siScore != sjScore {
					return siScore > sjScore
				}
				return pool[i].createdAt.After(pool[j].createdAt)
			})
		}
		n := sec.cap
		if len(pool) < n {
			n = len(pool)
		}
		selected[si] = pool[:n]
	}
	return selected
}

// keywordScore counts how many goal tokens appear in a candidate's title or
// rationale — the deterministic relevance heuristic used when no goal-triggered
// semantic ranking is requested.
func keywordScore(goal string, c bundleCandidate) int {
	tokens := tokenizeTitle(goal)
	if len(tokens) == 0 {
		return 0
	}
	hay := strings.ToLower(c.what + " " + c.why)
	score := 0
	for _, t := range tokens {
		if strings.Contains(hay, t) {
			score++
		}
	}
	return score
}

// semanticSelect runs one batched SemanticRecall over the combined pool and
// distributes the relevant candidates to their sections, respecting each cap.
// It returns (nil, nil) when the LLM ranking could not be applied so the caller
// falls back to the deterministic keyword ranking (fail-open).
func semanticSelect(ctx context.Context, db *sql.DB, projectID, goal string, combined []bundleCandidate, agent string, sections []bundleSection) ([][]bundleCandidate, map[string]string) {
	if len(combined) == 0 {
		return make([][]bundleCandidate, len(sections)), nil
	}
	cands := make([]*MemorySearchResult, 0, len(combined))
	for _, c := range combined {
		cands = append(cands, &MemorySearchResult{ID: c.id})
	}
	results, reasons, used := SemanticRecall(ctx, db, projectID, goal, cands, agent, len(combined))
	if !used {
		return nil, nil
	}
	byID := make(map[string]bundleCandidate, len(combined))
	for _, c := range combined {
		byID[c.id] = c
	}
	selected := make([][]bundleCandidate, len(sections))
	counts := make([]int, len(sections))
	for _, r := range results {
		c, ok := byID[r.ID]
		if !ok {
			continue
		}
		if counts[c.section] >= sections[c.section].cap {
			continue
		}
		selected[c.section] = append(selected[c.section], c)
		counts[c.section]++
	}
	return selected, reasons
}

// renderBundleCandidates renders a section's candidates, appending the semantic
// relevance reason to the rationale when available and withWhy is set.
func renderBundleCandidates(items []bundleCandidate, withWhy bool, reasons map[string]string) []string {
	out := make([]string, 0, len(items))
	for _, c := range items {
		if withWhy {
			why := c.why
			if r, ok := reasons[c.id]; ok && r != "" {
				why += " — " + r
			}
			why = TruncateText(security.SanitizeText(why), bundleWhyCharsLimit())
			out = append(out, fmt.Sprintf("- **[%s] %s** (ID: %s)\n  *Why:* %s", strings.ToUpper(c.cat), security.SanitizeText(c.what), c.id, why))
		} else {
			out = append(out, fmt.Sprintf("- **[%s] %s** (ID: %s)", strings.ToUpper(c.cat), security.SanitizeText(c.what), c.id))
		}
	}
	return out
}
