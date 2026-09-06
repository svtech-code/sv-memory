# mcp-tool-surface Specification

## Requirements

### Requirement: Concise LLM-visible tool descriptions
The cumulative length of the LLM-visible tool descriptions in NewServer MUST be reduced below 9500 characters (baseline 10576).

#### Scenario: description budget measured

### Requirement: Default MCP server exposes only core tools
The default MCP server MUST register only the core tool set, and every non-core maintenance/admin tool MUST remain available when the environment variable SV_MEMORY_FULL_TOOLS is set to a truthy value.

#### Scenario: default server hides maintenance tools

#### Scenario: full tool mode opt-in

### Requirement: Protocol promotes graph and spec tools
The injected agent protocol MUST surface the Graph and Spec-Driven tools at the top of its Tool Quick Reference and MUST note that the non-core maintenance/admin tools are opt-in (SV_MEMORY_FULL_TOOLS).

#### Scenario: graph and spec first in quick reference