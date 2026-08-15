# Requirements

> **Language:** English | [Español](requirement_ES.md)

The system must be a local memory of the project, recording decisions, ideas, methodologies, context, structure, and everything needed to keep track of the project's thread. However, it must also handle how everything is connected through knowledge graphs, just like graphify does.

The idea:

- Initialized once per project
- Updates or creates the AGENTS.md with the instructions for the agent in use
- Autonomously stores and records decisions, context, methodology, and everything related
- Must also be able to generate and update the knowledge graph and the relations between all the project's features

The system must support a spec-driven decision cycle: before writing code, the agent proposes a change, validates it against rules and invariants, and promotes it to a durable memory. Proposals may carry structured OpenSpec-style delta requirements (ADDED/MODIFIED/REMOVED/RENAMED, RFC 2119 SHALL/MUST/SHOULD keywords, and GIVEN/WHEN/THEN scenarios) that are merged into a persistent per-capability state, projected as a human-readable Markdown mirror under `.sv-memory/specs/`, and wired into the knowledge graph (capability nodes with `implements` edges) so the context recoverable by the agent includes the applicable contract for each path.
