package egraph

import (
	"runtime"
	"sync"
)

type repairPlan struct {
	classID     Id
	deleteKeys  []string
	insertKeys  []string
	insertClass []Id
	newParents  []parentEdge
	unions      [][2]Id
}

func (g *EGraph) planRepair(id Id) repairPlan {
	class := g.classes[id]
	plan := repairPlan{classID: id}

	for _, p := range class.Parents {
		plan.deleteKeys = append(plan.deleteKeys, p.Node.key())
		canon := g.canonicalizeRO(p.Node)
		plan.insertKeys = append(plan.insertKeys, canon.key())
		plan.insertClass = append(plan.insertClass, g.findRO(p.Class))
	}

	seen := make(map[string]parentEdge, len(class.Parents))
	for _, p := range class.Parents {
		canon := g.canonicalizeRO(p.Node)
		key := canon.key()
		if existing, ok := seen[key]; ok {
			plan.unions = append(plan.unions, [2]Id{existing.Class, p.Class})
			continue
		}
		seen[key] = parentEdge{Node: canon, Class: g.findRO(p.Class)}
	}
	plan.newParents = make([]parentEdge, 0, len(seen))
	for _, p := range seen {
		plan.newParents = append(plan.newParents, p)
	}
	return plan
}

func (g *EGraph) canonicalizeRO(n ENode) ENode {
	out := ENode{Op: n.Op, Children: make([]Id, len(n.Children))}
	for i, c := range n.Children {
		out.Children[i] = g.findRO(c)
	}
	return out
}

// ParallelRebuild is the concurrent counterpart to Rebuild. Within
// each round it is a bulk-synchronous-parallel (BSP) superstep:
//
//   - Phase A (parallel): every dirty e-class's repair is computed
//     independently by a worker pool. This phase only reads the
//     e-graph (via findRO and plain map reads), so it is race-free by
//     construction -- nothing mutates g until Phase A's WaitGroup
//     returns, which is exactly the barrier that makes it safe.
//   - Phase B (sequential): the round's plans are applied in two
//     passes -- first every class's own Parents list is installed
//     (each todo id is still, at this point, its own untouched map
//     key, since none of this round's unions have run yet), then
//     every discovered congruent-parent union is performed. Splitting
//     these two passes matters: performing a union before every
//     plan's Parents assignment would let an assignment clobber
//     Parents another plan's union had just merged in.
//
// Any cascading merges this round's repairs force (upward congruence)
// land back on g's worklist exactly as Rebuild would, so the two
// converge to the identical fixed point -- ParallelRebuild just
// batches the work by round instead of by individual e-class.
//
// workers <= 0 means runtime.GOMAXPROCS(0). Must not be called
// concurrently with anything else that touches g.
func (g *EGraph) ParallelRebuild(workers int) {
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	for len(g.worklist) > 0 {
		todo := g.dedupCanon(g.worklist)
		g.worklist = nil

		plans := make([]repairPlan, len(todo))
		n := workers
		if n > len(todo) {
			n = len(todo)
		}
		if n < 1 {
			n = 1
		}

		chunk := (len(todo) + n - 1) / n
		var wg sync.WaitGroup
		for w := 0; w < n; w++ {
			lo := w * chunk
			hi := lo + chunk
			if lo >= len(todo) {
				break
			}
			if hi > len(todo) {
				hi = len(todo)
			}
			wg.Add(1)
			go func(lo, hi int) {
				defer wg.Done()
				for i := lo; i < hi; i++ {
					plans[i] = g.planRepair(todo[i])
				}
			}(lo, hi)
		}
		wg.Wait()

		for _, plan := range plans {
			for _, k := range plan.deleteKeys {
				delete(g.hashcons, k)
			}
			for i, k := range plan.insertKeys {
				g.hashcons[k] = plan.insertClass[i]
			}
			if c, ok := g.classes[plan.classID]; ok {
				c.Parents = plan.newParents
			}
		}

		for _, plan := range plans {
			for _, u := range plan.unions {
				g.Union(u[0], u[1])
			}
		}
	}
}

// ParallelSaturateFull uses ParallelSearchAll AND ParallelRebuild, so
// every phase of the equality-saturation loop that can run
// concurrently does.
func (g *EGraph) ParallelSaturateFull(rules []*RewriteRule, maxIters, workers int) int {
	for i := 0; i < maxIters; i++ {
		matches := g.ParallelSearchAll(rules, workers)
		if len(matches) == 0 {
			return i
		}
		changed := g.ApplyAll(matches)
		g.ParallelRebuild(workers)
		if changed == 0 {
			return i + 1
		}
	}
	return maxIters
}
