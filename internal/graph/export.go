package graph

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ExportObsidianVault exports the project's graph nodes and memories as an Obsidian Vault folder.
func ExportObsidianVault(db *sql.DB, projectID, outputDir string) error {
	nodesDir := filepath.Join(outputDir, "nodes")
	memDir := filepath.Join(outputDir, "memories")

	if err := os.MkdirAll(nodesDir, 0755); err != nil {
		return fmt.Errorf("failed to create nodes dir: %w", err)
	}
	if err := os.MkdirAll(memDir, 0755); err != nil {
		return fmt.Errorf("failed to create memories dir: %w", err)
	}

	// 1. Export Graph Nodes
	type nodeInfo struct {
		id, nType, label, pathStr string
	}
	var nodeList []nodeInfo

	nodeRows, err := db.Query("SELECT id, node_type, label, path FROM graph_nodes WHERE project_id = ?", projectID)
	if err != nil {
		return fmt.Errorf("failed to query graph_nodes: %w", err)
	}
	for nodeRows.Next() {
		var n nodeInfo
		if scanErr := nodeRows.Scan(&n.id, &n.nType, &n.label, &n.pathStr); scanErr == nil {
			nodeList = append(nodeList, n)
		}
	}
	nodeRows.Close()

	for _, n := range nodeList {
		safeFileName := sanitizeFilename(n.id) + ".md"
		filePath := filepath.Join(nodesDir, safeFileName)

		var sb strings.Builder
		fmt.Fprintf(&sb, "# %s\n\n", n.label)
		fmt.Fprintf(&sb, "- **Node ID:** `%s`\n", n.id)
		fmt.Fprintf(&sb, "- **Type:** `%s`\n", n.nType)
		if n.pathStr != "" {
			fmt.Fprintf(&sb, "- **Path:** `%s`\n", n.pathStr)
		}
		sb.WriteString("\n## Connected Edges\n\n")

		// Query edges for this node
		edgeRows, eErr := db.Query("SELECT target_id, relation_type FROM graph_edges WHERE project_id = ? AND source_id = ?", projectID, n.id)
		if eErr == nil {
			for edgeRows.Next() {
				var targetID, relType string
				if eScan := edgeRows.Scan(&targetID, &relType); eScan == nil {
					targetFile := sanitizeFilename(targetID)
					fmt.Fprintf(&sb, "- -[%s]-> [[%s]]\n", relType, targetFile)
				}
			}
			edgeRows.Close()
		}

		_ = os.WriteFile(filePath, []byte(sb.String()), 0644)
	}

	// 2. Export Memories
	memRows, err := db.Query("SELECT id, category, what, why, learned, created_at FROM memories WHERE project_id = ? AND deleted_at IS NULL", projectID)
	if err == nil {
		defer memRows.Close()
		for memRows.Next() {
			var id, cat, what, why, learned, createdAt string
			if mScan := memRows.Scan(&id, &cat, &what, &why, &learned, &createdAt); mScan == nil {
				filePath := filepath.Join(memDir, id+".md")
				var sb strings.Builder
				fmt.Fprintf(&sb, "# %s\n\n", what)
				fmt.Fprintf(&sb, "- **Memory ID:** `%s`\n", id)
				fmt.Fprintf(&sb, "- **Category:** `%s`\n", strings.ToUpper(cat))
				fmt.Fprintf(&sb, "- **Created:** `%s`\n\n", createdAt)
				fmt.Fprintf(&sb, "### Why\n%s\n\n", why)
				fmt.Fprintf(&sb, "### Learned\n%s\n", learned)
				_ = os.WriteFile(filePath, []byte(sb.String()), 0644)
			}
		}
	}

	return nil
}

// ExportCypher exports the graph nodes and edges as Cypher statements for Neo4j / FalkorDB.
func ExportCypher(db *sql.DB, projectID, outputPath string) error {
	var sb strings.Builder
	fmt.Fprintf(&sb, "// Cypher Export for sv-memory project %s\n", projectID)
	sb.WriteString("// Compatible with Neo4j and FalkorDB\n\n")

	// 1. Export Nodes
	nodeRows, err := db.Query("SELECT id, node_type, label, path FROM graph_nodes WHERE project_id = ?", projectID)
	if err != nil {
		return fmt.Errorf("failed to query graph_nodes for cypher: %w", err)
	}
	defer nodeRows.Close()

	for nodeRows.Next() {
		var id, nType, label, pathStr string
		if scanErr := nodeRows.Scan(&id, &nType, &label, &pathStr); scanErr == nil {
			cleanID := escapeCypherStr(id)
			cleanLabel := escapeCypherStr(label)
			cleanType := sanitizeCypherLabel(nType)
			fmt.Fprintf(&sb, "CREATE (:Node:%s {id: \"%s\", label: \"%s\", path: \"%s\"});\n", cleanType, cleanID, cleanLabel, escapeCypherStr(pathStr))
		}
	}

	sb.WriteString("\n// Edges\n")

	// 2. Export Edges
	edgeRows, err := db.Query("SELECT source_id, target_id, relation_type FROM graph_edges WHERE project_id = ?", projectID)
	if err == nil {
		defer edgeRows.Close()
		for edgeRows.Next() {
			var srcID, tgtID, relType string
			if eScan := edgeRows.Scan(&srcID, &tgtID, &relType); eScan == nil {
				cleanRel := sanitizeCypherLabel(relType)
				fmt.Fprintf(&sb, "MATCH (a:Node {id: \"%s\"}), (b:Node {id: \"%s\"}) CREATE (a)-[:%s]->(b);\n", escapeCypherStr(srcID), escapeCypherStr(tgtID), cleanRel)
			}
		}
	}

	return os.WriteFile(outputPath, []byte(sb.String()), 0644)
}

func sanitizeFilename(name string) string {
	r := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	return r.Replace(name)
}

func escapeCypherStr(s string) string {
	return strings.ReplaceAll(s, "\"", "\\\"")
}

func sanitizeCypherLabel(s string) string {
	r := strings.NewReplacer("-", "_", " ", "_", "/", "_", ".", "_")
	return strings.ToUpper(r.Replace(s))
}
