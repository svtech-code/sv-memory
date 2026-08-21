# graph-engine Specification

## Requirements

### Requirement: AST Call Edge Extraction
The graph extractor SHALL produce AST-precision call references for Go source files using the standard library go/parser and go/ast, assigning confidence EXTRACTED with 1-based line and column coordinates, eliminating heuristic tokenization and false positives from strings or comments.

#### Scenario: Extract Go call sites with AST precision

### Requirement: Transitive Blast Radius Impact Analysis
The graph and context engine SHALL compute the multi-hop upstream blast radius (depth 1 to 3) for resolved code nodes, discovering transitive callers and dependent consumers that will be impacted by modifications to the target entity, with bounded traversal and hub indicators.

#### Scenario: Compute multi-hop blast radius for a symbol