# Live Task Progress Tracking in ContextPack and Spec Engine

- **ID:** `3eb6c66ab3014cb3`
- **Slug:** `openspec-live-task-progress`
- **Status:** `applied`
- **Where:** `internal/memory/tasks.go`
- **Capability:** `specs/task-progress`
- **Created:** 2026-08-21T19:49:08-04:00

## Proposal

Implement ParseTaskProgress to parse markdown checklist tasks and surface real-time task completion metrics across ContextPack and MCP tools.

## Goal

Provide real-time visibility into task execution progress for AI agents without manual overhead.

## Design

Pure Go parser counting checkboxes and calculating percentage completion; integrated into ContextChange and rendered into RenderContextPack and tools_spec.

## Tasks

- [x] Implement ParseTaskProgress in internal/memory/tasks.go
- [x] Integrate into ContextChange and RenderContextPack
- [x] Surface tasks progress in sv_propose_spec and sv_validate_decision
- [x] Update specs list CLI with TASKS column
- [x] Add unit tests and verify CI passes