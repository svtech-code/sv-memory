package graph

import (
	"database/sql"
	"encoding/json"
)

// DetectCommunities runs a label propagation community detection algorithm on the graph.
func (g *InMemoryGraph) DetectCommunities() map[string]int {
	nodeList := make([]string, 0, len(g.Nodes))
	nodeIndex := make(map[string]int)

	idx := 0
	for id := range g.Nodes {
		nodeList = append(nodeList, id)
		nodeIndex[id] = idx
		idx++
	}

	labels := make([]int, len(nodeList))
	for i := range labels {
		labels[i] = i
	}

	maxIterations := 15
	for iter := 0; iter < maxIterations; iter++ {
		changed := false
		for i, id := range nodeList {
			labelFreq := make(map[int]float64)

			// Outgoing edges
			for _, edge := range g.EdgesBySource[id] {
				targetIdx, ok := nodeIndex[edge.TargetID]
				if ok {
					labelFreq[labels[targetIdx]] += 1.0
				}
			}
			// Incoming edges
			for _, edge := range g.EdgesByTarget[id] {
				sourceIdx, ok := nodeIndex[edge.SourceID]
				if ok {
					labelFreq[labels[sourceIdx]] += 1.0
				}
			}

			if len(labelFreq) == 0 {
				continue
			}

			maxFreq := -1.0
			bestLabel := labels[i]
			for lbl, freq := range labelFreq {
				if freq > maxFreq {
					maxFreq = freq
					bestLabel = lbl
				} else if freq == maxFreq && lbl < bestLabel {
					bestLabel = lbl
				}
			}

			if labels[i] != bestLabel {
				labels[i] = bestLabel
				changed = true
			}
		}
		if !changed {
			break
		}
	}

	communities := make(map[string]int)
	labelMap := make(map[int]int)
	commIdx := 1
	for i, id := range nodeList {
		lbl := labels[i]
		c, ok := labelMap[lbl]
		if !ok {
			labelMap[lbl] = commIdx
			c = commIdx
			commIdx++
		}
		communities[id] = c
	}

	return communities
}

// BetweennessCentrality calculates betweenness centrality for all nodes using Brandes' algorithm.
func (g *InMemoryGraph) BetweennessCentrality() map[string]float64 {
	centrality := make(map[string]float64)
	for id := range g.Nodes {
		centrality[id] = 0.0
	}

	for s := range g.Nodes {
		S := []string{}
		pred := make(map[string][]string)
		sigma := make(map[string]float64)
		dist := make(map[string]int)

		for v := range g.Nodes {
			dist[v] = -1
		}

		sigma[s] = 1.0
		dist[s] = 0

		queue := []string{s}

		for len(queue) > 0 {
			v := queue[0]
			queue = queue[1:]
			S = append(S, v)

			for _, edge := range g.EdgesBySource[v] {
				w := edge.TargetID
				if _, exists := g.Nodes[w]; !exists {
					continue
				}
				if dist[w] < 0 {
					dist[w] = dist[v] + 1
					queue = append(queue, w)
				}
				if dist[w] == dist[v]+1 {
					sigma[w] += sigma[v]
					pred[w] = append(pred[w], v)
				}
			}
		}

		delta := make(map[string]float64)
		for len(S) > 0 {
			w := S[len(S)-1]
			S = S[:len(S)-1]

			for _, v := range pred[w] {
				delta[v] += (sigma[v] / sigma[w]) * (1.0 + delta[w])
			}
			if w != s {
				centrality[w] += delta[w]
			}
		}
	}

	return centrality
}

// UpdateCommunitiesAndCentrality updates the community_id and betweenness_centrality in node metadata.
func UpdateCommunitiesAndCentrality(db *sql.DB, projectID string) error {
	g, err := LoadFullGraph(db, projectID)
	if err != nil {
		return err
	}

	comms := g.DetectCommunities()
	centrality := g.BetweennessCentrality()

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("UPDATE graph_nodes SET metadata = ? WHERE id = ? AND project_id = ?")
	if err != nil {
		return err
	}
	defer stmt.Close()

	for id, node := range g.Nodes {
		if node.Metadata == nil {
			node.Metadata = make(map[string]interface{})
		}
		node.Metadata["community_id"] = comms[id]
		node.Metadata["betweenness_centrality"] = centrality[id]

		metaBytes, _ := json.Marshal(node.Metadata)
		metaStr := string(metaBytes)
		if metaStr == "null" {
			metaStr = "{}"
		}

		if _, err := stmt.Exec(metaStr, id, projectID); err != nil {
			return err
		}
	}

	return tx.Commit()
}
