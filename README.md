# GoEgg — a parallel e-graph / equality-saturation engine in Go

> This is the **release branch** (`master`): source comments are trimmed to
> exported-symbol doc comments only (what `go doc` / pkg.go.dev show), and
> versions are tagged here. For the fully-commented version with design
> rationale inline, see the `dev` branch; for an interactive, no-build-step
> visualization of the algorithm, see the `showcase` branch.

```
go get github.com/Razeefshaik/GoEgg@v0.1.3
```

`GoEgg` is an [e-graph](https://en.wikipedia.org/wiki/E-graph) (equality
graph) library with equality-saturation-style term rewriting, built the way
[`egg`](https://egraphs-good.github.io/) works in Rust — union-find over
e-classes, a hashcons memo table, and a deferred "rebuild" that restores
congruence closure in batches — except every phase that can run on more
than one core does: matching, rebuild, and extraction all have a
goroutine-parallel implementation alongside a sequential one.

As of when this was built (August 2026), there was no e-graph or
equality-saturation library in the Go ecosystem at all — the community's
own [awesome-egraphs](https://github.com/philzook58/awesome-egraphs) list
has entries for Rust, Julia, and Python, nothing for Go — and no reason to
expect that any of the existing Go union-find or Voronoi packages would
need to solve this particular problem. So this is offered as a first: not
just "a parallel e-graph," but an e-graph in Go, period, built parallel
from the start.

## Installation

```
go get github.com/Razeefshaik/GoEgg@v0.1.3
```

Requires Go 1.24 or later (the module targets `go 1.24.7`). No other
dependencies — the whole library is the standard library plus `sync`.

## Quick start

```go
import "github.com/Razeefshaik/GoEgg/egraph"

// Build a term: (x * 1) + 0
g := egraph.NewEGraph()
root := g.AddTerm(egraph.Node("+",
    egraph.Node("*", egraph.Leaf("x"), egraph.Leaf("1")),
    egraph.Leaf("0"),
))
g.Rebuild()

// Describe rewrites as LHS -> RHS patterns. "?a" is a pattern
// variable; anything else must match an e-node's operator exactly.
rules := []*egraph.RewriteRule{
    egraph.Rule("mul-one", egraph.PNode("*", egraph.PVar("a"), egraph.PNode("1")), egraph.PVar("a")),
    egraph.Rule("add-zero-r", egraph.PNode("+", egraph.PVar("a"), egraph.PNode("0")), egraph.PVar("a")),
}

// Run search -> apply -> rebuild to a fixed point, using every phase's
// parallel implementation (0 workers = runtime.GOMAXPROCS(0)).
g.ParallelSaturateFull(rules, 10, 0)

// Pull out the cheapest term equivalent to root under a cost function.
term, cost := g.ParallelExtract(root, egraph.AstSizeCost, 0)
fmt.Println(term, cost) // x  1
```

That's the whole workflow: build a term, describe your rewrites as
patterns, saturate, extract. Everything below is detail on the pieces
that workflow is made of.

## What you can do with it

Every capability has a sequential entry point (the correctness
baseline) and, where the phase parallelizes, a `Parallel*` twin with
the identical contract plus a `workers int` argument (`0` = all
cores). Mix and match freely — e.g. `ParallelSearchAll` followed by
the plain sequential `Rebuild` is fine.

| You want to... | Sequential | Parallel |
|---|---|---|
| Build/insert a term or e-node | `AddTerm`, `Add` | — (read-mostly, not a hot path) |
| Merge two e-classes | `Union` | — |
| Restore hashcons + congruence after unions | `Rebuild` | `ParallelRebuild` |
| Find every rule match in the graph | `SearchAll` | `ParallelSearchAll` |
| Apply a batch of matches | `ApplyAll` | — (mutates the graph, kept single-threaded) |
| Run search→apply→rebuild to a fixed point | `Saturate` | `ParallelSaturate` (parallel search only), `ParallelSaturateFull` (parallel search *and* rebuild) |
| Extract the cheapest equivalent term | `Extract` | `ParallelExtract` |

A few things worth knowing before you reach for the parallel calls:

- **Never call a `Parallel*` read (search/extract) concurrently with
  anything that mutates the graph** (`Add`, `Union`, `Rebuild`,
  `ApplyAll`) — the read-only parallel paths use a non-compressing
  `findRO` specifically so they don't race with a compressing `Find`,
  which only holds if searches run to completion before any mutation
  starts. `ParallelSaturate`/`ParallelSaturateFull` already sequence
  this correctly for you; only worry about it if you're calling the
  phases individually.
- **Plug in your own cost model** by implementing `CostFn` — a
  function from `(ENode, childCosts []float64) float64`. Only
  `AstSizeCost` (every node costs 1) ships out of the box.
- **Rules are just LHS/RHS `Pattern`s.** Build them with `PNode`,
  `PVar`, and `Rule`; a pattern operator starting with `?` is a
  variable, anything else must match an e-node's operator and arity
  exactly.

## Why Go, and why this shape of parallelism

Go's goroutines are a CPU-multicore concurrency model, not a GPU one —
there's an actual Go team proposal to let goroutines run on CUDA-like
cores, and it was closed "not planned" for exactly the reasons you'd
expect (scheduler design, host/device memory sync, toolchain
complexity). So the parallelism here targets multicore CPUs and leans on
Go's actual strengths — goroutines, channels, `sync.WaitGroup`, plain
slices as lock-free scratch space — rather than trying to fight the
runtime into doing something it isn't built for.

The three phases of equality saturation parallelize in three genuinely
different ways, which is part of why this was worth building rather than
being a one-line `sync.WaitGroup` exercise:

- **Matching (`ParallelSearchAll`)** is embarrassingly parallel: it never
  mutates the e-graph, so any number of goroutines can search different
  e-classes at once. The only subtlety is that the union-find's normal
  `Find` performs path compression (a write), which would race under
  concurrent readers — so the parallel path uses a non-compressing
  `findRO` instead. Work is split into one static contiguous chunk per
  worker rather than doled out one item at a time over a channel, because
  for graphs where a single match attempt is cheap, per-item channel
  synchronization can cost more than the match itself.

- **Rebuild (`ParallelRebuild`)** is a bulk-synchronous-parallel (BSP)
  restructuring of `egg`'s worklist algorithm. Within a round, every dirty
  e-class's repair is *planned* concurrently (pure reads, safe by
  construction because nothing mutates the graph until the round's
  `WaitGroup` returns), then the round's plans are *applied*
  single-threaded in two passes — first installing every class's own
  parent list, then performing the unions those plans discovered.
  (Doing it in one pass is the one correctness trap in this whole
  project: applying a union before every plan's parent-list assignment
  lets that assignment silently clobber parents another plan's union had
  just merged in. Splitting into two passes avoids it.) Cascading merges
  flow into the next round's worklist exactly as `egg`'s does, so it
  converges to the identical fixed point, just batched by round instead
  of by individual e-class.

- **Extraction (`ParallelExtract`)** switches the usual Gauss-Seidel-style
  relaxation (every e-class sees others' already-updated costs within the
  same pass) to Jacobi-style relaxation (every round reads only the
  previous round's finished snapshot). That makes every e-class's update
  within a round independent, so costs and best-node choices live in
  plain slices indexed by e-class id — two goroutines writing different
  slots touch disjoint memory and need no locking at all.

## Two real bugs this surfaced

Building the correctness test suite (and specifically, trying to make the
parallel and sequential paths agree) found two genuine bugs, not just in
the new parallel code:

1. **Stale child references in cost lookup** (`egraph/extract.go`).
   `EGraph` never rewrites an already-stored e-node's children in place
   after a later union — same as `egg` — so every consumer is supposed to
   canonicalize a child id via `Find` before using it. The original
   extraction code forgot to, which meant a class whose only node
   referenced a since-merged-away child id would silently and permanently
   look like it had infinite cost. `TestExtractHandlesStaleChildReferences`
   pins this down.

2. **A cascading-merge crash in the *sequential* `repair()`**
   (`egraph/egraph.go`). Once the rule set included commutativity, a
   dense enough congruence cascade could merge an e-class away *during*
   the very worklist batch that still listed it, and the next iteration
   crashed on a nil map lookup. Fixed by re-resolving to the current
   canonical id at the top of `repair()`. Interestingly, `ParallelRebuild`
   never had this bug — its BSP phase split (no mutation happens until
   every plan for the round is computed) rules the whole failure mode out
   structurally, which wasn't the original motivation for that design but
   turned out to be a real side benefit of it.

Both are covered by regression tests in `egraph/egraph_test.go`.

## Benchmarks

Measured on this project's own 2-core sandbox (`go test -bench . -cpu 1,2`
against a synthetic e-graph of ~4,000 random arithmetic expressions over
60 variables). These are the actual numbers from this run, not estimates:

| Phase | Sequential (1 core) | Parallel (1 core) | Parallel (2 cores) | Speedup (seq → parallel, 2 cores) |
|---|---|---|---|---|
| Search (`SearchAll`) | 57.1 ms | 58.5 ms | 34.3 ms | **1.66x** |
| Extract | 54.3 ms | 45.8 ms | 30.0 ms | **1.81x** |
| Rebuild | 521.5 ms | 184.3 ms | 137.6 ms | **3.79x** (see caveat) |
| Full saturation loop, end to end | 1671.0 ms | 1610.5 ms | 931.6 ms | **1.79x** |

Search and Extract are clean, apples-to-apples parallelism numbers: the
exact same algorithm, just spread across workers. On a genuinely
two-core box, 1.66–1.81x is close to what you'd hope for, and the honest
expectation on a real many-core machine is that these two keep scaling
roughly with core count (they're the embarrassingly-parallel and
Jacobi-relaxation phases respectively).

Rebuild's 3.79x needs an asterisk: part of it isn't parallelism at all.
Going from sequential to `ParallelRebuild` *at 1 core* already drops
521.5 ms to 184.3 ms (2.83x) — that's the BSP restructuring (batching
every round's unions after its hashcons fixups, instead of interleaving
them) generating substantially less GC pressure, confirmed with
`pprof`. The further drop from 184.3 ms to 137.6 ms going from 1 to 2
cores (1.34x) is the actual parallelism. Both numbers are real and both
are reported above rather than only quoting the flattering combined one.

Run them yourself with:

```
go test ./egraph/... -run '^$' -bench . -benchtime 2s -cpu 1,2,4,8
```

on a machine with more cores to see how each phase actually scales
past 2.

## Correctness

```
go test ./...            # unit tests, sequential + parallel
go test ./... -race      # same, with the race detector
go test ./... -race -count=25 -run TestParallel   # stress the concurrent paths
```

The parallel test suite (`egraph/parallel_test.go`) doesn't just check that
the parallel code runs without racing — it builds two e-graphs from
identical construction sequences, runs one through the sequential path and
one through the parallel path, and asserts they agree: the same
(rule, e-class, substitution) matches, the same union-find partition of
the original ids, and the same extraction cost.

## Layout

```
egraph/
  unionfind.go          sequential union-find (path compression + union by rank)
  egraph.go             ENode/EClass/EGraph, Add/Union/Rebuild (sequential baseline)
  term.go               plain Term tree for building input and reading extracted output
  match.go               Pattern/Subst/RewriteRule, sequential SearchAll/ApplyAll/Saturate
  extract.go             sequential cost-based extraction (Bellman-Ford-style relaxation)
  parallel_match.go       ParallelSearchAll, ParallelSaturate
  parallel_rebuild.go     ParallelRebuild (BSP), ParallelSaturateFull
  parallel_extract.go     ParallelExtract (Jacobi relaxation)
  egraph_test.go          sequential correctness tests
  parallel_test.go        parallel-vs-sequential agreement tests (run with -race)
  bench_test.go           benchmarks
```

## What this is not (yet)

This is a correctness- and design-focused MVP, not a production optimizer:
matching is naive backtracking rather than the relational e-matching
modern `egg`-family tools use, there's no proof production / explanation
for why two terms are equal, and the cost model is pluggable but only
`AstSizeCost` is provided out of the box. The parallel *design* — BSP
rebuild, Jacobi extraction, chunked matching — is the part meant to be
new and reusable; a real optimizer would still want relational
e-matching on top.

## License

[MIT](LICENSE)
