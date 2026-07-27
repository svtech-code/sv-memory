package graph

import (
	"math/rand"
	"sort"
)

type leidenState struct {
	n          int
	nodeIDs    []string
	nodeIndex  map[string]int
	community  []int
	tot        []float64
	m          float64
	gamma      float64
	rng        *rand.Rand
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
		g.leidenLocalMoving(ls)

		refined := g.leidenRefine(ls)
		merged := g.leidenMerge(ls, refined)

		if !g.leidenAssign(ls, merged) {
			break
		}
	}

	result := make(map[string]int)
	remap := make(map[int]int)
	nextID := 1
	zeroCount := 0
	for i, id := range ls.nodeIDs {
		c := ls.community[i]
		if c == 0 {
			zeroCount++
		}
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
		moved := false
		order := ls.rng.Perm(ls.n)
		for _, idx := range order {
			curComm := ls.community[idx]
			k_v := g.degreeByIdx(idx, ls)

			neighborComms := make(map[int]float64)
			for _, edge := range g.EdgesBySource[ls.nodeIDs[idx]] {
				if tIdx, ok := ls.nodeIndex[edge.TargetID]; ok {
					neighborComms[ls.community[tIdx]] += 1.0
				}
			}
			for _, edge := range g.EdgesByTarget[ls.nodeIDs[idx]] {
				if sIdx, ok := ls.nodeIndex[edge.SourceID]; ok {
					neighborComms[ls.community[sIdx]] += 1.0
				}
			}

			bestComm := curComm
			bestDelta := 0.0

			removeDelta := -neighborComms[curComm]/ls.m +
				ls.gamma*k_v*(ls.tot[curComm]-k_v)/(2.0*ls.m*ls.m)

			for nc, w := range neighborComms {
				if nc == curComm {
					continue
				}
				addDelta := w/ls.m - ls.gamma*k_v*ls.tot[nc]/(2.0*ls.m*ls.m)
				totalDelta := removeDelta + addDelta
				if totalDelta > bestDelta+1e-12 {
					bestDelta = totalDelta
					bestComm = nc
				}
			}

			if bestComm != curComm {
				ls.tot[curComm] -= k_v
				ls.tot[bestComm] += k_v
				ls.community[idx] = bestComm
				moved = true
			}
		}
		if !moved {
			break
		}
	}
}

func (g *InMemoryGraph) leidenRefine(ls *leidenState) []int {
	refined := make([]int, ls.n)
	rTot := make([]float64, ls.n)
	for i := range refined {
		refined[i] = i
		rTot[i] = g.degreeByIdx(i, ls)
	}

	for localIter := 0; localIter < 10; localIter++ {
		moved := false
		order := ls.rng.Perm(ls.n)
		for _, idx := range order {
			curComm := ls.community[idx]
			curRef := refined[idx]
			k_v := g.degreeByIdx(idx, ls)

			neighborWeights := make(map[int]float64)
			for _, edge := range g.EdgesBySource[ls.nodeIDs[idx]] {
				if tIdx, ok := ls.nodeIndex[edge.TargetID]; ok {
					if ls.community[tIdx] == curComm {
						neighborWeights[refined[tIdx]] += 1.0
					}
				}
			}
			for _, edge := range g.EdgesByTarget[ls.nodeIDs[idx]] {
				if sIdx, ok := ls.nodeIndex[edge.SourceID]; ok {
					if ls.community[sIdx] == curComm {
						neighborWeights[refined[sIdx]] += 1.0
					}
				}
			}

			bestRef := curRef
			bestDelta := 0.0

			removeDelta := -neighborWeights[curRef]/ls.m +
				ls.gamma*k_v*(rTot[curRef]-k_v)/(2.0*ls.m*ls.m)

			for nr, w := range neighborWeights {
				if nr == curRef {
					continue
				}
				addDelta := w/ls.m - ls.gamma*k_v*rTot[nr]/(2.0*ls.m*ls.m)
				totalDelta := removeDelta + addDelta
				if totalDelta > bestDelta+1e-12 {
					bestDelta = totalDelta
					bestRef = nr
				}
			}

			if bestRef != curRef {
				rTot[curRef] -= k_v
				rTot[bestRef] += k_v
				refined[idx] = bestRef
				moved = true
			}
		}
		if !moved {
			break
		}
	}

	refMap := make(map[int]int)
	nextID := 0
	for i := range refined {
		r := refined[i]
		if _, ok := refMap[r]; !ok {
			refMap[r] = nextID
			nextID++
		}
		refined[i] = refMap[r]
	}

	return refined
}

func (g *InMemoryGraph) leidenMerge(ls *leidenState, refined []int) []int {
	nodeToRef := make(map[int]struct{})
	for _, r := range refined {
		nodeToRef[r] = struct{}{}
	}
	if len(nodeToRef) >= ls.n {
		return nil
	}

	maxComm := 0
	for _, c := range ls.community {
		if c > maxComm {
			maxComm = c
		}
	}

	merged := make([]int, ls.n)
	for i := range merged {
		c := ls.community[i]
		r := refined[i]
		merged[i] = c*(maxComm+1) + r
	}

	mMap := make(map[int]int)
	nextID := 0
	for i := range merged {
		m := merged[i]
		if _, ok := mMap[m]; !ok {
			mMap[m] = nextID
			nextID++
		}
		merged[i] = mMap[m]
	}

	return merged
}

func (g *InMemoryGraph) leidenAssign(ls *leidenState, merged []int) bool {
	if merged == nil {
		return false
	}

	oldCount := g.countCommunities(ls.community)
	newCount := g.countCommunities(merged)
	if newCount >= oldCount {
		return false
	}

	newTot := make([]float64, newCount)
	for i, id := range ls.nodeIDs {
		newTot[merged[i]] += float64(g.degree(id))
	}

	ls.community = merged
	ls.tot = newTot
	return true
}

func (g *InMemoryGraph) countCommunities(community []int) int {
	seen := make(map[int]struct{})
	for _, c := range community {
		seen[c] = struct{}{}
	}
	return len(seen)
}
