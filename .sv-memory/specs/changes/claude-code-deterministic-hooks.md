# Deterministic Claude Code hooks (auto-boot, auto-close, prompt capture)

- **ID:** `edd52acf5e534952`
- **Slug:** `claude-code-deterministic-hooks`
- **Status:** `applied`
- **Where:** `internal/hook/scripts/claude-code-session-start.sh`
- **Capability:** `claude-code-deterministic-hooks`
- **Created:** 2026-09-06T13:45:54-03:00

## Proposal

Extiende la paridad multi-agente: SessionStart de Claude Code arranca la sesión e inyecta el Auto-Boot Context Bundle como additionalContext (fail-open, sin doble-start); SessionEnd cierra la sesión de forma idempotente; y un nuevo hook UserPromptSubmit captura cada prompt vía el CLI de F2. Misma lógica determinista que el plugin de opencode (F3), reutilizando los comandos CLI de sesión (F2).

## Goal

Paridad de automatización determinista en Claude Code usando los comandos CLI de sesión (F2): auto-boot en SessionStart, cierre en SessionEnd y captura de prompts en UserPromptSubmit, sin depender de la voluntad del modelo.

## Design

En los hooks de Claude Code (generados por internal/hook): SessionStart ahora ejecuta `sv-memory session start` (si no hay sesión activa) e inyecta el Auto-Boot Context Bundle como additionalContext vía JSON (fail-open); SessionEnd ejecuta `sv-memory session end` (idempotente) para no dejar sesiones colgadas; nuevo hook UserPromptSubmit ejecuta `sv-memory capture prompt "$PROMPT"` para captura de prompts. Se añade el evento UserPromptSubmit a claudeLifecycleEvents y el script claude-code-user-prompt-submit.sh al go:embed. Cursor/Windsurf/Codex permanecen protocol-driven (sin superficie de hooks deterministas); Antigravity conserva su PreToolUse. Docs AGENT-SETUP EN/ES + CHANGELOG actualizados.

## Tasks

- [x] Add claude-code-user-prompt-submit.sh + go:embed + UserPromptSubmit event in claudeLifecycleEvents
- [x] Rewrite claude-code-session-start.sh: auto session start + Auto-Boot bundle as additionalContext (idempotent)
- [x] Rewrite claude-code-session-end.sh: auto-close via session end (idempotent)
- [x] Update hook_test.go lifecycle dirs + events lists
- [x] Smoke: session-start inyecta bundle / nota sin doble-start; session-end cierra
- [x] Update AGENT-SETUP EN/ES (six hooks, deterministic) + CHANGELOG
- [x] Run go build, go vet, go test -race, gofmt, golangci-lint, bash -n

## ADDED Requirements

### Requirement: Deterministic Claude Code SessionStart
The Claude Code SessionStart hook MUST auto-start the sv-memory session when none is active and MUST emit the Auto-Boot Context Bundle as additionalContext; when a session is already active it MUST emit a short note and MUST NOT start a second session.

#### Scenario: session start injects bundle

#### Scenario: no double start

### Requirement: Deterministic Claude Code SessionEnd
The Claude Code SessionEnd hook MUST close the active sv-memory session via the CLI (idempotent), so no active session leaks.

#### Scenario: session closed on end

### Requirement: Claude Code prompt capture
The Claude Code UserPromptSubmit hook MUST persist every user prompt via `sv-memory capture prompt`, fail-open.

#### Scenario: prompt persisted