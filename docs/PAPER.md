# ACBS: an exact bidirectional search with an adaptive edge-work scheduler

Author: sahen

Status: draft for internal review. Not submitted. No external review has taken
place.

## Abstract

We describe Aegis Coupled-Bound Search (ACBS), an exact point-to-point shortest
path algorithm for weighted directed road graphs. ACBS advances a forward and a
reverse frontier under a single admissible potential, and stops when the sum of
the two frontier minima reaches the best complete path found. We identify this
termination rule as the g-bound, which is known: it is equivalent to bound B3 in
the enumeration of Siag et al. (IJCAI 2023). We therefore make **no novelty
claim for the stopping rule**. What ACBS adds is a scheduler that decides which
frontier receives the next chunk of edge work, using the observed rise in the
coupled lower bound per unit of work.

We measured ACBS against Dijkstra, bidirectional Dijkstra and A* on real
OpenStreetMap networks for Tokyo (1,098,173 nodes) and Osaka (748,903 nodes),
under both a distance metric and a travel-time metric. Every query returned an
exact shortest path. On distance, ACBS was 8.64 times faster than bidirectional
Dijkstra in Tokyo and 6.59 times faster in Osaka by median. On travel time the
gains were 3.46 and 3.39 times. The advantage shrinks as the geographic lower
bound carries less information, and on a synthetic uniform lattice it disappears
entirely. We report that reversal rather than omit it.

![ACBS search structure](figures/acbs-search.png)

## 1. Scope and what is new

This note reports measurements rather than a new algorithm. Two things have to
be kept apart.

**Not new: the termination rule.** ACBS terminates when the sum of the two
frontier minimum priority keys reaches the incumbent. Section 3 shows that this
sum reduces to the well-known g-bound. Siag et al. (2023) enumerate seventeen
non-dominated global bounds and prove that the maximum over available bounds is
the tightest single condition; the g-bound is one of those seventeen. An earlier
internal note described the rule as a contribution. It is not.

**The candidate contribution: the scheduler.** Given a fixed potential and a
fixed stopping rule, an exact bidirectional search still has freedom in how much
edge work to give each frontier before reconsidering. ACBS measures, online, how
much the coupled lower bound rose per unit of work in each direction, and
allocates the next chunk accordingly. The ablation that removes only the
scheduler, keeping the potential and the stopping rule identical, is available
as `aegis-static`.

Whether that scheduler is genuinely distinct from prior expansion policies is
**not yet established**. Siag et al. describe expansion policies aimed at
raising a chosen bound quickly; a scheduler that raises the coupled bound
quickly is plausibly an instance of that idea rather than a departure from it.
`docs/NOVELTY_CHECKLIST.md` records this as an open item.

## 2. The search

For source s, target t and node v:

- `g_F(v)` is the best known cost from s to v, `g_B(v)` the best known cost
  from v to t.
- `h_F(v)` is an admissible lower bound on the cost from v to t, and `h_B(v)`
  the corresponding bound from s to v. Both are derived from Earth-chord
  distance scaled by a conservative minimum cost per metre, so both are zero
  when coordinates or a safe scale are unavailable.
- The balanced potential is `phi2(v) = h_F(v) - h_B(v)`.
- Front keys are `k_F = 2 g_F + phi2(v) - phi2(s)` and
  `k_B = 2 g_B + phi2(t) - phi2(v)`.
- `U` is the cost of the best complete path found so far.

Consistency of the directional bounds keeps reduced edge costs non-negative in
both directions, which lets each frontier use label setting on its reduced-cost
graph.

## 3. The termination rule is the g-bound

The implementation stops when `k_F + k_B >= 2U + phi2(t) - phi2(s)`
(`internal/search/acbs.go`). Because `phi2(v) = h_F(v) - h_B(v)` and the
`phi2(v)` terms cancel between the two keys,

```
k_F + k_B = 2 (g_F + g_B) + h_F(t) - h_B(s)
bestReduced = 2 U + h_F(t) - h_B(s)
```

so the test reduces to

```
U <= g_F + g_B
```

which is the g-bound, `gMin_F + gMin_B + epsilon`. In the enumeration of Siag et
al. (2023) this is bound B3. It is the same family used by MM (Holte et al.,
2017).

Two consequences follow, and both shape what this note can claim.

First, the measured advantage does not come from the stopping rule, because the
rule is shared with MM-class algorithms. It comes from the order in which work
is performed.

Second, Siag et al. observe that taking the maximum over several available
bounds gives the tightest condition available. ACBS consults only one. Section
5.4 reports what happened when we tested the obvious follow-up.

## 4. Method

All comparisons use the same graph object, the same query set, the same warmup,
and an interleaved measurement order, with no preprocessing for any algorithm.
Steady-state allocation is zero bytes per query for every algorithm. Results are
reported as the median over repeated measurements and as a paired comparison of
the same queries, because the two disagree on some graphs.

**Data.** OpenStreetMap extracts from Geofabrik, restricted to a box around the
city centre and to ways that carry vehicle traffic, imported with the `car`
profile. Cost is `ceil(meters * 1000)` for the distance metric and
`ceil(meters / speed * 1000)` for travel time.

| Graph | Nodes | Directed edges | heuristicStrength (distance / time) |
|---|---:|---:|---:|
| Tokyo | 1,098,173 | 2,262,568 | 0.9999 / 0.2505 |
| Osaka | 748,903 | 1,575,615 | 1.0000 / 0.2786 |
| Synthetic lattice | 1,000,000 | 2,596,627 | 0.9955 |

`heuristicStrength` is the ratio of the minimum to the mean cost per metre, so
it falls as edge costs stop tracking straight-line distance. A travel-time
metric saturates the geographic bound, which is exactly why it is reported
separately.

## 5. Results

### 5.1 Distance metric

| Graph | Algorithm | Median | p95 | vs Dijkstra | Expanded | Correct |
|---|---|---:|---:|---:|---:|---:|
| Tokyo | Dijkstra | 72.35 ms | 156.88 ms | 1.00x | 531,031 | 400/400 |
| Tokyo | bidirectional Dijkstra | 50.60 ms | 125.64 ms | 1.43x | 368,789 | 400/400 |
| Tokyo | A* | 16.95 ms | 104.08 ms | 4.27x | 70,630 | 400/400 |
| Tokyo | ACBS | **8.38 ms** | **46.77 ms** | **8.64x** | 44,201 | 400/400 |
| Osaka | Dijkstra | 50.65 ms | 119.15 ms | 1.00x | 373,981 | 300/300 |
| Osaka | bidirectional Dijkstra | 33.71 ms | 87.97 ms | 1.50x | 234,918 | 300/300 |
| Osaka | A* | 13.70 ms | 87.83 ms | 3.70x | 57,780 | 300/300 |
| Osaka | ACBS | **7.69 ms** | **48.52 ms** | **6.59x** | 34,137 | 300/300 |

Paired against bidirectional Dijkstra, ACBS finished ahead on 390 of 400 Tokyo
queries and 288 of 300 Osaka queries.

### 5.2 Travel-time metric

| Graph | Algorithm | Median | p95 | vs Dijkstra | Expanded | Correct |
|---|---|---:|---:|---:|---:|---:|
| Tokyo | Dijkstra | 95.54 ms | 198.28 ms | 1.00x | 554,840 | 300/300 |
| Tokyo | bidirectional Dijkstra | 50.23 ms | 127.40 ms | 1.90x | 319,568 | 300/300 |
| Tokyo | A* | 62.94 ms | 263.30 ms | 1.52x | 229,332 | 300/300 |
| Tokyo | ACBS | **27.64 ms** | **94.97 ms** | **3.46x** | 120,199 | 300/300 |
| Osaka | Dijkstra | 56.53 ms | 120.43 ms | 1.00x | 362,323 | 300/300 |
| Osaka | bidirectional Dijkstra | 25.68 ms | 80.22 ms | 2.20x | 183,213 | 300/300 |
| Osaka | A* | 41.51 ms | 171.00 ms | 1.36x | 166,990 | 300/300 |
| Osaka | ACBS | **16.70 ms** | **85.95 ms** | **3.39x** | 84,249 | 300/300 |

Paired against bidirectional Dijkstra, ACBS finished ahead on 252 of 300 Tokyo
queries and 223 of 300 Osaka queries.

A weak lower bound is only useful if it remains a valid one, so admissibility
was checked directly on the travel-time graph. Across 80 sampled source-target
pairs the geographic bound exceeded the exact Dijkstra distance zero times.

### 5.3 Where the advantage goes away

The gain depends on the metric. To isolate that dependence, a 1,000,000-node
uniform lattice was re-weighted so that the same topology carries progressively
less geographic information.

| heuristicStrength | ACBS vs bidirectional Dijkstra, paired |
|---:|---:|
| 0.996 | 1.001 (tied) |
| 0.434 | 0.926 |
| 0.311 | 0.991 |
| 0.264 | 1.002 |
| 0.202 | 1.099 |
| 0.096 | 1.203 |

On the lattice the advantage is absent even at high heuristic strength and
becomes a clear loss as the bound weakens. A uniform lattice rewards both
frontiers about equally, so there is little asymmetry for a scheduler to exploit.
Real road networks are irregular, and there the same scheduler keeps an
advantage even when the bound is weak.

This reversal is not unique to ACBS. Holte et al. (2017) note that as heuristic
accuracy changes, the advantage shifts between algorithms, and Shperberg et al.
(2020) formalise the related property of well-behavedness. Wang et al. (2025)
report the same direction in their own experiments, with their MEET algorithm
roughly on par with A* on one grid map and about twice as slow on another where
the heuristics are less informative.

### 5.4 Does a tighter termination condition help?

Section 5.1 to 5.3 measure the speed of ACBS but say nothing about whether its
termination condition is the tightest available. This section tests that.

Since ACBS keeps only the g-bound, the natural experiment is to keep several.
We added `multibound`, a plain bidirectional A* that terminates on the maximum
of the g-bound, the f-bound and the b-bound. It deliberately has no scheduler,
so the only change from a plain bidirectional A* is the termination condition.
That isolates the effect we want to measure.

The first version was wrong, and the benchmark said so: 267 of 300 queries
returned over-estimates. The cause is instructive. The frontiers are ordered by
reduced key, and we were reading the peeked item's `g` as the frontier minimum.
That is not valid, because the item holding the smallest reduced key need not
hold the smallest `g`. The resulting "lower bound" exceeded the true optimum,
which is exactly what a lower bound must never do. Converting the reduced-key
sum back to original units, as ACBS already does, fixed it, and the corrected
version returned 300 of 300.

The corrected version is still slower.

| Algorithm | Median | vs Dijkstra | Expanded | Relaxed | Correct |
|---|---:|---:|---:|---:|---:|
| Dijkstra | 81.06 ms | 1.00x | 531,266 | 818,508 | 300/300 |
| bidirectional Dijkstra | 61.45 ms | 1.32x | 411,246 | 818,508 | 300/300 |
| A* | 18.33 ms | 4.42x | 70,630 | 144,344 | 300/300 |
| ACBS | **9.76 ms** | **8.31x** | 44,726 | 92,176 | 300/300 |
| multibound | 15.71 ms | 5.16x | 54,864 | 60,356 | 300/300 |

Paired against ACBS, multibound finished ahead on only 23 of 300 queries.

The work counters explain the result. multibound relaxes 35 percent **fewer**
edges than ACBS, so the tighter bound does prune work, but it expands 23 percent
**more** nodes. It advances on the smaller key, while ACBS spends edge work
according to how fast each direction is raising the coupled bound. **The
scheduler is worth more than the bound.** That is consistent with ACBS's own
ablation, where removing only the scheduler cost 1.5 to 2.5 ms per query on a
1,000,000-node lattice.

So the theoretical improvement in the termination condition did not translate
into a practical one, and the reason is that the experiment held the expansion
order fixed. The obvious follow-up is to hold the scheduler fixed instead and
vary only the bound. We tried that and could not make it exact, so it is
recorded as an open question rather than a result.

That attempt added a bound-mode switch to the ACBS search so the scheduler could
run unchanged while the termination rule widened. On real Tokyo queries the
variant returned over-estimates on 5 of 40 sampled pairs, all above the true
optimum. Disabling the individual extra terms one at a time showed the fault was
in how the f-bound and the b-bound were derived from frontier nodes whose labels
are ordered by reduced key: converting a per-node bound into reduced units and
comparing it against the reduced incumbent is not sound in general, because the
conversion rounds and because a node's label need not be the frontier minimum.

The change was reverted rather than shipped behind a flag. Exactness comes before
speed, and a termination condition that is wrong in the unsafe direction runs
fast and returns wrong answers. The question this section set out to answer, does
a tighter bound help once the scheduler is held fixed, therefore remains open.

## 6. Limitations

- Two cities, one machine. No claim is made about other cities or hardware.
- The stopping rule is known; the scheduler is the only candidate contribution
  and its distinctness from prior expansion policies is unresolved.
- Turn restrictions are not modelled. The Tokyo box contains 2,089 OSM
  restrictions, 0.092 percent of directed edges. They are small in number,
  thinly spread and neutral with respect to algorithm choice, so they are
  unlikely to change the ranking, but absolute distances here are optimistic.
  These graphs are a performance test bed, not a validation of routing
  correctness.
- Only one of the enumerated bounds is consulted. Section 5.4 tests a
  three-bound variant and finds it slower with a plain expansion order, and
  records that the attempt to combine a wider bound with the ACBS scheduler
  could not be made exact and was reverted. Whether a sound version would help is
  unknown.
- Travel-time results use a speed model derived from `maxspeed` tagging, not
  observed traffic.

## 7. Reproducing

The repository is <https://github.com/sahenjp/aegis-acbs>. Synthetic graphs come
from `scripts/gen`. The OSM graphs are not distributed because of size; they are
rebuilt from the Geofabrik Kanto and Kansai extracts with a bounding box around
each city centre, then imported with `import-osm`. Full per-experiment records,
including the defects found during development and how each was resolved, are in
`docs/CANDIDATES.md`.

## References

- Chen, Holte, Zilles, Sturtevant. Front-to-End Bidirectional Heuristic Search
  with Near-Optimal Node Expansions. IJCAI 2017. arXiv:1703.03868.
- Holte, Felner, Sharon, Sturtevant, Chen. MM: A bidirectional search algorithm
  that is guaranteed to meet in the middle. Artificial Intelligence 252, 2017.
- Kaindl, Kainz. Bidirectional Heuristic Search Reconsidered. JAIR 7, 1997.
- Shperberg, Felner, Sturtevant, Shimony, Hayoun. Bidirectional Heuristic
  Search: Expanding Nodes by a Lower Bound. IJCAI 2020.
- Siag, Shperberg, Felner, Sturtevant. Front-to-End Bidirectional Heuristic
  Search with Consistent Heuristics: Enumerating and Evaluating Algorithms and
  Bounds. IJCAI 2023.
- Wang, Weiss, Mu, Salzman. Bidirectional Search while Ensuring
  Meet-In-The-Middle via Effective and Efficient-to-Compute Termination
  Conditions. IJCAI 2025.
- Alcázar, Riddle, Barley. A Unifying View on Individual Bounds and Heuristic
  Inaccuracies in Bidirectional Search. AAAI 2020.