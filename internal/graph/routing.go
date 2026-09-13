package graph

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/svtech-code/sv-memory/internal/config"
	"github.com/svtech-code/sv-memory/internal/graph/schema"
)

// routeFrameworks tracks which routing frameworks are evidenced
// in a package (by config files or dependency declarations).
type routeFrameworks struct {
	Next      bool // Next.js (Pages Router + App Router)
	Nuxt      bool // Nuxt/Vue
	SvelteKit bool // SvelteKit
	Laravel   bool // Laravel (PHP)
}

var (
	// File-based routing patterns
	svelteRouteRe = regexp.MustCompile(`src/routes/(.+?)/\+(page|server|layout)\.(svelte|ts|js)$`)
	nextAppRe     = regexp.MustCompile(`app/(.+?)/(route|page|layout)\.(ts|js|tsx|jsx)$`)
	nextPagesRe   = regexp.MustCompile(`pages/(.+?)\.(ts|js|tsx|jsx)$`)
	nuxtRe        = regexp.MustCompile(`pages/(.+?)\.vue$`)

	// Code-based routing patterns (Python FastAPI/Flask, Java Spring)
	pythonRouteRe = regexp.MustCompile(`(?m)^\s*@(?:app|router|blueprint)\.(get|post|put|delete|patch)\(['"]([^'"]+)['"]`)
	springRouteRe = regexp.MustCompile(`(?m)^\s*@(GetMapping|PostMapping|PutMapping|DeleteMapping|PatchMapping|RequestMapping)\((?:value=)?(?:\[)?['"]([^'"]+)['"]`)

	// Config file patterns for framework detection
	nextConfigRe   = regexp.MustCompile(`(^|/)next\.config\.(js|ts|mjs|cjs)$`)
	svelteConfigRe = regexp.MustCompile(`(^|/)svelte\.config\.(js|ts)$`)
	nuxtConfigRe   = regexp.MustCompile(`(^|/)nuxt\.config\.(js|ts|mjs)$`)

	// Laravel route patterns
	laravelRouteRe = regexp.MustCompile(`Route::(get|post|put|delete|patch|match|any)\(\s*['"]([^'"]+)['"]`)
)

// detectPackageFrameworks determines which file-based routing frameworks are
// active within a specific package root by checking config files and the
// package.json dependencies in that package.
func detectPackageFrameworks(projPath string, nodes map[string]*Node, pkgRoot string) routeFrameworks {
	var fw routeFrameworks

	// Scope prefix for config file matching within this package.
	prefix := ""
	if pkgRoot != "" {
		prefix = pkgRoot + "/"
	}

	// 1. Check config file nodes within this package.
	for _, node := range nodes {
		if node.Type != schema.NodeTypeFile {
			continue
		}
		p := node.Path
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		relToPkg := p[len(prefix):]
		if nextConfigRe.MatchString(relToPkg) {
			fw.Next = true
		} else if svelteConfigRe.MatchString(relToPkg) {
			fw.SvelteKit = true
		} else if nuxtConfigRe.MatchString(relToPkg) {
			fw.Nuxt = true
		}
	}

	// 2. Check package.json dependencies within this package.
	pkgJSON := filepath.Join(projPath, pkgRoot, "package.json")
	content, err := os.ReadFile(pkgJSON)
	if err == nil {
		deps := parsePackageJSON(content)
		for _, dep := range deps {
			switch dep {
			case "next":
				fw.Next = true
			case "nuxt", "nuxt3":
				fw.Nuxt = true
			case "@sveltejs/kit":
				fw.SvelteKit = true
			}
		}
	}

	// 3. Check for Laravel evidence: artisan file or laravel/framework in composer.json.
	artisanPath := filepath.Join(projPath, pkgRoot, "artisan")
	if _, err := os.Stat(artisanPath); err == nil {
		fw.Laravel = true
	}
	composerPath := filepath.Join(projPath, pkgRoot, "composer.json")
	if !fw.Laravel {
		if composerContent, cErr := os.ReadFile(composerPath); cErr == nil {
			if strings.Contains(string(composerContent), `"laravel/framework"`) {
				fw.Laravel = true
			}
		}
	}

	return fw
}

// extractRoutingEdges parses files for framework routing semantics (both
// file-based like Next.js/SvelteKit and code-based like FastAPI/Spring) and
// returns synthetic Route nodes and edges linking them to the implementation files.
//
// File-based patterns are evaluated per-package: each file's nearest package
// root determines which framework evidence is checked, and route node IDs are
// scoped by package root to prevent collisions across packages in monorepos.
//
// Code-based patterns (Python, Java) are always active since they match against
// actual code content, not directory structure.
func extractRoutingEdges(projPath string, fileContents map[string][]byte, filePkgRoot map[string]string, nodes map[string]*Node) (map[string]*Node, []*Edge) {
	routeNodes := make(map[string]*Node)
	var edges []*Edge

	// Group files by package root for per-package evidence detection.
	pkgFiles := make(map[string]map[string][]byte) // pkgRoot → fileContents
	for path, content := range fileContents {
		pkgRoot := filePkgRoot[path]
		if pkgFiles[pkgRoot] == nil {
			pkgFiles[pkgRoot] = make(map[string][]byte)
		}
		pkgFiles[pkgRoot][path] = content
	}

	// Process each package independently.
	for pkgRoot, files := range pkgFiles {
		frameworks := detectPackageFrameworks(projPath, nodes, pkgRoot)

		// Apply config overrides: disabled recipes suppress detection.
		if !config.RoutingEnabled("nextjs") {
			frameworks.Next = false
		}
		if !config.RoutingEnabled("nuxt") {
			frameworks.Nuxt = false
		}
		if !config.RoutingEnabled("sveltekit") {
			frameworks.SvelteKit = false
		}
		if !config.RoutingEnabled("laravel") {
			frameworks.Laravel = false
		}

		for path, content := range files {
			fileID := path

			// 1. File-based routing (gated by per-package framework evidence)
			if frameworks.SvelteKit {
				if m := svelteRouteRe.FindStringSubmatch(path); len(m) > 0 {
					routePath := "/" + m[1]
					addRouteEdge(routePath, fileID, path, pkgRoot, "SvelteKit", routeNodes, &edges)
				}
			}
			if frameworks.Next {
				if m := nextAppRe.FindStringSubmatch(path); len(m) > 0 {
					routePath := "/" + m[1]
					addRouteEdge(routePath, fileID, path, pkgRoot, "Next.js (App)", routeNodes, &edges)
				} else if m := nextPagesRe.FindStringSubmatch(path); len(m) > 0 {
					routePath := "/" + m[1]
					if routePath == "/index" {
						routePath = "/"
					}
					addRouteEdge(routePath, fileID, path, pkgRoot, "Next.js (Pages)", routeNodes, &edges)
				}
			}
			if frameworks.Nuxt {
				if m := nuxtRe.FindStringSubmatch(path); len(m) > 0 {
					routePath := "/" + m[1]
					if routePath == "/index" {
						routePath = "/"
					}
					addRouteEdge(routePath, fileID, path, pkgRoot, "Nuxt/Vue", routeNodes, &edges)
				}
			}

			// 2. Code-based routing (Laravel PHP) — gated by per-package evidence
			if frameworks.Laravel && strings.HasSuffix(path, ".php") {
				matches := laravelRouteRe.FindAllStringSubmatch(string(content), -1)
				for _, m := range matches {
					method := strings.ToUpper(m[1])
					routePath := m[2]
					routeLabel := fmt.Sprintf("%s %s", method, routePath)
					addRouteEdge(routeLabel, fileID, path, pkgRoot, "Laravel", routeNodes, &edges)
				}
			}

			// 3. Code-based routing (Python) — always active, not package-scoped
			if strings.HasSuffix(path, ".py") {
				matches := pythonRouteRe.FindAllStringSubmatch(string(content), -1)
				for _, m := range matches {
					method := strings.ToUpper(m[1])
					routePath := m[2]
					routeLabel := fmt.Sprintf("%s %s", method, routePath)
					addRouteEdge(routeLabel, fileID, path, pkgRoot, "FastAPI/Flask", routeNodes, &edges)
				}
			}

			// 3. Code-based routing (Java/Spring) — always active, not package-scoped
			if strings.HasSuffix(path, ".java") {
				matches := springRouteRe.FindAllStringSubmatch(string(content), -1)
				for _, m := range matches {
					mapping := m[1]
					routePath := m[2]
					method := strings.TrimSuffix(mapping, "Mapping")
					if method == "Request" {
						method = "ANY"
					} else {
						method = strings.ToUpper(method)
					}
					routeLabel := fmt.Sprintf("%s %s", method, routePath)
					addRouteEdge(routeLabel, fileID, path, pkgRoot, "Spring", routeNodes, &edges)
				}
			}
		}
	}

	return routeNodes, edges
}

// addRouteEdge creates or reuses a route node and appends an edge from the route
// to the target file. The route node ID is scoped by pkgRoot to prevent
// collisions across packages in monorepos (e.g. two apps both having "/").
func addRouteEdge(routePath, targetFileID, path, pkgRoot, framework string, nodes map[string]*Node, edges *[]*Edge) {
	// Scope the hash input by package root to prevent cross-package collisions.
	scopeInput := pkgRoot + "|" + routePath
	nodeID := "route:" + fmt.Sprintf("%x", sha256.Sum256([]byte(scopeInput)))[:12]

	if _, exists := nodes[nodeID]; !exists {
		nodes[nodeID] = &Node{
			ID:    nodeID,
			Type:  schema.NodeTypeRoute,
			Label: routePath,
			Path:  routePath,
			Metadata: map[string]interface{}{
				"framework": framework,
				"package":   pkgRoot,
			},
		}
	}

	edgeID := fmt.Sprintf("routes:%s:%s", nodeID, targetFileID)
	*edges = append(*edges, &Edge{
		ID:           edgeID,
		SourceID:     nodeID,
		TargetID:     targetFileID,
		RelationType: schema.EdgeRoutes,
		Confidence:   "EXTRACTED",
	})
}
