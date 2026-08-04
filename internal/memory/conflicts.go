package memory

import (
	"database/sql"
	"fmt"
	"time"
)

// lastConflictScanAt returns the timestamp of the last applied conflict scan for
// the project, or the zero time if no applied scan has run yet.
func lastConflictScanAt(db *sql.DB, projectID string) time.Time {
	var cutoffStr sql.NullString
	err := db.QueryRow("SELECT last_conflict_scan_at FROM projects WHERE id = ?", projectID).Scan(&cutoffStr)
	if err != nil || !cutoffStr.Valid || cutoffStr.String == "" {
		return time.Time{}
	}
	if t, err := parseTime(cutoffStr.String); err == nil {
		return t
	}
	return time.Time{}
}

// setLastConflictScanAt records that a conflict scan was applied, so future
// scans only compare memories created afterwards instead of re-scanning every
// pair from scratch.
func setLastConflictScanAt(db *sql.DB, projectID string, t time.Time) error {
	_, err := db.Exec("UPDATE projects SET last_conflict_scan_at = ? WHERE id = ?", t.Format(time.RFC3339), projectID)
	return err
}

// ListConflicts returns all relations of type 'conflicts_with' for a project, optionally filtered by status.
func ListConflicts(db *sql.DB, projectID string, status string) ([]*MemoryRelation, error) {
	query := `
		SELECT r.id, r.project_id, r.source_id, r.target_id, r.relation_type, r.status, r.score, r.reason, r.judged_by, r.created_at,
		       m1.what as source_what, m2.what as target_what
		FROM memory_relations r
		LEFT JOIN memories m1 ON r.source_id = m1.id
		LEFT JOIN memories m2 ON r.target_id = m2.id
		WHERE r.project_id = ? AND r.relation_type = 'conflicts_with'
	`
	var args []interface{}
	args = append(args, projectID)

	if status != "" {
		query += " AND r.status = ?"
		args = append(args, status)
	}

	query += " ORDER BY r.created_at DESC"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query conflicts: %w", err)
	}
	defer rows.Close()

	var list []*MemoryRelation
	for rows.Next() {
		var r MemoryRelation
		var srcWhat, tgtWhat sql.NullString
		var createdAtStr string
		err := rows.Scan(
			&r.ID, &r.ProjectID, &r.SourceID, &r.TargetID, &r.RelationType,
			&r.Status, &r.Score, &r.Reason, &r.JudgedBy, &createdAtStr,
			&srcWhat, &tgtWhat,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan conflict: %w", err)
		}

		r.SourceWhat = srcWhat.String
		r.TargetWhat = tgtWhat.String
		if t, err := time.Parse("2006-01-02 15:04:05", createdAtStr); err == nil {
			r.CreatedAt = t
		} else if t, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
			r.CreatedAt = t
		} else {
			r.CreatedAt = time.Now() // Fallback
		}

		list = append(list, &r)
	}

	return list, nil
}

// IgnoreConflict updates a conflict relation's status to 'ignored'.
func IgnoreConflict(db *sql.DB, projectID string, relationID string) error {
	result, err := db.Exec(
		"UPDATE memory_relations SET status = 'ignored', judged_by = 'user' WHERE id = ? AND project_id = ?",
		relationID, projectID,
	)
	if err != nil {
		return fmt.Errorf("failed to ignore conflict: %w", err)
	}

	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("conflict relation %s not found in project %s", relationID, projectID)
	}
	return nil
}

// ConflictStats returns the count of conflicts grouped by their status (pending, judged, ignored).
func ConflictStats(db *sql.DB, projectID string) (map[string]int, error) {
	query := `
		SELECT status, COUNT(*) 
		FROM memory_relations 
		WHERE project_id = ? AND relation_type = 'conflicts_with'
		GROUP BY status
	`
	rows, err := db.Query(query, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch conflict stats: %w", err)
	}
	defer rows.Close()

	stats := map[string]int{
		"pending": 0,
		"judged":  0,
		"ignored": 0,
	}

	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err == nil {
			stats[status] = count
		}
	}
	return stats, nil
}

// ScanConflicts performs pairwise Jaccard similarity analysis on project
// memories. If similarity exceeds the threshold and no prior relation exists, it
// flags it as 'conflicts_with'.
//
// The scan is incremental: only memories created after the last *applied* scan
// (projects.last_conflict_scan_at) are compared against the full memory set, so
// repeated scans cost O(newMemories × totalMemories) instead of O(N²). The
// cutoff only advances when apply=true and the scan completes without being
// stopped early by maxInsert, so dry-runs remain repeatable and a partial scan
// never silently skips memories.
func ScanConflicts(db *sql.DB, projectID string, apply bool, maxInsert int, threshold float64) ([]*MemoryRelation, error) {
	if threshold <= 0 {
		threshold = 0.45 // Default threshold
	}

	// 1. Load all active memories with their creation time.
	rows, err := db.Query("SELECT id, what, category, created_at FROM memories WHERE project_id = ? AND deleted_at IS NULL", projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch memories for scan: %w", err)
	}
	defer rows.Close()

	type scanItem struct {
		id        string
		what      string
		category  string
		createdAt time.Time
	}

	var items []scanItem
	for rows.Next() {
		var item scanItem
		var createdAtStr string
		if scanErr := rows.Scan(&item.id, &item.what, &item.category, &createdAtStr); scanErr == nil {
			item.createdAt, _ = parseTime(createdAtStr)
			items = append(items, item)
		}
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, rowsErr
	}
	if len(items) < 2 {
		return nil, nil
	}

	// 2. Incremental filtering: mark which memories are new since the last
	// applied scan. On the first scan (zero cutoff) everything is new.
	cutoff := lastConflictScanAt(db, projectID)
	newFlags := make([]bool, len(items))
	newCount := 0
	if !cutoff.IsZero() {
		for i := range items {
			if items[i].createdAt.After(cutoff) {
				newFlags[i] = true
				newCount++
			}
		}
		if newCount == 0 {
			return nil, nil // no new memories since the last applied scan
		}
	} else {
		for i := range items {
			newFlags[i] = true
		}
	}

	// 3. Cache tokenizations so each title is tokenized once, not once per pair.
	tokens := make([][]string, len(items))
	for i := range items {
		tokens[i] = tokenizeTitle(items[i].what)
	}

	// 4. Load existing relations to avoid duplicate comparisons.
	existing := make(map[string]bool)
	relRows, err := db.Query("SELECT source_id, target_id FROM memory_relations WHERE project_id = ?", projectID)
	if err == nil {
		defer relRows.Close()
		for relRows.Next() {
			var src, tgt string
			if errScan := relRows.Scan(&src, &tgt); errScan == nil {
				existing[src+":"+tgt] = true
				existing[tgt+":"+src] = true
			}
		}
	}

	// 5. Compare candidate pairs (pairs involving at least one new memory).
	var found []*MemoryRelation
	insertedCount := 0
	earlyStop := false

	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if !newFlags[i] && !newFlags[j] {
				continue
			}
			if existing[items[i].id+":"+items[j].id] {
				continue
			}

			sim := jaccardSimilarity(tokens[i], tokens[j])

			if sim >= threshold {
				reason := fmt.Sprintf("High description similarity (%.0f%%) between %s and %s", sim*100, items[i].id, items[j].id)
				rel := &MemoryRelation{
					ID:           newID(),
					ProjectID:    projectID,
					SourceID:     items[i].id,
					TargetID:     items[j].id,
					RelationType: "conflicts_with",
					Status:       "pending",
					Score:        sim,
					Reason:       reason,
					JudgedBy:     "system",
					SourceWhat:   items[i].what,
					TargetWhat:   items[j].what,
					CreatedAt:    time.Now(),
				}
				found = append(found, rel)

				// Apply insertions if requested.
				if apply {
					if maxInsert > 0 && insertedCount >= maxInsert {
						earlyStop = true
						break
					}
					_, err := db.Exec(
						`INSERT INTO memory_relations (id, project_id, source_id, target_id, relation_type, status, score, reason, judged_by)
						 VALUES (?, ?, ?, ?, 'conflicts_with', 'pending', ?, ?, 'system')`,
						rel.ID, rel.ProjectID, rel.SourceID, rel.TargetID, rel.Score, rel.Reason,
					)
					if err == nil {
						insertedCount++
					}
				}
			}
		}
		if earlyStop {
			break
		}
	}

	// Only advance the scan cutoff when the results were persisted and the whole
	// scan completed; a dry-run or a scan cut short by maxInsert must leave the
	// cutoff untouched so nothing is skipped on the next run.
	if apply && !earlyStop {
		_ = setLastConflictScanAt(db, projectID, time.Now())
	}

	return found, nil
}
