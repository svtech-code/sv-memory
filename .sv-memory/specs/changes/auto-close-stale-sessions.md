# Auto-cierre de sesiones stale al iniciar nueva sesión

- **ID:** `5fc9de33b4f74398`
- **Slug:** `auto-close-stale-sessions`
- **Status:** `applied`
- **Where:** `internal/memory/memory_session.go, internal/mcp/tools_session.go, cmd/sv-memory/cmd_session.go, internal/config/config.go`
- **Capability:** `auto-close-stale-sessions`
- **Created:** 2026-09-06T15:07:03-03:00

## Proposal

Cuando sv_mem_session_start se llama y ya existe una sesión activa > stale_session_hours (default 24h), auto-cerrarla antes de crear la nueva. Previene sesiones fugadas en agentes sin SessionEnd hook (OpenCode, Antigravity, Cursor, Windsurf, Codex).

## Goal

Corregir 54 sesiones activas stale confirmadas en la DB, prevenir futuras fugas

## Design

1. CloseStaleSessions(db, projectID, maxAge) - UPDATE batch que cierra sesiones activas > maxAge. 2. Llamar en handleSessionStart antes de StartSession. 3. Llamar en cmd session start. 4. Config stale_session_hours=24.

## Tasks

- [x] Añadir CloseStaleSessions en memory_session.go\n- [x] Añadir default stale_session_hours=24 en config.go\n- [x] Llamar CloseStaleSessions en handleSessionStart (MCP)\n- [x] Llamar CloseStaleSessions en cmd session start (CLI)\n- [x] Añadir test CloseStaleSessions\n- [x] Añadir test handleSessionStart auto-cierre\n- [x] go build ./...\n- [x] go vet ./...\n- [x] go test -race ./internal/memory/... ./internal/mcp/...\n- [x] golangci-lint run ./...