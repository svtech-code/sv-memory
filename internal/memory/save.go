package memory

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/svtech-code/sv-memory/internal/security"
)

// prepareMemoryForSave validates and sanitizes a memory and stamps the derived
// fields (normalized hash, review deadline) shared by every save path. Shared by
// SaveMemory and the atomic spec commit so the two cannot drift on the rules.
func prepareMemoryForSave(mem *Memory, now time.Time) error {
	if mem.ProjectID == "" {
		return errors.New("memory ProjectID cannot be empty")
	}
	if err := validateMemoryFields(mem.What, mem.Why, mem.Learned, mem.WherePath, mem.Impact, mem.ErrorsFaced, mem.NextSteps, mem.TopicKey, mem.SessionID); err != nil {
		return err
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

	mem.NormalizedHash = computeHash(mem.What, mem.Why, mem.Learned, mem.WherePath)
	if mem.ReviewAfter.IsZero() {
		mem.ReviewAfter = now.Add(decayReviewAfter(mem.Category))
	}
	return nil
}

// saveMemoryInTx runs the full save logic (topic-key upsert, duplicate
// suppression, or fresh insert) inside an existing transaction. It is the
// shared body of SaveMemory and the atomic spec commit, which both want the
// three check-and-write pairs serialized on the single writer connection.
func saveMemoryInTx(tx *sql.Tx, mem *Memory, now time.Time) error {
	// Path 1: duplicate suppression (exact identical hash + category within 24h)
	if handled, err := bumpDuplicate(tx, mem, now); err != nil {
		return err
	} else if handled {
		return nil
	}

	// Path 2: topic-key upsert (same topic_key, evolved content)
	if handled, err := upsertByTopicKey(tx, mem, now); err != nil {
		return err
	} else if handled {
		return nil
	}

	// Path 3: fresh insert
	return insertMemory(tx, mem, now)
}

// upsertByTopicKey handles the topic-key upsert path within a transaction.
// Returns true if the memory was upserted and the transaction should be committed.
func upsertByTopicKey(tx *sql.Tx, mem *Memory, now time.Time) (bool, error) {
	if mem.TopicKey == "" {
		return false, nil
	}
	var existingID string
	var revCount int
	var existingCreatedAtStr string
	err := tx.QueryRow(
		"SELECT id, revision_count, created_at FROM memories WHERE project_id = ? AND topic_key = ? AND deleted_at IS NULL ORDER BY created_at DESC LIMIT 1",
		mem.ProjectID, mem.TopicKey,
	).Scan(&existingID, &revCount, &existingCreatedAtStr)
	if err != nil {
		return false, nil // not found, continue to next path
	}

	mem.ID = existingID
	mem.RevisionCount = revCount + 1
	// Preserve the original creation timestamp
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
		return false, fmt.Errorf("failed to update memory via topic_key: %w", uErr)
	}
	return true, nil
}

// bumpDuplicate handles the duplicate detection path within a transaction.
// Returns true if a duplicate was found and bumped, and the transaction should be committed.
func bumpDuplicate(tx *sql.Tx, mem *Memory, now time.Time) (bool, error) {
	var existingID string
	var dupCount int
	// Compare against a Go-computed cutoff instead of SQLite's
	// datetime('now') (which is UTC): created_at is stored with the local
	// offset, so the two formats never compared correctly for zones west
	// of UTC. Binding the cutoff as a time.Time keeps both sides in the
	// same RFC3339Nano format.
	cutoff := now.Add(-24 * time.Hour)
	err := tx.QueryRow(
		"SELECT id, duplicate_count FROM memories WHERE project_id = ? AND normalized_hash = ? AND category = ? AND created_at > ?",
		mem.ProjectID, mem.NormalizedHash, mem.Category, cutoff,
	).Scan(&existingID, &dupCount)
	if err != nil {
		return false, nil // not found, continue to insert path
	}

	if _, dErr := tx.Exec("UPDATE memories SET duplicate_count = ?, last_seen_at = ? WHERE id = ?",
		dupCount+1, now, existingID); dErr != nil {
		return false, fmt.Errorf("failed to update duplicate count: %w", dErr)
	}
	mem.ID = existingID
	mem.DuplicateCount = dupCount + 1
	mem.LastSeenAt = now
	return true, nil
}

// insertMemory handles the fresh insert path within a transaction.
func insertMemory(tx *sql.Tx, mem *Memory, now time.Time) error {
	if mem.ID == "" {
		mem.ID = newID()
	}
	// A caller-supplied ID must be path-safe: it becomes a chunk/vault file
	// name on sync/export, so refuse anything that could escape the store.
	if !validMemoryID(mem.ID) {
		return fmt.Errorf("failed to save memory: unsafe id %q", mem.ID)
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
		return fmt.Errorf("failed to save memory: %w", iErr)
	}
	return nil
}
