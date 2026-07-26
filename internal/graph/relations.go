package graph

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

// extractRationaleEdges maps each rationale node to its containing function, class, or file.
func extractRationaleEdges(nodes map[string]*Node) []*Edge {
	var edges []*Edge

	fileSymbols := make(map[string][]*Node)
	for _, node := range nodes {
		if node.Type == "function" || node.Type == "class" {
			fileSymbols[node.Path] = append(fileSymbols[node.Path], node)
		}
	}

	for filePath := range fileSymbols {
		sortSymbolsByLine(fileSymbols[filePath])
	}

	for _, node := range nodes {
		if node.Type != "rationale" {
			continue
		}

		lineVal, _ := node.Metadata["line"]
		var line int
		switch v := lineVal.(type) {
		case float64:
			line = int(v)
		case int:
			line = v
		case int64:
			line = int(v)
		}

		targetID := node.Path
		symbols := fileSymbols[node.Path]
		var bestTarget *Node
		for i, sym := range symbols {
			symLineVal, _ := sym.Metadata["line"]
			var symLine int
			switch v := symLineVal.(type) {
			case float64:
				symLine = int(v)
			case int:
				symLine = v
			case int64:
				symLine = int(v)
			}

			if symLine <= line {
				if i == len(symbols)-1 {
					bestTarget = sym
				} else {
					nextSymLineVal, _ := symbols[i+1].Metadata["line"]
					var nextSymLine int
					switch v := nextSymLineVal.(type) {
					case float64:
						nextSymLine = int(v)
					case int:
						nextSymLine = v
					case int64:
						nextSymLine = int(v)
					}
					if line < nextSymLine {
						bestTarget = sym
					}
				}
			}
		}

		if bestTarget != nil {
			targetID = bestTarget.ID
		}

		edges = append(edges, &Edge{
			ID:           node.ID + "->rationale_for->" + targetID,
			SourceID:     node.ID,
			TargetID:     targetID,
			RelationType: "rationale_for",
		})
	}

	return edges
}
