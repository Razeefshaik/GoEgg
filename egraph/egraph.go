// Package egraph implements an e-graph (equality graph) with
// equality-saturation-style rewriting, following the design described
// in Willsey et al., "egg: Fast and Extensible Equality Saturation"
// (POPL 2021): a union-find over e-classes, a hashcons memo table
// keyed on canonical e-nodes, and a deferred "rebuild" that restores
// the hashcons and congruence-closure invariants in batches.
//
// This file holds the sequential implementation, which is the
// correctness baseline. The parallel variants (parallel_match.go,
// parallel_rebuild.go, parallel_extract.go) are checked against it.
package egraph

import (
	"strconv"
	"strings"
)

// ENode is a function symbol (Op) applied to a list of e-class ids.
// An ENode is canonical when every id in Children is itself a
// canonical e-class id (Find(id) == id).
type ENode struct {
	Op       string
	Children []Id
}

// key returns a string suitable for use as a hashcons map key. Two
// ENodes with the same Op and the same Children (in order) produce
// the same key.
func (n ENode) key() string {
	var b strings.Builder
	b.WriteString(n.Op)
	for _, c := range n.Children {
		b.WriteByte(0)
		b.WriteString(strconv.FormatInt(int64(c), 10))
	}
	return b.String()
}

// parentEdge records that Node, as it looked at insertion or last
// repair time, has the owning e-class as one of its children, and
// itself lives in e-class Class.
type parentEdge struct {
	Node  ENode
	Class Id
}

// EClass is an equivalence class of e-nodes, plus the set of e-nodes
// elsewhere in the graph that reference it as a child (its parents).
// The parent list is what lets repair() find newly-congruent nodes
// after a union.
type EClass struct {
	Id      Id
	Nodes   []ENode
	Parents []parentEdge
}

// EGraph is a sequential e-graph: a set of e-classes closed under
// congruence, plus a memo table (hashcons) from canonical e-nodes to
// the e-class id containing them.
type EGraph struct {
	uf       *UnionFind
	classes  map[Id]*EClass
	hashcons map[string]Id
	worklist []Id
}

// NewEGraph returns an empty e-graph.
func NewEGraph() *EGraph {
	return &EGraph{
		uf:       NewUnionFind(),
		classes:  make(map[Id]*EClass),
		hashcons: make(map[string]Id),
	}
}

// Find returns the current canonical e-class id for id.
func (g *EGraph) Find(id Id) Id { return g.uf.Find(id) }

// canonicalize returns a copy of n with every child replaced by its
// current canonical id.
func (g *EGraph) canonicalize(n ENode) ENode {
	out := ENode{Op: n.Op, Children: make([]Id, len(n.Children))}
	for i, c := range n.Children {
		out.Children[i] = g.Find(c)
	}
	return out
}

// Add inserts node (canonicalizing its children first) and returns
// the id of the e-class that contains it, reusing an existing
// e-class if an equivalent (congruent) node is already present.
func (g *EGraph) Add(node ENode) Id {
	node = g.canonicalize(node)
	key := node.key()
	if id, ok := g.hashcons[key]; ok {
		return g.Find(id)
	}
	id := g.uf.MakeSet()
	g.classes[id] = &EClass{Id: id, Nodes: []ENode{node}}
	g.hashcons[key] = id
	for _, c := range node.Children {
		cc := g.classes[g.Find(c)]
		cc.Parents = append(cc.Parents, parentEdge{Node: node, Class: id})
	}
	return id
}

// Union merges the e-classes containing a and b, if they are not
// already the same e-class, and schedules the result for congruence
// repair via Rebuild. Returns true if a merge happened.
func (g *EGraph) Union(a, b Id) bool {
	a, b = g.Find(a), g.Find(b)
	if a == b {
		return false
	}
	newRoot := g.uf.Union(a, b)
	loser := a
	if newRoot == a {
		loser = b
	}
	winner := g.classes[newRoot]
	lost := g.classes[loser]
	winner.Nodes = append(winner.Nodes, lost.Nodes...)
	winner.Parents = append(winner.Parents, lost.Parents...)
	delete(g.classes, loser)
	g.worklist = append(g.worklist, newRoot)
	return true
}

// Rebuild restores the hashcons and congruence-closure invariants
// after a batch of Union calls. Matching and extraction only give
// correct answers once Rebuild has been called with an empty
// resulting worklist; it is a cheap no-op when nothing is dirty.
func (g *EGraph) Rebuild() {
	for len(g.worklist) > 0 {
		todo := g.dedupCanon(g.worklist)
		g.worklist = nil
		for _, id := range todo {
			g.repair(id)
		}
	}
}

// dedupCanon canonicalizes every id in ids via Find and removes
// duplicates, preserving first-seen order.
func (g *EGraph) dedupCanon(ids []Id) []Id {
	seen := make(map[Id]bool, len(ids))
	out := make([]Id, 0, len(ids))
	for _, id := range ids {
		c := g.Find(id)
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

// repair restores the hashcons and congruence invariants for the
// single e-class id, discovering (and scheduling via Union, which
// re-populates the worklist) any further merges forced by
// newly-congruent parent e-nodes.
func (g *EGraph) repair(id Id) {
	// Re-resolve to the current canonical id: an earlier repair()
	// call in this same Rebuild batch may have already merged id away
	// (a congruent-parent union cascading onto id itself). When that
	// happens id's original Parents were already transferred onto the
	// class it merged into (EGraph.Union appends them), so there is
	// nothing left to do here — either that class is elsewhere in
	// this batch and will pick up the transferred parents when it
	// runs, or it was already processed with a stale snapshot and
	// will be repaired again next round, since Union always
	// re-dirties its result.
	id = g.Find(id)
	class, ok := g.classes[id]
	if !ok {
		return
	}

	// Step 1: fix the hashcons. Existing entries for this class's
	// parents may be keyed on now-stale (non-canonical) children;
	// remove them and reinsert under the canonical key, pointing at
	// the canonical owning class.
	for _, p := range class.Parents {
		delete(g.hashcons, p.Node.key())
		canon := g.canonicalize(p.Node)
		g.hashcons[canon.key()] = g.Find(p.Class)
	}

	// Step 2: re-canonicalize and dedup the parent list. Two parents
	// that are now congruent (same canonical form) mean their owning
	// e-classes must also be merged ("upward merging").
	newParents := make(map[string]parentEdge, len(class.Parents))
	for _, p := range class.Parents {
		canon := g.canonicalize(p.Node)
		key := canon.key()
		if existing, ok := newParents[key]; ok {
			g.Union(existing.Class, p.Class)
		}
		newParents[key] = parentEdge{Node: canon, Class: g.Find(p.Class)}
	}
	parents := make([]parentEdge, 0, len(newParents))
	for _, p := range newParents {
		parents = append(parents, p)
	}
	// The Unions above may have merged id's class into something
	// else's bookkeeping only indirectly (id itself is untouched by
	// them), but always look it up by canonical id defensively.
	g.classes[g.Find(id)].Parents = parents
}

// Class returns the e-class currently identified by id (after
// canonicalizing id).
func (g *EGraph) Class(id Id) *EClass {
	return g.classes[g.Find(id)]
}

// ClassIds returns the ids of all live e-classes, in no particular
// order.
func (g *EGraph) ClassIds() []Id {
	ids := make([]Id, 0, len(g.classes))
	for id := range g.classes {
		ids = append(ids, id)
	}
	return ids
}

// NumClasses returns the number of live e-classes.
func (g *EGraph) NumClasses() int {
	return len(g.classes)
}

// Dirty reports whether Rebuild still has work to do.
func (g *EGraph) Dirty() bool {
	return len(g.worklist) > 0
}
