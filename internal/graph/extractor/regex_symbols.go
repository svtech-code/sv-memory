package extractor

import "strings"

// parseRegexSymbols parses symbol declarations for the given extension using the
// legacy regex patterns.
func parseRegexSymbols(content []byte, lines []string, ext string) []Symbol {
	switch ext {
	case ".js", ".jsx", ".ts", ".tsx", ".astro":
		return regexJSSymbols(content, lines)
	case ".py":
		return regexPySymbols(content, lines)
	case ".go":
		return regexGoSymbols(content, lines)
	case ".php":
		return regexPhpSymbols(content, lines)
	case ".md":
		return extractMarkdownSymbols(lines)
	case ".sql":
		syms, _ := extractSQLSymbols(lines)
		return syms
	}
	return nil
}

func regexJSSymbols(content []byte, lines []string) []Symbol {
	var out []Symbol
	for _, m := range jsFuncRegex.FindAllSubmatch(content, -1) {
		if len(m) > 1 {
			out = append(out, mkSymbol(string(m[1]), "function", strings.HasPrefix(string(m[0]), "export"), findLineNumberInLines(lines, string(m[0]))))
		}
	}
	for _, m := range jsClassRegex.FindAllSubmatch(content, -1) {
		if len(m) > 1 {
			out = append(out, mkSymbol(string(m[1]), "class", strings.HasPrefix(string(m[0]), "export"), findLineNumberInLines(lines, string(m[0]))))
		}
	}
	return out
}

func regexPySymbols(content []byte, lines []string) []Symbol {
	var out []Symbol
	for _, m := range pyFuncRegex.FindAllSubmatch(content, -1) {
		if len(m) > 1 {
			out = append(out, mkSymbol(string(m[1]), "function", true, findLineNumberInLines(lines, string(m[0]))))
		}
	}
	for _, m := range pyClassRegex.FindAllSubmatch(content, -1) {
		if len(m) > 1 {
			out = append(out, mkSymbol(string(m[1]), "class", true, findLineNumberInLines(lines, string(m[0]))))
		}
	}
	return out
}

func regexGoSymbols(content []byte, lines []string) []Symbol {
	var out []Symbol
	for _, m := range goFuncRegex.FindAllSubmatch(content, -1) {
		if len(m) <= 1 {
			continue
		}
		name := string(m[1])
		if name == "init" || name == "main" {
			continue
		}
		out = append(out, mkSymbol(name, "function", isExportedName(name), findLineNumberInLines(lines, string(m[0]))))
	}
	for _, m := range goStructRegex.FindAllSubmatch(content, -1) {
		if len(m) > 1 {
			name := string(m[1])
			out = append(out, mkSymbol(name, "class", isExportedName(name), findLineNumberInLines(lines, string(m[0]))))
		}
	}
	return out
}

func regexPhpSymbols(content []byte, lines []string) []Symbol {
	var out []Symbol
	for _, m := range phpFuncRegex.FindAllSubmatch(content, -1) {
		for i := 1; i < len(m); i++ {
			if len(m[i]) == 0 {
				continue
			}
			symType := "function"
			if i == 2 {
				symType = "class"
			}
			out = append(out, mkSymbol(string(m[i]), symType, true, findLineNumberInLines(lines, string(m[0]))))
		}
	}
	return out
}
