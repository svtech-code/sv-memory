package db

import (
	"database/sql"
	"fmt"
	"sort"
)

const graphNodesDDL = `CREATE TABLE IF NOT EXISTS graph_nodes (
    project_id TEXT NOT NULL,
    id TEXT NOT NULL,
    node_type TEXT NOT NULL,
    label TEXT NOT NULL,
    path TEXT NOT NULL,
    metadata TEXT,
    PRIMARY KEY(project_id, id),
    FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE
);`

const graphEdgesDDL = `CREATE TABLE IF NOT EXISTS graph_edges (
    id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    target_id TEXT NOT NULL,
    relation_type TEXT NOT NULL,
    confidence TEXT NOT NULL DEFAULT 'EXTRACTED',
    source_location TEXT,
    PRIMARY KEY(project_id, id),
    FOREIGN KEY(project_id, source_id) REFERENCES graph_nodes(project_id, id) ON DELETE CASCADE,
    FOREIGN KEY(project_id, target_id) REFERENCES graph_nodes(project_id, id) ON DELETE CASCADE,
    FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE,
    UNIQUE(project_id, source_id, target_id, relation_type)
);`

const graphFilesMetaDDL = `CREATE TABLE IF NOT EXISTS graph_files_meta (
    project_id TEXT NOT NULL,
    path TEXT NOT NULL,
    mtime_ms INTEGER NOT NULL,
    size INTEGER NOT NULL,
    PRIMARY KEY(project_id, path),
    FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE
);`

const graphIndexesDDL = `CREATE INDEX IF NOT EXISTS idx_graph_edges_source ON graph_edges(project_id, source_id);
CREATE INDEX IF NOT EXISTS idx_graph_edges_target ON graph_edges(project_id, target_id);`

const schema = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

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
    review_after DATETIME,
    pinned INTEGER NOT NULL DEFAULT 0,
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
` + graphNodesDDL + graphEdgesDDL + graphFilesMetaDDL + graphIndexesDDL + `

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

CREATE INDEX IF NOT EXISTS idx_memories_project_created ON memories(project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_memories_project_category ON memories(project_id, category);
`

type migration struct {
	version int
	name    string
	apply   func(*sql.DB) error
}

var migrations = []migration{
	{1, "initial_schema", applyInitialSchema},
	{2, "legacy_graph_schema", migrateLegacyGraphSchema},
	{3, "add_memory_columns", addMemoryColumns},
	{4, "add_graph_edge_columns", addGraphEdgeColumns},
	{5, "add_memory_relation_columns", addMemoryRelationColumns},
	{6, "create_post_indexes", createPostIndexes},
	{7, "graph_edges_composite_pk", migrateGraphEdgesCompositePK},
	{8, "add_project_scan_meta", addProjectScanMeta},
	{9, "add_session_index", addSessionIndex},
	{10, "add_review_after_and_pinned", addReviewAfterAndPinned},
	{11, "add_last_compaction_at", addLastCompactionAt},
	{12, "add_session_partial_indexes", addSessionPartialIndexes},
	{13, "add_user_prompts", addUserPrompts},
	{14, "add_change_lifecycle", addChangeLifecycle},
	{15, "add_spec_requirements", addSpecRequirements},
}

func applyMigrations(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %w", err)
	}

	for _, m := range migrations {
		var applied int
		err := db.QueryRow("SELECT 1 FROM schema_migrations WHERE version = ?", m.version).Scan(&applied)
		if err == nil {
			continue
		}
		if err != sql.ErrNoRows {
			return fmt.Errorf("failed to check migration %d (%s): %w", m.version, m.name, err)
		}

		if err := m.apply(db); err != nil {
			return fmt.Errorf("migration %d (%s) failed: %w", m.version, m.name, err)
		}

		if _, err := db.Exec("INSERT INTO schema_migrations (version, name) VALUES (?, ?)", m.version, m.name); err != nil {
			return fmt.Errorf("failed to record migration %d (%s): %w", m.version, m.name, err)
		}
	}

	return nil
}

func applyInitialSchema(db *sql.DB) error {
	_, err := db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to run initial schema: %w", err)
	}
	return nil
}

func migrateLegacyGraphSchema(db *sql.DB) error {
	rows, err := db.Query("PRAGMA table_info(graph_nodes)")
	if err != nil {
		// graph_nodes does not exist yet (fresh schema) — nothing to migrate.
		return nil
	}
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
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed reading graph_nodes pragma: %w", err)
	}

	if countPK != 1 {
		return nil
	}

	// Legacy single-column PK shape: the old graph tables cannot hold
	// project-scoped IDs, so they are dropped and immediately recreated with
	// the current composite (project_id, id) schema. graph_files_meta already
	// uses a composite PK and is left untouched.
	if _, err := db.Exec("DROP TABLE IF EXISTS graph_edges"); err != nil {
		return fmt.Errorf("failed dropping legacy graph_edges: %w", err)
	}
	if _, err := db.Exec("DROP TABLE IF EXISTS graph_nodes"); err != nil {
		return fmt.Errorf("failed dropping legacy graph_nodes: %w", err)
	}
	if _, err := db.Exec(graphNodesDDL); err != nil {
		return fmt.Errorf("failed recreating graph_nodes: %w", err)
	}
	if _, err := db.Exec(graphEdgesDDL); err != nil {
		return fmt.Errorf("failed recreating graph_edges: %w", err)
	}
	if _, err := db.Exec(graphIndexesDDL); err != nil {
		return fmt.Errorf("failed recreating graph indexes: %w", err)
	}
	return nil
}

// migrateGraphEdgesCompositePK rebuilds graph_edges with a composite primary
// key (project_id, id) so edge IDs are scoped per project. Previously the id
// column was a global PRIMARY KEY while edge IDs are generated as
// "<sourcePath>-<targetID>-<relationType>" without a project scope; two
// projects with identical relative paths collided and the second project's
// edges were silently dropped by INSERT ... ON CONFLICT DO NOTHING.
func migrateGraphEdgesCompositePK(db *sql.DB) error {
	// Detect the legacy single-column PK shape (id INTEGER PRIMARY KEY or
	// id TEXT PRIMARY KEY). If the table already uses the composite PK
	// (e.g. brand-new installs created from the updated schema), skip.
	pkInfo, err := tablePKInfo(db, "graph_edges")
	if err != nil {
		return err
	}
	if len(pkInfo) >= 2 {
		return nil
	}
	if len(pkInfo) == 1 && pkInfo[0] != "id" {
		return fmt.Errorf("unexpected primary key columns on graph_edges: %v", pkInfo)
	}

	// SQLite cannot alter a table's primary key, so rebuild it. Copy all
	// existing rows; since ids collide across projects the source data may
	// already be missing the losers, but no data is lost here.
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS graph_edges_new (
			id TEXT NOT NULL,
			project_id TEXT NOT NULL,
			source_id TEXT NOT NULL,
			target_id TEXT NOT NULL,
			relation_type TEXT NOT NULL,
			confidence TEXT NOT NULL DEFAULT 'EXTRACTED',
			source_location TEXT,
			PRIMARY KEY(project_id, id),
			FOREIGN KEY(project_id, source_id) REFERENCES graph_nodes(project_id, id) ON DELETE CASCADE,
			FOREIGN KEY(project_id, target_id) REFERENCES graph_nodes(project_id, id) ON DELETE CASCADE,
			FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE,
			UNIQUE(project_id, source_id, target_id, relation_type)
		)`); err != nil {
		return fmt.Errorf("failed creating graph_edges_new: %w", err)
	}

	if _, err := db.Exec(`
		INSERT OR IGNORE INTO graph_edges_new (id, project_id, source_id, target_id, relation_type, confidence, source_location)
		SELECT id, project_id, source_id, target_id, relation_type, confidence, source_location FROM graph_edges`); err != nil {
		return fmt.Errorf("failed copying graph_edges: %w", err)
	}

	if _, err := db.Exec("DROP TABLE IF EXISTS graph_edges"); err != nil {
		return fmt.Errorf("failed dropping legacy graph_edges: %w", err)
	}
	if _, err := db.Exec("ALTER TABLE graph_edges_new RENAME TO graph_edges"); err != nil {
		return fmt.Errorf("failed renaming graph_edges_new: %w", err)
	}

	// Re-create the lookup indexes that lived on the legacy table.
	for _, idx := range []string{
		"CREATE INDEX IF NOT EXISTS idx_graph_edges_source ON graph_edges(project_id, source_id);",
		"CREATE INDEX IF NOT EXISTS idx_graph_edges_target ON graph_edges(project_id, target_id);",
	} {
		if _, err := db.Exec(idx); err != nil {
			return fmt.Errorf("failed recreating graph_edges index: %w", err)
		}
	}
	return nil
}

// tablePKInfo returns the ordered list of PRIMARY KEY column names for a table,
// ordered by the key column ordinal (the order declared in PRIMARY KEY(...)).
func tablePKInfo(db *sql.DB, table string) ([]string, error) {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type pkCol struct {
		ordinal int
		name    string
	}
	var pks []pkCol
	for rows.Next() {
		var cid int
		var name string
		var typeVal string
		var notnull int
		var dfltVal interface{}
		var pk int
		if err := rows.Scan(&cid, &name, &typeVal, &notnull, &dfltVal, &pk); err != nil {
			return nil, err
		}
		if pk > 0 {
			pks = append(pks, pkCol{ordinal: pk, name: name})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(pks, func(i, j int) bool { return pks[i].ordinal < pks[j].ordinal })
	cols := make([]string, 0, len(pks))
	for _, c := range pks {
		cols = append(cols, c.name)
	}
	return cols, nil
}

func columnExists(db *sql.DB, table, col string) (bool, error) {
	allowed := map[string]bool{
		"memories":         true,
		"graph_edges":      true,
		"graph_nodes":      true,
		"memory_relations": true,
		"sessions":         true,
		"projects":         true,
	}
	if !allowed[table] {
		return false, fmt.Errorf("columnExists: unknown table %q", table)
	}
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false, fmt.Errorf("failed to query table info for %s: %w", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var typeVal string
		var notnull int
		var dfltVal interface{}
		var pk int
		if errScan := rows.Scan(&cid, &name, &typeVal, &notnull, &dfltVal, &pk); errScan == nil {
			if name == col {
				return true, nil
			}
		}
	}
	return false, nil
}

func addMemoryColumns(db *sql.DB) error {
	columns := map[string]string{
		"git_branch":      "ALTER TABLE memories ADD COLUMN git_branch TEXT;",
		"git_commit":      "ALTER TABLE memories ADD COLUMN git_commit TEXT;",
		"author":          "ALTER TABLE memories ADD COLUMN author TEXT;",
		"impact":          "ALTER TABLE memories ADD COLUMN impact TEXT;",
		"errors_faced":    "ALTER TABLE memories ADD COLUMN errors_faced TEXT;",
		"next_steps":      "ALTER TABLE memories ADD COLUMN next_steps TEXT;",
		"session_id":      "ALTER TABLE memories ADD COLUMN session_id TEXT;",
		"topic_key":       "ALTER TABLE memories ADD COLUMN topic_key TEXT;",
		"revision_count":  "ALTER TABLE memories ADD COLUMN revision_count INTEGER DEFAULT 1;",
		"duplicate_count": "ALTER TABLE memories ADD COLUMN duplicate_count INTEGER DEFAULT 0;",
		"last_seen_at":    "ALTER TABLE memories ADD COLUMN last_seen_at DATETIME;",
		"normalized_hash": "ALTER TABLE memories ADD COLUMN normalized_hash TEXT;",
		"deleted_at":      "ALTER TABLE memories ADD COLUMN deleted_at DATETIME;",
	}
	for col, alterStmt := range columns {
		exists, err := columnExists(db, "memories", col)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := db.Exec(alterStmt); err != nil {
				return fmt.Errorf("failed to add column %s to memories: %w", col, err)
			}
		}
	}
	return nil
}

func addGraphEdgeColumns(db *sql.DB) error {
	for _, col := range []string{"confidence", "source_location"} {
		exists, err := columnExists(db, "graph_edges", col)
		if err != nil {
			return err
		}
		if !exists {
			var alterStmt string
			if col == "confidence" {
				alterStmt = "ALTER TABLE graph_edges ADD COLUMN confidence TEXT NOT NULL DEFAULT 'EXTRACTED';"
			} else {
				alterStmt = "ALTER TABLE graph_edges ADD COLUMN source_location TEXT;"
			}
			if _, err := db.Exec(alterStmt); err != nil {
				return fmt.Errorf("failed to add column %s to graph_edges: %w", col, err)
			}
		}
	}
	return nil
}

func addMemoryRelationColumns(db *sql.DB) error {
	for _, col := range []string{"status", "score"} {
		exists, err := columnExists(db, "memory_relations", col)
		if err != nil {
			return err
		}
		if !exists {
			var alterStmt string
			if col == "status" {
				alterStmt = "ALTER TABLE memory_relations ADD COLUMN status TEXT DEFAULT 'pending';"
			} else {
				alterStmt = "ALTER TABLE memory_relations ADD COLUMN score REAL;"
			}
			if _, err := db.Exec(alterStmt); err != nil {
				return fmt.Errorf("failed to add column %s to memory_relations: %w", col, err)
			}
		}
	}
	return nil
}

func createPostIndexes(db *sql.DB) error {
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_memories_topic ON memories(project_id, topic_key);",
		"CREATE INDEX IF NOT EXISTS idx_memories_hash ON memories(project_id, normalized_hash);",
		"CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project_id, started_at DESC);",
		"CREATE INDEX IF NOT EXISTS idx_memory_relations_source ON memory_relations(project_id, source_id);",
		"CREATE INDEX IF NOT EXISTS idx_memory_relations_target ON memory_relations(project_id, target_id);",
	}
	for _, idx := range indexes {
		if _, err := db.Exec(idx); err != nil {
			return fmt.Errorf("failed to create index %s: %w", idx, err)
		}
	}
	return nil
}

// addProjectScanMeta adds per-project metadata used to make the pairwise
// conflict scan incremental: last_conflict_scan_at records the timestamp of the
// last applied scan, so subsequent scans only compare newly created memories
// against the existing set instead of re-comparing every O(N²) pair.
func addProjectScanMeta(db *sql.DB) error {
	exists, err := columnExists(db, "projects", "last_conflict_scan_at")
	if err != nil {
		return err
	}
	if !exists {
		if _, err := db.Exec("ALTER TABLE projects ADD COLUMN last_conflict_scan_at DATETIME;"); err != nil {
			return fmt.Errorf("failed to add column last_conflict_scan_at to projects: %w", err)
		}
	}
	return nil
}

// addSessionIndex indexes memories by (project_id, session_id), which is the
// lookup used by GetActiveSession on every sv_mem_save to auto-associate
// memories with the running session. Without it each save scans the table.
func addSessionIndex(db *sql.DB) error {
	if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_memories_session ON memories(project_id, session_id);"); err != nil {
		return fmt.Errorf("failed to create idx_memories_session: %w", err)
	}
	return nil
}

// addReviewAfterAndPinned adds the review_after (decay-driven review due date)
// and pinned (local context priority) columns to the memories table. Both are
// additive and idempotent; fresh installs get them from the base schema.
func addReviewAfterAndPinned(db *sql.DB) error {
	columns := map[string]string{
		"review_after": "ALTER TABLE memories ADD COLUMN review_after DATETIME;",
		"pinned":       "ALTER TABLE memories ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0;",
	}
	for col, alterStmt := range columns {
		exists, err := columnExists(db, "memories", col)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := db.Exec(alterStmt); err != nil {
				return fmt.Errorf("failed to add column %s to memories: %w", col, err)
			}
		}
	}
	return nil
}

// addLastCompactionAt records when the last incremental compaction ran for a
// project, so the background compaction worker only re-processes topic keys
// that changed since then instead of scanning the full history every tick.
func addLastCompactionAt(db *sql.DB) error {
	exists, err := columnExists(db, "projects", "last_compaction_at")
	if err != nil {
		return err
	}
	if !exists {
		if _, err := db.Exec("ALTER TABLE projects ADD COLUMN last_compaction_at DATETIME;"); err != nil {
			return fmt.Errorf("failed to add column last_compaction_at to projects: %w", err)
		}
	}
	return nil
}

// addSessionPartialIndexes adds partial indexes tailored to the two hot session
// lookups. GetActiveSession filters status='active' and orders by started_at
// DESC; GetLastSession filters status='completed' and orders by ended_at DESC.
// The pre-existing idx_sessions_project index covers (project_id, started_at
// DESC) but not the status predicate nor the ended_at sort, so these partial
// indexes serve both the filter and the sort for each lookup directly.
func addSessionPartialIndexes(db *sql.DB) error {
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_sessions_active_started ON sessions(project_id, started_at DESC) WHERE status = 'active';",
		"CREATE INDEX IF NOT EXISTS idx_sessions_completed_ended ON sessions(project_id, ended_at DESC) WHERE status = 'completed';",
	}
	for _, idx := range indexes {
		if _, err := db.Exec(idx); err != nil {
			return fmt.Errorf("failed to create index %s: %w", idx, err)
		}
	}
	return nil
}

// addUserPrompts adds the user_prompts table (captured user prompts, e.g. via
// sv_mem_capture_prompt) plus its FTS5 index, mirroring Engram's user_prompts /
// prompts_fts pair. Prompts are local observations attached to a session and
// are NOT part of the git sync payload in this phase; they live only in the
// local SQLite store so future sessions can recover the user's intent.
func addUserPrompts(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS user_prompts (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			session_id TEXT,
			content TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE
		);`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS user_prompts_fts USING fts5(
			content,
			content=user_prompts,
			content_rowid=rowid
		);`,
		`CREATE TRIGGER IF NOT EXISTS user_prompts_ai AFTER INSERT ON user_prompts BEGIN
			INSERT INTO user_prompts_fts(rowid, content) VALUES (new.rowid, new.content);
		END;`,
		`CREATE TRIGGER IF NOT EXISTS user_prompts_ad AFTER DELETE ON user_prompts BEGIN
			INSERT INTO user_prompts_fts(user_prompts_fts, rowid, content) VALUES('delete', old.rowid, old.content);
		END;`,
		`CREATE TRIGGER IF NOT EXISTS user_prompts_au AFTER UPDATE ON user_prompts BEGIN
			INSERT INTO user_prompts_fts(user_prompts_fts, rowid, content) VALUES('delete', old.rowid, old.content);
			INSERT INTO user_prompts_fts(rowid, content) VALUES (new.rowid, new.content);
		END;`,
		"CREATE INDEX IF NOT EXISTS idx_user_prompts_session ON user_prompts(project_id, session_id, created_at DESC);",
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("failed to create user_prompts schema: %w", err)
		}
	}
	return nil
}

// addChangeLifecycle adds the changes table (spec-driven proposal lifecycle)
// plus a nullable change_id column on memories so a committed decision can be
// traced back to the change that produced it. Both additions are additive and
// idempotent; the memories column uses ON DELETE SET NULL so deleting a change
// never orphans the memory, it just unlinks it.
func addChangeLifecycle(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS changes (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			slug TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'draft',
			title TEXT NOT NULL,
			what TEXT,
			goal TEXT,
			where_path TEXT,
			design TEXT,
			tasks TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME,
			archived_at DATETIME,
			FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE,
			UNIQUE(project_id, slug)
		);`,
		"CREATE INDEX IF NOT EXISTS idx_changes_project_status ON changes(project_id, status);",
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("failed to create changes schema: %w", err)
		}
	}

	if exists, err := columnExists(db, "memories", "change_id"); err != nil {
		return err
	} else if !exists {
		if _, err := db.Exec("ALTER TABLE memories ADD COLUMN change_id TEXT REFERENCES changes(id) ON DELETE SET NULL;"); err != nil {
			return fmt.Errorf("failed to add column change_id to memories: %w", err)
		}
	}
	if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_memories_change ON memories(project_id, change_id);"); err != nil {
		return fmt.Errorf("failed to create idx_memories_change: %w", err)
	}

	return nil
}

// addSpecRequirements adds the delta-spec tables for the OpenSpec-style
// requirements layer: spec_requirements holds the per-change delta rows
// (ADDED/MODIFIED/REMOVED/RENAMED requirements with scenarios), and
// spec_capabilities holds the materialized current state of each capability
// (the merge target applied on commit). A FTS5 index over requirement/body
// makes the deltas searchable without a second migration. All additions are
// additive and idempotent.
func addSpecRequirements(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS spec_requirements (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			change_id TEXT NOT NULL,
			capability_path TEXT NOT NULL,
			delta_op TEXT NOT NULL,
			requirement TEXT NOT NULL,
			rename_to TEXT,
			body TEXT,
			rfc2119 TEXT,
			scenarios TEXT,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE,
			FOREIGN KEY(change_id) REFERENCES changes(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_spec_requirements_change ON spec_requirements(project_id, change_id);`,
		`CREATE INDEX IF NOT EXISTS idx_spec_requirements_capability ON spec_requirements(project_id, capability_path);`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS spec_requirements_fts USING fts5(
			requirement,
			body,
			content=spec_requirements,
			content_rowid=rowid
		);`,
		`CREATE TRIGGER IF NOT EXISTS spec_requirements_ai AFTER INSERT ON spec_requirements BEGIN
			INSERT INTO spec_requirements_fts(rowid, requirement, body) VALUES (new.rowid, new.requirement, COALESCE(new.body, ''));
		END;`,
		`CREATE TRIGGER IF NOT EXISTS spec_requirements_ad AFTER DELETE ON spec_requirements BEGIN
			INSERT INTO spec_requirements_fts(spec_requirements_fts, rowid, requirement, body) VALUES('delete', old.rowid, old.requirement, COALESCE(old.body, ''));
		END;`,
		`CREATE TRIGGER IF NOT EXISTS spec_requirements_au AFTER UPDATE ON spec_requirements BEGIN
			INSERT INTO spec_requirements_fts(spec_requirements_fts, rowid, requirement, body) VALUES('delete', old.rowid, old.requirement, COALESCE(old.body, ''));
			INSERT INTO spec_requirements_fts(rowid, requirement, body) VALUES (new.rowid, new.requirement, COALESCE(new.body, ''));
		END;`,
		`CREATE TABLE IF NOT EXISTS spec_capabilities (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			capability_path TEXT NOT NULL,
			requirement TEXT NOT NULL,
			body TEXT,
			rfc2119 TEXT,
			scenarios TEXT,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE,
			UNIQUE(project_id, capability_path, requirement)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_spec_capabilities_path ON spec_capabilities(project_id, capability_path);`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("failed to create spec requirements schema: %w", err)
		}
	}
	return nil
}
