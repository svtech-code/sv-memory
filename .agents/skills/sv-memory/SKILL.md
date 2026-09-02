---
name: sv-memory
description: >-
  Persistent architectural memory, decision tracking, and dependency graph for AI coding agents.
  Use before reading or modifying files, when making architectural decisions, or when fixing bugs.
---

# sv-memory

Persistent architectural memory and dependency graph for AI coding agents.

## Description

`sv-memory` maintains persistent architectural knowledge, conventions, and a code dependency graph across sessions.

### Single-Call Context Initialization (Best Practice):
Before inspecting or modifying source files in a package or path:
- Call `sv_mem_context_pack(path="...")`: Retrieves the code node's role, community, dependency fan-in/fan-out, and linked decisions/standards in **one compact call**.

## Session Lifecycle:
1. **Start:** Call `sv_mem_session_start` at the beginning of a complex task to receive the Auto-Boot bundle.
2. **Capture Knowledge:** Call `sv_mem_save(category=..., what=..., why=..., learned=...)`. The `topic_key` is auto-derived for evolving categories (`decision`, `standard`, `architecture`, `bugfix`), enabling upsert semantics.
3. **End Session:** Call `sv_mem_session_end(accomplished="...")` before finishing to save accomplishments and close the session cleanly.

## Spec-Driven Decision Cycle:
For structural or architectural changes:
1. `sv_propose_spec(slug="...", title="...", what="...", where_path="...")`
2. `sv_validate_decision(change_id="...")`
3. `sv_commit_spec(change_id="...")`

## Core Tool Quick Reference:
- **Context Pack:** `sv_mem_context_pack(path=...)` (combines graph role + memories + active specs)
- **Memory Save:** `sv_mem_save(category=..., what=..., why=..., learned=...)`
- **Memory Search & Detail:** `sv_mem_search(query=...)`, `sv_mem_get(id=...)`
- **Session:** `sv_mem_session_start(goal=...)`, `sv_mem_session_end(accomplished=...)`, `sv_mem_context()`
- **Graph:** `sv_graph_explore` (multi-symbol explore + call path, source counts as read), `sv_graph_query(path_or_node=...)`, `sv_graph_explain(node=...)`, `sv_graph_search(query=...)` (discover nodes by text pattern, optional node_type/limit), `sv_graph_communities(top_n=..., community_id=...)` (list communities or detail members), `sv_graph_report` (aggregate GRAPH_REPORT.md overview), `sv_graph_sync()`
- **Spec Engine:** `sv_propose_spec`, `sv_validate_decision`, `sv_commit_spec`
