package egraph

import (
	"math"
	"runtime"
	"sync"
	"sync/atomic"
)

// ParallelExtract is the concurrent counterpart to Extract.
//
// Extract's relaxation is Gauss-Seidel style: every e-class shares
// one mutable cost map, so a class can see other classes' costs as
// updated earlier in the SAME round. ParallelExtract is Jacobi style
// instead: every round reads only the previous round's finished
// snapshot and writes into a separate buffer, so every e-class's
// update within a round is independent of every other one's.
//
// Costs and chosen nodes live in plain slices indexed directly by Id
// rather than maps, so two goroutines updating different e-classes'
// slots touch disjoint memory and need no locking at all — the
// WaitGroup at the end of each round is the only synchronization.
// (Jacobi relaxation needs at most as many rounds as Gauss-Seidel to
// converge on an acyclic-cost problem like this one, just with more
// total work per round since nothing propagates within a round; that
// trade is exactly what buys the lock-free parallelism.)
func (g *EGraph) ParallelExtract(root Id, cost CostFn, workers int) (*Term, float64) {
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	ids := g.ClassIds()
	size := g.uf.Len()
	n := workers
	if n > len(ids) {
		n = len(ids)
	}
	if n < 1 {
		n = 1
	}
	chunk := (len(ids) + n - 1) / n

	// Two fixed pairs of buffers, reused every round by swapping which
	// is "current" and which is "next" -- avoids reallocating (and
	// zeroing/copying into) four slices of size `size` on every round,
	// which for a large e-graph run over many rounds can otherwise
	// dwarf the actual per-e-class work being parallelized.
	costA := make([]float64, size)
	costB := make([]float64, size)
	bestA := make([]ENode, size)
	bestB := make([]ENode, size)
	haveA := make([]bool, size)
	haveB := make([]bool, size)
	for i := range costA {
		costA[i] = math.Inf(1)
	}
	cur, next := costA, costB
	best, nextBest := bestA, bestB
	haveBest, nextHave := haveA, haveB

	maxRounds := len(ids) + 1
	for round := 0; round < maxRounds; round++ {
		copy(next, cur)
		copy(nextBest, best)
		copy(nextHave, haveBest)

		var anyChanged int32
		var wg sync.WaitGroup
		for w := 0; w < n; w++ {
			lo := w * chunk
			hi := lo + chunk
			if lo >= len(ids) {
				break
			}
			if hi > len(ids) {
				hi = len(ids)
			}
			wg.Add(1)
			go func(myIDs []Id) {
				defer wg.Done()
				for _, id := range myIDs {
					class := g.classes[id]
					bestCost, bestNode, ok := bestOfSlice(g, cur, class, cost)
					if ok && bestCost < cur[id] {
						// Safe without a lock: each id belongs to
						// exactly one goroutine's chunk, and every
						// write here lands at index id, which no
						// other goroutine this round ever touches.
						next[id] = bestCost
						nextBest[id] = bestNode
						nextHave[id] = true
						atomic.AddInt32(&anyChanged, 1)
					}
				}
			}(ids[lo:hi])
		}
		wg.Wait()

		cur, next = next, cur
		best, nextBest = nextBest, best
		haveBest, nextHave = nextHave, haveBest
		if anyChanged == 0 {
			break
		}
	}

	r := g.findRO(root)
	if !haveBest[r] {
		return nil, math.Inf(1)
	}
	return buildTermFromSlice(g, best, haveBest, r), cur[r]
}

// bestOfSlice is bestOf's twin, reading a []float64 snapshot indexed
// by canonical Id instead of a map, and canonicalizing children with
// findRO — the same stale-child-reference fix bestOf needs (see its
// comment in extract.go), done through the read-only accessor since
// this runs concurrently with other e-classes' bestOfSlice calls.
func bestOfSlice(g *EGraph, costSnapshot []float64, class *EClass, cost CostFn) (float64, ENode, bool) {
	bestCost := math.Inf(1)
	var bestNode ENode
	found := false
	for _, node := range class.Nodes {
		childCosts := make([]float64, len(node.Children))
		ready := true
		for i, c := range node.Children {
			cc := costSnapshot[g.findRO(c)]
			if math.IsInf(cc, 1) {
				ready = false
				break
			}
			childCosts[i] = cc
		}
		if !ready {
			continue
		}
		cst := cost(node, childCosts)
		if cst < bestCost {
			bestCost = cst
			bestNode = node
			found = true
		}
	}
	return bestCost, bestNode, found
}

func buildTermFromSlice(g *EGraph, best []ENode, haveBest []bool, id Id) *Term {
	id = g.findRO(id)
	if !haveBest[id] {
		return nil
	}
	node := best[id]
	children := make([]*Term, len(node.Children))
	for i, c := range node.Children {
		children[i] = buildTermFromSlice(g, best, haveBest, g.findRO(c))
	}
	return &Term{Op: node.Op, Children: children}
}
