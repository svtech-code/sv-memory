package graph

import (
	"path/filepath"
	"strings"
)

func tokenizeText(text string) map[string]int {
	words := make(map[string]int)
	var current strings.Builder
	for lineIdx, line := range strings.Split(text, "\n") {
		for _, r := range line {
			if isWordRune(r) {
				current.WriteRune(r)
				continue
			}
			flushWord(words, &current, lineIdx+1)
		}
		flushWord(words, &current, lineIdx+1)
	}
	return words
}

func isWordRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
}

// flushWord records the current token against its 1-based line (keeping the
// first occurrence) and resets the builder.
func flushWord(words map[string]int, current *strings.Builder, line int) {
	if current.Len() == 0 {
		return
	}
	w := current.String()
	if _, exists := words[w]; !exists {
		words[w] = line
	}
	current.Reset()
}

// resolveMarkdownLink resolves a markdown link or wikilink to a node in the graph,
// supporting relative paths, project absolute paths, and fuzzy base name matches.
func resolveMarkdownLink(projPath, sourcePath, target string, nodes map[string]*Node) (string, bool) {
	target = stripLinkFragment(target)
	if target == "" || isExternalLink(target) {
		return "", false
	}

	if id, ok := matchNodePath(relMarkdownPath(sourcePath, target), nodes); ok {
		return id, true
	}
	if id, ok := matchNodePath(cleanMarkdownPath(target), nodes); ok {
		return id, true
	}
	return matchNodeByBaseName(filepath.Base(target), nodes)
}

// stripLinkFragment removes any #anchor or ?query suffix and trims whitespace.
func stripLinkFragment(target string) string {
	if idx := strings.IndexAny(target, "#?"); idx != -1 {
		target = target[:idx]
	}
	return strings.TrimSpace(target)
}

func isExternalLink(target string) bool {
	return strings.HasPrefix(target, "http://") ||
		strings.HasPrefix(target, "https://") ||
		strings.HasPrefix(target, "mailto:")
}

// relMarkdownPath joins a link target to its source directory and normalizes it.
func relMarkdownPath(sourcePath, target string) string {
	rel := filepath.Clean(filepath.Join(filepath.Dir(sourcePath), target))
	rel = strings.ReplaceAll(rel, "\\", "/")
	return strings.TrimPrefix(rel, "./")
}

// cleanMarkdownPath normalizes a project-absolute link target.
func cleanMarkdownPath(target string) string {
	clean := filepath.Clean(target)
	clean = strings.ReplaceAll(clean, "\\", "/")
	return strings.TrimPrefix(clean, "./")
}

// matchNodePath returns the node id for a path, or its .md-suffixed variant when
// the path carries no extension.
func matchNodePath(p string, nodes map[string]*Node) (string, bool) {
	if _, exists := nodes[p]; exists {
		return p, true
	}
	if filepath.Ext(p) == "" {
		if _, exists := nodes[p+".md"]; exists {
			return p + ".md", true
		}
	}
	return "", false
}

// matchNodeByBaseName finds a file/document node whose base name matches
// targetBase case-insensitively, tolerating a missing .md extension.
func matchNodeByBaseName(targetBase string, nodes map[string]*Node) (string, bool) {
	noExt := filepath.Ext(targetBase) == ""
	for id, node := range nodes {
		if node.Type != "file" && node.Type != "document" {
			continue
		}
		nodeBase := filepath.Base(node.Path)
		if strings.EqualFold(nodeBase, targetBase) {
			return id, true
		}
		if noExt && strings.EqualFold(nodeBase, targetBase+".md") {
			return id, true
		}
	}
	return "", false
}
