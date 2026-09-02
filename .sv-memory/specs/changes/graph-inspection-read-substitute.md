# Position graph as grep/read substitute in injected protocol

- **ID:** `3cbc26cbda6b4e83`
- **Slug:** `graph-inspection-read-substitute`
- **Status:** `applied`
- **Where:** `internal/protocol/protocol.go`
- **Capability:** `graph-inspection-read-substitute`
- **Created:** 2026-09-01T23:35:27-04:00

## Proposal

Rewrite the "Graph Inspection" block of the injected protocol template (internal/protocol/protocol.go) to lead agents to use the graph as a substitute for grep/read on synced code, following the codegraph anti-pattern style: explore first, trust graph results instead of re-verifying with grep, don't reconstruct flows by hand, use raw Read/Grep only for unindexed files (configs, docs) or confirmed staleness. Sync the rewritten block to AGENTS.md, documentation/spect.md (both copies) and documentation/spect_ES.md (both copies), and the sv-memory SKILL.md graph bullet.

## Goal

Agents stop redundantly grepping/reading symbled code the graph already indexes; they route structural questions through sv_graph_explore/sv_graph_query first, cutting token usage and round-trips modeled on codegraph's server-instructions anti-patterns.

## Design

Rewrite the section in protocolTemplate (and its mirrors) to: (1) state sv_graph_explore + sv_mem_context_pack return line-numbered source that counts as already read; (2) list the existing inspection tools (god_nodes, explain, query, path) with explore/context-pack first; (3) add an explicit anti-patterns bullet list (don't re-verify with grep, don't grep/read first, don't hand-reconstruct flows, check staleness); (4) keep the refresh rule. Update SKILL.md Graph bullet to mention explore-first. No Go API changes; only the protocol template document and mirrors.

## ADDED Requirements

### Requirement: Agent routes code understanding through the graph before grep/read

#### Scenario: An agent needs to understand how symbol X relates to symbol Y before editing

### Requirement: Protocol lists concrete graph exploration anti-patterns

#### Scenario: An agent considers re-verifying a graph result with grep

### Requirement: All protocol mirrors stay in sync

#### Scenario: A user reads the protocol from the injected template, a mirror doc, the project AGENTS.md, or the skill