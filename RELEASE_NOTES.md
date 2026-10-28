# Aegis ACBS v0.3.0

`v0.3.0` scopes the contribution honestly and measures its own scheduler. The
shipped `aegis` search is unchanged; four new benchmark-only ablation variants
were added.

## What is new

**The scheduler is now specified and ablated.** Section 3 of the paper gives the
full scheduler algorithm (state, work and gain measure, direction rules, budget
schedule) with every constant checked against the code, plus a test verifying
the work formula against 156 real traced chunks. Section 3.6 maps ACBS onto the
three-decision framework of Siag et al. (IJCAI 2023): the scheduler is an
instance of their open direction hook at chunk granularity, with work-normalized
efficiency measurement and closed-loop allocation. Its side-selection pieces are
coarse-grained variants of Pohl's cardinality and alternating policies.

**Component ablation, two seeds, real data.** Four variants each remove exactly
one scheduler component:

| Algorithm | seed 1010 median | seed 7 median |
|---|---:|---:|
| ACBS (production) | 13.23 ms | 13.87 ms |
| aegis-static | 14.85 ms | 17.07 ms |
| aegis-nodeadband | 12.37 ms | 14.31 ms |
| aegis-noalternation | 13.22 ms | 14.52 ms |
| aegis-instant | 12.32 ms | 13.95 ms |
| aegis-nomultiplier | 12.65 ms | 14.30 ms |

All exact (300/300 against Dijkstra on both seeds). Two stable findings: the
adaptive direction choice is the productive component (production beats the
static ablation by 10.9 and 18.7 percent, consistent across seeds), and the
tuning components - dead band, EMA smoothing, budget multipliers - show no
consistent effect across seeds. A corollary is recorded but not acted on: a
lighter scheduler without the dead band and multipliers might dominate the
shipped one; changing the default requires the release gates.

## Scope of the claim

The stopping rule is a standard reduced-cost bidirectional rule. The scheduler
is an instance of the direction hook that Siag et al.'s framework leaves open,
with elements absent from their published framework (chunk granularity,
work-normalized efficiency, closed-loop budget allocation); its side-selection
pieces are coarse-grained variants of known policies. Whether the design choices
are good is answered by measurement above, and the honest answer is: only the
direction switching is, and the refinements are neutral on this workload.

## Unchanged

Tokyo and Osaka measurements from v0.2.2, the defect records, and the
reproduction guide.
