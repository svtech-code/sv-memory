---
trigger: always_on
---

# sv-memory (always-on protocol)

This project uses **sv-memory**: persistent architectural memory plus a code
dependency graph exposed as MCP tools (`sv_*`). The full workflow lives in the
`sv-memory` skill (`.agents/skills/sv-memory/SKILL.md`). Follow these rules.

## Before reading source

1. Call `sv_graph_explore` (or `sv_mem_context_pack`) for the file/symbol — one
   call returns source, structure, blast radius and linked memories.
2. Call `sv_mem_search` for past decisions, standards or bugfixes on the topic.
3. Only then read raw files.

## Before changing behavior, contracts, APIs or architecture

Run the Spec-Driven Decision Cycle — do NOT edit first:

1. `sv_spec_list` — pending changes
2. `sv_propose_spec` — register the change
3. `sv_update_spec` — implement and mark tasks
4. `sv_validate_decision`
5. `sv_commit_spec`

Config/docs-only changes are exempt.

## While working

- Save decisions, standards and non-obvious bugfixes with `sv_mem_save`
  (`sv_mem_suggest_topic_key` gives a stable topic key).
- After adding files or packages: `sv_graph_sync`.

## Session

The session is auto-managed by the sv-memory `PreInvocation` hook — do NOT call
`sv_mem_session_start`. Close it with `sv_mem_session_end` when done.
