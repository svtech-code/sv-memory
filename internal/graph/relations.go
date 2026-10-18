package graph

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/svtech-code/sv-memory/internal/graph/extractor"
	"github.com/svtech-code/sv-memory/internal/graph/schema"
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

	// Try resolving path aliases (@/, ~/) via tsconfig paths or heuristic.
	if resolved, ok := resolveAlias(projPath, sourcePath, imp, nodes); ok {
		return resolved, true
	}

	return "", false
}

// tsconfigCache caches tsconfig path mappings per project root to avoid
// re-reading tsconfig.json for every import resolution.
var tsconfigCache = make(map[string]map[string][]string)

// tsconfigPaths represents the relevant部分 of a tsconfig.json file.
type tsconfigPaths struct {
	CompilerOptions struct {
		Paths   map[string][]string `json:"paths"`
		BaseURL string              `json:"baseUrl"`
	} `json:"compilerOptions"`
}

// loadTsconfigPaths reads tsconfig.json (or tsconfig.app.json) from the
// nearest ancestor of sourcePath and returns the path alias mappings.
// Results are cached per project root.
func loadTsconfigPaths(projPath, sourcePath string) map[string][]string {
	// Find the nearest tsconfig.json by walking up from the source file's directory.
	dir := filepath.Dir(filepath.Join(projPath, sourcePath))
	var tsconfigPath string
	for dir != "." && dir != "" && strings.HasPrefix(dir, projPath) {
		for _, name := range []string{"tsconfig.json", "tsconfig.app.json"} {
			candidate := filepath.Join(dir, name)
			if _, err := os.Stat(candidate); err == nil {
				tsconfigPath = candidate
				break
			}
		}
		if tsconfigPath != "" {
			break
		}
		dir = filepath.Dir(dir)
	}

	if tsconfigPath == "" {
		return nil
	}

	// Check cache by the tsconfig path (not projPath, since different
	// packages within the same project may have different tsconfigs).
	if cached, ok := tsconfigCache[tsconfigPath]; ok {
		return cached
	}

	content, err := os.ReadFile(tsconfigPath)
	if err != nil {
		tsconfigCache[tsconfigPath] = nil
		return nil
	}

	var cfg tsconfigPaths
	if err := json.Unmarshal(content, &cfg); err != nil {
		tsconfigCache[tsconfigPath] = nil
		return nil
	}

	// Normalize: resolve relative to the tsconfig directory.
	tsconfigDir := filepath.Dir(tsconfigPath)
	paths := make(map[string][]string, len(cfg.CompilerOptions.Paths))
	for alias, targets := range cfg.CompilerOptions.Paths {
		var resolved []string
		for _, t := range targets {
			// Strip trailing /* and /*.suffix for matching.
			clean := strings.TrimSuffix(t, "/*")
			abs := filepath.Join(tsconfigDir, clean)
			rel, err := filepath.Rel(projPath, abs)
			if err != nil {
				continue
			}
			rel = filepath.ToSlash(rel)
			resolved = append(resolved, rel)
		}
		paths[alias] = resolved
	}

	tsconfigCache[tsconfigPath] = paths
	return paths
}

// resolveAlias attempts to resolve a TS/JS path alias (e.g., @/components/button)
// using tsconfig.json compilerOptions.paths. Falls back to a heuristic mapping
// (@/ → src/) when no tsconfig exists.
func resolveAlias(projPath, sourcePath, imp string, nodes map[string]*Node) (string, bool) {
	// Only process alias-like imports: @, ~, or known prefixes.
	if !strings.HasPrefix(imp, "@") && !strings.HasPrefix(imp, "~") {
		return "", false
	}

	// Try tsconfig paths first.
	paths := loadTsconfigPaths(projPath, sourcePath)
	for alias, targets := range paths {
		// alias is like "@/*" or "@components/*"
		prefix := strings.TrimSuffix(alias, "/*")
		if strings.HasPrefix(imp, prefix+"/") || imp == prefix {
			suffix := strings.TrimPrefix(imp, prefix+"/")
			for _, base := range targets {
				candidate := base + "/" + suffix
				// Try extensions.
				exts := []string{"", ".ts", ".tsx", ".js", ".jsx", "/index.ts", "/index.tsx", "/index.js", "/index.jsx"}
				for _, ext := range exts {
					testPath := candidate + ext
					if _, exists := nodes[testPath]; exists {
						return testPath, true
					}
				}
			}
		}
	}

	// Heuristic fallback: @/ → src/, ~/ → src/
	if strings.HasPrefix(imp, "@/") || strings.HasPrefix(imp, "~/") {
		suffix := strings.TrimPrefix(imp, "@/")
		suffix = strings.TrimPrefix(suffix, "~/")
		candidate := "src/" + suffix
		exts := []string{"", ".ts", ".tsx", ".js", ".jsx", "/index.ts", "/index.tsx", "/index.js", "/index.jsx"}
		for _, ext := range exts {
			testPath := candidate + ext
			if _, exists := nodes[testPath]; exists {
				return testPath, true
			}
		}
	}

	return "", false
}

// isExternalPkg returns true if the import path represents a external library.
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

func tokenizeText(text string) map[string]int {
	words := make(map[string]int)
	var current strings.Builder
	for lineIdx, line := range strings.Split(text, "\n") {
		for _, r := range line {
			if isWordRune(r) {
				current.WriteRune(r)
				continue
			}
			flushWord(words, &current, lineIdx+1)
		}
		flushWord(words, &current, lineIdx+1)
	}
	return words
}

func isWordRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
}

// flushWord records the current token against its 1-based line (keeping the
// first occurrence) and resets the builder.
func flushWord(words map[string]int, current *strings.Builder, line int) {
	if current.Len() == 0 {
		return
	}
	w := current.String()
	if _, exists := words[w]; !exists {
		words[w] = line
	}
	current.Reset()
}

// resolveMarkdownLink resolves a markdown link or wikilink to a node in the graph,
// supporting relative paths, project absolute paths, and fuzzy base name matches.
func resolveMarkdownLink(projPath, sourcePath, target string, nodes map[string]*Node) (string, bool) {
	target = stripLinkFragment(target)
	if target == "" || isExternalLink(target) {
		return "", false
	}

	if id, ok := matchNodePath(relMarkdownPath(sourcePath, target), nodes); ok {
		return id, true
	}
	if id, ok := matchNodePath(cleanMarkdownPath(target), nodes); ok {
		return id, true
	}
	return matchNodeByBaseName(filepath.Base(target), nodes)
}

// stripLinkFragment removes any #anchor or ?query suffix and trims whitespace.
func stripLinkFragment(target string) string {
	if idx := strings.IndexAny(target, "#?"); idx != -1 {
		target = target[:idx]
	}
	return strings.TrimSpace(target)
}

func isExternalLink(target string) bool {
	return strings.HasPrefix(target, "http://") ||
		strings.HasPrefix(target, "https://") ||
		strings.HasPrefix(target, "mailto:")
}

// relMarkdownPath joins a link target to its source directory and normalizes it.
func relMarkdownPath(sourcePath, target string) string {
	rel := filepath.Clean(filepath.Join(filepath.Dir(sourcePath), target))
	rel = strings.ReplaceAll(rel, "\\", "/")
	return strings.TrimPrefix(rel, "./")
}

// cleanMarkdownPath normalizes a project-absolute link target.
func cleanMarkdownPath(target string) string {
	clean := filepath.Clean(target)
	clean = strings.ReplaceAll(clean, "\\", "/")
	return strings.TrimPrefix(clean, "./")
}

// matchNodePath returns the node id for a path, or its .md-suffixed variant when
// the path carries no extension.
func matchNodePath(p string, nodes map[string]*Node) (string, bool) {
	if _, exists := nodes[p]; exists {
		return p, true
	}
	if filepath.Ext(p) == "" {
		if _, exists := nodes[p+".md"]; exists {
			return p + ".md", true
		}
	}
	return "", false
}

// matchNodeByBaseName finds a file/document node whose base name matches
// targetBase case-insensitively, tolerating a missing .md extension.
func matchNodeByBaseName(targetBase string, nodes map[string]*Node) (string, bool) {
	noExt := filepath.Ext(targetBase) == ""
	for id, node := range nodes {
		if node.Type != "file" && node.Type != "document" {
			continue
		}
		nodeBase := filepath.Base(node.Path)
		if strings.EqualFold(nodeBase, targetBase) {
			return id, true
		}
		if noExt && strings.EqualFold(nodeBase, targetBase+".md") {
			return id, true
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
		if node.Type == schema.NodeTypeDocument || node.Type == schema.NodeTypeSQL {
			prefix := id + ":"
			for childID := range nodes {
				if strings.HasPrefix(childID, prefix) && childID != id {
					edgeID := fmt.Sprintf("%s-%s-contains", id, childID)
					edges = append(edges, &Edge{
						ID:           edgeID,
						SourceID:     id,
						TargetID:     childID,
						RelationType: schema.EdgeContains,
						Confidence:   "INFERRED",
					})
				}
			}
		}
	}
	return edges
}
