package db

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
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
    category TEXT NOT NULL, -- 'bugfix' | 'architecture' | 'standard' | 'decision' | 'journal' | 'postmortem'
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

-- Triggers to keep FTS5 virtual table synced with memories table
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
    node_type TEXT NOT NULL, -- 'file' | 'module' | 'component' | 'service' | 'function' | 'class'
    label TEXT NOT NULL,
    path TEXT NOT NULL,
    metadata TEXT, -- JSON payload stored as string
    PRIMARY KEY(project_id, id),
    FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS graph_edges (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    target_id TEXT NOT NULL,
    relation_type TEXT NOT NULL, -- 'imports' | 'calls' | 'depends_on'
    FOREIGN KEY(project_id, source_id) REFERENCES graph_nodes(project_id, id) ON DELETE CASCADE,
    FOREIGN KEY(project_id, target_id) REFERENCES graph_nodes(project_id, id) ON DELETE CASCADE,
    FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE,
    UNIQUE(project_id, source_id, target_id, relation_type)
);
`

// InitDB initializes the SQLite database and runs all migrations.
func InitDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database at %s: %w", dbPath, err)
	}

	// Enable foreign key constraints
	if _, err := db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// Enable Write-Ahead Logging (WAL) mode for better concurrency
	if _, err := db.Exec("PRAGMA journal_mode = WAL;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	// Migrate if old single-column primary key schema is detected
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
		// If table exists but only has 1 primary key column (id), it's the old schema
		if countPK == 1 {
			// Drop old tables to migrate to composite primary keys
			_, _ = db.Exec("DROP TABLE IF EXISTS graph_edges")
			_, _ = db.Exec("DROP TABLE IF EXISTS graph_nodes")
		}
	}

	// Execute migrations schema
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run database migration schema: %w", err)
	}

	// Migrate memories schema if missing columns
	columnChecks := map[string]string{
		"git_branch":   "ALTER TABLE memories ADD COLUMN git_branch TEXT;",
		"git_commit":   "ALTER TABLE memories ADD COLUMN git_commit TEXT;",
		"author":       "ALTER TABLE memories ADD COLUMN author TEXT;",
		"impact":       "ALTER TABLE memories ADD COLUMN impact TEXT;",
		"errors_faced": "ALTER TABLE memories ADD COLUMN errors_faced TEXT;",
		"next_steps":   "ALTER TABLE memories ADD COLUMN next_steps TEXT;",
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
		}
		if !exists {
			if _, errAlter := db.Exec(alterStmt); errAlter != nil {
				fmt.Printf("Warning: failed to add column %s to memories: %v\n", col, errAlter)
			}
		}
	}

	return db, nil
}

// RegisterProject inserts or updates a project in the registry.
func RegisterProject(db *sql.DB, id, name, path string) error {
	query := `
	INSERT INTO projects (id, name, path)
	VALUES (?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		name = excluded.name,
		path = excluded.path;
	`
	_, err := db.Exec(query, id, name, path)
	if err != nil {
		return fmt.Errorf("failed to register project %s: %w", name, err)
	}
	return nil
}
