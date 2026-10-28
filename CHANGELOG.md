# Changelog

Public semantic versioning starts at `v0.1.0`. Earlier numbered tags were private research iterations and are not part of the public release series.

## v0.2.2

- Add Section 3 of the paper: a full specification of the adaptive scheduler (per-direction state, work and gain measure, direction rules, budget schedule), with every constant checked against internal/search/acbs.go.
- Add Section 6.3: a decomposition of the speedup into an expansion factor and a per-node cost factor, per-query-class results, and an explanation of why the advantage is weakest on short queries.
- Correct Section 6.3 after a pre-publication audit recomputed every figure from the stored raw JSON: an earlier draft had computed the decomposition on a subset that dropped duplicate query pairs, and mixed two median conventions.
- Add a test that verifies the section 3.2 work formula and the section 3.4 budget set against actual traced scheduler chunks (156 chunks on real data).
- Record in the limitations that the scheduler has not been compared against any other policy beyond the static ablation.
## v0.2.1

- Correct three errors in `docs/PAPER.md` that an independent review found.
- The abstract overstated the baseline: it reported the speedup against plain
  unidirectional Dijkstra (8.64x) as if it were the speedup against
  bidirectional Dijkstra. Measured against bidirectional Dijkstra the ratios are
  6.04 (Tokyo distance), 4.38 (Osaka distance), 1.82 and 1.54 (travel time).
- The paper claimed the termination rule reduces to the g-bound, bound B3 of
  Siag et al. That derivation only holds when both frontiers are read at the same
  node. The rule is a standard reduced-cost bidirectional rule and is not B3.
  Section 3 now derives it correctly and records the earlier error.
- A table gave Dijkstra and bidirectional Dijkstra the same relaxed-edge count.
  The correct value for Dijkstra is 1,091,302.
- Section 5.4 is rewritten after re-reading the code: `multibound` assigns the
  reduced-key sum to the f-bound and b-bound slots as well, so it is not a
  multi-bound implementation. The section now says so and marks the question
  open.
- Apply English corrections from the same review, including tense consistency,
  `shortest-path` as a compound adjective, a defined epsilon, and replacement of
  "saturates" (which in mathematics means equality holds, the opposite of the
  intended meaning).
- Add to the limitations that the scheduler, the only candidate contribution, is
  not specified in the draft.
- Add `docs/ZENODO.md` with the deposit metadata.

## v0.2.0

- Measure ACBS on real OpenStreetMap road networks for Tokyo (1,098,173 nodes,
  2,262,568 directed edges) and Osaka (748,903 nodes, 1,575,615 edges), under
  both a distance metric and a travel-time metric. Every query returned an exact
  shortest path.
- On distance, ACBS finished ahead of bidirectional Dijkstra on 390 of 400 Tokyo
  queries (8.64x by median) and 288 of 300 Osaka queries (6.59x).
- On travel time the gains were 3.46x and 3.39x. The advantage shrinks as the
  geographic lower bound carries less information, and on a synthetic uniform
  lattice it disappears entirely.
- Add `docs/PAPER.md`, a draft that derives the termination rule, identifies it
  as the known g-bound (bound B3 of Siag et al., IJCAI 2023), and states that no
  novelty is claimed for it.
- Add `docs/CANDIDATES.md` recording three further exact search variants
  (MEET, PB-Buckets, CATCH) and a multi-bound experiment. None beat
  bidirectional Dijkstra; the reasons and the defects found along the way are
  documented rather than omitted.
- Add `docs/REPRODUCING.md` with bounding boxes, seeds, inspection values and an
  exactness check that does not trust the harness flag, because the raw reports
  for these runs are too large to commit.
- Add an architecture diagram of the search under `docs/figures`.
- Add `internal/graph/turn.go`, an OSM turn-restriction parser, and quantify the
  2,089 restrictions the current import drops in the Tokyo box.
- Move the Go module path and links to `sahenjp/aegis-acbs`.
- Credit the work to the pen name `sahen`, and add `CITATION.cff` and `AUTHORS`.

## v0.1.0

- Publish the first public research release without changing the default ACBS search.
- Record the Tokyo 10,000-query correctness, tail validation, isolated replay, rejected guard gate, and narrow diagnostic trigger.
- Add raw-evidence import and strict release-evidence validation scripts.
- Add English and Japanese README files and a dedicated evidence/limitations document.
- Update GitHub Actions to current official checkout/setup-go generations and test Go 1.23 compatibility plus Go 1.26.
- Replace the third-party GitHub Release action with GitHub CLI.
- Keep all rejected guard variants diagnostic/experimental only.

## v0.11.2-experimental

- Kept the default `aegis` scheduler unchanged after every v0.11.1 connection-guard candidate failed the Tokyo release gate.
- Added `profile-trigger` to trace all validation queries at deterministic chunk checkpoints.
- Added checkpoint features for lower-bound progress, frontier growth, queue and score imbalance, priority imbalance, stale rate, finite-meeting rate, directional work balance, and cumulative work.
- Added ranked one- and two-condition trigger-rule search with a configurable whole-suite match cap.
- Added JSON, CSV, and self-contained HTML trigger-profile reports.
- Extended opt-in ACBS trace events without adding telemetry to the normal hot path.

## v0.11.1-experimental

- Added three connection-guard candidates: chunk 32 until upper bound, chunk 40 until upper bound, and chunk 32 with a 16-chunk cap.
- Extended isolated regret replay to compare all candidates on the reproduced Tokyo tails.
- Added a machine-readable selector that rejects candidates exceeding 1% timing or work regression.
- Kept the normal `aegis` implementation unchanged pending the Tokyo release gate.


## v0.11.0-experimental

- Added the opt-in `aegis-late-guard` ACBS variant for reproduced late-upper-bound scheduler tails.
- The guard can activate only on travel-time routes after 48 completed chunks, before any upper bound, when direction switching is frequent and forward/backward efficiency estimates remain close.
- The guard uses the static lower-key direction rule and base edge budget for at most eight chunks, then returns to the normal adaptive scheduler.
- Added late-guard activation counters and chunk-level trace markers.
- Extended `replay-regret` to measure the guard beside adaptive and static ACBS, report improvement/regression, and emit a release-gate verdict.
- Added exactness and activation-scope tests for long time routes, distance routes, and short time routes.
- Added a machine-readable v0.11 release-gate script and a staged GitHub publication plan.
- Kept the default `aegis` algorithm unchanged until the Tokyo replay and 10,000-query release gates pass.

## v0.10.0-experimental

- Added `aegis replay-regret` to isolate and repeatedly remeasure meaningful tail cases retained by `validate-regret`.
- Added interleaved replay of Dijkstra, bidirectional Dijkstra, A*, static ACBS, and adaptive ACBS.
- Added classification into non-reproduced timing noise, adaptive-scheduler tails, and persistent classical-special-case tails.
- Added opt-in per-chunk ACBS traces with lower-bound gain, work, queue sizes, direction, efficiency scores, and upper-bound discovery.
- Added self-contained JSON, CSV, and HTML replay reports.
- Kept the production ACBS scheduler, potential, radix heaps, CSR graph, and exact stopping condition unchanged.

## v0.9.0-experimental

- Added `aegis validate-regret` for recursively aggregating benchmark reports across seeds and runs.
- Added observed meaningful-slowdown rate, Wilson 95% confidence interval, and exact one-sided 95% upper bound when zero events are observed.
- Added per-run JSON/CSV/HTML validation summaries and top meaningful slowdown retention.
- Added resumable `scripts/validate-tail.sh` for large multi-seed validation.
- Added configurable minimum query count and maximum acceptable meaningful-slowdown rate with CI-friendly exit status.
- Kept the v0.8 ACBS scheduler and search semantics unchanged.

## v0.8.0-experimental

- Added query-level regret diagnosis in JSON, CSV, and HTML.
- Added absolute latency penalty and configurable meaningful-regret thresholds.
- Added endpoint/query features and correlation analysis.
- Added meaningful slowdown and absolute-penalty metrics to benchmark summaries.
- Kept the v0.7 scheduler unchanged after rejecting a tail-guard prototype that regressed synthetic time-road performance.


## 0.7.0-experimental

- Remove inactive incumbent-pruning evaluation from the default ACBS hot path.
- Add explicit `aegis-prune` and retain `aegis-no-prune` as a compatibility alias.
- Make `aegis-static` isolate only the adaptive direction scheduler.
- Move projection and pruning variants behind `benchmark --experimental`.
- Clarify fastest-classical runtime and classical-oracle-regret labels.
- Label connection and queue work counters as medians in CLI output.
- Add in-process concurrent `stress` validation with sampled Dijkstra checks.
- Add worker-scaling and repeated soak scripts.

## 0.6.0-experimental

- Replace bidirectional binary heaps with exact monotone radix heaps over non-negative reduced keys.
- Compact forward and reverse adjacency into CSR arrays after graph finalization.
- Build the node-ID lookup map lazily instead of retaining it for every routing process.
- Store bidirectional parents and touched-node indexes as 32-bit values.
- Add `aegis-projection`, an exact linear-projection feasible-potential ablation.
- Keep the default `aegis` on the stronger balanced chord potential.
- Add radix ordering, projection reduced-cost, and dual-potential correctness tests.
- Preserve the adaptive scheduler, incumbent, and coupled lower-bound termination semantics.

## 0.5.0-experimental

- Replace `container/heap` with a specialized allocation-free binary heap.
- Reuse priority-queue backing arrays through single-frontier and bidirectional workspace pools.
- Reconstruct paths with one exact-sized allocation.
- Add heap-ordering and steady-state allocation regression tests.
- Add a large-grid allocation benchmark and reproducible v0.4/v0.5 comparison script.
- Keep ACBS scheduling, potential, pruning, and exact termination semantics unchanged.

## 0.4.0-experimental

- Interleave algorithms within every repetition using a deterministic per-query shuffle to reduce cache, thermal, and fixed-order bias.
- Add mean, minimum, maximum, p50, p95, and p99 latency to CLI, JSON, CSV, and HTML.
- Add optional untimed per-query allocation measurement with `--measure-memory`.
- Record process peak RSS and Go heap/runtime memory totals.
- Split connection accounting into all connection checks, finite forward/backward overlaps, and incumbent updates.
- Add publication-scale ten-seed validation and standalone memory-profile scripts.
- Preserve `--order rotated` for comparison with v0.3 reports.

## 0.3.0-experimental

- Split queue work into pushes, pops, and stale pops.
- Split incumbent pruning into pop-time and relaxation-time counters.
- Add meeting-check and p99 latency metrics.
- Define ratio-of-medians, median per-query, and geometric-mean speedups separately.
- Rename sub-1.0 baseline comparison to relative runtime and add clamped oracle regret.
- Add `aegis aggregate` with self-contained JSON, CSV, and HTML matrix reports.
- Add five-seed multi-graph benchmark automation.
- Add regional Japan preparation for Tokyo, Yokohama, Osaka, and Nagoya, each with distance and time graphs.
- Add matrix aggregation and pruning-counter consistency tests.

## 0.2.0-experimental

- Replace per-node great-circle evaluation with precomputed unit vectors and a consistent chord-distance potential.
- Change adaptive chunks from node-count budgets to edge-work budgets.
- Add incumbent-bound pruning using exact admissible forward/backward bounds.
- Add exactness certificate fields: upper bound, lower bound, and optimality gap.
- Add scheduler, potential, pruning, and upper-bound diagnostics.
- Add `aegis-static` and `aegis-no-prune` research ablations plus `benchmark --research`.
- Add 10,000 random directed time-road differential queries and chord admissibility checks.
- Add a formal novelty checklist and expanded correctness notes.

## 0.1.0-experimental

- Introduce Aegis Coupled-Bound Search.
- Replace the default Aegis portfolio selector with one exact coupled bidirectional algorithm.
- Add balanced-potential reduced costs, adaptive bound-progress scheduling, proof-oriented metrics, and ACBS visual reports.