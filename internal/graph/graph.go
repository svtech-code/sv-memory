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
)

// SyncGraph scans the project directory, builds the dependency graph, and saves it in SQLite.
func SyncGraph(db *sql.DB, projectID string, projPath string) error {
	// 1. Scan files
	nodes := make(map[string]*Node) // keyed by node ID (relative path)
	fileList := []string{}

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

		// Relativize path for consistent IDs
		relPath, relErr := filepath.Rel(projPath, path)
		if relErr != nil {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(relPath))
		switch ext {
		case ".go", ".py", ".js", ".jsx", ".ts", ".tsx", ".html", ".php", ".css":
			nodeID := relPath
			nodes[nodeID] = &Node{
				ID:    nodeID,
				Type:  "file",
				Label: filepath.Base(relPath),
				Path:  relPath,
				Metadata: map[string]interface{}{
					"extension": ext,
					"size":      getFileSize(path),
				},
			}
			fileList = append(fileList, relPath)
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed walking directory: %w", err)
	}

	// 2. Parse imports & create edges
	var edges []*Edge

	for _, sourcePath := range fileList {
		absPath := filepath.Join(projPath, sourcePath)
		content, err := os.ReadFile(absPath)
		if err != nil {
			continue // skip unreadable files
		}

		ext := strings.ToLower(filepath.Ext(sourcePath))
		var imports []string

		switch ext {
		case ".js", ".ts", ".jsx", ".tsx":
			matches := jsImportRegex.FindAllSubmatch(content, -1)
			for _, m := range matches {
				if len(m) > 1 && len(m[1]) > 0 {
					imports = append(imports, string(m[1]))
				} else if len(m) > 2 && len(m[2]) > 0 {
					imports = append(imports, string(m[2]))
				}
			}
		case ".py":
			// Simple line scanner for python imports
			lines := strings.Split(string(content), "\n")
			for _, line := range lines {
				m := pyImportRegex.FindStringSubmatch(line)
				if len(m) > 0 {
					if len(m[1]) > 0 { // import x
						parts := strings.Split(m[1], ",")
						for _, p := range parts {
							imports = append(imports, strings.TrimSpace(p))
						}
					} else if len(m[2]) > 0 { // from x import y
						imports = append(imports, strings.TrimSpace(m[2]))
					}
				}
			}
		case ".go":
			// Go imports are usually in an import block
			strContent := string(content)
			importIdx := strings.Index(strContent, "import")
			if importIdx != -1 {
				// Scan Go imports
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
		}

		// Resolve imports into target nodes
		for _, imp := range imports {
			targetID, found := resolveImport(projPath, sourcePath, imp, nodes)
			if found {
				edgeID := fmt.Sprintf("%s-%s-%s", sourcePath, targetID, "imports")
				edges = append(edges, &Edge{
					ID:           edgeID,
					SourceID:     sourcePath,
					TargetID:     targetID,
					RelationType: "imports",
				})
			} else {
				// It's an external library / package
				// Create an external package node if it's a standard/library import
				if isExternalPkg(imp) {
					pkgNodeID := "pkg:" + imp
					if _, exists := nodes[pkgNodeID]; !exists {
						nodes[pkgNodeID] = &Node{
							ID:    pkgNodeID,
							Type:  "package",
							Label: imp,
							Path:  imp,
						}
					}
					edgeID := fmt.Sprintf("%s-%s-%s", sourcePath, pkgNodeID, "imports")
					edges = append(edges, &Edge{
						ID:           edgeID,
						SourceID:     sourcePath,
						TargetID:     pkgNodeID,
						RelationType: "imports",
					})
				}
			}
		}
	}

	// 3. Save graph to SQLite database in a transaction
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Clear previous graph data for this project
	_, err = tx.Exec("DELETE FROM graph_edges WHERE project_id = ?", projectID)
	if err != nil {
		return fmt.Errorf("failed deleting old edges: %w", err)
	}
	_, err = tx.Exec("DELETE FROM graph_nodes WHERE project_id = ?", projectID)
	if err != nil {
		return fmt.Errorf("failed deleting old nodes: %w", err)
	}

	// Insert nodes
	nodeQuery := `
	INSERT INTO graph_nodes (id, project_id, node_type, label, path, metadata)
	VALUES (?, ?, ?, ?, ?, ?)
	`
	nodeStmt, err := tx.Prepare(nodeQuery)
	if err != nil {
		return err
	}
	defer nodeStmt.Close()

	for _, node := range nodes {
		metaBytes, _ := json.Marshal(node.Metadata)
		metaStr := string(metaBytes)
		if metaStr == "null" {
			metaStr = "{}"
		}
		_, err = nodeStmt.Exec(node.ID, projectID, node.Type, node.Label, node.Path, metaStr)
		if err != nil {
			return fmt.Errorf("failed inserting node %s: %w", node.ID, err)
		}
	}

	// Insert edges
	edgeQuery := `
	INSERT INTO graph_edges (id, project_id, source_id, target_id, relation_type)
	VALUES (?, ?, ?, ?, ?)
	ON CONFLICT DO NOTHING
	`
	edgeStmt, err := tx.Prepare(edgeQuery)
	if err != nil {
		return err
	}
	defer edgeStmt.Close()

	for _, edge := range edges {
		_, err = edgeStmt.Exec(edge.ID, projectID, edge.SourceID, edge.TargetID, edge.RelationType)
		if err != nil {
			return fmt.Errorf("failed inserting edge %s: %w", edge.ID, err)
		}
	}

	return tx.Commit()
}

// resolveImport checks if an import maps to an existing file in the project.
func resolveImport(projPath, sourcePath, imp string, nodes map[string]*Node) (string, bool) {
	// If it's a relative path (starts with . or ..)
	if strings.HasPrefix(imp, ".") {
		sourceDir := filepath.Dir(sourcePath)
		resolvedRel := filepath.Clean(filepath.Join(sourceDir, imp))

		// Check options with extensions
		exts := []string{"", ".ts", ".js", ".tsx", ".jsx", "/index.ts", "/index.js", "/index.tsx", "/index.jsx"}
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
