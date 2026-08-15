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

	// Calculate exports_count for javascript-like languages.
	if regexExt, ok := currentExtractor.(*extractor.RegexExtractor); ok {
		meta["exports_count"] = regexExt.GetExportsCount(content, ext)
	} else if tsExt, ok := currentExtractor.(*extractor.TreeSitterExtractor); ok {
		// Use a temporary regex extractor helper for exports count in Hito 3.2.
		meta["exports_count"] = extractor.NewRegexExtractor().GetExportsCount(content, ext)
		_ = tsExt
	}

	symbols, _, err := currentExtractor.Extract(content, relPath, ext)
	if err != nil {
		return nil, meta
	}

	var symbolNodes []*Node
	var rationales []string
	for _, sym := range symbols {
		if sym.Type == "rationale" {
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

//nolint:gocyclo // long denylist of invalid symbol characters; refactor later
func isValidSymbolName(name string) bool {
	if name == "" {
		return false
	}
	// Reject names with whitespace, braces, brackets, or operators — those are
	// code fragments or malformed extractions, not valid symbols.
	for _, r := range name {
		if r == '{' || r == '}' || r == '(' || r == ')' || r == '[' || r == ']' {
			return false
		}
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return false
		}
		if r == '=' || r == '+' || r == '*' || r == '/' || r == '%' {
			return false
		}
		if r == '<' || r == '>' || r == '&' || r == '|' || r == '!' || r == '^' || r == '~' {
			return false
		}
	}
	// Reject names that are just punctuation
	hasLetter := false
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			hasLetter = true
			break
		}
	}
	return hasLetter
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

	type parseResult struct {
		sourcePath string
		ext        string
		imports    []string
		err        error
	}

	bufSize := numWorkers * 2
	if len(toParse) < bufSize {
		bufSize = len(toParse)
	}
	jobs := make(chan string, bufSize)
	results := make(chan parseResult, bufSize)

	for w := 0; w < numWorkers; w++ {
		go func() {
			for sourcePath := range jobs {
				ext := strings.ToLower(filepath.Ext(sourcePath))
				var content []byte
				var err error
				if cached, ok := fileContents[sourcePath]; ok {
					content = cached
				} else {
					absPath := filepath.Join(projPath, sourcePath)
					content, err = os.ReadFile(absPath)
				}
				if err != nil {
					results <- parseResult{sourcePath: sourcePath, ext: ext, err: err}
					continue
				}

				_, imports, err := currentExtractor.Extract(content, sourcePath, ext)
				if err != nil {
					results <- parseResult{sourcePath: sourcePath, ext: ext, err: err}
					continue
				}

				results <- parseResult{sourcePath: sourcePath, ext: ext, imports: imports}
			}
		}()
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
		for _, imp := range res.imports {
			var targetID string
			var found bool
			if res.ext == ".md" {
				targetID, found = resolveMarkdownLink(projPath, res.sourcePath, imp, nodes)
			} else {
				targetID, found = resolveImport(projPath, res.sourcePath, imp, nodes)
			}

			if found {
				relType := "imports"
				if res.ext == ".md" {
					relType = "references"
				}
				edgeID := fmt.Sprintf("%s-%s-%s", res.sourcePath, targetID, relType)
				edges = append(edges, &Edge{
					ID:           edgeID,
					SourceID:     res.sourcePath,
					TargetID:     targetID,
					RelationType: relType,
					Confidence:   "EXTRACTED",
				})
			} else if res.ext != ".md" && isExternalPkg(imp) && !isStdlib(imp, res.ext) {
				pkgNodeID := "pkg:" + imp
				if _, exists := nodes[pkgNodeID]; !exists {
					nodes[pkgNodeID] = &Node{
						ID:    pkgNodeID,
						Type:  "package",
						Label: imp,
						Path:  imp,
					}
				}
				edgeID := fmt.Sprintf("%s-%s-%s", res.sourcePath, pkgNodeID, "imports")
				edges = append(edges, &Edge{
					ID:           edgeID,
					SourceID:     res.sourcePath,
					TargetID:     pkgNodeID,
					RelationType: "imports",
					Confidence:   "EXTRACTED",
				})
			}
		}
	}
	return edges
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
					Type:  "package",
					Label: dep,
					Path:  dep,
				}
			}
			edgeID := fmt.Sprintf("%s-%s-depends_on", mf, pkgID)
			edges = append(edges, &Edge{
				ID:           edgeID,
				SourceID:     mf,
				TargetID:     pkgID,
				RelationType: "depends_on",
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
