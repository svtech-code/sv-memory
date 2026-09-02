---
name: sv-memory
description: >-
  Persistent architectural memory, decision tracking, and dependency graph for AI coding agents.
  Use before reading or modifying files, when making architectural decisions, or when fixing bugs.
---

# sv-memory

Persistent architectural memory, decision tracking, and structural dependency graph for AI coding agents.

## Description

`sv-memory` maintains persistent architectural knowledge, conventions, and a code dependency graph across sessions.

### Single-Call Context Initialization (Best Practice):
Before inspecting or modifying source files in a package or path:
- Call `sv_mem_context_pack(path="...", include_changes="true")` (or `sv_graph_explore`): Retrieves the code node's role, community, dependency fan-in/fan-out, linked decisions/standards, active spec proposals, and implemented capabilities in **one compact call**.

## Session Lifecycle:
1. **Start:** Call `sv_mem_session_start` at the beginning of work to receive the **Auto-Boot Context Bundle** (previous session summary, key decisions, standards, recent bugfixes, top graph hubs).
2. **Capture Knowledge:** Call `sv_mem_save(category=..., what=..., why=..., learned=...)`. The `topic_key` is auto-derived for evolving categories (`decision`, `standard`, `architecture`, `bugfix`), enabling upsert semantics. Use `sv_mem_capture_passive` for lightweight observations.
3. **End Session:** Call `sv_mem_session_end(accomplished="...")` before finishing to save accomplishments and close the session cleanly.

After a compaction or context reset, call `sv_mem_context` to recover the last session state.

## Progressive Disclosure (Save Tokens):
Use the 3-layer pattern instead of dumping full memory content:
1. **Search:** `sv_mem_search` returns a compact list (IDs + titles + topic keys).
2. **Timeline:** `sv_mem_timeline(observation_id=...)` shows chronological context around a memory.
3. **Get:** `sv_mem_get(id=...)` retrieves full content on demand.

## Spec-Driven Decision Cycle (OpenSpec Workflow):
For structural, architectural, or multi-step behavior changes:
1. **Explore Context:** `sv_mem_context_pack(path=..., include_changes="true")` to surface role, linked decisions, active changes, and capability state.
2. **Propose:** `sv_propose_spec(slug="...", title="...", what="...", where_path="...", requirements="...", tasks="...")` registers the change and runs the pre-flight check (`BLOCK`/`WARN`/`PASS` against invariants). The `requirements` parameter supports OpenSpec delta requirements (`## ADDED/MODIFIED/REMOVED Requirements`, `### Requirement:`, `#### Scenario:` with `GIVEN/WHEN/THEN/AND`).
3. **Apply & Track Tasks:** As you implement each task, call `sv_update_spec(change_id="...", tasks="...")` to update progress checkboxes (`- [x]`) and refine design/requirements in real-time.
4. **Validate:** `sv_validate_decision(change_id="...")` re-checks invariants and validates delta requirements (RFC 2119 keyword presence and scenario consistency).
5. **Commit:** `sv_commit_spec(change_id="...")` promotes the change into a durable decision memory, merges delta requirements into capability state (`.sv-memory/specs/capabilities/` and `openspec/`), and marks it applied.

## Graph — Structural Exploration:
- `sv_graph_explore`: Multi-symbol structural role, call paths, and blast radius.
- `sv_graph_diff`: Compare structural code differences, added/removed symbols, and blast radius impact against a Git reference (e.g. `HEAD~1`, `main`).
- `sv_graph_god_nodes`: Inspect high-centrality architectural hotspots.
- `sv_graph_query` / `sv_graph_explain`: Subgraph and module network metrics.
- `sv_graph_sync`: Incremental re-scan after adding files or restructuring packages.

## Core Tool Quick Reference:
- **Session:** `sv_mem_session_start`, `sv_mem_session_end`, `sv_mem_context`, `sv_mem_session_summary`
- **Memory CRUD:** `sv_mem_save`, `sv_mem_update`, `sv_mem_get`, `sv_mem_delete`, `sv_mem_search`, `sv_mem_timeline`
- **Quality & Health:** `sv_mem_compact`, `sv_mem_stats`, `sv_mem_diagnose`, `sv_mem_conflicts`, `sv_mem_judge`
- **Context Pack:** `sv_mem_context_pack(path=...)`, `sv_graph_explore`
- **Spec Engine (OpenSpec):** `sv_propose_spec`, `sv_update_spec`, `sv_validate_decision`, `sv_commit_spec`
- **Graph:** `sv_graph_explore`, `sv_graph_diff`, `sv_graph_query`, `sv_graph_explain`, `sv_graph_god_nodes`, `sv_graph_path`, `sv_graph_sync`, `sv_graph_report`
