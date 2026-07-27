package graph

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

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

// Supported extensions for symbol scanning.
var symbolScanExts = map[string]bool{
	".go": true, ".py": true, ".js": true, ".jsx": true, ".ts": true, ".tsx": true,
	".php": true, ".astro": true, ".lua": true, ".rb": true, ".rs": true, ".java": true,
	".vue": true, ".svelte": true,
}

var manifestFilenames = []string{"package.json", "go.mod", "requirements.txt", "Cargo.toml", "composer.json", "Gemfile"}

type walkResult struct {
	nodes         map[string]*Node
	fileList      []string
	fileMeta      map[string]fileMetaEntry
	manifestFiles []string
	fileContents  map[string][]byte
}

type fileMetaEntry struct {
	mtimeMs int64
	size    int64
}

func scanFiles(projPath string) (*walkResult, error) {
	nodes := make(map[string]*Node)
	fileList := []string{}
	fileMeta := make(map[string]fileMetaEntry)
	fileContents := make(map[string][]byte)

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
					fileContents[relPath] = content
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
		fileContents:  fileContents,
	}, nil
}
