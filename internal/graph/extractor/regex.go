package extractor

import (
	"strings"

	"github.com/svtech-code/sv-memory/internal/graph/schema"
)

// RegexExtractor implements legacy regex-based extraction.
type RegexExtractor struct{}

// NewRegexExtractor creates a new RegexExtractor.
func NewRegexExtractor() *RegexExtractor {
	return &RegexExtractor{}
}

// Extract parses the file content using regex patterns to find symbols and imports.
func (r *RegexExtractor) Extract(content []byte, relPath, ext string) ([]Symbol, []string, error) {
	lines := strings.Split(string(content), "\n")
	symbols := parseRegexSymbols(content, lines, ext)
	imports := parseRegexImports(content, ext)
	symbols = append(symbols, extractCommentRationales(lines, ext)...)
	return symbols, imports, nil
}

// GetExportsCount returns the number of JS exports in the file.
func (r *RegexExtractor) GetExportsCount(content []byte, ext string) int {
	switch ext {
	case ".js", ".jsx", ".ts", ".tsx", ".astro":
		return len(jsExportRegex.FindAll(content, -1))
	}
	return 0
}

// mkSymbol builds a Symbol with the common fields.
func mkSymbol(name, symType string, exported bool, line int) Symbol {
	return Symbol{Name: name, Type: symType, Line: line, Exported: exported}
}

// extractCommentRationales turns tagged comments (NOTE/WHY/HACK/TODO/FIXME) into
// rationale symbols. This is the legacy/fallback pass that mirrors the AST one.
func extractCommentRationales(lines []string, ext string) []Symbol {
	var out []Symbol
	for i, line := range lines {
		if text, ok := commentRationaleText(strings.TrimSpace(line), ext); ok {
			out = append(out, mkSymbol(text, schema.NodeTypeRationale, false, i+1))
		}
	}
	return out
}

// commentRationaleText trims comment decoration and reports whether the text
// carries a rationale tag prefix.
func commentRationaleText(trimmed, ext string) (string, bool) {
	var clean string
	switch {
	case strings.HasPrefix(trimmed, "//"):
		clean = strings.TrimPrefix(trimmed, "//")
	case strings.HasPrefix(trimmed, "#"):
		clean = strings.TrimPrefix(trimmed, "#")
	case strings.HasPrefix(trimmed, "/*"):
		clean = strings.TrimSuffix(strings.TrimPrefix(trimmed, "/*"), "*/")
	case strings.HasPrefix(trimmed, "*") && isCStarCommentLang(ext):
		clean = strings.TrimPrefix(trimmed, "*")
	default:
		return "", false
	}

	clean = strings.TrimSpace(clean)
	upper := strings.ToUpper(clean)
	for _, p := range rationaleTagPrefixes {
		if strings.HasPrefix(upper, p) {
			return clean, true
		}
	}
	return "", false
}

// isCStarCommentLang reports whether a leading `*` should be treated as a
// JSDoc-style continuation comment for the given extension.
func isCStarCommentLang(ext string) bool {
	switch ext {
	case ".js", ".ts", ".jsx", ".tsx", ".go", ".java":
		return true
	}
	return false
}

// findLineNumberInLines returns the 1-based line number where substr appears.
func findLineNumberInLines(lines []string, substr string) int {
	substr = strings.TrimSpace(substr)
	if len(substr) > 80 {
		substr = substr[:80]
	}
	for i, line := range lines {
		if strings.Contains(line, substr) {
			return i + 1
		}
	}
	return 0
}
