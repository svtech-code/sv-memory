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
	Next        bool // Next.js (Pages Router + App Router)
	Nuxt        bool // Nuxt/Vue
	SvelteKit   bool // SvelteKit
	Laravel     bool // Laravel (PHP)
	ReactRouter bool // React Router (JS/TS)
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

	// React Router patterns
	reactRouterJSXRe  = regexp.MustCompile(`<Route\s+path=["']([^"']+)["']`)
	reactRouterPathRe = regexp.MustCompile(`path:\s*["']([^"']+)["']`)
)

// detectPackageFrameworks determines which routing frameworks are evidenced
// in a specific package root by checking config files, dependencies, and
// framework markers.
func detectPackageFrameworks(projPath string, nodes map[string]*Node, pkgRoot string) routeFrameworks {
	var fw routeFrameworks

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
		switch {
		case nextConfigRe.MatchString(relToPkg):
			fw.Next = true
		case svelteConfigRe.MatchString(relToPkg):
			fw.SvelteKit = true
		case nuxtConfigRe.MatchString(relToPkg):
			fw.Nuxt = true
		}
	}

	// 2. Check package.json dependencies.
	pkgJSON := filepath.Join(projPath, pkgRoot, "package.json")
	if content, err := os.ReadFile(pkgJSON); err == nil {
		for _, dep := range parsePackageJSON(content) {
			switch dep {
			case "next":
				fw.Next = true
			case "nuxt", "nuxt3":
				fw.Nuxt = true
			case "@sveltejs/kit":
				fw.SvelteKit = true
			case "react-router-dom", "react-router":
				fw.ReactRouter = true
			}
		}
	}

	// 3. Check for Laravel evidence: artisan file or laravel/framework in composer.json.
	if _, err := os.Stat(filepath.Join(projPath, pkgRoot, "artisan")); err == nil {
		fw.Laravel = true
	} else if composerContent, err := os.ReadFile(filepath.Join(projPath, pkgRoot, "composer.json")); err == nil {
		fw.Laravel = strings.Contains(string(composerContent), `"laravel/framework"`)
	}

	return fw
}

// applyConfigOverrides disables frameworks whose recipes are disabled in config.
func applyConfigOverrides(fw *routeFrameworks) {
	if !config.RoutingEnabled("nextjs") {
		fw.Next = false
	}
	if !config.RoutingEnabled("nuxt") {
		fw.Nuxt = false
	}
	if !config.RoutingEnabled("sveltekit") {
		fw.SvelteKit = false
	}
	if !config.RoutingEnabled("laravel") {
		fw.Laravel = false
	}
	if !config.RoutingEnabled("react-router") {
		fw.ReactRouter = false
	}
}

// extractFileBasedRoutes extracts routes from file path patterns (SvelteKit, Next.js, Nuxt).
func extractFileBasedRoutes(path, pkgRoot string, fw routeFrameworks, nodes map[string]*Node, edges *[]*Edge) {
	if fw.SvelteKit {
		if m := svelteRouteRe.FindStringSubmatch(path); len(m) > 0 {
			addRouteEdge("/"+m[1], path, path, pkgRoot, "SvelteKit", nodes, edges)
		}
	}
	if fw.Next {
		if m := nextAppRe.FindStringSubmatch(path); len(m) > 0 {
			addRouteEdge("/"+m[1], path, path, pkgRoot, "Next.js (App)", nodes, edges)
		} else if m := nextPagesRe.FindStringSubmatch(path); len(m) > 0 {
			routePath := "/" + m[1]
			if routePath == "/index" {
				routePath = "/"
			}
			addRouteEdge(routePath, path, path, pkgRoot, "Next.js (Pages)", nodes, edges)
		}
	}
	if fw.Nuxt {
		if m := nuxtRe.FindStringSubmatch(path); len(m) > 0 {
			routePath := "/" + m[1]
			if routePath == "/index" {
				routePath = "/"
			}
			addRouteEdge(routePath, path, path, pkgRoot, "Nuxt/Vue", nodes, edges)
		}
	}
}

// extractCodeBasedRoutes extracts routes from code content (Laravel, React Router, Python, Java).
func extractCodeBasedRoutes(path string, content []byte, pkgRoot string, fw routeFrameworks, nodes map[string]*Node, edges *[]*Edge) {
	contentStr := string(content)

	// Laravel PHP
	if fw.Laravel && strings.HasSuffix(path, ".php") {
		for _, m := range laravelRouteRe.FindAllStringSubmatch(contentStr, -1) {
			routeLabel := fmt.Sprintf("%s %s", strings.ToUpper(m[1]), m[2])
			addRouteEdge(routeLabel, path, path, pkgRoot, "Laravel", nodes, edges)
		}
	}

	// React Router (JSX/TSX)
	if fw.ReactRouter && (strings.HasSuffix(path, ".tsx") || strings.HasSuffix(path, ".jsx")) {
		for _, m := range reactRouterJSXRe.FindAllStringSubmatch(contentStr, -1) {
			if m[1] != "/" {
				addRouteEdge(m[1], path, path, pkgRoot, "React Router", nodes, edges)
			}
		}
		for _, m := range reactRouterPathRe.FindAllStringSubmatch(contentStr, -1) {
			if m[1] != "/" && !strings.HasPrefix(m[1], ":") {
				addRouteEdge(m[1], path, path, pkgRoot, "React Router", nodes, edges)
			}
		}
	}

	// Python FastAPI/Flask
	if strings.HasSuffix(path, ".py") {
		for _, m := range pythonRouteRe.FindAllStringSubmatch(contentStr, -1) {
			routeLabel := fmt.Sprintf("%s %s", strings.ToUpper(m[1]), m[2])
			addRouteEdge(routeLabel, path, path, pkgRoot, "FastAPI/Flask", nodes, edges)
		}
	}

	// Java/Spring
	if strings.HasSuffix(path, ".java") {
		for _, m := range springRouteRe.FindAllStringSubmatch(contentStr, -1) {
			method := strings.TrimSuffix(m[1], "Mapping")
			if method == "Request" {
				method = "ANY"
			} else {
				method = strings.ToUpper(method)
			}
			routeLabel := fmt.Sprintf("%s %s", method, m[2])
			addRouteEdge(routeLabel, path, path, pkgRoot, "Spring", nodes, edges)
		}
	}
}

// extractRoutingEdges parses files for framework routing semantics and returns
// synthetic Route nodes and edges. File-based patterns are evaluated per-package;
// code-based patterns are always active.
func extractRoutingEdges(projPath string, fileContents map[string][]byte, filePkgRoot map[string]string, nodes map[string]*Node) (map[string]*Node, []*Edge) {
	routeNodes := make(map[string]*Node)
	var edges []*Edge

	// Group files by package root.
	pkgFiles := make(map[string]map[string][]byte)
	for path, content := range fileContents {
		pkgRoot := filePkgRoot[path]
		if pkgFiles[pkgRoot] == nil {
			pkgFiles[pkgRoot] = make(map[string][]byte)
		}
		pkgFiles[pkgRoot][path] = content
	}

	// Process each package independently.
	for pkgRoot, files := range pkgFiles {
		fw := detectPackageFrameworks(projPath, nodes, pkgRoot)
		applyConfigOverrides(&fw)

		for path, content := range files {
			extractFileBasedRoutes(path, pkgRoot, fw, routeNodes, &edges)
			extractCodeBasedRoutes(path, content, pkgRoot, fw, routeNodes, &edges)
		}
	}

	return routeNodes, edges
}

// addRouteEdge creates or reuses a route node and appends an edge from the route
// to the target file. The route node ID is scoped by pkgRoot to prevent
// collisions across packages in monorepos.
func addRouteEdge(routePath, targetFileID, path, pkgRoot, framework string, nodes map[string]*Node, edges *[]*Edge) {
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
