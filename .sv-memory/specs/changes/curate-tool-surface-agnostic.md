# Curate MCP tool surface for token economy and model-agnostic adherence

- **ID:** `42857d888f904426`
- **Slug:** `curate-tool-surface-agnostic`
- **Status:** `applied`
- **Where:** `internal/mcp/mcp.go`
- **Capability:** `mcp-tool-surface`
- **Created:** 2026-09-06T13:07:23-03:00

## Proposal

sv-memory expone 42 tools MCP en cada request (~2.6k tokens de descripciones + 157 schemas de params). Modelos pequeños/flash lo pierden de vista y no usan grafo/specs (evidencia: sv_graph_search=0, sv_spec_list=2). F1 reduce la superficie: (1) las 7 tools de mantenimiento/admin (sv_mem_diagnose, sv_mem_compare, sv_mem_merge_projects, sv_graph_report, sv_graph_viz, sv_graph_merge, sv_graph_surprising_connections) dejan de registrarse por defecto y quedan disponibles solo con SV_MEMORY_FULL_TOOLS=1; (2) se recortan las descripciones LLM-visibles más largas (spec/graph); (3) AGENTS.md/skill promueven graph+spec (Quick Reference al frente, nota graph-first). La superficie por defecto pasa a 35 tools core.

## Goal

Reducir tokens por request y mejorar la adherencia de cualquier modelo a las tools de grafo y specs, sin romper el flujo diario del protocolo.

## Design

Añadir campo Core bool a Tool. NewServer registra solo tools core por defecto; las 7 no-core se envuelven en if fullToolsEnabled() (env SV_MEMORY_FULL_TOOLS). fullToolsEnabled() es opt-in, fail-safe. Recortar descripciones NewTool+AllTools de los 7 descriptores más largos. En protocol.go reordenar Tool Quick Reference para poner Graph y Spec Flow primero y añadir nota graph-first + distinción core/full. Sincronizar AGENTS.md y skill.

## Tasks

- [x] Add Hidden field to Tool struct and mark the 7 non-core tools in AllTools
- [x] Add fullToolsEnabled() helper (SV_MEMORY_FULL_TOOLS env) and wrap non-core AddTool registrations
- [x] Trim the longest NewTool/AllTools descriptions (spec + graph tools)
- [x] Add tests: default core-only server, full mode all 42, description budget
- [x] Reorder protocol.go Tool Quick Reference (Graph/Spec first) + core/full note + graph-first note
- [x] Sync AGENTS.md, opencode-skill.md, and installed skill
- [x] Run go build, go vet, go test -race, gofmt, golangci-lint

## ADDED Requirements

### Requirement: Default MCP server exposes only core tools
The default MCP server MUST register only the core tool set, and every non-core maintenance/admin tool MUST remain available when the environment variable SV_MEMORY_FULL_TOOLS is set to a truthy value.

#### Scenario: default server hides maintenance tools

#### Scenario: full tool mode opt-in

### Requirement: Concise LLM-visible tool descriptions
The cumulative length of the LLM-visible tool descriptions in NewServer MUST be reduced below 9500 characters (baseline 10576).

#### Scenario: description budget measured

### Requirement: Protocol promotes graph and spec tools
The injected agent protocol MUST surface the Graph and Spec-Driven tools at the top of its Tool Quick Reference and MUST note that the non-core maintenance/admin tools are opt-in (SV_MEMORY_FULL_TOOLS).

#### Scenario: graph and spec first in quick reference