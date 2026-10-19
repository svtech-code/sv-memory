package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/svtech-code/sv-memory/internal/config"
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

// CloseStaleSessions ends all active sessions older than maxAge. Returns the
// count of sessions closed. Used by sv_mem_session_start to auto-clean leaked
// sessions when the user exits an agent without calling sv_mem_session_end.
func CloseStaleSessions(db *sql.DB, projectID string, maxAge time.Duration) (int, error) {
	cutoff := time.Now().Add(-maxAge)
	result, err := db.Exec(
		"UPDATE sessions SET ended_at = ?, summary = ?, status = 'completed' WHERE project_id = ? AND status = 'active' AND started_at < ?",
		time.Now(), "Session expired (auto-closed)", projectID, cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to close stale sessions: %w", err)
	}
	n, _ := result.RowsAffected()
	return int(n), nil
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
		return noSessionContext(db, projectID, limit)
	}

	var sb strings.Builder
	renderSessionHeader(&sb, session)

	mems, err := SearchMemoriesBySessionCompact(db, projectID, session.ID, limit)
	if err != nil {
		return "", err
	}
	renderSessionMemories(&sb, mems)
	renderUserPrompts(&sb, db, projectID, session.ID)
	renderPinnedSection(&sb, db, projectID)
	return sb.String(), nil
}

// noSessionContext renders the fallback when no completed session exists.
func noSessionContext(db *sql.DB, projectID string, limit int) (string, error) {
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

// renderSessionHeader writes the previous-session heading and metadata.
func renderSessionHeader(sb *strings.Builder, session *Session) {
	sb.WriteString("## Previous Session Context\n\n")
	fmt.Fprintf(sb, "**Session ID:** %s\n", session.ID)
	fmt.Fprintf(sb, "**Started:** %s\n", session.StartedAt.Format("2006-01-02 15:04"))
	if !session.EndedAt.IsZero() {
		fmt.Fprintf(sb, "**Ended:** %s\n", session.EndedAt.Format("2006-01-02 15:04"))
	}
	if session.Goal != "" {
		fmt.Fprintf(sb, "**Goal:** %s\n", security.SanitizeText(session.Goal))
	}
	if session.Summary != "" {
		fmt.Fprintf(sb, "**Summary:** %s\n", security.SanitizeText(session.Summary))
	}
}

// renderSessionMemories writes the memories saved during the session.
func renderSessionMemories(sb *strings.Builder, mems []*MemorySearchResult) {
	if len(mems) == 0 {
		return
	}
	fmt.Fprintf(sb, "\n**Memories saved (%d):**\n", len(mems))
	for _, m := range mems {
		fmt.Fprintf(sb, "- [%s] **%s** (ID: %s)\n", strings.ToUpper(m.Category), m.What, m.ID)
	}
}

// renderUserPrompts surfaces the session's recent prompts so intent survives
// compaction (sv_mem_capture_prompt).
func renderUserPrompts(sb *strings.Builder, db *sql.DB, projectID, sessionID string) {
	prompts, err := RecentPrompts(db, projectID, sessionID, 5)
	if err != nil || len(prompts) == 0 {
		return
	}
	fmt.Fprintf(sb, "\n**User prompts (%d):**\n", len(prompts))
	for _, p := range prompts {
		preview := strings.ReplaceAll(p.Content, "\n", " ")
		if len(preview) > 140 {
			preview = preview[:140] + "…"
		}
		fmt.Fprintf(sb, "- %s\n", security.SanitizeText(preview))
	}
}

// renderPinnedSection surfaces pinned memories first so key decisions stay
// visible regardless of session recency (sv_mem_pin).
func renderPinnedSection(sb *strings.Builder, db *sql.DB, projectID string) {
	pinned, err := SearchPinnedMemories(db, projectID, 5)
	if err != nil || len(pinned) == 0 {
		return
	}
	fmt.Fprintf(sb, "\n**📌 Pinned memories (%d):**\n", len(pinned))
	for _, m := range pinned {
		fmt.Fprintf(sb, "- [%s] **%s** (ID: %s)\n", strings.ToUpper(m.Category), m.What, m.ID)
	}
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
	{title: "Recent Work & Known Issues", where: "category IN ('bugfix', 'journal') AND COALESCE(why, '') NOT LIKE 'automatic post-commit capture for %'", withWhy: false, cap: 2},
	{title: "Active Ideas", where: "category = 'idea' AND what NOT LIKE 'RESUELTO:%'", withWhy: false, cap: 2},
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

	shown := collectShownAndSession(db, projectID, &sb)
	bySection, combined := collectAutoBootCandidates(db, projectID, opts, shown)
	selected, reasons := selectAutoBootCandidates(ctx, db, projectID, opts, bySection, combined)
	renderAutoBootSections(&sb, selected, reasons)
	renderConflictSection(&sb, db, projectID)
	renderActiveChangesSection(&sb, db, projectID)

	return strings.TrimSpace(sb.String()), nil
}

// collectShownAndSession writes the previous-session context to sb and returns
// a set of memory IDs to deduplicate against in the per-category sections.
func collectShownAndSession(db *sql.DB, projectID string, sb *strings.Builder) map[string]bool {
	shown := map[string]bool{}
	sessCtx, err := GetSessionContext(db, projectID, 0)
	if err != nil || sessCtx == "" || strings.HasPrefix(sessCtx, "No previous session") || strings.HasPrefix(sessCtx, "No recorded sessions.") {
		return shown
	}
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
	return shown
}

// collectAutoBootCandidates queries the candidate pool per section. With a goal
// the pool is widened so the relevance ranking has material; without a goal it
// stays at the section cap (pure recency).
func collectAutoBootCandidates(db *sql.DB, projectID string, opts AutoBootOptions, shown map[string]bool) ([][]bundleCandidate, []bundleCandidate) {
	bySection := make([][]bundleCandidate, len(autoBootSections))
	var combined []bundleCandidate
	for si, sec := range autoBootSections {
		limit := sec.cap
		if opts.Goal != "" {
			if opts.Semantic {
				// 5 per section × 6 = 30, within SemanticRecallMaxCandidates.
				limit = 5
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
	return bySection, combined
}

// selectAutoBootCandidates selects which candidates to show per section based
// on whether a goal is provided and whether semantic ranking is enabled.
func selectAutoBootCandidates(ctx context.Context, db *sql.DB, projectID string, opts AutoBootOptions, bySection [][]bundleCandidate, combined []bundleCandidate) ([][]bundleCandidate, map[string]string) {
	switch {
	case opts.Goal == "":
		return perSectionCaps(bySection, autoBootSections, false, ""), nil
	case opts.Semantic:
		selected, reasons := semanticSelect(ctx, db, projectID, opts.Goal, combined, ResolveSemanticAgent(opts.Agent), autoBootSections)
		if selected != nil {
			return selected, reasons
		}
		return perSectionCaps(bySection, autoBootSections, true, opts.Goal), nil // fail-open
	default:
		return perSectionCaps(bySection, autoBootSections, true, opts.Goal), nil
	}
}

// renderAutoBootSections writes the per-category section headings and items.
func renderAutoBootSections(sb *strings.Builder, selected [][]bundleCandidate, reasons map[string]string) {
	for si, sec := range autoBootSections {
		items := renderBundleCandidates(selected[si], sec.withWhy, reasons)
		if len(items) > 0 {
			sb.WriteString("**" + sec.title + ":**\n")
			sb.WriteString(strings.Join(items, "\n"))
			sb.WriteString("\n\n")
		}
	}
}

// renderConflictSection surfaces unresolved decision conflicts.
func renderConflictSection(sb *strings.Builder, db *sql.DB, projectID string) {
	if stats, err := ConflictStats(db, projectID); err == nil {
		if pending := stats["pending"]; pending > 0 {
			fmt.Fprintf(sb, "\n**⚠ Pending memory conflicts:** %d — run `sv_mem_conflicts action=scan` to review.\n", pending)
		}
	}
}

// renderActiveChangesSection surfaces in-flight spec changes with task progress,
// and summarizes already-applied changes on a single line to avoid token bloat.
func renderActiveChangesSection(sb *strings.Builder, db *sql.DB, projectID string) {
	stats, err := ChangeStats(db, projectID)
	if err != nil {
		return
	}
	pendingCount := stats[ChangeStatusDraft] + stats[ChangeStatusProposed] + stats[ChangeStatusValidated]
	appliedCount := stats[ChangeStatusApplied]
	if pendingCount+appliedCount == 0 {
		return
	}
	changes, cErr := ListChangesByStatus(db, projectID, "")
	if cErr != nil || len(changes) == 0 {
		fmt.Fprintf(sb, "\n**📋 Active changes:** %d\n", pendingCount+appliedCount)
		return
	}
	var pending []*Change
	for _, c := range changes {
		if c.Status == ChangeStatusDraft || c.Status == ChangeStatusProposed || c.Status == ChangeStatusValidated {
			pending = append(pending, c)
		}
	}
	if len(pending) == 0 {
		fmt.Fprintf(sb, "\n**📋 Active changes:** 0 pending (%d applied — use `sv_spec_list(status=\"applied\")` to inspect)\n", appliedCount)
		return
	}
	sb.WriteString("\n**📋 Active changes:**\n")
	for _, c := range pending {
		tasksLine := ""
		if prog := ParseTaskProgress(c.Tasks); prog.Total > 0 {
			tasksLine = " — " + prog.Summary
		}
		fmt.Fprintf(sb, "- `%s` [%s] %s%s\n", c.Slug, c.Status, TruncateText(c.Title, 50), tasksLine)
	}
	if appliedCount > 0 {
		fmt.Fprintf(sb, "- *(+%d applied changes — use `sv_spec_list(status=\"applied\")` to view)*\n", appliedCount)
	}
	sb.WriteString("\nUse `sv_spec_list` to see all changes, `sv_spec_get(change_id=\"<slug>\")` to inspect one.\n")
}

// BundleWhyCharsLimit returns the configured Auto-Boot rationale truncation
// cap, delegated to config so the memory package never reads viper directly.
// The same cap is used by the Auto-Boot bundle and the context-pack renderers.
func BundleWhyCharsLimit() int {
	return config.BundleWhyChars()
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
			why = TruncateText(security.SanitizeText(why), BundleWhyCharsLimit())
			out = append(out, fmt.Sprintf("- **[%s] %s** (ID: %s)\n  *Why:* %s", strings.ToUpper(c.cat), security.SanitizeText(c.what), c.id, why))
		} else {
			out = append(out, fmt.Sprintf("- **[%s] %s** (ID: %s)", strings.ToUpper(c.cat), security.SanitizeText(c.what), c.id))
		}
	}
	return out
}
