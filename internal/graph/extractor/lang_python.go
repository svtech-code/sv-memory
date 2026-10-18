package extractor

import "github.com/odvcencio/gotreesitter"

func parsePython(root *gotreesitter.Node, lang *gotreesitter.Language, content []byte) ([]Symbol, []string) {
	var symbols []Symbol
	var imports []string

	traverse(root, func(n *gotreesitter.Node) {
		switch n.Type(lang) {
		case "function_definition":
			if name, ok := firstChildText(n, lang, content, "identifier"); ok {
				symbols = append(symbols, Symbol{Name: name, Type: "function", Line: nodeLine(n), Exported: true})
			}
		case "class_definition":
			if name, ok := firstChildText(n, lang, content, "identifier"); ok {
				symbols = append(symbols, Symbol{Name: name, Type: "class", Line: nodeLine(n), Exported: true})
			}
		case "import_statement":
			imports = append(imports, pyImportNames(n, lang, content)...)
		case "import_from_statement":
			if name, ok := firstChildText(n, lang, content, "dotted_name", "relative_import"); ok {
				imports = append(imports, name)
			}
		}
	})

	return symbols, imports
}

func pyImportNames(n *gotreesitter.Node, lang *gotreesitter.Language, content []byte) []string {
	var out []string
	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		switch child.Type(lang) {
		case "dotted_name":
			out = append(out, child.Text(content))
		case "aliased_import":
			if name, ok := firstChildText(child, lang, content, "dotted_name"); ok {
				out = append(out, name)
			}
		}
	}
	return out
}
