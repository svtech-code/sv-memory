package graph

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/svtech-code/sv-memory/internal/graph/extractor"
	"github.com/svtech-code/sv-memory/internal/graph/schema"
)

// extractCallEdges identifies call relationships (functions calling other
// functions/classes) within the project. Per file it prefers AST-precision
// call refs when the active extractor supports them (tree-sitter); files whose
// language has no AST call coverage (regex-only, Go due to the upstream parser
// bug, parse failures) fall back to the tokenize heuristic. Edges from the AST
// path carry confidence EXTRACTED with a precise L<line>:<col> location.
func extractCallEdges(nodes map[string]*Node, fileContents map[string][]byte) []*Edge {
	var refExt extractor.CallRefExtractor
	if re, ok := currentExtractor.(extractor.CallRefExtractor); ok {
		refExt = re
	}

	// Files whose calls were already emitted by the AST path must be excluded
	// from the heuristic pass so the same edge is not produced twice.
	astCovered := map[string]bool{}
	var edges []*Edge

	if refExt != nil {
		astEdges, covered := extractASTCallEdges(nodes, fileContents, refExt)
		edges = append(edges, astEdges...)
		for f := range covered {
			astCovered[f] = true
		}
	}

	heuristicEdges := extractHeuristicCallEdges(nodes, fileContents, astCovered)
	return append(edges, heuristicEdges...)
}

// extractASTCallEdges builds "calls" edges from tree-sitter AST call
// references. For each file the extractor supports, every call site is resolved
// against the project's function/class nodes (same file first, then cross-file
// by label within the same language group). It returns the edges plus the set
// of file paths whose calls were emitted (so the heuristic skips them).
func extractASTCallEdges(nodes map[string]*Node, fileContents map[string][]byte, refExt extractor.CallRefExtractor) ([]*Edge, map[string]bool) {
	fileSymbols, langSymbols := buildCallSymbolMaps(nodes)

	var edges []*Edge
	covered := map[string]bool{}
	seen := map[string]bool{}

	for filePath, symbols := range fileSymbols {
		fileEdges, ok := extractASTEdgesForFile(filePath, symbols, fileSymbols, langSymbols, fileContents, refExt, seen)
		if !ok {
			continue
		}
		covered[filePath] = true
		edges = append(edges, fileEdges...)
	}

	return edges, covered
}

// buildCallSymbolMaps indexes function/class nodes per file and per language
// group for cross-file call resolution.
func buildCallSymbolMaps(nodes map[string]*Node) (fileSymbols, langSymbols map[string][]*Node) {
	fileSymbols = make(map[string][]*Node)
	langSymbols = make(map[string][]*Node)
	for _, node := range nodes {
		if node.Type != schema.NodeTypeFunction && node.Type != schema.NodeTypeClass {
			continue
		}
		fileSymbols[node.Path] = append(fileSymbols[node.Path], node)
		if lang := getLanguageGroup(strings.ToLower(filepath.Ext(node.Path))); lang != "" {
			langSymbols[lang] = append(langSymbols[lang], node)
		}
	}
	return fileSymbols, langSymbols
}

// extractASTEdgesForFile returns the call edges for a single file. ok is false
// when the file is unsupported, its content is unavailable (unchanged file
// during a lazy sync), or its parse failed — in which case the heuristic pass
// handles it.
func extractASTEdgesForFile(filePath string, symbols []*Node, fileSymbols, langSymbols map[string][]*Node, fileContents map[string][]byte, refExt extractor.CallRefExtractor, seen map[string]bool) ([]*Edge, bool) {
	ext := strings.ToLower(filepath.Ext(filePath))
	lang := getLanguageGroup(ext)
	if lang == "" {
		return nil, false
	}
	content, ok := fileContents[filePath]
	if !ok {
		// Lazy incremental syncs only re-parse the changed files; call
		// edges of unchanged files are preserved, not re-extracted.
		return nil, false
	}

	refs, err := refExt.ExtractCallRefs(content, filePath, ext)
	if err != nil {
		return nil, false // regex-only or parse failure → heuristic will handle it
	}

	// Sort symbols by line so the containing caller can be resolved.
	sortSymbolsByLine(symbols)

	var edges []*Edge
	for _, ref := range refs {
		if edge := astCallEdge(symbols, fileSymbols[filePath], langSymbols[lang], ref, seen); edge != nil {
			edges = append(edges, edge)
		}
	}
	return edges, true
}

// astCallEdge resolves a single AST call reference to an edge, or nil when it
// cannot be attributed.
func astCallEdge(symbols, fileSymbols, langSymbols []*Node, ref extractor.CallRef, seen map[string]bool) *Edge {
	if ref.Callee == "" {
		return nil
	}
	// Resolve caller: the last symbol whose start line <= call line.
	caller := resolveCallerAtLine(symbols, ref.Line)
	if caller == nil {
		// A top-level call before the first function/class (or in a file with
		// no functions) has no containing caller; skip it instead of
		// dereferencing a nil caller.
		return nil
	}
	// Resolve callee: same file first, then cross-file same language.
	target := resolveCalleeNode(fileSymbols, ref.Callee, langSymbols, caller)
	if target == nil {
		return nil
	}

	edgeID := fmt.Sprintf("%s-%s-calls", caller.ID, target.ID)
	if seen[edgeID] {
		return nil
	}
	seen[edgeID] = true
	return &Edge{
		ID:             edgeID,
		SourceID:       caller.ID,
		TargetID:       target.ID,
		RelationType:   schema.EdgeCalls,
		Confidence:     "EXTRACTED",
		SourceLocation: fmt.Sprintf("L%d:%d", ref.Line, ref.Col),
	}
}

// resolveCallerAtLine returns the function/class node whose body contains the
// given source line (the last symbol declared at or before that line). symbols
// must be sorted by start line ascending.
func resolveCallerAtLine(symbols []*Node, line int) *Node {
	var caller *Node
	for _, s := range symbols {
		if getSymbolLine(s) <= line {
			caller = s
		}
	}
	return caller
}

// resolveCalleeNode resolves a call callee name to a project symbol node,
// preferring a same-file match, then a unique cross-file match within the same
// language group. Ambiguous cross-file names resolve to the same-file match if
// any; otherwise the caller is not attributed.
func resolveCalleeNode(fileSymbols []*Node, callee string, langSymbols []*Node, caller *Node) *Node {
	if caller == nil {
		return nil
	}
	// Same file first.
	for _, s := range fileSymbols {
		if s.Label == callee {
			return s
		}
	}
	// Cross-file within the language: unique by label.
	var matches []*Node
	for _, s := range langSymbols {
		if s.Label == callee && s.ID != caller.ID {
			matches = append(matches, s)
		}
	}
	if len(matches) == 1 {
		return matches[0]
	}
	return nil
}

// extractHeuristicCallEdges is the original tokenize-based call extraction,
// kept as the fallback for files/languages without AST call coverage. Files in
// astCovered already had their calls emitted by the AST path and are skipped so
// each edge is produced exactly once. It scans each function/class body for
// identifiers matching other project symbols.
func extractHeuristicCallEdges(nodes map[string]*Node, fileContents map[string][]byte, astCovered map[string]bool) []*Edge {
	fileSymbols, langSymbols := buildCallSymbolMaps(nodes)

	var edges []*Edge
	for filePath, symbols := range fileSymbols {
		// Files already covered by the AST call path are skipped so each edge is
		// produced exactly once (EXTRACTED from AST, not duplicated INFERRED).
		if astCovered[filePath] {
			continue
		}
		content, ok := fileContents[filePath]
		if !ok {
			// Lazy incremental syncs only re-parse the changed files; call
			// edges of unchanged files are preserved, not re-extracted.
			continue
		}
		edges = append(edges, heuristicFileEdges(filePath, symbols, langSymbols, content)...)
	}
	return edges
}

// heuristicFileEdges emits INFERRED call edges for a single file's symbols.
func heuristicFileEdges(filePath string, symbols []*Node, langSymbols map[string][]*Node, content []byte) []*Edge {
	lang := getLanguageGroup(strings.ToLower(filepath.Ext(filePath)))
	if lang == "" {
		return nil
	}
	lines := strings.Split(string(content), "\n")
	if len(lines) == 0 {
		return nil
	}
	sortSymbolsByLine(symbols)
	candidates := langSymbols[lang]

	var edges []*Edge
	for idx, caller := range symbols {
		startLine := symbolStartLine(caller)
		if startLine > len(lines) {
			continue
		}
		body := symbolBody(lines, symbols, idx, startLine)
		edges = append(edges, heuristicCallerEdges(caller, candidates, body, startLine)...)
	}
	return edges
}

// symbolStartLine returns a symbol's 1-based start line, defaulting to 1 when
// unknown.
func symbolStartLine(n *Node) int {
	if l := getSymbolLine(n); l > 0 {
		return l
	}
	return 1
}

// symbolBody returns the source text spanning a symbol's body, bounded by the
// next symbol's start line.
func symbolBody(lines []string, symbols []*Node, idx, startLine int) string {
	endLine := len(lines)
	if idx+1 < len(symbols) {
		if nextLine := getSymbolLine(symbols[idx+1]); nextLine > startLine {
			endLine = nextLine - 1
		}
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	return strings.Join(lines[startLine-1:endLine], "\n")
}

// heuristicCallerEdges scans one symbol body for references to other project
// symbols and emits INFERRED call edges.
func heuristicCallerEdges(caller *Node, candidates []*Node, bodyText string, startLine int) []*Edge {
	words := tokenizeText(bodyText)
	var edges []*Edge
	for _, callee := range candidates {
		if callee.ID == caller.ID {
			continue
		}
		relLine, exists := words[callee.Label]
		if !exists {
			continue
		}
		edges = append(edges, &Edge{
			ID:             fmt.Sprintf("%s-%s-calls", caller.ID, callee.ID),
			SourceID:       caller.ID,
			TargetID:       callee.ID,
			RelationType:   schema.EdgeCalls,
			Confidence:     "INFERRED",
			SourceLocation: fmt.Sprintf("L%d", startLine+relLine-1),
		})
	}
	return edges
}

func getLanguageGroup(ext string) string {
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js", ".jsx", ".ts", ".tsx", ".astro", ".vue", ".svelte":
		return "js"
	case ".php":
		return "php"
	case ".css":
		return "css"
	case ".sh":
		return "bash"
	case ".lua":
		return "lua"
	case ".rb":
		return "ruby"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	}
	return ""
}

func sortSymbolsByLine(symbols []*Node) {
	sort.Slice(symbols, func(i, j int) bool {
		return getSymbolLine(symbols[i]) < getSymbolLine(symbols[j])
	})
}

func getSymbolLine(n *Node) int {
	if n.Metadata == nil {
		return 0
	}
	if val, ok := n.Metadata["line"]; ok {
		switch v := val.(type) {
		case int:
			return v
		case float64:
			return int(v)
		case int64:
			return int(v)
		}
	}
	return 0
}
