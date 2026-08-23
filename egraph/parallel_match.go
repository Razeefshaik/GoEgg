package egraph

import (
	"runtime"
	"sync"
)

// findRO returns the current root of id WITHOUT mutating the
// union-find (no path compression). It is safe to call concurrently
// from many goroutines, as long as nothing calls Union or the
// (compressing) Find concurrently with it. That is exactly the
// discipline this whole package follows: a rewriting round always
// searches (read-only) to completion, THEN applies and rebuilds
// (mutating, single-threaded-with-respect-to-structure); the two
// phases never overlap on the same e-graph.
func (u *UnionFind) findRO(id Id) Id {
	for u.parent[id] != id {
		id = u.parent[id]
	}
	return id
}

func (g *EGraph) findRO(id Id) Id { return g.uf.findRO(id) }

// matchPatternRO and matchChildrenRO mirror matchPattern and
// matchChildren in match.go exactly, but call findRO instead of Find
// so that many goroutines can run them concurrently without racing
// on union-find path compression. They are kept as separate
// (duplicated) functions rather than parameterizing matchPattern over
// a "find" callback, so the hot sequential path pays no indirection
// cost and the concurrency contract stays easy to audit in one place.
func (g *EGraph) matchPatternRO(pattern Pattern, id Id, sub Subst, out []Subst) []Subst {
	id = g.findRO(id)
	if pattern.IsVar() {
		name := pattern.Name()
		if existing, ok := sub[name]; ok {
			if existing != id {
				return out
			}
			return append(out, sub.clone())
		}
		next := sub.clone()
		next[name] = id
		return append(out, next)
	}
	class := g.classes[id] // read-only map lookup; safe, no writer during search
	for _, node := range class.Nodes {
		if node.Op != pattern.Op || len(node.Children) != len(pattern.Children) {
			continue
		}
		out = g.matchChildrenRO(pattern.Children, node.Children, sub, out)
	}
	return out
}

func (g *EGraph) matchChildrenRO(pats []Pattern, ids []Id, sub Subst, out []Subst) []Subst {
	if len(pats) == 0 {
		return append(out, sub.clone())
	}
	var head []Subst
	head = g.matchPatternRO(pats[0], ids[0], sub, head)
	for _, s := range head {
		out = g.matchChildrenRO(pats[1:], ids[1:], s, out)
	}
	return out
}

// ParallelSearchAll is the concurrent counterpart to SearchAll: it
// finds every (rule, e-class, substitution) match, spreading the
// (rule, e-class) work items across a pool of workers goroutines
// (workers <= 0 means runtime.GOMAXPROCS(0)).
//
// It is read-only exactly like SearchAll and must not be called
// concurrently with anything that mutates g (Add, Union, Rebuild, or
// ApplyAll) -- only with other read-only calls, or not at all.
func (g *EGraph) ParallelSearchAll(rules []*RewriteRule, workers int) []Match {
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	ids := g.ClassIds()
	if len(ids) == 0 || len(rules) == 0 {
		return nil
	}
	if workers > len(ids) {
		workers = len(ids)
	}

	// Statically split ids into one contiguous chunk per worker,
	// rather than handing out one (rule, id) pair at a time through a
	// channel: for e-graphs where a single match attempt is cheap
	// (the common case), per-item channel synchronization can cost
	// more than the match itself, which would erase the whole benefit
	// of parallelizing. A static split pays goroutine/scheduling
	// overhead once per worker instead of once per work item.
	perWorkerResults := make([][]Match, workers)
	chunk := (len(ids) + workers - 1) / workers

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		lo := w * chunk
		hi := lo + chunk
		if lo >= len(ids) {
			break
		}
		if hi > len(ids) {
			hi = len(ids)
		}
		wg.Add(1)
		go func(w int, myIDs []Id) {
			defer wg.Done()
			var local []Match
			for _, r := range rules {
				for _, id := range myIDs {
					for _, sub := range g.matchPatternRO(r.LHS, id, Subst{}, nil) {
						local = append(local, Match{Rule: r, Class: id, Sub: sub})
					}
				}
			}
			perWorkerResults[w] = local
		}(w, ids[lo:hi])
	}
	wg.Wait()

	var all []Match
	for _, local := range perWorkerResults {
		all = append(all, local...)
	}
	return all
}

// ParallelSaturate is the concurrent counterpart to Saturate: it uses
// ParallelSearchAll for the read-only search phase (the phase with
// the most independent work in a typical rewrite system) and the
// sequential ApplyAll/Rebuild for the phase that mutates the graph.
// Applying matches and rebuilding congruence are still done
// single-threaded here; ParallelRebuild (parallel_rebuild.go) is the
// separate, opt-in way to parallelize the rebuild phase too.
func (g *EGraph) ParallelSaturate(rules []*RewriteRule, maxIters, workers int) int {
	for i := 0; i < maxIters; i++ {
		matches := g.ParallelSearchAll(rules, workers)
		if len(matches) == 0 {
			return i
		}
		changed := g.ApplyAll(matches)
		g.Rebuild()
		if changed == 0 {
			return i + 1
		}
	}
	return maxIters
}
