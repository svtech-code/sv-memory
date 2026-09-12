# Close point A: verify remaining audit items and mark memories resolved

- **ID:** `b464af5198af470a`
- **Slug:** `close-point-a-backlog`
- **Status:** `applied`
- **Where:** `internal/graph/export.go, internal/graph/wiki.go, internal/graph/export.go`
- **Capability:** `close-point-a-backlog`
- **Created:** 2026-09-12T19:16:08-03:00

## Proposal

The audit journal and session-end idea from 2026-08-16/18 remain open in memory but the underlying issues are mostly resolved in code. Need to verify the 2 remaining items (escapeCypherStr usage, wiki/export XSS) and mark the stale memory records as resolved.

## Goal

Clean up the memory backlog: verify escapeCypherStr is not dead code, verify wiki/export label escaping, mark session-end idea and audit journal as resolved.

## Design

Use sv_graph_explore to verify escapeCypherStr usage and wiki/export escaping. If issues found, fix them. Then use sv_mem_update to mark the stale memories as resolved.

## Tasks

- [x] Verify escapeCypherStr is used (not dead code) or remove if unused
- [x] Verify wiki/export label escaping handles special characters
- [x] Mark idea/session-end-idempotent as resolved
- [x] Mark journal/pendientes-auditoria-sv-memory as resolved
- [x] Run gate CI