package extractor

// Symbol represents a declared entity (e.g. function, class, struct) found in code.
type Symbol struct {
	Name     string
	Type     string                 // 'function' | 'class' | 'section' | 'code_block' | 'table' | ...
	Line     int
	Exported bool
	Metadata map[string]interface{} // extra info (heading level, code language, column type, etc.)
}

// Extractor defines the interface for parsing file contents to extract symbols and import paths.
type Extractor interface {
	Extract(content []byte, relPath, ext string) ([]Symbol, []string, error)
}
