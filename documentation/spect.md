# SPEC.md — SV-Memory Specification v3

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

`sv-memory` provides **22 CLI commands** organized under Cobra's root and sub-commands:

### Core Commands

#### 1. `sv-memory init`
- Calculates a deterministic 16-char hex `project_id` (SHA256 of the Git root path).
- Registers the project entry into the central SQLite database.
- Checks for `AGENTS.md`, `.cursorrules`, or `.windsurfrules`:
  - If any exist: Injects or updates the `<!-- SV-MEMORY:START -->...<!-- SV-MEMORY:END -->` block.
  - If none exist: Creates `AGENTS.md` with the full protocol template.
- Scans the project directory and performs an initial build of the knowledge graph.
- Syncs memories from `.sv-memory/memories.json` (team-shared context).

#### 2. `sv-memory mcp`
- Starts the JSON-RPC MCP server over `stdio` for agent consumption.
- Registers all 25 MCP tools.
- Maintains an in-memory graph cache for zero-SQL BFS traversals.
- Debounces Git sync writes (500ms coalescing).

#### 3. `sv-memory diagnose`
- Runs health checks verifying database connections, schemas, folders, write permissions, and active settings.

#### 4. `sv-memory stats`
- Displays project statistics: total memories, deleted memories, recent 24-hour saves, sessions count, active sessions, and relation counts.

#### 5. `sv-memory sync`
- Pulls from `.sv-memory/memories.json` and pushes local SQLite changes back to it.

#### 6. `sv-memory configure`
- Interactive wizard for automatic/manual configurations of editors (Cursor, VS Code, Zed, Windsurf, OpenCode) and CLIs (Claude Code, Codex, Antigravity).

#### 7. `sv-memory obsidian-export [-o output-dir]`
- Exports all project memories to Markdown files inside the target folder (default `.obsidian-sv-memory`) structured as an Obsidian vault.

#### 8. `sv-memory export [output-file]`
- Exports all non-deleted memories for this project to a portable JSON file.

#### 9. `sv-memory import <input-file>`
- Imports memories from a JSON file using upsert by ID.

### Memory & Session Deletion Commands

#### 10. `sv-memory delete session <session-id>`
- Deletes an empty session (fails if the session contains associated memories).

#### 11. `sv-memory delete project <project-id> [--hard]`
- Cascade-deletes all project data. Soft-deletes memories by default; `--hard` removes them permanently from SQLite.

### Project Registry Management

#### 12. `sv-memory projects list`
- Lists all registered projects with their ID, name, path, memory counts, and session counts.

#### 13. `sv-memory projects prune`
- Prunes empty projects (those with 0 memories and 0 sessions) from the central SQLite registry.

#### 14. `sv-memory projects consolidate <source-project-id> <target-project-id>`
- Merges all memories and sessions from the source project into the target project, then prunes the source project.

### Code Graph Management

#### 15. `sv-memory graph rebuild`
- Forces a full code directory scan, rebuilding graph nodes and edges.

#### 16. `sv-memory graph path <source> <target>`
- Computes and prints the shortest dependency path between two code nodes in the graph (up to 10 hops).

#### 17. `sv-memory graph explain <node>`
- Outputs detailed information for a specific node: type, label, path, metadata JSON, and fan-in/fan-out metrics.

#### 18. `sv-memory graph communities`
- Runs Leiden community detection on the graph. Lists community clusters, their member nodes, centrality scores, and god nodes.

#### 19. `sv-memory graph wiki [--output dir]`
- Exports Markdown wiki pages for each detected community, listing member files, centrality scores, and inter-community dependencies. Default output directory: `graph-wiki`.

#### 20. `sv-memory graph viz [--output file]`
- Generates an interactive HTML visualization using vis.js with community-colored physics simulation, node filtering, and tooltips. Default output: `graph.html`.

#### 21. `sv-memory graph merge <json-file>`
- Merges a JSON graph snapshot into the current project graph, upserting nodes and edges by ID.

### 7. Conflict Management

#### 22. `sv-memory conflicts`
- Displays conflicting memories and detected semantic overlaps across the project.

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

-- Persistent Decision Memories
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
    deleted_at DATETIME,
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

-- Graph Nodes (Files, Symbols, Packages)
CREATE TABLE IF NOT EXISTS graph_nodes (
    project_id TEXT NOT NULL,
    id TEXT NOT NULL,
    node_type TEXT NOT NULL,  -- 'file' | 'module' | 'component' |
                             -- 'service' | 'function' | 'class'
    label TEXT NOT NULL,
    path TEXT NOT NULL,
    metadata TEXT,            -- JSON payload
    PRIMARY KEY(project_id, id),
    FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE
);

-- Graph Edges (Directed Relationships)
CREATE TABLE IF NOT EXISTS graph_edges (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    target_id TEXT NOT NULL,
    relation_type TEXT NOT NULL,              -- 'imports' | 'calls' | 'depends_on' | 'rationale_for'
    confidence TEXT NOT NULL DEFAULT 'EXTRACTED', -- 'EXTRACTED' | 'INFERRED' | 'AMBIGUOUS'
    source_location TEXT,                     -- Line numbers/ranges
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

-- Memory relations (conflict surfacing & supersedes timeline)
CREATE TABLE IF NOT EXISTS memory_relations (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    target_id TEXT NOT NULL,
    relation_type TEXT NOT NULL, -- 'supersedes' | 'conflicts_with' | 'relates_to'
    reason TEXT,
    judged_by TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE,
    FOREIGN KEY(project_id, source_id) REFERENCES memories(project_id, id) ON DELETE CASCADE,
    FOREIGN KEY(project_id, target_id) REFERENCES memories(project_id, id) ON DELETE CASCADE
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
CREATE INDEX IF NOT EXISTS idx_memory_relations_source ON memory_relations(project_id, source_id);
CREATE INDEX IF NOT EXISTS idx_memory_relations_target ON memory_relations(project_id, target_id);
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

`sv-memory` registers **25 MCP tools** for AI agents:

### 1. `sv_mem_save`
Persist a key architectural decision, bug fix, progress journal, or standard guideline.
- **Parameters:**
  - `category` (string, required): `bugfix` | `architecture` | `standard` | `decision` | `journal` | `postmortem` | `discussion` | `idea` | `qa`
  - `what` (string, required): Concise description.
  - `why` (string, required): Detailed reasoning.
  - `learned` (string, required): Rule or key lesson.
  - `where_path` (string, optional): Affected file or module.
  - `impact` (string, optional): What went well.
  - `errors_faced` (string, optional): Errors or roadblocks.
  - `next_steps` (string, optional): Pending tasks.
  - `topic_key` (string, optional): Stable key for upsert semantics (updates in-place).
  - `session_id` (string, optional): Associated session ID (auto-detected if omitted).

### 2. `sv_mem_suggest_topic_key`
Generate a stable topic key in kebab-case format before saving.
- **Parameters:**
  - `category` (string, required): Memory category.
  - `what` (string, required): Title or description.
- **Returns:** Suggested key like `category/kebab-case-description`.

### 3. `sv_mem_session_start`
Register a new coding session.
- **Parameters:**
  - `goal` (string, optional): Session objective.
  - `directory` (string, optional): Working directory.

### 4. `sv_mem_session_end`
Close an active session.
- **Parameters:**
  - `session_id` (string, required): Session ID.
  - `summary` (string, optional): Accomplishments.

### 5. `sv_mem_session_summary`
Update goals, discoveries, and next steps for the session.
- **Parameters:**
  - `session_id` (string, required): Session ID.
  - `goal` (string, optional): Updated goal.
  - `discoveries` (string, optional): Findings.
  - `accomplished` (string, optional): Completed tasks.
  - `next_steps` (string, optional): Upcoming goals.
  - `files` (string, optional): Edited files list.

### 6. `sv_mem_context`
Recover context from the last completed session.
- **Parameters:**
  - `limit` (string, optional): Max memories to retrieve (default `10`).

### 7. `sv_mem_search` (Layer 1 — Progressive Disclosure)
FTS5-powered memory search. Returns only IDs, categories, dates, titles, and topic keys.
- **Parameters:**
  - `query` (string, required): Keyword search terms.
  - `category` (string, optional): Category filter.
  - `limit` (string, optional): Max results (default `10`).
  - `offset` (string, optional): Pagination offset.

### 8. `sv_mem_timeline` (Layer 2 — Progressive Disclosure)
Retrieve a chronological list of observations centered around a specific memory.
- **Parameters:**
  - `observation_id` (string, required): Memory ID.
  - `before` (string, optional): Count of memories preceding (default `5`).
  - `after` (string, optional): Count of memories succeeding (default `5`).

### 9. `sv_mem_get` (Layer 3 — Progressive Disclosure)
Retrieve all fields of a specific memory. Text fields are truncated beyond `max_chars`.
- **Parameters:**
  - `id` (string, required): Memory ID.
  - `max_chars` (string, optional): Max characters per field (default `2000`).

### 10. `sv_mem_judge`
Create a relation (judgment) between two memories to maintain continuity or record conflicts.
- **Parameters:**
  - `source_id` (string, required): Newer memory ID.
  - `target_id` (string, required): Older memory ID.
  - `relation_type` (string, required): `supersedes` | `conflicts_with` | `relates_to`.
  - `reason` (string, optional): Reasoning.
  - `judged_by` (string, optional): Judge identity (default `'agent'`).

### 11. `sv_mem_compare`
Compare two memories side-by-side in Markdown format.
- **Parameters:**
  - `id1` (string, required): First memory ID.
  - `id2` (string, required): Second memory ID.

### 12. `sv_mem_review`
Find memories needing maintenance (e.g. stale, excessive duplicate counts, consolidation candidates).
- **Parameters:** None.

### 13. `sv_mem_stats`
Provides aggregate metrics (counts, breakdown by category).
- **Parameters:** None.

### 14. `sv_mem_current_project`
Retrieves the active project name, path, and ID.
- **Parameters:** None.

### 15. `sv_mem_delete`
Deletes a memory. Soft-deletes by default; set `hard` to `'true'` to erase permanently.
- **Parameters:**
  - `id` (string, required): Memory ID.
  - `hard` (string, optional): `'true'` for permanent delete.

### 16. `sv_mem_capture_passive`
Logs a lightweight journal entry automatically (e.g., test outcomes, file changes).
- **Parameters:**
  - `what` (string, required): Summary description.
  - `why` (string, required): Context or rationale.

### 17. `sv_graph_query`
Queries structural relations using a Breadth-First Search (BFS). Returns a Mermaid diagram.
- **Parameters:**
  - `path_or_node` (string, required): File path or module to center on.
  - `depth` (string, optional): Hop distance (default `1`).
  - `relation_type` (string, optional): Filter (e.g., `'imports'`, `'calls'`).
  - `direction` (string, optional): Traversal direction: `'in'` | `'out'` | `'all'` (default `'out'`).

### 18. `sv_graph_path`
Finds the shortest dependency route between two graph nodes.
- **Parameters:**
  - `source` (string, required): Source node ID.
  - `target` (string, required): Target node ID.
  - `max_hops` (string, optional): Hop limit (default `10`).

### 19. `sv_graph_sync`
Triggers an incremental scan of modified files to sync nodes/edges. Invalidates cache.
- **Parameters:** None.

### 20. `sv_mem_conflicts`
Detects and surfaces conflicting memories with semantic overlap analysis.
- **Parameters:** None.

### 21. `sv_graph_explain`
Outputs detailed information for a specific graph node: type, label, path, metadata, and fan-in/fan-out metrics.
- **Parameters:**
  - `path_or_node` (string, required): File path or node ID.

### 22. `sv_graph_god_nodes`
Identifies the most connected nodes in the graph based on betweenness centrality and degree. Returns a ranked list of god nodes with metrics.
- **Parameters:**
  - `limit` (string, optional): Max results to return (default `10`).

### 23. `sv_graph_surprising_connections`
Finds non-obvious or unexpected dependency paths in the graph. Highlights structural anomalies that may indicate architectural concerns.
- **Parameters:** None.

### 24. `sv_graph_viz`
Generates an interactive HTML visualization of the graph using vis.js with community coloring, physics simulation, node filtering, and tooltips.
- **Parameters:**
  - `output` (string, optional): Output file path (default `graph.html`).

### 25. `sv_graph_merge`
Merges a JSON graph snapshot into the current project graph, upserting nodes and edges by ID.
- **Parameters:**
  - `json` (string, required): JSON string containing nodes and edges arrays.

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
   - Call 'sv_mem_search' (e.g., filter category 'journal', 'postmortem', 'discussion', 'idea' or 'qa') to check the latest work logs, achievements, pending next steps, and past conversations, questions, or ideas.
   - Call 'sv_mem_search' (e.g., filter category 'architecture' or 'decision') to check past project decisions, standards, and solved bugs.
   - **Proactive search:** On first user message referencing a project, feature, or problem, call 'sv_mem_search' with their keywords before responding.

2. **Session Lifecycle (Token Economization):**
   - **Session start:** Call 'sv_mem_session_start' at the beginning of work to register a new session.
   - **Associate saves:** Pass 'session_id' to 'sv_mem_save' to group memories under the active session. If omitted, the active session is auto-detected.
   - **Session summary:** Call 'sv_mem_session_summary' with goal, discoveries, accomplished, next steps, and files before closing.
   - **Session end:** Call 'sv_mem_session_end' to mark the session as completed.
   - **After compaction or context reset:** Call 'sv_mem_context' immediately to recover the last session state — goal, summary, and associated memories.

3. **Progressive Disclosure (Token-Efficient Retrieval):**
   Use the 3-layer pattern to minimise tokens:
   - **Layer 1 — Search:** Call 'sv_mem_search' to get a compact list (IDs + titles) of relevant memories (~80 tokens/result).
   - **Layer 2 — Timeline:** Call 'sv_mem_timeline(observation_id=...)' to see chronological context around a specific memory.
   - **Layer 3 — Get full content:** Call 'sv_mem_get(id=...)' to retrieve the full content of a specific memory.
   Never dump all fields from search — drill down on demand.

4. **Topic Keys (Upsert Semantics):**
   - Use 'sv_mem_suggest_topic_key(category, what)' to generate a stable 'category/kebab-case' key.
   - Pass 'topic_key' to 'sv_mem_save' to enable upsert: saves to the same project+topic update in place (revision_count++) instead of creating a new record.
   - Use topic keys for evolving topics (architecture decisions, long-running features, recurring patterns). Skip for one-off bugs or single facts.

5. **Context Save (Session Compaction):** You MUST invoke 'sv_mem_save' to persist knowledge and compact the session context before finishing:
   - **Session Compaction:** At the end of every work session or major task, save a progress journal entry (category 'journal'): write a compacted summary of the conversation, key decisions made, code changes, and precise next steps. This serves as a lightweight checkpoint for future agents to resume context immediately.
   - When a significant Q&A, discussion, or improvement idea is discussed or agreed upon, save it: set 'category' to 'discussion', 'idea', or 'qa' to record the insights, questions, answers, and rationale.
   - When fixing a complex/non-obvious bug (category 'bugfix').
   - When introducing or refactoring a design pattern/rule (category 'architecture' or 'decision').
   - When choosing to avoid a library/framework feature.

6. **Graph Inspection:** Use 'sv_graph_query' to inspect module dependencies before deleting or restructuring code.

7. **Graph Refresh:** Execute 'sv_graph_sync' after adding major new files or modifying package structures.

## Repository Restrictions & Commit Standards:

- **Commit Format:** Always provide commit messages using the Conventional Commits format (e.g., 'feat(scope): descripcion'). Use Spanish by default, unless specified otherwise for the project.
- **Forbidden Actions:** You MUST NOT run 'git add', 'git commit', or 'git push' commands autonomously. The user must review changes and run these commands manually.
<!-- SV-MEMORY:END -->
```

---

## 9. Project Layout

```text
sv-memory/
├── cmd/
│   └── sv-memory/
│       └── main.go              # Cobra Command registration, CLI execution & MCP Server
├── internal/
│   ├── config/                  # App paths, settings parsing, viper config & configure cmd
│   ├── db/                      # DB initialization, composite migrations, WAL pools & PRAGMAs
│   ├── graph/                   # Code scanner, BFS query, community detection (Leiden),
│   │                            # betweenness centrality, god nodes, HTML viz, wiki export,
│   │                            # graph merge, surprising connections, incremental updates
│   ├── mcp/                     # Server start, 25 handlers registration, in-memory graph cache
│   ├── memory/                  # CRUD, sessions storage, dedup checks, Obsidian export
│   ├── protocol/                # AGENTS.md / editor rules injection
│   └── security/                # Regex secrets sanitizer
├── documentation/
│   ├── requirement.md           # Product constraints
│   └── spect.md                 # This specification
├── AGENTS.md                    # Injected protocols block (committed)
├── .sv-memory/
│   └── memories.json            # Portable team-shared memory export (committed)
├── go.mod
├── go.sum
├── install.sh                   # Unix setup script
└── README.md                    # Core project introduction
```

---

## 10. Language Support for Dependency Graph

| Language | Extensions | Import Detection Mechanism |
|---|---|---|
| Go | `.go` | `import "path"` |
| Python | `.py` | `import x`, `from x import y` |
| JavaScript | `.js`, `.jsx` | `import ... from`, `require()`, `import()` |
| TypeScript | `.ts`, `.tsx` | `import ... from`, `require()`, `import()` |
| Astro | `.astro` | Frontmatter imports block (`import ...`) |
| HTML | `.html` | Script src tags, link stylesheet tags |
| CSS | `.css` | `@import 'path'`, `@import url(...)` |
| PHP | `.php` | `include`, `require`, `use Namespace` |
| Bash | `.sh` | `source`, `. script.sh` |
| Lua | `.lua` | `require()`, `dofile()`, `loadfile()` |
| Ruby | `.rb` | `require`, `load`, `require_relative` |
| Rust | `.rs` | `use path`, `mod path`, `extern crate` |
| Java | `.java` | `import package` |
| Vue | `.vue` | `<script>` block imports |
| Svelte | `.svelte` | `<script>` block imports |

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
| Token budget benchmark (`BenchmarkToolResult`) | Measures and reports `tokens_used` / `total_budget` ratio in MCP response metadata for `sv_graph_query`, `sv_graph_god_nodes`, and `sv_graph_surprising_connections` | Agent-aware token consumption (visible in MCP response metadata) |

---

## 12. Memory Relations & Conflict Surfacing

To maintain coherence across long-term agent interactions, `sv-memory` includes a conflict detection and resolution system.

- **The `memory_relations` Table:** Tracks how memory nodes relate dynamically.
- **Relation Types:**
  - `supersedes`: A newer decision overrides an old guideline (deactivates or flags the target memory).
  - `conflicts_with`: Two decisions explicitly contradict each other. Flagged for manual review.
  - `relates_to`: Loose association between memories (e.g. standard relates to bugfix).
- **Conflict Lifecycle:**
  1. **Detection:** When a save is triggered without a topic key, a background heuristic scanner runs `sv_mem_compare` and computes a Jaccard/FTS overlap. If high semantic overlap exists with different rules, a `conflicts_with` relation is registered.
  2. **Surfacing:** Use `sv_mem_conflicts` (MCP tool) or `sv-memory conflicts` (CLI) to display detected semantic overlaps and conflicting memories across the project. Calling `sv_mem_review` also highlights pending conflicts.

---

## 13. Sessions Lifecycle

Sessions isolate memory creation per tasks and provide a rolling buffer of accomplishments.

```text
  Session Start              Observation Saves             Session Summary            Session End
┌───────────────┐          ┌───────────────────┐          ┌───────────────┐         ┌─────────────┐
│  Start timer  │ ───────> │  sv_mem_save /    │ ───────> │ Compaction &  │ ──────> │  End timer  │
│  Set goal     │          │  capture_passive  │          │ next steps    │         │  Set status │
└───────────────┘          └───────────────────┘          └───────────────┘         └─────────────┘
```

1. **Start:** `sv_mem_session_start` initiates a record in `sessions` status `'active'`.
2. **Execution:** All memories saved during this time are automatically linked to `session_id` to aggregate a timeline.
3. **Summary:** The agent summarizes accomplishments, discoveries, files modified, and next steps via `sv_mem_session_summary`.
4. **End:** `sv_mem_session_end` updates the status to `'completed'` and locks the session.

---

## 14. Code-Memory Graph Unification

Code entities and memory observations are mapped onto a unified directed graph stored in SQLite:

- **Entity Nodes:** Code symbols (functions, classes, files) represent structural dependencies.
- **Memory Nodes:** Decisions and standards are mapped into the same topological space.
- **Unification Edges (`rationale_for`):** Connecting a memory to a code entity links the *Why* directly to the *What*. Traversing the code graph via `sv_graph_query` retrieves both related imports/calls and associated decisions, giving developers and agents full context at the point of interest.

---

*Specification v3 — reflecting the full implementation as of July 2026.*

