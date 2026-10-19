package graph

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/svtech-code/sv-memory/internal/graph/extractor"
	"github.com/svtech-code/sv-memory/internal/graph/schema"
	"github.com/svtech-code/sv-memory/internal/security"
)

// DefaultMaxHops bounds the shortest-path search in both the CLI
// (`sv-memory graph path`) and the MCP tool (`sv_graph_path`), so the two
// surfaces agree without duplicating the literal.
const DefaultMaxHops = 10

// DefaultTopCommunities is the default number of communities listed by the CLI
// and MCP graph community surfaces (sv_graph_communities / `graph communities`).
const DefaultTopCommunities = 10

// Node represents a vertex in the code dependency graph.
type Node = schema.Node

// Edge represents a directed relationship between two nodes.
type Edge = schema.Edge

// Extension to human-readable language mapping.
var languageFromExt = map[string]string{
	".go":     "go",
	".py":     "python",
	".js":     "javascript",
	".jsx":    "javascript",
	".ts":     "typescript",
	".tsx":    "typescript",
	".html":   "html",
	".php":    "php",
	".css":    "css",
	".astro":  "astro",
	".sh":     "bash",
	".lua":    "lua",
	".rb":     "ruby",
	".rs":     "rust",
	".java":   "java",
	".vue":    "vue",
	".svelte": "svelte",
	".md":     "markdown",
	".sql":    "sql",
}

// Well-known entry point file names.
var entryPointNames = map[string]bool{
	"main.go": true, "index.go": true,
	"index.js": true, "main.js": true, "app.js": true, "cli.js": true, "server.js": true,
	"index.ts": true, "main.ts": true, "app.ts": true, "cli.ts": true, "server.ts": true,
	"index.jsx": true, "index.tsx": true,
	"__main__.py": true, "main.py": true, "cli.py": true, "app.py": true,
	"index.php": true, "index.html": true, "index.htm": true,
}

// Current parser engine mapping to tree-sitter or regex fallback.
var currentExtractor extractor.Extractor = extractor.NewTreeSitterExtractor()

// parseSymbols reads file content and returns child nodes (function/class) plus
// enriched metadata for the parent file node.
func parseSymbols(relPath, ext string, content []byte) ([]*Node, map[string]interface{}) {
	lines := strings.Split(string(content), "\n")
	loc := len(lines)
	lang := languageFromExt[ext]

	meta := map[string]interface{}{
		"language":      lang,
		"loc":           loc,
		"entry_point":   entryPointNames[filepath.Base(relPath)],
		"exports_count": 0,
	}

	// exports_count for javascript-like languages is computed with the regex
	// extractor regardless of the primary parser engine (cheap and accurate for
	// export statements; the tree-sitter extractor delegates to the same logic).
	meta["exports_count"] = extractor.NewRegexExtractor().GetExportsCount(content, ext)

	symbols, _, err := currentExtractor.Extract(content, relPath, ext)
	if err != nil {
		return nil, meta
	}

	var symbolNodes []*Node
	var rationales []string
	for _, sym := range symbols {
		if sym.Type == schema.NodeTypeRationale {
			if sym.Name != "" {
				rationales = append(rationales, security.SanitizeText(sym.Name))
			}
			continue
		}
		// Section headings, code blocks, and diagrams can have spaces in their names
		isStructural := sym.Type == "section" || sym.Type == "code_block" || sym.Type == "diagram"
		if !isStructural && !isValidSymbolName(sym.Name) {
			continue
		}
		// Structural names and rationale text derive from file content and may
		// embed secrets (e.g. "## API key: sk-ant-..."). Redact them before the
		// name becomes a node id/label, so the id stays consistent with the
		// contains-edges built from the node map.
		sym.Name = security.SanitizeText(sym.Name)
		symMeta := map[string]interface{}{
			"line":     sym.Line,
			"exported": sym.Exported,
		}
		// Carry over any extra metadata from the extractor
		for k, v := range sym.Metadata {
			symMeta[k] = v
		}
		// For code_blocks without explicit name, use line-based identifier
		nodeID := relPath + ":" + sym.Name
		if sym.Name == "" {
			nodeID = relPath + ":L" + fmt.Sprint(sym.Line)
		}
		symbolNodes = append(symbolNodes, &schema.Node{
			ID:       nodeID,
			Type:     sym.Type,
			Label:    sym.Name,
			Path:     relPath,
			Metadata: symMeta,
		})
	}
	if len(rationales) > 0 {
		meta["rationales"] = rationales
	}

	return symbolNodes, meta
}

// invalidSymbolRunes are characters that mark a code fragment or a malformed
// extraction rather than a valid symbol name: whitespace, braces, brackets,
// and operators.
var invalidSymbolRunes = func() map[rune]bool {
	set := make(map[rune]bool)
	for _, r := range "{}()[] \t\n\r=+*/%<>&|!^~" {
		set[r] = true
	}
	return set
}()

func isValidSymbolName(name string) bool {
	if name == "" {
		return false
	}
	hasIdentifier := false
	for _, r := range name {
		if invalidSymbolRunes[r] {
			return false
		}
		if isSymbolIdentifierRune(r) {
			hasIdentifier = true
		}
	}
	// Reject names that are just punctuation.
	return hasIdentifier
}

func isSymbolIdentifierRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
}

// parseResult is the outcome of parsing a single file's imports.
type parseResult struct {
	sourcePath string
	ext        string
	imports    []string
	err        error
}

// parseFiles concurrently parses imports for the given file list using a
// bounded worker pool. Returns edges for all files.
func parseFiles(projPath string, nodes map[string]*Node, toParse []string, fileContents map[string][]byte) []*Edge {
	if len(toParse) == 0 {
		return nil
	}

	numWorkers := runtime.NumCPU()
	if numWorkers < 1 {
		numWorkers = 1
	}
	if len(toParse) < numWorkers {
		numWorkers = len(toParse)
	}

	bufSize := numWorkers * 2
	if len(toParse) < bufSize {
		bufSize = len(toParse)
	}
	jobs := make(chan string, bufSize)
	results := make(chan parseResult, bufSize)

	for w := 0; w < numWorkers; w++ {
		go parseWorker(projPath, jobs, results, fileContents)
	}

	go func() {
		for _, sourcePath := range toParse {
			jobs <- sourcePath
		}
		close(jobs)
	}()

	var edges []*Edge
	for i := 0; i < len(toParse); i++ {
		res := <-results
		if res.err != nil {
			continue
		}
		edges = append(edges, edgesForImportResult(projPath, res, nodes)...)
	}
	return edges
}

// parseWorker consumes file paths from jobs and emits a parseResult per file.
func parseWorker(projPath string, jobs <-chan string, results chan<- parseResult, fileContents map[string][]byte) {
	for sourcePath := range jobs {
		results <- parseOneFile(projPath, sourcePath, fileContents)
	}
}

// parseOneFile parses a single file's imports, preferring cached content.
func parseOneFile(projPath, sourcePath string, fileContents map[string][]byte) parseResult {
	ext := strings.ToLower(filepath.Ext(sourcePath))
	content, ok := fileContents[sourcePath]
	if !ok {
		var err error
		content, err = os.ReadFile(filepath.Join(projPath, sourcePath))
		if err != nil {
			return parseResult{sourcePath: sourcePath, ext: ext, err: err}
		}
	}
	_, imports, err := currentExtractor.Extract(content, sourcePath, ext)
	if err != nil {
		return parseResult{sourcePath: sourcePath, ext: ext, err: err}
	}
	return parseResult{sourcePath: sourcePath, ext: ext, imports: imports}
}

// edgesForImportResult turns a parsed file's imports into import/reference edges,
// synthesising package nodes for external imports.
func edgesForImportResult(projPath string, res parseResult, nodes map[string]*Node) []*Edge {
	var edges []*Edge
	for _, imp := range res.imports {
		if edge, ok := importEdge(projPath, res, imp, nodes); ok {
			edges = append(edges, edge)
		}
	}
	return edges
}

// importEdge resolves a single import to an edge. Returns (edge, true) for both
// resolvable internal imports and synthesised external package imports.
func importEdge(projPath string, res parseResult, imp string, nodes map[string]*Node) (*Edge, bool) {
	if res.ext == ".md" {
		targetID, found := resolveMarkdownLink(projPath, res.sourcePath, imp, nodes)
		if !found {
			return nil, false
		}
		return newImportEdge(res.sourcePath, targetID, schema.EdgeReferences), true
	}

	if targetID, found := resolveImport(projPath, res.sourcePath, imp, nodes); found {
		return newImportEdge(res.sourcePath, targetID, schema.EdgeImports), true
	}

	if !isExternalPkg(imp) || isStdlib(imp, res.ext) {
		return nil, false
	}
	pkgNodeID := "pkg:" + imp
	if _, exists := nodes[pkgNodeID]; !exists {
		nodes[pkgNodeID] = &Node{
			ID:    pkgNodeID,
			Type:  schema.NodeTypePackage,
			Label: imp,
			Path:  imp,
		}
	}
	return newImportEdge(res.sourcePath, pkgNodeID, schema.EdgeImports), true
}

func newImportEdge(sourceID, targetID, relType string) *Edge {
	return &Edge{
		ID:           fmt.Sprintf("%s-%s-%s", sourceID, targetID, relType),
		SourceID:     sourceID,
		TargetID:     targetID,
		RelationType: relType,
		Confidence:   "EXTRACTED",
	}
}

// parseManifests reads known manifest files and returns depends_on edges.
func parseManifests(projPath string, nodes map[string]*Node, manifests []string) []*Edge {
	var edges []*Edge
	for _, mf := range manifests {
		absPath := filepath.Join(projPath, mf)
		content, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}
		var deps []string
		switch mf {
		case "package.json":
			deps = parsePackageJSON(content)
		case "go.mod":
			deps = parseGoMod(content)
		case "requirements.txt":
			deps = parseRequirementsTXT(content)
		case "Cargo.toml":
			deps = parseCargoToml(content)
		case "composer.json":
			deps = parseComposerJSON(content)
		case "Gemfile":
			deps = parseGemfile(content)
		}
		for _, dep := range deps {
			pkgID := "pkg:" + dep
			if _, exists := nodes[pkgID]; !exists {
				nodes[pkgID] = &Node{
					ID:    pkgID,
					Type:  schema.NodeTypePackage,
					Label: dep,
					Path:  dep,
				}
			}
			edgeID := fmt.Sprintf("%s-%s-depends_on", mf, pkgID)
			edges = append(edges, &Edge{
				ID:           edgeID,
				SourceID:     mf,
				TargetID:     pkgID,
				RelationType: schema.EdgeDependsOn,
				Confidence:   "INFERRED",
			})
		}
	}
	return edges
}

func parsePackageJSON(content []byte) []string {
	var parsed struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(content, &parsed); err != nil {
		return nil
	}
	var deps []string
	for name := range parsed.Dependencies {
		deps = append(deps, name)
	}
	for name := range parsed.DevDependencies {
		deps = append(deps, name)
	}
	return deps
}

func parseComposerJSON(content []byte) []string {
	// composer.json uses "require" and "require-dev" instead of npm's
	// "dependencies" / "devDependencies".
	var parsed struct {
		Require    map[string]string `json:"require"`
		RequireDev map[string]string `json:"require-dev"`
	}
	if err := json.Unmarshal(content, &parsed); err != nil {
		return nil
	}
	var deps []string
	for name := range parsed.Require {
		deps = append(deps, name)
	}
	for name := range parsed.RequireDev {
		deps = append(deps, name)
	}
	return deps
}

func parseGoMod(content []byte) []string {
	var deps []string
	for _, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "module ") || strings.HasPrefix(trimmed, "go ") || strings.HasPrefix(trimmed, "require ") || strings.HasPrefix(trimmed, "toolchain ") || trimmed == ")" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		parts := strings.Fields(trimmed)
		if len(parts) >= 2 {
			deps = append(deps, parts[0])
		}
	}
	return deps
}

func parseRequirementsTXT(content []byte) []string {
	var deps []string
	for _, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "-") {
			continue
		}
		parts := strings.FieldsFunc(trimmed, func(r rune) bool {
			return r == '=' || r == '>' || r == '<' || r == '~' || r == '!' || r == '@' || r == ';' || r == ' '
		})
		if len(parts) > 0 {
			deps = append(deps, strings.TrimSpace(parts[0]))
		}
	}
	return deps
}

func parseCargoToml(content []byte) []string {
	var deps []string
	inDeps := false
	for _, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[dependencies]") {
			inDeps = true
			continue
		}
		if strings.HasPrefix(trimmed, "[") && inDeps {
			break // next section
		}
		if inDeps && strings.Contains(trimmed, "=") && !strings.HasPrefix(trimmed, "#") {
			parts := strings.SplitN(trimmed, "=", 2)
			name := strings.TrimSpace(parts[0])
			if name != "" && !strings.HasPrefix(name, "#") {
				deps = append(deps, name)
			}
		}
	}
	return deps
}

func parseGemfile(content []byte) []string {
	var deps []string
	gemRe := regexp.MustCompile(`(?m)^\s*gem\s+['"]([^'"]+)['"]`)
	for _, m := range gemRe.FindAllSubmatch(content, -1) {
		if len(m) > 1 {
			deps = append(deps, string(m[1]))
		}
	}
	return deps
}
