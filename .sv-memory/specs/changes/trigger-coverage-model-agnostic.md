# Trigger coverage for model-agnostic tool invocation

- **ID:** `6a149dacdf0244dd`
- **Slug:** `trigger-coverage-model-agnostic`
- **Status:** `applied`
- **Where:** `internal/mcp/mcp.go`
- **Capability:** `trigger-coverage-model-agnostic`
- **Created:** 2026-09-12T19:08:30-03:00

## Proposal

Add concise "USE WHEN:" trigger clauses to key MCP tools that currently lack explicit usage guidance. Only 5 of 43 tools carry TRIGGER: prefixes. Spec flow (spec_list, spec_get, update_spec, validate_decision, commit_spec), session (session_start/end/summary/context), graph_search, capture_passive, and review lack "when to use" cues that help weaker models invoke tools automatically.

## Goal

Make sv-memory tools discoverable and self-documenting for any LLM, not just strong models that can infer tool usage from protocol alone. Keep token cost bounded.

## Design

Each tool gets a concise "USE WHEN:" prefix (similar to existing TRIGGER: pattern) appended to its mcp.WithDescription string. The AllTools catalog entries are updated to match. The TestToolDescriptionBudget threshold is reviewed — if the new descriptions push total under 10k, keep threshold; if over, adjust with justification and update the comment.

## Tasks

- [x] Add TRIGGER/USE WHEN clauses to spec flow tools: sv_spec_list, sv_spec_get, sv_update_spec, sv_validate_decision, sv_commit_spec
- [x] Add TRIGGER/USE WHEN clauses to session tools: sv_mem_session_start, sv_mem_session_end, sv_mem_session_summary, sv_mem_context
- [x] Add TRIGGER/USE WHEN to sv_graph_search, sv_mem_capture_passive, sv_mem_review
- [x] Update AllTools descriptions to match registered descriptions
- [x] Verify total description budget stays under 10k chars (trimmed context_pack, graph_explore, propose_spec, validate_decision to compensate)
- [x] Run gate CI