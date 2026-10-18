package extractor

import (
	"strings"

	"github.com/svtech-code/sv-memory/internal/graph/schema"
)

// extractSQLSymbols parses SQL DDL content for tables, views, indexes, types,
// and FK references.
func extractSQLSymbols(lines []string) ([]Symbol, []string) {
	content := []byte(strings.Join(lines, "\n"))
	var symbols []Symbol
	symbols = append(symbols, sqlTableSymbols(content, lines)...)
	symbols = append(symbols, sqlViewSymbols(content, lines)...)
	symbols = append(symbols, sqlIndexSymbols(content, lines)...)
	symbols = append(symbols, sqlTypeSymbols(content, lines)...)
	return symbols, sqlForeignKeyRefs(content)
}

func sqlTableSymbols(content []byte, lines []string) []Symbol {
	var symbols []Symbol
	for _, m := range sqlCreateTableRegex.FindAllSubmatch(content, -1) {
		if len(m) <= 2 || len(m[2]) == 0 {
			continue
		}
		line := findLineNumberInLines(lines, string(m[0]))
		meta := map[string]interface{}{"line": line}
		if schemaName := string(m[1]); schemaName != "" {
			meta["schema"] = schemaName
		}
		if cols := extractSQLColumns(content, string(m[0])); len(cols) > 0 {
			meta["columns"] = cols
		}
		symbols = append(symbols, Symbol{Name: string(m[2]), Type: schema.NodeTypeTable, Line: line, Exported: true, Metadata: meta})
	}
	return symbols
}

func sqlViewSymbols(content []byte, lines []string) []Symbol {
	var symbols []Symbol
	for _, m := range sqlCreateViewRegex.FindAllSubmatch(content, -1) {
		if len(m) <= 2 || len(m[2]) == 0 {
			continue
		}
		line := findLineNumberInLines(lines, string(m[0]))
		meta := map[string]interface{}{"line": line}
		if schemaName := string(m[1]); schemaName != "" {
			meta["schema"] = schemaName
		}
		symbols = append(symbols, Symbol{Name: string(m[2]), Type: schema.NodeTypeView, Line: line, Exported: true, Metadata: meta})
	}
	return symbols
}

func sqlIndexSymbols(content []byte, lines []string) []Symbol {
	var symbols []Symbol
	for _, m := range sqlCreateIndexRegex.FindAllSubmatch(content, -1) {
		if len(m) <= 4 || len(m[2]) == 0 {
			continue
		}
		line := findLineNumberInLines(lines, string(m[0]))
		symbols = append(symbols, Symbol{
			Name:     string(m[2]),
			Type:     schema.NodeTypeIndex,
			Line:     line,
			Exported: true,
			Metadata: map[string]interface{}{
				"line":   line,
				"table":  string(m[4]),
				"unique": strings.Contains(strings.ToUpper(string(m[0])), "UNIQUE"),
			},
		})
	}
	return symbols
}

func sqlTypeSymbols(content []byte, lines []string) []Symbol {
	var symbols []Symbol
	for _, m := range sqlCreateTypeRegex.FindAllSubmatch(content, -1) {
		if len(m) <= 3 || len(m[2]) == 0 {
			continue
		}
		line := findLineNumberInLines(lines, string(m[0]))
		symbols = append(symbols, Symbol{
			Name:     string(m[2]),
			Type:     schema.NodeTypeType,
			Line:     line,
			Exported: true,
			Metadata: map[string]interface{}{"line": line, "values": string(m[3])},
		})
	}
	return symbols
}

func sqlForeignKeyRefs(content []byte) []string {
	var imports []string
	for _, m := range sqlAlterFKRegex.FindAllSubmatch(content, -1) {
		if len(m) > 6 {
			imports = append(imports, string(m[2])+"("+string(m[3])+") REFERENCES "+string(m[5])+"("+string(m[6])+")")
		}
	}
	return imports
}

// extractSQLColumns attempts to parse column definitions from a CREATE TABLE body.
func extractSQLColumns(content []byte, createTableMatch string) []map[string]string {
	startIdx := sqlTableBodyStart(content, createTableMatch)
	if startIdx < 0 {
		return nil
	}
	endIdx := matchingParenEnd(string(content), startIdx)
	if endIdx < 0 {
		return nil
	}
	var columns []map[string]string
	for _, line := range strings.Split(string(content)[startIdx+1:endIdx], "\n") {
		if col, ok := parseSQLColumnLine(line); ok {
			columns = append(columns, col)
		}
	}
	return columns
}

// sqlTableBodyStart locates the opening paren of the CREATE TABLE body. The
// first branch preserves the historical (match-relative) behaviour.
func sqlTableBodyStart(content []byte, createTableMatch string) int {
	if i := strings.Index(createTableMatch, "("); i >= 0 {
		return i
	}
	matchEnd := strings.Index(string(content), createTableMatch)
	if matchEnd < 0 {
		return -1
	}
	after := string(content)[matchEnd+len(createTableMatch):]
	parenIdx := strings.Index(after, "(")
	if parenIdx < 0 {
		return -1
	}
	return matchEnd + len(createTableMatch) + parenIdx
}

func matchingParenEnd(content string, startIdx int) int {
	body := content[startIdx:]
	depth := 0
	for i, ch := range body {
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return startIdx + i
			}
		}
	}
	return -1
}

func parseSQLColumnLine(line string) (map[string]string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || isSQLConstraintLine(trimmed) {
		return nil, false
	}
	parts := strings.Fields(trimmed)
	if len(parts) < 2 {
		return nil, false
	}
	col := map[string]string{"name": parts[0], "datatype": parts[1]}
	rest := strings.ToUpper(strings.Join(parts[2:], " "))
	if strings.Contains(rest, "NOT NULL") || strings.Contains(rest, "PRIMARY KEY") {
		col["nullable"] = "false"
	} else {
		col["nullable"] = "true"
	}
	if strings.Contains(rest, "PRIMARY KEY") {
		col["primary_key"] = "true"
	}
	if sqlIsAutoColumn(rest) {
		col["auto"] = "true"
	}
	if def, ok := sqlDefaultValue(rest); ok {
		col["default"] = def
	}
	return col, true
}

func isSQLConstraintLine(s string) bool {
	for _, p := range []string{"CONSTRAINT", "PRIMARY", "UNIQUE", "CHECK", "FOREIGN", "INDEX", "KEY", ")"} {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func sqlIsAutoColumn(rest string) bool {
	for _, kw := range []string{"AUTO_INCREMENT", "AUTOINCREMENT", "GENERATED", "SERIAL"} {
		if strings.Contains(rest, kw) {
			return true
		}
	}
	return false
}

func sqlDefaultValue(rest string) (string, bool) {
	defIdx := strings.Index(rest, "DEFAULT")
	if defIdx < 0 {
		return "", false
	}
	defVal := strings.TrimSpace(rest[defIdx+7:])
	for _, ew := range []string{"NOT", "NULL", "PRIMARY", "UNIQUE", "CHECK", "REFERENCES", "ON"} {
		if ewIdx := strings.Index(defVal, ew); ewIdx >= 0 {
			defVal = strings.TrimSpace(defVal[:ewIdx])
		}
	}
	if defVal == "" {
		return "", false
	}
	return strings.Trim(defVal, "'\""), true
}
