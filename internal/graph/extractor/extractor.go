package extractor

import "errors"

// Symbol represents a declared entity (e.g. function, class, struct) found in code.
type Symbol struct {
	Name     string
	Type     string // 'function' | 'class' | 'section' | 'code_block' | 'table' | ...
	Line     int
	Exported bool
	Metadata map[string]interface{} // extra info (heading level, code language, column type, etc.)
}

// CallRef represents a single call site discovered in source code. The Callee
// is the identifier/name being invoked (function, method, constructor); Line
// and Col are 1-based positions of the call expression in the source file.
type CallRef struct {
	Callee string
	Line   int
	Col    int
}

// ErrNoASTCallRefs is returned by ExtractCallRefs when the extractor cannot
// produce AST-precision call references for the given extension (e.g. languages
// parsed by regex fallback). Callers fall back to the tokenize heuristic.
var ErrNoASTCallRefs = errors.New("no AST call extraction for this language")

// Extractor defines the interface for parsing file contents to extract symbols and import paths.
type Extractor interface {
	Extract(content []byte, relPath, ext string) ([]Symbol, []string, error)
}

// CallRefExtractor is an optional interface for extractors that can discover
// call sites from a syntax tree. When implemented, the graph builder prefers
// these AST-precision references (confidence EXTRACTED) over the tokenize
// heuristic, which can produce false positives from identifiers inside strings
// and comments.
type CallRefExtractor interface {
	ExtractCallRefs(content []byte, relPath, ext string) ([]CallRef, error)
}
