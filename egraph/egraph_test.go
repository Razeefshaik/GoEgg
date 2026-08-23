package egraph

import "testing"

func TestUnionFindBasics(t *testing.T) {
	u := NewUnionFind()
	a, b, c := u.MakeSet(), u.MakeSet(), u.MakeSet()
	if u.Find(a) != a || u.Find(b) != b || u.Find(c) != c {
		t.Fatalf("fresh sets should be their own representative")
	}
	u.Union(a, b)
	if u.Find(a) != u.Find(b) {
		t.Fatalf("a and b should be unioned")
	}
	if u.Find(a) == u.Find(c) {
		t.Fatalf("c should still be separate")
	}
	u.Union(b, c)
	if u.Find(a) != u.Find(c) {
		t.Fatalf("a and c should now be transitively unioned")
	}
}

func TestAddCongruence(t *testing.T) {
	g := NewEGraph()
	x := g.Add(ENode{Op: "x"})
	y := g.Add(ENode{Op: "y"})
	f1 := g.Add(ENode{Op: "f", Children: []Id{x}})

	f1again := g.Add(ENode{Op: "f", Children: []Id{x}})
	if f1 != f1again {
		t.Fatalf("adding an identical e-node should reuse the e-class")
	}
	f2 := g.Add(ENode{Op: "f", Children: []Id{y}})
	if f1 == f2 {
		t.Fatalf("f(x) and f(y) should NOT be congruent before x=y")
	}

	g.Union(x, y)
	g.Rebuild()
	if g.Find(f1) != g.Find(f2) {
		t.Fatalf("f(x) and f(y) should be congruent once x=y and the graph is rebuilt")
	}
}

func TestRebuildIsIdempotent(t *testing.T) {
	g := NewEGraph()
	x := g.Add(ENode{Op: "x"})
	g.Rebuild()
	if g.Dirty() {
		t.Fatalf("rebuild on a clean graph should stay clean")
	}
	_ = x
}

func arith() (*EGraph, *Term) {
	g := NewEGraph()

	term := Node("+",
		Node("*", Leaf("x"), Leaf("1")),
		Node("*", Leaf("0"), Leaf("y")),
	)
	return g, term
}

func arithRules() []*RewriteRule {
	return []*RewriteRule{
		Rule("mul-one", PNode("*", PVar("a"), PNode("1")), PVar("a")),
		Rule("mul-zero", PNode("*", PNode("0"), PVar("a")), PNode("0")),
		Rule("add-zero-l", PNode("+", PNode("0"), PVar("a")), PVar("a")),
		Rule("add-zero-r", PNode("+", PVar("a"), PNode("0")), PVar("a")),
	}
}

func TestSaturationSimplifiesArithmetic(t *testing.T) {
	g, term := arith()
	root := g.AddTerm(term)
	g.Rebuild()

	g.Saturate(arithRules(), 10)

	xClass := g.AddTerm(Leaf("x"))
	if g.Find(root) != g.Find(xClass) {
		t.Fatalf("(x*1)+(0*y) should be equal to x after saturation, got root=%d xClass=%d", g.Find(root), g.Find(xClass))
	}

	extracted, cost := g.Extract(root, AstSizeCost)
	if extracted.String() != "x" {
		t.Fatalf("expected extraction to find x, got %s (cost %v)", extracted.String(), cost)
	}
	if cost != 1 {
		t.Fatalf("expected cost 1 for the single leaf x, got %v", cost)
	}
}

func TestExtractPicksCheapestEquivalentTerm(t *testing.T) {
	g := NewEGraph()
	x := g.AddTerm(Leaf("x"))

	four := g.AddTerm(Leaf("4"))
	mul := g.Add(ENode{Op: "*", Children: []Id{x, four}})
	sum := g.AddTerm(Node("+", Node("+", Node("+", Leaf("x"), Leaf("x")), Leaf("x")), Leaf("x")))
	g.Union(mul, sum)
	g.Rebuild()

	extracted, _ := g.Extract(mul, AstSizeCost)
	if extracted.String() != "(* x 4)" {
		t.Fatalf("expected the smaller term (* x 4), got %s", extracted.String())
	}
}

// TestExtractHandlesStaleChildReferences guards against a real bug
// found while building this package: EGraph never rewrites an
// already-stored e-node's Children in place after a later union (it
// relies on every consumer calling Find on child ids instead), so
// extraction must canonicalize before looking up a child's cost —
// otherwise a class whose only node references a since-merged-away
// child id would incorrectly appear to have infinite cost forever.
func TestExtractHandlesStaleChildReferences(t *testing.T) {
	g := NewEGraph()
	a := g.AddTerm(Leaf("a"))
	b := g.AddTerm(Leaf("b"))
	f := g.Add(ENode{Op: "f", Children: []Id{a}})

	g.Union(b, a)
	g.Rebuild()
	if g.Find(a) == a {
		t.Fatalf("test setup assumption broken: expected a's class to have been merged away")
	}

	term, cost := g.Extract(f, AstSizeCost)
	if cost != 2 {
		t.Fatalf("expected cost 2 for f(<leaf>), got %v (term %s)", cost, term)
	}
	if term.String() != "(f a)" && term.String() != "(f b)" {
		t.Fatalf("expected (f a) or (f b), got %s", term.String())
	}
}

func TestCyclicEGraphExtractionTerminates(t *testing.T) {

	g := NewEGraph()
	x := g.AddTerm(Leaf("x"))
	one := g.AddTerm(Leaf("1"))
	mul := g.Add(ENode{Op: "*", Children: []Id{x, one}})
	g.Union(x, mul)
	g.Rebuild()

	term, cost := g.Extract(x, AstSizeCost)
	if term.String() != "x" {
		t.Fatalf("expected extraction to still find the leaf x, got %s", term.String())
	}
	if cost != 1 {
		t.Fatalf("expected cost 1, got %v", cost)
	}
}
