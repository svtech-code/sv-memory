package extractor

import (
	"strings"
)

// MDSemanticExtractor extracts structured semantic entities (headings, tables, code blocks, rationales)
// and references from Markdown, Text, and documentation files.
type MDSemanticExtractor struct{}

func NewMDSemanticExtractor() *MDSemanticExtractor {
	return &MDSemanticExtractor{}
}

//nolint:gocyclo // markdown parser handles many element kinds; refactor later
func (e *MDSemanticExtractor) Extract(content []byte, relPath, ext string) ([]Symbol, []string, error) {
	lines := strings.Split(string(content), "\n")
	var symbols []Symbol
	var imports []string

	// 1. Extract markdown links & wikilinks as imports
	for _, m := range mdLinkRegex.FindAllSubmatch(content, -1) {
		if len(m) > 1 {
			target := string(m[1])
			if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") && !strings.HasPrefix(target, "#") {
				imports = append(imports, target)
			}
		}
	}
	for _, m := range mdWikilinkRegex.FindAllSubmatch(content, -1) {
		if len(m) > 1 {
			imports = append(imports, string(m[1]))
		}
	}

	// 2. Extract headings, code blocks, diagrams, tables, and rationales
	inFence := false
	fenceLang := ""
	fenceStart := 0

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if matches := mdFenceOpenRegex.FindStringSubmatch(trimmed); matches != nil && !inFence {
			inFence = true
			fenceLang = matches[1]
			fenceStart = i + 1
			continue
		}
		if mdFenceCloseRegex.MatchString(trimmed) && inFence {
			symType := "code_block"
			if strings.EqualFold(fenceLang, "mermaid") {
				symType = "diagram"
			}
			symbols = append(symbols, Symbol{
				Name:     fenceLang,
				Type:     symType,
				Line:     fenceStart,
				Exported: false,
				Metadata: map[string]interface{}{
					"language":   fenceLang,
					"end_line":   i + 1,
					"line_count": i + 1 - fenceStart,
				},
			})
			inFence = false
			fenceLang = ""
			continue
		}

		if inFence {
			continue
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

		// Table detection (Header line starting with | and separator line on next line)
		if strings.HasPrefix(trimmed, "|") && strings.HasSuffix(trimmed, "|") && i+1 < len(lines) {
			nextLine := strings.TrimSpace(lines[i+1])
			if strings.HasPrefix(nextLine, "|") && (strings.Contains(nextLine, "---") || strings.Contains(nextLine, "-|-")) {
				cols := []string{}
				for _, c := range strings.Split(trimmed, "|") {
					col := strings.TrimSpace(c)
					if col != "" {
						cols = append(cols, col)
					}
				}
				symbols = append(symbols, Symbol{
					Name:     strings.Join(cols, ", "),
					Type:     "table",
					Line:     i + 1,
					Exported: false,
					Metadata: map[string]interface{}{
						"columns": cols,
					},
				})
			}
		}

		// Rationale bullet points (e.g. IMPORTANT:, NOTE:, DECISION:, TODO:)
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "> ") {
			lower := strings.ToLower(trimmed)
			if strings.Contains(lower, "important") || strings.Contains(lower, "note") || strings.Contains(lower, "decision") || strings.Contains(lower, "arch") {
				symbols = append(symbols, Symbol{
					Name:     trimmed,
					Type:     "rationale",
					Line:     i + 1,
					Exported: false,
				})
			}
		}
	}

	return symbols, imports, nil
}
