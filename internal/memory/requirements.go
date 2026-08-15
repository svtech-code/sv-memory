package memory

import (
	"fmt"
	"strings"
)

// Delta operation constants mirroring OpenSpec's delta sections. They are
// stored without the " Requirements" suffix for a compact, comparable value.
const (
	DeltaAdded    = "ADDED"
	DeltaModified = "MODIFIED"
	DeltaRemoved  = "REMOVED"
	DeltaRenamed  = "RENAMED"
)

// DeltaOpValid reports whether the given op is a legal delta operation.
func DeltaOpValid(op string) bool {
	switch op {
	case DeltaAdded, DeltaModified, DeltaRemoved, DeltaRenamed:
		return true
	}
	return false
}

// ScenarioStep is a single GIVEN/WHEN/THEN/AND bullet inside a scenario.
type ScenarioStep struct {
	Keyword string `json:"keyword"` // GIVEN | WHEN | THEN | AND
	Text    string `json:"text"`
}

// Scenario is a concrete, testable example under a requirement.
type Scenario struct {
	Name  string         `json:"name"`
	Steps []ScenarioStep `json:"steps"`
}

// Requirement is a parsed requirement from a delta section. For RENAMED the
// Name is the old header and RenameTo the new one (no body or scenarios); for
// the other ops RenameTo is empty.
type Requirement struct {
	Name      string     `json:"name"`
	RenameTo  string     `json:"rename_to,omitempty"`
	Body      string     `json:"body,omitempty"`
	Scenarios []Scenario `json:"scenarios,omitempty"`
}

// Delta is a single delta section (one op) with its ordered requirements.
type Delta struct {
	Op           string        `json:"op"`
	Requirements []Requirement `json:"requirements"`
}

// scenarioStepKeywords are the accepted leading keywords of a scenario step.
var scenarioStepKeywords = map[string]bool{
	"GIVEN": true,
	"WHEN":  true,
	"THEN":  true,
	"AND":   true,
}

// ParseSpecDeltas parses OpenSpec-style delta markdown into ordered deltas.
// The four delta headers (## ADDED/MODIFIED/REMOVED/RENAMED Requirements) open
// a section; each "### Requirement: <name>" block carries a narrative body
// before its "#### Scenario:" sub-blocks, whose bullets are GIVEN/WHEN/THEN/AND
// steps. RENAMED uses "FROM:"/"TO:" bullet pairs instead of requirement blocks.
// Parsing is lenient: unknown sections are skipped and malformed blocks are
// dropped rather than failing the whole document, matching ParseChangeMarkdown.
func ParseSpecDeltas(content string) []Delta {
	lines := strings.Split(content, "\n")
	var deltas []Delta
	section := ""
	var curReq *Requirement
	var curScenario *Scenario
	var pendingFrom string

	flushScenario := func() {
		if curScenario == nil {
			return
		}
		if curReq != nil {
			curReq.Scenarios = append(curReq.Scenarios, *curScenario)
		}
		curScenario = nil
	}
	flushRequirement := func() {
		flushScenario()
		if curReq == nil {
			return
		}
		appendRequirement(&deltas, section, *curReq)
		curReq = nil
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if level, text, ok := headerLine(trimmed); ok {
			switch level {
			case 2:
				flushRequirement()
				pendingFrom = ""
				section = deltaOpForHeader(text)
			case 3:
				flushRequirement()
				if name, ok := strings.CutPrefix(text, "Requirement:"); ok {
					name = strings.TrimSpace(name)
					if name != "" {
						curReq = &Requirement{Name: name}
					}
				}
			case 4:
				if curReq == nil {
					continue
				}
				if name, ok := strings.CutPrefix(text, "Scenario:"); ok {
					name = strings.TrimSpace(name)
					if name != "" {
						flushScenario()
						curScenario = &Scenario{Name: name}
					}
				}
			}
			continue
		}

		if bullet, isBullet := trimBullet(trimmed); isBullet {
			switch {
			case curScenario != nil:
				if step, ok := parseScenarioStep(bullet); ok {
					curScenario.Steps = append(curScenario.Steps, step)
				}
			case section == DeltaRenamed:
				// RENAMED pairs a FROM bullet with a following TO bullet.
				if from, ok := parseRenameToken(bullet, "FROM"); ok {
					pendingFrom = from
				} else if to, ok := parseRenameToken(bullet, "TO"); ok && pendingFrom != "" {
					appendRequirement(&deltas, section, Requirement{Name: pendingFrom, RenameTo: to})
					pendingFrom = ""
				}
			}
			// Bullets outside a scenario/rename section are not body prose and
			// are dropped.
			continue
		}

		// Plain prose: a requirement narrative before its first scenario.
		if curScenario == nil && curReq != nil {
			curReq.Body = appendBodyLine(curReq.Body, trimmed)
		}
	}
	flushRequirement()
	return deltas
}

// headerLine reports the heading level (2, 3, or 4) and text of a markdown
// header line, e.g. "## ADDED Requirements" -> (2, "ADDED Requirements", true).
func headerLine(trimmed string) (int, string, bool) {
	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	if level < 2 || level > 4 {
		return 0, "", false
	}
	if level < len(trimmed) && trimmed[level] != ' ' {
		return 0, "", false
	}
	return level, strings.TrimSpace(trimmed[level:]), true
}

// deltaOpForHeader maps a section header to its op, or "" for unknown headers
// (e.g. "## Purpose"), which the parser skips.
func deltaOpForHeader(text string) string {
	switch text {
	case "ADDED Requirements":
		return DeltaAdded
	case "MODIFIED Requirements":
		return DeltaModified
	case "REMOVED Requirements":
		return DeltaRemoved
	case "RENAMED Requirements":
		return DeltaRenamed
	}
	return ""
}

// trimBullet strips a leading "- " or "* " bullet marker, returning the inner
// text and whether the line was a bullet.
func trimBullet(trimmed string) (string, bool) {
	for _, marker := range []string{"- ", "* "} {
		if strings.HasPrefix(trimmed, marker) {
			return strings.TrimSpace(trimmed[len(marker):]), true
		}
	}
	return "", false
}

// parseScenarioStep parses a bullet into a scenario step when it starts with a
// bolded or bare keyword (GIVEN/WHEN/THEN/AND). It accepts "**WHEN** text",
// "WHEN text", and "WHEN: text" forms. Returns ok=false when the bullet is not
// a recognized step.
func parseScenarioStep(bullet string) (ScenarioStep, bool) {
	word, rest, ok := leadingWord(bullet)
	if !ok || !scenarioStepKeywords[word] {
		return ScenarioStep{}, false
	}
	return ScenarioStep{Keyword: word, Text: rest}, true
}

// leadingWord extracts a leading bolded ("**WORD**") or bare ("WORD") token and
// the remaining text, stripping an optional trailing colon after the token.
func leadingWord(s string) (word, rest string, ok bool) {
	if strings.HasPrefix(s, "**") {
		closeIdx := strings.Index(s[2:], "**")
		if closeIdx < 0 {
			return "", "", false
		}
		// A trailing colon may sit inside the bold markers (e.g. "**FROM:**").
		word = strings.TrimSuffix(strings.ToUpper(strings.TrimSpace(s[2:2+closeIdx])), ":")
		rest = strings.TrimSpace(s[2+closeIdx+2:])
		rest = strings.TrimPrefix(rest, ":")
		return word, strings.TrimSpace(rest), true
	}

	spaceIdx := strings.IndexAny(s, " :")
	word = s
	if spaceIdx >= 0 {
		word = s[:spaceIdx]
	}
	word = strings.ToUpper(word)
	rest = s[len(word):]
	rest = strings.TrimPrefix(strings.TrimSpace(rest), ":")
	return word, strings.TrimSpace(rest), true
}

// parseRenameToken extracts the value of a "**FROM:** x" / "**TO:** x" (or bare
// "FROM: x") bullet, stripping backticks and surrounding spaces.
func parseRenameToken(bullet, key string) (string, bool) {
	word, rest, ok := leadingWord(bullet)
	if !ok || word != key {
		return "", false
	}
	value := strings.Trim(rest, "`")
	return value, value != ""
}

// appendBodyLine appends a line to a requirement body with a single newline
// separator, returning the updated body.
func appendBodyLine(body, line string) string {
	if body == "" {
		return line
	}
	return body + "\n" + line
}

// appendRequirement appends a requirement to the delta of the given op,
// creating the delta on first use so section order is preserved.
func appendRequirement(deltas *[]Delta, op string, req Requirement) {
	for i := range *deltas {
		if (*deltas)[i].Op == op {
			(*deltas)[i].Requirements = append((*deltas)[i].Requirements, req)
			return
		}
	}
	*deltas = append(*deltas, Delta{Op: op, Requirements: []Requirement{req}})
}

// ExtractRFC2119 returns the RFC 2119 normative keywords present in a
// requirement body, in a canonical order (MUST, SHALL, SHOULD, MAY with their
// " NOT" negations reported as distinct keywords). Used to warn when a
// non-empty body carries no normative intent, mirroring OpenSpec's guidance.
func ExtractRFC2119(body string) []string {
	upper := strings.ToUpper(body)
	var out []string
	for _, kw := range []string{"MUST", "SHALL", "SHOULD", "MAY"} {
		neg := kw + " NOT"
		switch {
		case hasKeywordWord(upper, neg):
			out = append(out, neg)
		case hasKeywordWord(upper, kw):
			out = append(out, kw)
		}
	}
	return out
}

// hasKeywordWord reports whether s contains word as a whole word (bounded by
// non-letter characters on both sides), avoiding false positives such as
// "MUST" matching "MUSTACHE".
func hasKeywordWord(s, word string) bool {
	idx := 0
	for {
		i := strings.Index(s[idx:], word)
		if i < 0 {
			return false
		}
		start := idx + i
		end := start + len(word)
		before := start == 0 || !isLetterByte(s[start-1])
		after := end == len(s) || !isLetterByte(s[end])
		if before && after {
			return true
		}
		idx = end
	}
}

// isLetterByte reports whether b is an ASCII letter.
func isLetterByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// DeltasToMarkdown renders ordered deltas back into OpenSpec-style delta
// markdown, the inverse of ParseSpecDeltas. It is the single source of truth
// for the delta sections embedded in the change mirror (Phase 2) and for the
// round-trip test of the parser.
func DeltasToMarkdown(deltas []Delta) string {
	var sb strings.Builder
	for i, d := range deltas {
		if i > 0 {
			sb.WriteString("\n")
		}
		fmt.Fprintf(&sb, "## %s Requirements\n", d.Op)
		for _, r := range d.Requirements {
			sb.WriteString("\n")
			if d.Op == DeltaRenamed {
				fmt.Fprintf(&sb, "- **FROM:** `%s`\n", r.Name)
				fmt.Fprintf(&sb, "- **TO:** `%s`\n", r.RenameTo)
				continue
			}
			fmt.Fprintf(&sb, "### Requirement: %s\n", r.Name)
			if r.Body != "" {
				sb.WriteString(r.Body + "\n")
			}
			for _, sc := range r.Scenarios {
				fmt.Fprintf(&sb, "\n#### Scenario: %s\n", sc.Name)
				for _, st := range sc.Steps {
					fmt.Fprintf(&sb, "- **%s** %s\n", st.Keyword, st.Text)
				}
			}
		}
	}
	return strings.TrimSpace(sb.String())
}
