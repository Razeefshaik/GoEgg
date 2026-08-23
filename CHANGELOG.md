# Changelog

All notable changes to this module are documented here. Versioning follows
[Semantic Versioning](https://semver.org/): breaking API changes bump the
major version, backwards-compatible additions bump the minor version, and
fixes bump the patch version.

## v0.1.4 — add license

- Added the MIT LICENSE file (previously missing, so nothing was actually
  licensed for reuse despite the repo being public). Bumped the README's
  `go get` version references to match.

## v0.1.3 — docs, no code change

- Expanded README with an Installation section, a full runnable Quick
  start, and a task-to-function table pairing every sequential entry point
  with its parallel twin.

## v0.1.2 — rename, no code change

- Renamed the project from `pareg` to `GoEgg` to match the GitHub repo.

## v0.1.1 — comment cleanup, no behavior change

- Source comments trimmed to exported-symbol doc comments only (what
  `go doc` / pkg.go.dev show), via an AST-based tool rather than manual
  editing, so it's mechanically consistent across every file. The
  fully-commented version with inline design rationale lives on the `dev`
  branch.

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
