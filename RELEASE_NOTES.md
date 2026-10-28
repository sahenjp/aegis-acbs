# Aegis ACBS v0.2.2

`v0.2.2` completes the paper draft and documents its remaining gaps. **No search
code changed** from v0.2.1; this release is documentation plus one test.

## Scheduler specification (Section 3)

The scheduler, the only candidate contribution, is now specified in full:
per-direction state, the work and gain measure
(`work = relaxed + 4*expanded + 2*queueGrowth`), the exponential moving average,
the direction rules with their 10 percent dead band and periodic alternation,
and the budget schedule with all constants. Every constant was checked against
`internal/search/acbs.go`, and a new test verifies the work formula and the
budget set against 156 real traced scheduler chunks.

## Speedup decomposition (Section 6.3)

Against bidirectional Dijkstra on Tokyo distance, the 6.30x total speedup
factors as 9.19x from expanding fewer nodes times 0.68x from a 46 percent higher
per-node cost. Per-query-class results are new: local queries gain only 2.69x
and ACBS wins 93 percent of them.

An internal audit caught this section computing on a subset that dropped
duplicate query pairs, and mixing two median conventions. The corrected numbers
differ by up to four points in the proportions but not in the direction of any
conclusion. The paper now states which median convention it uses and why Table
6.1 and Section 6.3 quote different ratios for the same comparison.

## What the paper does not claim

The stopping rule is a standard reduced-cost bidirectional rule, not bound B3,
and the scheduler has not been compared against any other policy beyond the
static ablation. Both are stated in the limitations. The v0.2.1 release notes
carry the fuller list of what an independent review corrected.
