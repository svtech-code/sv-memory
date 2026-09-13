package graph

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/svtech-code/sv-memory/internal/graph/schema"
)

// routeFrameworks tracks which file-based routing frameworks are evidenced
// in the project (by config files or package.json dependencies).
type routeFrameworks struct {
	Next      bool // Next.js (Pages Router + App Router)
	Nuxt      bool // Nuxt/Vue
	SvelteKit bool // SvelteKit
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
)

// detectFileRoutingFrameworks determines which file-based routing frameworks
// are active in the project by checking config files and package.json dependencies.
func detectFileRoutingFrameworks(projPath string, nodes map[string]*Node, manifests []string) routeFrameworks {
	var fw routeFrameworks

	// 1. Check config file nodes
	for _, node := range nodes {
		if node.Type != schema.NodeTypeFile {
			continue
		}
		p := node.Path
		if nextConfigRe.MatchString(p) {
			fw.Next = true
		} else if svelteConfigRe.MatchString(p) {
			fw.SvelteKit = true
		} else if nuxtConfigRe.MatchString(p) {
			fw.Nuxt = true
		}
	}

	// 2. Check package.json dependencies
	for _, mf := range manifests {
		if !strings.HasSuffix(mf, "package.json") {
			continue
		}
		absPath := filepath.Join(projPath, mf)
		content, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}
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

	return fw
}

// extractRoutingEdges parses files for framework routing semantics (both
// file-based like Next.js/SvelteKit and code-based like FastAPI/Spring) and
// returns synthetic Route nodes and edges linking them to the implementation files.
// File-based patterns only activate when the corresponding framework is evidenced
// (via config files or package.json dependencies). Code-based patterns (Python,
// Java) are always active since they match against actual code content.
func extractRoutingEdges(fileContents map[string][]byte, frameworks routeFrameworks) (map[string]*Node, []*Edge) {
	routeNodes := make(map[string]*Node)
	var edges []*Edge

	for path, content := range fileContents {
		fileID := path

		// 1. File-based routing (gated by framework evidence)
		if frameworks.SvelteKit {
			if m := svelteRouteRe.FindStringSubmatch(path); len(m) > 0 {
				routePath := "/" + m[1]
				addRouteEdge(routePath, fileID, path, "SvelteKit", routeNodes, &edges)
			}
		}
		if frameworks.Next {
			if m := nextAppRe.FindStringSubmatch(path); len(m) > 0 {
				routePath := "/" + m[1]
				addRouteEdge(routePath, fileID, path, "Next.js (App)", routeNodes, &edges)
			} else if m := nextPagesRe.FindStringSubmatch(path); len(m) > 0 {
				routePath := "/" + m[1]
				if routePath == "/index" {
					routePath = "/"
				}
				addRouteEdge(routePath, fileID, path, "Next.js (Pages)", routeNodes, &edges)
			}
		}
		if frameworks.Nuxt {
			if m := nuxtRe.FindStringSubmatch(path); len(m) > 0 {
				routePath := "/" + m[1]
				if routePath == "/index" {
					routePath = "/"
				}
				addRouteEdge(routePath, fileID, path, "Nuxt/Vue", routeNodes, &edges)
			}
		}

		// 2. Code-based routing (Python) — always active
		if strings.HasSuffix(path, ".py") {
			matches := pythonRouteRe.FindAllStringSubmatch(string(content), -1)
			for _, m := range matches {
				method := strings.ToUpper(m[1])
				routePath := m[2]
				routeLabel := fmt.Sprintf("%s %s", method, routePath)
				addRouteEdge(routeLabel, fileID, path, "FastAPI/Flask", routeNodes, &edges)
			}
		}

		// 3. Code-based routing (Java/Spring) — always active
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
				addRouteEdge(routeLabel, fileID, path, "Spring", routeNodes, &edges)
			}
		}
	}

	return routeNodes, edges
}

func addRouteEdge(routePath, targetFileID, path, framework string, nodes map[string]*Node, edges *[]*Edge) {
	nodeID := "route:" + fmt.Sprintf("%x", sha256.Sum256([]byte(routePath)))[:12]
	if _, exists := nodes[nodeID]; !exists {
		nodes[nodeID] = &Node{
			ID:    nodeID,
			Type:  schema.NodeTypeRoute,
			Label: routePath,
			Path:  routePath,
			Metadata: map[string]interface{}{
				"framework": framework,
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
