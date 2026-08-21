package graph

import (
	"database/sql"
	"fmt"
)

// BlastRadiusNode represents a transitive upstream consumer (caller, importer)
// that depends on the target entity within the code graph.
type BlastRadiusNode struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	Type         string `json:"type"`
	Path         string `json:"path"`
	Depth        int    `json:"depth"`         // Hop distance from target (1, 2, 3...)
	RelationType string `json:"relation_type"` // e.g. "calls", "imports", "implements"
	IsHub        bool   `json:"is_hub"`        // High degree / betweenness centrality hotspot
}

// CalculateBlastRadius performs a bounded BFS traversal upstream (following incoming edges:
// nodes where target_id == current) to find all transitive consumers up to maxDepth and maxNodes.
func CalculateBlastRadius(db *sql.DB, projectID, nodeID string, maxDepth, maxNodes int) ([]BlastRadiusNode, error) {
	if maxDepth <= 0 {
		maxDepth = 3
	}
	if maxDepth > 5 {
		maxDepth = 5
	}
	if maxNodes <= 0 {
		maxNodes = 15
	}
	if maxNodes > 50 {
		maxNodes = 50
	}

	visited := map[string]bool{nodeID: true}
	var results []BlastRadiusNode

	type queueItem struct {
		id    string
		depth int
	}

	queue := []queueItem{{id: nodeID, depth: 0}}

	for len(queue) > 0 && len(results) < maxNodes {
		curr := queue[0]
		queue = queue[1:]

		if curr.depth >= maxDepth {
			continue
		}

		nextDepth := curr.depth + 1

		// Query incoming edges pointing at curr.id
		rows, err := db.Query(`
			SELECT e.source_id, n.label, n.node_type, n.path, e.relation_type,
				(SELECT COUNT(*) FROM graph_edges ge WHERE ge.project_id = e.project_id AND (ge.source_id = e.source_id OR ge.target_id = e.source_id)) AS degree
			FROM graph_edges e
			JOIN graph_nodes n ON n.id = e.source_id AND n.project_id = e.project_id
			WHERE e.project_id = ? AND e.target_id = ? AND e.relation_type != 'rationale_for'
			ORDER BY degree DESC
			LIMIT ?`, projectID, curr.id, maxNodes-len(results))
		if err != nil {
			return nil, fmt.Errorf("failed querying incoming blast radius edges: %w", err)
		}

		for rows.Next() {
			var srcID, label, nodeType, path, relType string
			var degree int
			if scanErr := rows.Scan(&srcID, &label, &nodeType, &path, &relType, &degree); scanErr != nil {
				rows.Close()
				return nil, scanErr
			}

			if !visited[srcID] {
				visited[srcID] = true
				isHub := degree >= 10
				results = append(results, BlastRadiusNode{
					ID:           srcID,
					Label:        label,
					Type:         nodeType,
					Path:         path,
					Depth:        nextDepth,
					RelationType: relType,
					IsHub:        isHub,
				})
				queue = append(queue, queueItem{id: srcID, depth: nextDepth})
				if len(results) >= maxNodes {
					break
				}
			}
		}
		rows.Close()
		if rows.Err() != nil {
			return nil, rows.Err()
		}
	}

	return results, nil
}
