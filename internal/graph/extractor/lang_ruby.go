package extractor

import (
	"strings"

	"github.com/odvcencio/gotreesitter"
)

func parseRuby(root *gotreesitter.Node, lang *gotreesitter.Language, content []byte) ([]Symbol, []string) {
	var symbols []Symbol
	var imports []string

	traverse(root, func(n *gotreesitter.Node) {
		switch n.Type(lang) {
		case "method":
			if name, ok := firstChildText(n, lang, content, "identifier"); ok {
				symbols = append(symbols, Symbol{Name: name, Type: "function", Line: nodeLine(n), Exported: true})
			}
		case "class", "module":
			if name, ok := firstChildText(n, lang, content, "constant", "identifier"); ok {
				symbols = append(symbols, Symbol{Name: name, Type: "class", Line: nodeLine(n), Exported: true})
			}
		case "call":
			if p, ok := rubyRequirePath(n, lang, content); ok {
				imports = append(imports, p)
			}
		}
	})

	return symbols, imports
}

// rubyRequirePath returns the path of a require/require_relative "..." call.
func rubyRequirePath(n *gotreesitter.Node, lang *gotreesitter.Language, content []byte) (string, bool) {
	var (
		isRequire bool
		path      string
	)
	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		switch child.Type(lang) {
		case "identifier":
			if t := child.Text(content); t == "require" || t == "require_relative" {
				isRequire = true
			}
		case "argument_list":
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
