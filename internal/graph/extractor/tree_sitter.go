package extractor

import (
	"strings"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"

	"github.com/svtech-code/sv-memory/internal/graph/schema"
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
// falling back to RegexExtractor for others or if syntax tree parsing fails.
func (t *TreeSitterExtractor) Extract(content []byte, relPath, ext string) ([]Symbol, []string, error) {
	// Use standard library go/parser and go/ast for Go files (zero-dependency,
	// immune to upstream tree-sitter C-binding parser bugs).
	if ext == ".go" {
		syms, imps, err := parseGoSource(content, relPath)
		if err == nil {
			return syms, imps, nil
		}
		return t.regexFallback.Extract(content, relPath, ext)
	}

	lang := t.GetLanguage(ext)
	if lang == nil {
		return t.regexFallback.Extract(content, relPath, ext)
	}

	parser := gotreesitter.NewParser(lang)
	tree, err := parser.Parse(content)
	if err != nil || tree == nil || tree.RootNode() == nil {
		return t.regexFallback.Extract(content, relPath, ext)
	}

	root := tree.RootNode()
	symbols, imports := parseDispatch(root, lang, content, ext)
	if symbols == nil && imports == nil {
		return t.regexFallback.Extract(content, relPath, ext)
	}

	symbols = append(symbols, extractRationaleComments(root, lang, content)...)

	return symbols, imports, nil
}

// parseDispatch routes the parsed root node to the language-specific symbol and
// import extractor. It returns (nil, nil) when the extension is unsupported so
// the caller can fall back to the regex extractor.
func parseDispatch(root *gotreesitter.Node, lang *gotreesitter.Language, content []byte, ext string) ([]Symbol, []string) {
	switch ext {
	case ".go":
		return parseGo(root, lang, content)
	case ".py":
		return parsePython(root, lang, content)
	case ".js", ".jsx", ".ts", ".tsx":
		return parseJavascript(root, lang, content)
	case ".php":
		return parsePhp(root, lang, content)
	case ".rs":
		return parseRust(root, lang, content)
	case ".rb":
		return parseRuby(root, lang, content)
	case ".java":
		return parseJava(root, lang, content)
	case ".html":
		return parseHtml(root, lang, content)
	case ".css":
		return parseCss(root, lang, content)
	}
	return nil, nil
}

// rationaleTagPrefixes are the comment markers that promote a comment into a
// rationale symbol.
var rationaleTagPrefixes = []string{"NOTE:", "WHY:", "HACK:", "TODO:", "FIXME:"}

// extractRationaleComments walks the tree and turns tagged comments
// (NOTE/WHY/HACK/TODO/FIXME) into rationale symbols.
func extractRationaleComments(root *gotreesitter.Node, lang *gotreesitter.Language, content []byte) []Symbol {
	var symbols []Symbol
	traverse(root, func(n *gotreesitter.Node) {
		if !strings.Contains(n.Type(lang), "comment") {
			return
		}
		if text, ok := rationaleText(n.Text(content)); ok {
			symbols = append(symbols, Symbol{
				Name:     text,
				Type:     schema.NodeTypeRationale,
				Line:     nodeLine(n),
				Exported: false,
			})
		}
	})
	return symbols
}

// rationaleText trims comment decoration and reports whether the text carries a
// rationale tag prefix.
func rationaleText(raw string) (string, bool) {
	clean := strings.TrimLeft(raw, "/#* \t")
	clean = strings.TrimRight(clean, "*/ \t\r\n")
	upper := strings.ToUpper(clean)
	for _, p := range rationaleTagPrefixes {
		if strings.HasPrefix(upper, p) {
			return clean, true
		}
	}
	return "", false
}

// ExtractCallRefs returns AST-precision call sites (callee + 1-based
// line/column) for languages parsed by tree-sitter or Go standard AST.
// Languages handled by the regex fallback (Lua, Markdown, shell, Vue/Svelte/Astro
// script blocks) return ErrNoASTCallRefs so the graph builder falls back to
// the tokenize heuristic for those files.
func (t *TreeSitterExtractor) ExtractCallRefs(content []byte, relPath, ext string) ([]CallRef, error) {
	if ext == ".go" {
		return extractGoCallRefsAST(content, relPath)
	}

	lang := t.GetLanguage(ext)
	if lang == nil {
		return nil, ErrNoASTCallRefs
	}

	parser := gotreesitter.NewParser(lang)
	tree, err := parser.Parse(content)
	if err != nil || tree == nil || tree.RootNode() == nil {
		return nil, ErrNoASTCallRefs
	}

	var refs []CallRef
	traverse(tree.RootNode(), func(n *gotreesitter.Node) {
		callee := callCallee(n, lang, content, ext)
		if callee == "" {
			return
		}
		pt := n.StartPoint()
		refs = append(refs, CallRef{
			Callee: callee,
			Line:   int(pt.Row) + 1,
			Col:    int(pt.Column) + 1,
		})
	})
	return refs, nil
}

// callCallee returns the invoked identifier/name for a call-site AST node, or ""
// when the node is not a call of interest. Call expression node types vary by
// grammar: call_expression (Go/JS/TS/Rust), call (Python/Ruby), method_invocation
// (Java), function_call_expression (PHP). The callee is the function name
// identifier, or the last property_identifier of a member/scope access
// (e.g. foo.bar() → bar; Class.method() → method).
func callCallee(n *gotreesitter.Node, lang *gotreesitter.Language, content []byte, ext string) string {
	nodeType := n.Type(lang)
	switch nodeType {
	case "call_expression", "call", "method_invocation", "function_call_expression":
	default:
		return ""
	}

	// Ruby "call" nodes also cover method calls with parentheses; a bare
	// identifier call (no parens/args) is not a call node in the Ruby grammar.
	if ext == ".rb" && nodeType != "call" {
		return ""
	}

	return lastCalleeChild(n, lang, content)
}

// lastCalleeChild walks the direct children of a call node and returns the
// trailing identifier/member name (deepest member access wins).
func lastCalleeChild(n *gotreesitter.Node, lang *gotreesitter.Language, content []byte) string {
	var callee string
	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		switch child.Type(lang) {
		case "identifier", "property_identifier", "field_identifier", "name", "method":
			callee = child.Text(content)
		case "scoped_identifier", "member_expression", "attribute":
			callee = memberCallee(child, lang, content)
		}
	}
	return callee
}

// memberCallee extracts the trailing field/property name from a member
// expression or scoped identifier (e.g. `a.b.c` → `c`).
func memberCallee(n *gotreesitter.Node, lang *gotreesitter.Language, content []byte) string {
	if n == nil {
		return ""
	}
	var last string
	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		switch child.Type(lang) {
		case "property_identifier", "field_identifier", "identifier", "name":
			last = child.Text(content)
		case "member_expression", "scoped_identifier":
			last = memberCallee(child, lang, content)
		}
	}
	return last
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

func traverse(node *gotreesitter.Node, visitor func(*gotreesitter.Node)) {
	if node == nil {
		return
	}
	visitor(node)
	count := node.ChildCount()
	for i := 0; i < count; i++ {
		traverse(node.Child(i), visitor)
	}
}
