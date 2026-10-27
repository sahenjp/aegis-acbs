# Aegis ACBS v0.2.1

`v0.2.1` corrects errors in the paper draft. **No code changed**; the search
implementation is identical to v0.2.0.

## Why this release exists

An independent review of `docs/PAPER.md` found three errors that would have made
the paper unpublishable. All three were corrected before archiving.

### 1. The abstract overstated the baseline

It reported a speedup of 8.64x against bidirectional Dijkstra. That figure is the
ratio against **plain unidirectional** Dijkstra. Against bidirectional Dijkstra:

| City | Metric | vs bidirectional Dijkstra | vs unidirectional Dijkstra |
|---|---|---:|---:|
| Tokyo | distance | 6.04x | 8.64x |
| Osaka | distance | 4.38x | 6.59x |
| Tokyo | travel time | 1.82x | 3.46x |
| Osaka | travel time | 1.54x | 3.39x |

### 2. The termination rule is not bound B3

An earlier draft claimed the stopping rule reduces to the g-bound (bound B3 of
Siag et al., IJCAI 2023). The derivation assumed the potential cancels between
the two frontier minima, which only holds when both are read at the same node.
The rule is a standard reduced-cost bidirectional rule, valid because reduced
costs are non-negative, but it is not B3.

### 3. A table had a duplicated number

Dijkstra and bidirectional Dijkstra were both listed with 818,508 relaxed edges,
which cannot be true when bidirectional search expands 120,000 fewer nodes. The
correct value for Dijkstra is 1,091,302.

## What this means for the claims

The honest summary after these corrections is narrower than v0.2.0 implied:
ACBS is 6.04x faster than bidirectional Dijkstra on Tokyo distance, 4.38x on
Osaka distance, and 1.5-1.8x on travel time. The stopping rule is not novel and
is not the bound the paper first named. The scheduler remains the only candidate
contribution, and **the draft does not yet specify it**, which is recorded in the
limitations.

## Unchanged

The code, the tests, and the released binaries. `v0.2.0` remains valid for
anyone who wants the implementation; this release is the paper with its numbers
corrected.
