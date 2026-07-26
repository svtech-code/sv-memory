package extractor

import (
	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// TreeSitterExtractor implements AST-based extraction using gotreesitter.
type TreeSitterExtractor struct {
	regexFallback *RegexExtractor
}

// NewTreeSitterExtractor creates a new TreeSitterExtractor.
func NewTreeSitterExtractor() *TreeSitterExtractor {
	return &TreeSitterExtractor{
		regexFallback: NewRegexExtractor(),
	}
}

// Extract parses the file content using gotreesitter for supported languages,
// falling back to RegexExtractor for others.
func (t *TreeSitterExtractor) Extract(content []byte, relPath, ext string) ([]Symbol, []string, error) {
	// For Hito 3.2, we fallback to RegexExtractor for all files.
	// We will implement AST parsing step-by-step in Hito 3.3.
	return t.regexFallback.Extract(content, relPath, ext)
}

// GetLanguage resolves the extension to a gotreesitter Language object.
func (t *TreeSitterExtractor) GetLanguage(ext string) *gotreesitter.Language {
	switch ext {
	case ".go":
		return grammars.GoLanguage()
	case ".py":
		return grammars.PythonLanguage()
	case ".js", ".jsx":
		return grammars.JavascriptLanguage()
	case ".ts", ".tsx":
		return grammars.TypescriptLanguage()
	case ".rs":
		return grammars.RustLanguage()
	case ".java":
		return grammars.JavaLanguage()
	case ".rb":
		return grammars.RubyLanguage()
	case ".php":
		return grammars.PhpLanguage()
	case ".css":
		return grammars.CssLanguage()
	case ".html":
		return grammars.HtmlLanguage()
	case ".sh":
		return grammars.BashLanguage()
	}
	return nil
}
