package extractor

import (
	"regexp"
	"strings"
)

// parseRegexImports parses module imports for the given extension using the
// legacy regex patterns.
func parseRegexImports(content []byte, ext string) []string {
	switch ext {
	case ".js", ".ts", ".jsx", ".tsx", ".astro", ".vue", ".svelte":
		return importFirstGroup(content, jsImportRegex, 1, 2)
	case ".py":
		return pythonImports(content)
	case ".go":
		return goImports(content)
	case ".php":
		return importFirstGroup(content, phpImportRegex, 1, 2)
	case ".rb":
		return importFirstGroup(content, rbImportRegex, 1)
	case ".rs":
		return importFirstGroup(content, rsImportRegex, 1)
	case ".java":
		return importFirstGroup(content, javaImportRegex, 1)
	case ".css":
		return importFirstGroup(content, cssImportRegex, 1)
	case ".html":
		return importFirstGroup(content, htmlImportRegex, 1, 2)
	case ".sh":
		return importFirstGroup(content, shImportRegex, 1)
	case ".lua":
		return importFirstGroup(content, luaImportRegex, 1)
	case ".md":
		return markdownImports(content)
	case ".sql":
		return sqlForeignKeyRefs(content)
	}
	return nil
}

// importFirstGroup appends the first non-empty capture group per match, using
// the given group priorities (mirrors the `else if` chains of the original).
func importFirstGroup(content []byte, re *regexp.Regexp, groups ...int) []string {
	var out []string
	for _, m := range re.FindAllSubmatch(content, -1) {
		if v := firstNonEmptyGroup(m, groups...); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func firstNonEmptyGroup(m [][]byte, groups ...int) string {
	for _, g := range groups {
		if g < len(m) && len(m[g]) > 0 {
			return string(m[g])
		}
	}
	return ""
}

func pythonImports(content []byte) []string {
	var out []string
	for _, m := range pyImportRegex.FindAllSubmatch(content, -1) {
		importStr := firstNonEmptyGroup(m, 1, 3, 4)
		if importStr == "" {
			continue
		}
		for _, p := range strings.Split(importStr, ",") {
			out = append(out, strings.TrimSpace(p))
		}
	}
	return out
}

func goImports(content []byte) []string {
	var out []string
	for _, m := range goImportRegex.FindAllSubmatch(content, -1) {
		if len(m) > 1 && len(m[1]) > 0 {
			out = append(out, string(m[1]))
		}
		if len(m) > 2 && len(m[2]) > 0 {
			for _, bm := range goImportBlockRegex.FindAllSubmatch(m[2], -1) {
				if len(bm) > 1 && len(bm[1]) > 0 {
					out = append(out, string(bm[1]))
				}
			}
		}
	}
	return out
}

func markdownImports(content []byte) []string {
	var out []string
	for _, re := range []*regexp.Regexp{mdLinkRegex, mdWikilinkRegex} {
		out = append(out, importFirstGroup(content, re, 1)...)
	}
	return out
}
