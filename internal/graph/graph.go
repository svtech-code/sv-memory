package graph

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Node represents a vertex in the code dependency graph.
type Node struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"` // 'file' | 'module' | 'package' | 'component'
	Label    string                 `json:"label"`
	Path     string                 `json:"path"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// Edge represents a directed relationship between two nodes.
type Edge struct {
	ID             string `json:"id"`
	SourceID       string `json:"source_id"`
	TargetID       string `json:"target_id"`
	RelationType   string `json:"relation_type"`   // 'imports' | 'calls' | 'depends_on'
	Confidence     string `json:"confidence"`       // 'EXTRACTED' | 'INFERRED' | 'AMBIGUOUS'
	SourceLocation string `json:"source_location,omitempty"` // line number or empty
}

// Extension to human-readable language mapping.
var languageFromExt = map[string]string{
	".go":   "go",
	".py":   "python",
	".js":   "javascript",
	".jsx":  "javascript",
	".ts":   "typescript",
	".tsx":  "typescript",
	".html": "html",
	".php":  "php",
	".css":  "css",
	".astro": "astro",
	".sh":   "bash",
	".lua":  "lua",
	".rb":   "ruby",
	".rs":   "rust",
	".java": "java",
	".vue":  "vue",
	".svelte": "svelte",
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

// Symbol detection regexes (heuristic, not AST-based).
var (
	jsFuncRegex   = regexp.MustCompile(`(?m)(?:export\s+)?(?:async\s+)?function\s+(\w+)`)
	jsClassRegex  = regexp.MustCompile(`(?m)(?:export\s+)?class\s+(\w+)`)
	jsExportRegex = regexp.MustCompile(`(?m)(?:export\s+(?:default\s+)?(?:function|class|const|let|var)\s+\w+|module\.exports\s*=|exports\.\w+)`)

	pyFuncRegex   = regexp.MustCompile(`(?m)^\s*(?:async\s+)?def\s+(\w+)`)
	pyClassRegex  = regexp.MustCompile(`(?m)^\s*class\s+(\w+)`)

	goFuncRegex   = regexp.MustCompile(`(?m)^\s*func\s+(\w+)`)
	goStructRegex = regexp.MustCompile(`(?m)^\s*type\s+(\w+)\s+struct`)

	phpFuncRegex  = regexp.MustCompile(`(?m)(?:function\s+(\w+)|class\s+(\w+))`)
)

// Supported extensions for symbol scanning.
var symbolScanExts = map[string]bool{
	".go": true, ".py": true, ".js": true, ".jsx": true, ".ts": true, ".tsx": true,
	".php": true, ".astro": true, ".lua": true, ".rb": true, ".rs": true, ".java": true,
	".vue": true, ".svelte": true,
}

// parseSymbols reads file content and returns child nodes (function/class) plus
// enriched metadata for the parent file node. The caller adds the returned nodes
// to the global node map.
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

	var symbols []*Node
	addSymbol := func(name, symType string, exported bool, line int) {
		id := relPath + ":" + name
		symbols = append(symbols, &Node{
			ID:    id,
			Type:  symType,
			Label: name,
			Path:  relPath,
			Metadata: map[string]interface{}{
				"line":     line,
				"exported": exported,
			},
		})
	}

	switch ext {
	case ".js", ".jsx", ".ts", ".tsx", ".astro":
		// Functions
		for _, m := range jsFuncRegex.FindAllSubmatch(content, -1) {
			if len(m) > 1 {
				name := string(m[1])
				exported := strings.HasPrefix(string(m[0]), "export")
				line := findLineNumber(lines, string(m[0]))
				addSymbol(name, "function", exported, line)
			}
		}
		// Classes
		for _, m := range jsClassRegex.FindAllSubmatch(content, -1) {
			if len(m) > 1 {
				name := string(m[1])
				exported := strings.HasPrefix(string(m[0]), "export")
				line := findLineNumber(lines, string(m[0]))
				addSymbol(name, "class", exported, line)
			}
		}
		// Export count
		meta["exports_count"] = countMatches(jsExportRegex, content)

	case ".py":
		for _, m := range pyFuncRegex.FindAllSubmatch(content, -1) {
			if len(m) > 1 {
				name := string(m[1])
				line := findLineNumber(lines, string(m[0]))
				addSymbol(name, "function", true, line)
			}
		}
		for _, m := range pyClassRegex.FindAllSubmatch(content, -1) {
			if len(m) > 1 {
				name := string(m[1])
				line := findLineNumber(lines, string(m[0]))
				addSymbol(name, "class", true, line)
			}
		}

	case ".go":
		for _, m := range goFuncRegex.FindAllSubmatch(content, -1) {
			if len(m) > 1 {
				name := string(m[1])
				if name == "init" || name == "main" {
					continue // skip built-in Go entry points
				}
				isExported := name[0] >= 'A' && name[0] <= 'Z'
				line := findLineNumber(lines, string(m[0]))
				addSymbol(name, "function", isExported, line)
			}
		}
		// Treat exported structs as "class" nodes
		for _, m := range goStructRegex.FindAllSubmatch(content, -1) {
			if len(m) > 1 {
				name := string(m[1])
				isExported := name[0] >= 'A' && name[0] <= 'Z'
				line := findLineNumber(lines, string(m[0]))
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
						line := findLineNumber(lines, string(m[0]))
						addSymbol(name, symType, true, line)
					}
				}
			}
		}
	}

	return symbols, meta
}

func findLineNumber(lines []string, substr string) int {
	substr = strings.TrimSpace(substr)
	for i, line := range lines {
		if strings.Contains(line, substr) {
			return i + 1
		}
	}
	return 0
}

func countMatches(re *regexp.Regexp, content []byte) int {
	return len(re.FindAll(content, -1))
}

// Common directories to ignore during code scanning.
var ignoreDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"dist":         true,
	"build":        true,
	"out":          true,
	".sv-memory":   true,
	".config":      true,
	"__pycache__":  true,
	".venv":        true,
	"venv":         true,
	".idea":        true,
	".vscode":      true,
}

// Language import regex patterns.
var (
	// JS/TS: matches import ... from 'path' or require('path') or import('path')
	jsImportRegex = regexp.MustCompile(`(?m)(?:import\s+(?:[^'"]+\s+from\s+)?['"]([^'"]+)['"])|(?:require\s*\(\s*['"]([^'"]+)['"]\s*\))`)

	// Python: matches import x, y or from x import y, optionally multiline with ()
	pyImportRegex = regexp.MustCompile(`(?m)(?:import\s+([\w\.,\s]+)|from\s+([\w\.]+)\s+import\s*(?:\(([^)]+)\)|([\w\.,\s]+)))`)

	// Go: matches import "path" or import (...) block
	goImportRegex = regexp.MustCompile("(?m)(?:import\\s+[\"`]([^\"`]+)[\"`])|(?:import\\s*\\(([\\s\\S]*?)\\))")
	goImportBlockRegex = regexp.MustCompile("(?m)[\"`]([^\"`]+)[\"`]")

	// Ruby: matches require 'path' or require_relative 'path'
	rbImportRegex = regexp.MustCompile(`(?m)(?:require|require_relative)\s+['"]([^'"]+)['"]`)

	// Rust: matches use path::to::module;
	rsImportRegex = regexp.MustCompile(`(?m)use\s+([\w\d:]+);`)

	// Java: matches import path.to.module;
	javaImportRegex = regexp.MustCompile(`(?m)import\s+([\w\.]+);`)

	// Vue/Svelte (js-like): matches import ... from 'path'
	vueImportRegex = jsImportRegex

	// PHP: matches include(_once)?/require(_once)? 'path' or namespace/use path
	phpImportRegex = regexp.MustCompile(`(?m)(?:include|require)(?:_once)?\s*\(?\s*['"]([^'"]+)['"]\s*\)?|use\s+([\w\\]+)(?:\s+as\s+\w+)?;`)

	// CSS: matches @import 'path' or @import url('path')
	cssImportRegex = regexp.MustCompile(`(?m)@import\s+(?:url\()?['"]([^'"]+)['"]`)

	// HTML: matches <script src="path"> or <link href="path">
	htmlImportRegex = regexp.MustCompile(`(?m)<script\s+[^>]*src=['"]([^'"]+)['"]|<link\s+[^>]*href=['"]([^'"]+)['"]`)

	// Markdown links: matches standard [label](target) and Obsidian [[target]] or [[target|label]]
	mdLinkRegex     = regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)
	mdWikilinkRegex = regexp.MustCompile(`\[\[([^\]|]+)(?:\|[^\]]*)?\]\]`)

	// Bash: matches source script.sh or . script.sh
	shImportRegex = regexp.MustCompile(`(?m)^\s*(?:source|\.)\s+['"]?([^'"\s#;]+)['"]?`)

	// Lua: matches require("module") or dofile("file.lua") or loadfile("file.lua")
	luaImportRegex = regexp.MustCompile(`(?m)(?:require|dofile|loadfile)\s*\(?\s*['"]([^'"]+)['"]\s*\)?`)
)

// SyncGraph scans the project directory and builds or incrementally updates the
// dependency graph stored in SQLite. It detects changed files via mtime/size
// and avoids reparsing unchanged files. When more than 30 % of tracked files
// have changed, or when no prior metadata exists, it falls back to a full
// rebuild (delete-all + rescan).
func SyncGraph(db *sql.DB, projectID string, projPath string) error {
	return syncGraph(db, projectID, projPath)
}

// SyncGraphFull forces a full rebuild: all existing nodes and edges for the
// project are deleted and re-scanned from disk. Use this for the CLI `graph
// rebuild` command. Called internally as a fallback when the incremental path
// detects too many changes.
func SyncGraphFull(db *sql.DB, projectID string, projPath string) error {
	return syncGraphFull(db, projectID, projPath)
}

// syncGraph dispatches to incremental or full rebuild.
func syncGraph(db *sql.DB, projectID string, projPath string) error {
	ok, err := trySyncGraphIncremental(db, projectID, projPath)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	// Fall back to full rebuild when incremental is not viable (first run or
	// too many changes).
	return syncGraphFull(db, projectID, projPath)
}

// walkResult carries the data collected during a single file scan pass.
type walkResult struct {
	nodes         map[string]*Node
	fileList      []string
	fileMeta      map[string]fileMetaEntry // relative path → mtime+size
	manifestFiles []string
}

type fileMetaEntry struct {
	mtimeMs int64
	size    int64
}

// Known manifest file names that declare project-level external dependencies.
var manifestFilenames = []string{"package.json", "go.mod", "requirements.txt", "Cargo.toml", "composer.json", "Gemfile"}

// scanFiles walks projPath and collects code files into a nodes map, a file
// list for parsing, and a metadata map for change detection. Shared between
// full and incremental paths.
func scanFiles(projPath string) (*walkResult, error) {
	nodes := make(map[string]*Node)
	fileList := []string{}
	fileMeta := make(map[string]fileMetaEntry)

	err := filepath.WalkDir(projPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if ignoreDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		relPath, relErr := filepath.Rel(projPath, path)
		if relErr != nil {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(relPath))
		switch ext {
		case ".go", ".py", ".js", ".jsx", ".ts", ".tsx", ".html", ".php", ".css", ".astro", ".sh", ".lua", ".rb", ".rs", ".java", ".vue", ".svelte", ".md":
			fi, fiErr := os.Stat(path)
			mtimeMs := int64(0)
			size := int64(0)
			if fiErr == nil {
				mtimeMs = fi.ModTime().UnixMilli()
				size = fi.Size()
			}

			baseMeta := map[string]interface{}{
				"extension": ext,
				"size":      size,
			}

			// Read file content for symbol detection and metadata enrichment.
			if symbolScanExts[ext] {
				content, readErr := os.ReadFile(path)
				if readErr == nil {
					symbolNodes, symMeta := parseSymbols(relPath, ext, content)
					for k, v := range symMeta {
						baseMeta[k] = v
					}
					for _, sn := range symbolNodes {
						nodes[sn.ID] = sn
					}
				}
			}

			nodeType := "file"
			if ext == ".md" {
				nodeType = "document"
			}
			nodes[relPath] = &Node{
				ID:       relPath,
				Type:     nodeType,
				Label:    filepath.Base(relPath),
				Path:     relPath,
				Metadata: baseMeta,
			}
			fileList = append(fileList, relPath)
			fileMeta[relPath] = fileMetaEntry{mtimeMs: mtimeMs, size: size}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed walking directory: %w", err)
	}
	// Detect manifest files in the project root and create nodes for them.
	var manifestFiles []string
	for _, mf := range manifestFilenames {
		mfPath := filepath.Join(projPath, mf)
		if fi, stErr := os.Stat(mfPath); stErr == nil {
			mtimeMs := fi.ModTime().UnixMilli()
			size := fi.Size()
			manifestFiles = append(manifestFiles, mf)

			nodes[mf] = &Node{
				ID:    mf,
				Type:  "file",
				Label: mf,
				Path:  mf,
				Metadata: map[string]interface{}{
					"size":      size,
					"extension": filepath.Ext(mf),
				},
			}
			fileList = append(fileList, mf)
			fileMeta[mf] = fileMetaEntry{mtimeMs: mtimeMs, size: size}
		}
	}
	return &walkResult{
		nodes:         nodes,
		fileList:      fileList,
		fileMeta:      fileMeta,
		manifestFiles: manifestFiles,
	}, nil
}

// parseFiles concurrently parses imports for the given file list using a
// bounded worker pool. Returns edges for all files; unresolved imports are
// treated as external package nodes (added to the nodes map).
func parseFiles(projPath string, nodes map[string]*Node, toParse []string) []*Edge {
	if len(toParse) == 0 {
		return nil
	}

	numWorkers := 8
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
				absPath := filepath.Join(projPath, sourcePath)
				content, err := os.ReadFile(absPath)
				ext := strings.ToLower(filepath.Ext(sourcePath))
				if err != nil {
					results <- parseResult{sourcePath: sourcePath, ext: ext, err: err}
					continue
				}
				var imports []string

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
						// Group 1: direct import x, y
						// Group 2: from x import ...
						// Group 3: from x import (a, b) - multiline match
						// Group 4: from x import a, b
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
					// Handle block imports
					for _, m := range goImportBlockRegex.FindAllSubmatch(content, -1) {
						if len(m) > 1 && len(m[1]) > 0 {
							imports = append(imports, string(m[1]))
						}
					}
				case ".php":
					// ... (same as before)
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

// parseManifests reads known manifest files (package.json, go.mod, etc.) and
// returns depends_on edges from the manifest to each declared external package.
// Packages are added to the nodes map for deduplication.
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
			deps = parsePackageJSON(content) // same structure
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

func parseGoMod(content []byte) []string {
	var deps []string
	for _, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		// Lines like: github.com/foo/bar v1.0.0
		// Skip module, go, require, toolchain lines
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
		// Handles: flask==2.0, flask>=2.0, flask, flask~=2.0
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

// syncGraphFull is the original delete-all-plus-rescan implementation.
func syncGraphFull(db *sql.DB, projectID string, projPath string) error {
	wr, err := scanFiles(projPath)
	if err != nil {
		return err
	}

	edges := parseFiles(projPath, wr.nodes, wr.fileList)
	manifestEdges := parseManifests(projPath, wr.nodes, wr.manifestFiles)
	edges = append(edges, manifestEdges...)

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec("DELETE FROM graph_edges WHERE project_id = ?", projectID)
	if err != nil {
		return fmt.Errorf("failed deleting old edges: %w", err)
	}
	_, err = tx.Exec("DELETE FROM graph_nodes WHERE project_id = ?", projectID)
	if err != nil {
		return fmt.Errorf("failed deleting old nodes: %w", err)
	}

	if err := bulkInsertNodes(tx, projectID, wr.nodes); err != nil {
		return err
	}
	if err := bulkInsertEdges(tx, projectID, edges); err != nil {
		return err
	}

	// Phase 2: Extract function and class calls
	callEdges := extractCallEdges(projPath, wr.nodes)
	if err := bulkInsertEdges(tx, projectID, callEdges); err != nil {
		return err
	}

	// Phase 1: Unify code graph with memories
	if err := syncMemoriesToGraph(tx, projectID); err != nil {
		return fmt.Errorf("failed syncing memories to graph: %w", err)
	}

	// Store fresh file metadata for future incremental runs.
	updateFileMeta(tx, projectID, wr.fileMeta)

	return tx.Commit()
}

// trySyncGraphIncremental attempts a partial graph update using file mtime/size
// comparison. Returns (true, nil) on success, (false, nil) when a full rebuild
// is recommended (no prior meta or too many changes), or (false, err) on
// database error.
func trySyncGraphIncremental(db *sql.DB, projectID string, projPath string) (bool, error) {
	wr, err := scanFiles(projPath)
	if err != nil {
		return false, err
	}

	// Load existing file metadata from the DB.
	oldMeta, err := loadFileMeta(db, projectID)
	if err != nil {
		return false, err
	}

	// First run — no prior metadata, do full rebuild.
	if len(oldMeta) == 0 {
		return false, nil
	}

	// Classify files as unchanged, new, changed, or deleted.
	var unchanged, toParse, deleted []string
	for _, p := range wr.fileList {
		cur := wr.fileMeta[p]
		prev, exists := oldMeta[p]
		if !exists {
			toParse = append(toParse, p)
		} else if cur.mtimeMs != prev.mtimeMs || cur.size != prev.size {
			toParse = append(toParse, p)
		} else {
			unchanged = append(unchanged, p)
		}
	}
	for p := range oldMeta {
		if _, stillExists := wr.fileMeta[p]; !stillExists {
			deleted = append(deleted, p)
		}
	}

	// If more than 30% of tracked files changed, fall back to full rebuild.
	totalTracked := len(oldMeta)
	churn := len(toParse) + len(deleted)
	if totalTracked > 0 && float64(churn)/float64(totalTracked) > 0.30 {
		return false, nil
	}

	tx, err := db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	// 1. Remove deleted files's nodes and edges (including child symbols).
	for _, p := range deleted {
		if _, e := tx.Exec("DELETE FROM graph_edges WHERE project_id = ? AND (source_id = ? OR source_id LIKE ? OR target_id = ? OR target_id LIKE ?)", projectID, p, p+":%", p, p+":%"); e != nil {
			return false, fmt.Errorf("failed deleting edges for deleted file %s: %w", p, e)
		}
		if _, e := tx.Exec("DELETE FROM graph_nodes WHERE project_id = ? AND (id = ? OR id LIKE ?)", projectID, p, p+":%"); e != nil {
			return false, fmt.Errorf("failed deleting nodes for deleted file %s: %w", p, e)
		}
		if _, e := tx.Exec("DELETE FROM graph_files_meta WHERE project_id = ? AND path = ?", projectID, p); e != nil {
			return false, fmt.Errorf("failed removing file meta for %s: %w", p, e)
		}
	}

	// 2. Remove old edges and child nodes for changed files.
	for _, p := range toParse {
		if _, e := tx.Exec("DELETE FROM graph_edges WHERE project_id = ? AND (source_id = ? OR source_id LIKE ?)", projectID, p, p+":%"); e != nil {
			return false, fmt.Errorf("failed deleting edges for changed file %s: %w", p, e)
		}
		// Remove stale child symbols (function/class nodes) that will be
		// re-created by scanFiles during step 3.
		if _, e := tx.Exec("DELETE FROM graph_nodes WHERE project_id = ? AND id LIKE ?", projectID, p+":%"); e != nil {
			return false, fmt.Errorf("failed deleting child nodes for changed file %s: %w", p, e)
		}
	}

	// Separate manifest files from code files before parsing.
	isManifest := make(map[string]bool, len(wr.manifestFiles))
	for _, mf := range wr.manifestFiles {
		isManifest[mf] = true
	}
	var codeToParse []string
	var manifestToParse []string
	for _, p := range toParse {
		if isManifest[p] {
			manifestToParse = append(manifestToParse, p)
		} else {
			codeToParse = append(codeToParse, p)
		}
	}

	// 3. Parse new+changed code files and insert nodes+edges.
	codeEdges := parseFiles(projPath, wr.nodes, codeToParse)
	for _, p := range codeToParse {
		upsertNode(tx, projectID, wr.nodes[p])
		// Also upsert child symbol nodes (functions/classes) for this file.
		prefix := p + ":"
		for id, node := range wr.nodes {
			if strings.HasPrefix(id, prefix) {
				if err := upsertNode(tx, projectID, node); err != nil {
					return false, fmt.Errorf("failed upserting child node %s: %w", id, err)
				}
			}
		}
	}
	// Also process changed manifests.
	var edges []*Edge
	edges = append(edges, codeEdges...)
	manifestEdges := parseManifests(projPath, wr.nodes, manifestToParse)
	edges = append(edges, manifestEdges...)
	// Upsert package nodes (from both code and manifest parsers).
	for _, node := range wr.nodes {
		if node.Type == "package" {
			if err := upsertNode(tx, projectID, node); err != nil {
				return false, fmt.Errorf("failed upserting package node %s: %w", node.ID, err)
			}
		}
	}
	if err := bulkInsertEdges(tx, projectID, edges); err != nil {
		return false, err
	}

	// 4. Update file metadata for added/changed files (code + manifests).
	for _, p := range toParse {
		m := wr.fileMeta[p]
		if _, e := tx.Exec("INSERT OR REPLACE INTO graph_files_meta (project_id, path, mtime_ms, size) VALUES (?, ?, ?, ?)", projectID, p, m.mtimeMs, m.size); e != nil {
			return false, fmt.Errorf("failed upserting file meta for %s: %w", p, e)
		}
	}

	// Phase 1: Unify code graph with memories (re-create all memory nodes/edges)
	if _, err := tx.Exec("DELETE FROM graph_nodes WHERE project_id = ? AND node_type = 'concept'", projectID); err != nil {
		return false, fmt.Errorf("failed deleting old memory nodes: %w", err)
	}
	if err := syncMemoriesToGraph(tx, projectID); err != nil {
		return false, fmt.Errorf("failed syncing memories to graph: %w", err)
	}

	// Phase 2: Extract and sync function and class calls (re-create all calls edges)
	if _, err := tx.Exec("DELETE FROM graph_edges WHERE project_id = ? AND relation_type = 'calls'", projectID); err != nil {
		return false, fmt.Errorf("failed deleting old call edges: %w", err)
	}
	callEdges := extractCallEdges(projPath, wr.nodes)
	if err := bulkInsertEdges(tx, projectID, callEdges); err != nil {
		return false, err
	}

	return true, tx.Commit()
}

// --- SQL helpers shared by full and incremental paths ---

func bulkInsertNodes(tx *sql.Tx, projectID string, nodes map[string]*Node) error {
	stmt, err := tx.Prepare("INSERT INTO graph_nodes (id, project_id, node_type, label, path, metadata) VALUES (?, ?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, node := range nodes {
		metaBytes, _ := json.Marshal(node.Metadata)
		metaStr := string(metaBytes)
		if metaStr == "null" {
			metaStr = "{}"
		}
		if _, e := stmt.Exec(node.ID, projectID, node.Type, node.Label, node.Path, metaStr); e != nil {
			return fmt.Errorf("failed inserting node %s: %w", node.ID, e)
		}
	}
	return nil
}

func bulkInsertEdges(tx *sql.Tx, projectID string, edges []*Edge) error {
	if len(edges) == 0 {
		return nil
	}
	stmt, err := tx.Prepare("INSERT INTO graph_edges (id, project_id, source_id, target_id, relation_type, confidence, source_location) VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING")
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, edge := range edges {
		confidence := edge.Confidence
		if confidence == "" {
			confidence = "EXTRACTED"
		}
		if _, e := stmt.Exec(edge.ID, projectID, edge.SourceID, edge.TargetID, edge.RelationType, confidence, edge.SourceLocation); e != nil {
			return fmt.Errorf("failed inserting edge %s: %w", edge.ID, e)
		}
	}
	return nil
}

func upsertNode(tx *sql.Tx, projectID string, node *Node) error {
	metaBytes, _ := json.Marshal(node.Metadata)
	metaStr := string(metaBytes)
	if metaStr == "null" {
		metaStr = "{}"
	}
	_, err := tx.Exec(`
		INSERT INTO graph_nodes (id, project_id, node_type, label, path, metadata)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id, project_id) DO UPDATE SET
			node_type = excluded.node_type,
			label = excluded.label,
			path = excluded.path,
			metadata = excluded.metadata
	`, node.ID, projectID, node.Type, node.Label, node.Path, metaStr)
	return err
}

func loadFileMeta(db *sql.DB, projectID string) (map[string]fileMetaEntry, error) {
	rows, err := db.Query("SELECT path, mtime_ms, size FROM graph_files_meta WHERE project_id = ?", projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	meta := make(map[string]fileMetaEntry)
	for rows.Next() {
		var path string
		var m fileMetaEntry
		if err := rows.Scan(&path, &m.mtimeMs, &m.size); err == nil {
			meta[path] = m
		}
	}
	return meta, rows.Err()
}

func updateFileMeta(tx *sql.Tx, projectID string, meta map[string]fileMetaEntry) {
	if len(meta) == 0 {
		return
	}
	for path, m := range meta {
		tx.Exec("INSERT OR REPLACE INTO graph_files_meta (project_id, path, mtime_ms, size) VALUES (?, ?, ?, ?)", projectID, path, m.mtimeMs, m.size)
	}
}

// resolveImport checks if an import maps to an existing file in the project.
func resolveImport(projPath, sourcePath, imp string, nodes map[string]*Node) (string, bool) {
	// If it's a relative path (starts with . or ..)
	if strings.HasPrefix(imp, ".") {
		sourceDir := filepath.Dir(sourcePath)
		resolvedRel := filepath.Clean(filepath.Join(sourceDir, imp))

		// Check options with extensions
		exts := []string{"", ".ts", ".js", ".tsx", ".jsx", ".astro", ".sh", ".lua", "/index.ts", "/index.js", "/index.tsx", "/index.jsx"}
		for _, ext := range exts {
			testPath := resolvedRel + ext
			if _, exists := nodes[testPath]; exists {
				return testPath, true
			}
		}
	}

	// Direct matching (e.g., matching from project root)
	if _, exists := nodes[imp]; exists {
		return imp, true
	}

	// Try resolving absolute imports from project root if it starts with slash
	if strings.HasPrefix(imp, "/") {
		testPath := strings.TrimPrefix(imp, "/")
		if _, exists := nodes[testPath]; exists {
			return testPath, true
		}
	}

	return "", false
}

// Stdlib module sets — imports matching these are not registered as pkg: nodes.
var (
	goStdlib = map[string]bool{
		"archive": true, "arena": true, "bufio": true, "bytes": true, "cmp": true,
		"compress": true, "container": true, "context": true, "crypto": true,
		"database": true, "debug": true, "embed": true, "encoding": true,
		"errors": true, "expvar": true, "flag": true, "fmt": true, "go": true,
		"hash": true, "html": true, "image": true, "index": true, "io": true,
		"iter": true, "log": true, "maps": true, "math": true, "mime": true,
		"net": true, "os": true, "path": true, "plugin": true, "reflect": true,
		"regexp": true, "runtime": true, "slices": true, "sort": true,
		"strconv": true, "strings": true, "structs": true, "sync": true,
		"syscall": true, "testing": true, "text": true, "time": true,
		"unicode": true, "unique": true, "unsafe": true,
	}

	pyStdlib = map[string]bool{
		"abc": true, "argparse": true, "array": true, "ast": true, "asyncio": true,
		"base64": true, "binascii": true, "bisect": true, "builtins": true,
		"calendar": true, "collections": true, "configparser": true, "contextlib": true,
		"copy": true, "csv": true, "ctypes": true, "dataclasses": true,
		"datetime": true, "decimal": true, "difflib": true, "dis": true,
		"email": true, "enum": true, "errno": true, "functools": true,
		"gc": true, "getpass": true, "glob": true, "gzip": true, "hashlib": true,
		"heapq": true, "hmac": true, "html": true, "http": true, "importlib": true,
		"inspect": true, "io": true, "itertools": true, "json": true,
		"logging": true, "lzma": true, "math": true, "multiprocessing": true,
		"operator": true, "os": true, "pathlib": true, "pickle": true,
		"platform": true, "pprint": true, "profile": true, "pstats": true,
		"queue": true, "random": true, "re": true, "readline": true,
		"reprlib": true, "resource": true, "runpy": true, "selectors": true,
		"shlex": true, "shutil": true, "signal": true, "site": true,
		"smtplib": true, "socket": true, "socketserver": true, "sqlite3": true,
		"ssl": true, "stat": true, "statistics": true, "string": true,
		"struct": true, "subprocess": true, "symtable": true, "sys": true,
		"tabnanny": true, "tarfile": true, "tempfile": true, "textwrap": true,
		"threading": true, "time": true, "timeit": true, "tkinter": true,
		"tokenize": true, "tomllib": true, "trace": true, "traceback": true,
		"tracemalloc": true, "turtle": true, "types": true, "typing": true,
		"unicodedata": true, "unittest": true, "urllib": true, "uuid": true,
		"venv": true, "warnings": true, "wave": true, "weakref": true,
		"winreg": true, "winsound": true, "xml": true, "xmlrpc": true,
		"zipfile": true, "zipimport": true, "zoneinfo": true, "__future__": true,
	}

	nodeStdlib = map[string]bool{
		"assert": true, "async_hooks": true, "buffer": true, "child_process": true,
		"cluster": true, "console": true, "constants": true, "crypto": true,
		"dgram": true, "diagnostics_channel": true, "dns": true, "domain": true,
		"events": true, "fs": true, "http": true, "http2": true, "https": true,
		"inspector": true, "module": true, "net": true, "os": true, "path": true,
		"perf_hooks": true, "process": true, "punycode": true, "querystring": true,
		"readline": true, "repl": true, "stream": true, "string_decoder": true,
		"sys": true, "timers": true, "tls": true, "trace_events": true,
		"tty": true, "url": true, "util": true, "v8": true, "vm": true,
		"wasi": true, "worker_threads": true, "zlib": true,
	}
)

// isStdlib returns true if the import is a built-in module for the given
// language extension (e.g. "os" in Python, "fmt" in Go, "fs" in Node).
func isStdlib(imp, ext string) bool {
	switch ext {
	case ".go":
		return goStdlib[imp]
	case ".py":
		return pyStdlib[imp]
	case ".js", ".ts", ".jsx", ".tsx":
		return nodeStdlib[imp]
	}
	return false
}

// isExternalPkg returns true if the import path represents a external library.
func isExternalPkg(imp string) bool {
	// Simple rule: if it doesn't start with "." and doesn't contain a path separator
	// (or contains a path separator but is a standard package layout like github.com/...)
	if strings.HasPrefix(imp, ".") || strings.HasPrefix(imp, "/") {
		return false
	}
	// Ignore empty imports
	if strings.TrimSpace(imp) == "" {
		return false
	}
	return true
}

func getFileSize(path string) int64 {
	stat, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return stat.Size()
}

// syncMemoriesToGraph queries all memories for the project and creates 'concept' nodes
// and 'rationale_for' edges linking them to files in the structural codebase graph.
func syncMemoriesToGraph(tx *sql.Tx, projectID string) error {
	rows, err := tx.Query("SELECT id, category, what, where_path, why, learned FROM memories WHERE project_id = ? AND deleted_at IS NULL", projectID)
	if err != nil {
		return fmt.Errorf("failed to query memories for graph sync: %w", err)
	}
	defer rows.Close()

	type memoryNode struct {
		id        string
		category  string
		what      string
		wherePath string
		why       string
		learned   string
	}
	var memories []memoryNode
	for rows.Next() {
		var m memoryNode
		var wherePath sql.NullString
		if errScan := rows.Scan(&m.id, &m.category, &m.what, &wherePath, &m.why, &m.learned); errScan == nil {
			if wherePath.Valid {
				m.wherePath = wherePath.String
			}
			memories = append(memories, m)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	nodeStmt, err := tx.Prepare("INSERT INTO graph_nodes (id, project_id, node_type, label, path, metadata) VALUES (?, ?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer nodeStmt.Close()

	edgeStmt, err := tx.Prepare("INSERT INTO graph_edges (id, project_id, source_id, target_id, relation_type, confidence, source_location) VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING")
	if err != nil {
		return err
	}
	defer edgeStmt.Close()

	for _, m := range memories {
		metaMap := map[string]interface{}{
			"category": m.category,
			"why":      m.why,
			"learned":  m.learned,
		}
		metaBytes, _ := json.Marshal(metaMap)

		_, err = nodeStmt.Exec(m.id, projectID, "concept", m.what, m.wherePath, string(metaBytes))
		if err != nil {
			return fmt.Errorf("failed to insert memory node %s: %w", m.id, err)
		}

		if m.wherePath != "" {
			cleanedPath := filepath.Clean(m.wherePath)
			cleanedPath = strings.ReplaceAll(cleanedPath, "\\", "/")
			cleanedPath = strings.TrimPrefix(cleanedPath, "./")

			var count int
			errRow := tx.QueryRow("SELECT COUNT(*) FROM graph_nodes WHERE project_id = ? AND id = ?", projectID, cleanedPath).Scan(&count)
			if errRow == nil && count > 0 {
				edgeID := fmt.Sprintf("%s-%s-rationale_for", m.id, cleanedPath)
				_, errEdge := edgeStmt.Exec(edgeID, projectID, m.id, cleanedPath, "rationale_for", "EXTRACTED", nil)
				if errEdge != nil {
					return fmt.Errorf("failed to insert rationale edge for memory %s: %w", m.id, errEdge)
				}
			}
		}
	}

	return nil
}

// extractCallEdges identifies call relationships (functions calling other functions/classes)
// within the project by scanning function body source code.
func extractCallEdges(projPath string, nodes map[string]*Node) []*Edge {
	var edges []*Edge

	langSymbols := make(map[string][]*Node)
	for _, node := range nodes {
		if node.Type == "function" || node.Type == "class" {
			ext := strings.ToLower(filepath.Ext(node.Path))
			lang := getLanguageGroup(ext)
			if lang != "" {
				langSymbols[lang] = append(langSymbols[lang], node)
			}
		}
	}

	fileSymbols := make(map[string][]*Node)
	for _, node := range nodes {
		if node.Type == "function" || node.Type == "class" {
			fileSymbols[node.Path] = append(fileSymbols[node.Path], node)
		}
	}

	for filePath, symbols := range fileSymbols {
		absPath := filepath.Join(projPath, filePath)
		content, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}
		lines := strings.Split(string(content), "\n")
		if len(lines) == 0 {
			continue
		}

		sortSymbolsByLine(symbols)

		ext := strings.ToLower(filepath.Ext(filePath))
		lang := getLanguageGroup(ext)
		if lang == "" {
			continue
		}
		candidates := langSymbols[lang]

		for idx, caller := range symbols {
			startLine := getSymbolLine(caller)
			if startLine <= 0 {
				startLine = 1
			}

			endLine := len(lines)
			if idx+1 < len(symbols) {
				nextLine := getSymbolLine(symbols[idx+1])
				if nextLine > startLine {
					endLine = nextLine - 1
				}
			}

			if startLine > len(lines) {
				continue
			}
			if endLine > len(lines) {
				endLine = len(lines)
			}

			bodyLines := lines[startLine-1 : endLine]
			bodyText := strings.Join(bodyLines, "\n")

			words := tokenizeText(bodyText)

			for _, callee := range candidates {
				if callee.ID == caller.ID {
					continue
				}

				if relLine, exists := words[callee.Label]; exists {
					absoluteLine := startLine + relLine - 1
					sourceLoc := fmt.Sprintf("L%d", absoluteLine)

					edgeID := fmt.Sprintf("%s-%s-calls", caller.ID, callee.ID)
					edges = append(edges, &Edge{
						ID:             edgeID,
						SourceID:       caller.ID,
						TargetID:       callee.ID,
						RelationType:   "calls",
						Confidence:     "INFERRED",
						SourceLocation: sourceLoc,
					})
				}
			}
		}
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
	for i := 0; i < len(symbols); i++ {
		for j := i + 1; j < len(symbols); j++ {
			lineI := getSymbolLine(symbols[i])
			lineJ := getSymbolLine(symbols[j])
			if lineI > lineJ {
				symbols[i], symbols[j] = symbols[j], symbols[i]
			}
		}
	}
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

func tokenizeText(text string) map[string]int {
	words := make(map[string]int)
	var current strings.Builder
	lines := strings.Split(text, "\n")
	for lineIdx, line := range lines {
		for _, r := range line {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
				current.WriteRune(r)
			} else {
				if current.Len() > 0 {
					w := current.String()
					if _, exists := words[w]; !exists {
						words[w] = lineIdx + 1
					}
					current.Reset()
				}
			}
		}
		if current.Len() > 0 {
			w := current.String()
			if _, exists := words[w]; !exists {
				words[w] = lineIdx + 1
			}
			current.Reset()
		}
	}
	return words
}

// resolveMarkdownLink resolves a markdown link or wikilink to a node in the graph,
// supporting relative paths, project absolute paths, and fuzzy base name matches.
func resolveMarkdownLink(projPath, sourcePath, target string, nodes map[string]*Node) (string, bool) {
	if idx := strings.IndexAny(target, "#?"); idx != -1 {
		target = target[:idx]
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return "", false
	}

	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "mailto:") {
		return "", false
	}

	sourceDir := filepath.Dir(sourcePath)
	relTarget := filepath.Join(sourceDir, target)
	relTarget = filepath.Clean(relTarget)
	relTarget = strings.ReplaceAll(relTarget, "\\", "/")
	relTarget = strings.TrimPrefix(relTarget, "./")

	if _, exists := nodes[relTarget]; exists {
		return relTarget, true
	}
	if filepath.Ext(relTarget) == "" {
		if _, exists := nodes[relTarget+".md"]; exists {
			return relTarget + ".md", true
		}
	}

	targetClean := filepath.Clean(target)
	targetClean = strings.ReplaceAll(targetClean, "\\", "/")
	targetClean = strings.TrimPrefix(targetClean, "./")
	if _, exists := nodes[targetClean]; exists {
		return targetClean, true
	}
	if filepath.Ext(targetClean) == "" {
		if _, exists := nodes[targetClean+".md"]; exists {
			return targetClean + ".md", true
		}
	}

	targetBase := filepath.Base(target)
	for id, node := range nodes {
		if node.Type == "file" || node.Type == "document" {
			nodeBase := filepath.Base(node.Path)
			if strings.EqualFold(nodeBase, targetBase) {
				return id, true
			}
			if filepath.Ext(targetBase) == "" {
				if strings.EqualFold(nodeBase, targetBase+".md") {
					return id, true
				}
			}
		}
	}

	return "", false
}
