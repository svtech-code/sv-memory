package memory

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/svtech-code/sv-memory/internal/graph"
	"github.com/svtech-code/sv-memory/internal/security"
)

type MemorySearchResult struct {
	ID             string    `json:"id"`
	Category       string    `json:"category"`
	What           string    `json:"what"`
	TopicKey       string    `json:"topic_key,omitempty"`
	RevisionCount  int       `json:"revision_count,omitempty"`
	DuplicateCount int       `json:"duplicate_count,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	Score          float64   `json:"score,omitempty"`
}

type Memory struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"project_id"`
	Category       string    `json:"category"`
	What           string    `json:"what"`
	Why            string    `json:"why"`
	WherePath      string    `json:"where_path,omitempty"`
	Learned        string    `json:"learned"`
	GitBranch      string    `json:"git_branch,omitempty"`
	GitCommit      string    `json:"git_commit,omitempty"`
	Author         string    `json:"author,omitempty"`
	Impact         string    `json:"impact,omitempty"`
	ErrorsFaced    string    `json:"errors_faced,omitempty"`
	NextSteps      string    `json:"next_steps,omitempty"`
	SessionID      string    `json:"session_id,omitempty"`
	TopicKey       string    `json:"topic_key,omitempty"`
	RevisionCount  int       `json:"revision_count,omitempty"`
	DuplicateCount int       `json:"duplicate_count,omitempty"`
	LastSeenAt     time.Time `json:"last_seen_at,omitempty"`
	NormalizedHash string    `json:"normalized_hash,omitempty"`
	ReviewAfter    time.Time `json:"review_after,omitempty"`
	Pinned         bool      `json:"pinned,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// decayReviewAfter returns the review-cycle duration for a memory category so
// policies and decisions are re-validated on a predictable schedule. Decisions
// and architecture need a periodic sanity check; standards are more stable;
// bugfixes and ideas go stale fastest.
func decayReviewAfter(category string) time.Duration {
	switch strings.ToLower(category) {
	case "decision", "architecture":
		return 6 * 30 * 24 * time.Hour
	case "standard":
		return 12 * 30 * 24 * time.Hour
	case "bugfix", "idea":
		return 3 * 30 * 24 * time.Hour
	default:
		return 6 * 30 * 24 * time.Hour
	}
}

// ActiveMemoryRationaleRefs returns the (id, category, what, where_path) of all
// active memories that reference a code path, for re-linking the memory <-> code
// rationale_for edges after a full graph rebuild (which wipes the graph tables).
func ActiveMemoryRationaleRefs(db *sql.DB, projectID string) ([]graph.MemoryRationaleRef, error) {
	rows, err := db.Query(`
		SELECT id, category, what, where_path
		FROM memories
		WHERE project_id = ? AND deleted_at IS NULL
		  AND where_path IS NOT NULL AND where_path != ''`, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to list memories for graph re-linking: %w", err)
	}
	defer rows.Close()

	var refs []graph.MemoryRationaleRef
	for rows.Next() {
		var r graph.MemoryRationaleRef
		if err := rows.Scan(&r.ID, &r.Category, &r.What, &r.WherePath); err != nil {
			return nil, err
		}
		refs = append(refs, r)
	}
	return refs, rows.Err()
}

// ActiveSpecCapabilityRefs returns the spec capabilities that should be linked
// into the graph after a rebuild: every capability in the materialized current
// state (capability node without a code edge) plus every non-terminal change
// that carries requirements (capability node + implements edge to where_path).
func ActiveSpecCapabilityRefs(db *sql.DB, projectID string) ([]graph.SpecCapabilityRef, error) {
	var refs []graph.SpecCapabilityRef

	rows, err := db.Query(`
		SELECT c.id, c.capability_path, COALESCE(c.where_path, '')
		FROM changes c
		WHERE c.project_id = ? AND c.status NOT IN ('archived', 'rejected')
		  AND c.capability_path IS NOT NULL AND c.capability_path != ''
		  AND EXISTS (SELECT 1 FROM spec_requirements r
		              WHERE r.project_id = c.project_id AND r.change_id = c.id)`, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to list changes for spec re-linking: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ref graph.SpecCapabilityRef
		if err = rows.Scan(&ref.ChangeID, &ref.CapabilityPath, &ref.WherePath); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	capRows, err := db.Query(`
		SELECT DISTINCT capability_path FROM spec_capabilities
		WHERE project_id = ?`, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to list capabilities for graph re-linking: %w", err)
	}
	defer capRows.Close()
	for capRows.Next() {
		var p string
		if err = capRows.Scan(&p); err != nil {
			return nil, err
		}
		refs = append(refs, graph.SpecCapabilityRef{CapabilityPath: p})
	}
	return refs, capRows.Err()
}

// PinMemory marks a memory as pinned (local context priority). Pinned memories
// surface first in session context so key decisions stay visible.
func PinMemory(db *sql.DB, projectID, id string) error {
	res, err := db.Exec("UPDATE memories SET pinned = 1 WHERE project_id = ? AND id = ? AND deleted_at IS NULL", projectID, id)
	if err != nil {
		return fmt.Errorf("failed to pin memory: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("memory %s not found in project", id)
	}
	return nil
}

// UnpinMemory clears the pinned flag on a memory.
func UnpinMemory(db *sql.DB, projectID, id string) error {
	res, err := db.Exec("UPDATE memories SET pinned = 0 WHERE project_id = ? AND id = ? AND deleted_at IS NULL", projectID, id)
	if err != nil {
		return fmt.Errorf("failed to unpin memory: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("memory %s not found in project", id)
	}
	return nil
}

type MemoryRelation struct {
	ID           string    `json:"id"`
	ProjectID    string    `json:"project_id"`
	SourceID     string    `json:"source_id"`
	TargetID     string    `json:"target_id"`
	RelationType string    `json:"relation_type"`
	Status       string    `json:"status,omitempty"`
	Score        float64   `json:"score,omitempty"`
	Reason       string    `json:"reason"`
	JudgedBy     string    `json:"judged_by"`
	CreatedAt    time.Time `json:"created_at"`
	SourceWhat   string    `json:"source_what,omitempty"`
	TargetWhat   string    `json:"target_what,omitempty"`
}

type MemoryReviewItem struct {
	Memory             *MemorySearchResult `json:"memory"`
	AgeDays            int                 `json:"age_days"`
	LastSeenDays       int                 `json:"last_seen_days,omitempty"`
	RevisionCount      int                 `json:"revision_count"`
	DuplicateCount     int                 `json:"duplicate_count"`
	RelationCount      int                 `json:"relation_count"`
	NeedsConsolidation bool                `json:"needs_consolidation"`
	NeedsReview        bool                `json:"needs_review,omitempty"`
	ReviewDueDays      int                 `json:"review_due_days,omitempty"`
	Reason             string              `json:"reason"`
}

type Session struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Goal      string    `json:"goal,omitempty"`
	Directory string    `json:"directory,omitempty"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
	Summary   string    `json:"summary,omitempty"`
	Status    string    `json:"status"`
}

// MemoryUpdate holds the fields an sv_mem_update call can change. A nil field
// keeps the stored value; a non-nil field overwrites it (including an empty
// string, which clears the field).
type MemoryUpdate struct {
	What        *string
	Why         *string
	Learned     *string
	WherePath   *string
	Impact      *string
	ErrorsFaced *string
	NextSteps   *string
}

// UpdateMemory partially updates an existing memory by ID. Identity fields
// (id, project_id, category, created_at, session_id, topic_key) are preserved;
// only the fields present in upd are changed. The revision counter advances,
// last_seen_at bumps so the next chunked Git sync re-writes the memory, and
// the normalized hash is recomputed from the final field values.
func UpdateMemory(db *sql.DB, projectID, id string, upd MemoryUpdate) (*Memory, error) {
	existing, err := GetMemory(db, projectID, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("memory %s not found in project", id)
	}

	what := existing.What
	if upd.What != nil {
		what = *upd.What
	}
	why := existing.Why
	if upd.Why != nil {
		why = *upd.Why
	}
	learned := existing.Learned
	if upd.Learned != nil {
		learned = *upd.Learned
	}
	wherePath := existing.WherePath
	if upd.WherePath != nil {
		wherePath = *upd.WherePath
	}
	impact := existing.Impact
	if upd.Impact != nil {
		impact = *upd.Impact
	}
	errorsFaced := existing.ErrorsFaced
	if upd.ErrorsFaced != nil {
		errorsFaced = *upd.ErrorsFaced
	}
	nextSteps := existing.NextSteps
	if upd.NextSteps != nil {
		nextSteps = *upd.NextSteps
	}

	if err = validateMemoryFields(what, why, learned, wherePath, impact, errorsFaced, nextSteps, "", ""); err != nil {
		return nil, err
	}

	what = security.SanitizeText(what)
	why = security.SanitizeText(why)
	wherePath = security.SanitizeText(wherePath)
	learned = security.SanitizeText(learned)
	impact = security.SanitizeText(impact)
	errorsFaced = security.SanitizeText(errorsFaced)
	nextSteps = security.SanitizeText(nextSteps)

	// Best-effort re-link of the memory to its code node (where_path may have
	// changed). Never fails the update if the graph is not built or the path is
	// unknown.
	defer func() {
		_ = graph.EnsureMemoryRationaleEdge(db, projectID, graph.MemoryRationaleRef{
			ID:        id,
			Category:  existing.Category,
			What:      what,
			WherePath: wherePath,
		})
	}()

	now := time.Now()
	revision := existing.RevisionCount + 1
	hash := computeHash(what, why, learned, wherePath)

	_, err = db.Exec(`
		UPDATE memories SET
			what = ?, why = ?, where_path = ?, learned = ?,
			impact = ?, errors_faced = ?, next_steps = ?,
			revision_count = ?, normalized_hash = ?, last_seen_at = ?
		WHERE project_id = ? AND id = ? AND deleted_at IS NULL`,
		what, why, wherePath, learned, impact, errorsFaced, nextSteps,
		revision, hash, now, projectID, id)
	if err != nil {
		return nil, fmt.Errorf("failed to update memory: %w", err)
	}

	return GetMemory(db, projectID, id)
}

func computeHash(what, why, learned, wherePath string) string {
	data := what + "\x00" + why + "\x00" + learned + "\x00" + wherePath
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])
}

func SaveMemory(db *sql.DB, mem *Memory) (*Memory, error) {
	if mem.ProjectID == "" {
		return nil, errors.New("memory ProjectID cannot be empty")
	}
	if err := validateMemoryFields(mem.What, mem.Why, mem.Learned, mem.WherePath, mem.Impact, mem.ErrorsFaced, mem.NextSteps, mem.TopicKey, mem.SessionID); err != nil {
		return nil, err
	}

	mem.What = security.SanitizeText(mem.What)
	mem.Why = security.SanitizeText(mem.Why)
	mem.WherePath = security.SanitizeText(mem.WherePath)
	mem.Learned = security.SanitizeText(mem.Learned)
	mem.GitBranch = security.SanitizeText(mem.GitBranch)
	mem.GitCommit = security.SanitizeText(mem.GitCommit)
	mem.Author = security.SanitizeText(mem.Author)
	mem.Impact = security.SanitizeText(mem.Impact)
	mem.ErrorsFaced = security.SanitizeText(mem.ErrorsFaced)
	mem.NextSteps = security.SanitizeText(mem.NextSteps)

	// Best-effort: link the memory to its code node in the structural graph via
	// a rationale_for edge. Runs on every save path (new, topic upsert, dedup)
	// and never fails the save if the graph is not built or the path is unknown.
	defer func() {
		_ = graph.EnsureMemoryRationaleEdge(db, mem.ProjectID, graph.MemoryRationaleRef{
			ID:        mem.ID,
			Category:  mem.Category,
			What:      mem.What,
			WherePath: mem.WherePath,
		})
	}()

	now := time.Now()
	mem.NormalizedHash = computeHash(mem.What, mem.Why, mem.Learned, mem.WherePath)
	if mem.ReviewAfter.IsZero() {
		mem.ReviewAfter = now.Add(decayReviewAfter(mem.Category))
	}

	// The dedup and topic-key upsert each pair a SELECT with a write. Without a
	// transaction two concurrent SaveMemory calls can interleave those steps and
	// create two rows for the same topic_key (or double-increment a duplicate).
	// Running the whole save inside one transaction on the single writer
	// connection serializes the check-and-write pairs.
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if mem.TopicKey != "" {
		var existingID string
		var revCount int
		var existingCreatedAtStr string
		err = tx.QueryRow(
			"SELECT id, revision_count, created_at FROM memories WHERE project_id = ? AND topic_key = ? AND deleted_at IS NULL ORDER BY created_at DESC LIMIT 1",
			mem.ProjectID, mem.TopicKey,
		).Scan(&existingID, &revCount, &existingCreatedAtStr)
		if err == nil {
			mem.ID = existingID
			mem.RevisionCount = revCount + 1
			// Preserve the original creation timestamp: a topic-key revision
			// must not drift the memory's creation date forward (UpdateMemory
			// and compaction both rely on created_at staying stable).
			if mem.CreatedAt.IsZero() {
				if t, parseErr := parseTime(existingCreatedAtStr); parseErr == nil {
					mem.CreatedAt = t
				} else {
					mem.CreatedAt = now
				}
			}
			query := `
			UPDATE memories SET
				category = ?, what = ?, why = ?, where_path = ?, learned = ?,
				git_branch = ?, git_commit = ?, author = ?, impact = ?,
				errors_faced = ?, next_steps = ?, session_id = ?,
				topic_key = ?, revision_count = ?, normalized_hash = ?,
				last_seen_at = ?, review_after = ?, created_at = ?, deleted_at = NULL
			WHERE id = ?`
			if _, uErr := tx.Exec(query,
				mem.Category, mem.What, mem.Why, mem.WherePath, mem.Learned,
				mem.GitBranch, mem.GitCommit, mem.Author, mem.Impact,
				mem.ErrorsFaced, mem.NextSteps, mem.SessionID,
				mem.TopicKey, mem.RevisionCount, mem.NormalizedHash,
				now, mem.ReviewAfter, mem.CreatedAt, mem.ID); uErr != nil {
				return nil, fmt.Errorf("failed to update memory via topic_key: %w", uErr)
			}
			if cErr := tx.Commit(); cErr != nil {
				return nil, fmt.Errorf("failed to commit topic-key upsert: %w", cErr)
			}
			return mem, nil
		}
	}

	if mem.TopicKey == "" {
		var existingID string
		var dupCount int
		// Compare against a Go-computed cutoff instead of SQLite's
		// datetime('now') (which is UTC): created_at is stored with the local
		// offset, so the two formats never compared correctly for zones west
		// of UTC. Binding the cutoff as a time.Time keeps both sides in the
		// same RFC3339Nano format.
		cutoff := time.Now().Add(-24 * time.Hour)
		err = tx.QueryRow(
			"SELECT id, duplicate_count FROM memories WHERE project_id = ? AND normalized_hash = ? AND category = ? AND created_at > ?",
			mem.ProjectID, mem.NormalizedHash, mem.Category, cutoff,
		).Scan(&existingID, &dupCount)
		if err == nil {
			if _, dErr := tx.Exec("UPDATE memories SET duplicate_count = ?, last_seen_at = ? WHERE id = ?",
				dupCount+1, now, existingID); dErr != nil {
				return nil, fmt.Errorf("failed to update duplicate count: %w", dErr)
			}
			mem.ID = existingID
			mem.DuplicateCount = dupCount + 1
			mem.LastSeenAt = now
			if cErr := tx.Commit(); cErr != nil {
				return nil, fmt.Errorf("failed to commit duplicate update: %w", cErr)
			}
			return mem, nil
		}
	}

	if mem.ID == "" {
		mem.ID = newID()
	}
	if mem.CreatedAt.IsZero() {
		mem.CreatedAt = now
	}
	if mem.TopicKey != "" {
		mem.RevisionCount = 1
	}
	mem.DuplicateCount = 0
	if mem.LastSeenAt.IsZero() {
		mem.LastSeenAt = now
	}

	if _, iErr := tx.Exec(memoryInsertConflictQuery(), memoryInsertArgs(mem, mem.CreatedAt)...); iErr != nil {
		return nil, fmt.Errorf("failed to save memory: %w", iErr)
	}
	if cErr := tx.Commit(); cErr != nil {
		return nil, fmt.Errorf("failed to commit save: %w", cErr)
	}
	return mem, nil
}

func GetMemory(db *sql.DB, projectID, id string) (*Memory, error) {
	query := `
	SELECT id, project_id, category, what, why, where_path, learned,
		git_branch, git_commit, author, impact, errors_faced, next_steps,
		session_id, topic_key, revision_count, duplicate_count, last_seen_at, normalized_hash, review_after, pinned, created_at
	FROM memories WHERE project_id = ? AND id = ? AND deleted_at IS NULL`
	row := db.QueryRow(query, projectID, id)
	var mem Memory
	var createdAtStr string
	var lastSeenAtStr, reviewAfterStr sql.NullString
	var gitBranch, gitCommit, author, impact, errorsFaced, nextSteps, sessionID, topicKey sql.NullString
	var revisionCount, duplicateCount sql.NullInt64
	var pinned sql.NullInt64
	var normalizedHash sql.NullString
	err := row.Scan(&mem.ID, &mem.ProjectID, &mem.Category, &mem.What, &mem.Why, &mem.WherePath, &mem.Learned, &gitBranch, &gitCommit, &author, &impact, &errorsFaced, &nextSteps, &sessionID, &topicKey, &revisionCount, &duplicateCount, &lastSeenAtStr, &normalizedHash, &reviewAfterStr, &pinned, &createdAtStr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get memory: %w", err)
	}
	mem.GitBranch = gitBranch.String
	mem.GitCommit = gitCommit.String
	mem.Author = author.String
	mem.Impact = security.SanitizeText(impact.String)
	mem.ErrorsFaced = security.SanitizeText(errorsFaced.String)
	mem.NextSteps = security.SanitizeText(nextSteps.String)
	mem.SessionID = sessionID.String
	mem.TopicKey = topicKey.String
	mem.What = security.SanitizeText(mem.What)
	mem.Why = security.SanitizeText(mem.Why)
	mem.Learned = security.SanitizeText(mem.Learned)
	mem.WherePath = security.SanitizeText(mem.WherePath)
	if revisionCount.Valid {
		mem.RevisionCount = int(revisionCount.Int64)
	}
	if duplicateCount.Valid {
		mem.DuplicateCount = int(duplicateCount.Int64)
	}
	if pinned.Valid {
		mem.Pinned = pinned.Int64 == 1
	}
	mem.NormalizedHash = normalizedHash.String
	if lastSeenAtStr.Valid && lastSeenAtStr.String != "" {
		if t, err := parseTime(lastSeenAtStr.String); err == nil {
			mem.LastSeenAt = t
		}
	}
	if reviewAfterStr.Valid && reviewAfterStr.String != "" {
		if t, err := parseTime(reviewAfterStr.String); err == nil {
			mem.ReviewAfter = t
		}
	}
	mem.CreatedAt = parseTimeOrNow(createdAtStr)
	return &mem, nil
}
func DeleteSession(db *sql.DB, id string) error {
	var memCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM memories WHERE session_id=?", id).Scan(&memCount); err != nil {
		return fmt.Errorf("failed to check session memories: %w", err)
	}
	if memCount > 0 {
		return fmt.Errorf("session %s has %d associated memories — delete them first", id, memCount)
	}
	result, err := db.Exec("DELETE FROM sessions WHERE id=?", id)
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("session %s not found", id)
	}
	return nil
}

func DeleteProject(db *sql.DB, id string, hard bool) error {
	if hard {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		if _, err := tx.Exec("DELETE FROM memory_relations WHERE project_id=?", id); err != nil {
			return fmt.Errorf("failed to delete relations: %w", err)
		}
		if _, err := tx.Exec("DELETE FROM graph_edges WHERE project_id=?", id); err != nil {
			return fmt.Errorf("failed to delete graph edges: %w", err)
		}
		if _, err := tx.Exec("DELETE FROM graph_nodes WHERE project_id=?", id); err != nil {
			return fmt.Errorf("failed to delete graph nodes: %w", err)
		}
		if _, err := tx.Exec("DELETE FROM graph_files_meta WHERE project_id=?", id); err != nil {
			return fmt.Errorf("failed to delete file meta: %w", err)
		}
		if _, err := tx.Exec("DELETE FROM sessions WHERE project_id=?", id); err != nil {
			return fmt.Errorf("failed to delete sessions: %w", err)
		}
		if _, err := tx.Exec("DELETE FROM memories WHERE project_id=?", id); err != nil {
			return fmt.Errorf("failed to delete memories: %w", err)
		}
		if _, err := tx.Exec("DELETE FROM projects WHERE id=?", id); err != nil {
			return fmt.Errorf("failed to delete project: %w", err)
		}
		return tx.Commit()
	}

	// Soft delete: mark memories as deleted and clean the derived data
	// (sessions, relations, graph) in one transaction so a failure cannot leave
	// the project partially cleaned. The graph is rebuildable via sync, so it
	// is removed for consistency with the hard-delete path.
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	var n int
	if err := tx.QueryRow("SELECT COUNT(*) FROM projects WHERE id=?", id).Scan(&n); err != nil {
		return fmt.Errorf("failed to check project: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("project %s not found", id)
	}

	if _, err := tx.Exec("UPDATE memories SET deleted_at=? WHERE project_id=? AND deleted_at IS NULL", time.Now(), id); err != nil {
		return fmt.Errorf("failed to soft-delete memories: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM sessions WHERE project_id=?", id); err != nil {
		return fmt.Errorf("failed to delete sessions: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM memory_relations WHERE project_id=?", id); err != nil {
		return fmt.Errorf("failed to delete relations: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM graph_edges WHERE project_id=?", id); err != nil {
		return fmt.Errorf("failed to delete graph edges: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM graph_nodes WHERE project_id=?", id); err != nil {
		return fmt.Errorf("failed to delete graph nodes: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM graph_files_meta WHERE project_id=?", id); err != nil {
		return fmt.Errorf("failed to delete graph file metadata: %w", err)
	}
	return tx.Commit()
}

func DeleteMemory(db *sql.DB, projectID, id string, hard bool) error {
	if hard {
		result, err := db.Exec("DELETE FROM memories WHERE project_id = ? AND id = ?", projectID, id)
		if err != nil {
			return fmt.Errorf("failed to hard-delete memory: %w", err)
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			return fmt.Errorf("memory %s not found", id)
		}
	} else {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("failed to begin transaction: %w", err)
		}
		defer tx.Rollback()

		result, err := tx.Exec("UPDATE memories SET deleted_at = ? WHERE project_id = ? AND id = ? AND deleted_at IS NULL",
			time.Now(), projectID, id)
		if err != nil {
			return fmt.Errorf("failed to soft-delete memory: %w", err)
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			return fmt.Errorf("memory %s not found or already deleted", id)
		}

		// A soft delete must not leave relations pointing at a deleted memory:
		// the FK cascade only fires on a real DELETE.
		if _, err := tx.Exec("DELETE FROM memory_relations WHERE source_id = ? OR target_id = ?", id, id); err != nil {
			return fmt.Errorf("failed to clean relations: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit soft delete: %w", err)
		}
	}
	return nil
}

func CapturePassive(db *sql.DB, projectID, what, why, sessionID string) (*Memory, error) {
	learned := what
	mem := &Memory{
		ProjectID: projectID,
		Category:  "journal",
		What:      what,
		Why:       why,
		Learned:   learned,
		SessionID: sessionID,
		GitBranch: "",
		GitCommit: "",
	}
	return SaveMemory(db, mem)
}

func SuggestTopicKey(category, what string) string {
	var sb strings.Builder
	sb.Grow(len(category) + 1 + len(what))
	sb.WriteString(category)
	sb.WriteByte('/')
	for _, r := range strings.ToLower(what) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(r)
		} else if r == ' ' || r == '-' || r == '_' {
			sb.WriteByte('-')
		}
	}
	key := sb.String()
	for strings.Contains(key, "--") {
		key = strings.ReplaceAll(key, "--", "-")
	}
	key = strings.Trim(key, "-")
	if len(key) > 80 {
		key = key[:80]
	}
	key = strings.TrimRight(key, "-")
	return key
}
