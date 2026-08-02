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
func CompactMemories(db *sql.DB, projectID string) (*CompactionReport, error) {
	report := &CompactionReport{
		ProjectID: projectID,
	}

	// 1. Find topic_keys with > 1 active memory entries OR revision_count >= 3
	topicQuery := `
	SELECT topic_key, COUNT(*) as cnt
	FROM memories
	WHERE project_id = ? AND topic_key IS NOT NULL AND topic_key != '' AND deleted_at IS NULL
	GROUP BY topic_key`

	rows, err := db.Query(topicQuery, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed querying topic keys for compaction: %w", err)
	}
	defer rows.Close()

	var topicKeysToCompact []string
	for rows.Next() {
		var tk string
		var cnt int
		if scanErr := rows.Scan(&tk, &cnt); scanErr == nil && tk != "" {
			if cnt > 1 {
				topicKeysToCompact = append(topicKeysToCompact, tk)
			}
		}
	}
	rows.Close()

	// Also check high revision memories without multiple rows
	highRevQuery := `
	SELECT topic_key
	FROM memories
	WHERE project_id = ? AND topic_key IS NOT NULL AND topic_key != '' AND revision_count >= 3 AND deleted_at IS NULL`

	hRows, hErr := db.Query(highRevQuery, projectID)
	if hErr == nil {
		for hRows.Next() {
			var tk string
			if scanErr := hRows.Scan(&tk); scanErr == nil && tk != "" {
				if !containsStr(topicKeysToCompact, tk) {
					topicKeysToCompact = append(topicKeysToCompact, tk)
				}
			}
		}
		hRows.Close()
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
			author, impact, errors_faced, next_steps, session_id, revision_count, created_at
		FROM memories
		WHERE project_id = ? AND topic_key = ? AND deleted_at IS NULL
		ORDER BY created_at ASC`

		memRows, mErr := tx.Query(memQuery, projectID, tk)
		if mErr != nil {
			continue
		}

		var group []*Memory
		for memRows.Next() {
			var m Memory
			var createdAtStr string
			var wherePath, why, learned sql.NullString
			var gitBranch, gitCommit, author sql.NullString
			var impact, errorsFaced, nextSteps, sessionID sql.NullString
			if sErr := memRows.Scan(&m.ID, &m.Category, &m.What, &why, &learned, &wherePath,
				&gitBranch, &gitCommit, &author, &impact, &errorsFaced, &nextSteps, &sessionID,
				&m.RevisionCount, &createdAtStr); sErr == nil {
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
				group = append(group, &m)
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
		// chosen source entry so session context recovery keeps working.
		synthID := newID()
		insertStmt := `
		INSERT INTO memories (id, project_id, category, what, why, where_path, learned,
			git_branch, git_commit, author, impact, errors_faced, next_steps, session_id,
			topic_key, revision_count, duplicate_count, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`

		_, insErr := tx.Exec(insertStmt, synthID, projectID, latest.Category, latest.What, consolidatedWhy,
			latest.WherePath, consolidatedLearned, meta.GitBranch, meta.GitCommit, meta.Author,
			meta.Impact, meta.ErrorsFaced, meta.NextSteps, meta.SessionID, tk, newRev, len(group))
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
				report, err := CompactMemories(db, projectID)
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
