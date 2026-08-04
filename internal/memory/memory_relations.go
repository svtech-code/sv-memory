package memory

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/svtech-code/sv-memory/internal/security"
)

func SaveJudgment(db *sql.DB, projectID, sourceID, targetID, relationType, reason, judgedBy string) (*MemoryRelation, error) {
	if sourceID == targetID {
		return nil, errors.New("cannot create a relation between a memory and itself")
	}
	id := newID()
	now := time.Now()
	reason = security.SanitizeText(reason)
	judgedBy = security.SanitizeText(judgedBy)

	var srcExists, tgtExists bool
	_ = db.QueryRow("SELECT COUNT(*) FROM memories WHERE project_id = ? AND id = ? AND deleted_at IS NULL", projectID, sourceID).Scan(&srcExists)
	_ = db.QueryRow("SELECT COUNT(*) FROM memories WHERE project_id = ? AND id = ? AND deleted_at IS NULL", projectID, targetID).Scan(&tgtExists)
	if !srcExists || !tgtExists {
		return nil, fmt.Errorf("one or both memories not found or are deleted (source=%v target=%v)", srcExists, tgtExists)
	}

	_, err := db.Exec(`
		INSERT INTO memory_relations (id, project_id, source_id, target_id, relation_type, reason, judged_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			relation_type = excluded.relation_type,
			reason = excluded.reason,
			judged_by = excluded.judged_by,
			created_at = excluded.created_at
	`, id, projectID, sourceID, targetID, relationType, reason, judgedBy, now)
	if err != nil {
		return nil, fmt.Errorf("failed to save judgment: %w", err)
	}
	return &MemoryRelation{
		ID:           id,
		ProjectID:    projectID,
		SourceID:     sourceID,
		TargetID:     targetID,
		RelationType: relationType,
		Reason:       reason,
		JudgedBy:     judgedBy,
		CreatedAt:    now,
	}, nil
}

func GetRelations(db *sql.DB, projectID, memoryID string) ([]*MemoryRelation, error) {
	rows, err := db.Query(`
		SELECT id, project_id, source_id, target_id, relation_type, reason, judged_by, created_at
		FROM memory_relations
		WHERE project_id = ? AND (source_id = ? OR target_id = ?)
		ORDER BY created_at DESC
	`, projectID, memoryID, memoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var relations []*MemoryRelation
	for rows.Next() {
		var r MemoryRelation
		var createdAtStr string
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.SourceID, &r.TargetID, &r.RelationType, &r.Reason, &r.JudgedBy, &createdAtStr); err != nil {
			return nil, err
		}
		if t, err := parseTime(createdAtStr); err == nil {
			r.CreatedAt = t
		}
		relations = append(relations, &r)
	}
	return relations, rows.Err()
}

func CountRelations(db *sql.DB, projectID, memoryID string) (int, error) {
	var n int
	err := db.QueryRow("SELECT COUNT(*) FROM memory_relations WHERE project_id = ? AND (source_id = ? OR target_id = ?)", projectID, memoryID, memoryID).Scan(&n)
	return n, err
}

func CompareMemories(db *sql.DB, projectID, id1, id2 string) (string, error) {
	m1, err := GetMemory(db, projectID, id1)
	if err != nil {
		return "", err
	}
	m2, err := GetMemory(db, projectID, id2)
	if err != nil {
		return "", err
	}
	if m1 == nil || m2 == nil {
		return "", errors.New("one or both memories not found")
	}

	var sb strings.Builder
	sb.WriteString("## Memory Comparison\n\n")
	sb.WriteString("| Field | Memory 1 | Memory 2 |\n")
	sb.WriteString("|-------|----------|----------|\n")
	fmt.Fprintf(&sb, "| **ID** | `%s` | `%s` |\n", m1.ID, m2.ID)
	fmt.Fprintf(&sb, "| **Category** | `%s` | `%s` |\n", m1.Category, m2.Category)
	fmt.Fprintf(&sb, "| **What** | %s | %s |\n", m1.What, m2.What)
	fmt.Fprintf(&sb, "| **Why** | %s | %s |\n", m1.Why, m2.Why)
	fmt.Fprintf(&sb, "| **Learned** | %s | %s |\n", m1.Learned, m2.Learned)
	if m1.WherePath != "" || m2.WherePath != "" {
		fmt.Fprintf(&sb, "| **Path** | `%s` | `%s` |\n", m1.WherePath, m2.WherePath)
	}
	if m1.TopicKey != "" || m2.TopicKey != "" {
		fmt.Fprintf(&sb, "| **Topic** | `%s` | `%s` |\n", m1.TopicKey, m2.TopicKey)
	}
	fmt.Fprintf(&sb, "| **Date** | %s | %s |\n", m1.CreatedAt.Format("2006-01-02"), m2.CreatedAt.Format("2006-01-02"))

	rels, _ := GetRelations(db, projectID, id1)
	for _, r := range rels {
		if (r.SourceID == id1 && r.TargetID == id2) || (r.SourceID == id2 && r.TargetID == id1) {
			fmt.Fprintf(&sb, "\n**Existing relation:** `%s` — %s\n", r.RelationType, r.Reason)
			break
		}
	}
	return sb.String(), nil
}

// ReviewMemories returns memories that may need attention (old, stale, high
// duplicates, or consolidation candidates). The relation count for every
// memory is fetched with a single grouped query instead of one COUNT per row,
// and the result is ordered review-worthy first (needs-attention items, then by
// age) and bounded by limit, so a small review surfaces the most relevant
// candidates rather than the most recently created memories.
func ReviewMemories(db *sql.DB, projectID string, limit int) ([]*MemoryReviewItem, error) {
	// Relation counts in one grouped query (avoids an N+1 COUNT per memory).
	// The rows must be fully consumed and closed before the next query on a
	// single-connection DB (the writer pool has MaxOpenConns=1).
	relCounts := make(map[string]int)
	relRows, err := db.Query(`
		SELECT mem_id, COUNT(*)
		FROM (
			SELECT source_id AS mem_id FROM memory_relations WHERE project_id = ?
			UNION ALL
			SELECT target_id AS mem_id FROM memory_relations WHERE project_id = ?
		)
		GROUP BY mem_id`, projectID, projectID)
	if err != nil {
		return nil, err
	}
	for relRows.Next() {
		var mid string
		var n int
		if err := relRows.Scan(&mid, &n); err == nil {
			relCounts[mid] = n
		}
	}
	relRows.Close()
	if err := relRows.Err(); err != nil {
		return nil, err
	}

	rows, err := db.Query(`
		SELECT id, category, what, topic_key, revision_count, duplicate_count, created_at, last_seen_at
		FROM memories
		WHERE project_id = ? AND deleted_at IS NULL
		ORDER BY created_at DESC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	now := time.Now()
	var items []*MemoryReviewItem
	for rows.Next() {
		var r MemoryReviewItem
		var createdAtStr string
		var lastSeenAtStr sql.NullString
		var topicKey sql.NullString
		var revisionCount, duplicateCount sql.NullInt64

		r.Memory = &MemorySearchResult{}
		err := rows.Scan(&r.Memory.ID, &r.Memory.Category, &r.Memory.What, &topicKey, &revisionCount, &duplicateCount, &createdAtStr, &lastSeenAtStr)
		if err != nil {
			return nil, err
		}
		r.Memory.TopicKey = topicKey.String
		if revisionCount.Valid {
			r.RevisionCount = int(revisionCount.Int64)
			r.Memory.RevisionCount = r.RevisionCount
		}
		if duplicateCount.Valid {
			r.DuplicateCount = int(duplicateCount.Int64)
			r.Memory.DuplicateCount = r.DuplicateCount
		}
		if t, err := parseTime(createdAtStr); err == nil {
			r.Memory.CreatedAt = t
			r.AgeDays = int(now.Sub(t).Hours() / 24)
		}
		if lastSeenAtStr.Valid && lastSeenAtStr.String != "" {
			if t, err := parseTime(lastSeenAtStr.String); err == nil {
				r.LastSeenDays = int(now.Sub(t).Hours() / 24)
			}
		}

		r.RelationCount = relCounts[r.Memory.ID]

		var reasons []string
		if r.AgeDays > 30 {
			reasons = append(reasons, fmt.Sprintf("old (%d days)", r.AgeDays))
		}
		if r.LastSeenDays > 60 {
			reasons = append(reasons, fmt.Sprintf("stale (last seen %d days ago)", r.LastSeenDays))
		}
		if r.DuplicateCount > 3 {
			reasons = append(reasons, fmt.Sprintf("high duplicates (%d)", r.DuplicateCount))
		}
		if r.RevisionCount > 5 {
			reasons = append(reasons, fmt.Sprintf("many revisions (%d)", r.RevisionCount))
			r.NeedsConsolidation = true
		}
		if len(reasons) == 0 {
			reasons = append(reasons, "recent and healthy")
		}
		r.Reason = strings.Join(reasons, "; ")
		items = append(items, &r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Prioritize memories that actually need attention, then by age, so the
	// limited output surfaces stale/duplicate/consolidation candidates first.
	sort.SliceStable(items, func(i, j int) bool {
		ineed := items[i].Reason != "recent and healthy"
		jneed := items[j].Reason != "recent and healthy"
		if ineed != jneed {
			return ineed
		}
		return items[i].AgeDays > items[j].AgeDays
	})

	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}
