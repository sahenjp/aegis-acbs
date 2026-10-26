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

A single measurement suggested ACBS was the fastest algorithm at 1M nodes. It
was re-measured independently, and the answer is **no — ACBS is not fastest, and
the apparent win was not an ACBS effect at all.**

### The median was misleading

The rerun covered three seeds on the uniform lattice (`grid-1m`, 1,000,000
nodes, 2,596,627 edges, 200 queries, 5 repeats, interleaved). The ranking by
median was identical on all three seeds, which is where the earlier claim came
from. But a paired comparison of the same queries shows the opposite:

| Seed | Dijkstra | bidirectional Dijkstra | ACBS | paired ACBS/bidirectional | ACBS wins / loses |
|---|---:|---:|---:|---:|---:|
| 424242 | 60.82 ms | 29.57 ms | 18.49 ms | **1.001** | 72 / 114 |
| 1010 | 71.31 ms | 40.93 ms | 27.11 ms | 0.911 | 80 / 109 |
| 7 | 75.92 ms | 40.68 ms | 30.49 ms | 1.029 | 59 / 123 |

ACBS loses more queries than it wins on **every** seed. The good median comes
from a heavy tail that the median hides: p95 is 145-165 ms for ACBS against
98-112 ms for bidirectional Dijkstra. A tail is what a routing service actually
feels, so the median is the wrong headline.

### The advantage belonged to the geographic bound, not to ACBS

`aegis-static` disables the scheduler and keeps the heuristic. It trails full
ACBS by only 1.5-2.5 ms on every graph, and the paired ACBS/aegis-static ratio is
0.94-0.96 throughout. Whatever advantage exists comes from the A* geographic
lower bound, which `astar` also uses. **It is not a property of the ACBS
scheduler.**

### It decays monotonically and reverses

The same 1M topology was re-weighted to weaken the heuristic while holding node
count, edge count and shape fixed:

| Graph | heuristicStrength | paired ACBS/bidirectional | result |
|---|---:|---:|---|
| grid-1m | 0.996 | 1.001 | tied |
| weak-b100s165 | 0.434 | 1.148 | bidirectional wins |
| weak-b100s230 | 0.311 | 1.226 | bidirectional wins |
| weak-b110s260 | 0.264 | 1.252 | bidirectional wins |
| weak-b115s310 | 0.202 | 1.283 | bidirectional wins |
| weak-b125s450 | 0.096 | 1.352 | bidirectional wins 194/200 queries |

The mechanism is visible in the expansion counts: ACBS expands 114k nodes on the
lattice against bidirectional Dijkstra's 268k, but at strength 0.096 it expands
241k. The pruning advantage disappears while the constant factors stay worse.

Distance-metric road graphs sit around heuristicStrength 0.25-0.45, which is
squarely inside the losing band.

### What may and may not be claimed

- May be claimed: on lattice-like distance graphs where the geographic lower
  bound is near-exact, A* using that bound expands far fewer nodes than
  bidirectional Dijkstra.
- May **not** be claimed: that ACBS is the fastest exact algorithm at 1M nodes,
  or that it beats bidirectional Dijkstra in general. On paired queries it loses,
  and on realistic heuristic strengths it loses clearly.
- May **not** be claimed: that the ACBS scheduler contributes the advantage. The
  ablation shows it contributes almost nothing; the heuristic does.

All runs were 200/200 correct (11 reports x 5 algorithms x 200 queries, 0 errors).
The decisive run was repeated under a clean host load and reached the same
conclusion.

This is exactly the kind of scale-dependent result that the v1.0.0 gate in
`docs/RELEASE_PLAN.md` requires independent reproduction to settle, and it is a
concrete example of why a single measurement and a single statistic are not
enough.
