package graph

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

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

func getFileSize(path string) int64 {
	stat, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return stat.Size()
}

// extractCallEdges identifies call relationships (functions calling other functions/classes)
// within the project by scanning function body source code.
func extractCallEdges(projPath string, nodes map[string]*Node, fileContents map[string][]byte) []*Edge {
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
		var content []byte
		var err error
		if cached, ok := fileContents[filePath]; ok {
			content = cached
		} else {
			absPath := filepath.Join(projPath, filePath)
			content, err = os.ReadFile(absPath)
		}
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
