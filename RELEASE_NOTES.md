# Aegis ACBS v0.2.0

`v0.2.0` adds real-road-network measurements, a paper draft, and the honest
negative results behind them. The default `aegis` search is unchanged from
v0.1.0.

## Measured on real road networks

Tokyo (1,098,173 nodes) and Osaka (748,903 nodes) from OpenStreetMap, distance
and travel-time metrics, every query exact:

| City | Metric | ACBS vs bidirectional Dijkstra | Paired |
|---|---|---:|---:|
| Tokyo | distance | 8.64x | 390 of 400 |
| Osaka | distance | 6.59x | 288 of 300 |
| Tokyo | travel time | 3.46x | 252 of 300 |
| Osaka | travel time | 3.39x | 223 of 300 |

The advantage depends on how much information the geographic lower bound
carries. On a synthetic uniform lattice it disappears and then reverses. That
reversal is reported in the paper rather than left out.

## What is claimed, and what is not

The termination rule is **not new**. `docs/PAPER.md` derives it, shows it reduces
to `U <= gF + gB`, and identifies it as bound B3 in the enumeration of Siag et
al. (IJCAI 2023). The scheduler that decides which frontier receives the next
edge-work chunk is the only candidate contribution, and the paper records that
its distinctness from prior expansion policies is unresolved.

## Negative results, recorded

Three further exact searches (MEET, PB-Buckets, CATCH) and a multi-bound
experiment were implemented and measured. None beat bidirectional Dijkstra.
`docs/CANDIDATES.md` records each attempt, the defects found in it, and how it
was resolved, including one variant whose "speedup" was an artifact of an
unsound termination condition that returned wrong answers.

## Limitations

Two cities, one machine. Turn restrictions are not modelled; the Tokyo box
contains 2,089 of them, which is 0.092 percent of directed edges. Travel-time
costs use a speed model derived from `maxspeed` tagging, not observed traffic.

## Reproducing

`docs/REPRODUCING.md` gives the bounding boxes, seeds, inspection values that
identify a matching extract, and a check that compares per-query distances
instead of trusting the harness correctness flag.
