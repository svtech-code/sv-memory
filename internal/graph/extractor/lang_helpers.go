package extractor

import "github.com/odvcencio/gotreesitter"

// nodeLine returns the 1-based line of a node's start position.
func nodeLine(n *gotreesitter.Node) int {
	return int(n.StartPoint().Row) + 1
}

// isExportedName reports whether a Go identifier is exported (upper-case first
// letter).
func isExportedName(name string) bool {
	return name != "" && name[0] >= 'A' && name[0] <= 'Z'
}

// firstChildText returns the text of the first direct child whose type is one
// of the given types.
func firstChildText(n *gotreesitter.Node, lang *gotreesitter.Language, content []byte, types ...string) (string, bool) {
	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		if childHasType(child, lang, types) {
			return child.Text(content), true
		}
	}
	return "", false
}

// lastChildText returns the text of the last direct child whose type is one of
// the given types (used where the trailing match wins, e.g. Go method names).
func lastChildText(n *gotreesitter.Node, lang *gotreesitter.Language, content []byte, types ...string) (string, bool) {
	var (
		text  string
		found bool
	)
	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		if childHasType(child, lang, types) {
			text, found = child.Text(content), true
		}
	}
	return text, found
}

// collectChildTexts returns the text of every direct child whose type is one of
// the given types, preserving document order.
func collectChildTexts(n *gotreesitter.Node, lang *gotreesitter.Language, content []byte, types ...string) []string {
	var out []string
	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		if childHasType(child, lang, types) {
			out = append(out, child.Text(content))
		}
	}
	return out
}

// hasChildType reports whether any direct child has one of the given types.
func hasChildType(n *gotreesitter.Node, lang *gotreesitter.Language, types ...string) bool {
	for i := 0; i < n.ChildCount(); i++ {
		if childHasType(n.Child(i), lang, types) {
			return true
		}
	}
	return false
}

func childHasType(child *gotreesitter.Node, lang *gotreesitter.Language, types []string) bool {
	ct := child.Type(lang)
	for _, want := range types {
		if ct == want {
			return true
		}
	}
	return false
}
