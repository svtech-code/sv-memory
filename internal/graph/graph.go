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
	ID           string `json:"id"`
	SourceID     string `json:"source_id"`
	TargetID     string `json:"target_id"`
	RelationType string `json:"relation_type"` // 'imports' | 'calls' | 'depends_on'
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
	".php": true, ".astro": true, ".lua": true,
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

	// Python: matches import x, y or from x import y
	pyImportRegex = regexp.MustCompile(`(?m)^\s*(?:import\s+([\w\.,\s]+)|from\s+([\w\.]+)\s+import)`)

	// Go: matches import "path" or import ( ... "path" ... )
	goImportRegex = regexp.MustCompile(`(?m)"([^"\n\r\t]+)"`)

	// PHP: matches include(_once)?/require(_once)? 'path' or namespace/use path
	phpImportRegex = regexp.MustCompile(`(?m)(?:include|require)(?:_once)?\s*\(?\s*['"]([^'"]+)['"]\s*\)?|use\s+([\w\\]+)(?:\s+as\s+\w+)?;`)

	// CSS: matches @import 'path' or @import url('path')
	cssImportRegex = regexp.MustCompile(`(?m)@import\s+(?:url\()?['"]([^'"]+)['"]`)

	// HTML: matches <script src="path"> or <link href="path">
	htmlImportRegex = regexp.MustCompile(`(?m)<script\s+[^>]*src=['"]([^'"]+)['"]|<link\s+[^>]*href=['"]([^'"]+)['"]`)

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
	nodes    map[string]*Node
	fileList []string
	fileMeta map[string]fileMetaEntry // relative path → mtime+size
}

type fileMetaEntry struct {
	mtimeMs int64
	size    int64
}

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
		case ".go", ".py", ".js", ".jsx", ".ts", ".tsx", ".html", ".php", ".css", ".astro", ".sh", ".lua":
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

			nodes[relPath] = &Node{
				ID:       relPath,
				Type:     "file",
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
	return &walkResult{nodes: nodes, fileList: fileList, fileMeta: fileMeta}, nil
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
				if err != nil {
					results <- parseResult{sourcePath: sourcePath, err: err}
					continue
				}
				ext := strings.ToLower(filepath.Ext(sourcePath))
				var imports []string

				switch ext {
				case ".js", ".ts", ".jsx", ".tsx", ".astro":
					matches := jsImportRegex.FindAllSubmatch(content, -1)
					for _, m := range matches {
						if len(m) > 1 && len(m[1]) > 0 {
							imports = append(imports, string(m[1]))
						} else if len(m) > 2 && len(m[2]) > 0 {
							imports = append(imports, string(m[2]))
						}
					}
				case ".py":
					lines := strings.Split(string(content), "\n")
					for _, line := range lines {
						m := pyImportRegex.FindStringSubmatch(line)
						if len(m) > 0 {
							if len(m[1]) > 0 {
								parts := strings.Split(m[1], ",")
								for _, p := range parts {
									imports = append(imports, strings.TrimSpace(p))
								}
							} else if len(m[2]) > 0 {
								imports = append(imports, strings.TrimSpace(m[2]))
							}
						}
					}
				case ".go":
					strContent := string(content)
					importIdx := strings.Index(strContent, "import")
					if importIdx != -1 {
						matches := goImportRegex.FindAllSubmatch(content, -1)
						for _, m := range matches {
							if len(m) > 1 {
								imports = append(imports, string(m[1]))
							}
						}
					}
				case ".php":
					matches := phpImportRegex.FindAllSubmatch(content, -1)
					for _, m := range matches {
						if len(m) > 1 && len(m[1]) > 0 {
							imports = append(imports, string(m[1]))
						} else if len(m) > 2 && len(m[2]) > 0 {
							imports = append(imports, strings.ReplaceAll(string(m[2]), "\\", "/"))
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
				}
				results <- parseResult{sourcePath: sourcePath, imports: imports}
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
			targetID, found := resolveImport(projPath, res.sourcePath, imp, nodes)
			if found {
				edgeID := fmt.Sprintf("%s-%s-%s", res.sourcePath, targetID, "imports")
				edges = append(edges, &Edge{
					ID:           edgeID,
					SourceID:     res.sourcePath,
					TargetID:     targetID,
					RelationType: "imports",
				})
			} else if isExternalPkg(imp) {
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
				})
			}
		}
	}
	return edges
}

// syncGraphFull is the original delete-all-plus-rescan implementation.
func syncGraphFull(db *sql.DB, projectID string, projPath string) error {
	wr, err := scanFiles(projPath)
	if err != nil {
		return err
	}

	edges := parseFiles(projPath, wr.nodes, wr.fileList)

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
		if _, e := tx.Exec("DELETE FROM graph_edges WHERE project_id = ? AND (source_id = ? OR target_id = ?)", projectID, p, p); e != nil {
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
		if _, e := tx.Exec("DELETE FROM graph_edges WHERE project_id = ? AND source_id = ?", projectID, p); e != nil {
			return false, fmt.Errorf("failed deleting edges for changed file %s: %w", p, e)
		}
		// Remove stale child symbols (function/class nodes) that will be
		// re-created by scanFiles during step 3.
		if _, e := tx.Exec("DELETE FROM graph_nodes WHERE project_id = ? AND id LIKE ?", projectID, p+":%"); e != nil {
			return false, fmt.Errorf("failed deleting child nodes for changed file %s: %w", p, e)
		}
	}

	// 3. Parse new+changed files and insert nodes+edges.
	edges := parseFiles(projPath, wr.nodes, toParse)
	for _, p := range toParse {
		upsertNode(tx, projectID, wr.nodes[p])
		// Also upsert child symbol nodes (functions/classes) for this file.
		// IDs follow the pattern "relPath:symbolName".
		prefix := p + ":"
		for id, node := range wr.nodes {
			if strings.HasPrefix(id, prefix) {
				if err := upsertNode(tx, projectID, node); err != nil {
					return false, fmt.Errorf("failed upserting child node %s: %w", id, err)
				}
			}
		}
	}
	// Also upsert package nodes that parseFiles may have added to wr nodes.
	// These are external dependencies (e.g. "pkg:react") referenced by edges;
	// without them the FK constraint on graph_edges would fail.
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

	// 4. Update file metadata for added/changed files.
	for _, p := range toParse {
		m := wr.fileMeta[p]
		if _, e := tx.Exec("INSERT OR REPLACE INTO graph_files_meta (project_id, path, mtime_ms, size) VALUES (?, ?, ?, ?)", projectID, p, m.mtimeMs, m.size); e != nil {
			return false, fmt.Errorf("failed upserting file meta for %s: %w", p, e)
		}
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
	stmt, err := tx.Prepare("INSERT INTO graph_edges (id, project_id, source_id, target_id, relation_type) VALUES (?, ?, ?, ?, ?) ON CONFLICT DO NOTHING")
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, edge := range edges {
		if _, e := stmt.Exec(edge.ID, projectID, edge.SourceID, edge.TargetID, edge.RelationType); e != nil {
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
