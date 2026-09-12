# Strict Protocol Enforcement and Trigger-based Tool Descriptions

- **ID:** `f3c329fac575479b`
- **Slug:** `strict-tool-enforcement`
- **Status:** `applied`
- **Where:** `internal/protocol/protocol.go, internal/mcp/mcp.go, AGENTS.md, documentation/spect.md, documentation/spect_ES.md`
- **Capability:** `strict-tool-enforcement`
- **Created:** 2026-09-12T16:30:55-03:00

## Proposal

Overhaul the protocol instructions in `protocol.go` (and mirrors) by introducing a `<CRITICAL_INSTRUCTIONS>` XML block with absolute negative constraints (e.g. 'NEVER do X, ALWAYS do Y'). Also modify MCP tool descriptions in `mcp.go` to include explicit `TRIGGER:` keywords so agents invoke them automatically without user prompting.

## Tasks

- [x] Modify `protocolTemplate` in `internal/protocol/protocol.go` to include `<CRITICAL_INSTRUCTIONS>` block with absolute negative constraints.
- [x] Add `TRIGGER: ...` prefixes to tool descriptions in `internal/mcp/mcp.go` (e.g., `sv_graph_explore`, `sv_propose_spec`, `sv_mem_search`, `sv_mem_save`).
- [x] Update `AGENTS.md` to reflect the new strict protocol.
- [x] Update `documentation/spect.md` and `documentation/spect_ES.md` to sync the strict wording.
- [x] Run `go build`, `go vet`, `go test -race`, and `golangci-lint run`.

## MODIFIED Requirements

### Requirement: Strict Protocol Instructions

#### Scenario: Agent tool selection for codebase exploration

### Requirement: Strict Spec Cycle Usage

#### Scenario: Agent proposes architectural or behavioral changes

### Requirement: Trigger-Based Tool Descriptions

#### Scenario: Agent reads MCP tool descriptions