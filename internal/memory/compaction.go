package memory

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"
)

// DefaultCompactionThreshold is the default number of fragmented topic keys before auto-compaction triggers.
const DefaultCompactionThreshold = 3

// CompactionReport summarizes the results of a memory auto-compaction run.
type CompactionReport struct {
	ProjectID           string   `json:"project_id"`
	ProcessedTopics     int      `json:"processed_topics"`
	MemoriesCompacted   int      `json:"memories_compacted"`
	NewSynthesesCreated int      `json:"new_syntheses_created"`
	TopicKeys           []string `json:"topic_keys"`
}

// CompactionHealth represents the fragmentation status of topic keys in the project.
type CompactionHealth struct {
	ProjectID               string   `json:"project_id"`
	FragmentedTopics        int      `json:"fragmented_topics"`
	TotalFragmentedMemories int      `json:"total_fragmented_memories"`
	Threshold               int      `json:"threshold"`
	NeedsCompaction         bool     `json:"needs_compaction"`
	CandidateTopicKeys      []string `json:"candidate_topic_keys"`
}

// CheckCompactionHealth inspects how many topic keys have multiple active rows
// and determines if compaction is needed according to the given threshold.
func CheckCompactionHealth(db *sql.DB, projectID string, threshold int) (*CompactionHealth, bool, error) {
	if threshold <= 0 {
		threshold = DefaultCompactionThreshold
	}

	rows, err := db.Query(`
		SELECT topic_key, COUNT(*) as cnt
		FROM memories
		WHERE project_id = ? AND topic_key IS NOT NULL AND topic_key != '' AND deleted_at IS NULL
		GROUP BY topic_key
		HAVING cnt > 1
		ORDER BY cnt DESC`, projectID)
	if err != nil {
		return nil, false, fmt.Errorf("failed to check compaction health: %w", err)
	}
	defer rows.Close()

	health := &CompactionHealth{
		ProjectID: projectID,
		Threshold: threshold,
	}

	for rows.Next() {
		var tk string
		var cnt int
		if err = rows.Scan(&tk, &cnt); err != nil {
			return nil, false, fmt.Errorf("failed scanning compaction health row: %w", err)
		}
		health.FragmentedTopics++
		health.TotalFragmentedMemories += cnt
		health.CandidateTopicKeys = append(health.CandidateTopicKeys, tk)
	}
	if err = rows.Err(); err != nil {
		return nil, false, fmt.Errorf("error iterating compaction health rows: %w", err)
	}

	health.NeedsCompaction = health.FragmentedTopics >= threshold
	return health, health.NeedsCompaction, nil
}

// MaybeAutoCompact checks compaction health against the threshold and, if needed,
// executes incremental compaction atomically. Returns (report, ran, err).
func MaybeAutoCompact(db *sql.DB, projectID string, threshold int) (*CompactionReport, bool, error) {
	_, needed, err := CheckCompactionHealth(db, projectID, threshold)
	if err != nil {
		return nil, false, err
	}
	if !needed {
		return nil, false, nil
	}
	report, err := CompactMemoriesIncremental(db, projectID)
	if err != nil {
		return nil, false, err
	}
	return report, true, nil
}

// CompactMemories consolidates multiple entries or high-revision histories under the same topic_key
// into clean, unified summary records, soft-deleting older historical entries.
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
	report := &CompactionReport{ProjectID: projectID}

	// Find topic_keys with > 1 active memory entry. A single surviving row under
	// a topic_key holds the consolidated truth (topic-key upserts overwrite
	// content, they don't accumulate history), so merging it would only inflate
	// revision_count with a near-duplicate synthesis. When `since` is set, only
	// topic keys that changed afterwards are re-examined (incremental watermark).
	topicKeysToCompact, err := topicKeysToCompact(db, projectID, since)
	if err != nil {
		return nil, err
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
		if err := compactTopicKey(tx, projectID, tk, report); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed committing compaction transaction: %w", err)
	}

	return report, nil
}

// topicKeysToCompact returns topic keys with more than one active memory row,
// optionally restricted to those with activity after since.
func topicKeysToCompact(db *sql.DB, projectID string, since time.Time) ([]string, error) {
	topicQuery := `
	SELECT topic_key, COUNT(*) as cnt
	FROM memories
	WHERE project_id = ? AND topic_key IS NOT NULL AND topic_key != '' AND deleted_at IS NULL
	GROUP BY topic_key
	HAVING COUNT(*) > 1`
	args := []interface{}{projectID}
	if !since.IsZero() {
		topicQuery += ` AND MAX(created_at) > ?`
		args = append(args, since)
	}

	rows, err := db.Query(topicQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed querying topic keys for compaction: %w", err)
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var tk string
		var cnt int
		if scanErr := rows.Scan(&tk, &cnt); scanErr == nil && tk != "" {
			keys = append(keys, tk)
		}
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, rowsErr
	}
	return keys, nil
}

// compactTopicKey consolidates one topic key's memory group into a single
// synthesis row and soft-deletes the originals.
func compactTopicKey(tx *sql.Tx, projectID, tk string, report *CompactionReport) error {
	group, err := loadCompactionGroup(tx, projectID, tk)
	if err != nil {
		return err
	}
	if len(group) == 0 {
		return nil
	}

	synth, now := buildSynthesis(projectID, tk, group)

	// Insert the synthesis FIRST so a failure aborts the transaction
	// (defer tx.Rollback) and the older entries are never soft-deleted.
	// Soft-deleting before the insert would silently lose every memory of the
	// topic if the synthesis insert failed while the transaction still committed.
	if _, insErr := tx.Exec(memoryInsertConflictQuery(), memoryInsertArgs(synth, synth.CreatedAt)...); insErr != nil {
		return fmt.Errorf("compaction: failed inserting synthesis for %s: %w", tk, insErr)
	}

	// Soft-delete older entries only after the synthesis is persisted.
	for _, m := range group {
		if _, e := tx.Exec("UPDATE memories SET deleted_at = ? WHERE id = ?", now, m.ID); e != nil {
			return fmt.Errorf("compaction: failed soft-deleting %s: %w", m.ID, e)
		}
	}

	report.ProcessedTopics++
	report.MemoriesCompacted += len(group)
	report.NewSynthesesCreated++
	report.TopicKeys = append(report.TopicKeys, tk)
	return nil
}

// loadCompactionGroup loads the active memories under a topic key in creation
// order. A query error yields an empty group so the caller skips the topic.
func loadCompactionGroup(tx *sql.Tx, projectID, tk string) ([]*Memory, error) {
	memQuery := `
	SELECT id, category, what, why, learned, where_path, git_branch, git_commit,
		author, impact, errors_faced, next_steps, session_id, revision_count, created_at,
		last_seen_at, normalized_hash, review_after, pinned
	FROM memories
	WHERE project_id = ? AND topic_key = ? AND deleted_at IS NULL
	ORDER BY created_at ASC`

	memRows, mErr := tx.Query(memQuery, projectID, tk)
	if mErr != nil {
		return nil, nil
	}
	defer memRows.Close()

	var group []*Memory
	for memRows.Next() {
		if m := scanCompactMemoryRow(memRows, tk); m != nil {
			group = append(group, m)
		}
	}
	return group, nil
}

// buildSynthesis constructs the consolidated memory from a topic-key group and
// returns it with the timestamp used for the soft-delete.
func buildSynthesis(projectID, tk string, group []*Memory) (*Memory, time.Time) {
	latest := group[len(group)-1]
	whyParts, learnedParts, maxRev := mergeGroupParts(group)

	// Pick the most recent entry carrying a session association so the
	// consolidated memory stays discoverable by session context (sv_mem_context
	// / Auto-Boot) after compaction. Falls back to the latest row.
	meta := latest
	for _, m := range group {
		if m.SessionID != "" {
			meta = m
		}
	}

	now := time.Now()
	consolidatedWhy := strings.Join(whyParts, " | ")
	consolidatedLearned := strings.Join(learnedParts, " | ")
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
		RevisionCount:  maxRev + len(group),
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
	return synth, now
}

// mergeGroupParts dedupes Why/Learned fragments across a group and finds the
// highest revision count.
func mergeGroupParts(group []*Memory) (whyParts, learnedParts []string, maxRev int) {
	for _, m := range group {
		whyParts = appendUniqueParts(whyParts, m.Why)
		learnedParts = appendUniqueParts(learnedParts, m.Learned)
		if m.RevisionCount > maxRev {
			maxRev = m.RevisionCount
		}
	}
	return whyParts, learnedParts, maxRev
}

// appendUniqueParts splits a " | "-joined string, trims each fragment, and
// appends the non-empty, not-yet-present fragments to dst.
func appendUniqueParts(dst []string, joined string) []string {
	for _, part := range strings.Split(joined, " | ") {
		p := strings.TrimSpace(part)
		if p != "" && !containsStr(dst, p) {
			dst = append(dst, p)
		}
	}
	return dst
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
