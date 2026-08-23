package egraph

import (
	"fmt"
	"math/rand"
	"testing"
)

func randTerm(rnd *rand.Rand, vars []string, depth int) *Term {
	if depth <= 0 || rnd.Intn(3) == 0 {
		if rnd.Intn(2) == 0 {
			return Leaf(vars[rnd.Intn(len(vars))])
		}
		return Leaf(fmt.Sprintf("%d", rnd.Intn(2)))
	}
	op := "+"
	if rnd.Intn(2) == 0 {
		op = "*"
	}
	return Node(op, randTerm(rnd, vars, depth-1), randTerm(rnd, vars, depth-1))
}

func buildBenchGraph(numVars, numExprs, depth int) *EGraph {
	g := NewEGraph()
	rnd := rand.New(rand.NewSource(1))
	vars := make([]string, numVars)
	for i := range vars {
		vars[i] = fmt.Sprintf("v%d", i)
	}
	for i := 0; i < numExprs; i++ {
		g.AddTerm(randTerm(rnd, vars, depth))
	}
	g.Rebuild()
	return g
}

const (
	benchVars  = 60
	benchExprs = 4000
	benchDepth = 6
)

func BenchmarkSearchAllSequential(b *testing.B) {
	g := buildBenchGraph(benchVars, benchExprs, benchDepth)
	rules := stressRules()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = g.SearchAll(rules)
	}
}

func BenchmarkSearchAllParallel(b *testing.B) {
	g := buildBenchGraph(benchVars, benchExprs, benchDepth)
	rules := stressRules()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = g.ParallelSearchAll(rules, 0)
	}
}

func BenchmarkRebuildSequential(b *testing.B) {
	rules := stressRules()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		g := buildBenchGraph(benchVars, benchExprs, benchDepth)
		matches := g.SearchAll(rules)
		g.ApplyAll(matches)
		b.StartTimer()

		g.Rebuild()
	}
}

func BenchmarkRebuildParallel(b *testing.B) {
	rules := stressRules()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		g := buildBenchGraph(benchVars, benchExprs, benchDepth)
		matches := g.SearchAll(rules)
		g.ApplyAll(matches)
		b.StartTimer()

		g.ParallelRebuild(0)
	}
}

func BenchmarkExtractSequential(b *testing.B) {
	g := buildBenchGraph(benchVars, benchExprs, benchDepth)
	g.Saturate(stressRules(), 6)
	root := g.ClassIds()[0]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.Extract(root, AstSizeCost)
	}
}

func BenchmarkExtractParallel(b *testing.B) {
	g := buildBenchGraph(benchVars, benchExprs, benchDepth)
	g.Saturate(stressRules(), 6)
	root := g.ClassIds()[0]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.ParallelExtract(root, AstSizeCost, 0)
	}
}

func BenchmarkSaturateFullSequential(b *testing.B) {
	rules := stressRules()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		g := buildBenchGraph(benchVars, benchExprs, benchDepth)
		b.StartTimer()
		g.Saturate(rules, 6)
	}
}

func BenchmarkSaturateFullParallel(b *testing.B) {
	rules := stressRules()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		g := buildBenchGraph(benchVars, benchExprs, benchDepth)
		b.StartTimer()
		g.ParallelSaturateFull(rules, 6, 0)
	}
}
