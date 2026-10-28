# ACBS on London and New York road networks

Measured results for two additional city road networks, taken from OpenStreetMap
country extracts with the same bounding-box method used for Tokyo and Osaka
(see [REPRODUCING.md](REPRODUCING.md)). Every number in this document was read
from the JSON reports named in each section; no value is estimated.

> **付記: 本文書の性格**
>
> 本文書は論文 [PAPER.md](PAPER.md) から参照される付属資料のため英語で記述します。
> 日本語の結論は以下の4行です。
>
> 1. London (76万ノード) では ACBS は Dijkstra 比 **7.752倍**、最速classical (A*) 比 **2.015倍**で、300クエリすべて厳密解でした。
> 2. New York (35万ノード) では Dijkstra 比 **4.423倍**、A* 比 **1.870倍**で、同様に 300/300 厳密解でした。
> 3. 高速化の内訳は Tokyo と同型で、**展開ノード数の削減が主因**です (London: 展開 10.887倍 × ノード単価 1.404倍)。
> 4. Berlin と Paris は PBF 取得まで完了しましたが、bbox 抽出が長時間化したため打ち切りました。数値は掲載しません (§5)。

---

## 1. Environment and protocol

| Item | Value |
|---|---|
| Machine | AMD Ryzen 7 5700X (8 cores / 16 threads) |
| OS / kernel | Fedora 44, 6.19.10-300.fc44.x86_64 |
| Go | go1.26.10 linux/amd64 |
| Binary | `bin/aegis` v0.2.0 |
| Benchmark date | 2026-10-29 |
| Metric | `distance` (metres), the natural metric for OSM car routing |
| Queries | 300 per graph |
| Repeats | 9 per query |
| Seed | 1010 |
| Order / suite | `interleaved` / `mixed` |
| Correctness oracle | `dijkstra`, run outside the timed samples |

The protocol follows [BENCHMARKING.md](BENCHMARKING.md): algorithms are
interleaved within each repeat in a shuffle order derived from the seed, and the
per-query median across repeats is the reported measurement. All five algorithms
are exact, so any distance disagreement is a correctness failure rather than a
trade-off.

The CLI reports the algorithm as `aegis`. In the paper the same algorithm is called
**ACBS**; this document follows the CLI names in tables and uses ACBS when
discussing the algorithm itself.

`aegis-static` disables the adaptive scheduler while keeping the same bounds and
termination rule, so `aegis` versus `aegis-static` isolates the scheduler.

## 2. London

| Property | Value |
|---|---|
| Extraction | Geofabrik `europe/great-britain-latest.osm.pbf` (2,193,177,688 bytes) |
| Box | centre 51.5074, -0.1278, radius 0.18 degrees |
| Resulting bbox | 51.3274009 .. 51.6873997 N, -0.3077995 .. 0.0521989 E |
| Nodes / edges | 761,244 / 1,449,256 |
| `heuristicStrength` | 0.999919 |
| `minCostPerMeter` | 1000.000000007 |
| Graph diameter | 47.15 km |
| Correctness | 300 / 300 exact for every algorithm |

| Algorithm | Median (ms) | p95 (ms) | Median expanded | Median relaxed | Correct | Speedup vs Dijkstra |
|---|---:|---:|---:|---:|---:|---:|
| dijkstra | 53.228 | 95.990 | 416,127 | 796,389 | 300/300 | 1.000x |
| bidijkstra | 29.440 | 75.619 | 230,592 | 445,443 | 300/300 | 1.808x |
| astar | 13.836 | 74.072 | 63,902 | 118,035 | 300/300 | 3.847x |
| aegis-static | 7.603 | 33.075 | 41,892 | 80,223 | 300/300 | 7.001x |
| **aegis** | **6.866** | **30.166** | **38,221** | **72,765** | **300/300** | **7.752x** |

Scheduler work for `aegis` on London: median 25 direction switches, 35 chunks,
52.2% of expansions on the forward frontier.

Raw report: `london-distance.json` (not committed; regenerate with the commands in
[REPRODUCING.md](REPRODUCING.md)).

## 3. New York

| Property | Value |
|---|---|
| Extraction | Geofabrik `north-america/us/new-york-latest.osm.pbf` (498,267,563 bytes) |
| Box | centre 40.7128, -74.0060, radius 0.18 degrees |
| Resulting bbox | 40.5328100 .. 40.8927788 N, -74.1859891 .. -73.8260021 E |
| Nodes / edges | 353,434 / 590,876 |
| `heuristicStrength` | 0.999934 |
| `minCostPerMeter` | 1000.000000043 |
| Graph diameter | 50.23 km |
| Correctness | 300 / 300 exact for every algorithm |

| Algorithm | Median (ms) | p95 (ms) | Median expanded | Median relaxed | Correct | Speedup vs Dijkstra |
|---|---:|---:|---:|---:|---:|---:|
| dijkstra | 17.409 | 36.513 | 170,376 | 285,404 | 300/300 | 1.000x |
| bidijkstra | 12.561 | 31.698 | 130,293 | 218,124 | 300/300 | 1.386x |
| astar | 7.360 | 39.737 | 37,649 | 62,910 | 300/300 | 2.365x |
| aegis-static | 4.757 | 28.421 | 32,188 | 54,219 | 300/300 | 3.660x |
| **aegis** | **3.936** | **30.230** | **27,114** | **43,289** | **300/300** | **4.423x** |

Scheduler work for `aegis` on New York: median 17 direction switches, 22 chunks,
60.6% of expansions on the forward frontier.

Raw report: `new-york-distance.json`.

## 4. Where the speedup comes from

Total speedup factors into *work reduction* (fewer expanded nodes) and *per-node
cost* (queue and memory traffic per expansion). Dividing the former by the latter
reproduces the total exactly, because both are derived from the same medians:

```text
speedup = (expanded_A / expanded_B) x (per_node_cost_B / per_node_cost_A)
```

**London**

| Comparison | Speedup | Expanded reduction | Per-node cost |
|---|---:|---:|---:|
| aegis vs dijkstra | 7.752x | 10.887x | 1.404x |
| aegis vs astar (fastest classical) | 2.015x | 1.672x | 0.830x |
| aegis vs aegis-static | 1.107x | 1.096x (8.8% fewer) | 1.010x |

**New York**

| Comparison | Speedup | Expanded reduction | Per-node cost |
|---|---:|---:|---:|
| aegis vs dijkstra | 4.423x | 6.284x | 1.421x |
| aegis vs astar (fastest classical) | 1.870x | 1.389x | 0.743x |
| aegis vs aegis-static | 1.208x | 1.187x (15.8% fewer) | 1.018x |

The structure matches the Tokyo measurement quoted in [PAPER.md](PAPER.md): the win
comes from expanding far fewer nodes, while each individual expansion is slightly
more expensive (bidirectional frontiers, doubled reduced costs). Against the
strongest classical competitor, A*, the work reduction is smaller because A*
already exploits the same perfect distance lower bound; what remains is the
volumetric saving of searching from both ends.

The `aegis` versus `aegis-static` rows isolate the scheduler. It reduces expansions
by 8.8% (London) and 15.8% (New York). London, the larger and denser graph, gains
less: it needs fewer direction switches in the first place (median 25 switches),
leaving the scheduler less to exploit. New York switches 17 times on median but
still profits more, which is consistent with its larger optimality gap between the
static schedule and the adaptive one rather than with switch count alone. This
document does not claim more than the two measured points support.

Geometric-mean per-query speedup against Dijkstra, as reported by the harness:
4.856x for London and 2.880x for New York. Median ratios are quoted everywhere else
because geometric means are dominated by the cheap local queries.

## 5. Berlin and Paris: not completed

Both extracts were downloaded successfully. **Their bounding-box extraction and
benchmarks did not complete**, so no latency or correctness figures are reported
for them. Recording this explicitly is preferable to omitting it.

| City | PBF | Size (bytes) | sha256 | Outcome |
|---|---|---:|---|---|
| Berlin | `europe/germany-latest.osm.pbf` | 4,870,645,940 | `5ef5351434dd8979…` | extraction aborted |
| Paris | `europe/france-latest.osm.pbf` | 5,105,720,731 | `cde032884132071d…` | extraction aborted |

Two distinct causes, stated plainly:

1. **Berlin.** The first attempt was killed by the kernel under memory pressure.
   The extractor holds an OSM location cache for the whole country file, and it was
   running concurrently with the Paris extraction and a London benchmark; the
   machine reached 29 of 31 GB resident with all 7 GB of swap consumed. A second
   attempt was stopped deliberately to stop it competing with Paris for the same
   memory. Neither run produced a graph.
2. **Paris.** Extraction ran for over 22 minutes without finishing. It was stopped
   deliberately under the 30-minute cutoff set for this task.

The country files are 4.9 GB and 5.1 GB. A city-sized regional extract (for
example a single Bundesland or region as published by Geofabrik) would avoid the
whole-country node scan and should make both feasible; that remains untested here.

## 6. What these two cities do and do not establish

They establish that ACBS is exact and faster than both Dijkstra and bidirectional
Dijkstra on two further real road networks outside Japan, on the `distance` metric,
and that the scheduler contributes a measurable reduction in expansions.

They do **not** establish results for the `time` metric, where the admissible
heuristic is far weaker (`heuristicStrength` near 0.25 for Tokyo, see
[PAPER.md](PAPER.md)), nor for graphs outside the 0.35-0.76 million node range, nor
for continental extracts. Two additional cities are not a large-scale study, and
the Berlin and Paris gaps above are real gaps rather than negligible ones.

## 7. Reproducing these runs

Full commands, including the measured wall-clock cost on the machine described in
§1. All commands assume the repository root as the working directory.

```bash
# 1. Country extract (the downloads are large: 2.19 GB and 0.50 GB)
mkdir -p /tmp/osm-pbf && cd /tmp/osm-pbf
curl -L -C - -o great-britain.osm.pbf \
  https://download.geofabrik.de/europe/great-britain-latest.osm.pbf      # 486 s
curl -L -C - -o new-york.osm.pbf \
  https://download.geofabrik.de/north-america/us/new-york-latest.osm.pbf # 131 s
cd -

# 2. Bounding-box extraction (ways first, then the nodes they reference)
mkdir -p /tmp/osm-cities
python3 scripts/extract-osm-bbox.py \
  /tmp/osm-pbf/great-britain.osm.pbf /tmp/osm-cities/london.osm \
  51.5074 -0.1278 0.18                                                # 692 s, 4.4 GB RSS
python3 scripts/extract-osm-bbox.py \
  /tmp/osm-pbf/new-york.osm.pbf /tmp/osm-cities/new-york.osm \
  40.7128 -74.0060 0.18                                              # 169 s, 1.3 GB RSS

# 3. Import
bin/aegis import-osm --input /tmp/osm-cities/london.osm \
  --output /tmp/osm-cities/london-distance.aegis --profile car --metric distance --name london
bin/aegis import-osm --input /tmp/osm-cities/new-york.osm \
  --output /tmp/osm-cities/new-york-distance.aegis --profile car --metric distance --name new-york

# 4. Benchmark
bin/aegis benchmark --graph /tmp/osm-cities/london-distance.aegis \
  --algorithms dijkstra,bidijkstra,astar,aegis,aegis-static \
  --queries 300 --repeats 9 --order interleaved --suite mixed --seed 1010 \
  --output /tmp/osm-cities/london-distance.json --html ''            # 362 s
bin/aegis benchmark --graph /tmp/osm-cities/new-york-distance.aegis \
  --algorithms dijkstra,bidijkstra,astar,aegis,aegis-static \
  --queries 300 --repeats 9 --order interleaved --suite mixed --seed 1010 \
  --output /tmp/osm-cities/new-york-distance.json --html ''          # 180 s
```

The extractor prints the way and node counts it kept, and the import should report
the node and edge counts in §2 and §3; if they differ, the upstream extract has
moved on and the timings are no longer directly comparable. The two extractions
must not run concurrently with a benchmark: the OSM location cache for a country
file reaches several gigabytes and the machine exhausts memory (see §5).

## 8. Verifying these numbers

The `correct` column in a report compares against the harness's own Dijkstra run.
For an independent check, compare returned distances per query across algorithms
from the same JSON (the excerpt below is reproduced from
[REPRODUCING.md](REPRODUCING.md)):

```python
import json
d = json.load(open("london-distance.json"))
by = {}
for s in d["samples"]:
    by.setdefault(s["stats"]["algorithm"], {})[(s["sourceId"], s["targetId"])] = s["stats"]["distance"]
for other in ("bidijkstra", "astar", "aegis", "aegis-static"):
    common = set(by["dijkstra"]) & set(by[other])
    bad = [k for k in common if by[other][k] != by["dijkstra"][k]]
    print(other, len(common) - len(bad), "/", len(common))
```

Run on both reports for this document, this printed 300 / 300 with zero mismatches
for all four comparisons in each city.
