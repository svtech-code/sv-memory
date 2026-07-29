package memory

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/svtech/sv-memory/internal/security"
)

type MemorySearchResult struct {
	ID             string    `json:"id"`
	Category       string    `json:"category"`
	What           string    `json:"what"`
	TopicKey       string    `json:"topic_key,omitempty"`
	RevisionCount  int       `json:"revision_count,omitempty"`
	DuplicateCount int       `json:"duplicate_count,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
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
	DeletedAt      time.Time `json:"deleted_at,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
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

func SaveSessionSummary(db *sql.DB, id, goal, discoveries, accomplished, nextSteps, files string) error {
	goal = security.SanitizeText(goal)
	discoveries = security.SanitizeText(discoveries)
	accomplished = security.SanitizeText(accomplished)
	nextSteps = security.SanitizeText(nextSteps)
	files = security.SanitizeText(files)
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

func GetSessionContext(db *sql.DB, projectID string) (string, error) {
	session, err := GetLastSession(db, projectID)
	if err != nil {
		return "", err
	}
	if session == nil {
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
	sb.WriteString("## Previous Session Context\n\n")
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

func GetAutoBootBundle(db *sql.DB, projectID string) (string, error) {
	var sb strings.Builder
	sb.WriteString("### 🚀 Auto-Boot Context Bundle\n\n")

	sessCtx, err := GetSessionContext(db, projectID)
	if err == nil && sessCtx != "" && !strings.HasPrefix(sessCtx, "No previous session") {
		sb.WriteString(sessCtx)
		sb.WriteString("\n\n")
	}

	rows, err := db.Query(`
	SELECT id, category, what, why, learned, created_at
	FROM memories
	WHERE project_id = ? AND category IN ('architecture', 'decision') AND deleted_at IS NULL
	ORDER BY created_at DESC LIMIT 3`, projectID)
	if err == nil {
		defer rows.Close()
		var archMems []string
		for rows.Next() {
			var id, cat, what, why, learned, createdAt string
			if scanErr := rows.Scan(&id, &cat, &what, &why, &learned, &createdAt); scanErr == nil {
				archMems = append(archMems, fmt.Sprintf("- **[%s] %s** (ID: %s)\n  *Why:* %s", strings.ToUpper(cat), what, id, why))
			}
		}
		if len(archMems) > 0 {
			sb.WriteString("**Key Architectural Decisions:**\n")
			sb.WriteString(strings.Join(archMems, "\n"))
			sb.WriteString("\n\n")
		}
	}
	return strings.TrimSpace(sb.String()), nil
}

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

func SearchMemoriesCompact(db *sql.DB, projectID string, searchTerm string, category string, limit int, offset int) ([]*MemorySearchResult, error) {
	return SearchMemoriesCompactScoped(db, projectID, searchTerm, category, "", limit, offset)
}

func SearchMemoriesCompactScoped(db *sql.DB, projectID string, searchTerm string, category string, pathFilter string, limit int, offset int) ([]*MemorySearchResult, error) {
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
		if pathFilter != "" {
			query += " AND (where_path LIKE ? ESCAPE '\\' OR where_path = ?)"
			args = append(args, "%"+pathFilter+"%", pathFilter)
		}
		query += " ORDER BY created_at DESC"
	} else {
		searchTerm = sanitizeFTS5Query(searchTerm)
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
		if pathFilter != "" {
			query += " AND (m.where_path LIKE ? ESCAPE '\\' OR m.where_path = ?)"
			args = append(args, "%"+pathFilter+"%", pathFilter)
		}
		query += " ORDER BY bm25(memories_fts, 10.0, 5.0, 2.0)"
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
		r.What = security.SanitizeText(r.What)
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

type MemoryCandidate struct {
	ID         string  `json:"id"`
	Category   string  `json:"category"`
	What       string  `json:"what"`
	Similarity float64 `json:"similarity"`
}

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

func FindSimilarMemories(db *sql.DB, projectID, title string, limit int, threshold float64) ([]*MemoryCandidate, error) {
	tokens := tokenizeTitle(title)
	if len(tokens) == 0 {
		return nil, nil
	}
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

func computeHash(what, why, learned, wherePath string) string {
	data := what + "\x00" + why + "\x00" + learned + "\x00" + wherePath
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])
}

func SaveMemory(db *sql.DB, mem *Memory) (*Memory, error) {
	if mem.ProjectID == "" {
		return nil, errors.New("memory ProjectID cannot be empty")
	}
	if len(mem.What) > 1000 {
		return nil, fmt.Errorf("field 'what' exceeds maximum length of 1000 characters")
	}
	if len(mem.Why) > 4000 {
		return nil, fmt.Errorf("field 'why' exceeds maximum length of 4000 characters")
	}
	if len(mem.Learned) > 4000 {
		return nil, fmt.Errorf("field 'learned' exceeds maximum length of 4000 characters")
	}
	if len(mem.WherePath) > 1000 {
		return nil, fmt.Errorf("field 'where_path' exceeds maximum length of 1000 characters")
	}
	if len(mem.Impact) > 4000 {
		return nil, fmt.Errorf("field 'impact' exceeds maximum length of 4000 characters")
	}
	if len(mem.ErrorsFaced) > 4000 {
		return nil, fmt.Errorf("field 'errors_faced' exceeds maximum length of 4000 characters")
	}
	if len(mem.NextSteps) > 4000 {
		return nil, fmt.Errorf("field 'next_steps' exceeds maximum length of 4000 characters")
	}
	if len(mem.TopicKey) > 256 {
		return nil, fmt.Errorf("field 'topic_key' exceeds maximum length of 256 characters")
	}
	if len(mem.SessionID) > 64 {
		return nil, fmt.Errorf("field 'session_id' exceeds maximum length of 64 characters")
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
	mem.NormalizedHash = computeHash(mem.What, mem.Why, mem.Learned, mem.WherePath)

	if mem.TopicKey != "" {
		var existingID string
		var revCount int
		err := db.QueryRow(
			"SELECT id, revision_count FROM memories WHERE project_id = ? AND topic_key = ? AND deleted_at IS NULL ORDER BY created_at DESC LIMIT 1",
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
				last_seen_at = ?, created_at = ?, deleted_at = NULL
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
	}

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
	}

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

	_, err := db.Exec(memoryInsertConflictQuery(),
		mem.ID, mem.ProjectID, mem.Category, mem.What, mem.Why, mem.WherePath, mem.Learned,
		mem.GitBranch, mem.GitCommit, mem.Author, mem.Impact, mem.ErrorsFaced, mem.NextSteps,
		mem.SessionID, mem.TopicKey, mem.RevisionCount, mem.DuplicateCount, mem.LastSeenAt,
		mem.NormalizedHash, mem.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to save memory: %w", err)
	}
	return mem, nil
}

func SearchMemories(db *sql.DB, projectID string, searchTerm string, category string, limit int) ([]*Memory, error) {
	return SearchMemoriesScoped(db, projectID, searchTerm, category, "", limit)
}

func SearchMemoriesScoped(db *sql.DB, projectID string, searchTerm string, category string, pathFilter string, limit int) ([]*Memory, error) {
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
		if pathFilter != "" {
			query += " AND (where_path LIKE ? ESCAPE '\\' OR where_path = ?)"
			args = append(args, "%"+pathFilter+"%", pathFilter)
		}
		query += " ORDER BY created_at DESC"
	} else {
		searchTerm = sanitizeFTS5Query(searchTerm)
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
		if pathFilter != "" {
			query += " AND (m.where_path LIKE ? ESCAPE '\\' OR m.where_path = ?)"
			args = append(args, "%"+pathFilter+"%", pathFilter)
		}
		query += " ORDER BY bm25(memories_fts, 10.0, 5.0, 2.0)"
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

func memoryInsertConflictQuery() string {
	return `
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
}

func sanitizeFTS5Query(term string) string {
	tokens := strings.Fields(term)
	if len(tokens) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(tokens))
	for _, t := range tokens {
		cleaned := strings.ReplaceAll(t, `"`, ``)
		quoted = append(quoted, `"`+cleaned+`"`)
	}
	return strings.Join(quoted, " ")
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

func GetTimeline(db *sql.DB, projectID, obsID string, before, after int) (previous, next []*Memory, err error) {
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
		for i, j := 0, len(previous)-1; i < j; i, j = i+1, j-1 {
			previous[i], previous[j] = previous[j], previous[i]
		}
	}

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

func SaveJudgment(db *sql.DB, projectID, sourceID, targetID, relationType, reason, judgedBy string) (*MemoryRelation, error) {
	if sourceID == targetID {
		return nil, errors.New("cannot create a relation between a memory and itself")
	}
	id := uuid.New().String()[:8]
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

	rels, _ := GetRelations(db, projectID, id1)
	for _, r := range rels {
		if (r.SourceID == id1 && r.TargetID == id2) || (r.SourceID == id2 && r.TargetID == id1) {
			sb.WriteString(fmt.Sprintf("\n**Existing relation:** `%s` — %s\n", r.RelationType, r.Reason))
			break
		}
	}
	return sb.String(), nil
}

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

		relCount, _ := CountRelations(db, projectID, r.Memory.ID)
		r.RelationCount = relCount

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

	if _, err := db.Exec("UPDATE memories SET deleted_at=? WHERE project_id=? AND deleted_at IS NULL", time.Now(), id); err != nil {
		return fmt.Errorf("failed to soft-delete memories: %w", err)
	}
	if _, err := db.Exec("DELETE FROM sessions WHERE project_id=?", id); err != nil {
		return fmt.Errorf("failed to delete sessions: %w", err)
	}
	if _, err := db.Exec("DELETE FROM memory_relations WHERE project_id=?", id); err != nil {
		return fmt.Errorf("failed to delete relations: %w", err)
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM projects WHERE id=?", id).Scan(&n); err != nil {
		return fmt.Errorf("failed to check project: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("project %s not found", id)
	}
	return nil
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
