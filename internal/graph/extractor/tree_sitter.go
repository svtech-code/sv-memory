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
	case ".js", ".jsx", ".ts", ".tsx", ".astro":
		symbols, imports = parseJavascript(root, lang, content)
	case ".php":
		symbols, imports = parsePhp(root, lang, content)
	case ".rs":
		symbols, imports = parseRust(root, lang, content)
	case ".rb":
		symbols, imports = parseRuby(root, lang, content)
	case ".java":
		symbols, imports = parseJava(root, lang, content)
	default:
		return t.regexFallback.Extract(content, relPath, ext)
	}

	return symbols, imports, nil
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
				t := child.Type(lang)
				if t == "type_identifier" {
					name = child.Text(content)
				} else if t == "struct_type" || t == "interface_type" {
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
