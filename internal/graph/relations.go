package graph

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
