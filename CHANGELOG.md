# Changelog

All notable changes to this module are documented here. Versioning follows
[Semantic Versioning](https://semver.org/): breaking API changes bump the
major version, backwards-compatible additions bump the minor version, and
fixes bump the patch version.

## v0.1.0 — initial release

- Sequential e-graph core: `UnionFind`, `EGraph` (`Add`/`Union`/`Rebuild`),
  `Term`, pattern matching (`Match`/`SearchAll`/`ApplyAll`/`Saturate`), and
  cost-based extraction (`Extract`).
- Parallel counterparts for every phase: `ParallelSearchAll`,
  `ParallelRebuild`, `ParallelExtract`, `ParallelSaturate`,
  `ParallelSaturateFull`.
- Correctness suite asserting the parallel and sequential paths agree on
  identical inputs, plus regression tests for two bugs found while building
  that suite (stale child references in extraction; a cascading-merge crash
  in sequential `repair()`).
- Benchmarks for all three parallelizable phases (`go test -bench .`).
