# cli-session-lifecycle Specification

## Requirements

### Requirement: Prompt capture CLI command
The CLI MUST provide `sv-memory capture prompt "<text>"` that persists the prompt as a local observation attached to the active session, mirroring `sv_mem_capture_prompt`.

#### Scenario: prompt attached to active session

### Requirement: Session lifecycle CLI commands
The CLI MUST provide `sv-memory session start`, `sv-memory session summary <id>`, `sv-memory session end`, and `sv-memory session active` mirroring the corresponding MCP tools.

#### Scenario: start prints Auto-Boot bundle

#### Scenario: end auto-detects active session