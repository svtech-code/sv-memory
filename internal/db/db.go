package db

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"time"

	_ "modernc.org/sqlite"
)

// debugEnabled reports whether SV_MEMORY_DEBUG env var is set to a truthy value.
// Used by callers to emit stderr timing logs without runtime overhead on hot path.
func debugEnabled() bool {
	v := os.Getenv("SV_MEMORY_DEBUG")
	if v == "" {
		return false
	}
	if b, err := strconv.ParseBool(v); err == nil {
		return b
	}
	return true
}

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
    category TEXT NOT NULL, -- 'bugfix' | 'architecture' | 'standard' | 'decision' | 'journal' | 'postmortem' | 'discussion' | 'idea' | 'qa'
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

-- Auxiliary table used by the incremental graph sync to avoid full rebuilds.
-- Records per-file mtime/size so we can detect whose imports need reparsing.
CREATE TABLE IF NOT EXISTS graph_files_meta (
    project_id TEXT NOT NULL,
    path TEXT NOT NULL,
    mtime_ms INTEGER NOT NULL,
    size INTEGER NOT NULL,
    PRIMARY KEY(project_id, path),
    FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE
);

-- Performance indexes (Fase 1 quick wins):
-- graph_edges are queried by (project_id, source_id) and (project_id, target_id)
-- during BFS in querySubGraph. Without these, SQLite does a full table scan per hop.
CREATE INDEX IF NOT EXISTS idx_graph_edges_source ON graph_edges(project_id, source_id);
CREATE INDEX IF NOT EXISTS idx_graph_edges_target ON graph_edges(project_id, target_id);

-- memories filtering by project + created_at (default listing order) and
-- project + category (filter path). Both patterns hit the search handler.
CREATE INDEX IF NOT EXISTS idx_memories_project_created ON memories(project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_memories_project_category ON memories(project_id, category);
`

// InitDB initializes the SQLite database and runs all migrations.
// The returned *sql.DB is tuned for writes (single connection, safe for
// transactional inserts). Callers that need concurrent read scaling should
// use OpenReader on the same path or use NewDBPool directly.
func InitDB(dbPath string) (*sql.DB, error) {
	db, err := openDBWithTuning(dbPath, true)
	if err != nil {
		return nil, fmt.Errorf("failed to open database at %s: %w", dbPath, err)
	}

	if err := applyMigrations(db); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

// Pool bundles a writer and a reader *sql.DB so the MCP server can scale reads
// while still serializing writes (SQLite + modernc.org/sqlite + WAL).
type Pool struct {
	Writer *sql.DB
	Reader *sql.DB
}

// Close releases both connections.
func (p *Pool) Close() error {
	var wErr, rErr error
	if p.Writer != nil {
		wErr = p.Writer.Close()
	}
	if p.Reader != nil {
		rErr = p.Reader.Close()
	}
	if wErr != nil {
		return wErr
	}
	return rErr
}

// NewDBPool initializes a tuned writer + reader pool over the same database file.
// Writes go through the Writer (MaxOpenConns=1) and concurrent reads from the MCP
// handlers go through the Reader (its read-only flag lets SQLite schedule them in
// parallel under WAL). If opening the reader fails we degrade gracefully to using
// the writer for both.
func NewDBPool(dbPath string) (*Pool, error) {
	w, err := openDBWithTuning(dbPath, true)
	if err != nil {
		return nil, fmt.Errorf("failed to open writer at %s: %w", dbPath, err)
	}
	if err := applyMigrations(w); err != nil {
		w.Close()
		return nil, err
	}

	r, rerr := openDBWithTuning(dbPath, false)
	if rerr != nil {
		// Degrade: return a pool where Reader == Writer. Callers already serialize
		// via single-writer semantics so this still functions correctly, just slower.
		return &Pool{Writer: w, Reader: w}, nil
	}
	return &Pool{Writer: w, Reader: r}, nil
}

// openDBWithTuning opens *sql.DB on modernc.org/sqlite with performance PRAGMAs
// appropriate for the role (writer requires MaxOpenConns=1; reader scales up).
// isWriter selects connection-pool sizing: writer=1 to serialize writes under WAL,
// reader=runtime.NumCPU() to let concurrent BFS / FTS reads scale.
func openDBWithTuning(dbPath string, isWriter bool) (*sql.DB, error) {
	// For readers we open in read-only mode via file URI ?mode=ro so SQLite can
	// schedule multiple readers in parallel without locking concerns under WAL.
	dsn := dbPath
	if !isWriter {
		dsn = "file:" + dbPath + "?mode=ro&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	// Connection-pool sizing.
	if isWriter {
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
	} else {
		// Allow concurrent readers but keep a sane ceiling. Idle conns reused.
		maxReaders := 8
		db.SetMaxOpenConns(maxReaders)
		db.SetMaxIdleConns(maxReaders)
	}
	db.SetConnMaxIdleTime(30 * time.Minute)

	// Common PRAGMAs applied per-connection (SQLite PRAGMAs are per-connection).
	pragmas := []string{
		"PRAGMA foreign_keys = ON;",
		"PRAGMA journal_mode = WAL;",
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA temp_store = MEMORY;",
		// Fase 1 tuning:
		//  ~20MB page cache per connection (negative value = KiB).
		"PRAGMA cache_size = -20000;",
		//  256MB memory-mapped I/O — lets SQLite avoid read() syscalls for the
		//  graph tables and FTS5 shadow tables (big win on cold reads).
		"PRAGMA mmap_size = 268435456;",
		//  Wait up to 5s on a locked db instead of failing immediately.
		"PRAGMA busy_timeout = 5000;",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to apply pragma %q: %w", p, err)
		}
	}

	return db, nil
}

// applyMigrations executes the schema (idempotent via IF NOT EXISTS) and runs
// the legacy column-add migration used to upgrade old sv-memory databases.
func applyMigrations(db *sql.DB) error {
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
		return fmt.Errorf("failed to run database migration schema: %w", err)
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

	return nil
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
