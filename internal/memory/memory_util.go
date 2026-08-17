package memory

import (
	"fmt"
	"os"
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

// parseTimeOrNow parses s with parseTime and falls back to time.Now() when the
// stored value cannot be parsed, matching the lenient display behavior used by
// the memory scan helpers.
func parseTimeOrNow(s string) time.Time {
	if t, err := parseTime(s); err == nil {
		return t
	}
	return time.Now()
}

// maxFieldChars is the shared field-length cap for the free-text memory fields,
// kept in one place so SaveMemory and UpdateMemory cannot drift apart on the
// limits they enforce.
const maxFieldChars = 4000

// validateMemoryFields enforces the per-field length caps shared by the
// SaveMemory and UpdateMemory paths. topicKey and sessionID carry their own
// (smaller) limits because they are identifiers, not prose.
func validateMemoryFields(what, why, learned, wherePath, impact, errorsFaced, nextSteps, topicKey, sessionID string) error {
	if len(what) > 1000 {
		return fmt.Errorf("field 'what' exceeds maximum length of 1000 characters")
	}
	if len(why) > maxFieldChars {
		return fmt.Errorf("field 'why' exceeds maximum length of %d characters", maxFieldChars)
	}
	if len(learned) > maxFieldChars {
		return fmt.Errorf("field 'learned' exceeds maximum length of %d characters", maxFieldChars)
	}
	if len(wherePath) > 1000 {
		return fmt.Errorf("field 'where_path' exceeds maximum length of 1000 characters")
	}
	if len(impact) > maxFieldChars {
		return fmt.Errorf("field 'impact' exceeds maximum length of %d characters", maxFieldChars)
	}
	if len(errorsFaced) > maxFieldChars {
		return fmt.Errorf("field 'errors_faced' exceeds maximum length of %d characters", maxFieldChars)
	}
	if len(nextSteps) > maxFieldChars {
		return fmt.Errorf("field 'next_steps' exceeds maximum length of %d characters", maxFieldChars)
	}
	if len(topicKey) > 256 {
		return fmt.Errorf("field 'topic_key' exceeds maximum length of 256 characters")
	}
	if len(sessionID) > 64 {
		return fmt.Errorf("field 'session_id' exceeds maximum length of 64 characters")
	}
	return nil
}

// validateChangeFields enforces the shared field-length caps of the spec-driven
// change lifecycle (CreateChange and UpdateChange enforce the same limits).
func validateChangeFields(title, what, goal, wherePath, capabilityPath, design, tasks string) error {
	if len(title) > 1000 {
		return fmt.Errorf("field 'title' exceeds maximum length of 1000 characters")
	}
	for name, v := range map[string]string{
		"what": what, "goal": goal, "where_path": wherePath,
		"capability_path": capabilityPath, "design": design, "tasks": tasks,
	} {
		if len(v) > maxChangeFieldChars {
			return fmt.Errorf("field '%s' exceeds maximum length of %d characters", name, maxChangeFieldChars)
		}
	}
	return nil
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

// TruncateText shortens s to maxChars (by runes to avoid splitting UTF-8
// sequences) and appends a notice when it was cut, keeping compact renders
// (Auto-Boot bundle, search expansion, MCP responses) token-efficient.
func TruncateText(s string, maxChars int) string {
	if maxChars <= 0 || len([]rune(s)) <= maxChars {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxChars]) + fmt.Sprintf("... [truncated %d chars]", len(runes)-maxChars)
}

// atomicWriteFile writes data to path atomically: it writes to a temp sibling
// first, then renames it into place, removing the temp on failure. Readers
// never observe a partially written file. The single implementation behind the
// JSON export, git-sync chunk/monolith writes, and the spec mirror.
func atomicWriteFile(path string, data []byte) error {
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp file %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename temp file %s: %w", path, err)
	}
	return nil
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

// memoryColumns is the shared SELECT column list for queries returning a full
// Memory row (without review_after/pinned, which only GetMemory adds). Keep in
// sync with scanMemories and the memories table schema.
const memoryColumns = `id, project_id, category, what, why, where_path, learned,
		git_branch, git_commit, author, impact, errors_faced, next_steps,
		session_id, topic_key, revision_count, duplicate_count, last_seen_at, normalized_hash, created_at`

// compactColumns is the shared SELECT column list for the compact
// (MemorySearchResult) queries: session, pinned, and scoped search.
const compactColumns = `id, category, what,
		topic_key, revision_count, duplicate_count, created_at`

// bm25Weights is the FTS5 BM25 column weight tuple shared by the scoring and
// ordering expressions so the SELECT and ORDER BY cannot drift apart.
const bm25Weights = "10.0, 5.0, 2.0"

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
