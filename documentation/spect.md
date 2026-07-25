# SPEC.md — SV-Memory Specification

## 1. Vision & Core Goal

`sv-memory` is a high-performance, single-binary CLI tool and Model Context Protocol (MCP) server written in **Go**. Its purpose is to eliminate AI agent context amnesia by combining:

1. **Persistent Decision Memory:** Capturing non-obvious fixes, architectural decisions, and coding standards using SQLite + FTS5.
2. **Structural Knowledge Graph:** Mapping code entities (files, components, imports, dependencies) to provide structural context to LLM agents.
3. **Autonomous Agent Orchestration:** Injecting protocol rules into `AGENTS.md` so agents automatically query, record, and maintain context during coding sessions.

Developed under the **SVTech** ecosystem as a free, open-source tool for the developer community.

---

## 2. Architecture & System Flow

```text
       ┌────────────────────────────────────────────────────────┐
       │             AI Agent (Antigravity CLI / OpenCode)      │
       └───────────────────────────┬────────────────────────────┘
                                   │  MCP Protocol via Stdio
       ┌───────────────────────────▼────────────────────────────┐
       │                   sv-memory Binary                     │
       │                                                        │
       │  ┌──────────────────┐ ┌──────────────┐ ┌────────────┐  │
       │  │ Memory Engine    │ │ Graph Engine │ │ Config/Env │  │
       │  └────────┬─────────┘ └──────┬───────┘ └─────┬──────┘  │
       └───────────┼──────────────────┼───────────────┼─────────┘
                   │                  │               │
       ┌───────────▼──────────────────▼───────────────▼─────────┐
       │             Global SQLite Storage (+FTS5)              │
       │           (~/.config/sv-memory/storage.db)             │
       └────────────────────────────────────────────────────────┘
```

---

## 3. Technology Stack & Key Libraries

- **Language:** Go 1.22+
- **Storage Engine:** SQLite3 (via `github.com/mattn/go-sqlite3` or modern CGO-free `modernc.org/sqlite`) with **FTS5** enabled.
- **Protocol Server:** MCP Go SDK (`github.com/mark3labs/mcp-go`).
- **CLI Framework:** `github.com/spf13/cobra` for command handling.
- **AST / Parsing:** Lightweight AST parsing using native Go parser & fast regex/tree scanners for JS/TS/Python.

---

## 4. CLI Commands & Workflow

### `sv-memory init`

- Calculates a unique hash for the current Git root path (`project_id`).
- Registers the project entry into the central SQLite database (`~/.config/sv-memory/storage.db`).
- Checks if `AGENTS.md` exists in the repository root:
  - If missing: Creates `AGENTS.md` with standard autonomous memory rules.
  - If existing: Appends the `SV-Memory` instruction block if not already present.
- Scans the project directory and performs an initial build of the knowledge graph.

### `sv-memory mcp`

- Starts the JSON-RPC MCP server over `stdio` for agent consumption.

### `sv-memory graph rebuild`

- Manually forces a full scan of the directory tree and updates code graph nodes/edges.

---

## 5. Database Schema

The database resides in `~/.config/sv-memory/storage.db`.

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
    category TEXT NOT NULL, -- 'bugfix' | 'architecture' | 'standard' | 'decision'
    what TEXT NOT NULL,
    why TEXT NOT NULL,
    where_path TEXT,
    learned TEXT NOT NULL,
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

-- Graph Nodes (Files, Packages, Components, Functions)
CREATE TABLE IF NOT EXISTS graph_nodes (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    node_type TEXT NOT NULL, -- 'file' | 'module' | 'component' | 'service'
    label TEXT NOT NULL,
    path TEXT NOT NULL,
    metadata JSON,
    FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE
);

-- Graph Edges (Relationships)
CREATE TABLE IF NOT EXISTS graph_edges (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    target_id TEXT NOT NULL,
    relation_type TEXT NOT NULL, -- 'imports' | 'calls' | 'depends_on'
    FOREIGN KEY(source_id) REFERENCES graph_nodes(id) ON DELETE CASCADE,
    FOREIGN KEY(target_id) REFERENCES graph_nodes(id) ON DELETE CASCADE
);
```

---

## 6. MCP Tools Definition

`sv-memory` registers 4 core MCP tools for agents:

### 1. `sv_mem_save`

- **Description:** Persist a key architectural decision, bug fix, or standard.
- **Parameters:**
  - `category` (string, required): `bugfix` | `architecture` | `standard` | `decision`
  - `what` (string, required): Concise summary of what was done.
  - `why` (string, required): Reasoning behind the action.
  - `where_path` (string, optional): Affected file or module.
  - `learned` (string, required): Rule or guideline for future agents.

### 2. `sv_mem_search`

- **Description:** Query historical project decisions and standards using keyword/FTS search.
- **Parameters:**
  - `query` (string, required): Search term.
  - `category` (string, optional): Filter by category.

### 3. `sv_graph_query`

- **Description:** Retrieve code structure, node connections, and dependencies for a given module/file.
- **Parameters:**
  - `path_or_node` (string, required): Target file path or component name.
  - `depth` (integer, optional): Hop distance in graph (default: `1`).

### 4. `sv_graph_sync`

- **Description:** Trigger an incremental re-scan of the project structure.
- **Parameters:** None.

---

## 7. Standard `AGENTS.md` Protocol Template

When initialized, `sv-memory` ensures the following protocol is present in `AGENTS.md`:

```markdown
<!-- SV-MEMORY:START -->

# SV-Memory Protocol Rules

This project uses `sv-memory` for persistent architectural memory and structural context graph.

## Mandatory Agent Workflow:

1. **Context Initialization:** Before proposing or executing architectural changes, call `sv_mem_search` to check past project decisions, standards, and solved bugs.
2. **Context Save:** You MUST invoke `sv_mem_save` whenever you:
   - Fix a complex or non-obvious bug.
   - Introduce or refactor a design pattern / rule.
   - Make an explicit choice to avoid a library/framework feature.
3. **Graph Inspection:** Use `sv_graph_query` to inspect module dependencies before deleting or restructuring code.
4. **Graph Refresh:** Execute `sv_graph_sync` after adding major new files or modifying package structures.
<!-- SV-MEMORY:END -->
```

---

## 8. Directory Project Layout (Go)

```text
sv-memory/
├── cmd/
│   └── sv-memory/
│       └── main.go          # Cobra Root Command & MCP Server Launcher
├── internal/
│   ├── config/              # App config & paths (~/.config/sv-memory)
│   ├── db/                  # SQLite connection, migrations & FTS5 setup
│   ├── graph/               # Project file scanner & graph builder
│   ├── mcp/                 # MCP Server handlers & tool registrations
│   ├── memory/              # CRUD operations for memories
│   └── protocol/            # AGENTS.md injection & management
├── go.mod
├── go.sum
├── SPEC.md
└── README.md
```

---

## 9. Next Action Steps for Antigravity CLI (`agy`)

To start implementation, execute the following prompt or steps in `agy`:

1. **Initialize Go Module:** `go mod init github.com/svtech/sv-memory`
2. **Create Project Layout:** Create folders `cmd/sv-memory`, `internal/config`, `internal/db`, `internal/memory`, `internal/graph`, `internal/mcp`, `internal/protocol`.
3. **Implement Core DB (`internal/db`):** Initialize SQLite with FTS5 support and run schema migrations.
4. **Implement Protocol (`internal/protocol`):** Build file inspector/injector for `AGENTS.md`.
5. **Implement CLI Init (`cmd/sv-memory`):** Connect `sv-memory init` command.
