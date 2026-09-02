# Graph overview report: GRAPH_REPORT.md via sv-memory graph report + sv_graph_report

- **ID:** `99e2b34c608d48c3`
- **Slug:** `graph-report-md`
- **Status:** `applied`
- **Where:** `internal/graph/report.go`
- **Capability:** `graph-report-md`
- **Created:** 2026-09-01T23:45:35-04:00

## Proposal

Add a generated aggregate graph overview report that complements the per-community wiki: `sv-memory graph report` (CLI) and `sv_graph_report` (MCP tool) both generate a GRAPH_REPORT.md standing file at the project root (configurable --output) from the existing graph primitives. The report includes: a summary header (nodes/edges/communities/hub threshold); top god nodes via TopDegreeNodes (SQL aggregate, excludes document/package nodes); top communities with auto-labels + member counts via LeidenDetectCommunities + DetectCommunityLabels; top surprising cross-community connections via FindSurprisingConnections; and a generated "Suggested Questions" section (refactor god nodes, trace bridges, review hot dependencies) — the structural rationale for what to explore next. This fills the validated gap that no aggregate graph report exists today (only per-community wiki pages and per-request tool outputs).

## Goal

Give agents and humans a single standing overview of the project's architectural hotspots, community structure, and cross-community bridges — one static file an agent can consult or a session can boot from, without running 5 graph queries.

## Design

New internal/graph/report.go with GenerateGraphReport(db, projectID, output, opts) writing GRAPH_REPORT.md: load graph once (LoadFullGraph), compute communities (LeidenDetectCommunities), centrality (BetweennessCentrality computed lazily when missing, mirroring tools_graph.go graphHasCentrality pattern), community labels (DetectCommunityLabels), tail TopDegreeNodes(db,...) via SQL, and FindSurprisingConnections (bounded, e.g. 10). Suggested Questions generated deterministically from the computed metrics (top god nodes -> refactor questions, top bridges -> trace questions, hub threshold -> blast-radius caution). CLI cmd_graph.go adds a graphReportCmd (Use: 'report', --output default 'GRAPH_REPORT.md', validated via security.ValidateWritePath) registered in main.go init(). MCP: register sv_graph_report in mcp.go AllTools + tool registration (bounded params: output, top_n, communities, connections), handler in tools_graph.go reusing the same graph package function, writes the file and returns path + byte size + a 3-line summary digest. Docs (EN/ES spect section 4 + README command list if present), CHANGELOG [Unreleased], and guard test for the MCP registration mirror the F1/F2 pattern.

## ADDED Requirements

### Requirement: Aggregate graph report file can be generated from CLI and MCP

#### Scenario: A user or agent wants a standing overview of the project graph

### Requirement: Report sections derive from real computed graph metrics

#### Scenario: The report lists architectural hotspots rather than invented content

### Requirement: MCP sv_graph_report mirrors the CLI behaviour and is registered

#### Scenario: An agent invokes the report through MCP