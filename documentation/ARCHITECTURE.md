# Architecture

How sv-memory works inside: the key data structures, lifecycle states, and design decisions that make persistent memory and code graphs work for AI agents.

## Session Lifecycle

Sessions track coding work across agent interactions. Every session starts `active` and must be `completed` before context recovery works.

```text
[not created]
    │
    ▼  sv_mem_session_start / sv-memory session start
 [active] ──────────────────────────────────────────────▶ [completed]
    │                                                       ▲
    │  sv_mem_session_end / sv-memory session end           │
    └───────────────────────────────────────────────────────┘
    │
    └── auto-close (stale > 24h) ──▶ [completed]
```

| State | Meaning | How it transitions |
| :--- | :--- | :--- |
| `active` | Session in progress, memories are being saved | Created by `StartSession` |
| `completed` | Session ended, summary saved | `EndSession`, `CloseStaleSessions` |

### Agent integration

| Agent | Start | End | Prompt capture |
| :--- | :--- | :--- | :--- |
| Claude Code | Hook `SessionStart` (auto-start + bundle inject) | Hook `SessionEnd` (auto-close, idempotent) | Hook `UserPromptSubmit` |
| OpenCode | Plugin `chat.messages.transform` | ❌ No exit hook | Plugin `chat.message` |
| Antigravity CLI | Protocol (`AGENTS.md`) | Protocol (`AGENTS.md`) | ❌ |
| Cursor / Windsurf | Protocol (`AGENTS.md`) | Protocol (`AGENTS.md`) | ❌ |
| Codex | No-op hook | No-op hook | ❌ |

### Auto-close stale sessions

When `sv_mem_session_start` is called and existing active sessions are older than `stale_session_hours` (default 24h), they are automatically closed. This prevents leaked sessions from accumulating when users exit agents without calling `sv_mem_session_end`.

```text
sv_mem_session_start
    │
    ▼
CloseStaleSessions(projectID, 24h)
    │  UPDATE sessions SET status='completed'
    │    WHERE status='active' AND started_at < cutoff
    │
    ▼
StartSession() → new active session
    │
    ▼
GetAutoBootBundle() → inject context
```

**Key file:** `internal/memory/memory_session.go` — `CloseStaleSessions`

## Progressive Disclosure

The 3-layer pattern minimizes tokens by returning compact results first, letting the agent drill down on demand:

| Layer | Tool | Returns | ~Tokens |
| :--- | :--- | :--- | :--- |
| 1 — Search | `sv_mem_search` | IDs, categories, dates, titles, topic_keys | 30/result |
| 2 — Timeline | `sv_mem_timeline` | Chronological context around one memory | 100/result |
| 3 — Get | `sv_mem_get` | Full content (what, why, learned, etc.) | 200/result |

**Token savings:** ~148x vs reading raw files (12,852 → 87 tokens for graph hubs).

**Key file:** `internal/memory/memory_search.go` — `SearchMemoriesCompact`

## Graph Unification

sv-memory unifies three node types in a single dependency graph:

```text
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│  Code Nodes │────▶│ Memory Nodes│────▶│  Spec Nodes │
│  (functions,│     │ (decisions, │     │(capabilities,│
│   classes,  │     │  standards, │     │   changes)  │
│   files)    │     │  bugfixes)  │     │             │
└─────────────┘     └─────────────┘     └─────────────┘
```

### Node types

| Type | Source | Example |
| :--- | :--- | :--- |
| `function` | tree-sitter AST | `InitDB`, `SaveMemory` |
| `class` | tree-sitter AST | `TreeSitterExtractor` |
| `file` | scanner | `internal/db/db.go` |
| `memory` | `sv_mem_save` | Decision: "Use Postgres" |
| `spec` | `sv_propose_spec` | Capability: `auth-session` |
| `rationale` | AST comments | `NOTE:`, `WHY:`, `HACK:` |

### Edge types

| Relation | Source | Meaning |
| :--- | :--- | :--- |
| `imports` | AST | File A imports file B |
| `calls` | AST (EXTRACTED) / heuristic (INFERRED) | Function A calls function B |
| `depends_on` | transitive | A depends on B (transitive closure) |
| `references` | AST | Symbol A references symbol B |
| `rationale_for` | AST comments | Comment explains code node |
| `implements` | spec linking | Code implements capability |
| `supersedes` | memory judgment | New decision replaces old |
| `conflicts_with` | memory judgment | Two decisions contradict |

### Community detection

The Leiden algorithm detects communities of code that changes together. Each node gets a `community_id` in its metadata. Communities enable:

- **graph_boost** in `sv_mem_search`: searching a file expands to its whole community
- **Context Pack**: surfaces community membership for orientation
- **God Nodes**: most-connected nodes across communities (architectural hotspots)

**Key files:** `internal/graph/communities.go`, `internal/graph/leiden.go`

## Conflict Surfacing

Memory conflicts arise when two decisions contradict each other.

```text
[no relation]
    │  sv_mem_judge
    ▼
 [pending] ──▶ semantic scan (LLM opt-in) ──▶ [judged]
                                                  │
                                    ┌─────────────┴─────────────┐
                                    ▼                           ▼
                              [applied]                    [ignored]
```

### Relation types

| Type | Meaning |
| :--- | :--- |
| `supersedes` | Newer decision replaces older |
| `conflicts_with` | Two decisions contradict |
| `relates_to` | General association |

**Key files:** `internal/memory/conflicts.go`, `internal/memory/semantic.go`

## Token Economy

sv-memory minimizes token usage at every layer:

| Mechanism | Location | Effect |
| :--- | :--- | :--- |
| `token_budget` | MCP tools | Truncates response to N tokens |
| `bundleWhyChars` | Auto-Boot bundle | Caps rationale per memory (default 300) |
| `truncateReason` | Judge/Compare | Caps relation reason to 200 chars |
| `graph_boost` | `sv_mem_search` | Expands search to community (avoids re-queries) |
| `semanticRecallMaxCandidates` | Semantic recall | Caps candidates sent to LLM (30) |
| `semanticRecallFieldChars` | Semantic recall | Caps each field per candidate (300) |
| `context_pack_max_memories` | Context Pack | Caps linked memories per pack (default 5) |

## Spec-Driven Decision Cycle

Before modifying behavior or architecture, agents must follow the 5-tool loop:

```text
1. sv_spec_list      → see what proposals exist
2. sv_spec_get       → inspect a specific proposal
3. sv_propose_spec   → register new proposal (pre-flight check)
4. sv_update_spec    → mark tasks complete during implementation
5. sv_validate_decision → re-check after edits
6. sv_commit_spec    → promote to durable decision memory
```

### Pre-flight check

`sv_propose_spec` runs a pre-flight check against existing standards/decisions:

| Verdict | Meaning | Action |
| :--- | :--- | :--- |
| `BLOCK` | Pinned invariant overlaps | Cannot proceed |
| `WARN` | Ordinary overlap | Proceed with caution |
| `PASS` | No conflicts | Safe to proceed |

### Capability state merging

When a change is committed, its delta requirements merge into the capability state (`.sv-memory/specs/capabilities/<cap>/spec.md`). The materialized current state is projected to the spec mirror for human readability.

**Key files:** `internal/memory/specs.go`, `internal/memory/changes.go`

## Sync and Freshness

### Git sync

Memories are synced to Git via chunked JSON files (`.sv-memory/chunks/<id>.json`). The sync is debounced (default 500ms) and coalesces multiple changes into a single commit.

### Background file watcher

An `fsnotify`-based watcher monitors the project directory for file changes. When active, graph queries skip the O(n) `DetectStaleFiles` walk — freshness comes from background sync, query path is O(1).

### Auto-freshness in context packs

`sv_mem_context_pack` and `sv_graph_explore` call `SyncGraphIfHasChanges` before resolving nodes, ensuring snippets reflect the latest disk state without manual `sv_graph_sync`.

**Key files:** `internal/memory/sync.go`, `internal/graph/watcher.go`, `internal/graph/incremental.go`
