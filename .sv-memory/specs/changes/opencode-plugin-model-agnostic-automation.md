# Model-agnostic automation in the OpenCode plugin

- **ID:** `397cab3a018f45cc`
- **Slug:** `opencode-plugin-model-agnostic-automation`
- **Status:** `applied`
- **Where:** `internal/hook/scripts/opencode-plugin-strict.ts`
- **Capability:** `opencode-plugin-automation`
- **Created:** 2026-09-06T13:40:08-03:00

## Proposal

Extiende opencode-plugin-strict.ts con automatización determinista (fail-open, disparo único) para que sv-memory funcione igual con cualquier modelo: auto-start de sesión + inyección del Auto-Boot bundle en el primer turno, captura de prompts por CLI, re-orientación compacta al cambiar de modelo (specs activas), nudge correctivo del flujo de specs tras edits que lo omitieron, y recordatorio de resumen antes de compactar. El protocolo (AGENTS.md) y el skill indican que la sesión es auto-gestionada en OpenCode para evitar doble start.

## Goal

Que opencode use sv-memory igual con cualquier modelo: sesión auto-iniciada, prompts capturados, Auto-Boot inyectado, re-orientación al cambiar de modelo y nudge de specs en edits, sin depender de la voluntad del modelo.

## Design

En opencode-plugin-strict.ts (template + instalado) se añaden 4 hooks fail-open con estado por sesión: chat.message (captura de prompts via CLI capture prompt + goal del primer mensaje), experimental.chat.messages.transform (inyección única del Auto-Boot bundle arrancando sesión via CLI session start/active; re-orientación con specs activas al cambiar de modelo; nudge correctivo de specs tras edit sin sv_spec_list), experimental.session.compacting (recordatorio de sv_memory session summary) y tool.execute.after (arma el nudge). Protocolo y skill actualizados con la nota de sesión auto-gestionada. Guard tests en hook_test.go + typecheck TS (bun + tsc strict).

## Tasks

- [x] Add chat.message hook: prompt capture + first-message goal
- [x] Add experimental.chat.messages.transform: auto session start + Auto-Boot injection, model-switch re-orientation, corrective spec nudge
- [x] Add experimental.session.compacting: pre-compaction summary reminder
- [x] Add tool.execute.after: arm corrective spec nudge on edits
- [x] Sync installed .opencode/plugin/sv-memory.ts and typecheck (bun build + tsc strict)
- [x] Update protocol/AGENTS.md + opencode skill with auto-managed session note
- [x] Add TestOpenCodePluginStrictModelAgnostic guard test; update AGENT-SETUP EN/ES + CHANGELOG
- [x] Run go build, go vet, go test -race, gofmt, golangci-lint

## ADDED Requirements

### Requirement: Deterministic session auto-start and Auto-Boot injection
The OpenCode plugin MUST auto-start a sv-memory session (via `sv-memory session start`) when none is active and MUST inject the Auto-Boot Context Bundle into the first request that carries a user turn, exactly once per opencode session.

#### Scenario: first user turn auto-starts session

#### Scenario: no double injection

### Requirement: Automatic user prompt capture
The plugin MUST persist every user prompt via `sv-memory capture prompt` attached to the active session.

#### Scenario: prompt captured

### Requirement: Model-switch re-orientation
When the model changes within an opencode session, the plugin MUST inject a compact re-orientation header with the session ID and the active spec changes list, exactly once per switch.

#### Scenario: model changed mid-session

### Requirement: Corrective spec-flow nudge
If the model edits/writes/patch without having called sv_spec_list/sv_propose_spec/sv_spec_get, the plugin MUST inject a corrective reminder to run the spec flow, at most once per session.

#### Scenario: edit without spec consultation