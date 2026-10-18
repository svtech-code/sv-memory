package extractor

import "github.com/odvcencio/gotreesitter"

func parseRust(root *gotreesitter.Node, lang *gotreesitter.Language, content []byte) ([]Symbol, []string) {
	var symbols []Symbol
	var imports []string

	traverse(root, func(n *gotreesitter.Node) {
		switch n.Type(lang) {
		case "function_item":
			if name, ok := firstChildText(n, lang, content, "identifier"); ok {
				symbols = append(symbols, Symbol{Name: name, Type: "function", Line: nodeLine(n), Exported: true})
			}
		case "struct_item", "enum_item", "trait_item":
			if name, ok := firstChildText(n, lang, content, "type_identifier"); ok {
				symbols = append(symbols, Symbol{Name: name, Type: "class", Line: nodeLine(n), Exported: true})
			}
		case "use_declaration":
			imports = append(imports, collectChildTexts(n, lang, content, "use_path", "scoped_identifier")...)
		}
	})

	return symbols, imports
}
