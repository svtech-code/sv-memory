# Add sv_graph_search and sv_graph_communities MCP tools + discovery fallback hint

- **ID:** `7e7344b7a3da4a85`
- **Slug:** `graph-search-communities-mcp`
- **Status:** `applied`
- **Where:** `internal/mcp/tools_graph.go`
- **Capability:** `graph-search-communities-mcp`
- **Created:** 2026-09-02T00:17:42-04:00

## Proposal

Adds two high-value MCP graph tools that close validated gaps: (1) sv_graph_search — deterministic multi-result node discovery across id/label/path with node_type filter and metrics, since FindNode only resolves a single best-rank node and no tool lets agents list nodes matching a pattern; (2) sv_graph_communities — MCP parity for the existing 'sv-memory graph communities' CLI, listing top communities with auto-labels (and optional per-community member detail). Both reuse the loaded graph and existing primitives (FindNode ranking rules, ExtractCommunities/LeidenDetectCommunities, computeCommLabels). Also adds a lightweight discovery fallback: sv_graph_query/sv_graph_explain responses now suggest sv_graph_search when no node matches.

## Goal

Cerrar dos gaps de alto valor en las tools MCP del grafo: descubrimiento multi-resultado de nodos (FindNode solo resuelve 1 match, no hay forma de listar nodos por patrón) y paridad CLI→MCP de comunidades (sv-memory graph communities existe en CLI, no hay tool MCP).

## Design

Two new MCP graph tools plus a discovery fallback hint. (1) sv_graph_search: add graph.SearchNodes(query, nodeType, limit) []SearchResult in internal/graph/memory.go iterating g.Nodes with deterministic ranking mirroring FindNode (path exact > label exact > substring; tie-break len(id) then alpha), optional node_type filter, returning id/label/type/path/fan-in/fan-out/degree/community per match; handler handleGraphSearch in internal/mcp/tools_graph.go (pattern handleGodNodes) rendering a bounded markdown table. (2) sv_graph_communities: handler handleGraphCommunities reusing ExtractCommunities (fallback LeidenDetectCommunities when metadata lacks community_id) + computeCommLabels + member grouping; top_n table default 10 cap 50; optional community_id to detail members with metrics. (3) Fallback hint: in handleGraphQuery and handleGraphExplain, when FindNode/Query finds no node, append 'sv_graph_search(query=...) suggested' line. Register both in AllTools + NewServer (renumber comments), guards auto-validated by TestAllToolsMatchesRegisteredTools. Tests: internal/graph SearchNodes (multi-match deterministic, node_type filter, no match) + internal/mcp TestGraphSearchTool/TestGraphCommunitiesTool (pattern TestGraphReportTool). Docs EN/ES spect tool sections, SKILL.md Graph bullet, CHANGELOG [Unreleased] Added.

## ADDED Requirements

### Requirement: sv_graph_search lists matching graph nodes by pattern

#### Scenario: An agent wants to discover code nodes without knowing exact names

### Requirement: sv_graph_communities lists top communities and their members

#### Scenario: An agent wants the community structure of the project

### Requirement: Discovery fallback hint on exact match failure

#### Scenario: A query/explain call fails to resolve to an existing node