# Codebase Guide

A tour of the sv-memory codebase: how the pieces fit together, the key data
flows (saving a memory, querying the graph, syncing, conflict detection), and
where to look when extending the project. It complements the specification
([spect.md](spect.md)) by focusing on **flows**, not interfaces.

## Package map

| Package | Responsibility | Key entry points |
| :--- | :--- | :--- |
| `cmd/sv-memory/` | Cobra CLI: root command registration, `init`, `mcp`, `setup`, `configure`, `hooks`, `permissions`, `graph`, `conflicts`, `projects` | `main.go`, `cmd_init.go`, `cmd_setup.go` |
| `internal/config/` | Paths, viper YAML config, `configure` wizard + MCP config writers (cursor/windsurf/claude) | `config.go`, `configure.go` |
| `internal/db/` | SQLite open/tuning, migrations, WAL reader/writer pool | `db.go`, `pool.go`, `migrations.go` |
| `internal/graph/` | Scanner, dependency graph build (incremental + full), BFS query, Leiden communities, betweenness, god nodes, AST call edges | `graph.go`, `incremental.go`, `relations.go`, `communities.go`, `leiden.go`, `memory.go` |
| `internal/graph/extractor/` | tree-sitter extractor (symbols, imports, AST call refs), regex fallback | `tree_sitter.go`, `regex.go`, `extractor.go` |
| `internal/mcp/` | MCP stdio server + 34 tool handlers | `mcp.go` (core + tool registration), `server_sync.go`, `graph_load.go`, `respond.go`, `tools_*.go` |
| `internal/memory/` | Memory CRUD, sessions, dedup, conflicts, compaction, git sync, context pack, stats | `memory.go`, `save.go`, `memory_session.go`, `conflicts.go`, `contextpack.go`, `sync.go`, `prompts.go` |
| `internal/hook/` | PreToolUse + lifecycle hook scripts/skills (Claude Code, OpenCode, Antigravity, Codex) | `hook.go`, `templates.go`, `scripts/` |
| `internal/protocol/` | AGENTS.md / `.cursorrules` / `.windsurfrules` protocol injection | `protocol.go` |
| `internal/perm/` | MCP tool allow-list management (Antigravity, Claude Code) | `perm.go` |
| `internal/security/` | Regex secrets sanitizer (API keys, JWTs, PEM, credentials) | `security.go` |
| `internal/tui/` | Interactive terminal UI (charmbracelet/huh + bubbletea) | `tui.go` |

## Flow 1 — Saving a memory (`sv_mem_save`)

The most common write path. Everything starts from the MCP handler and lands in
SQLite, then is debounced to a Git workspace chunk.

```text
Agent calls sv_mem_save
   │
   ▼
internal/mcp/tools_save.go  handleSave
   │  requires category / what / why / learned
   │  auto-associates session_id via GetActiveSession (reader pool)
   │  caches git metadata (branch/commit/author, 30s TTL)
   ▼
internal/memory/memory.go  SaveMemory (orchestrator)
   │  computes normalized_hash (what+why+learned+where_path)
   │  defers to save.go helpers on a single writer transaction:
   │    upsertByTopicKey → topic_key upsert (revision_count++)
   │    bumpDuplicate   → dedup check in rolling window (24h)
   │    insertMemory    → fresh insert via memoryInsertArgs shared helper
   ▼
SQLite (writer)  ──  memories + memories_fts (triggers keep FTS5 in sync)
   │
   ├─▶ scheduleSync() — debounced 500ms git sync
   │      └─ internal/memory/sync.go  SyncToGit → .sv-memory/chunks/{id}.json
   │
   └─▶ similarMemoriesHint() — time-boxed (200ms) FindSimilarMemories
            └─ surfaces candidates for sv_mem_judge
```

**Where to extend:** a new field on save touches `memoryInsertArgs` in
`memory_util.go`, the `memories` table migration in `migrations.go`, and the MCP
handler in `tools_save.go`. The save paths themselves live in `save.go`.

## Flow 2 — Querying the dependency graph (`sv_graph_query`)

Graph reads use a lazy load + LRU cache so a query never re-scans the project.

```text
Agent calls sv_graph_query(path_or_node, depth, direction, relation_type)
   │
   ▼
internal/mcp/tools_graph.go  handleGraphQuery
   │
   ├─▶ getOrLoadGraph()
   │      ├─ SyncGraphIfStale() — mtime/size check (internal/graph/stale.go)
   │      └─ GlobalGraphCache (LRU, per project) — miss → LoadFullGraph
   │
   ▼
internal/graph/memory.go  BFS query (sub-ms on cache hit)
   │
   ▼
Mermaid diagram rendered + returned (token-budget truncated via respond)
```

**Where to extend:** the BFS and path logic live in `internal/graph/memory.go`;
node metadata (communities, centrality) is added by `UpdateCommunitiesAndCentrality`
in `communities.go`.

## Flow 3 — Building / refreshing the graph (`sv_graph_sync`)

The graph is built incrementally; a full rebuild only happens on first run or
when >30% of tracked files changed.

```text
sv_graph_sync / sv-memory graph rebuild
   │
   ▼
internal/graph/incremental.go
   │
   ├─▶ trySyncGraphIncrementalFiltered
   │      scanFilesFiltered → classify by mtime/size
   │      │   unchanged → skip
   │      │   new/changed → toParse
   │      │   missing   → deleted
   │      churn > 30% of tracked → fall back to full rebuild
   │      tx: delete stale nodes/edges → parseFiles (imports/references)
   │          → parseManifests (depends_on) → extractCallEdges (calls)
   │          → extractContainsEdges (contains) → updateFileMeta
   │
   └─▶ syncGraphFull (fallback)
          DELETE all → rescan → bulkInsertNodes/Edges → same edge passes
```

**Call edge precision (AST vs heuristic):** `extractCallEdges` in
`relations.go` now prefers tree-sitter `ExtractCallRefs` per file — edges get
confidence `EXTRACTED` with a `L<line>:<col>` location. Files without AST call
coverage (Go, Lua, Markdown, shell, Vue/Svelte/Astro) keep the tokenize
heuristic (`INFERRED`). See [spect.md §14](spect.md).

## Flow 4 — Session lifecycle

Sessions group memories and enable post-compaction context recovery.

```text
sv_mem_session_start ──▶ internal/memory/memory_session.go  StartSession
   │                      (status='active', resets token ledger)
   │                      GetAutoBootBundle: previous summary + decisions
   │                      + standards + bugfixes + postmortem + Q&A + graph hubs
   │
   ├── sv_mem_save(session_id=...) ── memories linked to the session
   │
   ├── sv_mem_session_summary ── SaveSessionSummary (goal/discoveries/...)
   │
   └── sv_mem_session_end ── EndSession (status='completed')

After compaction:
   sv_mem_context ──▶ GetSessionContext: last completed session goal/summary
                      + memories + recent user prompts (sv_mem_capture_prompt)
```

## Flow 5 — Conflict detection & judgment

`sv_mem_conflicts` scans for similar memories and records relations.

```text
sv_mem_conflicts action=scan [semantic=true]
   │
   ▼
internal/memory/conflicts.go  ScanConflicts
   │  Jaccard similarity over what+why+learned (threshold configurable, 0.45)
   │  only new memories since last_conflict_scan_at (incremental)
   │  --apply persists pending relations; --max-insert caps (100)
   │
   ├─▶ lexical only → returns candidate list
   │
   └─▶ semantic=true → internal/memory/semantic.go  JudgeConflictCandidates
          │  shells out to the configured agent CLI (claude/opencode)
          │  verdicts: supersedes / conflicts_with / relates_to / none
          ▼
   sv_mem_judge ──▶ internal/memory/memory_relations.go  SaveJudgment
                    (reason capped at 200 chars for token discipline)
```

## Flow 6 — Context pack (`sv_mem_context_pack`)

The graph→memory bridge in one bounded call. Passing `include_changes="true"` additionally surfaces active spec changes whose `where_path` matches the path; capability nodes linked via `implements` edges add the "Capabilities implemented here" section (bounded: max 10 caps, 5 requirement names each).

```text
sv_mem_context_pack(path, [include_changes])
   │
   ▼
internal/memory/contextpack.go  GetContextPack
   │  resolve the path to a graph node (ResolveContextNode)
   │  node role: type, fan-in/fan-out, community, hub flag
   │  linked memories via where_path ∪ rationale_for edges
   │  each rendered as title + why truncated to bundle_why_chars
   │  (include_changes) active changes for the path (changesForPath)
   │  capabilities reachable via 'implements' edges (capabilitiesForNode)
   ▼
compact pack returned (max memories bounded by context_pack_max_memories)
```

## Flow 7 — Git sync (chunked)

Memories are shared across clones as per-memory JSON chunks, avoiding merge
conflicts between unrelated edits.

```text
sv-memory sync / debounced scheduleSync
   │
   ▼
internal/memory/sync.go  SyncToGit / SyncFromGit
   │  export: each memory → .sv-memory/chunks/{id}.json (atomic tmp+rename)
   │  import: read chunks → upsert by ID
   │  conflict markers or invalid JSON → skip chunk with warning
   │  newer/diverged local edit vs pulled chunk → last-writer-wins warning
   ▼
git commit the .sv-memory/ directory (user runs git add/commit)
```

## Flow 8 — Spec-driven decisions with delta requirements

The propose → validate → commit cycle carries OpenSpec-style delta
requirements that are merged into a durable per-capability state and wired
into the graph.

```text
sv_propose_spec(slug, title, what, where_path, requirements, capability_path)
   │  internal/mcp/tools_spec.go  handleProposeSpec
   │  CreateChange (changes) + optional SetChangeCapabilityPath (default=slug)
   │  ParseSpecDeltas → ReplaceChangeRequirements (spec_requirements)
   │  PreflightCheck (FTS5+Jaccard vs standard/decision/architecture)
   │  graph.EnsureSpecCapabilityEdges (spec:<cap> node + implements edge)
   ▼
sv_validate_decision(change_id)   ValidateChangeRequirements
   │  RFC 2119 presence warn · MODIFIED scenario-drop warn vs spec_capabilities
   ▼
sv_commit_spec(change_id)
   │  MergeChangeDeltas → MergeDeltas (spec_capabilities)
   │    RENAMED first, ADDED strict, MODIFIED replaces block, REMOVED lenient
   │  SaveMemory (decision/<slug>) → LinkDecisionToCapability (implements)
   │  WriteSpecMirror: changes/<slug>.md (with deltas) + capabilities/<cap>/spec.md
   ▼
change applied; capability state + graph + mirror all consistent
```

Parser: `internal/memory/requirements.go` (`ParseSpecDeltas` / `DeltasToMarkdown`
/ `ExtractRFC2119`); persistence/merge: `internal/memory/spec_requirements.go`;
graph wiring: `internal/graph/spec_link.go`.

## Where to add a new MCP tool

1. Register the handler in `internal/mcp/tools_*.go` (a `Server` method).
2. Register the tool in `NewServer` (`internal/mcp/mcp.go`) with `mcp.NewTool`.
3. Add a matching entry to `AllTools` (same file) — the guard test
   `TestAllToolsMatchesRegisteredTools` enforces the pairing.
4. Add the store function in `internal/memory/` (or `internal/graph/`).
5. Update the protocol (`internal/protocol/protocol.go`), the OpenCode skill
   (`internal/hook/scripts/opencode-skill.md`), and the docs (README, spect,
   getting-started, AGENT-SETUP) — EN and ES — plus the CHANGELOG.

## Where to add a new language to the graph

1. Add the extension→language mapping in `languageFromExt` (`graph.go`) and
   `getLanguageGroup` (`relations.go`).
2. Add the tree-sitter grammar to `GetLanguage` (`extractor/tree_sitter.go`)
   and a `parseX` symbol extractor; implement `ExtractCallRefs` for AST calls.
3. Add the extension to the scanner's supported set (`.sv-memoryignore` aware)
   and to the docs: `spect.md §10` (language table) EN/ES.
4. Add a test fixture with a small sample file that exercises symbol + import
   + call extraction.

## Conventions & guardrails

- **Single source of truth:** the MCP tool surface is `mcp.AllTools`; the graph
  node/edge shapes live in `internal/graph/schema/`.
- **Token discipline:** bulk-returning tools accept `token_budget` and route
  through `Server.respond` truncation; add the same for any new bulk reader.
- **Secrets:** any free-text field persisted must pass `security.SanitizeText`
  (memory text, session summaries, graph rationale text, prompts).
- **SQLite concurrency:** writes use the pool `Writer`; reads use `Reader`
  (WAL). Keep hot lookups indexed — add a migration for new indexes.
- **Lint before push:** `golangci-lint run ./...` (govet shadow analyzer is
  only enabled there, not in `go vet`). In tests, reuse the outer `err`
  (`if err = X(); err != nil`) when one exists in scope.
