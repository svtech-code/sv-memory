package extractor

import (
	"strings"

	"github.com/odvcencio/gotreesitter"
)

func parseGo(root *gotreesitter.Node, lang *gotreesitter.Language, content []byte) ([]Symbol, []string) {
	var symbols []Symbol
	var imports []string

	traverse(root, func(n *gotreesitter.Node) {
		switch n.Type(lang) {
		case "function_declaration":
			if name, ok := firstChildText(n, lang, content, "identifier"); ok && name != "init" && name != "main" {
				symbols = append(symbols, Symbol{Name: name, Type: "function", Line: nodeLine(n), Exported: isExportedName(name)})
			}
		case "method_declaration":
			if name, ok := lastChildText(n, lang, content, "field_identifier", "identifier"); ok {
				symbols = append(symbols, Symbol{Name: name, Type: "function", Line: nodeLine(n), Exported: isExportedName(name)})
			}
		case "type_spec":
			if name, ok := goTypeSpecName(n, lang, content); ok {
				symbols = append(symbols, Symbol{Name: name, Type: "class", Line: nodeLine(n), Exported: isExportedName(name)})
			}
		case "import_spec":
			imports = append(imports, goImportPaths(n, lang, content)...)
		}
	})

	return symbols, imports
}

// goTypeSpecName returns the type name only when the type spec declares a
// struct or interface (mirrors the historical class/struct filter).
func goTypeSpecName(n *gotreesitter.Node, lang *gotreesitter.Language, content []byte) (string, bool) {
	name, ok := firstChildText(n, lang, content, "type_identifier")
	if !ok || !hasChildType(n, lang, "struct_type", "interface_type") {
		return "", false
	}
	return name, true
}

func goImportPaths(n *gotreesitter.Node, lang *gotreesitter.Language, content []byte) []string {
	raw := collectChildTexts(n, lang, content, "import_path", "string_literal", "interpreted_string_literal", "raw_string_literal")
	var out []string
	for _, p := range raw {
		if p = strings.Trim(p, "\"`"); p != "" {
			out = append(out, p)
		}
	}
	return out
}
