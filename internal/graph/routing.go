package graph

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"

	"github.com/svtech-code/sv-memory/internal/graph/schema"
)

var (
	// File-based routing patterns
	svelteRouteRe = regexp.MustCompile(`src/routes/(.+?)/\+(page|server|layout)\.(svelte|ts|js)$`)
	nextAppRe     = regexp.MustCompile(`app/(.+?)/(route|page|layout)\.(ts|js|tsx|jsx)$`)
	nextPagesRe   = regexp.MustCompile(`pages/(.+?)\.(ts|js|tsx|jsx)$`)
	nuxtRe        = regexp.MustCompile(`pages/(.+?)\.vue$`)

	// Code-based routing patterns (Python FastAPI/Flask, Java Spring)
	pythonRouteRe = regexp.MustCompile(`(?m)^\s*@(?:app|router|blueprint)\.(get|post|put|delete|patch)\(['"]([^'"]+)['"]`)
	springRouteRe = regexp.MustCompile(`(?m)^\s*@(GetMapping|PostMapping|PutMapping|DeleteMapping|PatchMapping|RequestMapping)\((?:value=)?(?:\[)?['"]([^'"]+)['"]`)
)

// extractRoutingEdges parses files for framework routing semantics (both
// file-based like Next.js/SvelteKit and code-based like FastAPI/Spring) and
// returns synthetic Route nodes and edges linking them to the implementation files.
func extractRoutingEdges(fileContents map[string][]byte) (map[string]*Node, []*Edge) {
	routeNodes := make(map[string]*Node)
	var edges []*Edge

	for path, content := range fileContents {
		fileID := "file:" + path
		
		// 1. File-based routing
		if m := svelteRouteRe.FindStringSubmatch(path); len(m) > 0 {
			routePath := "/" + m[1]
			addRouteEdge(routePath, fileID, path, "SvelteKit", routeNodes, &edges)
		} else if m := nextAppRe.FindStringSubmatch(path); len(m) > 0 {
			routePath := "/" + m[1]
			addRouteEdge(routePath, fileID, path, "Next.js (App)", routeNodes, &edges)
		} else if m := nextPagesRe.FindStringSubmatch(path); len(m) > 0 {
			routePath := "/" + m[1]
			if routePath == "/index" {
				routePath = "/"
			}
			addRouteEdge(routePath, fileID, path, "Next.js (Pages)", routeNodes, &edges)
		} else if m := nuxtRe.FindStringSubmatch(path); len(m) > 0 {
			routePath := "/" + m[1]
			if routePath == "/index" {
				routePath = "/"
			}
			addRouteEdge(routePath, fileID, path, "Nuxt/Vue", routeNodes, &edges)
		}

		// 2. Code-based routing (Python)
		if strings.HasSuffix(path, ".py") {
			matches := pythonRouteRe.FindAllStringSubmatch(string(content), -1)
			for _, m := range matches {
				method := strings.ToUpper(m[1])
				routePath := m[2]
				routeLabel := fmt.Sprintf("%s %s", method, routePath)
				addRouteEdge(routeLabel, fileID, path, "FastAPI/Flask", routeNodes, &edges)
			}
		}

		// 3. Code-based routing (Java/Spring)
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
