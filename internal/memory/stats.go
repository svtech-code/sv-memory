package memory

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Stats holds aggregate statistics about project memories.
type Stats struct {
	TotalMemories   int            `json:"total_memories"`
	DeletedMemories int            `json:"deleted_memories"`
	ByCategory      map[string]int `json:"by_category"`
	TotalSessions   int            `json:"total_sessions"`
	ActiveSessions  int            `json:"active_sessions"`
	TotalRelations  int            `json:"total_relations"`
	Recent24h       int            `json:"recent_24h"`
}

// DiagnosticsResult holds a single diagnostic check outcome.
type DiagnosticsResult struct {
	Check   string `json:"check"`
	Status  string `json:"status"` // "pass" | "warn" | "fail"
	Message string `json:"message"`
}

// ProjectInfo holds summary data for a registered project.
type ProjectInfo struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Path         string `json:"path"`
	MemoryCount  int    `json:"memory_count"`
	SessionCount int    `json:"session_count"`
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

	cutoff := time.Now().Add(-24 * time.Hour)
	if err := db.QueryRow("SELECT COUNT(*) FROM memories WHERE project_id = ? AND deleted_at IS NULL AND created_at > ?", projectID, cutoff).Scan(&stats.Recent24h); err != nil {
		return nil, fmt.Errorf("failed to count recent memories: %w", err)
	}

	return stats, nil
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
	var srcName, tgtName string
	if srcErr := db.QueryRow("SELECT name FROM projects WHERE id=?", sourceID).Scan(&srcName); srcErr != nil {
		return 0, 0, fmt.Errorf("source project %s not found", sourceID)
	}
	if tgtErr := db.QueryRow("SELECT name FROM projects WHERE id=?", targetID).Scan(&tgtName); tgtErr != nil {
		return 0, 0, fmt.Errorf("target project %s not found", targetID)
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	res, err := tx.Exec("UPDATE memories SET project_id=? WHERE project_id=?", targetID, sourceID)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to move memories: %w", err)
	}
	memN, _ := res.RowsAffected()
	movedMemories = int(memN)

	res, err = tx.Exec("UPDATE sessions SET project_id=? WHERE project_id=?", targetID, sourceID)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to move sessions: %w", err)
	}
	sessN, _ := res.RowsAffected()
	movedSessions = int(sessN)

	if _, err := tx.Exec("UPDATE memory_relations SET project_id=? WHERE project_id=?", targetID, sourceID); err != nil {
		return 0, 0, fmt.Errorf("failed to move relations: %w", err)
	}

	if _, err := tx.Exec("UPDATE graph_nodes SET project_id=? WHERE project_id=?", targetID, sourceID); err != nil {
		return 0, 0, fmt.Errorf("failed to move graph nodes: %w", err)
	}

	if _, err := tx.Exec("UPDATE graph_edges SET project_id=? WHERE project_id=?", targetID, sourceID); err != nil {
		return 0, 0, fmt.Errorf("failed to move graph edges: %w", err)
	}

	if _, err := tx.Exec("UPDATE graph_files_meta SET project_id=? WHERE project_id=?", targetID, sourceID); err != nil {
		return 0, 0, fmt.Errorf("failed to move graph file metadata: %w", err)
	}

	if _, err := tx.Exec("DELETE FROM projects WHERE id=?", sourceID); err != nil {
		return 0, 0, fmt.Errorf("failed to delete source project: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}

	return movedMemories, movedSessions, nil
}

// RunDiagnostics performs read-only health checks on the project setup.
// Returns a list of check results with status pass/warn/fail.
func RunDiagnostics(db *sql.DB, projectID, projPath, dbPath string) []DiagnosticsResult {
	var results []DiagnosticsResult

	add := func(check, status, msg string) {
		results = append(results, DiagnosticsResult{Check: check, Status: status, Message: msg})
	}

	if _, err := os.Stat(dbPath); err == nil {
		add("database_file", "pass", fmt.Sprintf("Database file found at %s", dbPath))
	} else {
		add("database_file", "fail", fmt.Sprintf("Database file not found at %s: %v", dbPath, err))
		return results
	}

	if err := db.Ping(); err != nil {
		add("database_connection", "fail", fmt.Sprintf("Cannot ping database: %v", err))
		return results
	}
	add("database_connection", "pass", "Database connection is alive")

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

	var projCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM projects WHERE id=?", projectID).Scan(&projCount); err != nil {
		add("project_registered", "fail", fmt.Sprintf("Error querying project: %v", err))
	} else if projCount > 0 {
		add("project_registered", "pass", "Project is registered in database")
	} else {
		add("project_registered", "fail", "Project is not registered — run 'sv-memory init'")
	}

	tmpFile := filepath.Join(projPath, ".sv-memory-write-test")
	if err := os.WriteFile(tmpFile, []byte{}, 0644); err != nil {
		add("project_path_writable", "fail", fmt.Sprintf("Project path is not writable: %v", err))
	} else {
		os.Remove(tmpFile)
		add("project_path_writable", "pass", "Project path is writable")
	}

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
		if _, legacyErr := os.Stat(filepath.Join(projPath, ".sv-memory", "memories.json")); legacyErr == nil {
			add("chunk_directory", "warn", "Using legacy memories.json (no chunk directory)")
		} else {
			add("chunk_directory", "warn", "No sync directory found — run 'sv-memory sync' after first save")
		}
	} else {
		add("chunk_directory", "warn", fmt.Sprintf("Cannot stat chunk directory: %v", err))
	}

	var ftsCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM memories_fts").Scan(&ftsCount); err != nil {
		add("fts5_healthy", "fail", fmt.Sprintf("FTS5 query failed: %v", err))
	} else {
		add("fts5_healthy", "pass", fmt.Sprintf("FTS5 is healthy (%d indexed rows)", ftsCount))
	}

	return results
}
