# Go native AST extraction and call references

- **ID:** `56f1ccc0936e4215`
- **Slug:** `go-ast-call-coverage`
- **Status:** `applied`
- **Where:** `internal/graph/extractor/go.go; internal/graph/extractor/tree_sitter.go; internal/graph/extractor/tree_sitter_callrefs_test.go; internal/graph/graph_test.go; documentation/spect.md; documentation/spect_ES.md`
- **Capability:** `graph-engine`
- **Created:** 2026-08-21T16:06:36-04:00

## Proposal

Implement native AST parser for Go files using standard library go/parser and go/ast to extract symbols, imports, rationales, and precise call references (confidence EXTRACTED with L<line>:<col> coordinates), removing regex fallback for Go.

## MODIFIED Requirements

### Requirement: AST Call Edge Extraction
The graph extractor SHALL produce AST-precision call references for Go source files using the standard library go/parser and go/ast, assigning confidence EXTRACTED with 1-based line and column coordinates, eliminating heuristic tokenization and false positives from strings or comments.

#### Scenario: Extract Go call sites with AST precision