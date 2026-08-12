package memory

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"
)

// CompactionReport summarizes the results of a memory auto-compaction run.
type CompactionReport struct {
	ProjectID           string   `json:"project_id"`
	ProcessedTopics     int      `json:"processed_topics"`
	MemoriesCompacted   int      `json:"memories_compacted"`
	NewSynthesesCreated int      `json:"new_syntheses_created"`
	TopicKeys           []string `json:"topic_keys"`
}

// CompactMemories consolidates multiple entries or high-revision histories under the same topic_key
// into clean, unified summary records, soft-deleting older historical entries.
//
//nolint:gocyclo // consolidation logic spans many merge branches; refactor later
func CompactMemories(db *sql.DB, projectID string) (*CompactionReport, error) {
	return compactMemoriesSince(db, projectID, time.Time{})
}

// CompactMemoriesIncremental runs compaction only over topic keys that changed
// since the last run, tracking the watermark in projects.last_compaction_at.
// This keeps the periodic background worker cheap: after the first full pass,
// each tick only re-examines topic keys with new activity instead of scanning
// the entire history.
func CompactMemoriesIncremental(db *sql.DB, projectID string) (*CompactionReport, error) {
	var since time.Time
	var sinceStr sql.NullString
	if err := db.QueryRow("SELECT last_compaction_at FROM projects WHERE id = ?", projectID).Scan(&sinceStr); err == nil && sinceStr.Valid {
		if t, pErr := parseTime(sinceStr.String); pErr == nil {
			since = t
		}
	}

	report, err := compactMemoriesSince(db, projectID, since)
	if err != nil {
		return nil, err
	}

	// Always advance the watermark (even with nothing to do) so a project that
	// was compacted earlier isn't re-scanned from scratch on every worker tick.
	if _, err := db.Exec("UPDATE projects SET last_compaction_at = ? WHERE id = ?", time.Now(), projectID); err != nil {
		return nil, fmt.Errorf("failed updating last_compaction_at: %w", err)
	}
	return report, nil
}

func compactMemoriesSince(db *sql.DB, projectID string, since time.Time) (*CompactionReport, error) {
	report := &CompactionReport{
		ProjectID: projectID,
	}

	// 1. Find topic_keys with > 1 active memory entry. A single surviving row
	// under a topic_key holds the consolidated truth (topic-key upserts
	// overwrite content, they don't accumulate history), so merging it would
	// only inflate revision_count with a near-duplicate synthesis.
	// When `since` is set, only topic keys that changed afterwards are
	// re-examined (incremental watermark).
	topicQuery := `
	SELECT topic_key, COUNT(*) as cnt
	FROM memories
	WHERE project_id = ? AND topic_key IS NOT NULL AND topic_key != '' AND deleted_at IS NULL
	GROUP BY topic_key
	HAVING COUNT(*) > 1`
	args := []interface{}{projectID}
	if !since.IsZero() {
		topicQuery += ` AND MAX(created_at) > ?`
		args = append(args, since.Format("2006-01-02 15:04:05"))
	}

	rows, err := db.Query(topicQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed querying topic keys for compaction: %w", err)
	}

	var topicKeysToCompact []string
	for rows.Next() {
		var tk string
		var cnt int
		if scanErr := rows.Scan(&tk, &cnt); scanErr == nil && tk != "" {
			topicKeysToCompact = append(topicKeysToCompact, tk)
		}
	}
	rows.Close()
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, rowsErr
	}

	if len(topicKeysToCompact) == 0 {
		return report, nil
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin compaction transaction: %w", err)
	}
	defer tx.Rollback()

	for _, tk := range topicKeysToCompact {
		memQuery := `
		SELECT id, category, what, why, learned, where_path, git_branch, git_commit,
			author, impact, errors_faced, next_steps, session_id, revision_count, created_at,
			last_seen_at, normalized_hash, review_after, pinned
		FROM memories
		WHERE project_id = ? AND topic_key = ? AND deleted_at IS NULL
		ORDER BY created_at ASC`

		memRows, mErr := tx.Query(memQuery, projectID, tk)
		if mErr != nil {
			continue
		}

		var group []*Memory
		for memRows.Next() {
			if m := scanCompactMemoryRow(memRows, tk); m != nil {
				group = append(group, m)
			}
		}
		memRows.Close()

		if len(group) == 0 {
			continue
		}

		latest := group[len(group)-1]
		var whyParts []string
		var learnedParts []string
		maxRev := 0

		for _, m := range group {
			for _, part := range strings.Split(m.Why, " | ") {
				p := strings.TrimSpace(part)
				if p != "" && !containsStr(whyParts, p) {
					whyParts = append(whyParts, p)
				}
			}
			for _, part := range strings.Split(m.Learned, " | ") {
				p := strings.TrimSpace(part)
				if p != "" && !containsStr(learnedParts, p) {
					learnedParts = append(learnedParts, p)
				}
			}
			if m.RevisionCount > maxRev {
				maxRev = m.RevisionCount
			}
		}

		consolidatedWhy := strings.Join(whyParts, " | ")
		consolidatedLearned := strings.Join(learnedParts, " | ")
		newRev := maxRev + len(group)

		// Pick the most recent entry that carries a session association so the
		// consolidated memory keeps being discoverable by session context
		// (sv_mem_context / Auto-Boot) after compaction. Falls back to the
		// latest row when no entry has a session_id.
		meta := latest
		for _, m := range group {
			if m.SessionID != "" {
				meta = m
			}
		}

		// Soft-delete older entries
		now := time.Now()
		for _, m := range group {
			if _, e := tx.Exec("UPDATE memories SET deleted_at = ? WHERE id = ?", now, m.ID); e != nil {
				return nil, fmt.Errorf("compaction: failed soft-deleting %s: %w", m.ID, e)
			}
		}

		// Create clean unified memory, preserving session + metadata of the
		// chosen source entry so session context recovery keeps working. The
		// synthesized row keeps the source created_at for chronological
		// continuity, recomputes the dedup hash from the consolidated content,
		// and carries over last_seen_at/review_after/pinned so the row stays on
		// the policy-review and dedup radar after compaction.
		synth := &Memory{
			ID:             newID(),
			ProjectID:      projectID,
			Category:       latest.Category,
			What:           latest.What,
			Why:            consolidatedWhy,
			WherePath:      latest.WherePath,
			Learned:        consolidatedLearned,
			GitBranch:      meta.GitBranch,
			GitCommit:      meta.GitCommit,
			Author:         meta.Author,
			Impact:         meta.Impact,
			ErrorsFaced:    meta.ErrorsFaced,
			NextSteps:      meta.NextSteps,
			SessionID:      meta.SessionID,
			TopicKey:       tk,
			RevisionCount:  newRev,
			DuplicateCount: len(group),
			LastSeenAt:     meta.LastSeenAt,
			NormalizedHash: computeHash(latest.What, consolidatedWhy, consolidatedLearned, latest.WherePath),
			ReviewAfter:    meta.ReviewAfter,
			Pinned:         meta.Pinned,
			CreatedAt:      latest.CreatedAt,
		}
		if synth.CreatedAt.IsZero() {
			synth.CreatedAt = now
		}
		if synth.ReviewAfter.IsZero() {
			synth.ReviewAfter = now.Add(decayReviewAfter(synth.Category))
		}

		_, insErr := tx.Exec(memoryInsertConflictQuery(), memoryInsertArgs(synth, synth.CreatedAt)...)
		if insErr == nil {
			report.ProcessedTopics++
			report.MemoriesCompacted += len(group)
			report.NewSynthesesCreated++
			report.TopicKeys = append(report.TopicKeys, tk)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed committing compaction transaction: %w", err)
	}

	return report, nil
}

// scanCompactMemoryRow reads one compaction group row (the extended column set:
// base fields plus last_seen_at/normalized_hash/review_after/pinned) into a
// Memory, applying best-effort time parsing. Returns nil when the row cannot be
// scanned, mirroring the caller's skip behavior. Parsing helpers live here so
// compactMemoriesSince stays under the gocyclo budget.
func scanCompactMemoryRow(rows *sql.Rows, tk string) *Memory {
	var m Memory
	var createdAtStr string
	var wherePath, why, learned sql.NullString
	var gitBranch, gitCommit, author sql.NullString
	var impact, errorsFaced, nextSteps, sessionID sql.NullString
	var lastSeenAt, normalizedHash, reviewAfter sql.NullString
	var pinned sql.NullInt64
	if sErr := rows.Scan(&m.ID, &m.Category, &m.What, &why, &learned, &wherePath,
		&gitBranch, &gitCommit, &author, &impact, &errorsFaced, &nextSteps, &sessionID,
		&m.RevisionCount, &createdAtStr, &lastSeenAt, &normalizedHash, &reviewAfter, &pinned); sErr != nil {
		return nil
	}
	m.Why = why.String
	m.Learned = learned.String
	m.WherePath = wherePath.String
	m.GitBranch = gitBranch.String
	m.GitCommit = gitCommit.String
	m.Author = author.String
	m.Impact = impact.String
	m.ErrorsFaced = errorsFaced.String
	m.NextSteps = nextSteps.String
	m.SessionID = sessionID.String
	m.TopicKey = tk
	m.NormalizedHash = normalizedHash.String
	m.Pinned = pinned.Valid && pinned.Int64 == 1
	if t, pErr := parseTime(createdAtStr); pErr == nil {
		m.CreatedAt = t
	}
	if lastSeenAt.Valid {
		if t, pErr := parseTime(lastSeenAt.String); pErr == nil {
			m.LastSeenAt = t
		}
	}
	if reviewAfter.Valid {
		if t, pErr := parseTime(reviewAfter.String); pErr == nil {
			m.ReviewAfter = t
		}
	}
	return &m
}

func containsStr(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// StartAutoCompaction launches a background goroutine that runs CompactMemories
// at the specified interval (in minutes). Stops when ctx is cancelled.
func StartAutoCompaction(ctx context.Context, db *sql.DB, projectID string, intervalMinutes int) {
	go func() {
		if intervalMinutes < 1 {
			intervalMinutes = 60
		}
		ticker := time.NewTicker(time.Duration(intervalMinutes) * time.Minute)
		defer ticker.Stop()

		log.Printf("[sv-memory] Auto-compaction worker started (interval: %d min)", intervalMinutes)

		for {
			select {
			case <-ctx.Done():
				log.Println("[sv-memory] Auto-compaction worker stopped")
				return
			case <-ticker.C:
				report, err := CompactMemoriesIncremental(db, projectID)
				if err != nil {
					log.Printf("[sv-memory] Auto-compaction error: %v", err)
					continue
				}
				if report.ProcessedTopics > 0 {
					log.Printf("[sv-memory] Auto-compaction: %d topics processed, %d memories compacted, %d syntheses created",
						report.ProcessedTopics, report.MemoriesCompacted, report.NewSynthesesCreated)
				}
			}
		}
	}()
}
