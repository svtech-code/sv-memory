package extractor

import "github.com/odvcencio/gotreesitter"

func parseJava(root *gotreesitter.Node, lang *gotreesitter.Language, content []byte) ([]Symbol, []string) {
	var symbols []Symbol
	var imports []string

	traverse(root, func(n *gotreesitter.Node) {
		switch n.Type(lang) {
		case "method_declaration":
			if name, ok := firstChildText(n, lang, content, "identifier"); ok {
				symbols = append(symbols, Symbol{Name: name, Type: "function", Line: nodeLine(n), Exported: true})
			}
		case "class_declaration", "interface_declaration", "record_declaration", "enum_declaration":
			if name, ok := firstChildText(n, lang, content, "identifier"); ok {
				symbols = append(symbols, Symbol{Name: name, Type: "class", Line: nodeLine(n), Exported: true})
			}
		case "import_declaration":
			imports = append(imports, collectChildTexts(n, lang, content, "scoped_identifier", "identifier")...)
		}
	})

	return symbols, imports
}
