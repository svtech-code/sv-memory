package graph

import (
	"math/rand"
	"sort"
)

type leidenState struct {
	n         int
	nodeIDs   []string
	nodeIndex map[string]int
	community []int
	tot       []float64
	m         float64
	gamma     float64
	rng       *rand.Rand
}

func newLeidenState(g *InMemoryGraph) *leidenState {
	nodeIDs := make([]string, 0, len(g.Nodes))
	for id := range g.Nodes {
		nodeIDs = append(nodeIDs, id)
	}
	sort.Strings(nodeIDs)
	n := len(nodeIDs)

	nodeIndex := make(map[string]int, n)
	for i, id := range nodeIDs {
		nodeIndex[id] = i
	}

	m := 0.0
	for _, id := range nodeIDs {
		m += float64(g.degree(id))
	}
	m /= 2.0

	community := make([]int, n)
	tot := make([]float64, n)
	for i, id := range nodeIDs {
		community[i] = i
		tot[i] = float64(g.degree(id))
	}

	return &leidenState{
		n:         n,
		nodeIDs:   nodeIDs,
		nodeIndex: nodeIndex,
		community: community,
		tot:       tot,
		m:         m,
		gamma:     1.0,
		rng:       rand.New(rand.NewSource(42)),
	}
}

func (g *InMemoryGraph) degree(id string) int {
	return g.FanIn[id] + g.FanOut[id]
}

func (g *InMemoryGraph) degreeByIdx(idx int, ls *leidenState) float64 {
	return float64(g.degree(ls.nodeIDs[idx]))
}

func (g *InMemoryGraph) LeidenDetectCommunities() map[string]int {
	if len(g.Nodes) == 0 {
		return nil
	}

	ls := newLeidenState(g)
	if ls.m == 0 {
		result := make(map[string]int)
		for i, id := range ls.nodeIDs {
			result[id] = i + 1
		}
		return result
	}

	for iter := 0; iter < 15; iter++ {
		prevCount := g.countCommunities(ls.community)
		g.leidenLocalMoving(ls)
		if g.countCommunities(ls.community) >= prevCount {
			break
		}
	}

	result := make(map[string]int)
	remap := make(map[int]int)
	nextID := 1
	for i, id := range ls.nodeIDs {
		c := ls.community[i]
		if _, ok := remap[c]; !ok {
			remap[c] = nextID
			nextID++
		}
		result[id] = remap[c]
	}

	return result
}

func (g *InMemoryGraph) leidenLocalMoving(ls *leidenState) {
	for localIter := 0; localIter < 10; localIter++ {
		if !g.leidenSweep(ls) {
			return
		}
	}
}

// leidenSweep performs one local-moving pass over a random node order; reports
// whether any node moved.
func (g *InMemoryGraph) leidenSweep(ls *leidenState) bool {
	moved := false
	for _, idx := range ls.rng.Perm(ls.n) {
		if g.leidenMoveNode(ls, idx) {
			moved = true
		}
	}
	return moved
}

// leidenMoveNode relocates a single node to its best neighbouring community;
// reports whether it moved.
func (g *InMemoryGraph) leidenMoveNode(ls *leidenState, idx int) bool {
	curComm := ls.community[idx]
	k_v := g.degreeByIdx(idx, ls)
	neighborComms := g.neighborCommunityWeights(ls, idx)
	bestComm, _ := g.bestCommunity(ls, curComm, k_v, neighborComms)
	if bestComm == curComm {
		return false
	}
	ls.tot[curComm] -= k_v
	ls.tot[bestComm] += k_v
	ls.community[idx] = bestComm
	return true
}

// neighborCommunityWeights tallies edge weight per neighbouring community.
func (g *InMemoryGraph) neighborCommunityWeights(ls *leidenState, idx int) map[int]float64 {
	weights := make(map[int]float64)
	for _, edge := range g.EdgesBySource[ls.nodeIDs[idx]] {
		if tIdx, ok := ls.nodeIndex[edge.TargetID]; ok {
			weights[ls.community[tIdx]] += 1.0
		}
	}
	for _, edge := range g.EdgesByTarget[ls.nodeIDs[idx]] {
		if sIdx, ok := ls.nodeIndex[edge.SourceID]; ok {
			weights[ls.community[sIdx]] += 1.0
		}
	}
	return weights
}

// bestCommunity picks the neighbouring community with the largest positive
// modularity delta, or curComm when none improves on it.
func (g *InMemoryGraph) bestCommunity(ls *leidenState, curComm int, k_v float64, neighborComms map[int]float64) (int, float64) {
	bestComm := curComm
	bestDelta := 0.0
	removeDelta := -neighborComms[curComm]/ls.m +
		ls.gamma*k_v*(ls.tot[curComm]-k_v)/(2.0*ls.m*ls.m)

	for nc, w := range neighborComms {
		if nc == curComm {
			continue
		}
		addDelta := w/ls.m - ls.gamma*k_v*ls.tot[nc]/(2.0*ls.m*ls.m)
		if totalDelta := removeDelta + addDelta; totalDelta > bestDelta+1e-12 {
			bestDelta = totalDelta
			bestComm = nc
		}
	}
	return bestComm, bestDelta
}

func (g *InMemoryGraph) countCommunities(community []int) int {
	seen := make(map[int]struct{})
	for _, c := range community {
		seen[c] = struct{}{}
	}
	return len(seen)
}
