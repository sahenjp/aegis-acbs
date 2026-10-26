# Exact shortest-path candidates: measured results

Date: 2026-10-26
Status: **experimental**. Nothing in this document is a speed claim.

## What was attempted

Three additional exact point-to-point search variants were implemented and
measured against the existing baselines to test whether the ACBS scheduler
could be beaten on road-like graphs.

| Name | CLI | Idea |
|---|---|---|
| MEET | `meet` | Exact bidirectional search advancing the frontier with the smaller lower bound, cut-pruning at pop, and the coupled `mu <= kF + kB` termination rule. |
| PB-Buckets | `pb-buckets` | Bidirectional Dijkstra whose two frontiers are served by a radix heap with monotone bucket indexing. |
| CATCH | `catch`, `catch-ab`, `catch-r` | Exact bidirectional A* on the geographic lower bound (`MinCostPerMeter`), with per-frontier independent termination. |

All three are heuristic-free (MEET, PB) or heuristic-guided (CATCH) and none
use preprocessing.

## Correctness

Every algorithm returns the same shortest-path distance and a valid path as
Dijkstra. Verification was done by an independent agent using two oracles that
share no helper code with the production search: an `O(n^2)` reference
Dijkstra, and a Python implementation that parses the `.aegis` container
directly.

| Check | Result |
|---|---|
| Random digraphs (self-loops, parallel edges, unreachable sinks) | 1200 / 1200 per algorithm |
| route CLI vs independent oracle, 10k and 100k grids | 200 / 200 per algorithm |
| benchmark, 600 queries, 10k and 100k grids | 600 / 600 per algorithm |
| Hatfield repro graphs, 3000 queries | 3000 / 3000 per algorithm |
| Cross-query determinism (distance and expanded/relaxed/pushes) | identical |

`s == t` returns `Reachable=true, Distance=0` for all entry points.

### Defects found during this work, and their resolution

These are recorded because each was caught by measurement after the author's own
tests passed.

- **MEET**: an early revision cut-pruned on a global "best overlap" value. That
  bound is not admissible for a specific candidate and produced over-estimates
  on a 4-node graph. The pruning was removed rather than patched.
- **PB-Buckets**: an early revision (a) terminated on last-settled keys instead
  of frontier minima and (b) used a bit-reversed bucket symbol that broke radix
  monotonicity, so keys were popped out of order. Both are fixed; the
  termination rule is now `best <= kF + kB` over frontier minima.
- **CATCH**: an early revision terminated on `min(frontierKeys) <= best` with
  h-inflated keys, which is unsound and produced 26-54 wrong answers per 300.
  A suggested fix that summed the two direction keys (`best <= bF + bB`) is
  also unsound for per-direction `g + h` keys, because that sum inflates the
  bound by `hF + hB`. The exact rule is `best <= bF && best <= bB`.
- **Verifier test helper**: generated zero-cost edges, which `Finalize`
  rejects. Fixed to generate strictly positive costs.

## Performance

Measured on this machine, 600 queries, 5 repeats, interleaved order, seed
424242. Ratios are against plain Dijkstra on the same run.

| Algorithm | 10k geomean vs Dijkstra | 100k geomean vs Dijkstra | 100k median expanded |
|---|---:|---:|---:|
| Dijkstra | 1.00x | 1.00x | 47225 |
| bidirectional Dijkstra | **1.70x** | **2.13x** | 31125 |
| MEET | 1.69x | 2.13x | 31125 |
| PB-Buckets | 1.44x | 1.79x | 30085 |
| A* | 1.04x | 1.45x | 13083 |
| CATCH | 0.69x | 0.83x | 26446 |

**No candidate beats the baselines.** MEET is functionally equivalent to
bidirectional Dijkstra. PB-Buckets expands 3.3% fewer nodes than bidirectional
Dijkstra yet is 6-14% slower in wall clock: strict 1:1 alternation between
frontiers forces the weaker `kF + kB` bound, while the baselines always advance
the smaller frontier.

CATCH is bimodal. It wins on median for long queries and loses on mean, because
short local queries pay for the A* potential. Quoting either median or mean
alone misrepresents it.

## Fairness

Audited by the independent verifier. No algorithm reads precomputed data that
another algorithm is denied: there is no preprocessing anywhere, no landmark or
distance tables, the same `*graph.Graph` object and query set are used, and
steady-state allocation is 0 B per query for all of them.

Two caveats that must accompany any comparison:

1. **A\* and CATCH use the geographic lower bound; MEET, bidirectional Dijkstra
   and PB-Buckets do not.** Both groups are legitimate, but they are not a
   same-class comparison and must be reported separately.
2. Uniform grid graphs inflate the node advantage of heuristic-guided searches
   in ways that need not transfer to real road networks. Unproven here.

## What this does not establish

- It does not establish that ACBS is faster or slower in general.
- It does not establish generality to real road networks. The large graphs used
  here are synthetic grids.
- It does not establish academic novelty for any of these methods. The novelty
  checklist in `docs/NOVELTY_CHECKLIST.md` remains incomplete.

## The 1M question: is ACBS's large-graph win real?

A single measurement suggested ACBS was the fastest algorithm at 1M nodes
(29.56 ms against bidirectional Dijkstra's 33.64 ms). That was re-measured and
stress-tested independently, because one run is not evidence.

The rerun reproduced it across three seeds on the uniform lattice
(`grid-1m`, 1,000,000 nodes, 2,596,627 edges, 200 queries, 5 repeats,
interleaved), all queries correct 200/200:

| Seed | Dijkstra | bidirectional Dijkstra | ACBS | ACBS vs bidirectional |
|---|---:|---:|---:|---:|
| 424242 | 60.82 ms | 29.57 ms | 18.49 ms | 1.60x |
| 1010 | 71.31 ms | 40.93 ms | 27.11 ms | 1.51x |
| 7 | 75.92 ms | 40.68 ms | 30.49 ms | 1.33x |

The win is real **on that graph**. But the lattice has
`heuristicStrength = 0.996`, meaning the geographic lower bound is nearly exact.
Real road networks do not have that property, so the same 1M topology was
re-weighted to weaken the heuristic while keeping every other parameter fixed:

| Graph | heuristicStrength | Dijkstra | bidirectional Dijkstra | ACBS | ACBS vs bidirectional |
|---|---:|---:|---:|---:|---:|
| grid-1m | 0.996 | 71.31 ms | 40.93 ms | 27.11 ms | **1.51x** |
| weak-b100s165 | 0.434 | 69.47 ms | 35.51 ms | 32.88 ms | 1.08x |
| weak-b100s230 | 0.311 | 67.35 ms | 35.46 ms | 35.13 ms | 1.01x |
| weak-b110s260 | 0.264 | 67.73 ms | 36.44 ms | 36.51 ms | 1.00x |
| weak-b115s310 | 0.202 | 67.73 ms | 35.27 ms | 38.75 ms | **0.91x** |
| weak-b125s450 | 0.096 | 67.39 ms | 36.01 ms | 43.32 ms | **0.83x** |

**The advantage is a property of the heuristic, not of the algorithm.** It falls
monotonically to parity near heuristic strength 0.26 and becomes a loss as the
bound weakens further. A* degrades in the same direction and much more steeply
(24.9 ms at 0.996, 99.2 ms at 0.096).

All runs were 200/200 correct. Verified by an independent agent in an isolated
copy of the repository, with host load recorded.

### What may and may not be claimed

- May be claimed: on uniform grid graphs with a near-exact geographic lower
  bound, ACBS expands fewer nodes than bidirectional Dijkstra and finishes
  sooner at 1M nodes.
- May **not** be claimed: that ACBS is faster than existing methods in general,
  or on real road networks. The crossover point has not been located, and no
  real-world graph has been measured. Every large graph here is synthetic.

This is exactly the kind of scale-dependent result that the v1.0.0 gate in
`docs/RELEASE_PLAN.md` requires independent reproduction to settle.

