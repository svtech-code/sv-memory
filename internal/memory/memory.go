package memory

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/svtech/sv-memory/internal/security"
)

// Package-level cache so SyncToGit can skip redundant writes when nothing
// changed in SQLite and the json file on disk is already up-to-date. Held
// across the MCP server lifetime (stdio — one process).
var (
	syncCacheMu   sync.Mutex
	lastWriteInfo = map[string]syncCacheEntry{} // keyed by projectID
)

type syncCacheEntry struct {
	memoryCount int
	fileMtim    time.Time // mtime of the JSON file at last write
}

// Memory represents a recorded design decision, bugfix, or coding standard.
type Memory struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"project_id"`
	Category       string    `json:"category"` // 'bugfix' | 'architecture' | 'standard' | 'decision' | 'journal' | 'postmortem' | 'discussion' | 'idea' | 'qa'
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
	CreatedAt      time.Time `json:"created_at"`
}

// Session represents a coding session that groups multiple memories together.
// Sessions enable context recovery after compaction via sv_mem_context.
type Session struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Goal      string    `json:"goal,omitempty"`
	Directory string    `json:"directory,omitempty"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
	Summary   string    `json:"summary,omitempty"`
	Status    string    `json:"status"` // 'active' | 'completed'
}

// StartSession creates a new active session for the given project.
func StartSession(db *sql.DB, projectID, goal, directory string) (*Session, error) {
	id := uuid.New().String()[:8]
	now := time.Now()
	_, err := db.Exec(
		"INSERT INTO sessions (id, project_id, goal, directory, started_at, status) VALUES (?, ?, ?, ?, ?, 'active')",
		id, projectID, goal, directory, now)
	if err != nil {
		return nil, fmt.Errorf("failed to start session: %w", err)
	}
	return &Session{
		ID:        id,
		ProjectID: projectID,
		Goal:      goal,
		Directory: directory,
		StartedAt: now,
		Status:    "active",
	}, nil
}

// EndSession marks a session as completed with an optional summary.
func EndSession(db *sql.DB, id, summary string) error {
	result, err := db.Exec(
		"UPDATE sessions SET ended_at = ?, summary = ?, status = 'completed' WHERE id = ? AND status = 'active'",
		time.Now(), summary, id)
	if err != nil {
		return fmt.Errorf("failed to end session: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("session %s not found or already completed", id)
	}
	return nil
}

// SaveSessionSummary updates the goal and summary fields of a session.
func SaveSessionSummary(db *sql.DB, id, goal, discoveries, accomplished, nextSteps, files string) error {
	summary := fmt.Sprintf("Goal: %s\nDiscoveries: %s\nAccomplished: %s\nNext Steps: %s\nFiles: %s",
		goal, discoveries, accomplished, nextSteps, files)
	result, err := db.Exec("UPDATE sessions SET goal = ?, summary = ? WHERE id = ?", goal, summary, id)
	if err != nil {
		return fmt.Errorf("failed to save session summary: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("session %s not found", id)
	}
	return nil
}

// GetSession retrieves a single session by ID.
func GetSession(db *sql.DB, id string) (*Session, error) {
	row := db.QueryRow("SELECT id, project_id, goal, directory, started_at, ended_at, summary, status FROM sessions WHERE id = ?", id)
	var s Session
	var startedAtStr, endedAtStr string
	var goal, directory, summary sql.NullString
	err := row.Scan(&s.ID, &s.ProjectID, &goal, &directory, &startedAtStr, &endedAtStr, &summary, &s.Status)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}
	s.Goal = goal.String
	s.Directory = directory.String
	s.Summary = summary.String
	if t, err := parseTime(startedAtStr); err == nil {
		s.StartedAt = t
	}
	if endedAtStr != "" {
		if t, err := parseTime(endedAtStr); err == nil {
			s.EndedAt = t
		}
	}
	return &s, nil
}

// GetActiveSession returns the currently active session (status='active') for a
// project, or nil if none exists. There should be at most one active session.
func GetActiveSession(db *sql.DB, projectID string) (*Session, error) {
	row := db.QueryRow("SELECT id, project_id, goal, directory, started_at, ended_at, summary, status FROM sessions WHERE project_id = ? AND status = 'active' ORDER BY started_at DESC LIMIT 1", projectID)
	var s Session
	var startedAtStr, endedAtStr string
	var goal, directory, summary sql.NullString
	err := row.Scan(&s.ID, &s.ProjectID, &goal, &directory, &startedAtStr, &endedAtStr, &summary, &s.Status)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get active session: %w", err)
	}
	s.Goal = goal.String
	s.Directory = directory.String
	s.Summary = summary.String
	if t, err := parseTime(startedAtStr); err == nil {
		s.StartedAt = t
	}
	return &s, nil
}

// GetLastSession returns the most recently completed session for the project.
func GetLastSession(db *sql.DB, projectID string) (*Session, error) {
	row := db.QueryRow("SELECT id, project_id, goal, directory, started_at, ended_at, summary, status FROM sessions WHERE project_id = ? AND status = 'completed' ORDER BY ended_at DESC LIMIT 1", projectID)
	var s Session
	var startedAtStr, endedAtStr string
	var goal, directory, summary sql.NullString
	err := row.Scan(&s.ID, &s.ProjectID, &goal, &directory, &startedAtStr, &endedAtStr, &summary, &s.Status)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get last session: %w", err)
	}
	s.Goal = goal.String
	s.Directory = directory.String
	s.Summary = summary.String
	if t, err := parseTime(startedAtStr); err == nil {
		s.StartedAt = t
	}
	if endedAtStr != "" {
		if t, err := parseTime(endedAtStr); err == nil {
			s.EndedAt = t
		}
	}
	return &s, nil
}

// GetSessionContext returns a formatted Markdown summary of the last completed
// session and its associated memories. This is designed for post-compaction
// context recovery: the agent calls sv_mem_context to quickly resume work.
func GetSessionContext(db *sql.DB, projectID string) (string, error) {
	session, err := GetLastSession(db, projectID)
	if err != nil {
		return "", err
	}
	if session == nil {
		// Fall back to most recent memories if no session exists
		mems, err := SearchMemories(db, projectID, "", "", 5)
		if err != nil {
			return "", err
		}
		if len(mems) == 0 {
			return "No previous session context found for this project.", nil
		}
		var sb strings.Builder
		sb.WriteString("No recorded sessions. Most recent memories:\n\n")
		for _, m := range mems {
			sb.WriteString(fmt.Sprintf("- [%s] **%s** (ID: %s, %s)\n",
				strings.ToUpper(m.Category), m.What, m.ID, m.CreatedAt.Format("2006-01-02")))
		}
		return sb.String(), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Previous Session Context\n\n"))
	sb.WriteString(fmt.Sprintf("**Session ID:** %s\n", session.ID))
	sb.WriteString(fmt.Sprintf("**Started:** %s\n", session.StartedAt.Format("2006-01-02 15:04")))
	if !session.EndedAt.IsZero() {
		sb.WriteString(fmt.Sprintf("**Ended:** %s\n", session.EndedAt.Format("2006-01-02 15:04")))
	}
	if session.Goal != "" {
		sb.WriteString(fmt.Sprintf("**Goal:** %s\n", session.Goal))
	}
	if session.Summary != "" {
		sb.WriteString(fmt.Sprintf("**Summary:** %s\n", session.Summary))
	}

	// Fetch memories from that session
	mems, err := SearchMemoriesBySession(db, projectID, session.ID, 10)
	if err != nil {
		return "", err
	}
	if len(mems) > 0 {
		sb.WriteString(fmt.Sprintf("\n**Memories saved (%d):**\n", len(mems)))
		for _, m := range mems {
			sb.WriteString(fmt.Sprintf("- [%s] **%s** (ID: %s)\n",
				strings.ToUpper(m.Category), m.What, m.ID))
		}
	}

	return sb.String(), nil
}

// SearchMemoriesBySession returns memories associated with a specific session.
func SearchMemoriesBySession(db *sql.DB, projectID, sessionID string, limit int) ([]*Memory, error) {
	query := `
	SELECT id, project_id, category, what, why, where_path, learned,
		git_branch, git_commit, author, impact, errors_faced, next_steps,
		session_id, topic_key, revision_count, duplicate_count, last_seen_at, normalized_hash, created_at
	FROM memories WHERE project_id = ? AND session_id = ?
	ORDER BY created_at ASC`
	var args []interface{}
	args = append(args, projectID, sessionID)
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to search memories by session: %w", err)
	}
	defer rows.Close()
	return scanMemories(rows)
}

// computeHash returns a SHA256 hex digest of the concatenated what/why/learned fields.
func computeHash(what, why, learned string) string {
	data := what + "\x00" + why + "\x00" + learned
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])
}

// SaveMemory inserts or updates a memory record in the database.
// It implements two economisation strategies:
//  1. Topic-key upsert: if topic_key is set and a record with the same
//     project_id + topic_key exists, that record is updated (revision_count++)
//     instead of creating a new one.
//  2. Rolling-window dedup: if topic_key is NOT set and an identical
//     normalized_hash exists for the same project + category within the last
//     24 hours, duplicate_count is incremented and last_seen_at is bumped
//     without creating a new row.
// Returns the saved/updated memory so callers can inspect resulting state.
func SaveMemory(db *sql.DB, mem *Memory) (*Memory, error) {
	if mem.ProjectID == "" {
		return nil, errors.New("memory ProjectID cannot be empty")
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

	now := time.Now()
	mem.NormalizedHash = computeHash(mem.What, mem.Why, mem.Learned)

	// Strategy 1 — topic_key upsert: update existing record if same project + topic
	if mem.TopicKey != "" {
		var existingID string
		var revCount int
		err := db.QueryRow(
			"SELECT id, revision_count FROM memories WHERE project_id = ? AND topic_key = ?",
			mem.ProjectID, mem.TopicKey,
		).Scan(&existingID, &revCount)
		if err == nil {
			mem.ID = existingID
			mem.RevisionCount = revCount + 1
			if mem.CreatedAt.IsZero() {
				mem.CreatedAt = now
			}
			query := `
			UPDATE memories SET
				category = ?, what = ?, why = ?, where_path = ?, learned = ?,
				git_branch = ?, git_commit = ?, author = ?, impact = ?,
				errors_faced = ?, next_steps = ?, session_id = ?,
				topic_key = ?, revision_count = ?, normalized_hash = ?,
				last_seen_at = ?, created_at = ?
			WHERE id = ?`
			_, err := db.Exec(query,
				mem.Category, mem.What, mem.Why, mem.WherePath, mem.Learned,
				mem.GitBranch, mem.GitCommit, mem.Author, mem.Impact,
				mem.ErrorsFaced, mem.NextSteps, mem.SessionID,
				mem.TopicKey, mem.RevisionCount, mem.NormalizedHash,
				now, mem.CreatedAt, mem.ID)
			if err != nil {
				return nil, fmt.Errorf("failed to update memory via topic_key: %w", err)
			}
			return mem, nil
		}
		// Not found: fall through to insert
	}

	// Strategy 2 — rolling-window dedup: same content hash within 24h
	if mem.TopicKey == "" {
		var existingID string
		var dupCount int
		err := db.QueryRow(
			"SELECT id, duplicate_count FROM memories WHERE project_id = ? AND normalized_hash = ? AND category = ? AND created_at > datetime('now', '-24 hours')",
			mem.ProjectID, mem.NormalizedHash, mem.Category,
		).Scan(&existingID, &dupCount)
		if err == nil {
			_, err := db.Exec("UPDATE memories SET duplicate_count = ?, last_seen_at = ? WHERE id = ?",
				dupCount+1, now, existingID)
			if err != nil {
				return nil, fmt.Errorf("failed to update duplicate count: %w", err)
			}
			mem.ID = existingID
			mem.DuplicateCount = dupCount + 1
			mem.LastSeenAt = now
			return mem, nil
		}
		// Not found: fall through to insert
	}

	// New memory insert
	if mem.ID == "" {
		mem.ID = uuid.New().String()[:8]
	}
	if mem.CreatedAt.IsZero() {
		mem.CreatedAt = now
	}
	if mem.TopicKey != "" {
		mem.RevisionCount = 1
	}
	mem.DuplicateCount = 0

	query := `
	INSERT INTO memories (id, project_id, category, what, why, where_path, learned, git_branch, git_commit, author, impact, errors_faced, next_steps, session_id, topic_key, revision_count, duplicate_count, last_seen_at, normalized_hash, created_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
		created_at = excluded.created_at;`
	_, err := db.Exec(query,
		mem.ID, mem.ProjectID, mem.Category, mem.What, mem.Why, mem.WherePath, mem.Learned,
		mem.GitBranch, mem.GitCommit, mem.Author, mem.Impact, mem.ErrorsFaced, mem.NextSteps,
		mem.SessionID, mem.TopicKey, mem.RevisionCount, mem.DuplicateCount, mem.LastSeenAt,
		mem.NormalizedHash, mem.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to save memory: %w", err)
	}
	return mem, nil
}

// SearchMemories queries the database using FTS5 full-text search.
func SearchMemories(db *sql.DB, projectID string, searchTerm string, category string, limit int) ([]*Memory, error) {
	var query string
	var args []interface{}

	if searchTerm == "" {
		query = `
		SELECT id, project_id, category, what, why, where_path, learned,
			git_branch, git_commit, author, impact, errors_faced, next_steps,
			session_id, topic_key, revision_count, duplicate_count, last_seen_at, normalized_hash, created_at
		FROM memories
		WHERE project_id = ?
		`
		args = append(args, projectID)
		if category != "" {
			query += " AND category = ?"
			args = append(args, category)
		}
		query += " ORDER BY created_at DESC"
	} else {
		query = `
		SELECT m.id, m.project_id, m.category, m.what, m.why, m.where_path, m.learned,
			m.git_branch, m.git_commit, m.author, m.impact, m.errors_faced, m.next_steps,
			m.session_id, m.topic_key, m.revision_count, m.duplicate_count, m.last_seen_at, m.normalized_hash, m.created_at
		FROM memories m
		JOIN memories_fts f ON m.rowid = f.rowid
		WHERE m.project_id = ? AND memories_fts MATCH ?
		`
		args = append(args, projectID, searchTerm)
		if category != "" {
			query += " AND m.category = ?"
			args = append(args, category)
		}
		query += " ORDER BY rank"
	}

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed searching memories: %w", err)
	}
	defer rows.Close()

	var memories []*Memory
	for rows.Next() {
		var mem Memory
		var createdAtStr string
		var lastSeenAtStr string
		var gitBranch, gitCommit, author, impact, errorsFaced, nextSteps, sessionID, topicKey sql.NullString
		var revisionCount, duplicateCount sql.NullInt64
		var normalizedHash sql.NullString
		err := rows.Scan(&mem.ID, &mem.ProjectID, &mem.Category, &mem.What, &mem.Why, &mem.WherePath, &mem.Learned, &gitBranch, &gitCommit, &author, &impact, &errorsFaced, &nextSteps, &sessionID, &topicKey, &revisionCount, &duplicateCount, &lastSeenAtStr, &normalizedHash, &createdAtStr)
		if err != nil {
			return nil, fmt.Errorf("failed scanning memory row: %w", err)
		}
		mem.GitBranch = gitBranch.String
		mem.GitCommit = gitCommit.String
		mem.Author = author.String
		mem.Impact = impact.String
		mem.ErrorsFaced = errorsFaced.String
		mem.NextSteps = nextSteps.String
		mem.SessionID = sessionID.String
		mem.TopicKey = topicKey.String
		if revisionCount.Valid {
			mem.RevisionCount = int(revisionCount.Int64)
		}
		if duplicateCount.Valid {
			mem.DuplicateCount = int(duplicateCount.Int64)
		}
		mem.NormalizedHash = normalizedHash.String
		if lastSeenAtStr != "" {
			if t, err := parseTime(lastSeenAtStr); err == nil {
				mem.LastSeenAt = t
			}
		}
		if t, err := parseTime(createdAtStr); err == nil {
			mem.CreatedAt = t
		} else {
			mem.CreatedAt = time.Now()
		}
		memories = append(memories, &mem)
	}

	return memories, nil
}

// parseTime tries RFC3339 first, then the SQLite default datetime format.
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

// countMemories returns the total number of memories for a given project.
func countMemories(db *sql.DB, projectID string) (int, error) {
	var n int
	err := db.QueryRow("SELECT COUNT(*) FROM memories WHERE project_id = ?", projectID).Scan(&n)
	return n, err
}

// SyncToGit serializes all project memories to a local file in `.sv-memory/memories.json`.
// It uses an atomic write (tmp file + rename) so concurrent readers never see a partial file.
// When nothing changed since the last write (same memory count, same file mtime) it skips
// the write entirely to avoid unnecessary I/O and git diffs.
func SyncToGit(db *sql.DB, projectID string, projPath string) error {
	syncDir := filepath.Join(projPath, ".sv-memory")
	syncFile := filepath.Join(syncDir, "memories.json")

	// Quick check: count current memories and compare with cache.
	count, err := countMemories(db, projectID)
	if err != nil {
		return err
	}

	syncCacheMu.Lock()
	info := lastWriteInfo[projectID]
	currentMtim := time.Time{}
	if fi, statErr := os.Stat(syncFile); statErr == nil {
		currentMtim = fi.ModTime()
	}
	if count == info.memoryCount && currentMtim.Equal(info.fileMtim) {
		syncCacheMu.Unlock()
		return nil // nothing changed — skip write
	}
	syncCacheMu.Unlock()

	// Load all memories from SQLite.
	memories, err := SearchMemories(db, projectID, "", "", 0)
	if err != nil {
		return err
	}

	// Create directory if missing.
	if err := os.MkdirAll(syncDir, 0755); err != nil {
		return fmt.Errorf("failed to create sync directory: %w", err)
	}

	data, err := json.MarshalIndent(memories, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal memories JSON: %w", err)
	}

	// Atomic write: write to tmp file then rename (atomic on POSIX / macOS).
	tmpFile := syncFile + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp memories file: %w", err)
	}
	if err := os.Rename(tmpFile, syncFile); err != nil {
		// Clean up temp file on rename failure.
		os.Remove(tmpFile)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	// Update cache with mtime from the file we just wrote.
	if fi, statErr := os.Stat(syncFile); statErr == nil {
		syncCacheMu.Lock()
		lastWriteInfo[projectID] = syncCacheEntry{
			memoryCount: count,
			fileMtim:    fi.ModTime(),
		}
		syncCacheMu.Unlock()
	} else {
		// Should not happen; purge cache so next call retries.
		syncCacheMu.Lock()
		delete(lastWriteInfo, projectID)
		syncCacheMu.Unlock()
	}

	return nil
}

// SyncFromGit imports memories from `.sv-memory/memories.json` if it exists.
func SyncFromGit(db *sql.DB, projectID string, projPath string) error {
	syncFile := filepath.Join(projPath, ".sv-memory", "memories.json")
	if _, err := os.Stat(syncFile); os.IsNotExist(err) {
		// Nothing to sync, that is normal
		return nil
	}

	data, err := os.ReadFile(syncFile)
	if err != nil {
		return fmt.Errorf("failed to read memories file %s: %w", syncFile, err)
	}

	var memories []*Memory
	if err := json.Unmarshal(data, &memories); err != nil {
		return fmt.Errorf("failed to parse memories JSON: %w", err)
	}

	// Begin transaction
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `
	INSERT INTO memories (id, project_id, category, what, why, where_path, learned, git_branch, git_commit, author, impact, errors_faced, next_steps, session_id, topic_key, revision_count, duplicate_count, last_seen_at, normalized_hash, created_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
		created_at = excluded.created_at;`
	stmt, err := tx.Prepare(query)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, mem := range memories {
		mem.ProjectID = projectID
		createdAt := mem.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now()
		}
		_, err := stmt.Exec(
			mem.ID, mem.ProjectID, mem.Category, mem.What, mem.Why, mem.WherePath, mem.Learned,
			mem.GitBranch, mem.GitCommit, mem.Author, mem.Impact, mem.ErrorsFaced, mem.NextSteps,
			nullString(mem.SessionID), nullString(mem.TopicKey),
			mem.RevisionCount, mem.DuplicateCount,
			nullTime(mem.LastSeenAt), nullString(mem.NormalizedHash),
			createdAt)
		if err != nil {
			return fmt.Errorf("failed to sync memory %s: %w", mem.ID, err)
		}
	}

	return tx.Commit()
}

// nullString returns a *string for SQL NULL handling: empty string → nil.
func nullString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// nullTime returns a *time.Time for SQL NULL handling: zero time → nil.
func nullTime(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return t
}

// GetMemory retrieves a single memory by its full ID (project_id + id).
// Returns nil without error when the memory is not found.
func GetMemory(db *sql.DB, projectID, id string) (*Memory, error) {
	query := `
	SELECT id, project_id, category, what, why, where_path, learned,
		git_branch, git_commit, author, impact, errors_faced, next_steps,
		session_id, topic_key, revision_count, duplicate_count, last_seen_at, normalized_hash, created_at
	FROM memories WHERE project_id = ? AND id = ?`
	row := db.QueryRow(query, projectID, id)
	var mem Memory
	var createdAtStr string
	var lastSeenAtStr string
	var gitBranch, gitCommit, author, impact, errorsFaced, nextSteps, sessionID, topicKey sql.NullString
	var revisionCount, duplicateCount sql.NullInt64
	var normalizedHash sql.NullString
	err := row.Scan(&mem.ID, &mem.ProjectID, &mem.Category, &mem.What, &mem.Why, &mem.WherePath, &mem.Learned, &gitBranch, &gitCommit, &author, &impact, &errorsFaced, &nextSteps, &sessionID, &topicKey, &revisionCount, &duplicateCount, &lastSeenAtStr, &normalizedHash, &createdAtStr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get memory: %w", err)
	}
	mem.GitBranch = gitBranch.String
	mem.GitCommit = gitCommit.String
	mem.Author = author.String
	mem.Impact = impact.String
	mem.ErrorsFaced = errorsFaced.String
	mem.NextSteps = nextSteps.String
	mem.SessionID = sessionID.String
	mem.TopicKey = topicKey.String
	if revisionCount.Valid {
		mem.RevisionCount = int(revisionCount.Int64)
	}
	if duplicateCount.Valid {
		mem.DuplicateCount = int(duplicateCount.Int64)
	}
	mem.NormalizedHash = normalizedHash.String
	if lastSeenAtStr != "" {
		if t, err := parseTime(lastSeenAtStr); err == nil {
			mem.LastSeenAt = t
		}
	}
	if t, err := parseTime(createdAtStr); err == nil {
		mem.CreatedAt = t
	} else {
		mem.CreatedAt = time.Now()
	}
	return &mem, nil
}

// GetTimeline returns N memories created before and N memories after the
// given observation id, ordered chronologically. This is the second layer
// of the progressive disclosure pattern (context around a specific memory).
func GetTimeline(db *sql.DB, projectID, obsID string, before, after int) (previous, next []*Memory, err error) {
	// Find the created_at of the target observation
	var targetTime time.Time
	var targetCreatedAt string
	err = db.QueryRow("SELECT created_at FROM memories WHERE project_id = ? AND id = ?", projectID, obsID).Scan(&targetCreatedAt)
	if err != nil {
		return nil, nil, fmt.Errorf("observation not found: %w", err)
	}
	targetTime, err = parseTime(targetCreatedAt)
	if err != nil {
		return nil, nil, err
	}

	// N memories strictly before targetTime
	if before > 0 {
		rows, qErr := db.Query(`
		SELECT id, project_id, category, what, why, where_path, learned,
			git_branch, git_commit, author, impact, errors_faced, next_steps,
			session_id, topic_key, revision_count, duplicate_count, last_seen_at, normalized_hash, created_at
		FROM memories WHERE project_id = ? AND created_at < ? AND id != ?
		ORDER BY created_at DESC LIMIT ?`, projectID, targetTime.Format("2006-01-02 15:04:05"), obsID, before)
		if qErr != nil {
			return nil, nil, qErr
		}
		defer rows.Close()
		previous, qErr = scanMemories(rows)
		if qErr != nil {
			return nil, nil, qErr
		}
		// Reverse to get ascending order
		for i, j := 0, len(previous)-1; i < j; i, j = i+1, j-1 {
			previous[i], previous[j] = previous[j], previous[i]
		}
	}

	// N memories strictly after targetTime
	if after > 0 {
		rows, qErr := db.Query(`
		SELECT id, project_id, category, what, why, where_path, learned,
			git_branch, git_commit, author, impact, errors_faced, next_steps,
			session_id, topic_key, revision_count, duplicate_count, last_seen_at, normalized_hash, created_at
		FROM memories WHERE project_id = ? AND created_at > ? AND id != ?
		ORDER BY created_at ASC LIMIT ?`, projectID, targetTime.Format("2006-01-02 15:04:05"), obsID, after)
		if qErr != nil {
			return nil, nil, qErr
		}
		defer rows.Close()
		next, qErr = scanMemories(rows)
		if qErr != nil {
			return nil, nil, qErr
		}
	}

	return previous, next, nil
}

// scanMemories is a helper that scans all rows from a query result into []*Memory.
func scanMemories(rows *sql.Rows) ([]*Memory, error) {
	var memories []*Memory
	for rows.Next() {
		var mem Memory
		var createdAtStr string
		var lastSeenAtStr string
		var gitBranch, gitCommit, author, impact, errorsFaced, nextSteps, sessionID, topicKey sql.NullString
		var revisionCount, duplicateCount sql.NullInt64
		var normalizedHash sql.NullString
		err := rows.Scan(&mem.ID, &mem.ProjectID, &mem.Category, &mem.What, &mem.Why, &mem.WherePath, &mem.Learned, &gitBranch, &gitCommit, &author, &impact, &errorsFaced, &nextSteps, &sessionID, &topicKey, &revisionCount, &duplicateCount, &lastSeenAtStr, &normalizedHash, &createdAtStr)
		if err != nil {
			return nil, fmt.Errorf("failed scanning memory row: %w", err)
		}
		mem.GitBranch = gitBranch.String
		mem.GitCommit = gitCommit.String
		mem.Author = author.String
		mem.Impact = impact.String
		mem.ErrorsFaced = errorsFaced.String
		mem.NextSteps = nextSteps.String
		mem.SessionID = sessionID.String
		mem.TopicKey = topicKey.String
		if revisionCount.Valid {
			mem.RevisionCount = int(revisionCount.Int64)
		}
		if duplicateCount.Valid {
			mem.DuplicateCount = int(duplicateCount.Int64)
		}
		mem.NormalizedHash = normalizedHash.String
		if lastSeenAtStr != "" {
			if t, err := parseTime(lastSeenAtStr); err == nil {
				mem.LastSeenAt = t
			}
		}
		if t, err := parseTime(createdAtStr); err == nil {
			mem.CreatedAt = t
		} else {
			mem.CreatedAt = time.Now()
		}
		memories = append(memories, &mem)
	}
	return memories, rows.Err()
}

// SuggestTopicKey generates a topic_key suggestion in the format
// "category/kebab-case-description" from the memory category and title.
// This helps agents adopt a consistent naming convention for topic upserts.
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
	// Collapse multiple consecutive hyphens
	for strings.Contains(key, "--") {
		key = strings.ReplaceAll(key, "--", "-")
	}
	// Trim leading/trailing hyphens
	key = strings.Trim(key, "-")
	// Limit length
	if len(key) > 80 {
		key = key[:80]
	}
	key = strings.TrimRight(key, "-")
	return key
}
