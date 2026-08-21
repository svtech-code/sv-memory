# context-engine Specification

## Requirements

### Requirement: Auto-Freshness Staleness Probe in Context Pack
The context pack engine SHALL run a lightweight mtime-based staleness probe (SyncGraphIfStale) prior to resolving graph nodes, ensuring recently created or edited code symbols are discovered without requiring manual graph synchronization.

#### Scenario: Resolve newly added symbol after file modification

### Requirement: Surgical Source Snippet in Context Pack
The context pack engine SHALL extract and include bounded source code snippets for resolved function and class symbols from the workspace when available, rendering them in the markdown context pack within a configurable line limit.

#### Scenario: Extract source snippet for a function node