# Auto-freshness staleness probe during context pack resolution

- **ID:** `ac203f6d06214798`
- **Slug:** `contextpack-auto-freshness`
- **Status:** `applied`
- **Where:** `internal/memory/contextpack.go; internal/memory/contextpack_test.go; documentation/spect.md; documentation/spect_ES.md; CHANGELOG.md`
- **Capability:** `context-engine`
- **Created:** 2026-08-21T19:14:06-04:00

## Proposal

Connect SyncGraphIfStale probe into GetContextPack (internal/memory/contextpack.go) to automatically refresh modified AST symbols and call edges on-demand without manual sync or perceptible overhead.

## ADDED Requirements

### Requirement: Auto-Freshness Staleness Probe in Context Pack
The context pack engine SHALL run a lightweight mtime-based staleness probe (SyncGraphIfStale) prior to resolving graph nodes, ensuring recently created or edited code symbols are discovered without requiring manual graph synchronization.

#### Scenario: Resolve newly added symbol after file modification