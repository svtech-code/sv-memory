package graph

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"sort"
)

type vizNode struct {
	ID    string  `json:"id"`
	Label string  `json:"label"`
	Title string  `json:"title"`
	Group int     `json:"group"`
	Value float64 `json:"value"`
	Shape string  `json:"shape"`
	Size  int     `json:"size"`
}

type vizEdge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Label  string `json:"label"`
	Title  string `json:"title"`
	Arrows string `json:"arrows"`
}

func (g *InMemoryGraph) ExportHTML(w io.Writer, comms map[string]int, commLabels map[int]string) error {
	nodes, edges := g.vizData(comms)

	nodesJSON, _ := json.Marshal(nodes)
	edgesJSON, _ := json.Marshal(edges)
	communitiesJSON, _ := json.Marshal(commLabels)

	html := fmt.Sprintf(vizHTMLTemplate, string(nodesJSON), string(edgesJSON), string(communitiesJSON))
	_, err := fmt.Fprint(w, html)
	return err
}

// vizData builds the node and edge payloads for the HTML viewer.
func (g *InMemoryGraph) vizData(comms map[string]int) ([]vizNode, []vizEdge) {
	centrality := g.BetweennessCentrality()
	nodeIDs := g.sortedNodeIDs()
	nodeDegree := g.vizNodeDegrees(nodeIDs)
	maxDegree := maxVizDegree(nodeDegree)

	nodes := make([]vizNode, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		n := g.Nodes[id]
		bc := centrality[id]
		comm := comms[id]
		nodes = append(nodes, vizNode{
			ID:    n.ID,
			Label: n.Label,
			Title: vizNodeDetail(n, g.FanIn[id], g.FanOut[id], bc, comm),
			Group: comm,
			Value: bc + 1,
			Shape: "dot",
			Size:  vizNodeSize(nodeDegree[id], maxDegree),
		})
	}
	return nodes, g.vizEdges(nodeIDs)
}

func (g *InMemoryGraph) sortedNodeIDs() []string {
	ids := make([]string, 0, len(g.Nodes))
	for id := range g.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (g *InMemoryGraph) vizNodeDegrees(nodeIDs []string) map[string]int {
	degree := make(map[string]int, len(nodeIDs))
	for _, id := range nodeIDs {
		degree[id] = g.FanIn[id] + g.FanOut[id]
	}
	return degree
}

func maxVizDegree(degree map[string]int) int {
	maxDegree := 0
	for _, d := range degree {
		if d > maxDegree {
			maxDegree = d
		}
	}
	if maxDegree < 1 {
		maxDegree = 1
	}
	return maxDegree
}

func vizNodeSize(degree, maxDegree int) int {
	size := 10 + (degree * 30 / maxDegree)
	if size < 10 {
		return 10
	}
	if size > 60 {
		return 60
	}
	return size
}

func vizNodeDetail(n *Node, fanIn, fanOut int, bc float64, comm int) string {
	detail := fmt.Sprintf("ID: %s<br>Type: %s<br>Fan-In: %d<br>Fan-Out: %d<br>BC: %.2f<br>Community: %d<br>Path: %s",
		html.EscapeString(n.ID), html.EscapeString(n.Type), fanIn, fanOut, bc, comm, html.EscapeString(n.Path))
	if n.Metadata == nil {
		return detail
	}
	if lang, ok := n.Metadata["language"]; ok {
		detail += fmt.Sprintf("<br>Language: %v", html.EscapeString(fmt.Sprint(lang)))
	}
	if loc, ok := n.Metadata["loc"]; ok {
		detail += fmt.Sprintf("<br>LOC: %v", html.EscapeString(fmt.Sprint(loc)))
	}
	return detail
}

func (g *InMemoryGraph) vizEdges(nodeIDs []string) []vizEdge {
	var edges []vizEdge
	seen := make(map[string]bool)
	for _, id := range nodeIDs {
		for _, edge := range g.EdgesBySource[id] {
			key := edge.SourceID + "->" + edge.TargetID
			if seen[key] {
				continue
			}
			seen[key] = true
			edges = append(edges, vizEdge{
				From:   edge.SourceID,
				To:     edge.TargetID,
				Label:  edge.RelationType,
				Title:  fmt.Sprintf("Relation: %s<br>Confidence: %s", edge.RelationType, edge.Confidence),
				Arrows: "to",
			})
		}
	}
	return edges
}
