package extractor

import (
	"strings"

	"github.com/svtech-code/sv-memory/internal/graph/schema"
)

// fenceState tracks an open fenced code block while scanning markdown lines.
type fenceState struct {
	in    bool
	lang  string
	start int
}

// toggle opens or closes a fence on a ``` line, returning the emitted
// code-block symbol when a fence closes.
func (f *fenceState) toggle(trimmed string, i int) (Symbol, bool) {
	if !f.in {
		f.in = true
		f.start = i + 1
		f.lang = strings.TrimSpace(trimmed[3:])
		return Symbol{}, false
	}
	sym := Symbol{
		Name:     f.lang,
		Type:     "code_block",
		Line:     f.start,
		Exported: false,
		Metadata: map[string]interface{}{"line": f.start},
	}
	if f.lang != "" {
		sym.Metadata["language"] = f.lang
	}
	if strings.EqualFold(f.lang, "mermaid") {
		sym.Type = "diagram"
	}
	f.in = false
	f.lang = ""
	return sym, true
}

// inMermaid reports whether the currently open fence is a mermaid diagram.
func (f *fenceState) inMermaid() bool {
	return f.in && strings.EqualFold(f.lang, "mermaid")
}

// extractMarkdownSymbols parses markdown content for headings, fenced code
// blocks, and mermaid diagrams.
func extractMarkdownSymbols(lines []string) []Symbol {
	var symbols []Symbol
	fence := fenceState{}
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if sym, ok := fence.toggle(trimmed, i); ok {
				symbols = append(symbols, sym)
			}
			continue
		}
		if fence.inMermaid() {
			continue
		}
		if sym, ok := markdownHeadingSymbol(line, i); ok {
			symbols = append(symbols, sym)
		}
	}
	return symbols
}

func markdownHeadingSymbol(line string, i int) (Symbol, bool) {
	m := mdHeadingRegex.FindStringSubmatch(line)
	if m == nil {
		return Symbol{}, false
	}
	title := strings.TrimSpace(m[2])
	if title == "" {
		return Symbol{}, false
	}
	return Symbol{
		Name:     title,
		Type:     schema.NodeTypeSection,
		Line:     i + 1,
		Exported: false,
		Metadata: map[string]interface{}{"level": len(m[1])},
	}, true
}
