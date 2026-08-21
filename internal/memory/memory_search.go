package memory

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/svtech-code/sv-memory/internal/security"
)

func SearchMemoriesBySessionCompact(db *sql.DB, projectID, sessionID string, limit int) ([]*MemorySearchResult, error) {
	query := "SELECT " + compactColumns + `
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
	return SearchMemoriesCompactScoped(db, projectID, searchTerm, category, "", "all", limit, offset)
}

// SearchPinnedMemories returns pinned memories for a project, most recently
// created first. Pinned memories surface first in session context so key
// decisions stay visible regardless of session recency.
func SearchPinnedMemories(db *sql.DB, projectID string, limit int) ([]*MemorySearchResult, error) {
	query := "SELECT " + compactColumns + `
	FROM memories
	WHERE project_id = ? AND pinned = 1 AND deleted_at IS NULL
	ORDER BY created_at DESC`
	var args []interface{}
	args = append(args, projectID)
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to search pinned memories: %w", err)
	}
	defer rows.Close()
	return scanCompactMemories(rows)
}

// SearchMemoriesCompactScoped searches memories scoped to a project with an
// optional category and path filter. matchMode is "all" (every token must
// match, default) or "any" (broader recall — a memory matching one or more
// tokens is returned).
func SearchMemoriesCompactScoped(db *sql.DB, projectID string, searchTerm string, category string, pathFilter string, matchMode string, limit int, offset int) ([]*MemorySearchResult, error) {
	return searchMemoriesCompact(db, projectID, searchTerm, category, pathFilter, matchMode, nil, limit, offset)
}

// SearchMemoriesByPaths is the graph-aware variant of SearchMemoriesCompactScoped:
// it matches memories whose where_path equals/contains pathFilter OR belongs to
// the given paths set (a graph community), so a module search surfaces memories
// for the whole community in one call. When paths is empty it behaves exactly
// like the plain path-filtered search.
func SearchMemoriesByPaths(db *sql.DB, projectID string, searchTerm string, category string, matchMode string, pathFilter string, paths []string, limit int, offset int) ([]*MemorySearchResult, error) {
	return searchMemoriesCompact(db, projectID, searchTerm, category, pathFilter, matchMode, paths, limit, offset)
}

// searchMemoriesCompact is the shared core of SearchMemoriesCompactScoped and
// SearchMemoriesByPaths: a project-scoped FTS5 search with optional category,
// path, and graph-community (paths) filters. When paths is non-empty the path
// predicate ORs the exact where_path IN (...) set with the LIKE filter so a
// module search also surfaces memories from the whole community.
func searchMemoriesCompact(db *sql.DB, projectID string, searchTerm string, category string, pathFilter string, matchMode string, paths []string, limit int, offset int) ([]*MemorySearchResult, error) {
	pathFilter = sanitizePathFilter(pathFilter)

	// Build the path predicate: keep the precise filter and OR in the community
	// paths. Placeholders are appended after the filter so arg order stays stable.
	var pathClause string
	var pathArgs []interface{}
	if len(paths) > 0 {
		ph := make([]string, len(paths))
		for i := range paths {
			ph[i] = "?"
			pathArgs = append(pathArgs, paths[i])
		}
		in := "where_path IN (" + strings.Join(ph, ", ") + ")"
		if pathFilter != "" {
			pathClause = " AND ((where_path LIKE ? ESCAPE '\\' OR where_path = ?) OR " + in + ")"
			pathArgs = append([]interface{}{"%" + pathFilter + "%", pathFilter}, pathArgs...)
		} else {
			pathClause = " AND " + in
		}
	} else if pathFilter != "" {
		pathClause = " AND (where_path LIKE ? ESCAPE '\\' OR where_path = ?)"
		pathArgs = append(pathArgs, "%"+pathFilter+"%", pathFilter)
	}

	var query string
	var args []interface{}

	if searchTerm == "" {
		query = "SELECT " + compactColumns + `
		FROM memories
		WHERE project_id = ? AND deleted_at IS NULL` + pathClause
		args = append(args, projectID)
		args = append(args, pathArgs...)
		if category != "" {
			query += " AND category = ?"
			args = append(args, category)
		}
		query += " ORDER BY created_at DESC"
	} else {
		searchTerm = sanitizeFTS5QueryWithMode(searchTerm, matchMode)
		// An empty sanitized query (e.g. only quotes) must not reach `MATCH ''`,
		// which raises an FTS5 syntax error. Treat it as "no results".
		if searchTerm == "" {
			return nil, nil
		}
		query = `
		SELECT m.id, m.category, m.what,
			m.topic_key, m.revision_count, m.duplicate_count, m.created_at,
			` + triFactorScoreExpr + ` AS score
		FROM memories m
		JOIN memories_fts f ON m.rowid = f.rowid
		WHERE m.project_id = ? AND memories_fts MATCH ? AND m.deleted_at IS NULL` + pathClause
		args = append(args, projectID, searchTerm)
		args = append(args, pathArgs...)
		if category != "" {
			query += " AND m.category = ?"
			args = append(args, category)
		}
		query += " ORDER BY " + triFactorScoreExpr
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

	if searchTerm == "" {
		return scanCompactMemories(rows)
	}
	return scanCompactMemoriesScored(rows)
}

// scanCompactMemoriesScored is the FTS5 variant of scanCompactMemories; it also
// reads the BM25 score column so the agent can see per-result relevance.
func scanCompactMemoriesScored(rows *sql.Rows) ([]*MemorySearchResult, error) {
	return scanCompactMemoriesWithScore(rows, true)
}

func scanCompactMemories(rows *sql.Rows) ([]*MemorySearchResult, error) {
	return scanCompactMemoriesWithScore(rows, false)
}

// scanCompactMemoriesWithScore scans compact memory rows (id, category, what,
// topic_key, revision_count, duplicate_count, created_at) and, when withScore
// is set, an additional trailing BM25 score column.
func scanCompactMemoriesWithScore(rows *sql.Rows, withScore bool) ([]*MemorySearchResult, error) {
	var results []*MemorySearchResult
	for rows.Next() {
		var r MemorySearchResult
		var createdAtStr string
		var topicKey sql.NullString
		var revisionCount, duplicateCount sql.NullInt64
		var err error
		if withScore {
			var score sql.NullFloat64
			err = rows.Scan(&r.ID, &r.Category, &r.What, &topicKey, &revisionCount, &duplicateCount, &createdAtStr, &score)
			if err == nil && score.Valid {
				r.Score = score.Float64
			}
		} else {
			err = rows.Scan(&r.ID, &r.Category, &r.What, &topicKey, &revisionCount, &duplicateCount, &createdAtStr)
		}
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
		r.CreatedAt = parseTimeOrNow(createdAtStr)
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
	if limit < 1 {
		limit = 1
	}
	tokens := tokenizeTitle(title)
	if len(tokens) == 0 {
		return nil, nil
	}
	// Quote every token so FTS5 treats hyphens/underscores literally instead of
	// interpreting "-" as the NOT operator or "_" as a wildcard.
	quoted := make([]string, len(tokens))
	for i, t := range tokens {
		quoted[i] = `"` + t + `"`
	}
	ftsQuery := strings.Join(quoted, " OR ")

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

func SearchMemories(db *sql.DB, projectID string, searchTerm string, category string, limit int) ([]*Memory, error) {
	var query string
	var args []interface{}

	if searchTerm == "" {
		query = "SELECT " + memoryColumns + `
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
		searchTerm = sanitizeFTS5Query(searchTerm)
		// An empty sanitized query (e.g. only quotes) must not reach `MATCH ''`,
		// which raises an FTS5 syntax error. Treat it as "no results".
		if searchTerm == "" {
			return nil, nil
		}
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
		query += " ORDER BY bm25(memories_fts, " + bm25Weights + ")"
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

	return scanMemories(rows)
}

// sanitizeFTS5Query tokenizes a raw user query into a safe FTS5 MATCH
// expression. Every token is quoted (neutralizing operator words and special
// characters) and tokens of two or more characters get a prefix wildcard (e.g.
// "component" -> "component*") so inflections and word variants are matched
// too. Tokens that reduce to nothing after quote-stripping (e.g. a query made
// of only double quotes) are dropped; if no tokens survive, "" is returned and
// callers must treat it as "no results" instead of running an empty FTS5 MATCH
// expression, which raises an FTS5 syntax error.
func sanitizeFTS5Query(term string) string {
	return sanitizeFTS5QueryWithMode(term, "all")
}

// sanitizeFTS5QueryWithMode tokenizes a raw user query into a safe FTS5 MATCH
// expression. matchMode controls the operator between tokens: "all" (default)
// requires every token to match (implicit AND), "any" broadens recall so a
// memory matching one or more tokens is returned (explicit OR).
func sanitizeFTS5QueryWithMode(term string, matchMode string) string {
	tokens := strings.Fields(term)
	if len(tokens) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(tokens))
	for _, t := range tokens {
		cleaned := strings.ReplaceAll(t, `"`, ``)
		if cleaned == "" {
			continue
		}
		if len([]rune(cleaned)) >= 2 {
			quoted = append(quoted, `"`+cleaned+`"*`)
		} else {
			quoted = append(quoted, `"`+cleaned+`"`)
		}
	}
	if len(quoted) == 0 {
		return ""
	}
	if matchMode == "any" {
		return strings.Join(quoted, " OR ")
	}
	return strings.Join(quoted, " ")
}

// GetTimelineCompact is a token-efficient variant of the previous
// GetTimeline: it only loads the columns needed to render a timeline line
// (id, category, what, created_at) instead of the full memory row, so
// timeline responses stay lean.
func GetTimelineCompact(db *sql.DB, projectID, obsID string, before, after int) (previous, next []*MemorySearchResult, err error) {
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

	scanCompact := func(rows *sql.Rows) ([]*MemorySearchResult, error) {
		var results []*MemorySearchResult
		for rows.Next() {
			var r MemorySearchResult
			var createdAtStr string
			if err := rows.Scan(&r.ID, &r.Category, &r.What, &createdAtStr); err != nil {
				return nil, fmt.Errorf("failed scanning timeline row: %w", err)
			}
			r.What = security.SanitizeText(r.What)
			r.CreatedAt = parseTimeOrNow(createdAtStr)
			results = append(results, &r)
		}
		return results, rows.Err()
	}

	if before > 0 {
		rows, qErr := db.Query(`
		SELECT id, category, what, created_at
		FROM memories WHERE project_id = ? AND created_at < ? AND id != ? AND deleted_at IS NULL
		ORDER BY created_at DESC LIMIT ?`, projectID, targetTime, obsID, before)
		if qErr != nil {
			return nil, nil, qErr
		}
		defer rows.Close()
		previous, qErr = scanCompact(rows)
		if qErr != nil {
			return nil, nil, qErr
		}
		for i, j := 0, len(previous)-1; i < j; i, j = i+1, j-1 {
			previous[i], previous[j] = previous[j], previous[i]
		}
	}

	if after > 0 {
		rows, qErr := db.Query(`
		SELECT id, category, what, created_at
		FROM memories WHERE project_id = ? AND created_at > ? AND id != ? AND deleted_at IS NULL
		ORDER BY created_at ASC LIMIT ?`, projectID, targetTime, obsID, after)
		if qErr != nil {
			return nil, nil, qErr
		}
		defer rows.Close()
		next, qErr = scanCompact(rows)
		if qErr != nil {
			return nil, nil, qErr
		}
	}
	return previous, next, nil
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
		mem.CreatedAt = parseTimeOrNow(createdAtStr)
		memories = append(memories, &mem)
	}
	return memories, rows.Err()
}
