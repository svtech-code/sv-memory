# Transitive Blast Radius Impact Analysis in Graph and Context Pack

- **ID:** `a1b122b770534641`
- **Slug:** `blast-radius-impact-analysis`
- **Status:** `applied`
- **Where:** `internal/graph/blast_radius.go; internal/graph/blast_radius_test.go; internal/memory/contextpack.go; internal/memory/contextpack_test.go; documentation/spect.md; documentation/spect_ES.md; CHANGELOG.md`
- **Capability:** `graph-engine`
- **Created:** 2026-08-21T19:06:10-04:00

## Proposal

Implement multi-hop upstream blast radius impact analysis (internal/graph/blast_radius.go and internal/memory/contextpack.go) to surface transitive consumers (depth 1-3) affected by changes to symbols/files, integrating into context pack and decision governance.

## ADDED Requirements

### Requirement: Transitive Blast Radius Impact Analysis
The graph and context engine SHALL compute the multi-hop upstream blast radius (depth 1 to 3) for resolved code nodes, discovering transitive callers and dependent consumers that will be impacted by modifications to the target entity, with bounded traversal and hub indicators.

#### Scenario: Compute multi-hop blast radius for a symbol