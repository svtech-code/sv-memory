package graph

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/svtech-code/sv-memory/internal/graph/extractor"
)

func resolveImport(projPath, sourcePath, imp string, nodes map[string]*Node) (string, bool) {
	// If it's a relative path (starts with . or ..)
	if strings.HasPrefix(imp, ".") {
		sourceDir := filepath.Dir(sourcePath)
		// Leading dots are levels up from the source's package directory.
		// This matches Python's relative imports ("from .models import X" →
		// ".models" resolves against the source directory) and is equivalent
		// to filepath.Join for JS/TS relative paths ("../utils", "./foo").
		rel := imp
		up := 0
		for len(rel) > 0 && rel[0] == '.' {
			up++
			rel = rel[1:]
		}
		baseDir := sourceDir
		for i := 1; i < up; i++ {
			baseDir = filepath.Dir(baseDir)
		}
		resolvedRel := filepath.Clean(filepath.Join(baseDir, rel))
		// Node ids/paths are canonicalized to forward slashes (see scanner), so
		// normalize the resolved import on Windows where filepath uses "\".
		resolvedRel = filepath.ToSlash(resolvedRel)

		// Check options with extensions. The list is shared across languages:
		// tree-sitter Python emits relative imports (from .models import X →
		// ".models") that must resolve to "models.py", and "from . import sub"
		// can resolve to a package's __init__.py.
		exts := []string{"", ".ts", ".js", ".tsx", ".jsx", ".astro", ".sh", ".lua", ".py", ".pyi", "/index.ts", "/index.js", "/index.tsx", "/index.jsx", "/index.py", "/__init__.py", "/__init__.pyi"}
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
// language extension (e.g. "os" in Python, "fmt" in Go, "fs" in Node). A
// submodule resolves to its top-level package, so "net/http" (Go), "os.path"
// (Python) and "fs/promises" (Node) are all recognized as standard library.
func isStdlib(imp, ext string) bool {
	top := imp
	switch ext {
	case ".py":
		// Python submodules are dot-separated (os.path).
		if i := strings.IndexByte(imp, '.'); i > 0 {
			top = imp[:i]
		}
	default:
		// Go and Node use slash-separated subpackages (net/http, fs/promises).
		if i := strings.IndexByte(imp, '/'); i > 0 {
			top = imp[:i]
		}
	}
	switch ext {
	case ".go":
		return goStdlib[top]
	case ".py":
		return pyStdlib[top]
	case ".js", ".ts", ".jsx", ".tsx":
		return nodeStdlib[top]
	}
	return false
}

// isExternalPkg returns true if the import path represents a external library.
func isExternalPkg(imp string) bool {
	if strings.HasPrefix(imp, ".") || strings.HasPrefix(imp, "/") {
		return false
	}
	if strings.TrimSpace(imp) == "" {
		return false
	}
	// Reject strings with whitespace or newlines — those are code fragments
	// captured by over-eager regex, not valid package paths.
	for _, r := range imp {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return false
		}
	}
	return true
}

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
	// Build per-file and per-language symbol maps (function/class nodes).
	fileSymbols := make(map[string][]*Node)
	langSymbols := make(map[string][]*Node)
	for _, node := range nodes {
		if node.Type != "function" && node.Type != "class" {
			continue
		}
		fileSymbols[node.Path] = append(fileSymbols[node.Path], node)
		ext := strings.ToLower(filepath.Ext(node.Path))
		if lang := getLanguageGroup(ext); lang != "" {
			langSymbols[lang] = append(langSymbols[lang], node)
		}
	}

	var edges []*Edge
	covered := map[string]bool{}
	seen := map[string]bool{}

	for filePath, symbols := range fileSymbols {
		ext := strings.ToLower(filepath.Ext(filePath))
		if getLanguageGroup(ext) == "" {
			continue
		}
		content, ok := fileContents[filePath]
		if !ok {
			// Lazy incremental syncs only re-parse the changed files; call
			// edges of unchanged files are preserved, not re-extracted.
			continue
		}

		refs, err := refExt.ExtractCallRefs(content, filePath, ext)
		if err != nil {
			continue // regex-only or parse failure → heuristic will handle it
		}
		covered[filePath] = true

		// Sort symbols by line so the containing caller can be resolved.
		sortSymbolsByLine(symbols)

		for _, ref := range refs {
			if ref.Callee == "" {
				continue
			}
			// Resolve caller: the last symbol whose start line <= call line.
			caller := resolveCallerAtLine(symbols, ref.Line)
			// Resolve callee: same file first, then cross-file same language.
			target := resolveCalleeNode(fileSymbols[filePath], ref.Callee, langSymbols[getLanguageGroup(ext)], caller)
			if caller == nil || target == nil {
				continue
			}

			edgeID := fmt.Sprintf("%s-%s-calls", caller.ID, target.ID)
			if seen[edgeID] {
				continue
			}
			seen[edgeID] = true
			edges = append(edges, &Edge{
				ID:             edgeID,
				SourceID:       caller.ID,
				TargetID:       target.ID,
				RelationType:   "calls",
				Confidence:     "EXTRACTED",
				SourceLocation: fmt.Sprintf("L%d:%d", ref.Line, ref.Col),
			})
		}
	}

	return edges, covered
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
		// Files already covered by the AST call path are skipped so each edge
		// is produced exactly once (EXTRACTED from AST, not duplicated INFERRED).
		if astCovered[filePath] {
			continue
		}
		var content []byte
		if cached, ok := fileContents[filePath]; ok {
			content = cached
		} else {
			// Lazy incremental syncs only re-parse the changed files; call
			// edges of unchanged files are preserved, not re-extracted.
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

// extractRationaleEdges maps each rationale node to its containing function, class, or file.

// extractContainsEdges creates "contains" edges from parent documents/schemas to
// their child symbol nodes (sections, code blocks, tables, views, indexes, types).
func extractContainsEdges(nodes map[string]*Node) []*Edge {
	var edges []*Edge
	for id, node := range nodes {
		if node.Type == "document" || node.Type == "sql" {
			prefix := id + ":"
			for childID := range nodes {
				if strings.HasPrefix(childID, prefix) && childID != id {
					edgeID := fmt.Sprintf("%s-%s-contains", id, childID)
					edges = append(edges, &Edge{
						ID:           edgeID,
						SourceID:     id,
						TargetID:     childID,
						RelationType: "contains",
						Confidence:   "INFERRED",
					})
				}
			}
		}
	}
	return edges
}
