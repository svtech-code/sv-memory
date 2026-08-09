package memory

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/svtech-code/sv-memory/internal/security"
)

// newID returns a 16-character lowercase hex identifier (64 bits of entropy)
// derived from a random UUID with the hyphens stripped. Short 8-character
// prefixes (32 bits) are too prone to birthday collisions for a long-lived
// store: a collision would silently overwrite an unrelated memory via the
// INSERT ... ON CONFLICT(id) upsert used by SaveMemory. 16 hex chars matches
// the project ID convention.
func newID() string {
	return strings.ReplaceAll(uuid.New().String(), "-", "")[:16]
}

// maxPathFilterLen bounds the length of the where_path LIKE filter so a
// maliciously large value cannot build an unbounded LIKE pattern.
const maxPathFilterLen = 200

// sanitizePathFilter caps the input length and escapes SQL LIKE wildcards
// (% and _) plus the escape character itself. It lives in the memory layer
// (not the transport layer) so every caller of the search functions is
// protected regardless of entry point.
func sanitizePathFilter(input string) string {
	if len(input) > maxPathFilterLen {
		input = input[:maxPathFilterLen]
	}
	return security.SanitizeSQLitePathFilter(input)
}

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err == nil {
		return t, nil
	}
	t, err = time.Parse("2006-01-02 15:04:05", s)
	if err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("cannot parse time %q", s)
}

func nullString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func nullTime(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return t
}

// truncateText shortens s to maxChars (by runes to avoid splitting UTF-8
// sequences) and appends a notice when it was cut, keeping compact renders
// (Auto-Boot bundle, search expansion) token-efficient.
func truncateText(s string, maxChars int) string {
	if maxChars <= 0 || len([]rune(s)) <= maxChars {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxChars]) + fmt.Sprintf("... [truncated %d chars]", len(runes)-maxChars)
}

// sanitizeMemoryFields redacts secrets from every free-text field of a memory
// before it is persisted. Used by the import paths (ImportJSON, git-sync chunk
// import) that receive raw data from outside the normal sanitizing save path.
func sanitizeMemoryFields(mem *Memory) {
	mem.Category = security.SanitizeText(mem.Category)
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
	mem.SessionID = security.SanitizeText(mem.SessionID)
	mem.TopicKey = security.SanitizeText(mem.TopicKey)
}

// memoryInsertArgs returns the ordered argument list matching
// memoryInsertConflictQuery() column order. It must stay in sync with that
// query and the memories table columns.
func memoryInsertArgs(mem *Memory, createdAt time.Time) []interface{} {
	return []interface{}{
		mem.ID, mem.ProjectID, mem.Category, mem.What, mem.Why, mem.WherePath, mem.Learned,
		mem.GitBranch, mem.GitCommit, mem.Author, mem.Impact, mem.ErrorsFaced, mem.NextSteps,
		nullString(mem.SessionID), nullString(mem.TopicKey),
		mem.RevisionCount, mem.DuplicateCount,
		nullTime(mem.LastSeenAt), nullString(mem.NormalizedHash),
		nullTime(mem.ReviewAfter), mem.Pinned,
		createdAt,
	}
}

func memoryInsertConflictQuery() string {
	return `
	INSERT INTO memories (id, project_id, category, what, why, where_path, learned, git_branch, git_commit, author, impact, errors_faced, next_steps, session_id, topic_key, revision_count, duplicate_count, last_seen_at, normalized_hash, review_after, pinned, created_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		category = excluded.category,
		what = excluded.what,
		why = excluded.why,
		where_path = excluded.where_path,
		learned = excluded.learned,
		git_branch = excluded.git_branch,
		git_commit = excluded.git_commit,
		author = excluded.author,
		impact = excluded.impact,
		errors_faced = excluded.errors_faced,
		next_steps = excluded.next_steps,
		session_id = excluded.session_id,
		topic_key = excluded.topic_key,
		revision_count = excluded.revision_count,
		duplicate_count = excluded.duplicate_count,
		last_seen_at = excluded.last_seen_at,
		normalized_hash = excluded.normalized_hash,
		review_after = excluded.review_after,
		pinned = excluded.pinned,
		created_at = excluded.created_at;`
}
