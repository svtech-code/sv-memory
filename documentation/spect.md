# SPEC.md — SV-Memory Specification

## 1. Vision & Core Goal

`sv-memory` is a high-performance, single-binary CLI tool and Model Context Protocol (MCP) server written in **Go**. Its purpose is to eliminate AI agent context amnesia by combining:

1. **Persistent Decision Memory:** Capturing non-obvious fixes, architectural decisions, coding standards, progress journals, discussions, Q&As, and ideas using SQLite + FTS5 full-text search.
2. **Structural Knowledge Graph:** Mapping code entities (files, components, imports, dependencies) to provide structural context to LLM agents via directed dependency graphs.
3. **Autonomous Agent Orchestration:** Injecting protocol rules into `AGENTS.md`, `.cursorrules`, or `.windsurfrules` so agents automatically query, record, and maintain context during coding sessions.
4. **Team Collaboration:** Bidirectional Git-synced JSON (`.sv-memory/memories.json`) so the entire team shares context across clones.

Developed under the **SVTech** ecosystem as a free, open-source tool for the developer community.

---

## 2. Architecture & System Flow

```text
       ┌────────────────────────────────────────────────────────┐
       │       AI Agent (Cursor / Windsurf / Claude Code /       │
       │        OpenCode / Codex / Antigravity / Zed / VS Code)  │
       └───────────────────────────┬────────────────────────────┘
                                   │  MCP Protocol via Stdio
       ┌───────────────────────────▼────────────────────────────┐
       │                    sv-memory Binary                      │
       │                                                         │
       │  ┌──────────────────┐ ┌──────────────┐ ┌────────────┐   │
       │  │ Memory Engine    │ │ Graph Engine │ │ Config/Env │   │
       │  │ + Sessions       │ │ + Cache      │ │ + Security │   │
       │  └────────┬─────────┘ └──────┬───────┘ └─────┬──────┘   │
       └───────────┼──────────────────┼───────────────┼──────────┘
                   │                  │               │
       ┌───────────▼──────────────────▼───────────────▼─────────┐
       │           Global SQLite Storage (+FTS5)                 │
       │         (~/.config/sv-memory/storage.db)                │
       └────────────────────────────────────────────────────────┘
                             │
                   ┌─────────▼─────────┐
                   │ .sv-memory/       │
                   │  memories.json    │  ← Git-committed team sync
                   └───────────────────┘
```

### Key Design Principles:

- **Progressive Disclosure (Token-Efficient):** 3-layer retrieval pattern (compact search → timeline → full content) minimises LLM token consumption.
- **Session Lifecycle:** Sessions group related memories, enable post-compaction context recovery, and track goal/discoveries/next-steps.
- **Performance:** In-memory graph cache eliminates N+1 SQL round-trips; connection-pool split (writer + reader) under WAL mode; incremental mtime-based graph updates; debounced Git sync coalescing writes.
- **Safety:** Secret sanitization redacts API keys, tokens, and passwords before storage; no autonomous git operations.

---

## 3. Technology Stack & Key Libraries

- **Language:** Go 1.22+ (1.26.3 recommended)
- **Storage Engine:** SQLite via `modernc.org/sqlite` (pure Go, no CGO, fully portable) with **FTS5** full-text search.
- **Protocol Server:** MCP Go SDK (`github.com/mark3labs/mcp-go`, v0.57.0).
- **CLI Framework:** `github.com/spf13/cobra` for command handling + `github.com/AlecAivazis/survey/v2` for interactive prompts.
- **UUID Generation:** `github.com/google/uuid` (8-char short IDs).
- **Security:** Regex-based redaction for OpenAI keys, Anthropic keys, Gemini keys, JWT tokens, RSA/EC private keys, DB connection strings, and generic secret patterns.

---

## 4. CLI Commands & Workflow

### `sv-memory init`

- Calculates a deterministic 16-char hex `project_id` (SHA256 of the Git root path).
- Registers the project entry into the central SQLite database (`~/.config/sv-memory/storage.db`).
- Checks for `AGENTS.md`, `.cursorrules`, or `.windsurfrules`:
  - If any exist: Injects or updates the `<!-- SV-MEMORY:START -->...<!-- SV-MEMORY:END -->` block.
  - If none exist: Creates `AGENTS.md` with the full protocol template.
- Scans the project directory and performs an initial build of the knowledge graph.
- Syncs memories from `.sv-memory/memories.json` (team-shared context).

### `sv-memory mcp`

- Starts the JSON-RPC MCP server over `stdio` for agent consumption.
- Registers all MCP tools (11 total).
- Maintains an in-memory graph cache for zero-SQL BFS traversals.
- Debounces Git sync writes (500ms coalescing).

### `sv-memory graph rebuild`

- Manually forces a full re-scan of the directory tree and rebuilds code graph nodes/edges.

### `sv-memory sync`

- Bidirectional sync between SQLite and `.sv-memory/memories.json`.

### `sv-memory configure`

- Interactive 3-phase wizard for configuring MCP settings in supported tools.
- **Auto-configures:** Zed (JSON), OpenCode (JSON), Codex (TOML), Antigravity CLI (JSON).
- **Manual instructions for:** Cursor, VS Code, Windsurf, Claude Code.

---

## 5. Database Schema

The database resides in `~/.config/sv-memory/storage.db`. All schemas use `IF NOT EXISTS` for idempotency.

```sql
-- Projects Registry
CREATE TABLE IF NOT EXISTS projects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    path TEXT UNIQUE NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Persistent Decision Memories (20 columns)
CREATE TABLE IF NOT EXISTS memories (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    category TEXT NOT NULL,  -- 'bugfix' | 'architecture' | 'standard' |
                            -- 'decision' | 'journal' | 'postmortem' |
                            -- 'discussion' | 'idea' | 'qa'
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
    session_id TEXT,
    topic_key TEXT,
    revision_count INTEGER DEFAULT 1,
    duplicate_count INTEGER DEFAULT 0,
    last_seen_at DATETIME,
    normalized_hash TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE
);

-- Full-Text Search (FTS5) Virtual Table
CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
    what,
    why,
    learned,
    content=memories,
    content_rowid=rowid
);

-- FTS5 sync triggers (auto-sync on INSERT/UPDATE/DELETE)
CREATE TRIGGER IF NOT EXISTS memories_ai AFTER INSERT ON memories BEGIN
    INSERT INTO memories_fts(rowid, what, why, learned)
    VALUES (new.rowid, new.what, new.why, new.learned);
END;

CREATE TRIGGER IF NOT EXISTS memories_ad AFTER DELETE ON memories BEGIN
    INSERT INTO memories_fts(memories_fts, rowid, what, why, learned)
    VALUES('delete', old.rowid, old.what, old.why, old.learned);
END;

CREATE TRIGGER IF NOT EXISTS memories_au AFTER UPDATE ON memories BEGIN
    INSERT INTO memories_fts(memories_fts, rowid, what, why, learned)
    VALUES('delete', old.rowid, old.what, old.why, old.learned);
    INSERT INTO memories_fts(rowid, what, why, learned)
    VALUES (new.rowid, new.what, new.why, new.learned);
END;

-- Graph Nodes (Files, Packages, etc.)
CREATE TABLE IF NOT EXISTS graph_nodes (
    project_id TEXT NOT NULL,
    id TEXT NOT NULL,
    node_type TEXT NOT NULL,  -- 'file' | 'module' | 'component' |
                             -- 'service' | 'function' | 'class'
    label TEXT NOT NULL,
    path TEXT NOT NULL,
    metadata TEXT,  -- JSON payload
    PRIMARY KEY(project_id, id),
    FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE
);

-- Graph Edges (Directed Relationships)
CREATE TABLE IF NOT EXISTS graph_edges (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    target_id TEXT NOT NULL,
    relation_type TEXT NOT NULL,  -- 'imports' | 'calls' | 'depends_on'
    FOREIGN KEY(project_id, source_id) REFERENCES graph_nodes(project_id, id) ON DELETE CASCADE,
    FOREIGN KEY(project_id, target_id) REFERENCES graph_nodes(project_id, id) ON DELETE CASCADE,
    FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE,
    UNIQUE(project_id, source_id, target_id, relation_type)
);

-- File metadata cache (for incremental graph updates)
CREATE TABLE IF NOT EXISTS graph_files_meta (
    project_id TEXT NOT NULL,
    path TEXT NOT NULL,
    mtime_ms INTEGER NOT NULL,
    size INTEGER NOT NULL,
    PRIMARY KEY(project_id, path),
    FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE
);

-- Session tracking (coding session lifecycle)
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
```

### Performance Indexes

```sql
CREATE INDEX IF NOT EXISTS idx_graph_edges_source ON graph_edges(project_id, source_id);
CREATE INDEX IF NOT EXISTS idx_graph_edges_target ON graph_edges(project_id, target_id);
CREATE INDEX IF NOT EXISTS idx_memories_project_created ON memories(project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_memories_project_category ON memories(project_id, category);
CREATE INDEX IF NOT EXISTS idx_memories_topic ON memories(project_id, topic_key);
CREATE INDEX IF NOT EXISTS idx_memories_hash ON memories(project_id, normalized_hash);
CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project_id, started_at DESC);
```

### SQLite PRAGMA Configuration

| PRAGMA | Value | Purpose |
|---|---|---|
| `journal_mode` | WAL | Write-Ahead Logging: concurrent reads while writing |
| `synchronous` | NORMAL | Balanced durability/speed (crash-safe with WAL) |
| `temp_store` | MEMORY | Temp tables in RAM |
| `cache_size` | -20000 | ~20 MB page cache per connection |
| `mmap_size` | 268435456 | 256 MB memory-mapped I/O (avoids `read()` syscalls) |
| `busy_timeout` | 5000 | Wait 5s on lock instead of failure |
| `foreign_keys` | ON | Enforce referential integrity |

### Connection Pool

- **Writer:** `MaxOpenConns=1` (serialized writes under WAL)
- **Reader:** `MaxOpenConns=8` (parallel reads, `?mode=ro` for lock-free concurrency)
- **Degradation:** If reader fails to open, `Reader == Writer` (correct but slower)

---

## 6. MCP Tools Definition

`sv-memory` registers **11 MCP tools** for AI agents:

### 1. `sv_mem_save`

Persist a key architectural decision, bug fix, progress journal, or standard guideline.

**Parameters:**
- `category` (string, required): `bugfix` | `architecture` | `standard` | `decision` | `journal` | `postmortem` | `discussion` | `idea` | `qa`
- `what` (string, required): Concise description
- `why` (string, required): Detailed reasoning
- `learned` (string, required): Rule or key lesson
- `where_path` (string, optional): Affected file or module
- `impact` (string, optional): What went well
- `errors_faced` (string, optional): Errors or roadblocks
- `next_steps` (string, optional): Pending tasks
- `topic_key` (string, optional): Stable key for upsert semantics (update in place)
- `session_id` (string, optional): Session association (auto-detected from active session if omitted)

**Behavior:**
1. Sanitizes all text fields (redacts secrets).
2. **Strategy 1 — Topic Key Upsert:** If `topic_key` is set and a record with same `project_id + topic_key` exists, updates in place (`revision_count++`).
3. **Strategy 2 — Rolling Window Dedup:** If `topic_key` is not set and identical content hash exists within 24h for same project+category, increments `duplicate_count` (no new row).
4. **Fallback:** New row insert with `ON CONFLICT(id) DO UPDATE` for idempotency.
5. After SQLite write, schedules a debounced (500ms) Git sync to `.sv-memory/memories.json`.

### 2. `sv_mem_suggest_topic_key`

Generate a stable topic key before saving.

**Parameters:**
- `category` (string, required): Memory category
- `what` (string, required): Title or description

**Returns:** Suggested key in format `category/kebab-case-description` (max 80 chars).

### 3. `sv_mem_session_start`

Register a new coding session.

**Parameters:**
- `goal` (string, optional): Session objective
- `directory` (string, optional): Working directory (auto-detected)

### 4. `sv_mem_session_end`

Close an active session.

**Parameters:**
- `session_id` (string, required): Session ID to close
- `summary` (string, optional): What was accomplished

### 5. `sv_mem_session_summary`

Save structured session metadata.

**Parameters:**
- `session_id` (string, required): Session ID
- `goal` (string, optional): Original objective
- `discoveries` (string, optional): Key findings
- `accomplished` (string, optional): Completed work
- `next_steps` (string, optional): Pending items
- `files` (string, optional): Modified files

### 6. `sv_mem_context`

Recover context after compaction or context reset. Returns last completed session's goal, summary, and associated memories.

**Parameters:**
- `limit` (string, optional): Max memories to include (default `10`)

### 7. `sv_mem_search` (Layer 1 — Progressive Disclosure)

Search memories using FTS5 keyword search. Returns **compact output** (~80 tokens/result): only IDs, titles, categories, topic keys, and dates.

**Parameters:**
- `query` (string, required): Search term or keyword
- `category` (string, optional): Category filter
- `limit` (string, optional): Max results (default `10`)
- `offset` (string, optional): Pagination offset (default `0`)

### 8. `sv_mem_timeline` (Layer 2 — Progressive Disclosure)

Get chronological context around a specific memory.

**Parameters:**
- `observation_id` (string, required): Memory ID to center on
- `before` (string, optional): Memories before (default `5`)
- `after` (string, optional): Memories after (default `5`)

### 9. `sv_mem_get` (Layer 3 — Progressive Disclosure)

Retrieve full content of a specific memory. Long text fields (`why`, `learned`, `impact`, `errors_faced`, `next_steps`) may be truncated beyond `max_chars` to limit token consumption.

**Parameters:**
- `id` (string, required): Memory ID
- `max_chars` (string, optional): Max characters per text field (default `2000`, `0` = unlimited)

### 10. `sv_graph_query`

Query the project dependency graph using BFS traversal.

**Parameters:**
- `path_or_node` (string, required): File path, package name, or module
- `depth` (string, optional): Hop distance (default `1`)

**Returns:** Node list + Mermaid diagram of reachable sub-graph. Uses in-memory cache — zero SQL round-trips after initial load.

### 11. `sv_graph_sync`

Trigger a full re-scan of the project directory and refresh the graph in SQLite. Invalidates the in-memory cache.

---

## 7. Memory Save Strategies (Detail)

### Topic Key Upsert (Evolving Topics)

When `topic_key` is provided:
1. Query: `SELECT id, revision_count FROM memories WHERE project_id = ? AND topic_key = ?`
2. If found: `UPDATE` existing row, `revision_count++`, update all fields.
3. If not found: Fall through to insert with `revision_count = 1`.

Use case: Long-running features, recurring architectural patterns, evolving standards.

### Rolling-Window Dedup (Same Content, Short Window)

When `topic_key` is NOT provided:
1. Compute SHA256 hash of `what + "\x00" + why + "\x00" + learned`.
2. Query: `SELECT id, duplicate_count FROM memories WHERE project_id = ? AND normalized_hash = ? AND category = ? AND created_at > datetime('now', '-24 hours')`
3. If found: `UPDATE duplicate_count++`, bump `last_seen_at`. No new row.
4. If not found: Insert new row.

Use case: Multiple agents saving the same fact within a session.

### Security Sanitization

Before any save, 7 regex patterns redact:
- OpenAI API keys (`sk-...`)
- Anthropic keys (`sk-ant-sid...`)
- Gemini keys (`AIzaSy...`)
- JWT tokens
- RSA/EC private key blocks
- Database connection strings
- Generic `password`/`secret`/`token`/`api_key` assignments

All replaced with `[REDACTED_SECRET]`. Key names in assignments are preserved.

---

## 8. Agent Protocol Template

When initialized, `sv-memory` injects the following protocol block into `AGENTS.md`, `.cursorrules`, or `.windsurfrules`:

```markdown
<!-- SV-MEMORY:START -->
# SV-Memory Protocol Rules

This project uses 'sv-memory' for persistent architectural memory, progress journals, and structural context graph.

## Mandatory Agent Workflow:

1. **Context Initialization:** Before proposing or executing any changes:
   - Call 'sv_mem_search' to check work logs, decisions, and bugs.
   - Proactive search on first user message referencing a project.

2. **Session Lifecycle (Token Economization):**
   - Session start → associate saves → summary → end → context recovery.

3. **Progressive Disclosure (Token-Efficient Retrieval):**
   - Layer 1 — Search (~80 tokens/result)
   - Layer 2 — Timeline
   - Layer 3 — Get full content
   - Never dump all fields from search — drill down on demand.

4. **Topic Keys (Upsert Semantics):**
   - Use sv_mem_suggest_topic_key → pass topic_key to sv_mem_save for in-place updates.

5. **Context Save (Session Compaction):** Save journal entries at end of session.

6. **Graph Inspection:** Use sv_graph_query before deleting/restructuring code.

7. **Graph Refresh:** Execute sv_graph_sync after adding major files.

## Repository Restrictions:
- Conventional Commits format
- No autonomous git add/commit/push
<!-- SV-MEMORY:END -->
```

---

## 9. Project Layout

```text
sv-memory/
├── cmd/
│   └── sv-memory/
│       └── main.go              # Cobra Root Command & MCP Server Launcher
├── internal/
│   ├── config/                  # App config, paths, Git helpers, interactive configure
│   ├── db/                      # SQLite connection, migrations, pool, PRAGMAs
│   ├── graph/                   # Multi-language file scanner, import parser, graph builder
│   ├── mcp/                     # MCP Server (11 tools), in-memory graph cache, BFS
│   ├── memory/                  # Memory CRUD, sessions, dedup, topic keys, Git sync
│   ├── protocol/                # AGENTS.md/.cursorrules/.windsurfrules injection
│   └── security/                # Secret redaction/sanitization
├── documentation/
│   ├── requirement.md           # Original requirements (Spanish)
│   └── spect.md                 # This specification
├── AGENTS.md                    # Protocol rules (injected, committed)
├── .sv-memory/
│   └── memories.json            # Team-shared memory export (committed)
├── go.mod
├── go.sum
├── install.sh                   # Installer (macOS/Linux)
└── README.md                    # Full documentation (Spanish + English)
```

---

## 10. Language Support for Dependency Graph

| Language | Extensions | Import Detection |
|---|---|---|
| Go | `.go` | `import "path"` |
| Python | `.py` | `import x`, `from x import y` |
| JavaScript/TypeScript | `.js`, `.jsx`, `.ts`, `.tsx`, `.astro` | `import ... from`, `require()`, `import()` |
| PHP | `.php` | `include`, `require`, `use Namespace` |
| CSS | `.css` | `@import 'path'`, `@import url(...)` |
| HTML | `.html` | `<script src>`, `<link href>` |
| Bash | `.sh` | `source`, `. script.sh` |
| Lua | `.lua` | `require()`, `dofile()`, `loadfile()` |

Parsing uses concurrent worker pool (8 goroutines) with regex-based extractors.

---

## 11. Token Optimization Features

| Feature | Mechanism | Estimated Savings |
|---|---|---|
| Progressive 3-layer disclosure | Search returns 7 fields (~80 tokens/result); full content on demand | 60-80% of response tokens |
| Session compaction | Full conversation → structured journal entry (200-500 tokens) | 80-90% vs raw history |
| Field truncation (`sv_mem_get`) | `max_chars` cap per text field (default 2000) | Prevents unbounded token consumption |
| Topic key upsert | Update in-place instead of accumulating revisions | 50% fewer redundant search results |
| Rolling-window dedup | Suppress identical saves within 24h | Prevents duplicate bloat |
| Compact search SQL | SELECT only 7 needed columns instead of all 20 | ~60% less I/O per search |

---

*Specification v2 — reflecting the full implementation as of July 2026.*
