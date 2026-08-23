package egraph

import "math"

// CostFn assigns a cost to using a particular e-node, given the
// already-known best costs of its children's e-classes.
type CostFn func(node ENode, childCosts []float64) float64

// AstSizeCost is a simple, common cost function: every node costs 1,
// so the cheapest term is the smallest one.
func AstSizeCost(node ENode, childCosts []float64) float64 {
	total := 1.0
	for _, c := range childCosts {
		total += c
	}
	return total
}

// Extraction holds, per (canonical) e-class id, the cheapest e-node
// found and its cost.
type Extraction struct {
	cost map[Id]float64
	best map[Id]ENode
}

// Extract runs a bottom-up cost analysis to a fixed point and returns
// the cheapest concrete term equivalent to root, plus its cost.
//
// It uses iterative relaxation (Bellman-Ford style) rather than naive
// memoized recursion, because e-graphs produced by rewriting can
// contain cyclic e-node references (e.g. rules like x*1=x or x+0=x
// make a class reachable from itself). Relaxation converges correctly
// regardless of such cycles, and — this is the point, for
// parallel_extract.go — each round's work decomposes into
// independent per-class updates.
func (g *EGraph) Extract(root Id, cost CostFn) (*Term, float64) {
	ex := g.analyze(cost, len(g.classes)+1)
	r := g.Find(root)
	return ex.buildTerm(g, r), ex.cost[r]
}

// analyze computes, for every live e-class, its cheapest node and
// that node's cost, iterating in synchronous rounds until nothing
// improves. maxRounds bounds pathological inputs (NumClasses()+1 is
// always enough: no shortest-path-style relaxation needs more rounds
// than there are nodes).
func (g *EGraph) analyze(cost CostFn, maxRounds int) *Extraction {
	ex := &Extraction{cost: make(map[Id]float64, len(g.classes)), best: make(map[Id]ENode, len(g.classes))}
	for id := range g.classes {
		ex.cost[id] = math.Inf(1)
	}
	for round := 0; round < maxRounds; round++ {
		changed := false
		for id, class := range g.classes {
			bestCost, bestNode, ok := bestOf(g, ex, class, cost)
			if ok && bestCost < ex.cost[id] {
				ex.cost[id] = bestCost
				ex.best[id] = bestNode
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return ex
}

// bestOf finds the cheapest node in class whose children all
// currently have a finite known cost in ex. Children are canonicalized
// via g.Find before the cost lookup: a node's stored Children can be
// stale (pointing at an e-class id that has since been merged away —
// EGraph never rewrites already-stored e-nodes in place, exactly like
// egg), and ex.cost is only ever keyed by canonical ids, so skipping
// this would silently and permanently starve any node whose child
// happens to have been on the losing side of a later union.
//
// bestOf only reads ex.cost (never writes), so given a stable
// snapshot it is safe to call concurrently — this is exactly what
// parallel_extract.go does, one call per e-class per round.
func bestOf(g *EGraph, ex *Extraction, class *EClass, cost CostFn) (float64, ENode, bool) {
	bestCost := math.Inf(1)
	var bestNode ENode
	found := false
	for _, node := range class.Nodes {
		childCosts := make([]float64, len(node.Children))
		ready := true
		for i, c := range node.Children {
			cc, ok := ex.cost[g.Find(c)]
			if !ok || math.IsInf(cc, 1) {
				ready = false
				break
			}
			childCosts[i] = cc
		}
		if !ready {
			continue
		}
		c := cost(node, childCosts)
		if c < bestCost {
			bestCost = c
			bestNode = node
			found = true
		}
	}
	return bestCost, bestNode, found
}

// buildTerm reconstructs the concrete Term for id from the analysis.
func (ex *Extraction) buildTerm(g *EGraph, id Id) *Term {
	node := ex.best[id]
	children := make([]*Term, len(node.Children))
	for i, c := range node.Children {
		children[i] = ex.buildTerm(g, g.Find(c))
	}
	return &Term{Op: node.Op, Children: children}
}
