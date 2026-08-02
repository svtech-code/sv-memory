package memory

import (
	"database/sql"
	"fmt"
	"time"
)


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

// ScanConflicts performs pairwise Jaccard similarity analysis on project memories.
// If similarity exceeds the threshold and no prior relation exists, it flags it as 'conflicts_with'.
func ScanConflicts(db *sql.DB, projectID string, apply bool, maxInsert int, threshold float64) ([]*MemoryRelation, error) {
	if threshold <= 0 {
		threshold = 0.45 // Default threshold
	}

	// 1. Load all active memories
	rows, err := db.Query("SELECT id, what, category, why, learned FROM memories WHERE project_id = ? AND deleted_at IS NULL", projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch memories for scan: %w", err)
	}
	defer rows.Close()

	type scanItem struct {
		id       string
		what     string
		category string
	}

	var items []scanItem
	for rows.Next() {
		var item scanItem
		var why, learned string
		if err := rows.Scan(&item.id, &item.what, &item.category, &why, &learned); err == nil {
			items = append(items, item)
		}
	}

	// 2. Load existing relations to avoid duplicate comparisons
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

	// 3. Compare pairs
	var found []*MemoryRelation
	insertedCount := 0

	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			m1 := items[i]
			m2 := items[j]

			// Skip if already related
			if existing[m1.id+":"+m2.id] {
				continue
			}

			tokens1 := tokenizeTitle(m1.what)
			tokens2 := tokenizeTitle(m2.what)
			sim := jaccardSimilarity(tokens1, tokens2)

			if sim >= threshold {
				reason := fmt.Sprintf("High description similarity (%.0f%%) between %s and %s", sim*100, m1.id, m2.id)
				rel := &MemoryRelation{
					ID:           newID(),
					ProjectID:    projectID,
					SourceID:     m1.id,
					TargetID:     m2.id,
					RelationType: "conflicts_with",
					Status:       "pending",
					Score:        sim,
					Reason:       reason,
					JudgedBy:     "system",
					SourceWhat:   m1.what,
					TargetWhat:   m2.what,
					CreatedAt:    time.Now(),
				}
				found = append(found, rel)

				// Apply insertions if requested
				if apply {
					if maxInsert > 0 && insertedCount >= maxInsert {
						continue
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
	}

	return found, nil
}
