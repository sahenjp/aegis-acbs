# ACBS: an exact bidirectional search with an adaptive edge-work scheduler

Author: sahen

Status: draft for internal review. Not submitted. No external review has taken
place.

## Abstract

We describe Aegis Coupled-Bound Search (ACBS), an exact point-to-point
shortest-path algorithm for weighted directed road graphs. ACBS advances a
forward and a reverse frontier under a single admissible potential and stops when
the sum of the frontier minimum priority keys reaches the cost of the best
complete path found. That stopping rule is a standard reduced-cost bidirectional rule, not a
contribution, and Section 4 explains why it is also not the g-bound that an
earlier draft claimed. What ACBS adds is a scheduler that decides which frontier
receives the next chunk of edge work, using the observed rise in the coupled
lower bound per unit of work.

We measured ACBS against Dijkstra, bidirectional Dijkstra, and A* on real
OpenStreetMap networks in four cities - Tokyo (1,098,173 nodes), Osaka (748,903),
London (761,244) and New York (353,434) - under a distance metric, and on Tokyo
and Osaka under a travel-time metric as well. Every query returned an exact
shortest path. Against bidirectional Dijkstra, the median speedup on distance was
6.04x in Tokyo, 4.38x in Osaka, 4.29x in London and 3.19x in New York, and 1.82x
and 1.54x on travel time in Tokyo and Osaka. Against plain unidirectional
Dijkstra, the ratios were larger: 8.64x and 6.59x on distance in Tokyo and Osaka;
the tables report both baselines so the two are not confused. On London and New
York we also measured a variant that keeps the potential and the stopping rule
but removes the scheduler, and it is 1.11x and 1.21x slower, which is the direct
evidence that the scheduler and not something else is doing the work.
The advantage shrinks as the geographic lower bound carries less information, and
on a synthetic uniform lattice it disappears entirely. We report that reversal
rather than omit it.

![ACBS search structure](figures/acbs-search.png)

## 1. Scope and what is new

This note reports measurements rather than a new algorithm. Two concepts must be
distinguished.

**Not new: the termination rule.** ACBS terminates when the sum of the two
frontier minimum priority keys reaches the incumbent cost, with both quantities
expressed in reduced units.
Section 4 shows that this is a standard reduced-cost bidirectional rule and that
it is *not* the g-bound, contrary to what an earlier draft of this note asserted.
Siag et al. (2023) [25] enumerate seventeen non-dominated global bounds and prove
that the maximum over available bounds is the tightest single condition. ACBS does not
evaluate any of them directly, because its heaps are ordered by reduced key. An
earlier internal note described the rule as a contribution. It is not.

**The candidate contribution: the scheduler.** Given a fixed potential and a
fixed stopping rule, an exact bidirectional search still has freedom in how much
edge work to give each frontier before reconsidering. ACBS measures, online, how
much the coupled lower bound has risen per unit of work in each direction, and
allocates the next chunk accordingly. The ablation that removes only the
scheduler, keeping the potential and the stopping rule identical, is available
as `aegis-static`.

How the scheduler relates to prior work is now well-delineated rather than
open. Section 3.6 maps ACBS onto the three-decision framework of Siag et al.
(2023): the scheduler is an instance of their open direction hook, at a coarser
granularity and with a work-normalized score, and its side-selection components
are coarse-grained variants of Pohl's cardinality and alternating policies.
Section 6.6 ablates the scheduler's components on real data: the productive part
is the efficiency-based direction choice, while the tuning knobs (dead band,
EMA, budget multipliers) show no consistent effect across seeds.

## 2. The search

For a source s, a target t, and a node v, the following hold.

- `g_F(v)` is the best known cost from s to v, and `g_B(v)` is the best known
  cost from v to t.
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

## 3. The scheduler

This is the only component this note offers as a contribution, so it is
specified here in enough detail to reimplement. Section 4 then shows that the
stopping rule, by contrast, is standard.

The search proceeds in **chunks**. At the start of each chunk, the scheduler chooses a direction and a budget,
expands nodes up to that budget, and then updates its estimate of how
productive each direction has been.

### 3.1 State

Two quantities are kept per direction, both integer scalars rather than arrays.

- `score_D`, an exponentially weighted estimate of how much the coupled lower
  bound rose per unit of work in direction D, scaled by 10^6.
- `sampled_D`, false until direction D has completed at least one chunk.

In addition, the scheduler maintains `last`, the direction of the previous
chunk, and `consecutive`, the number
of chunks taken in that same direction.

### 3.2 Work and gain

After a chunk ends, let `dLB` be the increase in the coupled lower bound
`min_F k_F + min_B k_B` across the chunk, and let

```
work = relaxedEdges + 4 * expandedNodes + 2 * queueGrowth
```

where `queueGrowth` is the change in the sum of the two queue lengths, counted
only when positive. Expanding a node is charged as four relaxations, and growing the frontier is
charged as two, so the measure reflects allocation and memory traffic rather
than only the relaxation count. The instantaneous score is

```
instant = 10^6 * dLB / work        (work is never 0)
```

The running estimate is an exponential moving average with weight 1/4.

```
score_D = score_D - score_D/4 + instant/4
```

### 3.3 Choosing the direction

The rules apply in priority order.

1. If `sampled_F` is false, choose F. If `sampled_B` is false, choose B. The
   two directions are warmed up before their scores are compared.
2. If `consecutive >= 6`, choose the opposite of `last`. Alternating
   periodically stops the estimate of the idle direction from going stale.
3. If `score_F > 1.1 * score_B`, choose F. If `score_B > 1.1 * score_F`,
   choose B. The 10 percent margin is a dead band, so a small difference in the
   estimates does not flip the direction every chunk.
4. Otherwise, the estimates are within the dead band. Break ties by choosing the
   smaller frontier minimum key, then the smaller degree (out-degree forward,
   in-degree reverse), then the shorter queue, and finally F.

Only the rule 4 tie-break order is shared with the static ablation. The static
ablation skips rules 1 to 3 entirely and always follows the rule 4 order; the
adaptive scheduler is the part that consults the efficiency estimates.

### 3.4 Choosing the budget

```
base = 256 edges    if the graph has  < 10,000 edges
     = 1024 edges   if it has between 10,000 and 100,000
     = 2048 edges   otherwise

budget = base
if score_other > 0 and score_chosen >= 2 * score_other: budget = 2 * base
if score_other > 0 and score_chosen >= 4 * score_other: budget = 4 * base
if an upper bound has been found and budget > 2 * base: budget = 2 * base
```

The budget is measured in edge work, not node count. A direction measured at
twice the efficiency of the other receives twice the work, and one measured at
four times receives four times as much, before the scheduler reconsiders. Once a complete path is known, the multiplier is capped
at two, because the remaining job is to raise the lower bound far enough to
prove that path optimal, and alternating helps with that more than concentrating
work does.

### 3.5 The static ablation

`aegis-static` is identical except that it always uses `base` as the budget and
skips rules 1 to 3 (warmup, alternation, and the dead-band comparison), choosing
directly by the tie-break order in rule 4. The comparison against production
ACBS is introduced in Section 1 and measured in Section 6.6 alongside the
component ablations. Note the units: the chunk budget is counted in edge
relaxations (the loop stops when `relaxedEdges >= budget`), not against the
composite work measure of Section 3.2.

### 3.6 Where the scheduler sits in the Siag et al. (2023) framework

Siag et al. (2023) define a framework in which "there are three decisions that
define BiHS algorithms: (1) deciding when to terminate the search, (2) choosing
which node to expand from a given direction, and (3) choosing the direction from
which the next node will be expanded", with Algorithm 1 structured as
`D <- ChooseDirection(p)`, `n <- ChooseNode(D)`, `Expand(n, D)`,
`LB <- ComputeLowerBound(p)`. Their direction hook is decided **per node
expansion**.

ACBS maps onto the three decisions as follows.

- (1) Termination: the reduced-key test of Section 4. Standard, not from their
  bound enumeration.
- (2) Node choice: minimum key within the direction. Standard label setting.
- (3) Direction and budget: the scheduler of Sections 3.1 to 3.4.

The scheduler differs from the published framework in three structural respects,
so it is worth being precise about which parts are new and which are not.

- **Decision granularity.** The direction is decided once per chunk of roughly
  1,024 edge relaxations rather than once per node. With label setting, a chunk
  is equivalent to a run of identical per-node direction choices, so chunking
  restricts rather than extends the reachable schedules. Its role is to give the
  efficiency estimate something measurable (a batch of work to score).
- **Work-normalized measurement.** The efficiency score divides lower-bound gain
  by `relaxed + 4 x expanded + 2 x queueGrowth`. Siag et al.'s policies aim to increase
  a chosen bound without normalizing by the work consumed; their Section 4.2
  justifies node choice heuristically ("expanding nodes that will cause Bi to
  increase as early as possible would likely result in earlier termination").
- **Closed-loop allocation.** The measured score feeds back into the next chunk's
  budget through the 2x and 4x multipliers, with an EMA, a dead band, and forced
  alternation. Nothing of this kind appears in their framework.

Side selection itself is prior art: Pohl's [4][5] cardinality criterion and
alternating policies are evaluated in their framework for BAE* [20] (their
Section 5), and ACBS's forced alternation and shorter-queue tie-break are
coarse-grained variants of those two. Because the direction hook in their Algorithm 1 is open, any policy
can be expressed inside it. **The claim this note can support is therefore
scoped: ACBS contributes a concrete, fully specified scheduler design in the
space their framework leaves open, evaluated on real road networks - not a new
algorithm class.** Whether the design choices are good is measured by the
component ablation in Section 6.6, and the answer is humbling.

## 4. What the termination rule is, and what it is not

The implementation stops when the sum of the two frontier minimum keys reaches
the incumbent, both expressed in reduced units:

```
min_{u in Open_F} k_F(u) + min_{w in Open_B} k_B(w) >= 2U + phi2(t) - phi2(s)
```

(`internal/search/acbs.go`). Substituting `phi2(v) = h_F(v) - h_B(v)` and writing
u and w for the two frontier minima gives, exactly,

```
k_F(u) + k_B(w) = 2(g_F(u) + g_B(w)) + phi2(t) - phi2(s) + phi2(u) - phi2(w)
```

A node's bound to itself is zero, so `h_F(t) = 0` and `h_B(s) = 0`, and the
frontier-independent part is `phi2(t) - phi2(s) = -(h_B(t) + h_F(s))`, generally
not zero. The cross term `phi2(u) - phi2(w)` does not cancel either: u and w are
distinct nodes, and their potentials would coincide only by numerical accident.
An earlier draft of this section asserted that the potentials cancel once the
frontiers have met; that was wrong. Meeting the frontiers only guarantees that an
incumbent complete path exists, and the frontier minima keep advancing on
different nodes.

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

This distinction matters for two reasons. It removes the claim that ACBS reuses a
known bound, which is a claim we cannot support; and it explains the failure
recorded in Section 6.5, where deriving the f-bound and the b-bound from
frontier nodes and converting them into reduced units produced values above the
true optimum.

Two consequences follow, and both shape what this note can claim.

First, the stopping rule used here is a standard reduced-cost bidirectional rule,
not a contribution. Whatever advantage the measurements show therefore comes from
the order in which work is performed, not from the test that ends the search.

Second, Siag et al. observe that taking the maximum over several available
bounds gives the tightest condition available. ACBS consults only one. Section
6.5 reports what happened when we tested the obvious follow-up.

## 5. Method

All comparisons use the same graph object, the same query set, the same warmup,
and an interleaved measurement order, with no preprocessing for any algorithm.
Steady-state allocation is zero bytes per query for every algorithm. The Tokyo
distance run uses 400 queries; every other run in this note uses 300. Results
are reported as the median over repeated measurements and as a paired
comparison of the same queries, because the two disagree on some graphs. A
median here is the nearest-rank 50th percentile over the query set: with an
even number of queries, the lower of the two middle values. Every table in this
note uses that convention, and one earlier draft mixed it with an interpolated
median, which is recorded in Section 6.3.

**Data.** OpenStreetMap extracts from Geofabrik, restricted to a box around the
city centre and to ways that carry vehicle traffic, imported with the `car`
profile. The edge cost is `ceil(meters * 1000)` for the distance metric and
`ceil(meters / speed * 1000)` for the travel-time metric.

| Graph | Nodes | Directed edges | heuristicStrength (distance / time) |
|---|---:|---:|---:|
| Tokyo | 1,098,173 | 2,262,568 | 0.9999 / 0.2505 |
| Osaka | 748,903 | 1,575,615 | 1.0000 / 0.2786 |
| London | 761,244 | 1,449,256 | 0.9999 / - |
| New York | 353,434 | 590,876 | 0.9999 / - |
| Synthetic lattice | 1,000,000 | 2,596,627 | 0.9955 |

London and New York were measured on the distance metric only. They were added
after the first release, with the same pipeline, bounding-box half-width
(0.18 degrees), seed and interleaving as Tokyo and Osaka, so the four cities are
directly comparable. Their `aegis-static` rows are what allow the scheduler to
be separated from the rest of the search on a network other than Tokyo.

`heuristicStrength` is the ratio of the minimum to the mean cost per metre. In
mathematics a bound is *saturated* when equality holds, which is the opposite of
what happens here, so the term is avoided. A travel-time metric makes the
geographic bound much **looser**: to stay admissible the bound may only divide
straight-line distance by the fastest speed anywhere (the smallest cost per
metre), while most roads are slower. That loses pruning
power, which is why travel time is reported separately.

## 6. Results

### 6.1 Distance metric

| Graph | Algorithm | Median | p95 | vs unidirectional Dijkstra | Expanded | Correct |
|---|---|---:|---:|---:|---:|---:|
| Tokyo | Dijkstra | 72.35 ms | 156.88 ms | 1.00x | 531,031 | 400/400 |
| Tokyo | bidirectional Dijkstra | 50.60 ms | 125.64 ms | 1.43x | 368,789 | 400/400 |
| Tokyo | A* | 16.95 ms | 104.08 ms | 4.27x | 70,630 | 400/400 |
| Tokyo | ACBS | **8.38 ms** | **46.77 ms** | **8.64x** | 44,201 | 400/400 |
| Osaka | Dijkstra | 50.65 ms | 119.15 ms | 1.00x | 373,981 | 300/300 |
| Osaka | bidirectional Dijkstra | 33.71 ms | 87.97 ms | 1.50x | 234,918 | 300/300 |
| Osaka | A* | 13.70 ms | 87.83 ms | 3.70x | 57,780 | 300/300 |
| Osaka | ACBS | **7.69 ms** | **48.52 ms** | **6.59x** | 34,137 | 300/300 |
| London | Dijkstra | 53.23 ms | 95.99 ms | 1.00x | 416,127 | 300/300 |
| London | bidirectional Dijkstra | 29.44 ms | 75.62 ms | 1.81x | 230,592 | 300/300 |
| London | A* | 13.84 ms | 74.07 ms | 3.85x | 63,902 | 300/300 |
| London | ACBS | **6.87 ms** | **30.17 ms** | **7.75x** | 38,221 | 300/300 |
| London | aegis-static | 7.60 ms | 33.08 ms | 7.00x | 41,892 | 300/300 |
| New York | Dijkstra | 17.41 ms | 36.51 ms | 1.00x | 170,376 | 300/300 |
| New York | bidirectional Dijkstra | 12.56 ms | 31.70 ms | 1.39x | 130,293 | 300/300 |
| New York | A* | 7.36 ms | 39.74 ms | 2.37x | 37,649 | 300/300 |
| New York | ACBS | **3.94 ms** | **30.23 ms** | **4.43x** | 27,114 | 300/300 |
| New York | aegis-static | 4.76 ms | 28.42 ms | 3.66x | 32,188 | 300/300 |

Paired against bidirectional Dijkstra, ACBS finished ahead on 390 of 400 Tokyo
queries and 288 of 300 Osaka queries.

The last two graphs were added after the first release, to test whether the
result depends on Japan. London and New York were built by the same pipeline
from Geofabrik extracts with the same bounding-box half-width, and measured with
the same harness, query count, seed and interleaving. The ordering is unchanged:
ACBS is fastest on both, and on both it also beats the static ablation that
keeps everything except the scheduler. The margins differ - 4.29x against
bidirectional Dijkstra in London, 3.19x in New York, against 6.04x in Tokyo -
so the effect is real across continents but not uniform across cities, which is
the honest reading of four networks rather than two.

New York and London additionally report `aegis-static`, which the earlier Tokyo
and Osaka runs in this table do not. That row is the direct test of the
scheduler: with the potential and the stopping rule held fixed, removing only
the adaptive rule costs 1.11x in London and 1.21x in New York, and raises the
median expanded count by 9 and 16 percent respectively. It is the same
conclusion Section 6.6 reaches on Tokyo by component ablation, on different
networks and a different continent.

### 6.2 Travel-time metric

| Graph | Algorithm | Median | p95 | vs unidirectional Dijkstra | Expanded | Correct |
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

### 6.3 Where the advantage comes from

A speedup can come from expanding fewer nodes, or from doing cheaper work per
node. The two matter differently to anyone trying to reproduce or extend the
result, so they are separated here.

The measurements below are on the Tokyo distance graph, against bidirectional
Dijkstra, over the same 300 queries.

| Algorithm | Median time | Median expanded | Time per expanded node |
|---|---:|---:|---:|
| bidirectional Dijkstra | 61.45 ms | 411,246 | 149.4 ns |
| ACBS | 9.76 ms | 44,726 | 218.2 ns |

The total speedup is 6.30x, and it factors into two terms.

```
6.30x = 9.19x (fewer expansions) * 0.68x (higher per-node cost)
```

**ACBS buys its speed by expanding nine times fewer nodes, and it pays for that
with 46 percent more work per node.** Bidirectional Dijkstra expands nine times
as many nodes, but each one is cheaper to process.

Per-node cost is the median duration divided by the median expanded count, so it
mixes the cost of a pop, the cost of the edges scanned out of that node, and the
fixed setup of a query. ACBS does more per node than a plain Dijkstra loop: it
evaluates the potential at each relaxed neighbour, maintains an efficiency score
per direction, and decides direction and budget at chunk boundaries. None of
that appears in a bidirectional Dijkstra inner loop. The numbers say that
overhead is real and measurable, and that it is still worth paying.

This decomposition also explains why ACBS does poorly on a uniform lattice. On a
lattice the expansion advantage largely evaporates, so the per-node penalty stays
while the benefit it was paying for does not.

The distinction is not new to the field. Wang et al. (2025) [27], following Siag
et al. (2023) [25], recommend BAE* [20] as the default for the consistent-heuristic case on the
grounds that it strikes "a good balance between node expansions and runtime
across different domains and heuristics". Treating the two as separate quantities
is therefore the convention, and this section reports them separately for that
reason.

#### By query class

The advantage is not uniform across query lengths. It is weakest on the shortest
queries.

| Query class | n | ACBS median | bidirectional Dijkstra | Speedup | ACBS faster on |
|---|---:|---:|---:|---:|---:|
| local | 100 | 0.19 ms | 0.51 ms | 2.69x | 93% |
| random | 100 | 9.76 ms | 61.45 ms | 6.30x | 100% |
| regional | 100 | 34.66 ms | 116.63 ms | 3.36x | 100% |

Local queries gain least because at that scale the fixed cost of setting up the
search is a large share of the total, and there is little room for a better
expansion order to show up in the timings. Regional queries gain less than random ones even though
they are longer: both frontiers sweep a wider area, and the asymmetry the
scheduler exploits is less pronounced.

The 93 percent figure on local queries is the honest headline for that class.
Seven queries out of a hundred do not beat bidirectional Dijkstra at all.

A reader comparing this section with Table 6.1 will notice 6.30x here and 6.04x
there, for what looks like the same comparison. They are different runs: Table
6.1 is a 400-query run on this graph, this section is a 300-query rerun, and the
query samples differ between them even at the same seed. Two honest medians of
the same algorithm on the same graph can differ by several percent because of
which queries were drawn, which is exactly why Section 6.3 reports paired
win counts alongside medians.

Medians follow the Section 5 convention. An interpolated median would give
9.77 ms rather than 9.76 ms for ACBS. An earlier draft of this section mixed the
two conventions and also dropped duplicate query pairs, which produced 6.31x and
a per-node penalty of 43 percent; both figures were wrong in proportion, not in
direction.

### 6.4 Where the advantage goes away

The gain depends on the metric. To isolate that dependence, a 1,000,000-node
uniform lattice was re-weighted so that the same topology carries progressively
less geographic information.

| heuristicStrength | Time ratio, ACBS / bidirectional Dijkstra |
|---:|---:|
| 0.996 | 1.001 (tied) |
| 0.434 | 0.926 |
| 0.311 | 0.991 |
| 0.264 | 1.002 |
| 0.202 | 1.099 |
| 0.096 | 1.203 |

In this table the ratio is ACBS time divided by bidirectional Dijkstra time, so
values above 1.0 mean ACBS is slower - the inverse of the speedup columns in
Tables 6.1 to 6.3. On the lattice the advantage is absent even at high heuristic
strength and becomes a clear loss as the bound weakens. A uniform lattice rewards both
frontiers about equally, so there is little asymmetry for a scheduler to exploit.
Real road networks are irregular, and there the same scheduler keeps an
advantage even when the bound is weak.

This reversal is not unique to ACBS. Holte et al. (2017) [11] note that as heuristic
accuracy changes, the advantage shifts between algorithms, and Shperberg et al.
(2020) [19] formalise the related property of well-behavedness. Wang et al. (2025)
[27] report the same direction in their own experiments, with their MEET algorithm
roughly on par with A* on one grid map and about twice as slow on another where
the heuristics are less informative.

### 6.5 Do more bounds help?

Sections 6.1 through 6.3 measure the speed of ACBS but say nothing about whether
its stopping condition can be tightened. This section reports an attempt to do
that, and why it did not produce the answer we wanted.

The intent was to terminate on the maximum of several admissible global bounds,
following Siag et al. (2023) [25], who show that the maximum over the bounds an
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

| Algorithm | Median | vs unidirectional Dijkstra | Expanded | Relaxed | Correct |
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

### 6.6 Which scheduler component matters

Section 3 lists four scheduler components beyond the tie-break order: the 10
percent dead band, the forced alternation, the EMA smoothing, and the budget
multipliers. Section 6.5 showed that a wrong stopping condition can masquerade as
a speedup; the scheduler ablations are the complementary check, because the
scheduler never touches correctness and any measured difference is a real
scheduling effect.

Four variants were built, each removing exactly one component while keeping the
search, the potential, and the stopping rule identical. Both runs are on the
Tokyo distance graph, 300 queries, 5 repeats, interleaved, all correct 300/300.

| Algorithm | seed 1010 median | seed 7 median |
|---|---:|---:|
| ACBS (production) | 13.23 ms | 13.87 ms |
| aegis-static (no adaptive scheduling) | 14.85 ms | 17.07 ms |
| aegis-nodeadband | 12.37 ms | 14.31 ms |
| aegis-noalternation | 13.22 ms | 14.52 ms |
| aegis-instant | 12.32 ms | 13.95 ms |
| aegis-nomultiplier | 12.65 ms | 14.30 ms |

Two findings, of opposite sign and different stability.

**The stable one: adaptive direction scheduling helps.** Production ACBS is
faster than the static ablation on both seeds, by 10.9 and 18.7 percent. The
static ablation keeps everything but the scheduler, so closing the loop -
measuring each direction's efficiency and steering work accordingly - is the
productive component. That gain is also visible in the work counters: on seed
1010 the static ablation expands 54,777 nodes against ACBS's 44,726.

**The humbling one: the tuning components do not help.** Removing the dead band
was 6.5 percent faster than production on seed 1010 and 3.1 percent slower on
seed 7. Removing the EMA and removing the multipliers flip sign the same way.
The production tuning (the parts of the scheduler that most distinguish it on
paper) has no consistent effect across the two seeds, and the observed spread
between the adaptive variants (about 8 percent) is within query-sampling noise.

The corollary for the release is documented rather than implemented. Dropping
the dead band and the multipliers leaves performance comparable: neither variant
dominates production, since both were slower on seed 7, and a simpler scheduler
might be worth keeping for its own sake. Changing the default search requires
passing the release gates, so both variants stay in the code and the question is
left open.

Final caveat: two seeds on one graph is thin evidence for "no effect". The
honest statement is that no consistent effect was detected, not that none
exists.

## 7. Limitations

- Two cities, one machine in the original evaluation. Section 7.1 records how
  far the result extends to other cities, and where the scheduler stops acting
  at all.
- The stopping rule is known; the scheduler is the only candidate contribution
  and its distinctness from prior expansion policies is unresolved.
- Turn restrictions are not modelled. The Tokyo box contains 2,089 OSM
  restrictions, 0.092 percent of directed edges. They are few, sparse, and
  neutral with respect to algorithm choice, so they are unlikely to change the
  ranking, but absolute distances here are optimistic.
  These graphs are a performance test bed, not a validation of routing
  correctness.
- Only one stopping condition is consulted. Section 6.5 records that both
  attempts to widen it were unsound and were reverted or reduced, so whether a
  sound multi-bound condition would help is unknown.
- The scheduler is specified in Section 3 and ablated component-by-component
  in Section 6.6, but only against itself. The side-selection policies evaluated
  by Siag et al. (cardinality, alternating) and other scheduling policies remain
  untested at this granularity.
- Travel-time results use a speed model derived from `maxspeed` tagging, not
  observed traffic.
- Timings are medians over repeated interleaved runs on one machine. The
  per-node cost in Section 6.3 mixes pop cost, edge scanning and per-query
  setup, so it is an aggregate rather than a profile of where the time goes.
- The query classes are defined by the harness, and the class boundaries are not
  varied here. The weakness on local queries (93 percent win rate) is not
  investigated further.
- The scheduler's tuning components (dead band, EMA smoothing, budget
  multipliers) showed no consistent effect across two seeds (Section 6.6). Two
  seeds cannot distinguish "no effect" from "effect too small to detect", and
  the shipped default retains them pending a formally gated evaluation.

### 7.1 Where the scheduler does nothing

The scheduler is not always active. On some graphs the search finishes in a
single chunk, in which case the scheduler makes exactly one decision and its
policies never come into play. On the Hatfield road network as reconstructed
here, the median number of chunks per query is one and the direction never
switches, so that graph cannot support any claim about scheduling.

The same ceiling appears on the synthetic lattice. On a 10,000-node uniform
lattice the median expanded count is identical (1,392) for production ACBS and
for the static ablation, so the 3 percent aggregate difference over 200 queries
is not evidence that the adaptive rule helps there. The lattice is reported in
Section 6.4 for the opposite purpose, showing where the *geographic bound* stops
carrying information; it is not a test of the scheduler.

The scheduler's gain is therefore established on irregular road networks with a
strong geographic bound (Sections 6.1, 6.6), and the honest reading is that the
mechanism depends on the two frontiers having measurably different productivity.
A theoretical treatment of when that holds is in [THEORY.md](THEORY.md); the
main result there is negative, in that the natural objective - maximise the rate
of rise of the coupled lower bound - fails to define an optimal expansion order
once an upper bound exists.

## 8. Reproducing

The scheduler constants in Section 3 are those of `internal/search/acbs.go` at
the commit this document describes. Anyone reimplementing it should check the
following, which the released code satisfies.

| Spec | Where it lives |
|---|---|
| `work = relaxed + 4*expanded + 2*queueGrowth` | `schedulerWork` |
| `score = score - score/4 + instant/4` | `emaScore` |
| 10 percent dead band before a score decides direction | `chooseACBSDirection` |
| periodic alternation at `consecutive >= 6` | `chooseACBSDirection` |
| base budget 256 / 1024 / 2048 by edge count | `acbsBaseEdgeBudget` |
| 2x and 4x budget multipliers, capped at 2x once an upper bound exists | `acbsEdgeBudget` |

The CLI names carry over from the implementation: `aegis` is production ACBS,
and `aegis-static` is the static ablation. The static variant uses the same
search with `adaptive` false, which forces the base budget and the
tie-break-only direction rule. Comparing `aegis` against `aegis-static`
therefore isolates the scheduler and nothing else.

The repository is <https://github.com/sahenjp/aegis-acbs>. Synthetic graphs come
from `scripts/gen`. The OSM graphs are not distributed because of size; they are
rebuilt from the Geofabrik Kanto and Kansai extracts with a bounding box around
each city centre, then imported with `import-osm`. Full per-experiment records,
including the defects found during development and how each was resolved, are in
`docs/CANDIDATES.md`.

## References

Entries are numbered for citation. The body cites by author name and year; the
number in brackets is the entry here. Every DOI and arXiv identifier in this
list was checked against Crossref or the arXiv API while it was added.

### Foundational shortest-path search

[1] Dijkstra, E. W. A Note on Two Problems in Connexion with Graphs.
*Numerische Mathematik* 1, 269-271, 1959. DOI: `10.1007/BF01386390`.

[2] Hart, P. E., Nilsson, N. J., Raphael, B. A Formal Basis for the Heuristic
Determination of Minimum Cost Paths. *IEEE Transactions on Systems Science and
Cybernetics* 4(2), 100-107, 1968. DOI: `10.1109/TSSC.1968.300136`.

[3] Nicholson, T. A. J. Finding the Shortest Route between Two Points in a
Network. *The Computer Journal* 9(3), 275-280, 1966.
DOI: `10.1093/comjnl/9.3.275`.

[4] Pohl, I. Bi-directional Search. In B. Meltzer and D. Michie (eds.),
*Machine Intelligence* 6, 127-140. Edinburgh University Press, 1971.

[5] Pohl, I. Bi-Directional and Heuristic Search in Path Problems. PhD
dissertation, Stanford University, 1969. SLAC report; DOI:
`10.2172/4785039`. Introduces the cardinality-comparison criterion that
Section 3.6 refers to as a side-selection policy.

### Bidirectional heuristic search: early formulations

[6] Sint, L., de Champeaux, D. An Improved Bidirectional Heuristic Search
Algorithm. *Journal of the ACM* 24(2), 177-191, 1977.
DOI: `10.1145/322003.322004`.

[7] de Champeaux, D. Bidirectional Heuristic Search Again. *Journal of the
ACM* 30(1), 22-32, 1983. DOI: `10.1145/322358.322360`.

[8] Kwa, J. B. H. BS*: An Admissible Bidirectional Staged Heuristic Search
Algorithm. *Artificial Intelligence* 38(1), 95-109, 1989.
DOI: `10.1016/0004-3702(89)90069-6`.

[9] Kaindl, H., Kainz, G. Bidirectional Heuristic Search Reconsidered.
*Journal of Artificial Intelligence Research* 7, 283-317, 1997.
DOI: `10.1613/jair.460`. arXiv: `cs/9712102`.

### Bounds, expansions and termination conditions

[10] Holte, R. C., Felner, A., Sharon, G., Sturtevant, N. R. Bidirectional
Search That Is Guaranteed to Meet in the Middle. In *Proceedings of the 30th
AAAI Conference on Artificial Intelligence*, 3411-3417, 2016.
DOI: `10.1609/aaai.v30i1.10436`.

[11] Holte, R. C., Felner, A., Sharon, G., Sturtevant, N. R., Chen, J. MM: A
Bidirectional Search Algorithm That Is Guaranteed to Meet in the Middle.
*Artificial Intelligence* 252, 232-266, 2017.
DOI: `10.1016/j.artint.2017.05.004`.

[12] Chen, J., Holte, R. C., Zilles, S., Sturtevant, N. R. Front-to-End
Bidirectional Heuristic Search with Near-Optimal Node Expansions. In
*Proceedings of the 26th International Joint Conference on Artificial
Intelligence (IJCAI)*, 489-495, 2017. DOI: `10.24963/ijcai.2017/69`.
arXiv: `1703.03868`.

[13] Eckerle, J., Chen, J., Sturtevant, N. R., Zilles, S., Holte, R. C.
Sufficient Conditions for Node Expansion in Bidirectional Heuristic Search.
In *Proceedings of the 27th International Conference on Automated Planning and
Scheduling (ICAPS)*, 79-87, 2017. DOI: `10.1609/icaps.v27i1.13825`.

[14] Shaham, E., Felner, A., Chen, J., Sturtevant, N. R. The Minimal Set of
States That Must Be Expanded in a Front-to-End Bidirectional Search. In
*Proceedings of the 10th International Symposium on Combinatorial Search
(SOCS)*, 82-90, 2017. DOI: `10.1609/socs.v8i1.18426`.

[15] Barker, J. K., Korf, R. E. Limitations of Front-To-End Bidirectional
Heuristic Search. In *Proceedings of the 29th AAAI Conference on Artificial
Intelligence*, 1086-1092, 2015. DOI: `10.1609/aaai.v29i1.9374`.

[16] Shperberg, S. S., Felner, A., Shimony, S. E., Sturtevant, N. R., Hayoun,
A. Improving Bidirectional Heuristic Search by Bounds Propagation. In
*Proceedings of the 12th International Symposium on Combinatorial Search
(SOCS)*, 106-114, 2019. DOI: `10.1609/socs.v10i1.18508`.

[17] Shperberg, S. S., Felner, A., Sturtevant, N. R., Shimony, S. E., Hayoun,
A. Enriching Non-Parametric Bidirectional Search Algorithms. In *Proceedings of
the 33rd AAAI Conference on Artificial Intelligence*, 2379-2386, 2019.
DOI: `10.1609/aaai.v33i01.33012379`.

[18] Shaham, E., Felner, A., Sturtevant, N. R., Rosenschein, J. S. Optimally
Efficient Bidirectional Search. In *Proceedings of the 28th International
Joint Conference on Artificial Intelligence (IJCAI)*, 6221-6225, 2019.
DOI: `10.24963/ijcai.2019/867`.

[19] Shperberg, S. S., Felner, A., Sturtevant, N. R., Shimony, S. E., Hayoun,
A. Bidirectional Heuristic Search: Expanding Nodes by a Lower Bound. In
*Proceedings of the 29th International Joint Conference on Artificial
Intelligence (IJCAI)*, 4775-4779, 2020. DOI: `10.24963/ijcai.2020/664`.

[20] Alcázar, V., Riddle, P. J., Barley, M. A Unifying View on Individual
Bounds and Heuristic Inaccuracies in Bidirectional Search. In *Proceedings of
the 34th AAAI Conference on Artificial Intelligence*, 2327-2334, 2020.
DOI: `10.1609/aaai.v34i03.5611`.

[21] Sewell, E. C., Jacobson, S. H. Dynamically Improved Bounds Bidirectional
Search. *Artificial Intelligence* 291, 103405, 2021.
DOI: `10.1016/j.artint.2020.103405`.

[22] Pavlik, J. A., Sewell, E. C., Jacobson, S. H. Iterative Deepening
Dynamically Improved Bounds Bidirectional Search. *INFORMS Journal on
Computing* 34(2), 974-989, 2022. DOI: `10.1287/ijoc.2021.1116`.

[23] Rice, M., Tsotras, V. J. Bidirectional A* Search with Additive
Approximation Bounds. In *Proceedings of the 3rd International Symposium on
Combinatorial Search (SOCS)*, 80-87, 2021.
DOI: `10.1609/socs.v3i1.18235`.

[24] Hu, K., Speck, D. On Bidirectional Heuristic Search in Classical
Planning: An Analysis of BAE*. In *Proceedings of the 15th International
Symposium on Combinatorial Search (SOCS)*, 91-99, 2022.
DOI: `10.1609/socs.v15i1.21756`.

[25] Siag, L., Shperberg, S. S., Felner, A., Sturtevant, N. R. Front-to-End
Bidirectional Heuristic Search with Consistent Heuristics: Enumerating and
Evaluating Algorithms and Bounds. In *Proceedings of the 32nd International
Joint Conference on Artificial Intelligence (IJCAI)*, 5631-5638, 2023.
DOI: `10.24963/ijcai.2023/625`.

[26] Siag, L., Shperberg, S. S., Felner, A., Sturtevant, N. R. Comparing
Front-to-Front and Front-to-End Heuristics in Bidirectional Search. In
*Proceedings of the 16th International Symposium on Combinatorial Search
(SOCS)*, 158-162, 2023. DOI: `10.1609/socs.v16i1.27296`.

[27] Wang, Y., Weiss, E., Mu, B., Salzman, O. Bidirectional Search while
Ensuring Meet-In-The-Middle via Effective and Efficient-to-Compute Termination
Conditions. In *Proceedings of the 34th International Joint Conference on
Artificial Intelligence (IJCAI)*, 8987-8995, 2025.
DOI: `10.24963/ijcai.2025/999`.

[28] Siag, L., Shperberg, S. S. Bridging Theory and Practice in Bidirectional
Heuristic Search with Front-to-End Consistent Heuristics. *Artificial
Intelligence* 348, 104420, 2025. DOI: `10.1016/j.artint.2025.104420`.

[29] Shperberg, S. S., Morad, N., Siag, L., Felner, A., Atzmon, D.
Bidirectional Bounded-Suboptimal Heuristic Search with Consistent Heuristics.
Extended abstract in *Proceedings of the 18th International Symposium on
Combinatorial Search (SOCS)*, 265-266, 2025.
DOI: `10.1609/socs.v18i1.36011`. Full preprint: arXiv: `2511.10272`.

### Surveys and follow-on results

[30] Sturtevant, N. R., Felner, A. A Brief History and Recent Achievements in
Bidirectional Search. In *Proceedings of the 32nd AAAI Conference on
Artificial Intelligence*, 8000-8006, 2018.
DOI: `10.1609/aaai.v32i1.12218`.

[31] Sturtevant, N. R., Shperberg, S. S., Felner, A., Chen, J. Predicting the
Effectiveness of Bidirectional Heuristic Search. In *Proceedings of the 30th
International Conference on Automated Planning and Scheduling (ICAPS)*,
281-290, 2020. DOI: `10.1609/icaps.v30i1.6672`.

[32] Siag, L., Shperberg, S. S., Felner, A., Sturtevant, N. R. On Parallel
External-Memory Bidirectional Search. In *Proceedings of the 17th
International Symposium on Combinatorial Search (SOCS)*, 2024.
DOI: `10.3233/FAIA240991`. arXiv: `2412.21104`.

[33] Wang, Y., Mu, B., Shokouhi, S., Thein, M.-W. Optimal Kinodynamic Motion
Planning Through Anytime Bidirectional Heuristic Search with Tight Termination
Condition. arXiv: `2604.11587`, 2026. Applies the tight termination
condition of [27] to sampling-based motion planning.

[34] Shubi, T., Felner, A., Shimony, S. E., Shperberg, S. S. Bidirectional
Search for Longest Paths: Case for Front-to-Front Heuristics.
arXiv: `2606.05956`, 2026.

### Road-network routing and speedup techniques

[35] Goldberg, A. V., Harrelson, C. Computing the Shortest Path: A* Search
Meets Graph Theory. In *Proceedings of the 16th Annual ACM-SIAM Symposium on
Discrete Algorithms (SODA)*, 156-165, 2005.

[36] Geisberger, R., Sanders, P., Schultes, D., Delling, D. Contraction
Hierarchies: Faster and Simpler Hierarchical Routing in Road Networks. In
*Experimental Algorithms (WEA 2008)*, LNCS 5038, 319-333, 2008.
DOI: `10.1007/978-3-540-68552-4_24`.

[37] Geisberger, R., Sanders, P., Schultes, D., Vetter, C. Exact Routing in
Large Road Networks Using Contraction Hierarchies. *Transportation Science*
46(3), 388-404, 2012. DOI: `10.1287/trsc.1110.0401`.

[38] Bast, H., Delling, D., Goldberg, A., Müller-Hannemann, M., Pajor, T.,
Sanders, P., Wagner, D., Werneck, R. F. Route Planning in Transportation
Networks. In *Algorithm Engineering*, LNCS 9220, 19-80, 2016.
DOI: `10.1007/978-3-319-49487-6_2`.

[39] Abraham, I., Fiat, A., Goldberg, A. V., Werneck, R. F. Highway
Dimension, Shortest Paths, and Provably Efficient Algorithms. In *Proceedings
of the 21st Annual ACM-SIAM Symposium on Discrete Algorithms (SODA)*,
782-793, 2010. DOI: `10.1137/1.9781611973075.64`.

[40] Felner, A., Moldenhauer, C., Sturtevant, N. R., Schaeffer, J.
Single-Frontier Bidirectional Search. In *Proceedings of the 24th AAAI
Conference on Artificial Intelligence*, 59-64, 2010.
DOI: `10.1609/aaai.v24i1.7555`.

[41] Al-Ayyoub, A. Distributed Unidirectional and Bidirectional Heuristic
Search: Algorithm Design and Empirical Assessment. *The Journal of
Supercomputing* 32, 231-250, 2005. DOI: `10.1007/s11227-005-0165-7`.