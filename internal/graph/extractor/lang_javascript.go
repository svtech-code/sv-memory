package extractor

import (
	"strings"

	"github.com/odvcencio/gotreesitter"
)

func parseJavascript(root *gotreesitter.Node, lang *gotreesitter.Language, content []byte) ([]Symbol, []string) {
	var symbols []Symbol
	var imports []string

	traverse(root, func(n *gotreesitter.Node) {
		switch n.Type(lang) {
		case "function_declaration":
			if name, ok := firstChildText(n, lang, content, "identifier"); ok {
				symbols = append(symbols, Symbol{Name: name, Type: "function", Line: nodeLine(n), Exported: jsIsExported(n, lang)})
			}
		case "class_declaration":
			if name, ok := firstChildText(n, lang, content, "identifier"); ok {
				symbols = append(symbols, Symbol{Name: name, Type: "class", Line: nodeLine(n), Exported: jsIsExported(n, lang)})
			}
		case "method_definition":
			if name, ok := firstChildText(n, lang, content, "property_identifier", "private_property_identifier"); ok {
				symbols = append(symbols, Symbol{Name: name, Type: "function", Line: nodeLine(n), Exported: false})
			}
		case "import_statement":
			for _, p := range collectChildTexts(n, lang, content, "string") {
				if p = strings.Trim(p, "\"'"); p != "" {
					imports = append(imports, p)
				}
			}
		case "call_expression":
			if p, ok := jsRequirePath(n, lang, content); ok {
				imports = append(imports, p)
			}
		}
	})

	return symbols, imports
}

// jsIsExported reports whether a declaration node is wrapped in an export
// statement.
func jsIsExported(n *gotreesitter.Node, lang *gotreesitter.Language) bool {
	parent := n.Parent()
	if parent == nil {
		return false
	}
	switch parent.Type(lang) {
	case "export_statement", "export_specifier":
		return true
	}
	return false
}

// jsRequirePath returns the module path of a CommonJS require("...") call.
func jsRequirePath(n *gotreesitter.Node, lang *gotreesitter.Language, content []byte) (string, bool) {
	var (
		isRequire bool
		path      string
	)
	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		switch child.Type(lang) {
		case "identifier":
			if child.Text(content) == "require" {
				isRequire = true
			}
		case "arguments":
			if p, ok := firstChildText(child, lang, content, "string"); ok {
				path = strings.Trim(p, "\"'")
			}
		}
	}
	if isRequire && path != "" {
		return path, true
	}
	return "", false
}
