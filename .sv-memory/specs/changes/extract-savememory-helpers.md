# Extract SaveMemory orchestration into dedicated helpers

- **ID:** `ec2fd26e3f2147d8`
- **Slug:** `extract-savememory-helpers`
- **Status:** `applied`
- **Where:** `internal/memory/memory.go`
- **Capability:** `memory/save-helpers`
- **Created:** 2026-08-15T22:53:00-04:00

## Proposal

Refactor the 140-line SaveMemory monolith (memory.go:310-451) into three tx-scoped helpers in a new save.go file: upsertByTopicKey, bumpDuplicate, insertMemory. SaveMemory becomes a clean orchestrator (~70 lines). Zero behavior change, zero signature change. Improves testability of each branch and reduces cognitive load.

## Design

Create internal/memory/save.go with three functions operating on *sql.Tx:
1. upsertByTopicKey(tx, mem, now) (bool, error) — handles topic-key upsert path (memory.go:358-400)
2. bumpDuplicate(tx, mem, now) (bool, error) — handles dedup path (memory.go:402-428)
3. insertMemory(tx, mem, now) error — handles fresh insert path (memory.go:430-450)

SaveMemory validates, sanitizes, defers graph relink, begins tx, delegates to helpers, commits. All existing behavior preserved exactly.