# Metric ALT final evaluation — 2026-08-02

Status: rejected for promotion; retain as an experimental candidate.

## Evidence

- Workflow run: `30727575231`
- Artifact: `metric-alt-final-evaluation`
- Artifact digest: `sha256:df4b24dd5791d3d303d7e180083faef8506e8ad2da1c84327f169a13c552e7c6`
- Runner: Linux arm64, Go 1.23.12, 4 CPUs
- Graphs: Andorra, Bremen, Luxembourg, and a Tokyo metropolitan extract
- Seeds: `1010`, `424242`, `20260717`
- Queries: 300 per graph/metric/seed report
- Exactness: every report set `allCorrect=true`; all compared candidate distances agreed with Dijkstra

The regional matrix contains distance and time metrics for Andorra, Bremen, and Luxembourg. The Tokyo extract was evaluated with the time metric. Across the 21 reports, the guarded-race candidate therefore has 6,300 paired query observations against production Aegis.

## Candidate comparison

The strongest candidate in this run was `aegis-alt-guarded-race`. Relative runtime is candidate runtime divided by production `aegis` runtime, paired by query.

| Workload | Seed | Geometric mean | p95 | p99 | Median |
|---|---:|---:|---:|---:|---:|
| Andorra distance | 1010 | 0.7397 | 1.2953 | 1.5860 | 0.8354 |
| Andorra distance | 424242 | 0.6540 | 1.2717 | 1.5381 | 0.6885 |
| Andorra distance | 20260717 | 0.7155 | 1.2375 | 1.3534 | 0.8090 |
| Andorra time | 1010 | 0.6298 | 1.3252 | 2.1040 | 0.6383 |
| Andorra time | 424242 | 0.5999 | 1.2165 | 1.6774 | 0.6118 |
| Andorra time | 20260717 | 0.6002 | 1.1221 | 1.3499 | 0.6139 |
| Bremen distance | 1010 | 0.7710 | 1.2093 | 1.2930 | 0.9909 |
| Bremen distance | 424242 | 0.8041 | 1.2240 | 1.5252 | 1.0031 |
| Bremen distance | 20260717 | 0.8051 | 1.2482 | 1.3783 | 1.0078 |
| Bremen time | 1010 | 0.7430 | 1.1708 | 1.4376 | 0.9475 |
| Bremen time | 424242 | 0.7069 | 1.1655 | 1.7887 | 0.9578 |
| Bremen time | 20260717 | 0.7275 | 1.1728 | 1.4827 | 0.9299 |
| Luxembourg distance | 1010 | 0.8977 | 1.2534 | 1.4501 | 1.0029 |
| Luxembourg distance | 424242 | 0.9123 | 1.2680 | 1.4472 | 1.0071 |
| Luxembourg distance | 20260717 | 0.9283 | 1.2862 | 1.5964 | 1.0171 |
| Luxembourg time | 1010 | 0.7850 | 1.2661 | 1.5679 | 0.9314 |
| Luxembourg time | 424242 | 0.7689 | 1.2195 | 1.4677 | 0.8741 |
| Luxembourg time | 20260717 | 0.7854 | 1.2810 | 1.5240 | 0.9270 |
| Tokyo time | 1010 | 0.9458 | 1.3529 | 1.6949 | 0.9853 |
| Tokyo time | 424242 | 0.8798 | 1.2467 | 1.6278 | 0.9854 |
| Tokyo time | 20260717 | 0.9076 | 1.2644 | 1.6320 | 0.9907 |
| **All paired observations** | — | **0.7695** | **1.2446** | **1.5688** | **0.9579** |

The pooled geometric mean is favorable, but this is not sufficient for promotion. Every evaluated workload violates the required tail gate: p95 and p99 must be no more than 1.02 times the strongest comparable baseline. Tokyo also has only 900 paired queries, not the required 10,000-query comparison, and the run is not an independent repeat of that full gate.

`aegis-alt-top4` was also rejected. It regressed production Aegis on Tokyo in all three seeds, with geometric-mean ratios of 1.2369, 1.1886, and 1.1209, while its paired p95 ratios were between 1.7874 and 1.9630.

## Interpretation

The guarded race is useful evidence that metric ALT can reduce average work on smaller regional graphs, but the per-query runtime distribution is bimodal: queries for which the ALT path wins can be substantially faster, while losing queries pay enough duplicate-search and selection overhead to create unacceptable tails. The median near 1.0 on Bremen, Luxembourg, and Tokyo confirms that the pooled geometric-mean gain is concentrated in a subset of queries rather than being a uniform speedup.

The next candidate should avoid racing two complete searches. Prefer a single exact search with a low-cost query classifier or an adaptive potential policy that can fall back before substantial duplicate work is committed. Any classifier must be evaluated out-of-sample and its own inference cost must remain inside timed query runtime.

## Promotion decision

Rejected. Do not promote or merge based on this artifact.

A future promotion run must still provide:

- 100% Dijkstra distance agreement and zero optimality gap;
- at least three real road graphs including Tokyo or an equivalently large graph;
- at least three seeds and 10,000 query comparisons;
- at least 5% geometric-mean runtime improvement over the strongest comparable exact baseline;
- p95 and p99 no worse than 2%;
- preprocessing time, amortization, memory, and hardware reporting;
- an independent repeat confirming the result;
- separate conclusions for preprocessing-free and preprocessed algorithm classes.
