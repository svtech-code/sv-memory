package graph

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/svtech-code/sv-memory/internal/graph/schema"
)

// DetectCommunities runs a label propagation community detection algorithm on the graph.
func (g *InMemoryGraph) DetectCommunities() map[string]int {
	nodeList := make([]string, 0, len(g.Nodes))
	for id := range g.Nodes {
		nodeList = append(nodeList, id)
	}
	sort.Strings(nodeList)

	nodeIndex := make(map[string]int)
	for idx, id := range nodeList {
		nodeIndex[id] = idx
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

	comms := g.LeidenDetectCommunities()
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
		meta := make(map[string]interface{})
		for k, v := range node.Metadata {
			meta[k] = v
		}
		meta["community_id"] = comms[id]
		meta["betweenness_centrality"] = centrality[id]

		metaBytes, _ := json.Marshal(meta)
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

// ExtractCommunities reads community_id from node metadata and returns a community map.
func (g *InMemoryGraph) ExtractCommunities() map[string]int {
	communities := make(map[string]int)
	for id, node := range g.Nodes {
		if node.Metadata == nil {
			continue
		}
		val, ok := node.Metadata["community_id"]
		if !ok {
			continue
		}
		switch v := val.(type) {
		case float64:
			communities[id] = int(v)
		case int:
			communities[id] = v
		case int64:
			communities[id] = int(v)
		}
	}
	return communities
}

func (g *InMemoryGraph) DetectCommunityLabels(communities map[string]int, centrality map[string]float64) map[int]string {
	commBest := make(map[int]string)
	commFallback := make(map[int]string)

	for id, cID := range communities {
		node, ok := g.Nodes[id]
		if !ok {
			continue
		}
		label := cleanLabel(node, 50)
		if node.Type == schema.NodeTypeFile && label != "" {
			if existing, has := commBest[cID]; !has || centrality[id] > centrality[existing] {
				commBest[cID] = id
			}
		} else if label != "" {
			if existing, has := commFallback[cID]; !has || centrality[id] > centrality[existing] {
				commFallback[cID] = id
			}
		}
	}

	commLabels := make(map[int]string)
	for cID, id := range commBest {
		commLabels[cID] = cleanLabel(g.Nodes[id], 50)
	}
	for cID, id := range commFallback {
		if _, has := commLabels[cID]; !has {
			commLabels[cID] = cleanLabel(g.Nodes[id], 50)
		}
	}
	for _, cID := range communities {
		if _, has := commLabels[cID]; !has {
			commLabels[cID] = fmt.Sprintf("community_%d", cID)
		}
	}

	return commLabels
}

func cleanLabel(node *Node, maxLen int) string {
	label := strings.TrimSpace(node.Label)
	if label == "" {
		return ""
	}
	// Truncate by runes so multibyte characters are never split.
	if runes := []rune(label); len(runes) > maxLen {
		label = string(runes[:maxLen])
	}
	if node.Type == schema.NodeTypeFile {
		return label
	}
	firstLine := strings.SplitN(label, "\n", 2)
	label = strings.TrimSpace(firstLine[0])
	if runes := []rune(label); len(runes) > maxLen {
		label = string(runes[:maxLen])
	}
	return label
}

// SurprisingConnection represents a cross-community edge with a surprise score.
type SurprisingConnection struct {
	SourceID      string  `json:"source_id"`
	SourceLabel   string  `json:"source_label"`
	TargetID      string  `json:"target_id"`
	TargetLabel   string  `json:"target_label"`
	EdgeType      string  `json:"edge_type"`
	Confidence    string  `json:"confidence"`
	SrcCommunity  int     `json:"src_community"`
	DstCommunity  int     `json:"dst_community"`
	SurpriseScore float64 `json:"surprise_score"`
}

// FindSurprisingConnections finds cross-community edges that bridge different
// parts of the codebase. A connection is "surprising" when it links two different
// communities via endpoints that are individually low-degree (not obvious hubs).
func (g *InMemoryGraph) FindSurprisingConnections(communities map[string]int, centrality map[string]float64, limit int) []SurprisingConnection {
	if limit <= 0 {
		limit = 10
	}

	var candidates []SurprisingConnection
	seen := make(map[string]bool)

	for srcID, edges := range g.EdgesBySource {
		srcComm, srcOk := communities[srcID]
		if !srcOk {
			continue
		}
		for _, edge := range edges {
			dstComm, dstOk := communities[edge.TargetID]
			if !dstOk {
				continue
			}

			key := srcID + "->" + edge.TargetID
			if seen[key] {
				continue
			}
			seen[key] = true

			sDegree := g.FanIn[srcID] + g.FanOut[srcID]
			dDegree := g.FanIn[edge.TargetID] + g.FanOut[edge.TargetID]
			sBC := centrality[srcID]
			dBC := centrality[edge.TargetID]

			var score float64
			if srcComm != dstComm {
				avgBC := (sBC + dBC) / 2.0
				totalDegree := sDegree + dDegree
				if totalDegree == 0 {
					totalDegree = 1
				}
				score = avgBC * 2.0 / float64(totalDegree)
			}

			if score > 0 {
				srcLabel := srcID
				if n, ok := g.Nodes[srcID]; ok {
					srcLabel = n.Label
				}
				dstLabel := edge.TargetID
				if n, ok := g.Nodes[edge.TargetID]; ok {
					dstLabel = n.Label
				}

				candidates = append(candidates, SurprisingConnection{
					SourceID:      srcID,
					SourceLabel:   srcLabel,
					TargetID:      edge.TargetID,
					TargetLabel:   dstLabel,
					EdgeType:      edge.RelationType,
					Confidence:    edge.Confidence,
					SrcCommunity:  srcComm,
					DstCommunity:  dstComm,
					SurpriseScore: score,
				})
			}
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].SurpriseScore > candidates[j].SurpriseScore
	})

	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	return candidates
}
