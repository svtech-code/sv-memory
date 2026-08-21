# SPEC.md SV-Memory Specification v3

> **Language:** English | [Español](spect_ES.md)

## 1. Vision & Core Goal

`sv-memory` is a high-performance, single-binary CLI tool and Model Context Protocol (MCP) server written in **Go**. Its purpose is to eliminate AI agent context amnesia by combining:

1. **Persistent Decision Memory:** Capturing non-obvious fixes, architectural decisions, coding standards, progress journals, discussions, Q&As, and ideas using SQLite + FTS5 full-text search.
2. **Structural Knowledge Graph:** Mapping code entities (files, components, imports, dependencies) to provide structural context to LLM agents via directed dependency graphs.
3. **Autonomous Agent Orchestration:** Injecting protocol rules into `AGENTS.md`, `.cursorrules`, or `.windsurfrules` so agents automatically query, record, and maintain context during coding sessions.
4. **Team Collaboration:** Bidirectional Git-synced JSON (`.sv-memory/chunks/*.json`, one file per memory) so the entire team shares context across clones.

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
        ┌─────────────────────────▼─────────┐
        │ .sv-memory/chunks/                │
        │  <memory-id>.json                 │  ← Git-committed team sync
        └───────────────────────────────────┘
```

### Key Design Principles

- **Progressive Disclosure (Token-Efficient):** 3-layer retrieval pattern (compact search → timeline → full content) minimises LLM token consumption.
- **Session Lifecycle:** Sessions group related memories, enable post-compaction context recovery, and track goal/discoveries/next-steps.
- **Performance:** In-memory graph cache eliminates N+1 SQL round-trips; connection-pool split (writer + reader) under WAL mode; incremental mtime-based graph updates; debounced Git sync coalescing writes.
- **Safety:** Secret sanitization redacts API keys, tokens, and passwords before storage; no autonomous git operations.

---

## 3. Technology Stack & Key Libraries

- **Language:** Go 1.26+ (1.26.3 required by `go.mod`)
- **Storage Engine:** SQLite via `modernc.org/sqlite` (pure Go, no CGO, fully portable) with **FTS5** full-text search.
- **Protocol Server:** MCP Go SDK (`github.com/mark3labs/mcp-go`, v0.57.0).
- **CLI Framework:** `github.com/spf13/cobra` for command handling.
- **Interactive UIs:** `charmbracelet/huh` (configure wizard, TUI forms), `charmbracelet/bubbles` + `lipgloss` (TUI), `charmbracelet/bubbletea` (TUI runtime).
- **Config:** `github.com/spf13/viper` (YAML global/local config with flag/env precedence).
- **Graph Parsing:** `github.com/odvcencio/gotreesitter` (pure-Go tree-sitter bindings) with regex fallback for legacy/edge-case languages.
- **Graph Cache:** `github.com/hashicorp/golang-lru/v2` (in-memory LRU graph cache).
- **UUID Generation:** `github.com/google/uuid` (hex IDs with 64 bits of entropy via `newID()`).
- **Security:** Regex-based redaction for OpenAI keys, Anthropic keys, Gemini keys, JWT tokens, RSA/EC private keys, DB connection strings, and generic secret patterns.

---

## 4. CLI Commands & Workflow

`sv-memory` provides CLI commands organized under Cobra's root and sub-commands:

### Core Commands

#### 1. `sv-memory init`

- Calculates a deterministic 16-char hex `project_id` (SHA256 of the Git root path).
- Registers the project entry into the central SQLite database.
- Checks for `AGENTS.md`, `.cursorrules`, or `.windsurfrules`:
  - If any exist: Injects or updates the `<!-- SV-MEMORY:START -->
# SV-Memory Protocol Rules

This project uses 'sv-memory' for persistent architectural memory, progress journals, and structural context graph.

## Session Lifecycle (REQUIRED, in this order):

1. **Start:** Call 'sv_mem_session_start' at the beginning of work. It returns an **Auto-Boot Context Bundle** with the previous session summary, key architectural decisions, standards, recent bugfixes, last journals, and top graph hubs — read it and use it as your starting context.
2. **Associate saves:** Pass 'session_id' to 'sv_mem_save' to group memories under the active session. If omitted, the active session is auto-detected.
3. **Capture knowledge as you go:** Save journals, decisions, standards, and bugfixes with 'sv_mem_save' (see the Memory Capture Guidelines below). Use 'sv_mem_capture_passive' for lightweight observations that do not need an explicit save decision.
4. **Summary:** Call 'sv_mem_session_summary' with goal, discoveries, accomplished work, and next steps before closing.
5. **End:** Call 'sv_mem_session_end' to mark the session as completed and enable context recovery in the next session.

After a compaction or context reset, call 'sv_mem_context' to recover the last session state (goal, summary, associated memories).

## Tool Usage in Any Mode:

The sv-memory tools (session, memory, graph, diagnostics) may be called in ANY operational mode — plan, build, or review. They persist only to the project memory store ('.sv-memory/'), which is project data, not source code. Do not skip memory capture, context recovery, or the session lifecycle because of the current mode.

## Context Initialization (Search-Before-Work):

Memory must be consulted before proposing or executing changes:
- **Orientation:** On a new project, call 'sv_mem_stats' first — it is the cheapest overview of memory distribution (categories, counts, sessions).
- **Targeted search:** Call 'sv_mem_search' with the topic keywords of your task (feature, component, style, module). Filter by category when relevant ('journal', 'postmortem', 'discussion', 'idea', 'qa', 'architecture', 'decision'). Avoid repeating redundant searches — the Auto-Boot Bundle already carries the previous session context.
- **Proactive search:** On first user message referencing a project, feature, or problem, call 'sv_mem_search' with their keywords before responding. Never answer from assumptions alone — memory first, code second.

## Progressive Disclosure (Token-Efficient Retrieval):

Use the 3-layer pattern to minimise tokens:
- **Layer 1 — Search:** Call 'sv_mem_search' to get a compact list (IDs + titles + topic keys) of relevant memories (~30 tokens/result).
- **Layer 2 — Timeline:** Call 'sv_mem_timeline(observation_id=...)' to see chronological context around a specific memory (includes the central observation rationale).
- **Layer 3 — Get full content:** Call 'sv_mem_get(id=...)' to retrieve the full content of a specific memory.
Never dump all fields from search — drill down on demand. The top search result is already expanded inline, so only drill further when you need deeper detail.

## Topic Keys (Upsert Semantics):

- Use 'sv_mem_suggest_topic_key(category, what)' to generate a stable 'category/kebab-case' key.
- Pass 'topic_key' to 'sv_mem_save' to enable upsert: saves to the same project+topic update in place (revision_count++) instead of creating a new record.
- Use topic keys for evolving topics (architecture decisions, design systems, long-running features, recurring patterns). Skip for one-off bugs or single facts.
- **Convention:** Always kebab-case in English. Examples: 'standard/design-system', 'architecture/component-card', 'decision/use-bun-instead-of-npm', 'standard/workflow-git-commits', 'bugfix/tab-transition-absolute-position'.

## Memory Capture Guidelines (when to save what):

Always persist design knowledge as structured memories with a topic_key, not just session journals:

| Situation | Category | topic_key example |
| :--- | :--- | :--- |
| Visual style / design system / CSS / Tailwind tokens | 'standard' | standard/design-system |
| Reusable component or UI pattern | 'architecture' | architecture/component-card |
| Workflow / methodology / build & dev process | 'standard' | standard/workflow-dev-process |
| Architectural decision made (and its rationale) | 'decision' | decision/... |
| Code convention / naming / folder structure | 'standard' | standard/code-conventions |
| Complex or non-obvious bug fixed | 'bugfix' | bugfix/... |
| Relevant Q&A with lasting value | 'qa' | qa/... |
| Rejected library or framework feature | 'decision' | decision/avoid-... |
| Session progress checkpoint | 'journal' | journal/... |

**Golden rule:** when you define, change, or reuse a style, component, methodology, or convention, save it as 'standard' or 'architecture' with a topic_key. A journal is not a substitute — journals document progress, 'standard'/'architecture'/'decision' preserve the "how" and the "why" for future sessions.

## Graph Inspection (before modifying code):

- **Orient before touching code:** Call 'sv_graph_god_nodes' to see the most-connected hub nodes — these are the architectural hotspots any change may ripple through.
- **Understand a module:** Call 'sv_graph_explain(node=...)' before refactoring, deleting, or restructuring a file/module. It reports the node's role, community, centrality, fan-in/fan-out, neighbors, and suggested questions.
- **Inspect dependencies:** Call 'sv_graph_query(path_or_node=...)' to see a module's dependency sub-graph (imports/calls/depends_on) with depth, direction, and relation-type filters.
- **Trace a connection:** Call 'sv_graph_path(source=..., target=...)' to find the shortest dependency path between two nodes.

## Spec-Driven Decision Cycle (before proposing or changing behavior):

Proposals go through a lifecycle before code is written. Use it for any behavior/architecture change, not just large features:
- **Consult context:** 'sv_mem_context_pack(path="<file|pkg>", include_changes="true")' surfaces the node role, linked decisions/standards, and active changes affecting that path in one call.
- **Propose:** 'sv_propose_spec(slug="<kebab-case>", title=..., what=..., where_path=...)' registers the change and runs a pre-flight check against rules/invariants (standards, decisions, architecture memories). A pinned rule that overlaps the proposal returns a BLOCK verdict.
- **Validate:** 'sv_validate_decision(change_id=...)' re-checks a proposal after edits (PASS/WARN/BLOCK). Deterministic by default; pass semantic="true" to opt into agent re-ranking.
- **Commit:** 'sv_commit_spec(change_id=...)' promotes the change into a durable decision/standard memory, links it to the change_id, wires the rationale_for edge, and stamps it applied. A pre-flight BLOCK rejects the commit unless force="true" explicitly overrides the invariant. Call after implementation, before 'sv_mem_session_end'.
- Lifecycle states: 'draft' → 'proposed' → 'validated' → 'applied' (→ 'archived') | 'rejected'. Committed decisions get topic_key 'decision/<slug>'.
- **Human-visible mirror:** every change is auto-projected to '.sv-memory/specs/changes/<slug>.md' (git-synced). Humans can edit those files; 'sv-memory specs import <slug>' reconciles the edits back into the store (the SQLite DB stays authoritative). 'sv-memory specs export/list/archive' manage the mirror.

## Graph Refresh:

Execute 'sv_graph_sync' after adding major new files, creating new packages, or modifying package structures/imports. The graph is rebuilt incrementally and communities/centrality are computed lazily when queried.

## Memory Maintenance (periodic):

- **Review:** Call 'sv_mem_review' to list stale, duplicate, or consolidation-candidate memories.
- **Conflicts:** Call 'sv_mem_conflicts action=scan' to detect potential duplicate memories; judge them with 'sv_mem_judge' (supersedes / conflicts_with / relates_to) or with 'action=scan semantic=true' to LLM-judge candidates via the agent CLI. Keep relations coherent.
- **Compare:** Call 'sv_mem_compare(id1, id2)' before judging two similar memories.
- **Compact:** Call 'sv_mem_compact' periodically or after many topic-key upserts to consolidate revisions and keep search fast.

## Tool Quick Reference:

- **Session:** sv_mem_session_start, sv_mem_session_summary, sv_mem_session_end, sv_mem_context
- **Memory CRUD:** sv_mem_save, sv_mem_update, sv_mem_get, sv_mem_delete, sv_mem_search, sv_mem_timeline
- **Pin / Priority:** sv_mem_pin (action='unpin' to clear)
- **Knowledge quality:** sv_mem_suggest_topic_key, sv_mem_judge, sv_mem_compare, sv_mem_compact, sv_mem_review, sv_mem_capture_passive, sv_mem_conflicts, sv_mem_stats, sv_mem_diagnose
- **User intent:** sv_mem_capture_prompt (record what the user asked, recoverable via sv_mem_context)
- **Project admin:** sv_mem_merge_projects (merge project variants into a canonical project)
- **Context Pack:** sv_mem_context_pack (one bounded call: graph role + linked memories + active changes for a file/package/symbol)
- **Decision Engine:** sv_propose_spec, sv_validate_decision, sv_commit_spec (propose → validate → commit cycle with pre-flight checks)
- **Spec Mirror (CLI):** sv-memory specs export | import <slug> | list | archive (human-readable Markdown projection of changes under .sv-memory/specs/)
- **Graph:** sv_graph_query, sv_graph_explain, sv_graph_god_nodes, sv_graph_path, sv_graph_sync, sv_graph_surprising_connections, sv_graph_viz, sv_graph_merge

## Repository Restrictions & Commit Standards:

- **Commit Format:** Always provide commit messages using the Conventional Commits format (e.g., 'feat(scope): description'). Use the project's configured commit language (default: English), unless the project specifies otherwise.
- **Forbidden Actions:** You MUST NOT run 'git add', 'git commit', or 'git push' commands autonomously. The user must review changes and run these commands manually.
<!-- SV-MEMORY:END -->` block.
  - If none exist: Creates `AGENTS.md` with the full protocol template.
- Scans the project directory and performs an initial build of the knowledge graph.
- Syncs memories from `.sv-memory/chunks/` (team-shared context).

#### 2. `sv-memory mcp`

- Starts the JSON-RPC MCP server over `stdio` for agent consumption.
- Registers all 34 MCP tools.
- Maintains an in-memory graph cache for zero-SQL BFS traversals.
- Debounces Git sync writes (500ms coalescing).

#### 3. `sv-memory version`

- Prints the build version, commit, and Go runtime (`go`, `GOOS`/`GOARCH`).
- The version is injected at build time via `-ldflags`, so release binaries report the tag they were built from.

#### 4. `sv-memory update`

- Checks GitHub Releases for a newer version, asks for confirmation, downloads the platform binary, verifies its SHA-256 checksum against `checksums.txt`, and atomically replaces the running executable (on Windows it prints a manual `copy` command since the running `.exe` cannot be overwritten).

#### 5. `sv-memory diagnose`

- Runs health checks verifying database connections, schemas, folders, write permissions, and active settings.

#### 6. `sv-memory stats`

- Displays project statistics: total memories, deleted memories, recent 24-hour saves, sessions count, active sessions, and relation counts.

#### 7. `sv-memory sync`

- Pulls from `.sv-memory/chunks/` and pushes local SQLite changes back to it (chunked per-memory JSON). Distinct memory IDs never conflict; a same-ID concurrent edit leaves git conflict markers in `{id}.json`, which the import **skips with a warning** instead of aborting the whole sync. Importing a chunk that would overwrite a newer local edit (higher `revision_count`) or one diverged at the same revision logs a **last-writer-wins warning** the git chunk wins, but the lost local edit is surfaced. Resolve a conflicted chunk by removing the markers and re-running `sv-memory sync`.

#### 8. `sv-memory tui`

- Launches an interactive Terminal UI (`charmbracelet/huh`/bubbletea) for memory inspection, BM25 search, graph diagnostics, Obsidian vault export, and Neo4j/FalkorDB Cypher export.

#### 9. `sv-memory configure`

- Interactive wizard for automatic/manual configurations of editors (Cursor, VS Code, Zed, Windsurf, OpenCode) and CLIs (Claude Code, Codex, Antigravity).
- **Phase 4 (MCP Permissions):** Lists the 34 sv-memory MCP tools with descriptions and grants the selected allow-list entries to the allow-listed platforms chosen earlier (Antigravity CLI, Claude Code).
- **Sub-commands** for reading/writing configuration (YAML, global `~/.sv-memory/config.yaml` or local `.sv-memory/config.yaml`):
  - `sv-memory configure get <key>`: prints a single configuration value.
  - `sv-memory configure set <key> <value> [--local]`: writes a value globally (default) or project-locally.
  - `sv-memory configure list`: prints all active configuration values (`default_db_path`, `git_sync_enabled`, `conflict_threshold`, `default_review_limit`, `auto_compaction_enabled`, `compaction_interval_minutes`, `max_response_tokens`, `max_field_chars`, `search_expand_chars`, `timeline_why_chars`, `bundle_why_chars`, `context_pack_max_memories`, `graph_boost`, `prune_stale_days`).

#### 10. `sv-memory permissions`

- `list`: shows the 34 sv-memory MCP tools with human-readable descriptions.
- `grant --platform <p> [--all | --tool a,b] [--dry-run]`: writes allow-list entries (`mcp(sv-memory/<tool>)` for Antigravity, `mcp__sv-memory__<tool>` for Claude Code), preserving unrelated entries.
- `revoke --platform <p> [--dry-run]`: removes sv-memory allow-list entries.
- `status [--platform <p>]`: reports granted vs missing tools per platform.
- OpenCode and Codex use interactive approval and are skipped (no static allow-list).

#### 11. `sv-memory setup [agent]`

- One-shot agent integration mirroring Engram's `engram setup <agent>`: wires MCP server config, hooks/skills/plugins, protocol injection (`AGENTS.md` / `.cursorrules` / `.windsurfrules`), and MCP tool permissions for one agent.
- Supported agents: `claude-code`, `opencode`, `cursor`, `windsurf`, `antigravity`, `codex`.
- `sv-memory setup` (no args): read-only — prints per-agent install status.
- `sv-memory setup <agent>`: installs the agent end-to-end (idempotent).
- `--all`: installs every supported agent.
- `--strict`: installs strict hooks (blocks first raw file read on Antigravity; nudge-only on Claude Code).
- **Claude Code:** writes a project `.mcp.json` when the `claude` CLI is absent, installs `PreToolUse` + lifecycle hooks (`SessionStart`, `SessionEnd`, `PreCompact`, `SubagentStop`) under `.claude/hooks/` and registers them in `.claude/settings.json`, injects the `AGENTS.md` protocol, and grants the 34-tool allow-list in `~/.claude/settings.json`.
- **OpenCode:** registers the MCP server in `opencode.json`, installs `SKILL.md` plus the native TypeScript plugin `.opencode/plugin/sv-memory.ts` (adds the `sv_memory_context` tool), and injects the `AGENTS.md` protocol.
- **Cursor:** writes `.cursor/mcp.json` and injects `.cursorrules`.
- **Windsurf:** writes `.windsurf/mcp_config.json` and injects `.windsurfrules`.
- **Antigravity CLI:** registers the MCP server, installs `.agents/hooks.json` hooks, injects `AGENTS.md`, and grants the 34-tool allow-list.
- **Codex:** writes the `[mcp_servers.sv-memory]` block into `~/.codex/config.toml`, installs a no-op hook, and injects `AGENTS.md`.

#### 12. `sv-memory hooks`

- `install [--strict] [--context-injection] [--platform <p>]`: installs PreToolUse hooks (`.agents/hooks.json` + `.agents/hooks/sv-memory.sh`) so agents query memory before reading files. On Claude Code it also installs the lifecycle hooks (`SessionStart`, `SessionEnd`, `PreCompact`, `SubagentStop`) under `.claude/hooks/` and registers them in `.claude/settings.json` in the official array format. `--strict` blocks the first raw file read of each session. Default platform: all (`claude-code`, `codex`, `antigravity`, `opencode`).
- **Silent context injection (`--context-injection`, opt-in, default off):** when enabled, the Claude Code PreToolUse hook calls `sv-memory context <file>` on the first `Read` of each file and injects the compact graph+memory context pack (title + truncated `why`, bounded to 3 memories) as `additionalContext`. Output is cached per file for the session and time-bounded (2s, portable timeout); the hook always exits 0 (fail-open) so a missing binary or `.sv-memory` never breaks the tool call. Enabled by a `.sv-memory/context-injection-enabled` marker created by the flag. Antigravity, Codex, and OpenCode do not support `additionalContext` injection and keep the nudge/skill mechanism.
- **Strict degradation (fail-open):** the hook scripts never call the sv-memory server. Strict blocking is only implemented on Antigravity CLI; Claude Code strict is nudge-only (always `exit 0`). The block is skipped when sv-memory is unavailable (no `.sv-memory/`, binary missing, or `SV_MEMORY_STRICT_DISABLE=1`), so the agent is never deadlocked by a missing/unconfigured sv-memory.
- `uninstall [--context-injection] [--platform <p>]`: removes the hooks (and the context-injection marker when `--context-injection` is passed).
- `status`: reports which platforms have hooks installed and whether silent context injection is enabled.

#### 13. `sv-memory obsidian-export [-o output-dir]`

- Exports all project memories to Markdown files inside the target folder (default `.obsidian-sv-memory`) structured as an Obsidian vault.

#### 14. `sv-memory export [output-file]`

- Exports all non-deleted memories for this project to a portable JSON file.

#### 15. `sv-memory import <input-file>`

- Imports memories from a JSON file using upsert by ID.

### Memory & Session Deletion Commands

#### 16. `sv-memory delete session <session-id>`

- Deletes an empty session (fails if the session contains associated memories).

#### 17. `sv-memory delete project <project-id> [--hard]`

- Cascade-deletes all project data. Soft-deletes memories by default; `--hard` removes them permanently from SQLite.

### Project Registry Management

#### 18. `sv-memory projects list`

- Lists all registered projects with their ID, name, path, memory counts, and session counts.

#### 19. `sv-memory projects prune`

- Prunes empty projects (those with 0 memories and 0 sessions) from the central SQLite registry.

#### 20. `sv-memory projects consolidate <source-project-id> <target-project-id>`

- Merges all memories and sessions from the source project into the target project, then prunes the source project.

### Code Graph Management

#### 21. `sv-memory graph rebuild`

- Forces a full code directory scan, rebuilding graph nodes and edges.

#### 22. `sv-memory graph path <source> <target>`

- Computes and prints the shortest dependency path between two code nodes in the graph (up to 10 hops).

#### 23. `sv-memory graph explain <node>`

- Outputs detailed information for a specific node: type, label, path, metadata JSON, and fan-in/fan-out metrics.

#### 24. `sv-memory graph communities`

- Runs Leiden community detection on the graph. Lists community clusters, their member nodes, centrality scores, and god nodes.

#### 25. `sv-memory graph wiki [--output dir]`

- Exports Markdown wiki pages for each detected community, listing member files, centrality scores, and inter-community dependencies. Default output directory: `graph-wiki`.

#### 26. `sv-memory graph viz [--output file] [--open]`

- Generates an interactive HTML visualization using vis.js with community-colored physics simulation, node filtering, and tooltips. Default output: `graph.html`. Opens in the browser by default (`--open=false` to skip).

#### 27. `sv-memory graph merge <project-id-a> <project-id-b> [-o output-file]`

- Loads two project graphs and produces a union-merge by node ID, upserting nodes and edges into a JSON snapshot (default output: `merged-<a>-<b>.json`).

### Conflict Management

#### 28. `sv-memory conflicts`

- `list [--status pending|judged|ignored] [--project P]`: displays conflicting memories and detected semantic overlaps.
- `stats`: summarizes conflict relation counts by status.
- `scan [--apply] [--dry-run] [--max-insert N] [--threshold T]`: runs incremental semantic-overlap scanning; by default reports without persisting (`--apply` saves the detected `potential_conflict` relations).
- `scan --semantic [--agent claude|opencode|CMD] [--max-semantic N] [--concurrency N]`: after surfacing candidate pairs, LLM-judges them with the configured agent CLI. The agent compares full memory content and returns a verdict (`supersedes`, `conflicts_with`, `relates_to`, or `none`); verdicts are persisted with `judged_by='llm'` when `--apply`, and failed judgments stay pending for retry. Default agent: `$SV_MEMORY_SEMANTIC_AGENT` or `claude`.
- `ignore <relation-id>`: marks a detected conflict as ignored.

#### 29. `sv-memory context <path>`

- Prints a **compact context pack** for a file, package, or symbol: the node's structural role (type, fan-in/fan-out, community, hub flag) plus the memories linked to that path (`where_path` or `rationale_for` edges), each as title + truncated `why`. Flags: `--max-memories N` (default 5), `--why-chars N` (default 300), `--include-changes` (also list active spec changes affecting the path). Fast and bounded — this is the entry point the optional PreToolUse context-injection hook calls on first file read.

#### 30. `sv-memory specs`

Spec-driven change mirror management. The SQLite store is authoritative; the mirror is a git-versioned, human-readable Markdown projection under `.sv-memory/specs/`.

- `export`: writes every active change to `.sv-memory/specs/changes/<slug>.md` and moves archived/rejected changes to `.sv-memory/specs/archive/<date>-<slug>.md`, pruning orphaned mirrors.
- `import <slug>`: reconciles a human-edited mirror back into the store (only edited fields; identity never changes; a mirror can never create a change).
- `list`: shows each change's status and mirror state.
- `archive <slug>`: moves an applied change to `archived` and relocates its mirror.

The mirror is also written automatically by the incremental Git sync, so proposals stay in sync with zero manual steps.

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
    change_id TEXT,             -- links a committed decision to its change
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
    node_type TEXT NOT NULL,  -- 'file' | 'document' | 'sql' | 'package' |
                             -- 'function' | 'class' | 'module' | 'component' |
                             -- 'service' | 'concept' | ...
    label TEXT NOT NULL,
    path TEXT NOT NULL,
    metadata TEXT,            -- JSON payload
    PRIMARY KEY(project_id, id),
    FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE
);

-- Graph Edges (Directed Relationships)
CREATE TABLE IF NOT EXISTS graph_edges (
    id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    target_id TEXT NOT NULL,
    relation_type TEXT NOT NULL,              -- 'imports' | 'calls' | 'depends_on' | 'references' | 'rationale_for'
    confidence TEXT NOT NULL DEFAULT 'EXTRACTED', -- 'EXTRACTED' | 'INFERRED' | 'AMBIGUOUS'
    source_location TEXT,                     -- Line numbers/ranges
    PRIMARY KEY(project_id, id),
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
    relation_type TEXT NOT NULL, -- 'supersedes' | 'conflicts_with' | 'relates_to' |
                                -- 'potential_conflict' (candidate found by scan)
    status TEXT DEFAULT 'pending', -- 'pending' | 'judged' | 'ignored'
    score REAL,                 -- Jaccard similarity for potential_conflict
    reason TEXT,
    judged_by TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE,
    FOREIGN KEY(project_id, source_id) REFERENCES memories(project_id, id) ON DELETE CASCADE,
    FOREIGN KEY(project_id, target_id) REFERENCES memories(project_id, id) ON DELETE CASCADE
);

-- Change lifecycle (spec-driven decision engine)
-- Each proposal travels through the decision cycle:
-- 'draft' → 'proposed' → 'validated' → 'applied' (→ 'archived') | 'rejected'
CREATE TABLE IF NOT EXISTS changes (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    slug TEXT NOT NULL,          -- kebab-case, UNIQUE per project
    status TEXT NOT NULL DEFAULT 'draft',
    title TEXT NOT NULL,
    what TEXT,                   -- proposal: why and what changes
    goal TEXT,
    where_path TEXT,             -- affected code paths (AFFECTS edges)
    design TEXT,                 -- technical approach
    tasks TEXT,                  -- implementation checklist
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME,
    archived_at DATETIME,
    FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE,
    UNIQUE(project_id, slug)
);
```

> Note: in earlier databases the `status` and `score` columns are added idempotently
> by the migration in `internal/db/migrations.go`.

### Performance Indexes

```sql
CREATE INDEX IF NOT EXISTS idx_graph_edges_source ON graph_edges(project_id, source_id);
CREATE INDEX IF NOT EXISTS idx_graph_edges_target ON graph_edges(project_id, target_id);
CREATE INDEX IF NOT EXISTS idx_memories_project_created ON memories(project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_memories_project_category ON memories(project_id, category);
CREATE INDEX IF NOT EXISTS idx_memories_topic ON memories(project_id, topic_key);
CREATE INDEX IF NOT EXISTS idx_memories_hash ON memories(project_id, normalized_hash);
CREATE INDEX IF NOT EXISTS idx_memories_change ON memories(project_id, change_id);
CREATE INDEX IF NOT EXISTS idx_changes_project_status ON changes(project_id, status);
CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_sessions_active_started ON sessions(project_id, started_at DESC) WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_sessions_completed_ended ON sessions(project_id, ended_at DESC) WHERE status = 'completed';
CREATE INDEX IF NOT EXISTS idx_memory_relations_source ON memory_relations(project_id, source_id);
CREATE INDEX IF NOT EXISTS idx_memory_relations_target ON memory_relations(project_id, target_id);
```

### SQLite PRAGMA Configuration

| PRAGMA         | Value     | Purpose                                             |
| -------------- | --------- | --------------------------------------------------- |
| `journal_mode` | WAL       | Write-Ahead Logging: concurrent reads while writing |
| `synchronous`  | NORMAL    | Balanced durability/speed (crash-safe with WAL)     |
| `temp_store`   | MEMORY    | Temp tables in RAM                                  |
| `cache_size`   | -20000    | ~20 MB page cache per connection                    |
| `mmap_size`    | 268435456 | 256 MB memory-mapped I/O (avoids `read()` syscalls) |
| `busy_timeout` | 5000      | Wait 5s on lock instead of failure                  |
| `foreign_keys` | ON        | Enforce referential integrity                       |

### Connection Pool

- **Writer:** `MaxOpenConns=1` (serialized writes under WAL)
- **Reader:** `MaxOpenConns=16` (parallel reads, `?mode=ro` for lock-free concurrency; unlimited connection lifetime keeps WAL readers warm, idle pruning via `ConnMaxIdleTime`)
- **Degradation:** If reader fails to open, `Reader == Writer` (correct but slower)

---

## 6. MCP Tools Definition

`sv-memory` registers **34 MCP tools** for AI agents:

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

### 2. `sv_mem_update`

Partially update an existing memory by ID. Only the provided fields change; identity fields (id, category, session, topic_key) are preserved and the revision counter advances.

- **Parameters:**
  - `id` (string, required): The memory ID to update.
  - `what` (string, optional): New concise description.
  - `why` (string, optional): New detailed reasoning.
  - `learned` (string, optional): New rule or key lesson.
  - `where_path` (string, optional): New affected file/folder path (empty string clears it).
  - `impact` (string, optional): New achievements/what went well (empty string clears it).
  - `errors_faced` (string, optional): New errors/roadblocks (empty string clears it).
  - `next_steps` (string, optional): New pending tasks (empty string clears it).

### 3. `sv_mem_suggest_topic_key`

Generate a stable topic key in kebab-case format before saving.

- **Parameters:**
  - `category` (string, required): Memory category.
  - `what` (string, required): Title or description.
- **Returns:** Suggested key like `category/kebab-case-description`.

### 4. `sv_mem_session_start`

Register a new coding session. Returns the Auto-Boot Context Bundle (previous session summary, key decisions, standards, recent bugfixes, postmortems, recent Q&A, last journals, top graph hubs) bounded by the token budget. When a `goal` is provided, the surfaced decisions/standards/bugfixes are ranked by relevance to it instead of pure recency. When there are unresolved decision conflicts, a one-line `⚠ Pending memory conflicts` hint is appended so the agent can review them before relying on either side.

- **Parameters:**
  - `goal` (string, optional): Session objective. When set, the Auto-Boot bundle ranks the per-section candidates by relevance to it (pinned first, then keyword overlap, then recency).
  - `directory` (string, optional): Working directory.
  - `semantic` (string, optional): When `'true'` and a `goal` is given, re-ranks the bundle candidates with the configured agent CLI by semantic relevance (one batched call; fails open to the deterministic keyword ranking when the agent is unavailable). Default `'false'`.
  - `semantic_agent` (string, optional): Agent CLI for semantic recall. Defaults to `$SV_MEMORY_SEMANTIC_AGENT`, then `claude`.
  - `token_budget` (string, optional): Max tokens for the response; truncated with a notice when exceeded (default from config `max_response_tokens`, 4000; `'0'` = unlimited).

### 5. `sv_mem_session_end`

Close an active session.

- **Parameters:**
  - `session_id` (string, required): Session ID.
  - `summary` (string, optional): Accomplishments.

### 6. `sv_mem_session_summary`

Update goals, discoveries, and next steps for the session.

- **Parameters:**
  - `session_id` (string, required): Session ID.
  - `goal` (string, optional): Updated goal.
  - `discoveries` (string, optional): Findings.
  - `accomplished` (string, optional): Completed tasks.
  - `next_steps` (string, optional): Upcoming goals.
  - `files` (string, optional): Edited files list.

### 7. `sv_mem_context`

Recover context from the last completed session.

- **Parameters:**
  - `limit` (string, optional): Max memories to retrieve (default `10`).
  - `token_budget` (string, optional): Max tokens for the response; truncated with a notice when exceeded (default from config `max_response_tokens`, 4000; `'0'` = unlimited).

### 8. `sv_mem_compact`

Trigger automatic memory compaction: consolidates historical topic-key revisions and duplicates into clean summary records.

- **Parameters:** None.

### 9. `sv_mem_search` (Layer 1 Progressive Disclosure)

FTS5-powered memory search. Returns only IDs, categories, dates, titles, and topic keys.

- **Parameters:**
  - `query` (string, required): Keyword search terms.
  - `category` (string, optional): Category filter.
  - `path` (string, optional): Path/directory scope filter to narrow memories relevant to a specific file or directory. With `graph_boost` (default `true`), the recall expands to the whole graph community of that path: results for the entire module surface in one call, and community-expanded rows are annotated with a `[graph]` marker.
  - `limit` (string, optional): Max results (default `10`).
  - `offset` (string, optional): Pagination offset.
  - `match_mode` (string, optional): `'all'` (default) requires every token to match; `'any'` matches memories matching one or more tokens for broader recall.
  - `semantic` (string, optional): When `'true'`, re-ranks the keyword candidates with the configured agent CLI by semantic relevance (opt-in, one batched call; fails open to keyword results when the agent is unavailable). Default `'false'`.
  - `semantic_agent` (string, optional): Agent CLI for semantic recall. Defaults to `$SV_MEMORY_SEMANTIC_AGENT`, then `claude`.
  - `token_budget` (string, optional): Max tokens for the response; truncated with a notice when exceeded (default from config `max_response_tokens`, 4000; `'0'` = unlimited).

### 10. `sv_mem_timeline` (Layer 2 Progressive Disclosure)

Retrieve a chronological list of observations centered around a specific memory.

- **Parameters:**
  - `observation_id` (string, required): Memory ID.
  - `before` (string, optional): Count of memories preceding (default `5`).
  - `after` (string, optional): Count of memories succeeding (default `5`).
  - `token_budget` (string, optional): Max tokens for the response; truncated with a notice when exceeded (default from config `max_response_tokens`, 4000; `'0'` = unlimited).

### 11. `sv_mem_get` (Layer 3 Progressive Disclosure)

Retrieve all fields of a specific memory. Text fields are truncated beyond `max_chars`.

- **Parameters:**
  - `id` (string, required): Memory ID.
  - `max_chars` (string, optional): Max characters per field (default `1000`; `'0'` = unlimited).
  - `token_budget` (string, optional): Max tokens for the response; truncated with a notice when exceeded (default from config `max_response_tokens`, 4000; `'0'` = unlimited).

### 12. `sv_mem_judge`

Create a relation (judgment) between two memories to maintain continuity or record conflicts.

- **Parameters:**
  - `source_id` (string, required): Newer memory ID.
  - `target_id` (string, required): Older memory ID.
  - `relation_type` (string, required): `supersedes` | `conflicts_with` | `relates_to`.
  - `reason` (string, optional): Reasoning.
  - `judged_by` (string, optional): Judge identity (default `'agent'`).

### 13. `sv_mem_compare`

Compare two memories side-by-side in Markdown format.

- **Parameters:**
  - `id1` (string, required): First memory ID.
  - `id2` (string, required): Second memory ID.

### 14. `sv_mem_review`

Find memories needing maintenance (e.g. stale, excessive duplicate counts, consolidation candidates), reset a memory's policy-review deadline, or prune stale transient memories.

- **Parameters:**
  - `action` (string, optional): `'list'` (default), `'mark_reviewed'`, or `'prune_stale'`.
  - `id` (string, optional): Required for `action='mark_reviewed'`: the memory ID to mark as reviewed. Resets `review_after` to `now + decay(category)`.
  - `older_than_days` (string, optional): For `action='prune_stale'`: prune memories not seen/created within this many days (default from config `prune_stale_days`, 90).
  - `category` (string, optional): For `action='prune_stale'`: comma-separated categories to prune instead of the default transient set (`journal,qa,discussion,idea`). Durable categories (decision, standard, architecture, postmortem, bugfix) are never pruned unless explicitly listed here.
  - `apply` (string, optional): For `action='prune_stale'`: `'true'` to actually soft-delete; default `'false'` (dry run — only lists what would be pruned). Pinned memories are never pruned.

### 15. `sv_mem_stats`

Provides aggregate metrics (counts, breakdown by category) plus the current active project (ID, name, path), and the **session token ledger**: an estimate of the tokens (chars/4) injected into the agent context since the last `sv_mem_session_start` (Auto-Boot bundle + bulk-returning read tools), alongside the `max_response_tokens` budget — so the agent can decide when to compact.

- **Parameters:**
  - `token_budget` (string, optional): Max tokens for the response; truncated with a notice when exceeded (default from config `max_response_tokens`, 4000; `'0'` = unlimited).

### 16. `sv_mem_diagnose`

Run read-only health checks for the active project: database file, schema tables, FTS5 triggers, project registration, write permissions, chunk directory, and structural graph integrity (dangling edges, orphan nodes, self-loops, missing files). Combines the memory `RunDiagnostics` checks with `graph.DiagnoseGraph`.

- **Parameters:**
  - `token_budget` (string, optional): Max tokens for the response; truncated with a notice when exceeded (default from config `max_response_tokens`, 4000; `'0'` = unlimited).

### 17. `sv_mem_delete`

Deletes a memory. Soft-deletes by default; set `hard` to `'true'` to erase permanently.

- **Parameters:**
  - `id` (string, required): Memory ID.
  - `hard` (string, optional): `'true'` for permanent delete.

### 18. `sv_mem_pin`

Pin a local memory so it surfaces first in `sv_mem_context` (key decisions stay visible), or clear it with `action='unpin'`. Pinned state is local to this device.

- **Parameters:**
  - `id` (string, required): Memory ID.
  - `action` (string, optional): `'pin'` (default) or `'unpin'`.

### 19. `sv_mem_capture_passive`

Logs a lightweight journal entry automatically (e.g., test outcomes, file changes).

- **Parameters:**
  - `what` (string, required): Summary description.
  - `why` (string, required): Context or rationale.

### 19b. `sv_mem_context_pack`

Builds a **compact, fused context pack** for a code path (file, package, or symbol) with **transparent auto-freshness** (runs an mtime staleness probe to sync modified files on demand): includes the node's structural role in the dependency graph (type, fan-in/fan-out, community, hub flag), a **surgical source code snippet** of the resolved symbol (up to 60 lines, eliminating separate file reads), a **transitive blast radius analysis** (multi-hop upstream dependents/callers with hop depth and hub indicators), plus the memories linked to that path via `where_path` or `rationale_for` edges (decisions, standards, bugfixes), each rendered as title + truncated `why`. With `include_changes='true'` it also lists active spec changes (proposals) whose `where_path` matches the path. One bounded call replaces the `sv_graph_explain` + `sv_mem_search path=` + several `sv_mem_get` round-trips, saving tokens. This is the proprietary graph→memory bridge that feeds the optional silent context-injection hook.

- **Parameters:**
  - `path` (string, required): File path, package name, or symbol to resolve.
  - `include_changes` (string, optional): When `'true'`, also list active spec changes affecting the path (default `'false'`).
  - `token_budget` (string, optional): Max tokens for the response; truncated with a notice when exceeded (default from config `max_response_tokens`, 4000; `'0'` = unlimited).
- **Config:** `context_pack_max_memories` (default `5`, max `20`) caps the linked memories; each `why` is truncated to `bundle_why_chars`.

### 19c. `sv_mem_capture_prompt`

Captures the **user's prompt** as a local observation attached to a session (Engram `mem_save_prompt` parity). Records what the user asked so future sessions have context about user goals after compaction.

- **Parameters:**
  - `content` (string, required): The user's prompt text. Secrets are redacted before write.
  - `session_id` (string, optional): Session to associate the prompt with; defaults to the active session.
- **Storage:** prompts live in the local `user_prompts` SQLite table (FTS5-indexed) and are **not** part of the git sync payload in this phase — they are local-only. Recoverable via `sv_mem_context` (recent prompts of the last session) and counted by `sv_mem_stats` (`Total user prompts`).

### 19d. `sv_mem_merge_projects`

Merges project name variants into a single canonical project (Engram `mem_merge_projects` parity, admin). Moves all memories, sessions, relations, and graph data from `from` into `to`, then deletes the source project.

- **Parameters:**
  - `from` (string, required): Source project ID to move data from and then delete.
  - `to` (string, required): Target project ID receiving the data.
- **Notes:** mirrors the `sv-memory projects consolidate <source> <target>` CLI. Both projects must exist and be distinct.

### 19e. `sv_propose_spec`

Registers a **spec change** (proposal) for the spec-driven decision engine: creates the change and advances it to the `proposed` lifecycle state, then runs a **pre-flight check** against the project's rules and invariants (memories in categories `standard`, `decision`, `architecture`). A pinned rule whose tokens overlap the proposal returns a **BLOCK** verdict; an ordinary overlap returns **WARN**; otherwise **PASS**. Use before writing code.

- **Parameters:**
  - `slug` (string, required): Kebab-case, project-unique identifier (e.g. `implement-session-auth`).
  - `title` (string, required): Concise title of the proposal.
  - `what` (string, optional): Why and what changes — the proposal body.
  - `goal` (string, optional): Intent of the proposal.
  - `where_path` (string, optional): Affected code path (file/folder/package) used for AFFECTS wiring and context-pack recall.
  - `design` / `tasks` (string, optional): Technical approach / implementation checklist.
  - `token_budget` (string, optional): Max tokens for the response.
- **Lifecycle:** `draft` → `proposed` → `validated` → `applied` (→ `archived`) | `rejected`. The committed decision memory gets `topic_key` `decision/<slug>`.
- **Config:** `conflict_threshold` (default `0.45`) is the Jaccard similarity at or above which an existing rule is considered in conflict.

### 19f. `sv_validate_decision`

Re-checks an existing change's proposal against the project's rules and invariants, returning a **PASS/WARN/BLOCK** verdict. Deterministic by default (SQLite FTS5 + Jaccard, zero LLM cost); `semantic='true'` opts into a single batched agent re-ranking by meaning (fails open to the deterministic verdict when the agent is unavailable).

- **Parameters:**
  - `change_id` (string, required): The change ID returned by `sv_propose_spec`.
  - `semantic` (string, optional): `'true'` to enable agent re-ranking (default `'false'`).
  - `semantic_agent` (string, optional): Agent CLI override (default `$SV_MEMORY_SEMANTIC_AGENT`, then `claude`).
  - `token_budget` (string, optional): Max tokens for the response.

### 19g. `sv_commit_spec`

Promotes a validated change into a **durable decision/standard memory**: saves the decision via the memory engine (`topic_key` `decision/<slug>`), links it to the change via `change_id`, wires the `rationale_for` edge to the affected code path, records `conflicts_with` relations for any pre-flight WARN/BLOCK rules, and stamps the change `applied`. A **BLOCK** verdict (pinned invariant) rejects the commit unless `force='true'` explicitly overrides it.

- **Parameters:**
  - `change_id` (string, required): The change ID to commit.
  - `category` (string, optional): Memory category (default `'decision'`; use `'standard'` for a reusable rule).
  - `force` (string, optional): `'true'` overrides a pre-flight BLOCK and commits anyway (default `'false'`).
  - `token_budget` (string, optional): Max tokens for the response.

### 20. `sv_graph_query`

Queries structural relations using a Breadth-First Search (BFS). By default it returns a compact, token-efficient textual edge list (`source →[rel]→ target`); pass `mermaid=true` to render a Mermaid diagram instead.

- **Parameters:**
  - `path_or_node` (string, required): File path or module to center on.
  - `depth` (string, optional): Hop distance (default `1`).
  - `relation_type` (string, optional): Filter (e.g., `'imports'`, `'calls'`, `'depends_on'`).
  - `direction` (string, optional): Traversal direction: `'in'` | `'out'` | `'all'` (default `'out'`).
  - `mermaid` (string, optional): Render the edges as a Mermaid diagram instead of the compact textual edge list (default `'false'`).
  - `token_budget` (string, optional): Max tokens for the response; the response is truncated with a notice when exceeded (default from config `max_response_tokens`, 4000; `'0'` = unlimited).

### 21. `sv_graph_path`

Finds the shortest dependency route between two graph nodes.

- **Parameters:**
  - `source` (string, required): Source node ID.
  - `target` (string, required): Target node ID.
  - `max_hops` (string, optional): Hop limit (default `10`).

### 22. `sv_graph_sync`

Triggers an incremental scan of modified files to sync nodes/edges. Invalidates cache.

- **Parameters:** None.

### 23. `sv_mem_conflicts`

Detects and surfaces conflicting memories with semantic overlap analysis, and can LLM-judge candidate pairs.

- **Parameters:**
  - `action` (string, required): Action to perform: `list`, `scan`, or `ignore`.
  - `status` (string, optional): Status filter for `list` (`pending`, `judged`, `ignored`).
  - `relation_id` (string, optional): Required for `ignore`: the conflict relation ID to ignore.
  - `threshold` (string, optional): Similarity threshold for `scan` (default `0.45`).
  - `apply` (string, optional): For `scan`: `'true'` to save scanned conflicts / semantic judgments to the database (default `'false'`).
  - `semantic` (string, optional): For `scan`: `'true'` to LLM-judge candidate conflicts with the agent CLI (default `'false'`).
  - `agent` (string, optional): Agent CLI for semantic judging (`claude`, `opencode`, or a custom command; default `$SV_MEMORY_SEMANTIC_AGENT` or `claude`).
  - `max_semantic` (string, optional): Maximum candidate pairs to judge (default: all).
  - `concurrency` (string, optional): Parallel agent judgments (default `3`).

### 24. `sv_graph_explain`

Outputs detailed information for a specific graph node: type, label, path, metadata, and fan-in/fan-out metrics.

- **Parameters:**
  - `node` (string, required): File path or node ID.

### 25. `sv_graph_god_nodes`

Identifies the most connected nodes in the graph based on betweenness centrality and degree. Returns a ranked list of god nodes with metrics.

- **Parameters:**
  - `top_n` (string, optional): Max results to return (default `10`).

### 26. `sv_graph_surprising_connections`

Finds non-obvious or unexpected dependency paths in the graph. Highlights structural anomalies that may indicate architectural concerns.

- **Parameters:**
  - `limit` (string, optional): Max connections to return (default `10`).

### 27. `sv_graph_viz`

Generates an interactive HTML visualization of the graph using vis.js with community coloring, physics simulation, node filtering, and tooltips.

- **Parameters:**
  - `output` (string, optional): Output file path (default `graph.html`).

### 28. `sv_graph_merge`

Merges two project graphs into one (union-merge by node ID), upserting nodes and edges.

- **Parameters:**
  - `project_a` (string, required): First project ID.
  - `project_b` (string, required): Second project ID.
  - `output` (string, optional): Output JSON file path.

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

1. Compute SHA256 hash of `what + "\x00" + why + "\x00" + learned + "\x00" + where_path`.
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

**Redaction is applied end-to-end, not only on the save path:**

- **Writes:** `SanitizeText` runs on the normal save/update path, on session summaries (`EndSession`, `SaveSessionSummary`), on relations/judgments, and via the shared `sanitizeMemoryFields` helper on every import path (`ImportJSON`, git chunk import, monolithic `memories.json`).
- **Graph:** content-derived node text (markdown headings, rationale comments `TODO:`/`WHY:`, SQL DDL defaults/values) is redacted at parse time and again by `sanitizeNodeForPersist` before `graph_nodes` upserts. `.env`/key/PEM/credential files are never scanned (not in the supported extensions), and the default `.sv-memoryignore` additionally excludes `.env*`, `*.pem`, `*.key`, `*.p12`, `id_rsa*`, `credentials*`, `.ssh/`, `.aws/`, `.gcp/`, `secrets.yaml`.
- **Reads/exports:** search/get, the Auto-Boot bundle, session context, the Obsidian vault exporters, wiki, and Cypher re-apply redaction so sanitized values never re-surface raw. The generated `graph.html` escapes node id/type/path/metadata, closing the stored-XSS sink in the detail panel.

The SQLite store lives outside the repository (`~/.config/sv-memory/storage.db`), so only the redacted per-memory chunk JSON is committed to Git.

---

## 8. Agent Protocol Template

When initialized, `sv-memory` injects the following protocol block into `AGENTS.md`, `.cursorrules`, or `.windsurfrules` (source of truth: `internal/protocol/protocol.go`):

```markdown
<!-- SV-MEMORY:START -->

# SV-Memory Protocol Rules

This project uses 'sv-memory' for persistent architectural memory, progress journals, and structural context graph.

## Session Lifecycle (REQUIRED, in this order):

1. **Start:** Call 'sv_mem_session_start' at the beginning of work. It returns an **Auto-Boot Context Bundle** with the previous session summary, key architectural decisions, standards, recent bugfixes, postmortems, recent Q&A, last journals, and top graph hubs read it and use it as your starting context.
2. **Associate saves:** Pass 'session_id' to 'sv_mem_save' to group memories under the active session. If omitted, the active session is auto-detected.
3. **Capture knowledge as you go:** Save journals, decisions, standards, and bugfixes with 'sv_mem_save' (see the Memory Capture Guidelines below). Use 'sv_mem_capture_passive' for lightweight observations that do not need an explicit save decision.
4. **Summary:** Call 'sv_mem_session_summary' with goal, discoveries, accomplished work, and next steps before closing.
5. **End:** Call 'sv_mem_session_end' to mark the session as completed and enable context recovery in the next session.

After a compaction or context reset, call 'sv_mem_context' to recover the last session state (goal, summary, associated memories).

## Tool Usage in Any Mode:

The sv-memory tools (session, memory, graph, diagnostics) may be called in ANY operational mode plan, build, or review. They persist only to the project memory store ('.sv-memory/'), which is project data, not source code. Do not skip memory capture, context recovery, or the session lifecycle because of the current mode.

## Context Initialization (Search-Before-Work):

Memory must be consulted before proposing or executing changes:

- **Orientation:** On a new project, call 'sv_mem_stats' first it is the cheapest overview of memory distribution (categories, counts, sessions).
- **Targeted search:** Call 'sv_mem_search' with the topic keywords of your task (feature, component, style, module). Filter by category when relevant ('journal', 'postmortem', 'discussion', 'idea', 'qa', 'architecture', 'decision'). Avoid repeating redundant searches the Auto-Boot Bundle already carries the previous session context.
- **Proactive search:** On first user message referencing a project, feature, or problem, call 'sv_mem_search' with their keywords before responding. Never answer from assumptions alone memory first, code second.

## Progressive Disclosure (Token-Efficient Retrieval):

Use the 3-layer pattern to minimise tokens:

- **Layer 1 Search:** Call 'sv_mem_search' to get a compact list (IDs + titles + topic keys) of relevant memories (~30 tokens/result).
- **Layer 2 Timeline:** Call 'sv_mem_timeline(observation_id=...)' to see chronological context around a specific memory (includes the central observation rationale).
- **Layer 3 Get full content:** Call 'sv_mem_get(id=...)' to retrieve the full content of a specific memory.
  Never dump all fields from search drill down on demand. The top search result is already expanded inline, so only drill further when you need deeper detail.

## Topic Keys (Upsert Semantics):

- Use 'sv_mem_suggest_topic_key(category, what)' to generate a stable 'category/kebab-case' key.
- Pass 'topic_key' to 'sv_mem_save' to enable upsert: saves to the same project+topic update in place (revision_count++) instead of creating a new record.
- Use topic keys for evolving topics (architecture decisions, design systems, long-running features, recurring patterns). Skip for one-off bugs or single facts.
- **Convention:** Always kebab-case in English. Examples: 'standard/design-system', 'architecture/component-card', 'decision/use-bun-instead-of-npm', 'standard/workflow-git-commits', 'bugfix/tab-transition-absolute-position'.

## Memory Capture Guidelines (when to save what):

Always persist design knowledge as structured memories with a topic_key, not just session journals:

| Situation                                            | Category       | topic_key example             |
| :--------------------------------------------------- | :------------- | :---------------------------- |
| Visual style / design system / CSS / Tailwind tokens | 'standard'     | standard/design-system        |
| Reusable component or UI pattern                     | 'architecture' | architecture/component-card   |
| Workflow / methodology / build & dev process         | 'standard'     | standard/workflow-dev-process |
| Architectural decision made (and its rationale)      | 'decision'     | decision/...                  |
| Code convention / naming / folder structure          | 'standard'     | standard/code-conventions     |
| Complex or non-obvious bug fixed                     | 'bugfix'       | bugfix/...                    |
| Relevant Q&A with lasting value                      | 'qa'           | qa/...                        |
| Rejected library or framework feature                | 'decision'     | decision/avoid-...            |
| Session progress checkpoint                          | 'journal'      | journal/...                   |

**Golden rule:** when you define, change, or reuse a style, component, methodology, or convention, save it as 'standard' or 'architecture' with a topic_key. A journal is not a substitute aaaajkjkkj journals document progress, 'standard'/'architecture'/'decision' preserve the "how" and the "why" for future sessions.

## Graph Inspection (before modifying code):

- **Orient before touching code:** Call 'sv_graph_god_nodes' to see the most-connected hub nodes these are the architectural hotspots any change may ripple through.
- **Understand a module:** Call 'sv_graph_explain(node=...)' before refactoring, deleting, or restructuring a file/module. It reports the node's role, community, centrality, fan-in/fan-out, neighbors, and suggested questions.
- **Inspect dependencies:** Call 'sv_graph_query(path_or_node=...)' to see a module's dependency sub-graph (imports/calls/depends_on) with depth, direction, and relation-type filters.
- **Trace a connection:** Call 'sv_graph_path(source=..., target=...)' to find the shortest dependency path between two nodes.

## Graph Refresh:

Execute 'sv_graph_sync' after adding major new files, creating new packages, or modifying package structures/imports. The graph is rebuilt incrementally and communities/centrality are computed lazily when queried.

## Memory Maintenance (periodic):

- **Review:** Call 'sv_mem_review' to list stale, duplicate, or consolidation-candidate memories.
- **Conflicts:** Call 'sv_mem_conflicts action=scan' to detect potential duplicate memories; judge them with 'sv_mem_judge' (supersedes / conflicts_with / relates_to) or with 'action=scan semantic=true' to LLM-judge candidates via the agent CLI. Keep relations coherent.
- **Compare:** Call 'sv_mem_compare(id1, id2)' before judging two similar memories.
- **Compact:** Call 'sv_mem_compact' periodically or after many topic-key upserts to consolidate revisions and keep search fast.

## Tool Quick Reference:

- **Session:** sv_mem_session_start, sv_mem_session_summary, sv_mem_session_end, sv_mem_context
- **Memory CRUD:** sv_mem_save, sv_mem_update, sv_mem_get, sv_mem_delete, sv_mem_search, sv_mem_timeline
- **Pin / Priority:** sv_mem_pin (action='unpin' to clear)
- **Knowledge quality:** sv_mem_suggest_topic_key, sv_mem_judge, sv_mem_compare, sv_mem_compact, sv_mem_review, sv_mem_capture_passive, sv_mem_conflicts, sv_mem_stats, sv_mem_diagnose
- **Graph:** sv_graph_query, sv_graph_explain, sv_graph_god_nodes, sv_graph_path, sv_graph_sync, sv_graph_surprising_connections, sv_graph_viz, sv_graph_merge

## Repository Restrictions & Commit Standards:

- **Commit Format:** Always provide commit messages using the Conventional Commits format (e.g., 'feat(scope): description'). Use the project's configured commit language (default: English), unless the project specifies otherwise.
- **Forbidden Actions:** You MUST NOT run 'git add', 'git commit', or 'git push' commands autonomously. The user must review changes and run these commands manually.
<!-- SV-MEMORY:END -->
```

---

## 9. Project Layout

```text
sv-memory/
├── cmd/
│   └── sv-memory/
│       ├── main.go              # Cobra root command registration, version & CLI execution
│       ├── cmd_init.go          # init, mcp, tui subcommands
│       ├── cmd_memory.go        # diagnose, stats, export, import, sync, obsidian-export
│       ├── cmd_projects.go      # delete session/project, projects list/prune/consolidate
│       ├── cmd_graph.go         # graph rebuild/path/explain/communities/wiki/viz/merge
│       ├── cmd_conflicts.go     # conflicts list/stats/scan/ignore
│       ├── cmd_configure.go     # interactive configure wizard
│       ├── cmd_config.go        # configure get/set/list (viper YAML)
│       ├── cmd_permissions.go   # permissions list/status/grant/revoke
│       ├── cmd_hooks.go         # hooks install/uninstall/status
│       └── cmd_update.go        # self-update command
├── internal/
│   ├── config/                  # App paths, settings parsing, viper config & configure wizard
│   ├── db/                      # DB initialization, composite migrations, WAL pools & PRAGMAs
│   ├── graph/                   # Code scanner, BFS query, Leiden communities, betweenness
│   │   │                        # centrality, god nodes, HTML viz, wiki export, graph merge,
│   │   │                        # surprising connections, incremental updates, stale checks
│   │   ├── extractor/           # tree-sitter extractor, regex fallback, markdown semantics
│   │   └── schema/              # Node/Edge structs
│   ├── hook/                    # PreToolUse hooks generation & templates
│   ├── mcp/                     # MCP server + 34 tool handlers; reads from internal/graph LRU cache
│   ├── memory/                  # CRUD, sessions storage, dedup, conflicts, compaction,
│   │                            # chunked git sync, Obsidian/Cypher export, stats
│   ├── perm/                    # MCP tool allow-list management (antigravity/claude-code)
│   ├── protocol/                # AGENTS.md / editor rules injection
│   ├── security/                # Regex secrets sanitizer
│   └── tui/                     # Interactive terminal UI (charmbracelet/huh + bubbletea)
├── documentation/
│   ├── requirement.md           # Product constraints
│   ├── spect.md                 # This specification
│   ├── CODEBASE-GUIDE.md        # Codebase tour: key data flows, extension points
│   └── getting_started_guide.md # Step-by-step installation & onboarding guide
├── AGENTS.md                    # Injected protocols block (committed)
├── CHANGELOG.md                 # Release notes
├── CONTRIBUTING.md              # Contribution guidelines
├── SECURITY.md                  # Vulnerability reporting policy
├── CODE_OF_CONDUCT.md           # Community code of conduct
├── Makefile                     # build/test/lint/vet/install targets
├── .golangci.yml                # Linter configuration
├── .github/workflows/           # CI + release pipelines
├── install.sh                   # Unix setup script
├── install.ps1                  # Windows setup script
├── .sv-memory/
│   └── chunks/                  # Portable team-shared per-memory JSON (committed)
├── go.mod
├── go.sum
└── README.md                    # Core project introduction
```

---

## 10. Language Support for Dependency Graph

Parsing uses **tree-sitter** (`gotreesitter`) for the primary languages, with a regex fallback for the rest. The scanner also handles `Markdown` (headings, code blocks, wikilinks) and `SQL`.

| Language   | Extensions    | Import Detection Mechanism                                 |
| ---------- | ------------- | ---------------------------------------------------------- |
| Go         | `.go`         | tree-sitter (`import "path"`)                              |
| Python     | `.py`         | tree-sitter (`import x`, `from x import y`)                |
| JavaScript | `.js`, `.jsx` | tree-sitter (`import ... from`, `require()`, `import()`)   |
| TypeScript | `.ts`, `.tsx` | tree-sitter (types, generics, type annotations)            |
| PHP        | `.php`        | tree-sitter (`use Namespace`, `include`, `require`)        |
| Rust       | `.rs`         | tree-sitter (`use path`, `mod path`, `extern crate`)       |
| Ruby       | `.rb`         | tree-sitter (`require`, `load`, `require_relative`)        |
| Java       | `.java`       | tree-sitter (`import package`)                             |
| HTML       | `.html`       | tree-sitter (script src, link stylesheet)                  |
| CSS        | `.css`        | tree-sitter (`@import 'path'`, `@import url(...)`)         |
| Bash       | `.sh`         | tree-sitter (`source`, `. script.sh`)                      |
| Astro      | `.astro`      | regex (frontmatter imports block)                          |
| Vue        | `.vue`        | regex (`<script>` block imports)                           |
| Svelte     | `.svelte`     | regex (`<script>` block imports)                           |
| Lua        | `.lua`        | regex (`require()`, `dofile()`, `loadfile()`)              |
| Markdown   | `.md`         | regex + semantic parser (headings, code blocks, wikilinks) |
| SQL        | `.sql`        | scanner-level (table/column references)                    |

---

## 11. Token Optimization Features

| Feature                                                | Mechanism                                                                                                                                 | Estimated Savings                                         |
| ------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------- |
| Progressive 3-layer disclosure                         | Search returns compact rows (~30 tokens/result); full content on demand                                                                   | 60-80% of response tokens                                 |
| Session compaction                                     | Full conversation → structured journal entry (200-500 tokens)                                                                             | 80-90% vs raw history                                     |
| Field truncation (`sv_mem_get`)                        | `max_chars` cap per text field (default 1000)                                                                                             | Prevents unbounded token consumption                      |
| Tunable truncation thresholds                           | `max_field_chars`, `search_expand_chars`, `timeline_why_chars`, `bundle_why_chars` config keys override the compiled-in caps via YAML     | Tune response size without recompiling                    |
| Session-start token guard (`sv_mem_session_start`)      | Auto-Boot Bundle + Graph Hubs bounded by `max_response_tokens` / per-call `token_budget`                                                  | Bounds the largest pre-tool payload of each session       |
| Context Pack (`sv_mem_context_pack`)                     | One bounded call fuses graph role + linked memories for a path (`where_path`/`rationale_for`), replacing explain+search+get round-trips      | 1 call instead of 3+; title + truncated `why` only        |
| Silent context injection (hooks `--context-injection`)    | Claude Code first-Read injects `sv-memory context <file>` as additionalContext (bounded to 3 memories, cached per file)                    | Relevant context at the exact moment, no search round-trip |
| Topic key upsert                                       | Update in-place instead of accumulating revisions                                                                                         | 50% fewer redundant search results                        |
| Rolling-window dedup                                   | Suppress identical saves within 24h                                                                                                       | Prevents duplicate bloat                                  |
| Compact search SQL                                     | SELECT only 7 needed columns instead of all 20                                                                                            | ~60% less I/O per search                                  |
| Token budget benchmark (`BenchmarkToolResponseTokens`) | Regression guard measuring per-call bytes and estimated tokens for `sv_mem_search`, `sv_mem_get`, `sv_mem_timeline`, and `sv_mem_context` | Guards against unbounded response growth between releases |

---

## 12. Memory Relations & Conflict Surfacing

To maintain coherence across long-term agent interactions, `sv-memory` includes a conflict detection and resolution system.

- **The `memory_relations` Table:** Tracks how memory nodes relate dynamically.
- **Relation Types:**
  - `supersedes`: A newer decision overrides an old guideline (deactivates or flags the target memory).
  - `conflicts_with`: Two decisions explicitly contradict each other. Flagged for manual review.
  - `relates_to`: Loose association between memories (e.g. standard relates to bugfix).
  - `potential_conflict`: A candidate detected by the incremental scan, initially with `status='pending'`.
- **Conflict Lifecycle:**
  1. **Detection:** Run `sv-memory conflicts scan` (CLI) or `sv_mem_conflicts` with `action=scan` (MCP). The scan is incremental (O(new memories × total) instead of O(N²)), caches tokenizations, and inserts at most `--max-insert N` (default 100) relations. By default it only reports; `--apply` (or `apply='true'`) persists the detected `potential_conflict` relations.
  2. **Review:** `sv-memory conflicts list` / `sv_mem_conflicts` with `action=list` displays pending conflicts. Calling `sv_mem_review` also highlights pending conflicts.
  3. **Resolution:** Judge the pair with `sv_mem_judge` (promoting to `supersedes`/`conflicts_with`/`relates_to`), run a **semantic LLM scan** (`sv-memory conflicts scan --semantic` / `sv_mem_conflicts action=scan semantic=true`) which automates the same judgment with the configured agent CLI, or mark it as reviewed/ignored with `sv-memory conflicts ignore <relation-id>` (or `action=ignore`).

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
- **Unification Edges (`rationale_for`):** Connecting a memory to a code entity links the _Why_ directly to the _What_. Traversing the code graph via `sv_graph_query` retrieves both related imports/calls and associated decisions, giving developers and agents full context at the point of interest.

**Native memory→code wiring:** when a memory is saved via `sv_mem_save` (or updated with `sv_mem_update`) and provides a `where_path`, sv-memory automatically upserts a `document` graph node for that memory (id = memory ID) and a `rationale_for` edge to the canonical code node at that path (best-effort: no-op when the graph is not built yet or the path is unknown). This means:

- `sv_graph_explain`/`sv_graph_query` on a file surfaces the associated decisions/standards under the **Memory/Decision** rationale neighbors.
- The Obsidian vault export links each memory note to its code files through the same edges.
- After a full graph rebuild (`sv-memory graph rebuild`, `sv_graph_sync`), the links are re-created automatically from all active memories with a `where_path`.

**Call edge extraction (AST-precision):** `calls` edges are produced per file by preferring the AST (tree-sitter or standard `go/parser` for Go) with confidence `EXTRACTED` and a precise `L<line>:<col>` source location, resolving each call site against the project's function/class nodes (same file first, then unique cross-file match within the language group). Files whose language has no AST call coverage (Lua, Markdown, shell, Vue/Svelte/Astro script blocks) fall back to the tokenize heuristic with confidence `INFERRED`. The AST path does not capture identifiers inside strings or comments, eliminating a class of false positives the heuristic produces. This improves the precision of `sv_graph_query`, `sv_graph_explain`, god nodes, and community detection on languages with AST coverage (Go, Python, JS/TS, Java, PHP, Ruby, Rust, CSS, HTML).

---

## 15. Spec-Driven Decision Engine

Integrates OpenSpec's spec-driven philosophy natively: proposals are first-class graph citizens that travel through a **propose → validate → commit** lifecycle before code is written, and the graph/memory layer becomes an active governance engine instead of a passive store.

### Change Lifecycle

A **change** is a proposal (slug-unique per project) stored in the `changes` table, traveling through the states:

```
draft → proposed → validated → applied (→ archived) | rejected
```

- `draft` — transient state while `sv_propose_spec` wires the change (capability path, delta requirements) before advancing it to `proposed`.
- `proposed` — created by `sv_propose_spec` (pre-flight check runs here).
- `validated` — `sv_validate_decision` returned PASS/WARN for the proposal (a BLOCK keeps it `proposed`).
- `applied` — `sv_commit_spec` promoted the proposal into a durable `decision`/`standard` memory and stamped it applied (`archived_at` set).
- `archived` / `rejected` — terminal history states (excluded from context-pack recall and the Auto-Boot "Active changes" hint).

Committed decisions get `topic_key` `decision/<slug>` and are linked back to the change via `memories.change_id`.

### Pre-Flight Validation

`sv_propose_spec` and `sv_validate_decision` run a pre-flight check against the project's rules and invariants (memories in categories `standard`, `decision`, `architecture`):

- **Deterministic (default):** FTS5 token match + Jaccard similarity against `conflict_threshold` (default `0.45`). A **pinned** rule at or above the threshold → **BLOCK** (invariant the agent must not silently violate); a non-pinned overlap → **WARN**; none → **PASS**.
- **Semantic (opt-in):** `semantic='true'` re-ranks the deterministic candidates by meaning with the configured agent CLI (single batched call), elevating confirmed overlaps to BLOCK. Fails open to the deterministic verdict when the agent is unavailable.

`sv_commit_spec` enforces the gate: a BLOCK rejects the commit unless `force='true'` explicitly overrides the invariant (and the agent should then update/archive the conflicting rule). On commit, `conflicts_with` relations are recorded for every flagged rule so the pending-conflict surfacing (Auto-Boot hint, `sv_mem_conflicts`) sees them.

### Graph Integration

The decision engine extends the graph vocabulary (values are free-form TEXT, no destructive migration):

- **Node types:** `spec`, `decision`, `rule` (in addition to the scanner's `file`/`function`/`class`/... and the memory `document` nodes).
- **Edge types:** `affects` (a change touches code entities via `where_path`), `constrains` (a rule bounds a decision), `implements` (a decision/entity fulfills a spec requirement).

The Auto-Boot bundle surfaces a `📋 Active changes: N` hint when non-terminal changes exist (zero token cost when healthy), and `sv_mem_context_pack(include_changes='true')` lists the proposals affecting a path so the agent reviews them before modifying the code.

### Markdown Mirror (human-visible, bidirectional)

Every change is auto-projected into the repo as a git-versioned Markdown file under `.sv-memory/specs/`, so humans read plain text while the SQLite store stays authoritative (no drift):

- **Layout:** active changes at `.sv-memory/specs/changes/<slug>.md`; archived/rejected at `.sv-memory/specs/archive/<date>-<slug>.md`.
- **Format:** a `# Title` heading, identity/status bullet lines, and `## Proposal` / `## Goal` / `## Design` / `## Tasks` sections. Only the human-editable content round-trips on import.
- **Bidirectional:** `sv-memory specs import <slug>` parses a human-edited mirror and reconciles the changed fields back into the DB (identity fields are never modified, and a mirror can never create a change). `sv-memory specs export` refreshes the whole tree and prunes orphaned mirrors; `sv-memory specs archive <slug>` moves an applied change to `archived`.
- **Automatic:** the incremental Git sync (`SyncToGit`) writes the mirror after every chunk pass, best-effort — a mirror failure is logged and never fails the memory sync.

---

_Specification v3 reflecting the full implementation as of August 2026._
