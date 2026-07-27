package db

import (
	"database/sql"
	"fmt"
)

const schema = `
CREATE TABLE IF NOT EXISTS projects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    path TEXT UNIQUE NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS memories (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    category TEXT NOT NULL,
    what TEXT NOT NULL,
    why TEXT NOT NULL,
    where_path TEXT,
    learned TEXT NOT NULL,
    git_branch TEXT,
    git_commit TEXT,
    author TEXT,
    impact TEXT,
    errors_faced TEXT,
    next_steps TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE
);

CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
    what,
    why,
    learned,
    content=memories,
    content_rowid=rowid
);

CREATE TRIGGER IF NOT EXISTS memories_ai AFTER INSERT ON memories BEGIN
    INSERT INTO memories_fts(rowid, what, why, learned) VALUES (new.rowid, new.what, new.why, new.learned);
END;

CREATE TRIGGER IF NOT EXISTS memories_ad AFTER DELETE ON memories BEGIN
    INSERT INTO memories_fts(memories_fts, rowid, what, why, learned) VALUES('delete', old.rowid, old.what, old.why, old.learned);
END;

CREATE TRIGGER IF NOT EXISTS memories_au AFTER UPDATE ON memories BEGIN
    INSERT INTO memories_fts(memories_fts, rowid, what, why, learned) VALUES('delete', old.rowid, old.what, old.why, old.learned);
    INSERT INTO memories_fts(rowid, what, why, learned) VALUES (new.rowid, new.what, new.why, new.learned);
END;

CREATE TABLE IF NOT EXISTS graph_nodes (
    project_id TEXT NOT NULL,
    id TEXT NOT NULL,
    node_type TEXT NOT NULL,
    label TEXT NOT NULL,
    path TEXT NOT NULL,
    metadata TEXT,
    PRIMARY KEY(project_id, id),
    FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS graph_edges (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    target_id TEXT NOT NULL,
    relation_type TEXT NOT NULL,
    confidence TEXT NOT NULL DEFAULT 'EXTRACTED',
    source_location TEXT,
    FOREIGN KEY(project_id, source_id) REFERENCES graph_nodes(project_id, id) ON DELETE CASCADE,
    FOREIGN KEY(project_id, target_id) REFERENCES graph_nodes(project_id, id) ON DELETE CASCADE,
    FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE,
    UNIQUE(project_id, source_id, target_id, relation_type)
);

CREATE TABLE IF NOT EXISTS graph_files_meta (
    project_id TEXT NOT NULL,
    path TEXT NOT NULL,
    mtime_ms INTEGER NOT NULL,
    size INTEGER NOT NULL,
    PRIMARY KEY(project_id, path),
    FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    goal TEXT,
    directory TEXT,
    started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    ended_at DATETIME,
    summary TEXT,
    status TEXT DEFAULT 'active',
    FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS memory_relations (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    target_id TEXT NOT NULL,
    relation_type TEXT NOT NULL,
    status TEXT DEFAULT 'pending',
    score REAL,
    reason TEXT,
    judged_by TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE,
    FOREIGN KEY(source_id) REFERENCES memories(id) ON DELETE CASCADE,
    FOREIGN KEY(target_id) REFERENCES memories(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_graph_edges_source ON graph_edges(project_id, source_id);
CREATE INDEX IF NOT EXISTS idx_graph_edges_target ON graph_edges(project_id, target_id);
CREATE INDEX IF NOT EXISTS idx_memories_project_created ON memories(project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_memories_project_category ON memories(project_id, category);
`

func applyMigrations(db *sql.DB) error {
	rows, err := db.Query("PRAGMA table_info(graph_nodes)")
	if err == nil {
		defer rows.Close()
		var countPK int
		for rows.Next() {
			var cid int
			var name string
			var typeVal string
			var notnull int
			var dfltVal interface{}
			var pk int
			if errScan := rows.Scan(&cid, &name, &typeVal, &notnull, &dfltVal, &pk); errScan == nil {
				if pk > 0 {
					countPK++
				}
			}
		}
		if countPK == 1 {
			_, _ = db.Exec("DROP TABLE IF EXISTS graph_edges")
			_, _ = db.Exec("DROP TABLE IF EXISTS graph_nodes")
		}
	}

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("failed to run database migration schema: %w", err)
	}

	columnChecks := map[string]string{
		"git_branch":       "ALTER TABLE memories ADD COLUMN git_branch TEXT;",
		"git_commit":       "ALTER TABLE memories ADD COLUMN git_commit TEXT;",
		"author":           "ALTER TABLE memories ADD COLUMN author TEXT;",
		"impact":           "ALTER TABLE memories ADD COLUMN impact TEXT;",
		"errors_faced":     "ALTER TABLE memories ADD COLUMN errors_faced TEXT;",
		"next_steps":       "ALTER TABLE memories ADD COLUMN next_steps TEXT;",
		"session_id":       "ALTER TABLE memories ADD COLUMN session_id TEXT;",
		"topic_key":        "ALTER TABLE memories ADD COLUMN topic_key TEXT;",
		"revision_count":   "ALTER TABLE memories ADD COLUMN revision_count INTEGER DEFAULT 1;",
		"duplicate_count":  "ALTER TABLE memories ADD COLUMN duplicate_count INTEGER DEFAULT 0;",
		"last_seen_at":     "ALTER TABLE memories ADD COLUMN last_seen_at DATETIME;",
		"normalized_hash":  "ALTER TABLE memories ADD COLUMN normalized_hash TEXT;",
		"deleted_at":       "ALTER TABLE memories ADD COLUMN deleted_at DATETIME;",
	}
	for col, alterStmt := range columnChecks {
		var exists bool
		rows, err := db.Query("PRAGMA table_info(memories)")
		if err == nil {
			for rows.Next() {
				var cid int
				var name string
				var typeVal string
				var notnull int
				var dfltVal interface{}
				var pk int
				if errScan := rows.Scan(&cid, &name, &typeVal, &notnull, &dfltVal, &pk); errScan == nil {
					if name == col {
						exists = true
						break
					}
				}
			}
			rows.Close()
		} else {
			return fmt.Errorf("failed to query table info for memories: %w", err)
		}
		if !exists {
			if _, errAlter := db.Exec(alterStmt); errAlter != nil {
				return fmt.Errorf("failed to add column %s to memories: %w", col, errAlter)
			}
		}
	}

	for _, col := range []string{"confidence", "source_location"} {
		var exists bool
		rows, err := db.Query("PRAGMA table_info(graph_edges)")
		if err == nil {
			for rows.Next() {
				var cid int
				var name string
				var typeVal string
				var notnull int
				var dfltVal interface{}
				var pk int
				if errScan := rows.Scan(&cid, &name, &typeVal, &notnull, &dfltVal, &pk); errScan == nil {
					if name == col {
						exists = true
						break
					}
				}
			}
			rows.Close()
		} else {
			return fmt.Errorf("failed to query table info for graph_edges: %w", err)
		}
		if !exists {
			var alterStmt string
			if col == "confidence" {
				alterStmt = "ALTER TABLE graph_edges ADD COLUMN confidence TEXT NOT NULL DEFAULT 'EXTRACTED';"
			} else {
				alterStmt = "ALTER TABLE graph_edges ADD COLUMN source_location TEXT;"
			}
			if _, errAlter := db.Exec(alterStmt); errAlter != nil {
				return fmt.Errorf("failed to add column %s to graph_edges: %w", col, errAlter)
			}
		}
	}

	for _, col := range []string{"status", "score"} {
		var exists bool
		rows, err := db.Query("PRAGMA table_info(memory_relations)")
		if err == nil {
			for rows.Next() {
				var cid int
				var name string
				var typeVal string
				var notnull int
				var dfltVal interface{}
				var pk int
				if errScan := rows.Scan(&cid, &name, &typeVal, &notnull, &dfltVal, &pk); errScan == nil {
					if name == col {
						exists = true
						break
					}
				}
			}
			rows.Close()
		} else {
			return fmt.Errorf("failed to query table info for memory_relations: %w", err)
		}
		if !exists {
			var alterStmt string
			if col == "status" {
				alterStmt = "ALTER TABLE memory_relations ADD COLUMN status TEXT DEFAULT 'pending';"
			} else {
				alterStmt = "ALTER TABLE memory_relations ADD COLUMN score REAL;"
			}
			if _, errAlter := db.Exec(alterStmt); errAlter != nil {
				return fmt.Errorf("failed to add column %s to memory_relations: %w", col, errAlter)
			}
		}
	}

	postIndexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_memories_topic ON memories(project_id, topic_key);",
		"CREATE INDEX IF NOT EXISTS idx_memories_hash ON memories(project_id, normalized_hash);",
		"CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project_id, started_at DESC);",
		"CREATE INDEX IF NOT EXISTS idx_memory_relations_source ON memory_relations(project_id, source_id);",
		"CREATE INDEX IF NOT EXISTS idx_memory_relations_target ON memory_relations(project_id, target_id);",
	}
	for _, idx := range postIndexes {
		if _, err := db.Exec(idx); err != nil {
			return fmt.Errorf("failed to create index %s: %w", idx, err)
		}
	}

	return nil
}
