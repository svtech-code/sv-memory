package extractor

import (
	"strings"

	"github.com/odvcencio/gotreesitter"
)

func parseHtml(root *gotreesitter.Node, lang *gotreesitter.Language, content []byte) ([]Symbol, []string) {
	var symbols []Symbol
	var imports []string

	traverse(root, func(n *gotreesitter.Node) {
		if n.Type(lang) != "attribute" {
			return
		}
		if val, ok := htmlImportValue(n, lang, content); ok {
			imports = append(imports, val)
		}
	})

	return symbols, imports
}

// htmlImportValue returns the value of an src/href HTML attribute.
func htmlImportValue(n *gotreesitter.Node, lang *gotreesitter.Language, content []byte) (string, bool) {
	var (
		isImport bool
		val      string
	)
	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		switch child.Type(lang) {
		case "attribute_name":
			if t := child.Text(content); t == "src" || t == "href" {
				isImport = true
			}
		case "attribute_value":
			val = strings.Trim(child.Text(content), "\"'")
		}
	}
	if isImport && val != "" {
		return val, true
	}
	return "", false
}

func parseCss(root *gotreesitter.Node, lang *gotreesitter.Language, content []byte) ([]Symbol, []string) {
	var symbols []Symbol
	var imports []string

	traverse(root, func(n *gotreesitter.Node) {
		switch n.Type(lang) {
		case "import_statement":
			imports = append(imports, cssImportPaths(n, lang, content)...)
		case "class_selector":
			if name := strings.TrimPrefix(n.Text(content), "."); name != "" {
				symbols = append(symbols, Symbol{Name: name, Type: "class", Line: nodeLine(n), Exported: true})
			}
		}
	})

	return symbols, imports
}

func cssImportPaths(n *gotreesitter.Node, lang *gotreesitter.Language, content []byte) []string {
	var out []string
	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		switch child.Type(lang) {
		case "string_value", "string":
			if p := strings.Trim(child.Text(content), "\"'()"); p != "" {
				out = append(out, p)
			}
		case "call_expression":
			out = append(out, cssCallArgs(child, lang, content)...)
		}
	}
	return out
}

// cssCallArgs returns string arguments of a css @import url(...) call.
func cssCallArgs(call *gotreesitter.Node, lang *gotreesitter.Language, content []byte) []string {
	var out []string
	for i := 0; i < call.ChildCount(); i++ {
		arg := call.Child(i)
		if arg.Type(lang) != "arguments" {
			continue
		}
		for k := 0; k < arg.ChildCount(); k++ {
			param := arg.Child(k)
			if t := param.Type(lang); t == "string_value" || t == "string" {
				if p := strings.Trim(param.Text(content), "\"'()"); p != "" {
					out = append(out, p)
				}
			}
		}
	}
	return out
}
