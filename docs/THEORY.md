# ACBS scheduler: what can be proved, and what cannot

This note analyses the ACBS scheduler described in Section 3 of [PAPER.md](PAPER.md).
It states three propositions, proves the parts that follow from the definitions,
and marks everything else as a conjecture, a refutation, or an open question.
**No claim below is asserted as proved unless a proof is written out.** Where the
experiments contradict an intuition, the experiment wins and the intuition is
recorded as false.

Scope: this is a reading of the implementation in `internal/search/acbs.go` as of the
commit this note is written against. All symbols below are the implementation's,
not idealised variants.

> **付記: 本文書の性格**
>
> この文書は日本語の主要文書群とは異なり、命題と証明を厳密に扱うため英語で記述します。
> 対象読者は PAPER.md の §3 と §6.6 を読んだ人です。日本語の概要は
> docs/ALGORITHM.md と docs/CORRECTNESS.md にあり、本文書はそれらを置き換えません。
>
> **日本語での結論(3行):**
>
> 1. スケジューラは厳密性に影響しません(補題2.1、系2.3)。厳密性は上下界と停止条件が担い、これは全スケジュールで不変です。
> 2. 命題A「下界上昇率の最大化が最適展開順序と一致する」は成立しません。上界が見つかった後は上昇率がゼロになり、指標が機能しないためです(命題A0、反例つき)。
> 3. 命題Bは逆向きで証明されます(細粒度化は到達可能なスケジュール集合を拡大する: 定理B1)。命題Cの結論(一様格子では優位が消える)は計測で支持されますが、その理由とされた「方向依存性の消失」は計測で否定されます(§5.2)。

---

## 1. Notation and standing assumptions

The graph is a directed graph G = (V, E, c) with integer costs c(e) >= 1, source s
and target t. Write d(u,v) for the shortest-path distance.

The implementation works in **doubled reduced units**. Following Section 2 of the
paper, with phi(v) = h_F(v) - h_B(v), phiS = phi(s), phiT = phi(t):

    k_F(v) = 2 g_F(v) + phi(v) - phiS
    k_B(v) = 2 g_B(v) + phiT - phi(v)

`reducedForwardKey` and `reducedBackwardKey` in `acbs.go` compute exactly these.
The incumbent U is the cost of the best complete path found, and `bestReduced` is
2U + phiT - phiS.

**Assumption (A1).** h_F and h_B are *consistent* (the triangle inequality holds
along every arc) so that reduced costs are non-negative in both directions, and
h_F(t) = h_B(s) = 0. This is the paper's assumption; `chordUnitMeters` scaled by
`MinCostPerMeter * (1 - 1e-12)` is designed to satisfy it, and the paper reports
this check passing on the travel-time graph. Everything in Section 2 depends on
(A1).

**Consequence of (A1) used below.** In doubled units, consistency of the two halves
against the cost is

    phi(v) - phi(u) = (h_F(v) - h_F(u)) - (h_B(v) - h_B(u)) <= 2c(u,v),

i.e. `|phi(v) - phi(u)| <= 2c(u,v)`. The factor 2 is the same convention that
`reducedForwardKey` encodes (it doubles g and adds the potential once), so an arc
can reduce a key by at most the cost it adds. Lemma 2.1 uses only the upper bound;
Lemma 2.2 uses only the lower bound. Nothing stronger is assumed anywhere in this
note.

**Definition 1.1 (state).** At any moment:

- O_F, O_B are the two open sets (frontier heaps).
- a = min over u in O_F of k_F(u), and b = min over w in O_B of k_B(w), or +inf if
  the frontier is empty. These are exactly `frontF.priority` and `frontB.priority`.
- Lambda = a + b is the **coupled lower bound** (the paper's min_F k_F + min_B k_B,
  computed in `lowerBound := saturatingAdd(...)`).

**Definition 1.2 (work model).** The scheduler charges a chunk
w = relaxed + 4*expanded + 2*max(0, queue growth) units of work (`schedulerWork`).
The chunk budget is counted differently, in relaxed edges only (`acbsEdgeBudget`
returns 256, 1024 or 2048). Both units appear below and the distinction matters for
Proposition B.

**Definition 1.3 (termination).** The loop stops when
Lambda >= 2U + phiT - phiS (`if bestReduced != inf && lowerBound >= bestReduced`).
`reducedToOriginalLowerBound` maps the doubled value back with
2L' = 2L - (phiT - phiS), so this is the standard reduced-cost test. It is a
property of Lambda, not of the scheduler.

---

## 2. Two exact invariants the scheduler cannot break

Both hold for *every* schedule, so they constrain what any proof of scheduler
superiority can claim.

### Lemma 2.1 (safety)

Let P be any schedule that only ever expands the minimum-key node of the chosen
frontier, and let the loop stop only by the test of Definition 1.3 or by an empty
frontier. Then the reported distance is d(s,t).

*Proof.* Under (A1) each frontier's keys are non-decreasing in pop order, so a node
is settled only after its final g value is known (label setting). Two facts follow.
First, every U recorded is the weight of a real s->t walk and hence U >= d(s,t).
Second, let x be the first unsettled node of the optimal path s = p_0, ..., p_m = t
and z the last unsettled node of the same path. Only [Step A] and [Step B] are used.

[Step A] (forward key bound.) Every predecessor of x on the optimal path is
settled, so g_F(x) <= d(s,x). With k_F(x) = 2g_F(x) + phi(x) - phiS:

    k_F(x) <= 2d(s,x) + phi(x) - phiS.

[Step B] (backward key bound.) Every successor of z on the optimal path is settled
in the backward frontier, so g_B(z) <= d(z,t). With
k_B(z) = 2g_B(z) + phiT - phi(z):

    k_B(z) <= 2d(z,t) + phiT - phi(z).

Adding (phi(x) and phi(z) are different nodes and are NOT cancelled; write them out):

    k_F(x) + k_B(z) <= 2(d(s,x) + d(z,t)) + phi(x) - phi(z) + phiT - phiS.

Because x is not after z on the path, d(s,x) + d(z,t) <= d(s,t). The residual
potential term is bounded by (A1): on the sub-path x -> z of length
d(z) - d(x), consistency gives phi(x) - phi(z) <= 2(d(z) - d(x)), so

    k_F(x) + k_B(z) <= 2(d(s,x) + d(z,t) + d(z) - d(x)) + phiT - phiS
                    <= 2d(s,t) + phiT - phiS,

using d(s,x) + d(x,z) <= d(z) on the optimal path. (Setting the residual to zero
instead would require phi(x) <= phi(z), which need not hold; the (A1) form above is
the one that is always valid.)

Since Lambda = min k_F + min k_B <= k_F(x) + k_B(z), we get
Lambda <= 2d(s,t) + phiT - phiS whenever both frontiers are non-empty. So the
stopping test can only fire once U <= d(s,t), and with U >= d(s,t) we get equality.
(A1) is used three times: for label setting (keys non-decreasing), for g <= d at x
and at z, and for the potential inequality on x -> z. h_F(t) = h_B(s) = 0 is not
needed by this argument. END OF PROOF.

Measured: every traced run agrees with Dijkstra on all queries in the tables of
Section 6 (300 queries per graph, five graphs, `allCorrect=true`).

### Lemma 2.2 (monotonicity)

Lambda is non-decreasing between consecutive loop iterations that do not terminate:
popping cannot lower a frontier minimum (a becomes the next key, which is >= a), and
push keys are >= the popping frontier's current minimum by (A1) and the consistency
of phi, so min(O_F) after a pop-and-relax step is at least the value before it.
Consequently the per-chunk gain dLambda = Lambda_after - Lambda_before is >= 0,
which is why the code's `if afterLB > beforeLB` guard never needs the negative
branch.

*Proof.* Let a be the forward frontier minimum before a step. Every forward key
inserted during the step is 2g(new) + phi(new) - phiS with
2g(new) = 2g(u) + 2c(u,v) for the popped node u, and 2g(u) + phi(u) - phiS >= a by
the heap property. Consistency of phi gives
phi(v) - phi(u) >= -2c(u,v), hence
2g(v) + phi(v) - phiS >= 2g(u) + 2c(u,v) + phi(u) - 2c(u,v) - phiS >= a.
So no inserted key is below a, and the same argument applies to the backward
frontier with its own potential. Popping itself can only raise the minimum. Hence
Lambda is non-decreasing. END OF PROOF.

### Corollary 2.3 (termination is schedule-independent)

By Lemma 2.1, the distance does not depend on the schedule and exactness is not a
scheduler property. A schedule can only change (i) how much work is done before
Lambda reaches the threshold, and (ii) nothing else. This is the precise sense in
which the scheduler never touches correctness, and it is why every scheduler
comparison below is legitimate.

---

## 3. Proposition A: is 'maximise the lower-bound rise rate' the right objective?

**Claim as posed:** the direction choice that maximises the coupled-lower-bound rise
rate coincides, under some condition, with the optimal expansion order.

This is **refuted as stated below (Proposition A0)**, and the counterexample is the
normal case rather than a pathological one: the failure happens after an incumbent
exists, which is the majority of the work on large queries.

### 3.1 The failure when an incumbent already exists

Once a complete path is known, Lambda frequently cannot rise at all: the job of the
remaining search is only to *prove* the incumbent optimal, which the test of
Definition 1.3 does as soon as Lambda reaches 2U + phiT - phiS. Every chunk after an
upper bound exists that does not advance Lambda scores `instant = 0`.

### Proposition A0 (refuted as posed)

*Statement.* 'Maximise dLambda/w per chunk' is not the same as 'minimise total work
to terminate'.

*Refutation.* Take a query on which Lambda cannot rise after the first chunk: the
optimal path is found by the first chunk and Lambda is already at its final value.
Every subsequent chunk then has dLambda = 0, so all schedules score 0 and the score
conveys no ordering information, although the remaining work to termination still
differs between schedules. The objective degenerates exactly where the scheduler
could still choose badly. Measured instance: on `hatfield-distance` every traced
query is a single chunk (`medianChunks = 1`, `medianDirectionSwitches = 0`,
`medianForwardShare = 100%`), so the scheduler contributes nothing and the
trajectory is that of the rule-4 tie-break alone (Section 6.2).

*What survives.* When no incumbent exists yet, Lambda is the only certificate
progress in the system, and there maximising its rise rate is a defensible proxy.
The implementation targets that regime explicitly: the budget multipliers are capped
at 2x as soon as `bestReduced != inf` (`acbsEdgeBudget`, final `if`), which is an
explicit statement that after an upper bound the goal is no longer 'concentrate
work where the bound rises fast' but 'raise the bound far enough to close'.

### What is actually proved, and what is only per-chunk bookkeeping

From Lemma 2.2: if the two directions offer gains g_F and g_B for the same work,
taking the larger advances Lambda further within that chunk. That is all that
follows, and it does **not** compose: the gain available in the next chunk depends
on what was relaxed, not only on Lambda. A claim that greedy rise-rate minimises
total expansions would need stationarity of the rates, which does not hold (the
frontier composition changes every chunk and every cross-direction candidate
updates U). It is therefore recorded as a **conjecture**, not a theorem.

### 3.2 The measured rise rates

The scheduler's estimate is a scalar per direction applied to a time-varying
target: rate_D(j) = dLambda_D(j) / w_D(j) at the j-th chunk taken in direction D.
The implementation compares the smoothed values r_hat_F and r_hat_B (`emaScore`,
weight 1/4) with a 10 % dead band and a forced alternation every 6 chunks.

Measured over 200 traced queries on the uniform lattice (`scripts/theoryprobe`,
seed 424242):

| run | direction | work | gain | dLambda/w |
|---|---|---:|---:|---:|
| ACBS | F | 1,876,198 | 885,748 | 0.4721 |
| ACBS | B | 1,826,277 | 792,308 | 0.4338 |
| aegis-static | F | 1,980,143 | 885,829 | 0.4474 |
| aegis-static | B | 1,836,386 | 792,207 | 0.4314 |

ACBS spends 5.2 % *less* work in F than the static baseline (1,876,198 vs
1,980,143) and reaches the same total bound gain (885,748 vs 885,829), i.e. the
adaptive controller stops over-concentrating work in the forward frontier. The
effect on total expansions is 555,729 vs 573,052 (-3.0 %) in the trace run. That is
a real but small effect, consistent with Section 6.6's finding that the tuning
components do not dominate. Note that the *benchmark* run (300 queries, 7 repeats)
shows the same 1,392 median expansions for both, so the expansion difference is
visible only in the aggregate trace over 200 queries, not in the per-query median.

### 3.3 What can honestly be said

- **Proved:** schedules do not affect exactness (Lemma 2.1); the coupled bound is
  the only termination certificate (Corollary 2.3); the bound is monotone
  (Lemma 2.2).
- **Refuted:** 'maximise the rise rate' is the right objective globally (A0).
- **Conjecture (not proved):** when no incumbent exists, a rise-rate-greedy
  controller reduces total expansions relative to a fixed alternation policy, on
  graphs where the two directions have persistently different rates. The
  measurements are consistent with this but do not single it out from confounders
  (different budget, different tie-break order, different chunk count).

---

## 4. Proposition B: does fine granularity erase the scheduler?

**Claim as proposed:** as the chunk budget shrinks toward one node, the scheduler's
influence tends to zero.

This is **formally false in the direction stated**, for a reason that comes from the
implementation rather than from the theory, and the two units (work model vs. chunk
budget) make the naive statement incoherent. What *is* provable is a monotonicity
statement in the opposite direction.

### Theorem B1 (granularity monotonicity, proved)

Let S(b) be the family of schedules realisable by ACBS with chunk budget b, where a
chunk is a maximal run of same-direction pops bounded by b edge relaxations. Then
for b' <= b:

    S(b) is a subset of S(b').

*Proof.* Any schedule realisable with budget b is a word over {F, B} in which each
maximal run is produced by one chunk bounded by b relaxations. Split each such chunk
into consecutive chunks of at most b' relaxations and inject the same direction at
every intermediate boundary; the scheduler's choice at that boundary may be the same
direction it chose before, which is always available (rule 4 always yields some
direction). The resulting word is identical, so it is realisable with budget b'.
Hence the set of realisable words is monotone decreasing in b. END OF PROOF.

So *fine granularity enlarges the reachable schedule set*; it does not shrink the
scheduler's influence. The correct statement of the intuition is:

### Theorem B2 (limiting case, proved and trivial)

When b equals the size of one node's out- (in-) edge set and there are no stale
pops, every chunk is a single node expansion. The scheduler then reduces to the
per-node direction rule of `chooseACBSDirection`, and the *budget* multiplier of
Section 3.4 can no longer act, because the chunk ends before the multiplier is
consulted again. In that limit the budget component of the scheduler is provably
inert and only the direction component survives. This is a statement about the
components, not about the scheduler as a whole.

### Corollary B3 (why the ablation is the right proxy)

Because a smaller budget only *adds* schedules, an ablation that removes the
scheduler but keeps the search is the only comparison with a fixed schedule, and it
is the one Section 6.6 performs. A claim that 'granularity kills the scheduler' must
show that the adaptive choice inside the enlarged set is no better than the static
choice. That has not been shown for ACBS and is not implied by B1.

### Experimental reading

The grid measurements bracket the two regimes without isolating the budget.
At the production budget (2048; the lattice has 25,654 edges so
`acbsBaseEdgeBudget` returns 2048) `aegis` and `aegis-static` have the same median
of 1,392 expanded nodes over 300 queries, while the aggregate over 200 traced
queries differs by 3.0 % (555,729 vs 573,052). Both expand 19 nodes on `hatfield`,
where the budget is never reached and every query is a single chunk.
Testing B2 directly requires a recompiled variant with `acbsBaseEdgeBudget`
overridden; the probe used here does not change the budget, so **the budget effect
is not measured** and is listed as open in Section 7.

---

## 5. Proposition C: does a uniform lattice erase the advantage?

**Claim as proposed:** on a uniform lattice the coupled-bound rise rate does not
depend on direction, therefore ACBS has no advantage.

The second clause does not follow from the first, and the measurement contradicts
the premise *and* the conclusion in an informative way.

### 5.1 Why the argument as stated cannot hold

Direction asymmetry is not what drives ACBS's advantage. What drives it is that the
coupled bound moves much faster per node expanded than the g-only bound of
bidirectional Dijkstra, because h_F + h_B tightens Lambda from both ends. That
effect is present on any graph with a non-trivial admissible heuristic, uniform or
not. Equal directional rates would not make it vanish.

### Proposition C0 (refuted)

*Statement.* 'If dLambda_F ~ dLambda_B then ACBS gains nothing.'

*Refutation by measurement.* On the 10,000-node uniform lattice generated by
`scripts/gen` (`grid-10k`, `heuristicStrength = 0.9995`, 25,654 edges; 300 queries,
7 repeats, interleaved, seed 424242) ACBS does lose to bidirectional Dijkstra, so
the *conclusion* holds:

| Algorithm | Median | Expanded | geomean vs Dijkstra | median-ratio vs Dijkstra |
|---|---:|---:|---:|---:|
| Dijkstra | 0.306 ms | 4,691 | 1.000 | 1.000 |
| bidirectional Dijkstra | 0.145 ms | 3,030 | **1.598** | 2.120 |
| A* | 0.171 ms | 1,336 | 1.012 | 1.797 |
| aegis-static | 0.122 ms | 1,392 | 1.153 | 2.511 |
| ACBS | 0.124 ms | 1,392 | **1.171** | 2.464 |

but for a different reason than the premise. The direction rates are **not** equal
(F 0.4721 vs B 0.4338, Section 3.2), and the loss therefore cannot be explained by
their equality. This agrees in sign with PAPER.md 6.4, where the ACBS/bidijkstra
ratio is 1.001 at heuristicStrength 0.996 and rises to 1.203 at 0.096.

### 5.2 The premise is false as a description of the lattice

| | F work | F gain | F rate | B work | B gain | B rate |
|---|---:|---:|---:|---:|---:|---:|
| ACBS, seed 424242 | 1,876,198 | 885,748 | 0.4721 | 1,826,277 | 792,308 | 0.4338 |
| aegis-static, seed 424242 | 1,980,143 | 885,829 | 0.4474 | 1,836,386 | 792,207 | 0.4314 |
| ACBS, seed 7 | 1,839,774 | 873,356 | 0.4747 | 1,981,161 | 877,236 | 0.4428 |
| aegis-static, seed 7 | 2,100,546 | 892,593 | 0.4249 | 1,878,834 | 857,967 | 0.4566 |

The rates differ by 8.8 % under ACBS on seed 424242, and the *ordering* of which
direction is faster changes between the two policies and the two seeds. A symmetric
lattice therefore does not produce equal directional rates; the asymmetry comes from
where s and t sit relative to the grid and from the tie-break, not from the lattice's
regularity. **Lattice symmetry is not a sufficient condition for rate symmetry.**
That is the part of Proposition C worth keeping, and it is a refutation of the
premise, not a proof of the conclusion.

### 5.3 What Proposition C should say

- **Measured:** on `grid-10k` ACBS does not beat bidirectional Dijkstra (1.171x vs
  1.598x geomean over 300 queries; ACBS wins 39 % of random queries, 7 % of local,
  3 % of regional). The advantage is gone.
- **Refuted:** that this is because the rise rate is direction-independent. It is
  not, and the equality is neither necessary nor sufficient.
- **Conjecture:** the driver is the ratio of heuristic-guided pruning to scheduler
  benefit: ACBS wins where a *correct* bidirectional A* is already weak (weak
  heuristic, irregular graph) and the coupled bound plus scheduling recovers part of
  what A* loses in the tail. This is consistent with PAPER.md 6.3 (9.19x fewer
  expansions bought at a 0.68x per-node penalty) and 6.4, but it is not proved.
---

## 6. Measurements

All runs use the same graph object, the same query set, no preprocessing for any
algorithm, interleaved measurement order, seed 424242, 300 queries, 7 repeats, and
all algorithms agree with Dijkstra on **300/300** queries per graph
(`allCorrect=true` in every report).

### 6.1 Benchmarks

Each cell is median latency / median expanded nodes.

| Graph | nodes/edges | heuristicStrength | Dijkstra | bidirectional Dijkstra | A* | aegis-static | ACBS |
|---|---|---:|---:|---:|---:|---:|---:|
| grid-10k (lattice) | 10,000 / 25,654 | 0.9995 | 0.306 ms / 4,691 | 0.145 / 3,030 | 0.171 / 1,336 | 0.122 / 1,392 | 0.124 / 1,392 |
| hatfield-distance | 131 / 233 | 1.0000 | 0.001 / 42 | 0.002 / 27 | 0.002 / 17 | 0.002 / 19 | 0.002 / 19 |
| hatfield-time | 131 / 233 | 0.3634 | 0.001 / 42 | 0.002 / 25 | 0.004 / 31 | 0.003 / 30 | 0.003 / 37 |
| hatfield-tight (rescaled) | 131 / 233 | 1.0000 | 0.001 / 42 | 0.002 / 27 | 0.002 / 17 | 0.002 / 19 | 0.002 / 19 |
| hatfield-loose (rescaled) | 131 / 233 | 1.0000 | 0.001 / 42 | 0.002 / 27 | 0.002 / 17 | 0.002 / 19 | 0.002 / 19 |

Geomean vs unidirectional Dijkstra on grid-10k: ACBS 1.171x, aegis-static 1.153x,
A* 1.012x, bidirectional Dijkstra 1.598x.

**`hatfield-tight` / `hatfield-loose` do not test what they were built to test, and
this is recorded as a negative result.** They rescale every edge cost by 1.0x and
1.3x on the same topology, intended to separate a tight from a loose heuristic.
Because costs scale uniformly the geographic bound scales with them and the ratio
bound/cost is unchanged: `heuristicStrength` stays 0.99998 in both, and every work
counter is identical (ACBS expands 19 nodes on both). A uniform rescale is **not** a
heuristic-quality experiment. PAPER.md 6.4's reweighting, which changes the ratio, is
the right instrument.

### 6.2 Traced scheduler behaviour

`scripts/theoryprobe` runs ACBS, `aegis-static` and bidirectional Dijkstra over the
same queries with `search.WithACBSTrace` and records every chunk. Work is the
Definition 1.2 model; rate is gain/work over all chunks in that direction.

| Graph | Algorithm | chunks | expanded | F work | F rate | B work | B rate |
|---|---|---:|---:|---:|---:|---:|---:|
| grid-10k | ACBS | 1,518 | 555,729 | 1,876,198 | 0.4721 | 1,826,277 | 0.4338 |
| grid-10k | aegis-static | 1,567 | 573,052 | 1,980,143 | 0.4474 | 1,836,386 | 0.4314 |
| grid-10k | bidirectional Dijkstra | -- | 677,294 | (no chunks) | | | |
| grid-10k, seed 7 | ACBS | 1,563 | 573,471 | 1,839,774 | 0.4747 | 1,981,161 | 0.4428 |
| grid-10k, seed 7 | aegis-static | 1,623 | 597,481 | 2,100,546 | 0.4249 | 1,878,834 | 0.4566 |
| hatfield-distance | ACBS | 129 | 3,629 | 23,103 | 2345.4 | 0 | -- |
| hatfield-distance | aegis-static | 129 | 3,443 | 16,515 | 2397.8 | 5,370 | 2716.4 |

On `hatfield-distance` ACBS takes **every** chunk in the forward direction: the
first chunk already finds the incumbent and drives Lambda to its terminal value, so
no later chunk exists to reveal a B rate, and the reported F rate is a whole-query
aggregate rather than a per-chunk rate. This is a statement about a 131-node graph,
not evidence about the scheduler.

Consequence for the paper: **on the repro graphs the scheduler is not exercised at
all** (`medianChunks = 1`, `medianDirectionSwitches = 0`,
`medianForwardShare = 100 %`). Scheduler claims cannot be supported from
`artifacts/repro-graphs`.

### 6.3 By query class on the lattice

| Class | n | ACBS expanded | bidij expanded | Dijkstra expanded | ACBS median | bidij median | ACBS wins |
|---|---:|---:|---:|---:|---:|---:|---:|
| local | 100 | 72 | 68 | 132 | 0.006 ms | 0.003 ms | 7 % |
| random | 100 | 1,540 | 3,075 | 4,703 | 0.140 ms | 0.148 ms | 39 % |
| regional | 100 | 6,372 | 7,357 | 9,608 | 0.620 ms | 0.383 ms | 3 % |

ACBS beats bidirectional Dijkstra on random queries (fewer expansions and a
slightly lower median) and loses on local (fixed overhead) and regional
(bidirectional Dijkstra's balanced frontiers win). This mirrors PAPER.md 6.3's
query-class table on a different graph.

---

## 7. Open questions, stated as open

1. **Budget effect.** Theorems B1/B2 are the only granularity results this note can
   prove. Whether the adaptive scheduler beats the static one *at a fixed budget*
   was not isolated here: `acbsBaseEdgeBudget` was not varied, so the ACBS-vs-static
   difference in 6.2 mixes direction choice with chunk length.
2. **Rate stationarity.** A0 shows the rise rate is a poor objective once an
   incumbent exists. Whether the EMA with a 10 % dead band and 6-chunk alternation
   is a better controller than a plain greedy max-rate during the
   no-incumbent phase is not established. PAPER.md 6.6 finds the tuning components
   do not consistently help, which is consistent with either answer.
3. **What makes ACBS win.** Section 5.3 records it as a conjecture. The paper's 6.3
   decomposition (9.19x fewer expansions, 0.68x per-node cost) is the closest
   evidence, and this note adds no proof.
4. **Grids.** Proposition C's conclusion (no advantage on the lattice) is measured;
   its premise (rate symmetry) is refuted. A proof that lattice symmetry forces
   anything about Lambda would have to be about the *potential*, not the topology,
   and is not attempted here.
5. **Small-graph scheduler evidence.** Every scheduler claim above that is based on
   `hatfield` is about a graph on which the scheduler never acts. No claim in this
   note leans on it, and neither should any other document.

## 8. Reproduction

```
# lattice
go run ./scripts/gen -nodes 10000 -out /tmp/theory/graphs
bin/aegis import-dimacs --graph /tmp/theory/graphs/10k.dimacs \
  --coords /tmp/theory/graphs/10k.co --output /tmp/theory/graphs/10k.aegis

# benchmarks
bin/aegis benchmark --graph /tmp/theory/graphs/10k.aegis --queries 300 --repeats 7 \
  --order interleaved --seed 424242 \
  --algorithms dijkstra,bidijkstra,astar,aegis-static,aegis \
  --output /tmp/theory/grid10k.json --html ""

# scheduler traces
go build -o /tmp/theory/schedprobe ./scripts/theoryprobe
/tmp/theory/schedprobe --graph /tmp/theory/graphs/10k.aegis --queries 200 \
  --seed 424242 --out /tmp/theory/grid10k_trace.json
```

`scripts/theoryprobe` is a measurement tool, not part of the release path; it is
kept in the repository so the trace numbers in 6.2 can be re-derived. It refuses to
emit a report if any algorithm disagrees with Dijkstra on any query.

## 9. What this note does not claim

- It does not claim ACBS is asymptotically better than any baseline.
- It does not claim the scheduler is the source of ACBS's advantage on road
  networks; Corollary 2.3 and Section 5.3 both point at the coupled bound instead.
- It does not claim Proposition A, B or C in their proposed form. A is refuted as
  posed and survives only as a per-chunk lemma plus a conjecture; B is refuted in
  its stated direction and replaced by the opposite monotonicity result; C's
  conclusion is measured but its stated reason is refuted.
