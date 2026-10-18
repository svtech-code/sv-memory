package graph

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/svtech-code/sv-memory/internal/graph/schema"
)

// gitignoreMatcher evaluates file paths against .gitignore-style patterns.
// Supports #comments, empty lines, !negation, trailing / for dirs, *, **, and
// leading / anchoring. Reads both .gitignore and .sv-memoryignore files.
type gitignoreMatcher struct {
	patterns []gitignorePattern
}

type gitignorePattern struct {
	raw     string
	negate  bool
	dirOnly bool
	rooted  bool
	parts   []string // split on /, with ** preserved
}

func loadGitignore(projPath string) (*gitignoreMatcher, error) {
	m := &gitignoreMatcher{}

	// Load .sv-memoryignore first (higher precedence)
	m.loadFile(filepath.Join(projPath, ".sv-memoryignore"))
	// Then .gitignore
	m.loadFile(filepath.Join(projPath, ".gitignore"))

	return m, nil
}

// DefaultMemoryIgnoreTemplate contains the default .sv-memoryignore template.
const DefaultMemoryIgnoreTemplate = `# Default .sv-memoryignore template
# Specify files and directories to exclude from sv-memory code graph analysis

# Version control & internal tools
.git/
.sv-memory/

# Dependencies & build outputs
node_modules/
vendor/
dist/
build/
out/
target/
bin/

# Environment & caches
.venv/
venv/
__pycache__/
.cache/
.coverage

# Secrets & credentials (defense in depth — keep these out of the graph)
.env
.env.*
*.pem
*.key
*.p12
*.pfx
*.jks
*.keystore
id_rsa
id_rsa.*
id_ed25519
id_ed25519.*
credentials
credentials.*
*.htpasswd
.aws/
.gcp/
.ssh/
secrets.yaml
secrets.yml

# IDEs & System files
.idea/
.vscode/
.DS_Store
`

// EnsureMemoryIgnore checks if .sv-memoryignore exists in projPath.
// If it does not exist, it creates a default .sv-memoryignore file.
func EnsureMemoryIgnore(projPath string) (bool, error) {
	ignorePath := filepath.Join(projPath, ".sv-memoryignore")
	if _, err := os.Stat(ignorePath); os.IsNotExist(err) {
		err := os.WriteFile(ignorePath, []byte(DefaultMemoryIgnoreTemplate), 0644)
		if err != nil {
			return false, fmt.Errorf("failed to create default .sv-memoryignore: %w", err)
		}
		return true, nil
	}
	return false, nil
}

func (m *gitignoreMatcher) loadFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m.addPattern(line)
	}
}

func (m *gitignoreMatcher) addPattern(line string) {
	p := gitignorePattern{raw: line}

	if strings.HasPrefix(line, "\\!") {
		line = line[1:] // escaped leading !
	} else if strings.HasPrefix(line, "!") {
		p.negate = true
		line = line[1:]
	}

	if strings.HasSuffix(line, "/") {
		p.dirOnly = true
		line = strings.TrimSuffix(line, "/")
	}

	if strings.HasPrefix(line, "/") {
		p.rooted = true
		line = line[1:]
	}

	// Split into parts for matching
	p.parts = strings.Split(line, "/")

	m.patterns = append(m.patterns, p)
}

// match returns true if the given relative path should be IGNORED.
// A negated pattern returns false (not ignored).
func (m *gitignoreMatcher) match(relPath string, isDir bool) bool {
	ignored := false
	for _, p := range m.patterns {
		if p.dirOnly && !isDir {
			continue
		}
		if p.matchPath(relPath) {
			ignored = !p.negate
		}
	}
	return ignored
}

func (p *gitignorePattern) matchPath(relPath string) bool {
	// Normalize path
	relPath = filepath.ToSlash(relPath)
	parts := strings.Split(relPath, "/")

	if p.rooted {
		return p.matchParts(parts, 0, 0)
	}
	// For non-rooted patterns, try matching at any position
	for start := 0; start <= len(parts); start++ {
		if p.matchParts(parts, start, 0) {
			return true
		}
	}
	return false
}

func (p *gitignorePattern) matchParts(pathParts []string, pi, si int) bool {
	if si >= len(p.parts) {
		return pi >= len(pathParts)
	}
	if pi >= len(pathParts) {
		// Only match trailing **
		for i := si; i < len(p.parts); i++ {
			if p.parts[i] != "**" {
				return false
			}
		}
		return true
	}

	part := p.parts[si]
	if part == "**" {
		// ** matches zero or more path components
		if p.matchParts(pathParts, pi, si+1) {
			return true
		}
		return p.matchParts(pathParts, pi+1, si)
	}

	if matchGlob(part, pathParts[pi]) {
		return p.matchParts(pathParts, pi+1, si+1)
	}
	return false
}

func matchGlob(pattern, s string) bool {
	// * matches anything except /
	if pattern == "*" {
		return true
	}
	// Simple glob matching: supports * and ?
	pi, si := 0, 0
	for si < len(pattern) {
		if pi >= len(s) {
			break
		}
		c := pattern[si]
		switch c {
		case '*':
			// Try matching zero or more characters
			for i := pi; i <= len(s); i++ {
				if matchGlob(pattern[si+1:], s[i:]) {
					return true
				}
			}
			return false
		case '?':
			pi++
			si++
		default:
			if pattern[si] != s[pi] {
				return false
			}
			pi++
			si++
		}
	}
	return si == len(pattern) && pi == len(s)
}

// Common directories to ignore during code scanning (fallback when
// .gitignore is not available).
var fallbackIgnoreDirs = map[string]bool{
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
	".vue": true, ".svelte": true, ".md": true, ".sql": true,
}

// supportedScanExts lists every extension that participates in the code graph.
// Used by both scanFiles (full scan) and DetectStaleFiles (cheap mtime probe).
var supportedScanExts = map[string]bool{
	".go": true, ".py": true, ".js": true, ".jsx": true, ".ts": true, ".tsx": true,
	".html": true, ".php": true, ".css": true, ".astro": true, ".sh": true,
	".lua": true, ".rb": true, ".rs": true, ".java": true, ".vue": true,
	".svelte": true, ".md": true, ".sql": true,
}

var manifestFilenames = []string{"package.json", "go.mod", "requirements.txt", "Cargo.toml", "composer.json", "Gemfile"}

type walkResult struct {
	nodes         map[string]*Node
	fileList      []string
	fileMeta      map[string]fileMetaEntry
	manifestFiles []string
	fileContents  map[string][]byte
	packageRoots  map[string]bool   // dirs containing a manifest ("" = project root)
	filePkgRoot   map[string]string // file relPath → nearest package root relPath
}

type fileMetaEntry struct {
	mtimeMs int64
	size    int64
}

func scanFiles(projPath string) (*walkResult, error) {
	return scanFilesFiltered(projPath, nil)
}

// scanState accumulates the results of a filesystem walk.
type scanState struct {
	readOnly     map[string]bool
	manifestSet  map[string]bool
	gi           *gitignoreMatcher
	nodes        map[string]*Node
	fileList     []string
	fileMeta     map[string]fileMetaEntry
	fileContents map[string][]byte
	packageRoots map[string]bool
	manifests    []string
}

// scanFilesFiltered walks the project and builds nodes/edges metadata. When
// readOnly is non-nil, only those relative paths are read from disk and parsed
// for symbols; every other supported file is still registered as a file node
// (with its mtime/size) so import resolution keeps working, but its content is
// not read. When readOnly is nil every supported file is fully scanned (the
// original full-scan behaviour).
func scanFilesFiltered(projPath string, readOnly map[string]bool) (*walkResult, error) {
	gi, _ := loadGitignore(projPath)
	s := &scanState{
		readOnly:     readOnly,
		manifestSet:  buildManifestSet(),
		gi:           gi,
		nodes:        make(map[string]*Node),
		fileList:     []string{},
		fileMeta:     make(map[string]fileMetaEntry),
		fileContents: make(map[string][]byte),
		packageRoots: make(map[string]bool),
	}

	err := filepath.WalkDir(projPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return s.visit(projPath, path, d)
	})
	if err != nil {
		return nil, fmt.Errorf("failed walking directory: %w", err)
	}

	s.collectManifestNodes(projPath)

	// Compute filePkgRoot: map each file to its nearest package root.
	filePkgRoot := make(map[string]string, len(s.fileContents))
	for relPath := range s.fileContents {
		filePkgRoot[relPath] = nearestPackageRoot(relPath, s.packageRoots)
	}

	return &walkResult{
		nodes:         s.nodes,
		fileList:      s.fileList,
		fileMeta:      s.fileMeta,
		manifestFiles: s.manifests,
		fileContents:  s.fileContents,
		packageRoots:  s.packageRoots,
		filePkgRoot:   filePkgRoot,
	}, nil
}

func buildManifestSet() map[string]bool {
	set := make(map[string]bool, len(manifestFilenames))
	for _, mf := range manifestFilenames {
		set[mf] = true
	}
	return set
}

// visit handles a single walk entry, skipping ignored dirs/files and recording
// supported files.
func (s *scanState) visit(projPath, path string, d fs.DirEntry) error {
	relPath, relErr := filepath.Rel(projPath, path)
	if relErr != nil {
		return nil
	}
	// Canonicalize node paths to forward slashes on every OS so graph node
	// ids/paths are identical regardless of the platform's separator. This
	// keeps markdown link resolution, sv_graph_query/path lookups, and the
	// exported vault/wiki consistent on Windows.
	relPath = filepath.ToSlash(relPath)

	if d.IsDir() {
		if s.skipDir(relPath, d.Name()) {
			return filepath.SkipDir
		}
		return nil
	}

	if s.gi != nil && s.gi.match(relPath, false) {
		return nil
	}

	s.recordPackageRoot(relPath)

	ext := strings.ToLower(filepath.Ext(relPath))
	if !supportedScanExts[ext] {
		return nil
	}
	s.addFileNode(projPath, path, relPath, ext)
	return nil
}

func (s *scanState) skipDir(relPath, name string) bool {
	if fallbackIgnoreDirs[name] {
		return true
	}
	return s.gi != nil && s.gi.match(relPath, true)
}

func (s *scanState) recordPackageRoot(relPath string) {
	if !s.manifestSet[filepath.Base(relPath)] {
		return
	}
	pkgDir := filepath.Dir(relPath)
	if pkgDir == "." {
		pkgDir = ""
	}
	s.packageRoots[pkgDir] = true
}

// addFileNode records a supported file's node, metadata, and (when requested)
// its parsed symbols.
func (s *scanState) addFileNode(projPath, path, relPath, ext string) {
	mtimeMs, size := statFile(path)
	baseMeta := map[string]interface{}{
		"extension": ext,
		"size":      size,
	}

	if symbolScanExts[ext] && (s.readOnly == nil || s.readOnly[relPath]) {
		if content, readErr := os.ReadFile(path); readErr == nil {
			s.fileContents[relPath] = content
			symbolNodes, symMeta := parseSymbols(relPath, ext, content)
			for k, v := range symMeta {
				baseMeta[k] = v
			}
			for _, sn := range symbolNodes {
				s.nodes[sn.ID] = sn
			}
		}
	}

	s.nodes[relPath] = &Node{
		ID:       relPath,
		Type:     nodeTypeForExt(ext),
		Label:    filepath.Base(relPath),
		Path:     relPath,
		Metadata: baseMeta,
	}
	s.fileList = append(s.fileList, relPath)
	s.fileMeta[relPath] = fileMetaEntry{mtimeMs: mtimeMs, size: size}
}

func nodeTypeForExt(ext string) string {
	switch ext {
	case ".md":
		return schema.NodeTypeDocument
	case ".sql":
		return schema.NodeTypeSQL
	default:
		return schema.NodeTypeFile
	}
}

// collectManifestNodes registers project-root manifest files as nodes.
func (s *scanState) collectManifestNodes(projPath string) {
	for _, mf := range manifestFilenames {
		fi, stErr := os.Stat(filepath.Join(projPath, mf))
		if stErr != nil {
			continue
		}
		s.manifests = append(s.manifests, mf)
		s.nodes[mf] = &Node{
			ID:    mf,
			Type:  schema.NodeTypeFile,
			Label: mf,
			Path:  mf,
			Metadata: map[string]interface{}{
				"size":      fi.Size(),
				"extension": filepath.Ext(mf),
			},
		}
		s.fileList = append(s.fileList, mf)
		s.fileMeta[mf] = fileMetaEntry{mtimeMs: fi.ModTime().UnixMilli(), size: fi.Size()}
	}
}

func statFile(path string) (mtimeMs, size int64) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, 0
	}
	return fi.ModTime().UnixMilli(), fi.Size()
}

// nearestPackageRoot returns the nearest ancestor directory of relPath that is
// a package root (contains a manifest). Returns "" for the project root.
func nearestPackageRoot(relPath string, packageRoots map[string]bool) string {
	dir := filepath.Dir(relPath)
	for dir != "." && dir != "" {
		if packageRoots[dir] {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	return ""
}
