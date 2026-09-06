# CLI session lifecycle + prompt capture for deterministic agent hooks

- **ID:** `3daa3af40b2e4104`
- **Slug:** `cli-session-lifecycle`
- **Status:** `applied`
- **Where:** `cmd/sv-memory/cmd_session.go`
- **Capability:** `cli-session-lifecycle`
- **Created:** 2026-09-06T13:30:00-03:00

## Proposal

Añade los comandos CLI `sv-memory session start|summary|end|active` y `sv-memory capture prompt "<text>"` como contraparte CLI de las tools MCP de sesión y captura de prompts. Reutiliza la misma lógica (StartSession, GetAutoBootBundle, SaveSessionSummary, EndSession, GetActiveSession, SavePrompt, graph.TopDegreeNodes) para que hooks/plugins de cualquier agente puedan arrancar/cerrar sesión y capturar prompts sin un round-trip MCP ni depender de la voluntad del modelo. Enabler para la F3 (automatización del plugin opencode).

## Goal

Exponer el ciclo de vida de sesión y la captura de prompts por CLI para que hooks/plugins de cualquier agente lo ejecuten de forma determinista, sin depender de que el modelo llame a una tool MCP.

## Design

Nuevo cmd/sv-memory/cmd_session.go con el grupo `session` (start/summary/end/active) y el subcomando `capture prompt`, reutilizando memory.StartSession/GetAutoBootBundle/SaveSessionSummary/EndSession/GetActiveSession/SavePrompt y graph.TopDegreeNodes (misma lógica que los handlers MCP sv_mem_session_* y sv_mem_capture_prompt). Registrados en main.go. Tests end-to-end sobre proyecto temporal (cmd_session_test.go).

## Tasks

- [x] Add cmd/sv-memory/cmd_session.go: session group (start/summary/end/active) reusing memory+graph logic
- [x] Add capture prompt subcommand under captureCmd
- [x] Register commands in main.go
- [x] Add cmd_session_test.go (subcommand registration + full lifecycle over temp project)
- [x] Update README EN/ES CLI reference + CHANGELOG
- [x] Run go build, go vet, go test -race, gofmt, golangci-lint

## ADDED Requirements

### Requirement: Session lifecycle CLI commands
The CLI MUST provide `sv-memory session start`, `sv-memory session summary <id>`, `sv-memory session end`, and `sv-memory session active` mirroring the corresponding MCP tools.

#### Scenario: start prints Auto-Boot bundle

#### Scenario: end auto-detects active session

### Requirement: Prompt capture CLI command
The CLI MUST provide `sv-memory capture prompt "<text>"` that persists the prompt as a local observation attached to the active session, mirroring `sv_mem_capture_prompt`.

#### Scenario: prompt attached to active session