package extractor

// Symbol represents a declared entity (e.g. function, class, struct) found in code.
type Symbol struct {
	Name     string
	Type     string // 'function' | 'class'
	Line     int
	Exported bool
}

// Extractor defines the interface for parsing file contents to extract symbols and import paths.
type Extractor interface {
	Extract(content []byte, relPath, ext string) ([]Symbol, []string, error)
}
