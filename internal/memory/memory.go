package memory

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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

// MemorySearchResult is a compact representation used for progressive disclosure
// (Layer 1 — search). It carries only the fields needed for the agent to decide
// whether to drill down via sv_mem_get (Layer 3) or sv_mem_timeline (Layer 2).
type MemorySearchResult struct {
	ID             string    `json:"id"`
	Category       string    `json:"category"`
	What           string    `json:"what"`
	TopicKey       string    `json:"topic_key,omitempty"`
	RevisionCount  int       `json:"revision_count,omitempty"`
	DuplicateCount int       `json:"duplicate_count,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// Memory represents a recorded design decision, bugfix, or coding standard.
type Memory struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"project_id"`
	Category       string    `json:"category"` // 'bugfix' | 'architecture' | 'standard' | 'decision' | 'journal' | 'postmortem' | 'discussion' | 'idea' | 'qa' | 'observation'
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
	DeletedAt      time.Time `json:"deleted_at,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// MemoryRelation records a judgment that links two memories together.
type MemoryRelation struct {
	ID           string    `json:"id"`
	ProjectID    string    `json:"project_id"`
	SourceID     string    `json:"source_id"`
	TargetID     string    `json:"target_id"`
	RelationType string    `json:"relation_type"` // 'supersedes' | 'conflicts_with' | 'relates_to'
	Reason       string    `json:"reason"`
	JudgedBy     string    `json:"judged_by"`
	CreatedAt    time.Time `json:"created_at"`
}

// MemoryReviewItem is a single item returned by sv_mem_review.
type MemoryReviewItem struct {
	Memory          *MemorySearchResult `json:"memory"`
	AgeDays         int                 `json:"age_days"`
	LastSeenDays    int                 `json:"last_seen_days,omitempty"`
	RevisionCount   int                 `json:"revision_count"`
	DuplicateCount  int                 `json:"duplicate_count"`
	RelationCount   int                 `json:"relation_count"`
	NeedsConsolidation bool             `json:"needs_consolidation"`
	Reason          string              `json:"reason"`
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

	// Fetch memories from that session (compact — only IDs + titles)
	mems, err := SearchMemoriesBySessionCompact(db, projectID, session.ID, 10)
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
	FROM memories WHERE project_id = ? AND session_id = ? AND deleted_at IS NULL
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

// SearchMemoriesBySessionCompact returns compact search results for memories
// associated with a specific session. Uses a lightweight SELECT (7 columns)
// to reduce I/O and token overhead in session context views.
func SearchMemoriesBySessionCompact(db *sql.DB, projectID, sessionID string, limit int) ([]*MemorySearchResult, error) {
	query := `
	SELECT id, category, what,
		topic_key, revision_count, duplicate_count, created_at
	FROM memories WHERE project_id = ? AND session_id = ? AND deleted_at IS NULL
	ORDER BY created_at ASC`
	var args []interface{}
	args = append(args, projectID, sessionID)
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to search compact memories by session: %w", err)
	}
	defer rows.Close()
	return scanCompactMemories(rows)
}

// SearchMemoriesCompact returns compact search results (7 columns instead of 20)
// for token-efficient progressive disclosure. Supports optional offset for
// pagination. Layer 1 of the progressive disclosure pattern.
func SearchMemoriesCompact(db *sql.DB, projectID string, searchTerm string, category string, limit int, offset int) ([]*MemorySearchResult, error) {
	var query string
	var args []interface{}

	if searchTerm == "" {
		query = `
		SELECT id, category, what,
			topic_key, revision_count, duplicate_count, created_at
		FROM memories
		WHERE project_id = ? AND deleted_at IS NULL
		`
		args = append(args, projectID)
		if category != "" {
			query += " AND category = ?"
			args = append(args, category)
		}
		query += " ORDER BY created_at DESC"
	} else {
		query = `
		SELECT m.id, m.category, m.what,
			m.topic_key, m.revision_count, m.duplicate_count, m.created_at
		FROM memories m
		JOIN memories_fts f ON m.rowid = f.rowid
		WHERE m.project_id = ? AND memories_fts MATCH ? AND m.deleted_at IS NULL
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
		if offset > 0 {
			query += " OFFSET ?"
			args = append(args, offset)
		}
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed searching compact memories: %w", err)
	}
	defer rows.Close()
	return scanCompactMemories(rows)
}

// scanCompactMemories scans compact memory rows into MemorySearchResult slice.
func scanCompactMemories(rows *sql.Rows) ([]*MemorySearchResult, error) {
	var results []*MemorySearchResult
	for rows.Next() {
		var r MemorySearchResult
		var createdAtStr string
		var topicKey sql.NullString
		var revisionCount, duplicateCount sql.NullInt64
		err := rows.Scan(&r.ID, &r.Category, &r.What, &topicKey, &revisionCount, &duplicateCount, &createdAtStr)
		if err != nil {
			return nil, fmt.Errorf("failed scanning compact memory row: %w", err)
		}
		r.TopicKey = topicKey.String
		if revisionCount.Valid {
			r.RevisionCount = int(revisionCount.Int64)
		}
		if duplicateCount.Valid {
			r.DuplicateCount = int(duplicateCount.Int64)
		}
		if t, err := parseTime(createdAtStr); err == nil {
			r.CreatedAt = t
		} else {
			r.CreatedAt = time.Now()
		}
		results = append(results, &r)
	}
	return results, rows.Err()
}

// MemoryCandidate is a potential duplicate returned by conflict surfacing.
type MemoryCandidate struct {
	ID         string  `json:"id"`
	Category   string  `json:"category"`
	What       string  `json:"what"`
	Similarity float64 `json:"similarity"`
}

// stopWords for English titles — common words that don't carry semantic weight.
var stopWords = map[string]bool{
	"the": true, "a": true, "an": true, "and": true, "or": true, "but": true,
	"in": true, "on": true, "at": true, "to": true, "for": true, "of": true,
	"with": true, "by": true, "from": true, "as": true, "is": true, "it": true,
	"be": true, "are": true, "was": true, "were": true, "been": true, "being": true,
	"have": true, "has": true, "had": true, "do": true, "does": true, "did": true,
	"will": true, "would": true, "could": true, "should": true, "may": true, "might": true,
	"not": true, "no": true, "nor": true, "this": true, "that": true, "these": true, "those": true,
	"i": true, "me": true, "my": true, "we": true, "our": true, "you": true, "your": true,
	"he": true, "she": true, "his": true, "her": true, "they": true, "them": true, "their": true,
	"use": true, "using": true, "used": true, "set": true, "via": true, "per": true,
	"all": true, "each": true, "every": true, "some": true, "any": true, "both": true,
	"its": true, "if": true, "then": true, "else": true, "than": true, "so": true,
}

// tokenizeTitle splits a title into normalized tokens, removing stop words.
func tokenizeTitle(title string) []string {
	title = strings.ToLower(title)
	var tokens []string
	var current strings.Builder
	for _, r := range title {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			current.WriteRune(r)
		} else {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	filtered := tokens[:0]
	for _, t := range tokens {
		if !stopWords[t] && len(t) > 1 {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// jaccardSimilarity computes the Jaccard similarity between two token slices.
func jaccardSimilarity(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	setA := make(map[string]bool, len(a))
	for _, t := range a {
		setA[t] = true
	}
	intersection := 0
	setB := make(map[string]bool, len(b))
	for _, t := range b {
		setB[t] = true
		if setA[t] {
			intersection++
		}
	}
	union := len(setA)
	for t := range setB {
		if !setA[t] {
			union++
		}
	}
	if union == 0 {
		return 0.0
	}
	return float64(intersection) / float64(union)
}

// Stats holds aggregate statistics about project memories.
type Stats struct {
	TotalMemories    int               `json:"total_memories"`
	DeletedMemories  int               `json:"deleted_memories"`
	ByCategory       map[string]int    `json:"by_category"`
	TotalSessions    int               `json:"total_sessions"`
	ActiveSessions   int               `json:"active_sessions"`
	TotalRelations   int               `json:"total_relations"`
	Recent24h        int               `json:"recent_24h"`
}

// DiagnosticsResult holds a single diagnostic check outcome.
type DiagnosticsResult struct {
	Check   string `json:"check"`
	Status  string `json:"status"` // "pass" | "warn" | "fail"
	Message string `json:"message"`
}

// RunDiagnostics performs read-only health checks on the project setup.
// Returns a list of check results with status pass/warn/fail.
func RunDiagnostics(db *sql.DB, projectID, projPath, dbPath string) []DiagnosticsResult {
	var results []DiagnosticsResult

	add := func(check, status, msg string) {
		results = append(results, DiagnosticsResult{Check: check, Status: status, Message: msg})
	}

	// 1. DB file exists and is readable
	if _, err := os.Stat(dbPath); err == nil {
		add("database_file", "pass", fmt.Sprintf("Database file found at %s", dbPath))
	} else {
		add("database_file", "fail", fmt.Sprintf("Database file not found at %s: %v", dbPath, err))
		return results
	}

	// 2. DB connection is alive
	if err := db.Ping(); err != nil {
		add("database_connection", "fail", fmt.Sprintf("Cannot ping database: %v", err))
		return results
	}
	add("database_connection", "pass", "Database connection is alive")

	// 3. Required tables exist
	requiredTables := []string{
		"projects", "memories", "memories_fts",
		"sessions", "memory_relations",
		"graph_nodes", "graph_edges", "graph_files_meta",
	}
	for _, table := range requiredTables {
		var found int
		err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&found)
		if err != nil {
			add("table_"+table, "fail", fmt.Sprintf("Error checking table %s: %v", table, err))
			continue
		}
		if found == 0 {
			// It might be a virtual table (like FTS)
			err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name=?", table).Scan(&found)
			if err != nil {
				add("table_"+table, "fail", fmt.Sprintf("Error checking virtual table %s: %v", table, err))
				continue
			}
		}
		if found > 0 {
			add("table_"+table, "pass", fmt.Sprintf("Table %s exists", table))
		} else {
			add("table_"+table, "fail", fmt.Sprintf("Table %s is missing", table))
		}
	}

	// 4. FTS5 triggers exist
	requiredTriggers := []string{"memories_ai", "memories_ad", "memories_au"}
	for _, trig := range requiredTriggers {
		var found int
		if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='trigger' AND name=?", trig).Scan(&found); err != nil {
			add("trigger_"+trig, "fail", fmt.Sprintf("Error checking trigger %s: %v", trig, err))
			continue
		}
		if found > 0 {
			add("trigger_"+trig, "pass", fmt.Sprintf("Trigger %s exists", trig))
		} else {
			add("trigger_"+trig, "warn", fmt.Sprintf("Trigger %s is missing — FTS5 sync may be incomplete", trig))
		}
	}

	// 5. Project is registered
	var projCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM projects WHERE id=?", projectID).Scan(&projCount); err != nil {
		add("project_registered", "fail", fmt.Sprintf("Error querying project: %v", err))
	} else if projCount > 0 {
		add("project_registered", "pass", "Project is registered in database")
	} else {
		add("project_registered", "fail", "Project is not registered — run 'sv-memory init'")
	}

	// 6. ProjPath writable
	tmpFile := filepath.Join(projPath, ".sv-memory-write-test")
	if err := os.WriteFile(tmpFile, []byte{}, 0644); err != nil {
		add("project_path_writable", "fail", fmt.Sprintf("Project path is not writable: %v", err))
	} else {
		os.Remove(tmpFile)
		add("project_path_writable", "pass", "Project path is writable")
	}

	// 7. Chunk directory state
	chunkDir := filepath.Join(projPath, ".sv-memory", "chunks")
	if fi, err := os.Stat(chunkDir); err == nil {
		if fi.IsDir() {
			entries, _ := os.ReadDir(chunkDir)
			jsonCount := 0
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
					jsonCount++
				}
			}
			add("chunk_directory", "pass", fmt.Sprintf("Chunk directory exists with %d JSON files", jsonCount))
		}
	} else if os.IsNotExist(err) {
		// Check legacy file
		if _, err := os.Stat(filepath.Join(projPath, ".sv-memory", "memories.json")); err == nil {
			add("chunk_directory", "warn", "Using legacy memories.json (no chunk directory)")
		} else {
			add("chunk_directory", "warn", "No sync directory found — run 'sv-memory sync' after first save")
		}
	} else {
		add("chunk_directory", "warn", fmt.Sprintf("Cannot stat chunk directory: %v", err))
	}

	// 8. FTS5 quick health check
	var ftsCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM memories_fts").Scan(&ftsCount); err != nil {
		add("fts5_healthy", "fail", fmt.Sprintf("FTS5 query failed: %v", err))
	} else {
		add("fts5_healthy", "pass", fmt.Sprintf("FTS5 is healthy (%d indexed rows)", ftsCount))
	}

	return results
}

// GetStats returns aggregate statistics for the given project.
func GetStats(db *sql.DB, projectID string) (*Stats, error) {
	stats := &Stats{
		ByCategory: make(map[string]int),
	}

	if err := db.QueryRow("SELECT COUNT(*) FROM memories WHERE project_id = ? AND deleted_at IS NULL", projectID).Scan(&stats.TotalMemories); err != nil {
		return nil, fmt.Errorf("failed to count memories: %w", err)
	}

	if err := db.QueryRow("SELECT COUNT(*) FROM memories WHERE project_id = ? AND deleted_at IS NOT NULL", projectID).Scan(&stats.DeletedMemories); err != nil {
		return nil, fmt.Errorf("failed to count deleted memories: %w", err)
	}

	catRows, err := db.Query("SELECT category, COUNT(*) FROM memories WHERE project_id = ? AND deleted_at IS NULL GROUP BY category ORDER BY COUNT(*) DESC", projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to query categories: %w", err)
	}
	defer catRows.Close()
	for catRows.Next() {
		var cat string
		var count int
		if err := catRows.Scan(&cat, &count); err != nil {
			return nil, fmt.Errorf("failed scanning category row: %w", err)
		}
		stats.ByCategory[cat] = count
	}
	if err := catRows.Err(); err != nil {
		return nil, err
	}

	if err := db.QueryRow("SELECT COUNT(*) FROM sessions WHERE project_id = ?", projectID).Scan(&stats.TotalSessions); err != nil {
		return nil, fmt.Errorf("failed to count sessions: %w", err)
	}

	if err := db.QueryRow("SELECT COUNT(*) FROM sessions WHERE project_id = ? AND status = 'active'", projectID).Scan(&stats.ActiveSessions); err != nil {
		return nil, fmt.Errorf("failed to count active sessions: %w", err)
	}

	if err := db.QueryRow("SELECT COUNT(*) FROM memory_relations WHERE project_id = ?", projectID).Scan(&stats.TotalRelations); err != nil {
		return nil, fmt.Errorf("failed to count relations: %w", err)
	}

	if err := db.QueryRow("SELECT COUNT(*) FROM memories WHERE project_id = ? AND deleted_at IS NULL AND created_at > datetime('now', '-24 hours')", projectID).Scan(&stats.Recent24h); err != nil {
		return nil, fmt.Errorf("failed to count recent memories: %w", err)
	}

	return stats, nil
}

// ProjectInfo holds summary data for a registered project.
type ProjectInfo struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Path         string `json:"path"`
	MemoryCount  int    `json:"memory_count"`
	SessionCount int    `json:"session_count"`
}

// ListProjects returns all registered projects with their memory and session counts.
func ListProjects(db *sql.DB) ([]*ProjectInfo, error) {
	rows, err := db.Query(`
		SELECT p.id, p.name, p.path,
			(SELECT COUNT(*) FROM memories m WHERE m.project_id = p.id AND m.deleted_at IS NULL) as mem_count,
			(SELECT COUNT(*) FROM sessions s WHERE s.project_id = p.id) as sess_count
		FROM projects p
		ORDER BY p.name ASC`)
	if err != nil {
		return nil, fmt.Errorf("failed to list projects: %w", err)
	}
	defer rows.Close()

	var projects []*ProjectInfo
	for rows.Next() {
		var p ProjectInfo
		if err := rows.Scan(&p.ID, &p.Name, &p.Path, &p.MemoryCount, &p.SessionCount); err != nil {
			return nil, fmt.Errorf("failed scanning project row: %w", err)
		}
		projects = append(projects, &p)
	}
	return projects, rows.Err()
}

// PruneProjects removes projects that have zero memories.
// Returns the IDs of pruned projects.
func PruneProjects(db *sql.DB) ([]string, error) {
	projects, err := ListProjects(db)
	if err != nil {
		return nil, err
	}

	var pruned []string
	for _, p := range projects {
		if p.MemoryCount == 0 && p.SessionCount == 0 {
			if _, err := db.Exec("DELETE FROM projects WHERE id=?", p.ID); err != nil {
				return pruned, fmt.Errorf("failed to prune project %s: %w", p.ID, err)
			}
			pruned = append(pruned, p.ID)
		}
	}
	return pruned, nil
}

// ConsolidateProjects moves all memories and sessions from sourceProjectID
// to targetProjectID, then removes the source project. Returns counts of
// moved memories and sessions.
func ConsolidateProjects(db *sql.DB, sourceID, targetID string) (movedMemories int, movedSessions int, err error) {
	// Verify both projects exist
	var srcName, tgtName string
	if err := db.QueryRow("SELECT name FROM projects WHERE id=?", sourceID).Scan(&srcName); err != nil {
		return 0, 0, fmt.Errorf("source project %s not found", sourceID)
	}
	if err := db.QueryRow("SELECT name FROM projects WHERE id=?", targetID).Scan(&tgtName); err != nil {
		return 0, 0, fmt.Errorf("target project %s not found", targetID)
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	// Move memories
	res, err := tx.Exec("UPDATE memories SET project_id=? WHERE project_id=?", targetID, sourceID)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to move memories: %w", err)
	}
	memN, _ := res.RowsAffected()
	movedMemories = int(memN)

	// Move sessions
	res, err = tx.Exec("UPDATE sessions SET project_id=? WHERE project_id=?", targetID, sourceID)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to move sessions: %w", err)
	}
	sessN, _ := res.RowsAffected()
	movedSessions = int(sessN)

	// Move relations
	if _, err := tx.Exec("UPDATE memory_relations SET project_id=? WHERE project_id=?", targetID, sourceID); err != nil {
		return 0, 0, fmt.Errorf("failed to move relations: %w", err)
	}

	// Delete source project
	if _, err := tx.Exec("DELETE FROM projects WHERE id=?", sourceID); err != nil {
		return 0, 0, fmt.Errorf("failed to delete source project: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}

	return movedMemories, movedSessions, nil
}

// FindSimilarMemories returns up to `limit` memory candidates whose title
// tokens overlap significantly with the given title (Jaccard > threshold).
// Uses FTS5 for initial candidate retrieval, then filters by token similarity.
func FindSimilarMemories(db *sql.DB, projectID, title string, limit int, threshold float64) ([]*MemoryCandidate, error) {
	tokens := tokenizeTitle(title)
	if len(tokens) == 0 {
		return nil, nil
	}

	// Build FTS5 query from tokens (OR match to maximize recall).
	ftsQuery := strings.Join(tokens, " OR ")

	rows, err := db.Query(`
		SELECT m.id, m.category, m.what
		FROM memories m
		JOIN memories_fts f ON m.rowid = f.rowid
		WHERE m.project_id = ? AND memories_fts MATCH ? AND m.deleted_at IS NULL
		ORDER BY rank
		LIMIT ?`, projectID, ftsQuery, limit*3)
	if err != nil {
		return nil, fmt.Errorf("failed to search similar memories: %w", err)
	}
	defer rows.Close()

	var candidates []*MemoryCandidate
	for rows.Next() {
		var id, category, what string
		if err := rows.Scan(&id, &category, &what); err != nil {
			return nil, fmt.Errorf("failed scanning similar memory: %w", err)
		}
		candidateTokens := tokenizeTitle(what)
		sim := jaccardSimilarity(tokens, candidateTokens)
		if sim >= threshold {
			candidates = append(candidates, &MemoryCandidate{
				ID:         id,
				Category:   category,
				What:       what,
				Similarity: math.Round(sim*100) / 100,
			})
			if len(candidates) >= limit {
				break
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return candidates, nil
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
		WHERE project_id = ? AND deleted_at IS NULL
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
		WHERE m.project_id = ? AND memories_fts MATCH ? AND m.deleted_at IS NULL
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
		var lastSeenAtStr sql.NullString
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
		if lastSeenAtStr.Valid && lastSeenAtStr.String != "" {
			if t, err := parseTime(lastSeenAtStr.String); err == nil {
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

// chunkedSyncDir returns the chunks directory path for a project.
func chunkedSyncDir(projPath string) string {
	return filepath.Join(projPath, ".sv-memory", "chunks")
}

// SyncToGitChunked writes each memory as its own JSON file in .sv-memory/chunks/.
// This avoids Git merge conflicts: each memory is an independent file, so parallel
// saves on different branches produce no merge conflicts.
func SyncToGitChunked(db *sql.DB, projectID string, projPath string) error {
	chunkDir := chunkedSyncDir(projPath)
	if err := os.MkdirAll(chunkDir, 0755); err != nil {
		return fmt.Errorf("failed to create chunks directory: %w", err)
	}

	// Load all memories including soft-deleted.
	memories, err := searchAllMemories(db, projectID)
	if err != nil {
		return err
	}

	// Track existing chunks so we can clean up stale ones.
	existingChunks := make(map[string]bool)
	entries, err := os.ReadDir(chunkDir)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
				existingChunks[strings.TrimSuffix(e.Name(), ".json")] = true
			}
		}
	}

	for _, mem := range memories {
		data, err := json.MarshalIndent(mem, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal chunk %s: %w", mem.ID, err)
		}
		chunkPath := filepath.Join(chunkDir, mem.ID+".json")
		tmpPath := chunkPath + ".tmp"
		if err := os.WriteFile(tmpPath, data, 0644); err != nil {
			return fmt.Errorf("failed to write chunk %s: %w", mem.ID, err)
		}
		if err := os.Rename(tmpPath, chunkPath); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("failed to rename chunk %s: %w", mem.ID, err)
		}
		delete(existingChunks, mem.ID)
	}

	// Remove chunks for memories that no longer exist (hard-deleted).
	for id := range existingChunks {
		os.Remove(filepath.Join(chunkDir, id+".json"))
	}

	return nil
}

// SyncFromGitChunked reads all memory chunks from .sv-memory/chunks/ and imports
// them into SQLite. Each chunk is a single JSON file per memory, so parallel edits
// on different branches never conflict. Falls back to legacy memories.json if no
// chunks directory exists.
func SyncFromGitChunked(db *sql.DB, projectID string, projPath string) error {
	chunkDir := chunkedSyncDir(projPath)
	if _, err := os.Stat(chunkDir); os.IsNotExist(err) {
		return SyncFromGit(db, projectID, projPath)
	}

	entries, err := os.ReadDir(chunkDir)
	if err != nil {
		return fmt.Errorf("failed to read chunks directory: %w", err)
	}

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

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		chunkPath := filepath.Join(chunkDir, entry.Name())
		data, err := os.ReadFile(chunkPath)
		if err != nil {
			return fmt.Errorf("failed to read chunk %s: %w", entry.Name(), err)
		}
		var mem Memory
		if err := json.Unmarshal(data, &mem); err != nil {
			return fmt.Errorf("failed to parse chunk %s: %w", entry.Name(), err)
		}
		mem.ProjectID = projectID
		createdAt := mem.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now()
		}
		_, err = stmt.Exec(
			mem.ID, mem.ProjectID, mem.Category, mem.What, mem.Why, mem.WherePath, mem.Learned,
			mem.GitBranch, mem.GitCommit, mem.Author, mem.Impact, mem.ErrorsFaced, mem.NextSteps,
			nullString(mem.SessionID), nullString(mem.TopicKey),
			mem.RevisionCount, mem.DuplicateCount,
			nullTime(mem.LastSeenAt), nullString(mem.NormalizedHash),
			createdAt)
		if err != nil {
			return fmt.Errorf("failed to import chunk %s: %w", entry.Name(), err)
		}
	}

	return tx.Commit()
}

// SyncToGit serializes all project memories to local files in `.sv-memory/`.
// Uses the chunked format (.sv-memory/chunks/{id}.json) to avoid Git merge
// conflicts. Also writes a legacy memories.json for backward compatibility.
// When nothing changed since the last write it skips the I/O entirely.
func SyncToGit(db *sql.DB, projectID string, projPath string) error {
	syncDir := filepath.Join(projPath, ".sv-memory")
	syncFile := filepath.Join(syncDir, "memories.json")
	chunkDir := chunkedSyncDir(projPath)

	// Quick check: count current memories and compare with cache.
	// Uses the chunk dir mtime as the signal (more reliable than the legacy file).
	count, err := countMemories(db, projectID)
	if err != nil {
		return err
	}

	syncCacheMu.Lock()
	info := lastWriteInfo[projectID]
	currentMtim := time.Time{}
	if fi, statErr := os.Stat(chunkDir); statErr == nil {
		currentMtim = fi.ModTime()
	} else if fi, statErr := os.Stat(syncFile); statErr == nil {
		currentMtim = fi.ModTime()
	}
	if count == info.memoryCount && currentMtim.Equal(info.fileMtim) {
		syncCacheMu.Unlock()
		return nil // nothing changed — skip write
	}
	syncCacheMu.Unlock()

	// Load all memories from SQLite (including soft-deleted for faithful sync).
	memories, err := searchAllMemories(db, projectID)
	if err != nil {
		return err
	}

	// Create directories if missing.
	if err := os.MkdirAll(syncDir, 0755); err != nil {
		return fmt.Errorf("failed to create sync directory: %w", err)
	}

	// 1. Write chunked format (primary — merge-conflict-free).
	if err := os.MkdirAll(chunkDir, 0755); err != nil {
		return fmt.Errorf("failed to create chunks directory: %w", err)
	}
	existingChunks := make(map[string]bool)
	entries, err := os.ReadDir(chunkDir)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
				existingChunks[strings.TrimSuffix(e.Name(), ".json")] = true
			}
		}
	}
	for _, mem := range memories {
		data, err := json.MarshalIndent(mem, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal chunk %s: %w", mem.ID, err)
		}
		chunkPath := filepath.Join(chunkDir, mem.ID+".json")
		tmpPath := chunkPath + ".tmp"
		if err := os.WriteFile(tmpPath, data, 0644); err != nil {
			return fmt.Errorf("failed to write chunk %s: %w", mem.ID, err)
		}
		if err := os.Rename(tmpPath, chunkPath); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("failed to rename chunk %s: %w", mem.ID, err)
		}
		delete(existingChunks, mem.ID)
	}
	for id := range existingChunks {
		os.Remove(filepath.Join(chunkDir, id+".json"))
	}

	// 2. Write legacy JSON array for backward compatibility.
	data, err := json.MarshalIndent(memories, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal memories JSON: %w", err)
	}
	tmpFile := syncFile + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp memories file: %w", err)
	}
	if err := os.Rename(tmpFile, syncFile); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	// Update cache with mtime from the chunk dir (primary signal for changes).
	signalPath := chunkDir
	if fi, statErr := os.Stat(signalPath); statErr == nil {
		syncCacheMu.Lock()
		lastWriteInfo[projectID] = syncCacheEntry{
			memoryCount: count,
			fileMtim:    fi.ModTime(),
		}
		syncCacheMu.Unlock()
	} else {
		syncCacheMu.Lock()
		delete(lastWriteInfo, projectID)
		syncCacheMu.Unlock()
	}

	return nil
}

// SyncFromGit imports memories from `.sv-memory/`, preferring the chunked format.
// Falls back to the legacy `memories.json` for backward compatibility.
func SyncFromGit(db *sql.DB, projectID string, projPath string) error {
	// Try chunked format first (merge-conflict-free).
	chunkDir := chunkedSyncDir(projPath)
	if _, err := os.Stat(chunkDir); err == nil {
		return SyncFromGitChunked(db, projectID, projPath)
	}

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

// searchAllMemories returns ALL memories for a project including soft-deleted ones.
// Used by SyncToGit to ensure a faithful export for team sync.
func searchAllMemories(db *sql.DB, projectID string) ([]*Memory, error) {
	rows, err := db.Query(`
		SELECT id, project_id, category, what, why, where_path, learned,
			git_branch, git_commit, author, impact, errors_faced, next_steps,
			session_id, topic_key, revision_count, duplicate_count, last_seen_at, normalized_hash, created_at
		FROM memories WHERE project_id = ?
		ORDER BY created_at ASC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMemories(rows)
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
	FROM memories WHERE project_id = ? AND id = ? AND deleted_at IS NULL`
	row := db.QueryRow(query, projectID, id)
	var mem Memory
	var createdAtStr string
	var lastSeenAtStr sql.NullString
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
	if lastSeenAtStr.Valid && lastSeenAtStr.String != "" {
		if t, err := parseTime(lastSeenAtStr.String); err == nil {
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
		FROM memories WHERE project_id = ? AND created_at < ? AND id != ? AND deleted_at IS NULL
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
		FROM memories WHERE project_id = ? AND created_at > ? AND id != ? AND deleted_at IS NULL
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

// SaveJudgment creates a relation between two memories (conflict surfacing).
// If a relation with the same source+target+type already exists, it updates in place.
func SaveJudgment(db *sql.DB, projectID, sourceID, targetID, relationType, reason, judgedBy string) (*MemoryRelation, error) {
	if sourceID == targetID {
		return nil, errors.New("cannot create a relation between a memory and itself")
	}
	id := uuid.New().String()[:8]
	now := time.Now()

	// Check if both memories exist and are not deleted
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

// GetRelations returns all relations involving a given memory (as source or target).
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

// CountRelations returns the total number of relations for a given memory.
func CountRelations(db *sql.DB, projectID, memoryID string) (int, error) {
	var n int
	err := db.QueryRow("SELECT COUNT(*) FROM memory_relations WHERE project_id = ? AND (source_id = ? OR target_id = ?)", projectID, memoryID, memoryID).Scan(&n)
	return n, err
}

// CompareMemories returns both memories side by side in a formatted Markdown string.
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
	sb.WriteString(fmt.Sprintf("| **ID** | `%s` | `%s` |\n", m1.ID, m2.ID))
	sb.WriteString(fmt.Sprintf("| **Category** | `%s` | `%s` |\n", m1.Category, m2.Category))
	sb.WriteString(fmt.Sprintf("| **What** | %s | %s |\n", m1.What, m2.What))
	sb.WriteString(fmt.Sprintf("| **Why** | %s | %s |\n", m1.Why, m2.Why))
	sb.WriteString(fmt.Sprintf("| **Learned** | %s | %s |\n", m1.Learned, m2.Learned))
	if m1.WherePath != "" || m2.WherePath != "" {
		sb.WriteString(fmt.Sprintf("| **Path** | `%s` | `%s` |\n", m1.WherePath, m2.WherePath))
	}
	if m1.TopicKey != "" || m2.TopicKey != "" {
		sb.WriteString(fmt.Sprintf("| **Topic** | `%s` | `%s` |\n", m1.TopicKey, m2.TopicKey))
	}
	sb.WriteString(fmt.Sprintf("| **Date** | %s | %s |\n", m1.CreatedAt.Format("2006-01-02"), m2.CreatedAt.Format("2006-01-02")))

	// Check for existing relations between them
	rels, _ := GetRelations(db, projectID, id1)
	for _, r := range rels {
		if (r.SourceID == id1 && r.TargetID == id2) || (r.SourceID == id2 && r.TargetID == id1) {
			sb.WriteString(fmt.Sprintf("\n**Existing relation:** `%s` — %s\n", r.RelationType, r.Reason))
			break
		}
	}

	return sb.String(), nil
}

// ReviewMemories returns a list of memories that may need attention: old, stale,
// with many duplicates, or candidates for consolidation.
func ReviewMemories(db *sql.DB, projectID string) ([]*MemoryReviewItem, error) {
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

		// Check for relations
		relCount, _ := CountRelations(db, projectID, r.Memory.ID)
		r.RelationCount = relCount

		// Determine reasons
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
	return items, rows.Err()
}

// DeleteSession deletes a session by ID. It must have no associated memories.
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

// DeleteProject cascade-deletes a project. When hard=true, all associated
// memories, sessions, relations, and graph data are permanently removed.
// When hard=false, memories are soft-deleted (deleted_at set) and the
// project row is removed — sessions and relations are cascade-deleted
// by foreign key.
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

	// Soft: mark all non-deleted memories as deleted, remove sessions and
	// relations, but keep the project row (shell) so foreign key references
	// from graph data remain valid.
	if _, err := db.Exec("UPDATE memories SET deleted_at=? WHERE project_id=? AND deleted_at IS NULL", time.Now(), id); err != nil {
		return fmt.Errorf("failed to soft-delete memories: %w", err)
	}
	if _, err := db.Exec("DELETE FROM sessions WHERE project_id=?", id); err != nil {
		return fmt.Errorf("failed to delete sessions: %w", err)
	}
	if _, err := db.Exec("DELETE FROM memory_relations WHERE project_id=?", id); err != nil {
		return fmt.Errorf("failed to delete relations: %w", err)
	}
	// Verify project exists
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM projects WHERE id=?", id).Scan(&n); err != nil {
		return fmt.Errorf("failed to check project: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("project %s not found", id)
	}
	return nil
}

// DeleteMemory performs a soft or hard delete of a memory.
// Soft: sets deleted_at to NOW, excluded from search results but recoverable.
// Hard: removes the row permanently.
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
		result, err := db.Exec("UPDATE memories SET deleted_at = ? WHERE project_id = ? AND id = ? AND deleted_at IS NULL",
			time.Now(), projectID, id)
		if err != nil {
			return fmt.Errorf("failed to soft-delete memory: %w", err)
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			return fmt.Errorf("memory %s not found or already deleted", id)
		}
	}
	return nil
}

// CapturePassive saves a lightweight observation without requiring the full
// memory schema. Used for passive context logging. The 'learned' field is
// derived from 'what' when not explicitly provided. Category is forced to
// 'observation' internally for search filtering.
func CapturePassive(db *sql.DB, projectID, what, why, sessionID string) (*Memory, error) {
	learned := what
	mem := &Memory{
		ProjectID: projectID,
		Category:  "journal",
		What:      what,
		Why:       why,
		Learned:   learned,
		SessionID: sessionID,
		GitBranch: "", // passive captures are not git-versioned
		GitCommit: "",
	}
	return SaveMemory(db, mem)
}

// scanMemories is a helper that scans all rows from a query result into []*Memory.
func scanMemories(rows *sql.Rows) ([]*Memory, error) {
	var memories []*Memory
	for rows.Next() {
		var mem Memory
		var createdAtStr string
		var lastSeenAtStr sql.NullString
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
		if lastSeenAtStr.Valid && lastSeenAtStr.String != "" {
			if t, err := parseTime(lastSeenAtStr.String); err == nil {
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

// ExportJSON exports all non-deleted project memories to a JSON file.
// Returns the number of memories exported.
func ExportJSON(db *sql.DB, projectID, filePath string) (int, error) {
	memories, err := SearchMemories(db, projectID, "", "", 0)
	if err != nil {
		return 0, fmt.Errorf("failed to query memories for export: %w", err)
	}

	data, err := json.MarshalIndent(memories, "", "  ")
	if err != nil {
		return 0, fmt.Errorf("failed to marshal memories JSON: %w", err)
	}

	tmpPath := filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return 0, fmt.Errorf("failed to write export file: %w", err)
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		os.Remove(tmpPath)
		return 0, fmt.Errorf("failed to finalize export file: %w", err)
	}

	return len(memories), nil
}

// ImportJSON imports memories from a JSON file into the database.
// Uses upsert semantics: existing IDs are updated, new IDs are inserted.
// Returns the number of memories imported.
func ImportJSON(db *sql.DB, projectID, filePath string) (int, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return 0, fmt.Errorf("failed to read import file: %w", err)
	}

	var memories []*Memory
	if err := json.Unmarshal(data, &memories); err != nil {
		return 0, fmt.Errorf("failed to parse import JSON: %w", err)
	}

	if len(memories) == 0 {
		return 0, nil
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
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
		return 0, fmt.Errorf("failed to prepare insert statement: %w", err)
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
			return 0, fmt.Errorf("failed to import memory %s: %w", mem.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit import: %w", err)
	}

	return len(memories), nil
}

// ExportObsidian exports all project memories as Markdown files in Obsidian vault
// format. Each memory becomes a .md file with YAML frontmatter and [[wikilinks]]
// to related memories. The vault is written to <projPath>/<outputDir>/.
// Tags are derived from the memory category.
func ExportObsidian(db *sql.DB, projectID, projPath, outputDir string) error {
	memories, err := SearchMemories(db, projectID, "", "", 0)
	if err != nil {
		return err
	}

	vaultDir := filepath.Join(projPath, outputDir)
	if err := os.MkdirAll(vaultDir, 0755); err != nil {
		return fmt.Errorf("failed to create vault directory: %w", err)
	}

	// Pre-load all relations for wikilink generation.
	type relPair struct{ source, target string }
	allRels := make(map[string][]relPair)
	relRows, err := db.Query("SELECT source_id, target_id FROM memory_relations WHERE project_id = ?", projectID)
	if err == nil {
		defer relRows.Close()
		for relRows.Next() {
			var s, t string
			if relRows.Scan(&s, &t) == nil {
				allRels[s] = append(allRels[s], relPair{s, t})
				allRels[t] = append(allRels[t], relPair{s, t})
			}
		}
	}

	for _, mem := range memories {
		// Frontmatter
		fm := fmt.Sprintf(`---
id: "%s"
category: "%s"
title: "%s"
created: "%s"
tags: [memory/%s]
`,
			mem.ID,
			mem.Category,
			mem.What,
			mem.CreatedAt.Format("2006-01-02"),
			mem.Category)

		if mem.TopicKey != "" {
			fm += fmt.Sprintf(`topic_key: "%s"
revision: %d
`, mem.TopicKey, mem.RevisionCount)
		}
		if mem.WherePath != "" {
			fm += fmt.Sprintf(`path: "%s"
`, mem.WherePath)
		}
		fm += "---\n\n"

		// Body
		body := fmt.Sprintf("# %s\n\n**Category:** `%s`\n\n", mem.What, mem.Category)

		if mem.Why != "" {
			body += fmt.Sprintf("## Why\n%s\n\n", mem.Why)
		}
		if mem.Learned != "" {
			body += fmt.Sprintf("## Learned\n%s\n\n", mem.Learned)
		}
		if mem.WherePath != "" {
			body += fmt.Sprintf("**Path:** `%s`\n\n", mem.WherePath)
		}
		if mem.Impact != "" {
			body += fmt.Sprintf("## Impact\n%s\n\n", mem.Impact)
		}
		if mem.ErrorsFaced != "" {
			body += fmt.Sprintf("## Errors Faced\n%s\n\n", mem.ErrorsFaced)
		}
		if mem.NextSteps != "" {
			body += fmt.Sprintf("## Next Steps\n%s\n\n", mem.NextSteps)
		}

		// Wikilinks to related memories
		if rels, ok := allRels[mem.ID]; ok && len(rels) > 0 {
			body += "## Related Memories\n"
			for _, r := range rels {
				otherID := r.target
				if r.target == mem.ID {
					otherID = r.source
				}
				body += fmt.Sprintf("- [[%s]]\n", otherID)
			}
			body += "\n"
		}

		body += fmt.Sprintf("---\n*Exported from sv-memory on %s*\n", time.Now().Format("2006-01-02 15:04:05"))

		content := fm + body
		filePath := filepath.Join(vaultDir, mem.ID+".md")
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", filePath, err)
		}
	}

	return nil
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
