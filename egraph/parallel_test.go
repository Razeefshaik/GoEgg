package egraph

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

func buildStressGraph() *EGraph {
	g := NewEGraph()
	vars := []string{"a", "b", "c", "d"}
	var terms []*Term
	for _, v := range vars {
		terms = append(terms,
			Node("+", Leaf(v), Leaf("0")),
			Node("*", Leaf(v), Leaf("1")),
			Node("*", Leaf("0"), Leaf(v)),
			Node("+", Node("*", Leaf(v), Leaf("1")), Leaf("0")),
		)
	}

	for i := 0; i < len(vars); i++ {
		for j := 0; j < len(vars); j++ {
			if i == j {
				continue
			}
			terms = append(terms, Node("+", Leaf(vars[i]), Node("*", Leaf(vars[j]), Leaf("1"))))
		}
	}
	for _, t := range terms {
		g.AddTerm(t)
	}
	g.Rebuild()
	return g
}

func stressRules() []*RewriteRule {
	return []*RewriteRule{
		Rule("mul-one", PNode("*", PVar("a"), PNode("1")), PVar("a")),
		Rule("mul-zero", PNode("*", PNode("0"), PVar("a")), PNode("0")),
		Rule("add-zero-l", PNode("+", PNode("0"), PVar("a")), PVar("a")),
		Rule("add-zero-r", PNode("+", PVar("a"), PNode("0")), PVar("a")),
		Rule("comm-add", PNode("+", PVar("a"), PVar("b")), PNode("+", PVar("b"), PVar("a"))),
	}
}

func substSig(s Subst) string {
	keys := make([]string, 0, len(s))
	for k := range s {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%d;", k, s[k])
	}
	return b.String()
}

func matchSignatures(g *EGraph, matches []Match) map[string]bool {
	out := make(map[string]bool, len(matches))
	for _, m := range matches {
		sig := fmt.Sprintf("%s|%d|%s", m.Rule.Name, g.Find(m.Class), substSig(m.Sub))
		out[sig] = true
	}
	return out
}

func TestParallelSearchAllMatchesSequential(t *testing.T) {
	g := buildStressGraph()
	rules := stressRules()

	seq := matchSignatures(g, g.SearchAll(rules))
	par := matchSignatures(g, g.ParallelSearchAll(rules, 4))

	if len(seq) != len(par) {
		t.Fatalf("match count differs: sequential=%d parallel=%d", len(seq), len(par))
	}
	for sig := range seq {
		if !par[sig] {
			t.Fatalf("parallel search missed match found by sequential search: %s", sig)
		}
	}
	for sig := range par {
		if !seq[sig] {
			t.Fatalf("parallel search found a match sequential search did not: %s", sig)
		}
	}
}

func samePartition(t *testing.T, a, b *EGraph, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			ai, aj := a.Find(Id(i)), a.Find(Id(j))
			bi, bj := b.Find(Id(i)), b.Find(Id(j))
			if (ai == aj) != (bi == bj) {
				t.Fatalf("partition mismatch for original ids %d,%d: sequential-equal=%v parallel-equal=%v", i, j, ai == aj, bi == bj)
			}
		}
	}
}

func TestParallelRebuildMatchesSequential(t *testing.T) {
	seqG := buildStressGraph()
	parG := buildStressGraph()
	n := seqG.uf.Len()
	if parG.uf.Len() != n {
		t.Fatalf("test setup broken: graphs allocated a different number of ids (%d vs %d)", n, parG.uf.Len())
	}

	rules := stressRules()
	for round := 0; round < 6; round++ {
		sm := seqG.SearchAll(rules)
		pm := parG.SearchAll(rules)
		seqG.ApplyAll(sm)
		parG.ApplyAll(pm)
		seqG.Rebuild()
		parG.ParallelRebuild(4)
	}

	samePartition(t, seqG, parG, n)

	rootSeq := seqG.AddTerm(Leaf("a"))
	rootPar := parG.AddTerm(Leaf("a"))
	_, cSeq := seqG.Extract(rootSeq, AstSizeCost)
	_, cPar := parG.Extract(rootPar, AstSizeCost)
	if cSeq != cPar {
		t.Fatalf("extraction cost of the same class differs: sequential=%v parallel=%v", cSeq, cPar)
	}
}

func TestParallelExtractMatchesSequential(t *testing.T) {
	g := buildStressGraph()
	g.Saturate(stressRules(), 8)

	for _, id := range g.ClassIds() {
		_, seqCost := g.Extract(id, AstSizeCost)
		_, parCost := g.ParallelExtract(id, AstSizeCost, 4)
		if seqCost != parCost {
			t.Fatalf("class %d: sequential cost %v != parallel cost %v", id, seqCost, parCost)
		}
	}
}

func TestParallelSaturateFullEndToEnd(t *testing.T) {
	g := buildStressGraph()
	g.ParallelSaturateFull(stressRules(), 8, 4)

	root := g.AddTerm(Node("+", Leaf("a"), Leaf("0")))
	aClass := g.AddTerm(Leaf("a"))
	if g.Find(root) != g.Find(aClass) {
		t.Fatalf("expected (a+0) == a after ParallelSaturateFull")
	}
	term, cost := g.ParallelExtract(root, AstSizeCost, 4)
	if term.String() != "a" || cost != 1 {
		t.Fatalf("expected extraction of a (cost 1), got %s (cost %v)", term, cost)
	}
}
