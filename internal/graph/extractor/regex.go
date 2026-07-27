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
	mdHeadingRegex     = regexp.MustCompile(`(?m)^(#{1,6})\s+(.+)$`)
	mdFenceOpenRegex   = regexp.MustCompile(`^` + "```" + `(\w*)`)
	mdFenceCloseRegex  = regexp.MustCompile(`^` + "```")
	shImportRegex      = regexp.MustCompile(`(?m)^\s*(?:source|\.)\s+['"]?([^'"\s#;]+)['"]?`)
	luaImportRegex     = regexp.MustCompile(`(?m)(?:require|dofile|loadfile)\s*\(?\s*['"]([^'"]+)['"]\s*\)?`)

	// SQL patterns
	sqlCreateTableRegex  = regexp.MustCompile(`(?mi)(?:CREATE\s+(?:TEMP(?:ORARY)?\s+)?TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:(\w+)\.)?(\w+)\s*\()`)
	sqlCreateViewRegex   = regexp.MustCompile(`(?mi)(?:CREATE\s+(?:OR\s+REPLACE\s+)?(?:TEMP(?:ORARY)?\s+)?VIEW\s+(?:(\w+)\.)?(\w+)\s+AS\s+)`)
	sqlAlterFKRegex      = regexp.MustCompile(`(?mi)(?:ALTER\s+TABLE\s+(?:(\w+)\.)?(\w+)\s+ADD\s+(?:CONSTRAINT\s+\w+\s+)?FOREIGN\s+KEY\s*\(([^)]+)\)\s*REFERENCES\s+(?:(\w+)\.)?(\w+)\s*\(([^)]+)\))`)
	sqlCreateIndexRegex  = regexp.MustCompile(`(?mi)CREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:CONCURRENTLY\s+)?(?:IF\s+NOT\s+EXISTS\s+)?(?:(\w+)\.)?(\w+)\s+ON\s+(?:ONLY\s+)?(?:(\w+)\.)?(\w+)\s*\(`)
	sqlCreateTypeRegex   = regexp.MustCompile(`(?mi)(?:CREATE\s+TYPE\s+(?:(\w+)\.)?(\w+)\s+AS\s+ENUM\s*\(([^)]+)\))`)
	sqlColumnDefRegex    = regexp.MustCompile(`(?m)^\s*(\w+)\s+(\w+(?:\s*\([^)]*\))?)\s*(.*?)(?:,|$)`)
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
	case ".md":
		mdSymbols := extractMarkdownSymbols(lines)
		symbols = append(symbols, mdSymbols...)
	case ".sql":
		sqlSymbols, sqlImports := extractSQLSymbols(lines)
		symbols = append(symbols, sqlSymbols...)
		imports = append(imports, sqlImports...)
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
			if len(m) > 2 && len(m[2]) > 0 {
				blockMatches := goImportBlockRegex.FindAllSubmatch(m[2], -1)
				for _, bm := range blockMatches {
					if len(bm) > 1 && len(bm[1]) > 0 {
						imports = append(imports, string(bm[1]))
					}
				}
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

	// 3. Global comment rationale extraction pass for legacy/fallback matching
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		var cleanText string
		hasComment := false
		if strings.HasPrefix(trimmed, "//") {
			cleanText = strings.TrimPrefix(trimmed, "//")
			hasComment = true
		} else if strings.HasPrefix(trimmed, "#") {
			cleanText = strings.TrimPrefix(trimmed, "#")
			hasComment = true
		} else if strings.HasPrefix(trimmed, "/*") {
			cleanText = strings.TrimPrefix(trimmed, "/*")
			cleanText = strings.TrimSuffix(cleanText, "*/")
			hasComment = true
		} else if strings.HasPrefix(trimmed, "*") && (ext == ".js" || ext == ".ts" || ext == ".jsx" || ext == ".tsx" || ext == ".go" || ext == ".java") {
			cleanText = strings.TrimPrefix(trimmed, "*")
			hasComment = true
		}

		if hasComment {
			cleanText = strings.TrimSpace(cleanText)
			upperClean := strings.ToUpper(cleanText)
			prefixes := []string{"NOTE:", "WHY:", "HACK:", "TODO:", "FIXME:"}
			hasPrefix := false
			for _, p := range prefixes {
				if strings.HasPrefix(upperClean, p) {
					hasPrefix = true
					break
				}
			}
			if hasPrefix {
				addSymbol(cleanText, "rationale", false, i+1)
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

// extractMarkdownSymbols parses markdown content for headings, fenced code blocks, and mermaid diagrams.
func extractMarkdownSymbols(lines []string) []Symbol {
	var symbols []Symbol
	inFence := false
	fenceLang := ""
	fenceStart := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Check for fence open/close
		if strings.HasPrefix(trimmed, "```") {
			if !inFence {
				inFence = true
				fenceStart = i + 1
				fenceLang = strings.TrimSpace(trimmed[3:])
				// Mermaid diagrams get a dedicated node type
				if strings.EqualFold(fenceLang, "mermaid") {
					// Accumulate lines until close
					continue
				}
			} else {
				// Close fence — emit code block
				symType := "code_block"
				meta := map[string]interface{}{"line": fenceStart}
				if fenceLang != "" {
					meta["language"] = fenceLang
				}
				// Mermaid block
				if strings.EqualFold(fenceLang, "mermaid") {
					symType = "diagram"
				}
				symbols = append(symbols, Symbol{
					Name:     fenceLang,
					Type:     symType,
					Line:     fenceStart,
					Exported: false,
					Metadata: meta,
				})
				inFence = false
				fenceLang = ""
			}
			continue
		}
		if inFence && strings.EqualFold(fenceLang, "mermaid") {
			continue // skip mermaid content
		}
		// Headings
		if matches := mdHeadingRegex.FindStringSubmatch(line); matches != nil {
			level := len(matches[1])
			title := strings.TrimSpace(matches[2])
			if title != "" {
				symbols = append(symbols, Symbol{
					Name:     title,
					Type:     "section",
					Line:     i + 1,
					Exported: false,
					Metadata: map[string]interface{}{"level": level},
				})
			}
		}
	}
	return symbols
}

// extractSQLSymbols parses SQL DDL content for tables, views, indexes, types, and FK references.
func extractSQLSymbols(lines []string) ([]Symbol, []string) {
	var symbols []Symbol
	var imports []string

	content := []byte(strings.Join(lines, "\n"))

	// CREATE TABLE
	for _, m := range sqlCreateTableRegex.FindAllSubmatch(content, -1) {
		if len(m) > 2 && len(m[2]) > 0 {
			tableName := string(m[2])
			schema := ""
			if len(m[1]) > 0 {
				schema = string(m[1])
			}
			line := findLineNumberInLines(lines, string(m[0]))
			meta := map[string]interface{}{"line": line}
			if schema != "" {
				meta["schema"] = schema
			}
			// Extract column definitions from the CREATE TABLE body
			columns := extractSQLColumns(content, string(m[0]))
			if len(columns) > 0 {
				meta["columns"] = columns
			}
			symbols = append(symbols, Symbol{
				Name:     tableName,
				Type:     "table",
				Line:     line,
				Exported: true,
				Metadata: meta,
			})
		}
	}

	// CREATE VIEW
	for _, m := range sqlCreateViewRegex.FindAllSubmatch(content, -1) {
		if len(m) > 2 && len(m[2]) > 0 {
			viewName := string(m[2])
			schema := ""
			if len(m[1]) > 0 {
				schema = string(m[1])
			}
			line := findLineNumberInLines(lines, string(m[0]))
			meta := map[string]interface{}{"line": line}
			if schema != "" {
				meta["schema"] = schema
			}
			symbols = append(symbols, Symbol{
				Name:     viewName,
				Type:     "view",
				Line:     line,
				Exported: true,
				Metadata: meta,
			})
		}
	}

	// CREATE INDEX
	for _, m := range sqlCreateIndexRegex.FindAllSubmatch(content, -1) {
		if len(m) > 2 && len(m[2]) > 0 {
			idxName := string(m[2])
			tableName := string(m[4])
			line := findLineNumberInLines(lines, string(m[0]))
			meta := map[string]interface{}{
				"line":  line,
				"table": tableName,
			}
			isUnique := strings.Contains(strings.ToUpper(string(m[0])), "UNIQUE")
			meta["unique"] = isUnique
			symbols = append(symbols, Symbol{
				Name:     idxName,
				Type:     "index",
				Line:     line,
				Exported: true,
				Metadata: meta,
			})
		}
	}

	// CREATE TYPE … AS ENUM
	for _, m := range sqlCreateTypeRegex.FindAllSubmatch(content, -1) {
		if len(m) > 2 && len(m[2]) > 0 {
			typeName := string(m[2])
			values := string(m[3])
			line := findLineNumberInLines(lines, string(m[0]))
			symbols = append(symbols, Symbol{
				Name:     typeName,
				Type:     "type",
				Line:     line,
				Exported: true,
				Metadata: map[string]interface{}{
					"line":   line,
					"values": values,
				},
			})
		}
	}

	// ALTER TABLE … ADD FOREIGN KEY — emit as import references
	for _, m := range sqlAlterFKRegex.FindAllSubmatch(content, -1) {
		if len(m) > 4 {
			sourceTable := string(m[2])
			sourceCol := string(m[3])
			targetTable := string(m[5])
			targetCol := string(m[6])
			fkDesc := sourceTable + "(" + sourceCol + ") REFERENCES " + targetTable + "(" + targetCol + ")"
			imports = append(imports, fkDesc)
		}
	}

	return symbols, imports
}

// extractSQLColumns attempts to parse column definitions from a CREATE TABLE body.
func extractSQLColumns(content []byte, createTableMatch string) []map[string]string {
	// Find the opening parenthesis after CREATE TABLE ... (
	startIdx := strings.Index(createTableMatch, "(")
	if startIdx < 0 {
		// Search in the broader content after the match
		matchEnd := strings.Index(string(content), createTableMatch)
		if matchEnd < 0 {
			return nil
		}
		remainder := string(content)[matchEnd+len(createTableMatch):]
		startIdx = strings.Index(remainder, "(")
		if startIdx < 0 {
			return nil
		}
		// No, we need to use original content. Find the paren after the match position.
		afterMatch := string(content)[matchEnd+len(createTableMatch):]
		parenIdx := strings.Index(afterMatch, "(")
		if parenIdx < 0 {
			return nil
		}
		startIdx = matchEnd + len(createTableMatch) + parenIdx
	}

	// Find matching closing paren considering nesting
	body := string(content)[startIdx:]
	depth := 0
	endIdx := -1
	for i, ch := range body {
		if ch == '(' {
			depth++
		} else if ch == ')' {
			depth--
			if depth == 0 {
				endIdx = startIdx + i
				break
			}
		}
	}
	if endIdx < 0 {
		return nil
	}

	inner := string(content)[startIdx+1 : endIdx]
	lines := strings.Split(inner, "\n")
	var columns []map[string]string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "CONSTRAINT") || strings.HasPrefix(trimmed, "PRIMARY") ||
			strings.HasPrefix(trimmed, "UNIQUE") || strings.HasPrefix(trimmed, "CHECK") ||
			strings.HasPrefix(trimmed, "FOREIGN") || strings.HasPrefix(trimmed, "INDEX") ||
			strings.HasPrefix(trimmed, "KEY") || strings.HasPrefix(trimmed, ")") {
			continue
		}
		parts := strings.Fields(trimmed)
		if len(parts) >= 2 {
			col := map[string]string{
				"name":     parts[0],
				"datatype": parts[1],
			}
			// Check for NOT NULL, PRIMARY KEY, etc.
			rest := strings.ToUpper(strings.Join(parts[2:], " "))
			if strings.Contains(rest, "NOT NULL") || strings.Contains(rest, "PRIMARY KEY") {
				col["nullable"] = "false"
			} else {
				col["nullable"] = "true"
			}
			if strings.Contains(rest, "PRIMARY KEY") {
				col["primary_key"] = "true"
			}
			if strings.Contains(rest, "AUTO_INCREMENT") || strings.Contains(rest, "AUTOINCREMENT") ||
				strings.Contains(rest, "GENERATED") || strings.Contains(rest, "SERIAL") {
				col["auto"] = "true"
			}
			if strings.Contains(rest, "DEFAULT") {
				// Capture default value after DEFAULT keyword
				if defIdx := strings.Index(rest, "DEFAULT"); defIdx >= 0 {
					defVal := strings.TrimSpace(rest[defIdx+7:])
					// Stop at constraint keywords
					endWords := []string{"NOT", "NULL", "PRIMARY", "UNIQUE", "CHECK", "REFERENCES", "ON"}
					for _, ew := range endWords {
						if ewIdx := strings.Index(defVal, ew); ewIdx >= 0 {
							defVal = strings.TrimSpace(defVal[:ewIdx])
						}
					}
					if defVal != "" {
						// Unquote
						defVal = strings.Trim(defVal, "'\"")
						col["default"] = defVal
					}
				}
			}
			columns = append(columns, col)
		}
	}
	return columns
}

// findLineNumberInLines returns the 1-based line number where substr appears.
func findLineNumberInLines(lines []string, substr string) int {
	substr = strings.TrimSpace(substr)
	if len(substr) > 80 {
		substr = substr[:80]
	}
	for i, line := range lines {
		if strings.Contains(line, substr) {
			return i + 1
		}
	}
	return 0
}
