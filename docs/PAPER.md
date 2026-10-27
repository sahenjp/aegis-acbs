# ACBS: an exact bidirectional search with an adaptive edge-work scheduler

Author: sahen

Status: draft for internal review. Not submitted. No external review has taken
place.

## Abstract

We describe Aegis Coupled-Bound Search (ACBS), an exact point-to-point
shortest-path algorithm for weighted directed road graphs. ACBS advances a
forward and a reverse frontier under a single admissible potential and stops when
the sum of the two frontier minimum priority keys reaches the best complete path
found. That stopping rule is a standard reduced-cost bidirectional rule, not a
contribution, and Section 3 explains why it is also not the g-bound that an
earlier draft claimed. What ACBS adds is a scheduler that decides which frontier
receives the next chunk of edge work, using the observed rise in the coupled
lower bound per unit of work.

We measured ACBS against Dijkstra, bidirectional Dijkstra, and A* on real
OpenStreetMap networks for Tokyo (1,098,173 nodes) and Osaka (748,903 nodes),
under both a distance metric and a travel-time metric. Every query returned an
exact shortest path. Against bidirectional Dijkstra, the median speedup was 6.04
times in Tokyo and 4.38 times in Osaka on distance, and 1.82 and 1.54 times on
travel time. Against plain unidirectional Dijkstra the ratios are larger, 8.64 and
6.59 on distance; the tables report both baselines so the two are not confused.
The advantage shrinks as the geographic lower bound carries less information, and
on a synthetic uniform lattice it disappears entirely. We report that reversal
rather than omitting it.

![ACBS search structure](figures/acbs-search.png)

## 1. Scope and what is new

This note reports measurements rather than a new algorithm. Two concepts must be
distinguished.

**Not new: the termination rule.** ACBS terminates when the sum of the two
frontier minimum priority keys reaches the incumbent, both in reduced units.
Section 3 shows that this is a standard reduced-cost bidirectional rule and that
it is *not* the g-bound, contrary to what an earlier draft of this note asserted.
Siag et al. (2023) enumerate seventeen non-dominated global bounds and prove that
the maximum over available bounds is the tightest single condition. ACBS does not
evaluate any of them directly, because its heaps are ordered by reduced key. An
earlier internal note described the rule as a contribution. It is not.

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

For source s, target t, and node v, the following hold.

- `g_F(v)` is the best known cost from s to v, `g_B(v)` the best known cost
  from v to t.
- `h_F(v)` is an admissible lower bound on the cost from v to t, and `h_B(v)`
  the corresponding bound from s to v. Both are derived from Earth-chord
  distance scaled by a conservative minimum cost per metre, so both are zero
  when coordinates or a safe scale are unavailable.
- The balanced potential is `phi2(v) = h_F(v) - h_B(v)`.
- Priority keys for node v are `k_F(v) = 2 g_F(v) + phi2(v) - phi2(s)` and
  `k_B(v) = 2 g_B(v) + phi2(t) - phi2(v)`.
- `U` is the cost of the best complete path found so far.

Consistency of the directional bounds keeps reduced edge costs non-negative in
both directions, which lets each frontier use label setting on its reduced-cost
graph.

## 3. What the termination rule is, and what it is not

The implementation stops when the sum of the two frontier minimum keys reaches
the incumbent, both expressed in reduced units:

```
min_{u in Open_F} k_F(u) + min_{w in Open_B} k_B(w) >= 2U + phi2(t) - phi2(s)
```

(`internal/search/acbs.go`). Substituting `phi2(v) = h_F(v) - h_B(v)` and writing
u and w for the two frontier minima gives

```
k_F(u) + k_B(w) = 2(g_F(u) + g_B(w)) + h_F(t) - h_B(s) + (h_F(u) - h_B(u)) - (h_F(w) - h_B(w))
```

so the cancellation is exact only when u and w carry the same potential, which holds once the
frontiers have met. Away from the meeting point the extra `(phi2(u) - phi2(w))` term
remains.

**This is not the g-bound, and an earlier draft of this section wrongly said it was.**
The g-bound of Siag et al. (bound B3) is

```
min_{x in Open_F} g_F(x) + min_{y in Open_B} g_B(y) + epsilon
```

which takes the minimum of the *original* g-values across each whole frontier and
uses no potential at all. ACBS does not track those minima. Its heaps are ordered
by reduced key, so the node carrying the smallest key is not in general the node
carrying the smallest g. The reduced-key sum is therefore a lower bound of a
different kind, valid because the reduced costs are non-negative, but it is not
bound B3.

This distinction matters twice over. It removes the claim that ACBS reuses a
known bound, which is a claim we cannot support; and it explains the failure
recorded in Section 5.4, where deriving the f-bound and the b-bound from
frontier nodes and converting them into reduced units produced values above the
true optimum.

Two consequences follow, and both shape what this note can claim.

First, the stopping rule used here is a standard reduced-cost bidirectional rule,
not a contribution. Whatever advantage the measurements show therefore comes from
the order in which work is performed, not from the test that ends the search.

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
profile. The edge cost is `ceil(meters * 1000)` for the distance metric and
`ceil(meters / speed * 1000)` for the travel-time metric.

| Graph | Nodes | Directed edges | heuristicStrength (distance / time) |
|---|---:|---:|---:|
| Tokyo | 1,098,173 | 2,262,568 | 0.9999 / 0.2505 |
| Osaka | 748,903 | 1,575,615 | 1.0000 / 0.2786 |
| Synthetic lattice | 1,000,000 | 2,596,627 | 0.9955 |

`heuristicStrength` is the ratio of the minimum to the mean cost per metre. In
mathematics a bound is *saturated* when equality holds, which is the opposite of
what happens here, so the term is avoided. A travel-time metric makes the
geographic bound much **looser**: to stay admissible it must assume the slowest
possible speed everywhere, while real roads vary widely. That loses pruning
power, which is why travel time is reported separately.

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
pairs, the geographic bound never exceeded the exact shortest-path distance.

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

### 5.4 Do more bounds help?

Sections 5.1 through 5.3 measure the speed of ACBS but say nothing about whether
its stopping condition can be tightened. This section reports an attempt to do
that, and why it did not produce the answer we wanted.

The intent was to terminate on the maximum of several admissible global bounds,
following Siag et al. (2023), who show that the maximum over the bounds an
algorithm can keep is the tightest condition available. Two variants were built.

- `multibound`, a plain bidirectional A* with no scheduler, so that only the
  stopping condition varies;
- a bound-mode switch on ACBS itself, so that the scheduler could be held fixed
  while only the stopping condition widened.

**Both variants were unsound and neither is in the released code.** The
diagnosis is worth recording because it is the same mistake twice.

The frontiers are ordered by reduced key. That key is a function of the node's
`g`, its potential, and the source or target potential, so the node carrying the
smallest reduced key is *not* in general the node carrying the smallest `g`.
Deriving an f-bound or a b-bound from the peeked node, and then converting that
value into reduced units to compare against the reduced incumbent, mixes two
different orderings. The result exceeded the true optimum, which is exactly what
a lower bound must never do: on real Tokyo queries the ACBS variant returned
overestimates on 5 of 40 sampled pairs, and the unscheduled variant on 33 of 300.
A "lower bound" that reaches 37,707,590 against a true optimum of 36,691,793 is
not a lower bound.

The bound-mode switch was reverted rather than shipped behind a flag, because a
termination condition that is wrong in the unsafe direction runs fast and answers
wrongly. What remains in the release is the plain `multibound` variant, and even
that one must be read carefully: after the fix it computes the reduced-key sum
and then assigns the same value to the f-bound and the b-bound slots, so it is
in practice only the reduced-cost rule plus a smaller-key expansion order. It is
kept as a measurement vehicle, not as an implementation of the multi-bound idea.

Measured on the Tokyo graph with the distance metric, 300 queries, all correct.

| Algorithm | Median | vs Dijkstra | Expanded | Relaxed | Correct |
|---|---:|---:|---:|---:|---:|
| Dijkstra | 81.06 ms | 1.00x | 531,266 | 1,091,302 | 300/300 |
| bidirectional Dijkstra | 61.45 ms | 1.32x | 411,246 | 818,508 | 300/300 |
| A* | 18.33 ms | 4.42x | 70,630 | 144,344 | 300/300 |
| ACBS | **9.76 ms** | **8.31x** | 44,726 | 92,176 | 300/300 |
| multibound | 15.71 ms | 5.16x | 54,864 | 60,356 | 300/300 |

Paired against ACBS, multibound finished ahead on only 23 of 300 queries. It
relaxes 35 percent fewer edges, so advancing on the smaller key does avoid work,
but it expands 23 percent more nodes. The scheduler is worth more than the
reduced-key ordering here.

**So the question remains open.** Whether a sound multi-bound termination
condition, combined with the ACBS scheduler, would beat the current rule has not
been measured.

## 6. Limitations

- Two cities, one machine. No claim is made about other cities or hardware.
- The stopping rule is known; the scheduler is the only candidate contribution
  and its distinctness from prior expansion policies is unresolved.
- Turn restrictions are not modelled. The Tokyo box contains 2,089 OSM
  restrictions, 0.092 percent of directed edges. They are few, sparse, and
  neutral with respect to algorithm choice, so they are unlikely to change the
  ranking, but absolute distances here are optimistic.
  These graphs are a performance test bed, not a validation of routing
  correctness.
- Only one stopping condition is consulted. Section 5.4 records that both
  attempts to widen it were unsound and were reverted or reduced, so whether a
  sound multi-bound condition would help is unknown.
- **The scheduler itself is not specified in this draft.** It is the only
  candidate contribution, and it is described here only in prose. An analysis of
  chunk sizing, the efficiency score, and the budget rule is required before this
  can be evaluated by anyone else.
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
- Goldberg, Harrelson. Computing the Shortest Path: A* Search Meets Graph
  Theory. SODA 2005. (Balanced bidirectional A* termination.)