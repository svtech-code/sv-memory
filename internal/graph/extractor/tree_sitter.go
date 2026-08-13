package extractor

import (
	"strings"

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
// falling back to RegexExtractor for others or if syntax tree parsing fails.
func (t *TreeSitterExtractor) Extract(content []byte, relPath, ext string) ([]Symbol, []string, error) {
	// Go tree-sitter parser has a stack overflow bug with complex new/make type
	// arguments. Use the regex extractor for Go files until the library is fixed.
	if ext == ".go" {
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
	var symbols []Symbol
	var imports []string

	switch ext {
	case ".go":
		symbols, imports = parseGo(root, lang, content)
	case ".py":
		symbols, imports = parsePython(root, lang, content)
	case ".js", ".jsx", ".ts", ".tsx":
		symbols, imports = parseJavascript(root, lang, content)
	case ".php":
		symbols, imports = parsePhp(root, lang, content)
	case ".rs":
		symbols, imports = parseRust(root, lang, content)
	case ".rb":
		symbols, imports = parseRuby(root, lang, content)
	case ".java":
		symbols, imports = parseJava(root, lang, content)
	case ".html":
		symbols, imports = parseHtml(root, lang, content)
	case ".css":
		symbols, imports = parseCss(root, lang, content)
	default:
		return t.regexFallback.Extract(content, relPath, ext)
	}

	// Global rationale comment extraction pass
	traverse(root, func(n *gotreesitter.Node) {
		nodeType := n.Type(lang)
		if strings.Contains(nodeType, "comment") {
			text := n.Text(content)
			cleanText := strings.TrimLeft(text, "/#* \t")
			cleanText = strings.TrimRight(cleanText, "*/ \t\r\n")

			upperClean := strings.ToUpper(cleanText)
			prefixes := []string{"NOTE:", "WHY:", "HACK:", "TODO:", "FIXME:"}
			hasPrefix := false
			for _, p := range prefixes {
				if strings.HasPrefix(upperClean, p) {
					hasPrefix = true
					break
				}
			}

			if hasPrefix {
				symbols = append(symbols, Symbol{
					Name:     cleanText,
					Type:     "rationale",
					Line:     int(n.StartPoint().Row) + 1,
					Exported: false,
				})
			}
		}
	})

	return symbols, imports, nil
}

// ExtractCallRefs returns AST-precision call sites (callee + 1-based
// line/column) for languages parsed by tree-sitter. Languages handled by the
// regex fallback (Go due to the upstream stack-overflow bug, Lua, Markdown,
// shell, Vue/Svelte/Astro script blocks) return ErrNoASTCallRefs so the graph
// builder falls back to the tokenize heuristic for those files.
func (t *TreeSitterExtractor) ExtractCallRefs(content []byte, relPath, ext string) ([]CallRef, error) {
	lang := t.GetLanguage(ext)
	if lang == nil {
		return nil, ErrNoASTCallRefs
	}
	if ext == ".go" {
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

	// Find the callee: the function/field name at the head of the call.
	// Walk children for identifier/property_identifier/field_identifier/name
	// nodes, taking the LAST one (deepest/member access).
	var callee string
	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		ct := child.Type(lang)
		switch ct {
		case "identifier", "property_identifier", "field_identifier", "name", "method":
			callee = child.Text(content)
		case "scoped_identifier", "member_expression", "attribute":
			// foo.bar(...) / Class.method(...): drill into the object to get
			// the trailing field name.
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
		ct := child.Type(lang)
		switch ct {
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

func parseGo(root *gotreesitter.Node, lang *gotreesitter.Language, content []byte) ([]Symbol, []string) {
	var symbols []Symbol
	var imports []string

	traverse(root, func(n *gotreesitter.Node) {
		nodeType := n.Type(lang)
		switch nodeType {
		case "function_declaration":
			var name string
			for i := 0; i < n.ChildCount(); i++ {
				child := n.Child(i)
				if child.Type(lang) == "identifier" {
					name = child.Text(content)
					break
				}
			}
			if name != "" && name != "init" && name != "main" {
				exported := name[0] >= 'A' && name[0] <= 'Z'
				symbols = append(symbols, Symbol{
					Name:     name,
					Type:     "function",
					Line:     int(n.StartPoint().Row) + 1,
					Exported: exported,
				})
			}

		case "method_declaration":
			var name string
			for i := 0; i < n.ChildCount(); i++ {
				child := n.Child(i)
				if child.Type(lang) == "field_identifier" || child.Type(lang) == "identifier" {
					name = child.Text(content)
				}
			}
			if name != "" {
				exported := name[0] >= 'A' && name[0] <= 'Z'
				symbols = append(symbols, Symbol{
					Name:     name,
					Type:     "function",
					Line:     int(n.StartPoint().Row) + 1,
					Exported: exported,
				})
			}

		case "type_spec":
			isStruct := false
			var name string
			for i := 0; i < n.ChildCount(); i++ {
				child := n.Child(i)
				switch t := child.Type(lang); t {
				case "type_identifier":
					name = child.Text(content)
				case "struct_type", "interface_type":
					isStruct = true
				}
			}
			if name != "" && isStruct {
				exported := name[0] >= 'A' && name[0] <= 'Z'
				symbols = append(symbols, Symbol{
					Name:     name,
					Type:     "class",
					Line:     int(n.StartPoint().Row) + 1,
					Exported: exported,
				})
			}

		case "import_spec":
			for i := 0; i < n.ChildCount(); i++ {
				child := n.Child(i)
				t := child.Type(lang)
				if t == "import_path" || t == "string_literal" || t == "interpreted_string_literal" || t == "raw_string_literal" {
					path := child.Text(content)
					path = strings.Trim(path, `"`+"`")
					if path != "" {
						imports = append(imports, path)
					}
				}
			}
		}
	})

	return symbols, imports
}

func parsePython(root *gotreesitter.Node, lang *gotreesitter.Language, content []byte) ([]Symbol, []string) {
	var symbols []Symbol
	var imports []string

	traverse(root, func(n *gotreesitter.Node) {
		nodeType := n.Type(lang)
		switch nodeType {
		case "function_definition":
			var name string
			for i := 0; i < n.ChildCount(); i++ {
				child := n.Child(i)
				if child.Type(lang) == "identifier" {
					name = child.Text(content)
					break
				}
			}
			if name != "" {
				symbols = append(symbols, Symbol{
					Name:     name,
					Type:     "function",
					Line:     int(n.StartPoint().Row) + 1,
					Exported: true,
				})
			}

		case "class_definition":
			var name string
			for i := 0; i < n.ChildCount(); i++ {
				child := n.Child(i)
				if child.Type(lang) == "identifier" {
					name = child.Text(content)
					break
				}
			}
			if name != "" {
				symbols = append(symbols, Symbol{
					Name:     name,
					Type:     "class",
					Line:     int(n.StartPoint().Row) + 1,
					Exported: true,
				})
			}

		case "import_statement":
			for i := 0; i < n.ChildCount(); i++ {
				child := n.Child(i)
				if child.Type(lang) == "dotted_name" || child.Type(lang) == "aliased_import" {
					text := child.Text(content)
					if child.Type(lang) == "aliased_import" {
						for j := 0; j < child.ChildCount(); j++ {
							if child.Child(j).Type(lang) == "dotted_name" {
								text = child.Child(j).Text(content)
								break
							}
						}
					}
					imports = append(imports, text)
				}
			}

		case "import_from_statement":
			for i := 0; i < n.ChildCount(); i++ {
				child := n.Child(i)
				if child.Type(lang) == "dotted_name" || child.Type(lang) == "relative_import" {
					imports = append(imports, child.Text(content))
					break
				}
			}
		}
	})

	return symbols, imports
}

//nolint:gocyclo // large AST dispatch for JS/TS; refactor later
func parseJavascript(root *gotreesitter.Node, lang *gotreesitter.Language, content []byte) ([]Symbol, []string) {
	var symbols []Symbol
	var imports []string

	traverse(root, func(n *gotreesitter.Node) {
		nodeType := n.Type(lang)
		switch nodeType {
		case "function_declaration":
			var name string
			for i := 0; i < n.ChildCount(); i++ {
				child := n.Child(i)
				if child.Type(lang) == "identifier" {
					name = child.Text(content)
					break
				}
			}
			if name != "" {
				exported := false
				if parent := n.Parent(); parent != nil {
					pType := parent.Type(lang)
					if pType == "export_statement" || pType == "export_specifier" {
						exported = true
					}
				}
				symbols = append(symbols, Symbol{
					Name:     name,
					Type:     "function",
					Line:     int(n.StartPoint().Row) + 1,
					Exported: exported,
				})
			}

		case "class_declaration":
			var name string
			for i := 0; i < n.ChildCount(); i++ {
				child := n.Child(i)
				if child.Type(lang) == "identifier" {
					name = child.Text(content)
					break
				}
			}
			if name != "" {
				exported := false
				if parent := n.Parent(); parent != nil {
					pType := parent.Type(lang)
					if pType == "export_statement" || pType == "export_specifier" {
						exported = true
					}
				}
				symbols = append(symbols, Symbol{
					Name:     name,
					Type:     "class",
					Line:     int(n.StartPoint().Row) + 1,
					Exported: exported,
				})
			}

		case "method_definition":
			var name string
			for i := 0; i < n.ChildCount(); i++ {
				child := n.Child(i)
				t := child.Type(lang)
				if t == "property_identifier" || t == "private_property_identifier" {
					name = child.Text(content)
					break
				}
			}
			if name != "" {
				symbols = append(symbols, Symbol{
					Name:     name,
					Type:     "function",
					Line:     int(n.StartPoint().Row) + 1,
					Exported: false,
				})
			}

		case "import_statement":
			for i := 0; i < n.ChildCount(); i++ {
				child := n.Child(i)
				if child.Type(lang) == "string" {
					path := child.Text(content)
					path = strings.Trim(path, `"'`)
					if path != "" {
						imports = append(imports, path)
					}
				}
			}

		case "call_expression":
			var isRequire bool
			var path string
			for i := 0; i < n.ChildCount(); i++ {
				child := n.Child(i)
				t := child.Type(lang)
				if t == "identifier" && child.Text(content) == "require" {
					isRequire = true
				} else if t == "arguments" {
					for j := 0; j < child.ChildCount(); j++ {
						arg := child.Child(j)
						if arg.Type(lang) == "string" {
							path = strings.Trim(arg.Text(content), `"'`)
						}
					}
				}
			}
			if isRequire && path != "" {
				imports = append(imports, path)
			}
		}
	})

	return symbols, imports
}

func parsePhp(root *gotreesitter.Node, lang *gotreesitter.Language, content []byte) ([]Symbol, []string) {
	var symbols []Symbol
	var imports []string

	traverse(root, func(n *gotreesitter.Node) {
		nodeType := n.Type(lang)
		switch nodeType {
		case "function_definition":
			var name string
			for i := 0; i < n.ChildCount(); i++ {
				child := n.Child(i)
				if child.Type(lang) == "name" || child.Type(lang) == "identifier" {
					name = child.Text(content)
					break
				}
			}
			if name != "" {
				symbols = append(symbols, Symbol{
					Name:     name,
					Type:     "function",
					Line:     int(n.StartPoint().Row) + 1,
					Exported: true,
				})
			}

		case "class_declaration":
			var name string
			for i := 0; i < n.ChildCount(); i++ {
				child := n.Child(i)
				if child.Type(lang) == "name" || child.Type(lang) == "identifier" {
					name = child.Text(content)
					break
				}
			}
			if name != "" {
				symbols = append(symbols, Symbol{
					Name:     name,
					Type:     "class",
					Line:     int(n.StartPoint().Row) + 1,
					Exported: true,
				})
			}

		case "include_expression", "require_expression", "include_once_expression", "require_once_expression":
			for i := 0; i < n.ChildCount(); i++ {
				child := n.Child(i)
				if child.Type(lang) == "string" {
					path := strings.Trim(child.Text(content), `"'`)
					if path != "" {
						imports = append(imports, path)
					}
				}
			}

		case "namespace_use_declaration":
			for i := 0; i < n.ChildCount(); i++ {
				child := n.Child(i)
				if child.Type(lang) == "namespace_use_clause" {
					for j := 0; j < child.ChildCount(); j++ {
						clauseChild := child.Child(j)
						if clauseChild.Type(lang) == "name" || clauseChild.Type(lang) == "qualified_name" {
							imports = append(imports, clauseChild.Text(content))
						}
					}
				}
			}
		}
	})

	return symbols, imports
}

func parseRust(root *gotreesitter.Node, lang *gotreesitter.Language, content []byte) ([]Symbol, []string) {
	var symbols []Symbol
	var imports []string

	traverse(root, func(n *gotreesitter.Node) {
		nodeType := n.Type(lang)
		switch nodeType {
		case "function_item":
			var name string
			for i := 0; i < n.ChildCount(); i++ {
				child := n.Child(i)
				if child.Type(lang) == "identifier" {
					name = child.Text(content)
					break
				}
			}
			if name != "" {
				symbols = append(symbols, Symbol{
					Name:     name,
					Type:     "function",
					Line:     int(n.StartPoint().Row) + 1,
					Exported: true,
				})
			}

		case "struct_item", "enum_item", "trait_item":
			var name string
			for i := 0; i < n.ChildCount(); i++ {
				child := n.Child(i)
				if child.Type(lang) == "type_identifier" {
					name = child.Text(content)
					break
				}
			}
			if name != "" {
				symbols = append(symbols, Symbol{
					Name:     name,
					Type:     "class",
					Line:     int(n.StartPoint().Row) + 1,
					Exported: true,
				})
			}

		case "use_declaration":
			for i := 0; i < n.ChildCount(); i++ {
				child := n.Child(i)
				if child.Type(lang) == "use_path" || child.Type(lang) == "scoped_identifier" {
					imports = append(imports, child.Text(content))
				}
			}
		}
	})

	return symbols, imports
}

func parseRuby(root *gotreesitter.Node, lang *gotreesitter.Language, content []byte) ([]Symbol, []string) {
	var symbols []Symbol
	var imports []string

	traverse(root, func(n *gotreesitter.Node) {
		nodeType := n.Type(lang)
		switch nodeType {
		case "method":
			var name string
			for i := 0; i < n.ChildCount(); i++ {
				child := n.Child(i)
				if child.Type(lang) == "identifier" {
					name = child.Text(content)
					break
				}
			}
			if name != "" {
				symbols = append(symbols, Symbol{
					Name:     name,
					Type:     "function",
					Line:     int(n.StartPoint().Row) + 1,
					Exported: true,
				})
			}

		case "class", "module":
			var name string
			for i := 0; i < n.ChildCount(); i++ {
				child := n.Child(i)
				t := child.Type(lang)
				if t == "constant" || t == "identifier" {
					name = child.Text(content)
					break
				}
			}
			if name != "" {
				symbols = append(symbols, Symbol{
					Name:     name,
					Type:     "class",
					Line:     int(n.StartPoint().Row) + 1,
					Exported: true,
				})
			}

		case "call":
			var isRequire bool
			var path string
			for i := 0; i < n.ChildCount(); i++ {
				child := n.Child(i)
				t := child.Type(lang)
				if t == "identifier" && (child.Text(content) == "require" || child.Text(content) == "require_relative") {
					isRequire = true
				} else if t == "argument_list" {
					for j := 0; j < child.ChildCount(); j++ {
						arg := child.Child(j)
						if arg.Type(lang) == "string" {
							path = strings.Trim(arg.Text(content), `"'`)
						}
					}
				}
			}
			if isRequire && path != "" {
				imports = append(imports, path)
			}
		}
	})

	return symbols, imports
}

func parseJava(root *gotreesitter.Node, lang *gotreesitter.Language, content []byte) ([]Symbol, []string) {
	var symbols []Symbol
	var imports []string

	traverse(root, func(n *gotreesitter.Node) {
		nodeType := n.Type(lang)
		switch nodeType {
		case "method_declaration":
			var name string
			for i := 0; i < n.ChildCount(); i++ {
				child := n.Child(i)
				if child.Type(lang) == "identifier" {
					name = child.Text(content)
					break
				}
			}
			if name != "" {
				symbols = append(symbols, Symbol{
					Name:     name,
					Type:     "function",
					Line:     int(n.StartPoint().Row) + 1,
					Exported: true,
				})
			}

		case "class_declaration", "interface_declaration", "record_declaration", "enum_declaration":
			var name string
			for i := 0; i < n.ChildCount(); i++ {
				child := n.Child(i)
				if child.Type(lang) == "identifier" {
					name = child.Text(content)
					break
				}
			}
			if name != "" {
				symbols = append(symbols, Symbol{
					Name:     name,
					Type:     "class",
					Line:     int(n.StartPoint().Row) + 1,
					Exported: true,
				})
			}

		case "import_declaration":
			for i := 0; i < n.ChildCount(); i++ {
				child := n.Child(i)
				if child.Type(lang) == "scoped_identifier" || child.Type(lang) == "identifier" {
					imports = append(imports, child.Text(content))
				}
			}
		}
	})

	return symbols, imports
}

func parseHtml(root *gotreesitter.Node, lang *gotreesitter.Language, content []byte) ([]Symbol, []string) {
	var symbols []Symbol
	var imports []string

	traverse(root, func(n *gotreesitter.Node) {
		nodeType := n.Type(lang)
		if nodeType == "attribute" {
			var isImportAttr bool
			var val string
			for i := 0; i < n.ChildCount(); i++ {
				child := n.Child(i)
				t := child.Type(lang)
				if t == "attribute_name" && (child.Text(content) == "src" || child.Text(content) == "href") {
					isImportAttr = true
				} else if t == "attribute_value" {
					val = strings.Trim(child.Text(content), `"'`)
				}
			}
			if isImportAttr && val != "" {
				imports = append(imports, val)
			}
		}
	})

	return symbols, imports
}

func parseCss(root *gotreesitter.Node, lang *gotreesitter.Language, content []byte) ([]Symbol, []string) {
	var symbols []Symbol
	var imports []string

	traverse(root, func(n *gotreesitter.Node) {
		nodeType := n.Type(lang)
		switch nodeType {
		case "import_statement":
			for i := 0; i < n.ChildCount(); i++ {
				child := n.Child(i)
				switch t := child.Type(lang); t {
				case "string_value", "string":
					path := strings.Trim(child.Text(content), `"'()`)
					if path != "" {
						imports = append(imports, path)
					}
				case "call_expression":
					for j := 0; j < child.ChildCount(); j++ {
						arg := child.Child(j)
						if arg.Type(lang) == "arguments" {
							for k := 0; k < arg.ChildCount(); k++ {
								param := arg.Child(k)
								if param.Type(lang) == "string_value" || param.Type(lang) == "string" {
									path := strings.Trim(param.Text(content), `"'()`)
									if path != "" {
										imports = append(imports, path)
									}
								}
							}
						}
					}
				}
			}

		case "class_selector":
			// We can capture class names in CSS as class symbols!
			name := strings.TrimPrefix(n.Text(content), ".")
			if name != "" {
				symbols = append(symbols, Symbol{
					Name:     name,
					Type:     "class",
					Line:     int(n.StartPoint().Row) + 1,
					Exported: true,
				})
			}
		}
	})

	return symbols, imports
}
