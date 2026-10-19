package memory

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/svtech-code/sv-memory/internal/graph"
	"github.com/svtech-code/sv-memory/internal/security"
)

func RenderContextPack(p *ContextPack, whyChars int) string {
	var sb strings.Builder

	if p.Node != nil {
		renderNodeHeader(&sb, p.Node)
	}
	if p.Snippet != "" {
		renderSnippet(&sb, p)
	}
	if len(p.CallPath) > 0 {
		renderCallPath(&sb, p.CallPath)
	}
	if len(p.ExtraSnippets) > 0 {
		renderExtraSnippets(&sb, p.ExtraSnippets)
	}
	if len(p.Memories) > 0 {
		renderMemories(&sb, p.Memories, whyChars)
	}
	if len(p.Dependents) > 0 {
		renderNeighbors(&sb, "\n### Direct dependents (who imports/calls this):\n", p.Dependents)
	}
	if len(p.BlastRadius) > 0 {
		renderBlastRadius(&sb, p.BlastRadius)
	}
	if len(p.Dependencies) > 0 {
		renderNeighbors(&sb, "\n### Direct dependencies (what this imports/calls):\n", p.Dependencies)
	}
	if len(p.Changes) > 0 {
		renderChanges(&sb, p.Changes)
	}
	if len(p.Capabilities) > 0 {
		renderCapabilities(&sb, p.Capabilities)
	}

	if contextPackEmpty(p) {
		return "No context found: no matching graph node and no path-scoped memories for the given path."
	}
	return strings.TrimSpace(sb.String())
}

// contextPackEmpty reports whether the pack carries nothing worth rendering.
func contextPackEmpty(p *ContextPack) bool {
	return p.Node == nil && len(p.Memories) == 0 && len(p.Changes) == 0 &&
		len(p.Capabilities) == 0 && len(p.ExtraSnippets) == 0
}

// renderNodeHeader writes the node summary header (type, path, degree, hub warning).
func renderNodeHeader(sb *strings.Builder, n *ContextNode) {
	fmt.Fprintf(sb, "## Context Pack: `%s` (`%s`)\n", n.Label, n.ID)
	fmt.Fprintf(sb, "- **Type:** `%s`", n.Type)
	if n.Path != "" {
		fmt.Fprintf(sb, " · **Path:** `%s`", n.Path)
	}
	sb.WriteString("\n")
	fmt.Fprintf(sb, "- **Fan-in (dependents):** %d · **Fan-out (dependencies):** %d · **Degree:** %d\n", n.FanIn, n.FanOut, n.Degree)
	community := "none"
	if n.CommunityID > 0 {
		community = fmt.Sprintf("ID %d", n.CommunityID)
	}
	fmt.Fprintf(sb, "- **Community:** %s\n", community)
	if n.Degree > 10 {
		sb.WriteString("- **Hub:** ⚠️ high connectivity — changes ripple; use `sv_graph_explain` before refactoring\n")
	}
}

// renderSnippet writes the fenced source-code snippet with its language tag.
func renderSnippet(sb *strings.Builder, p *ContextPack) {
	ext := "text"
	if p.Node != nil && p.Node.Path != "" {
		if cleanExt := strings.TrimPrefix(filepath.Ext(p.Node.Path), "."); cleanExt != "" {
			ext = cleanExt
		}
	}
	fmt.Fprintf(sb, "\n### Source Code Snippet (L%d):\n```%s\n%s\n```\n", p.SnippetLine, ext, p.Snippet)
}

// renderNeighbors writes a neighbor list under the given header.
func renderNeighbors(sb *strings.Builder, header string, neighbors []ContextNeighbor) {
	sb.WriteString(header)
	for _, d := range neighbors {
		fmt.Fprintf(sb, "- `%s` (%s, %s)\n", d.Label, d.RelationType, strings.ToLower(d.Confidence))
	}
}

// renderBlastRadius writes the transitive blast-radius list.
func renderBlastRadius(sb *strings.Builder, nodes []graph.BlastRadiusNode) {
	sb.WriteString("\n### 💥 Transitive blast radius (affected upstream consumers):\n")
	for _, b := range nodes {
		hubTag := ""
		if b.IsHub {
			hubTag = " ⚠️ Hub"
		}
		fmt.Fprintf(sb, "- `%s` (hop %d, %s)%s\n", b.ID, b.Depth, b.RelationType, hubTag)
	}
}

// renderCapabilities writes the implemented-capabilities section.
func renderCapabilities(sb *strings.Builder, caps []ContextCapability) {
	sb.WriteString("\n### Capabilities implemented here:\n")
	for _, cap := range caps {
		fmt.Fprintf(sb, "- **%s** (%d requirement%s)", cap.CapabilityPath, cap.RequirementCount, plural(cap.RequirementCount))
		if len(cap.Requirements) > 0 {
			sb.WriteString(": " + strings.Join(cap.Requirements, ", "))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n*Full spec: `.sv-memory/specs/capabilities/<cap>/spec.md` — drill down with `sv_graph_explore` or `sv_graph_query`.*\n")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// splitContextQuery splits an explore query into at most 3 symbols. It splits
// on commas (and trims whitespace), but returns a single-element slice when the
// caller gave one symbol, preserving the legacy single-node code path.

func renderCallPath(sb *strings.Builder, hops []CallPathHop) {
	sb.WriteString("\n### Call path (shortest dependency chain):\n")
	for i, hop := range hops {
		if i > 0 {
			arrow := " --> "
			if hop.RelationType != "" {
				arrow = fmt.Sprintf(" --[%s %s]--> ", hop.RelationType, strings.ToLower(hop.Confidence))
			}
			sb.WriteString(arrow)
		}
		fmt.Fprintf(sb, "`%s`", hop.Label)
	}
	sb.WriteString("\n")
}

// renderExtraSnippets writes the "Related symbols (source)" section for the
// secondary symbols of a multi-symbol explore call.
func renderExtraSnippets(sb *strings.Builder, snippets []ContextSnippet) {
	sb.WriteString("\n### Related symbols (source):\n")
	for _, s := range snippets {
		ext := "text"
		cleanExt := strings.TrimPrefix(filepath.Ext(s.Path), ".")
		if cleanExt != "" {
			ext = cleanExt
		}
		fmt.Fprintf(sb, "\n- **%s** (`%s`, L%d)\n```%s\n%s\n```\n", s.Label, s.Path, s.StartLine, ext, s.Text)
	}
}

// renderMemories writes the "Linked memories" section with truncated why fields.
func renderMemories(sb *strings.Builder, mems []ContextMemory, whyChars int) {
	sb.WriteString("\n### Linked memories (why this code is the way it is):\n")
	for _, m := range mems {
		fmt.Fprintf(sb, "- **[%s] %s** (ID: %s)\n", strings.ToUpper(m.Category), m.What, m.ID)
		if m.Why != "" {
			fmt.Fprintf(sb, "  *Why:* %s\n", TruncateText(security.SanitizeText(m.Why), whyChars))
		}
		if m.Source == "rationale_for" {
			sb.WriteString("  *linked via code graph (rationale_for)*\n")
		}
	}
	sb.WriteString("\n*Drill down with `sv_mem_get(id=\"<id>\")` for full content.*\n")
}

// renderChanges writes the "Active changes" section with optional task progress.
func renderChanges(sb *strings.Builder, changes []ContextChange) {
	sb.WriteString("\n### Active changes affecting this path:\n")
	for _, c := range changes {
		if c.TaskProgress != "" {
			fmt.Fprintf(sb, "- **[%s] %s** (`%s`, slug `%s`, tasks: %s)\n", strings.ToUpper(c.Status), c.Title, c.ID, c.Slug, c.TaskProgress)
		} else {
			fmt.Fprintf(sb, "- **[%s] %s** (`%s`, slug `%s`)\n", strings.ToUpper(c.Status), c.Title, c.ID, c.Slug)
		}
	}
	sb.WriteString("\n*Review with `sv_validate_decision(change_id=\"<id>\")` before modifying this code.*\n")
}
