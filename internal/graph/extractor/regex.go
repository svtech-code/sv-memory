package extractor

import (
	"regexp"
	"strings"
)

// Helper regexes for legacy fallback extraction.
var (
	jsFuncRegex   = regexp.MustCompile(`(?m)(?:export\s+)?(?:\basync\s+)?\bfunction\s+(\w+)`)
	jsClassRegex  = regexp.MustCompile(`(?m)(?:export\s+)?\bclass\s+(\w+)`)
	jsExportRegex = regexp.MustCompile(`(?m)(?:export\s+(?:default\s+)?(?:function|class|const|let|var)\s+\w+|module\.exports\s*=|exports\.\w+)`)

	pyFuncRegex  = regexp.MustCompile(`(?m)^\s*(?:async\s+)?def\s+(\w+)`)
	pyClassRegex = regexp.MustCompile(`(?m)^\s*class\s+(\w+)`)

	goFuncRegex   = regexp.MustCompile(`(?m)^\s*func\s+(\w+)`)
	goStructRegex = regexp.MustCompile(`(?m)^\s*type\s+(\w+)\s+struct`)

	phpFuncRegex = regexp.MustCompile(`(?m)(?:function\s+(\w+)|class\s+(\w+))`)

	// Import regex patterns
	jsImportRegex      = regexp.MustCompile(`(?m)(?:import\s+(?:[^'"]+\s+from\s+)?['"]([^'"]+)['"])|(?:require\s*\(\s*['"]([^'"]+)['"]\s*\))`)
	pyImportRegex      = regexp.MustCompile(`(?m)(?:import\s+([\w\.,\s]+)|from\s+([\w\.]+)\s+import\s*(?:\(([^)]+)\)|([\w\.,\s]+)))`)
	goImportRegex      = regexp.MustCompile("(?m)(?:import\\s+[\"`]([^\"`]+)[\"`])|(?:import\\s*\\(([\\s\\S]*?)\\))")
	goImportBlockRegex = regexp.MustCompile("(?m)[\"`]([^\"`]+)[\"`]")
	rbImportRegex      = regexp.MustCompile(`(?m)(?:require|require_relative)\s+['"]([^'"]+)['"]`)
	rsImportRegex      = regexp.MustCompile(`(?m)use\s+([\w\d:]+);`)
	javaImportRegex    = regexp.MustCompile(`(?m)import\s+([\w\.]+);`)
	phpImportRegex     = regexp.MustCompile(`(?m)(?:include|require)(?:_once)?\s*\(?\s*['"]([^'"]+)['"]\s*\)?|use\s+([\w\\]+)(?:\s+as\s+\w+)?;`)
	cssImportRegex     = regexp.MustCompile(`(?m)@import\s+(?:url\()?['"]([^'"]+)['"]`)
	htmlImportRegex    = regexp.MustCompile(`(?m)<script\s+[^>]*src=['"]([^'"]+)['"]|<link\s+[^>]*href=['"]([^'"]+)['"]`)
	mdLinkRegex        = regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)
	mdWikilinkRegex    = regexp.MustCompile(`\[\[([^\]|]+)(?:\|[^\]]*)?\]\]`)
	shImportRegex      = regexp.MustCompile(`(?m)^\s*(?:source|\.)\s+['"]?([^'"\s#;]+)['"]?`)
	luaImportRegex     = regexp.MustCompile(`(?m)(?:require|dofile|loadfile)\s*\(?\s*['"]([^'"]+)['"]\s*\)?`)
)

// RegexExtractor implements legacy regex-based extraction.
type RegexExtractor struct{}

// NewRegexExtractor creates a new RegexExtractor.
func NewRegexExtractor() *RegexExtractor {
	return &RegexExtractor{}
}

// Extract parses the file content using regex patterns to find symbols and imports.
func (r *RegexExtractor) Extract(content []byte, relPath, ext string) ([]Symbol, []string, error) {
	lines := strings.Split(string(content), "\n")
	var symbols []Symbol
	var imports []string

	addSymbol := func(name, symType string, exported bool, line int) {
		symbols = append(symbols, Symbol{
			Name:     name,
			Type:     symType,
			Line:     line,
			Exported: exported,
		})
	}

	// 1. Symbol parsing
	switch ext {
	case ".js", ".jsx", ".ts", ".tsx", ".astro":
		// Functions
		for _, m := range jsFuncRegex.FindAllSubmatch(content, -1) {
			if len(m) > 1 {
				name := string(m[1])
				exported := strings.HasPrefix(string(m[0]), "export")
				line := r.findLineNumber(lines, string(m[0]))
				addSymbol(name, "function", exported, line)
			}
		}
		// Classes
		for _, m := range jsClassRegex.FindAllSubmatch(content, -1) {
			if len(m) > 1 {
				name := string(m[1])
				exported := strings.HasPrefix(string(m[0]), "export")
				line := r.findLineNumber(lines, string(m[0]))
				addSymbol(name, "class", exported, line)
			}
		}
	case ".py":
		for _, m := range pyFuncRegex.FindAllSubmatch(content, -1) {
			if len(m) > 1 {
				name := string(m[1])
				line := r.findLineNumber(lines, string(m[0]))
				addSymbol(name, "function", true, line)
			}
		}
		for _, m := range pyClassRegex.FindAllSubmatch(content, -1) {
			if len(m) > 1 {
				name := string(m[1])
				line := r.findLineNumber(lines, string(m[0]))
				addSymbol(name, "class", true, line)
			}
		}
	case ".go":
		for _, m := range goFuncRegex.FindAllSubmatch(content, -1) {
			if len(m) > 1 {
				name := string(m[1])
				if name == "init" || name == "main" {
					continue
				}
				isExported := name[0] >= 'A' && name[0] <= 'Z'
				line := r.findLineNumber(lines, string(m[0]))
				addSymbol(name, "function", isExported, line)
			}
		}
		for _, m := range goStructRegex.FindAllSubmatch(content, -1) {
			if len(m) > 1 {
				name := string(m[1])
				isExported := name[0] >= 'A' && name[0] <= 'Z'
				line := r.findLineNumber(lines, string(m[0]))
				addSymbol(name, "class", isExported, line)
			}
		}
	case ".php":
		for _, m := range phpFuncRegex.FindAllSubmatch(content, -1) {
			if len(m) > 1 {
				for i := 1; i < len(m); i++ {
					if len(m[i]) > 0 {
						symType := "function"
						if i == 2 {
							symType = "class"
						}
						name := string(m[i])
						line := r.findLineNumber(lines, string(m[0]))
						addSymbol(name, symType, true, line)
					}
				}
			}
		}
	}

	// 2. Import parsing
	switch ext {
	case ".js", ".ts", ".jsx", ".tsx", ".astro", ".vue", ".svelte":
		matches := jsImportRegex.FindAllSubmatch(content, -1)
		for _, m := range matches {
			if len(m) > 1 && len(m[1]) > 0 {
				imports = append(imports, string(m[1]))
			} else if len(m) > 2 && len(m[2]) > 0 {
				imports = append(imports, string(m[2]))
			}
		}
	case ".py":
		matches := pyImportRegex.FindAllSubmatch(content, -1)
		for _, m := range matches {
			importStr := ""
			if len(m[1]) > 0 {
				importStr = string(m[1])
			} else if len(m[3]) > 0 {
				importStr = string(m[3])
			} else if len(m[4]) > 0 {
				importStr = string(m[4])
			}
			if importStr != "" {
				parts := strings.Split(importStr, ",")
				for _, p := range parts {
					imports = append(imports, strings.TrimSpace(p))
				}
			}
		}
	case ".go":
		matches := goImportRegex.FindAllSubmatch(content, -1)
		for _, m := range matches {
			if len(m) > 1 && len(m[1]) > 0 {
				imports = append(imports, string(m[1]))
			}
		}
		for _, m := range goImportBlockRegex.FindAllSubmatch(content, -1) {
			if len(m) > 1 && len(m[1]) > 0 {
				imports = append(imports, string(m[1]))
			}
		}
	case ".php":
		matches := phpImportRegex.FindAllSubmatch(content, -1)
		for _, m := range matches {
			if len(m) > 1 && len(m[1]) > 0 {
				imports = append(imports, string(m[1]))
			} else if len(m) > 2 && len(m[2]) > 0 {
				imports = append(imports, string(m[2]))
			}
		}
	case ".rb":
		matches := rbImportRegex.FindAllSubmatch(content, -1)
		for _, m := range matches {
			if len(m) > 1 && len(m[1]) > 0 {
				imports = append(imports, string(m[1]))
			}
		}
	case ".rs":
		matches := rsImportRegex.FindAllSubmatch(content, -1)
		for _, m := range matches {
			if len(m) > 1 && len(m[1]) > 0 {
				imports = append(imports, string(m[1]))
			}
		}
	case ".java":
		matches := javaImportRegex.FindAllSubmatch(content, -1)
		for _, m := range matches {
			if len(m) > 1 && len(m[1]) > 0 {
				imports = append(imports, string(m[1]))
			}
		}
	case ".css":
		matches := cssImportRegex.FindAllSubmatch(content, -1)
		for _, m := range matches {
			if len(m) > 1 {
				imports = append(imports, string(m[1]))
			}
		}
	case ".html":
		matches := htmlImportRegex.FindAllSubmatch(content, -1)
		for _, m := range matches {
			if len(m) > 1 && len(m[1]) > 0 {
				imports = append(imports, string(m[1]))
			} else if len(m) > 2 && len(m[2]) > 0 {
				imports = append(imports, string(m[2]))
			}
		}
	case ".sh":
		matches := shImportRegex.FindAllSubmatch(content, -1)
		for _, m := range matches {
			if len(m) > 1 && len(m[1]) > 0 {
				imports = append(imports, string(m[1]))
			}
		}
	case ".lua":
		matches := luaImportRegex.FindAllSubmatch(content, -1)
		for _, m := range matches {
			if len(m) > 1 && len(m[1]) > 0 {
				imports = append(imports, string(m[1]))
			}
		}
	case ".md":
		for _, m := range mdLinkRegex.FindAllSubmatch(content, -1) {
			if len(m) > 1 && len(m[1]) > 0 {
				imports = append(imports, string(m[1]))
			}
		}
		for _, m := range mdWikilinkRegex.FindAllSubmatch(content, -1) {
			if len(m) > 1 && len(m[1]) > 0 {
				imports = append(imports, string(m[1]))
			}
		}
	}

	return symbols, imports, nil
}

// GetExportsCount returns the number of JS exports in the file.
func (r *RegexExtractor) GetExportsCount(content []byte, ext string) int {
	switch ext {
	case ".js", ".jsx", ".ts", ".tsx", ".astro":
		return len(jsExportRegex.FindAll(content, -1))
	}
	return 0
}

func (r *RegexExtractor) findLineNumber(lines []string, substr string) int {
	substr = strings.TrimSpace(substr)
	for i, line := range lines {
		if strings.Contains(line, substr) {
			return i + 1
		}
	}
	return 0
}
