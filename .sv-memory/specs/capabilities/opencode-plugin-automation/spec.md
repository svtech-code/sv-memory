# opencode-plugin-automation Specification

## Requirements

### Requirement: Automatic user prompt capture
The plugin MUST persist every user prompt via `sv-memory capture prompt` attached to the active session.

#### Scenario: prompt captured

### Requirement: Corrective spec-flow nudge
If the model edits/writes/patch without having called sv_spec_list/sv_propose_spec/sv_spec_get, the plugin MUST inject a corrective reminder to run the spec flow, at most once per session.

#### Scenario: edit without spec consultation

### Requirement: Deterministic session auto-start and Auto-Boot injection
The OpenCode plugin MUST auto-start a sv-memory session (via `sv-memory session start`) when none is active and MUST inject the Auto-Boot Context Bundle into the first request that carries a user turn, exactly once per opencode session.

#### Scenario: first user turn auto-starts session

#### Scenario: no double injection

### Requirement: Model-switch re-orientation
When the model changes within an opencode session, the plugin MUST inject a compact re-orientation header with the session ID and the active spec changes list, exactly once per switch.

#### Scenario: model changed mid-session