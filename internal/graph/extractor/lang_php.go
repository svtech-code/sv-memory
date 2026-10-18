package extractor

import (
	"strings"

	"github.com/odvcencio/gotreesitter"
)

func parsePhp(root *gotreesitter.Node, lang *gotreesitter.Language, content []byte) ([]Symbol, []string) {
	var symbols []Symbol
	var imports []string

	traverse(root, func(n *gotreesitter.Node) {
		switch n.Type(lang) {
		case "function_definition":
			if name, ok := firstChildText(n, lang, content, "name", "identifier"); ok {
				symbols = append(symbols, Symbol{Name: name, Type: "function", Line: nodeLine(n), Exported: true})
			}
		case "class_declaration":
			if name, ok := firstChildText(n, lang, content, "name", "identifier"); ok {
				symbols = append(symbols, Symbol{Name: name, Type: "class", Line: nodeLine(n), Exported: true})
			}
		case "include_expression", "require_expression", "include_once_expression", "require_once_expression":
			for _, p := range collectChildTexts(n, lang, content, "string") {
				if p = strings.Trim(p, "\"'"); p != "" {
					imports = append(imports, p)
				}
			}
		case "namespace_use_declaration":
			imports = append(imports, phpUseNames(n, lang, content)...)
		}
	})

	return symbols, imports
}

func phpUseNames(n *gotreesitter.Node, lang *gotreesitter.Language, content []byte) []string {
	var out []string
	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		if child.Type(lang) != "namespace_use_clause" {
			continue
		}
		out = append(out, collectChildTexts(child, lang, content, "name", "qualified_name")...)
	}
	return out
}
