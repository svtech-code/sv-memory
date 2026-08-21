# Surgical source code snippet extraction in context pack

- **ID:** `c2f35fce954241f2`
- **Slug:** `contextpack-surgical-snippets`
- **Status:** `applied`
- **Where:** `internal/memory/contextpack.go; internal/memory/contextpack_test.go; internal/mcp/tools_context.go; documentation/spect.md; documentation/spect_ES.md; CHANGELOG.md`
- **Capability:** `context-engine`
- **Created:** 2026-08-21T18:28:09-04:00

## Proposal

Add surgical source code snippet extraction to ContextPack (internal/memory/contextpack.go) for function and class symbols with a token-safe line limit, eliminating the need for separate file reads by AI agents.

## ADDED Requirements

### Requirement: Surgical Source Snippet in Context Pack
The context pack engine SHALL extract and include bounded source code snippets for resolved function and class symbols from the workspace when available, rendering them in the markdown context pack within a configurable line limit.

#### Scenario: Extract source snippet for a function node