# Add confidence breakdown to graph report, confidence column to surprising connections, and confidence filter to graph query

- **ID:** `44925f58620d4c8c`
- **Slug:** `confidence-tags-exposure`
- **Status:** `applied`
- **Where:** `internal/mcp/tools_graph.go`
- **Capability:** `confidence-tags-exposure`
- **Created:** 2026-09-02T00:43:24-04:00

## Proposal

Complete the confidence exposure work (architecture memory 523b1bf4 P4): the graph stores EXTRACTED/INFERRED/AMBIGUOUS confidence on every edge, and sv_graph_query (text), sv_graph_explain, and sv_graph_path already show it, but sv_graph_report has no confidence metrics, sv_graph_surprising_connections omits it, and sv_graph_query has no filter by confidence level. This closes those three gaps with minimal, self-contained changes.

## Goal

Close the remaining confidence exposure gaps (memory 523b1bf4 P4): sv_graph_report has no confidence metrics, sv_graph_surprising_connections has no confidence column, sv_graph_query has no confidence filter.

## Design

Add confidence exposure to three graph tools: (1) New writeReportConfidence function in internal/graph/report.go that counts EXTRACTED/INFERRED/AMBIGUOUS across all edges, renders a percentage breakdown table, and lists the top 5 lowest-confidence edges for review — inserted after the header in GenerateGraphReport. (2) Add Confidence string field to SurprisingConnection struct in communities.go, populate it from edge.Confidence in FindSurprisingConnections, add a Confidence column to the handler's markdown table. (3) Add optional confidence parameter to sv_graph_query MCP tool in mcp.go, filter subGraph.Edges in handleGraphQuery before rendering (after Query returns, keeping node set intact). Update docs EN/ES (spect sections 23/26, spect_ES sections 23/26), SKILL.md, CHANGELOG.

## ADDED Requirements

### Requirement: Confidence breakdown in aggregate graph report

#### Scenario: Agent requests GRAPH_REPORT.md overview

### Requirement: Confidence column in surprising connections

#### Scenario: Agent inspects cross-community bridges

### Requirement: Confidence filter on graph query

#### Scenario: Agent wants to see only uncertain edges