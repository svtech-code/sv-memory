package graph

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

type vizNode struct {
	ID        string  `json:"id"`
	Label     string  `json:"label"`
	Title     string  `json:"title"`
	Group     int     `json:"group"`
	Value     float64 `json:"value"`
	Shape     string  `json:"shape"`
	Size      int     `json:"size"`
}

type vizEdge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Label  string `json:"label"`
	Title  string `json:"title"`
	Arrows string `json:"arrows"`
}

func (g *InMemoryGraph) ExportHTML(w io.Writer, commLabels map[int]string) error {
	centrality := g.BetweennessCentrality()
	communities := g.ExtractCommunities()

	minBC, maxBC := 1.0, 1.0
	first := true
	for _, bc := range centrality {
		if first {
			minBC = bc
			maxBC = bc
			first = false
		} else {
			if bc < minBC {
				minBC = bc
			}
			if bc > maxBC {
				maxBC = bc
			}
		}
	}
	bcRange := maxBC - minBC
	if bcRange < 1 {
		bcRange = 1
	}

	var nodes []vizNode
	nodeIDs := make([]string, 0, len(g.Nodes))
	for id := range g.Nodes {
		nodeIDs = append(nodeIDs, id)
	}
	sort.Strings(nodeIDs)

	nodeDegree := make(map[string]int)
	for _, id := range nodeIDs {
		nodeDegree[id] = g.FanIn[id] + g.FanOut[id]
	}

	maxDegree := 0
	for _, d := range nodeDegree {
		if d > maxDegree {
			maxDegree = d
		}
	}
	if maxDegree < 1 {
		maxDegree = 1
	}

	for _, id := range nodeIDs {
		n := g.Nodes[id]
		bc := centrality[id]
		comm := communities[id]
		deg := nodeDegree[id]

		size := 10 + (deg * 30 / maxDegree)
		if size < 10 {
			size = 10
		}
		if size > 60 {
			size = 60
		}

		detail := fmt.Sprintf("ID: %s<br>Type: %s<br>Fan-In: %d<br>Fan-Out: %d<br>BC: %.2f<br>Community: %d<br>Path: %s",
			n.ID, n.Type, g.FanIn[id], g.FanOut[id], bc, comm, n.Path)
		if n.Metadata != nil {
			if lang, ok := n.Metadata["language"]; ok {
				detail += fmt.Sprintf("<br>Language: %v", lang)
			}
			if loc, ok := n.Metadata["loc"]; ok {
				detail += fmt.Sprintf("<br>LOC: %v", loc)
			}
		}

		nodes = append(nodes, vizNode{
			ID:    n.ID,
			Label: n.Label,
			Title: detail,
			Group: comm,
			Value: bc + 1,
			Shape: "dot",
			Size:  size,
		})
	}

	var edges []vizEdge
	seen := make(map[string]bool)
	for _, id := range nodeIDs {
		for _, edge := range g.EdgesBySource[id] {
			key := edge.SourceID + "->" + edge.TargetID
			if seen[key] {
				continue
			}
			seen[key] = true
			title := fmt.Sprintf("Relation: %s<br>Confidence: %s", edge.RelationType, edge.Confidence)
			edges = append(edges, vizEdge{
				From:   edge.SourceID,
				To:     edge.TargetID,
				Label:  edge.RelationType,
				Title:  title,
				Arrows: "to",
			})
		}
	}

	nodesJSON, _ := json.Marshal(nodes)
	edgesJSON, _ := json.Marshal(edges)

	communitiesJSON, _ := json.Marshal(commLabels)

	html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>sv-memory Knowledge Graph</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #1a1a2e; color: #eee; }
  #header { padding: 16px 24px; background: #16213e; border-bottom: 1px solid #0f3460; display: flex; align-items: center; gap: 16px; }
  #header h1 { font-size: 18px; color: #e94560; }
  #header input { flex: 1; max-width: 400px; padding: 8px 12px; border: 1px solid #0f3460; border-radius: 6px; background: #1a1a2e; color: #eee; font-size: 14px; }
  #header input:focus { outline: none; border-color: #e94560; }
  #header .stats { font-size: 13px; color: #888; }
  #container { display: flex; height: calc(100vh - 60px); }
  #network { flex: 1; }
  #legend { width: 280px; overflow-y: auto; padding: 16px; background: #16213e; border-left: 1px solid #0f3460; font-size: 13px; }
  #legend h3 { color: #e94560; margin-bottom: 12px; font-size: 14px; }
  #legend .comm { display: flex; align-items: center; gap: 8px; padding: 4px 0; cursor: pointer; }
  #legend .comm:hover { background: #1a1a2e; }
  #legend .comm .dot { width: 12px; height: 12px; border-radius: 50%%; flex-shrink: 0; }
  #legend .comm .name { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  #legend .comm .count { color: #888; margin-left: auto; }
  #detail { display: none; position: fixed; top: 50%%; left: 50%%; transform: translate(-50%%, -50%%); background: #16213e; border: 1px solid #0f3460; border-radius: 8px; padding: 24px; max-width: 480px; width: 90%%; z-index: 100; max-height: 80vh; overflow-y: auto; }
  #detail h2 { color: #e94560; margin-bottom: 12px; }
  #detail .row { padding: 4px 0; display: flex; }
  #detail .row .l { color: #888; width: 120px; flex-shrink: 0; }
  #detail .row .v { color: #eee; }
  #detail .close { float: right; cursor: pointer; color: #888; font-size: 20px; }
  #detail .close:hover { color: #e94560; }
  #overlay { display: none; position: fixed; top: 0; left: 0; width: 100%%; height: 100%%; background: rgba(0,0,0,0.5); z-index: 99; }
</style>
</head>
<body>
<div id="header">
  <h1>sv-memory</h1>
  <input id="search" type="text" placeholder="Search nodes..." oninput="filterNodes(this.value)">
  <span class="stats" id="stats"></span>
</div>
<div id="container">
  <div id="network"></div>
  <div id="legend">
    <h3>Communities</h3>
    <div id="legend-list"></div>
  </div>
</div>
<div id="overlay" onclick="hideDetail()"></div>
<div id="detail">
  <span class="close" onclick="hideDetail()">&times;</span>
  <h2 id="detail-title"></h2>
  <div id="detail-body"></div>
</div>
<script src="https://cdnjs.cloudflare.com/ajax/libs/vis-network/9.1.6/vis-network.min.js"></script>
<script>
const PALETTE = [
  '#e94560','#0f3460','#16a085','#f39c12','#9b59b6','#2ecc71',
  '#e67e22','#1abc9c','#c0392b','#3498db','#f1c40f','#2980b9',
  '#d35400','#27ae60','#8e44ad','#ecf0f1','#7f8c8d','#34495e'
];

const nodes = new vis.DataSet(%s);
const edges = new vis.DataSet(%s);
const communities = %s;

const container = document.getElementById('network');
const data = { nodes, edges };
const options = {
  physics: { stabilization: { iterations: 100 }, solver: 'forceAtlas2Based', forceAtlas2Based: { gravitationalConstant: -40, centralGravity: 0.005, springLength: 120, springConstant: 0.08, damping: 0.4 } },
  edges: { smooth: { type: 'continuous' }, font: { size: 10, color: '#888' }, color: { inherit: 'to', opacity: 0.5 } },
  nodes: { font: { size: 12, color: '#eee' }, borderWidth: 1, borderWidthSelected: 2 },
  interaction: { hover: true, tooltipDelay: 200, navigationButtons: true, keyboard: true },
  groups: {}
};

const commIds = Object.keys(communities).map(Number);
commIds.forEach((id, i) => {
  const color = PALETTE[id %% PALETTE.length];
  options.groups[id] = { color: { background: color, border: '#fff' } };
});

const network = new vis.Network(container, data, options);

document.getElementById('stats').textContent = nodes.length + ' nodes, ' + edges.length + ' edges';

let legendHTML = '';
commIds.sort((a, b) => {
  const ca = nodes.get().filter(n => n.group === a).length;
  const cb = nodes.get().filter(n => n.group === b).length;
  return cb - ca;
});
commIds.forEach((id, i) => {
  const color = PALETTE[id %% PALETTE.length];
  const label = communities[id] || 'community_' + id;
  const count = nodes.get().filter(n => n.group === id).length;
  legendHTML += '<div class="comm" onclick="focusCommunity(' + id + ')">'
    + '<div class="dot" style="background:' + color + '"></div>'
    + '<span class="name">' + label + '</span>'
    + '<span class="count">' + count + '</span></div>';
});
document.getElementById('legend-list').innerHTML = legendHTML;

network.on('click', function(params) {
  if (params.nodes.length > 0) {
    const nId = params.nodes[0];
    const node = nodes.get(nId);
    if (node) showDetail(node);
  } else {
    hideDetail();
  }
});

function focusCommunity(id) {
  const commNodes = nodes.get().filter(n => n.group === id);
  if (commNodes.length > 0) {
    network.selectNodes(commNodes.map(n => n.id));
    network.focus(commNodes[0].id, { scale: 1.5, animation: true });
  }
}

function filterNodes(query) {
  if (!query) {
    nodes.forEach(n => nodes.update({ id: n.id, hidden: false }));
    return;
  }
  const q = query.toLowerCase();
  nodes.forEach(n => {
    const match = n.label.toLowerCase().includes(q) || n.id.toLowerCase().includes(q);
    nodes.update({ id: n.id, hidden: !match });
  });
  network.setData({ nodes, edges });
}

let currentDetail = null;
function showDetail(node) {
  currentDetail = node;
  document.getElementById('detail-title').textContent = node.label;
  document.getElementById('detail-body').innerHTML = node.title.replace(/<br>/g, '<div class="row"><span class="l">').replace(/:/g, ':</span><span class="v">') + '</span></div>';
  document.getElementById('overlay').style.display = 'block';
  document.getElementById('detail').style.display = 'block';
}

function hideDetail() {
  document.getElementById('overlay').style.display = 'none';
  document.getElementById('detail').style.display = 'none';
  currentDetail = null;
}
</script>
</body>
</html>`, string(nodesJSON), string(edgesJSON), string(communitiesJSON))

	_, err := fmt.Fprint(w, html)
	return err
}
