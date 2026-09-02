# sv-memory

Persistent architectural memory and dependency graph for AI coding agents.

## Description

sv-memory provides two complementary capabilities:
- **sv_mem_search / sv_mem_get / sv_mem_timeline**: Retrieve past architectural decisions, bug fixes, standards, discussions, and progress journals from persistent memory.
- **sv_graph_query / sv_graph_explain / sv_graph_god_nodes / sv_graph_path / sv_graph_sync**: Query the project's code dependency graph to understand module structure, relationships, and community clusters.

Using these tools **before** reading source files directly saves tokens and provides richer architectural awareness.

## Session Lifecycle

Manage a coding session to group memories and enable context recovery:

1. **Start:** Call `sv_mem_session_start` at the beginning of work. It returns an **Auto-Boot Context Bundle** (previous session summary, key decisions, standards, recent bugfixes, postmortems, recent Q&A, journals, top graph hubs) — use it as your starting context.
2. **Capture as you go:** Save knowledge with `sv_mem_save` as you work. Pass the `session_id` from the start call to associate memories with this session.
3. **Summary:** Before finishing, call `sv_mem_session_summary` with goal, discoveries, accomplished work, and next steps.
4. **End:** Call `sv_mem_session_end` to close the session.

After a compaction or context reset, call `sv_mem_context` to recover the last session state.

## Progressive Disclosure (save tokens)

Use the 3-layer pattern instead of dumping everything:
1. **Search:** `sv_mem_search` returns a compact list (IDs + titles + topic keys). ~30 tokens per result.
2. **Timeline:** `sv_mem_timeline(observation_id=...)` shows chronological context around a memory (with the central observation rationale).
3. **Get:** `sv_mem_get(id=...)` retrieves full content. Only drill down when you need deeper detail.

The top search result is already expanded inline — only drill further when necessary.

## Topic Keys (upsert semantics)

- Use `sv_mem_suggest_topic_key(category, what)` to generate a stable `category/kebab-case` key.
- Pass `topic_key` to `sv_mem_save` to update in place instead of creating duplicates.
- **Convention:** kebab-case in English. Examples: `standard/design-system`, `architecture/component-card`, `decision/use-bun-instead-of-npm`, `bugfix/tab-transition-absolute-position`.

## When to save what

| Situation | Category | topic_key example |
| :--- | :--- | :--- |
| Visual style / design system / Tailwind tokens | `standard` | standard/design-system |
| Reusable component or UI pattern | `architecture` | architecture/component-card |
| Workflow / methodology / build process | `standard` | standard/workflow-dev-process |
| Architectural decision (and rationale) | `decision` | decision/... |
| Code convention / naming / folder structure | `standard` | standard/code-conventions |
| Complex or non-obvious bug fixed | `bugfix` | bugfix/... |
| Relevant Q&A with lasting value | `qa` | qa/... |
| Rejected library or framework feature | `decision` | decision/avoid-... |
| Session progress checkpoint | `journal` | journal/... |

**Golden rule:** when you define, change, or reuse a style, component, methodology, or convention, save it as `standard` or `architecture` with a topic_key — not just as a journal. Use `sv_mem_capture_passive` for lightweight observations that don't need an explicit save decision.

## Instructions

### Before reading source files

1. Call `sv_graph_god_nodes` to see the most-connected hub nodes (architectural hotspots).
2. Call `sv_graph_explain` on the module/file you are about to read to understand its role, centrality, and neighbors.
3. Call `sv_mem_search` with keywords related to the task to check if a past decision, discussion, or bug fix already exists.
4. If you find relevant context, call `sv_mem_get` to retrieve the full content.
5. Only read the raw source file after the graph and memory context has been exhausted.

### When fixing bugs

1. Use `sv_mem_search` with category `bugfix` to check if this bug was previously diagnosed.
2. Use `sv_graph_query` on the affected module to see what depends on it and what it imports.
3. After fixing, save a memory with category `bugfix` via `sv_mem_save`, using `sv_mem_suggest_topic_key` to get a stable key.

### When making architectural decisions

1. Use `sv_mem_search` with category `decision` or `architecture`.
2. Use `sv_graph_explain` on key modules to understand their centrality.
3. Save the decision with category `architecture` or `decision` and a topic_key.

### Before proposing or changing behavior (Spec-Driven Decision Cycle)

1. Call `sv_mem_context_pack(path=..., include_changes="true")` to surface the node role, linked decisions/standards, active changes, and the capabilities implemented at the path.
2. Call `sv_propose_spec(slug=..., title=..., what=..., where_path=..., requirements=..., tasks=..., capability_path=...)` to register the change and run the pre-flight check (BLOCK/WARN/PASS against pinned rules and invariants). The optional `requirements` carries OpenSpec-style delta requirements (ADDED/MODIFIED/REMOVED/RENAMED, RFC 2119, GIVEN/WHEN/THEN scenarios) targeting a single capability.
3. During implementation (Apply phase), call `sv_update_spec(change_id=..., tasks=...)` to mark completed task checkboxes (`- [x]`) and refine technical design or requirements in real-time.
4. After edits, call `sv_validate_decision(change_id=...)` to re-check (deterministic by default; `semantic="true"` opts into agent re-ranking) and validate the deltas (RFC 2119 presence, MODIFIED scenario drops).
5. After implementing, call `sv_commit_spec(change_id=...)` to promote the change into a durable decision/standard memory, merge the deltas into the capability state, and stamp it applied.

### After adding new files or packages

1. Call `sv_graph_sync` so the dependency graph reflects the new structure.

### Periodic maintenance

1. Call `sv_mem_review` to list stale, duplicate, or consolidation-candidate memories.
2. Call `sv_mem_conflicts action=scan` to detect potential duplicates; judge them with `sv_mem_judge` (supersedes / conflicts_with / relates_to).
3. Call `sv_mem_compact` after many topic-key upserts to keep search fast.
4. Call `sv_mem_stats` for a cheap overview of memory distribution.

## Tool Quick Reference

- **Session:** `sv_mem_session_start`, `sv_mem_session_summary`, `sv_mem_session_end`, `sv_mem_context`
- **Memory CRUD:** `sv_mem_save`, `sv_mem_update`, `sv_mem_get`, `sv_mem_delete`, `sv_mem_search`, `sv_mem_timeline`
- **Pin / Priority:** `sv_mem_pin` (action='unpin' to clear)
- **Knowledge quality:** `sv_mem_suggest_topic_key`, `sv_mem_judge`, `sv_mem_compare`, `sv_mem_compact`, `sv_mem_review`, `sv_mem_capture_passive`, `sv_mem_conflicts`, `sv_mem_stats`, `sv_mem_diagnose`
- **User intent:** `sv_mem_capture_prompt` (record what the user asked, recoverable via `sv_mem_context`)
- **Project admin:** `sv_mem_merge_projects` (merge project variants into a canonical project)
- **Context Pack:** `sv_mem_context_pack` (one bounded call: graph role + linked memories + active changes for a file/package/symbol)
- **Decision Engine (OpenSpec):** `sv_propose_spec`, `sv_update_spec`, `sv_validate_decision`, `sv_commit_spec` (propose → update tasks/design → validate → commit cycle with pre-flight checks and OpenSpec-style delta requirements)
- **Spec Mirror (CLI):** `sv-memory specs export | import <slug> | list | archive | capabilities` (human-readable Markdown projection of changes and capability state under `.sv-memory/specs/` and `openspec/`)
- **Graph:** `sv_graph_explore`, `sv_graph_diff`, `sv_graph_query`, `sv_graph_explain`, `sv_graph_god_nodes`, `sv_graph_path`, `sv_graph_sync`, `sv_graph_surprising_connections`, `sv_graph_report`
