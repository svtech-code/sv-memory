# claude-code-deterministic-hooks Specification

## Requirements

### Requirement: Claude Code prompt capture
The Claude Code UserPromptSubmit hook MUST persist every user prompt via `sv-memory capture prompt`, fail-open.

#### Scenario: prompt persisted

### Requirement: Deterministic Claude Code SessionEnd
The Claude Code SessionEnd hook MUST close the active sv-memory session via the CLI (idempotent), so no active session leaks.

#### Scenario: session closed on end

### Requirement: Deterministic Claude Code SessionStart
The Claude Code SessionStart hook MUST auto-start the sv-memory session when none is active and MUST emit the Auto-Boot Context Bundle as additionalContext; when a session is already active it MUST emit a short note and MUST NOT start a second session.

#### Scenario: session start injects bundle

#### Scenario: no double start