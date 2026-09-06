# Aegis Max Adaptive Exact Solver

この文書は `research/aegis-max-exact` の adaptive exact solver selector の設計と、同一 query で得た実測証拠を記録します。

## 目的

CH/CCH/ALT は query 自体が速くても前処理を必要とします。native Aegis は前処理なしで走れます。そのため「query latency が最小の solver を常に使う」のではなく、期待する workload horizon 全体のコストで選びます。

throughput を重視する既定の `mean` objective は、各 graph で測定した profile を使い、solver ごとに次を計算します。

```text
mean_score = startup/preprocess
           + queries * query_mean
           + metric_updates * update_cost
```

- native exact runner: `preprocess = 0`, `update_cost = 0`
- RoutingKit CH cold: 初回 CH build を startup、metric/weight 更新時も full rebuild
- RoutingKit CH warm: fingerprint-bound persisted CH index の load を startup、更新時は元の full rebuild cost
- RoutingKit CCH: topology/order を初期前処理、対応する metric 更新では `customize` を update cost
- ALT: 現在の実装では landmark table 再構築を update cost

固定の node 数しきい値は使いません。graph size/topology/hardware の影響は、その graph で測定した `query/preprocess/update` profile に含めます。

## mean / p95 / p99 objective

リアルタイム用途では平均だけ速くても、まれに非常に遅い query がある solver は扱いづらいことがあります。そのため selector は `mean` に加えて `p95` と `p99` を選択できます。

```text
risk_score(stat) = startup/preprocess
                 + queries * query_stat
                 + metric_updates * update_cost

stat = mean | p95 | p99
```

`p95` / `p99` の score は、観測された query 時間をそのまま合計した wall-clock time ではありません。**tail latency を重く評価するための risk objective** です。SLO や応答時間の安定性を優先する workload では、平均値より早い段階で CH/CCH を選ぶために使います。

```bash
bin/aegis-max-select \
  --benchmark summary.json \
  --queries 1000 \
  --metric-updates 0 \
  --selection-stat p95 \
  --preprocess-state cold
```

## cold / warm preprocessing

同じ graph を繰り返し使うサービスでは、CH を毎プロセス起動時に再構築する必要はありません。Aegis Max の RoutingKit CH sidecar は初回 cold build 後、次を sidecar graph の隣に保存します。

```text
graph.routingkit-ch.ch-index
graph.routingkit-ch.ch-index.meta
```

metadata には cache format magic、Aegis graph fingerprint、cold rebuild time を保存します。次回起動時は sidecar graph header の fingerprint/node count と metadata を確認し、cache が一致すれば全 edge の再読込と CH build をせず `ContractionHierarchy::load_file` で起動します。

`aegis-max-batch` の adaptive selection は既定で `--preprocess-state auto` です。

- matching local CH cache が存在: `warm`
- cache がない、metadata が違う: `cold`
- 実験では `--preprocess-state cold|warm` で固定可能

warm evidence が存在しない solver は warm を指定しても cold preprocessing cost へ安全側 fallback します。

cache の graph fingerprint は、古いgraphや別metricとの**取り違えを防ぐためのidentity check**です。RoutingKit の serialized CH は信頼済みローカル生成物としてのみ扱います。fingerprint は悪意ある CH file を安全にsandbox化する仕組みではありません。

## persistent batch

```bash
bin/aegis-max-batch \
  --graph graph.aegis \
  --queries queries.txt \
  --routingkit-ch-server bin/aegis-routingkit-ch-server \
  --routingkit-ch-graph graph.routingkit-ch \
  --routingkit-cch-server bin/aegis-routingkit-cch-server \
  --routingkit-cch-graph graph.routingkit-cch \
  --alt-landmarks 16 \
  --auto-select-benchmark summary.json \
  --metric-updates 4 \
  --selection-stat p95 \
  --preprocess-state auto \
  --verify --summary-only
```

selector は候補を全部初期化してから選びません。profile から先に一つ選択し、選択された CH/CCH/ALT だけを初期化します。native solver が選ばれた場合は preprocessed sidecar を起動しません。

## benchmark output

`aegis-max-road-bench.sh` はCHを意図的にcold起動したあと、同じquery setでもう一度warm起動し、次を分離して保存します。

- `preprocessNs`: cold CH build time
- `warmPreprocessNs`: persisted CH load time
- `rebuildNs` / `updateNs`: weight/metric変更時のfull CH rebuild time
- `cacheIndexBytes`: serialized CH index size
- query `mean/p50/p95/p99`

同じ benchmark profile から `mean/p95/p99 × cold/warm × static/4 updates` の選択結果を生成します。従来の `selection-static.json` / `selection-updates.json` と `selection-{mean,p95,p99}-{static,updates}.json` は conservative cold-start alias です。

## graph identity

`aegis-max-road-bench.sh` は `from/to/cost` と node/edge count から得た SHA-256 graph fingerprint を `summary.json` に保存します。

`aegis-max-batch --auto-select-benchmark` は実行対象 graph の fingerprint と benchmark profile の fingerprint が一致しない場合、solver を選択せずエラーにします。別地域、別metric、古いgraphの測定値を誤って流用しないためです。

## Andorra smoke

37,191 nodes / 66,104 edges / time metric / 120 identical queries の CI smoke では、全 solver が正解し、CH/CCH/ALT は BiDijkstra consensus を通過しました。

2026-09-06 の persistent-cache smoke の代表値:

| solver | query mean | p95 | cold preprocess | warm preprocess | update cost |
|---|---:|---:|---:|---:|---:|
| RoutingKit CH | **0.175 ms** | **0.341 ms** | 70.855 ms | **1.405 ms** | 70.855 ms |
| RoutingKit CCH | 0.200 ms | 0.390 ms | 461.197 ms | - | **1.576 ms** |
| Aegis | 0.913 ms | 2.317 ms | 0 | 0 | 0 |
| ALT | 0.992 ms | 3.840 ms | 79.486 ms | - | 79.486 ms |

CH cache index は約 2.43 MB。cold build 70.855 ms から warm load 1.405 ms へ、約 **50.4x** startup を短縮しました。warm起動後も update cost は cold rebuild 70.855 ms のまま保持します。

同じ120 queryのadaptive batchでは、mean/static、p95/static、p99/static は CH、mean/4 updates は Aegis を選び、選択したsolver自身が120/120 queryを処理しました。

## Tokyo cold benchmark

2026-09-06 の GitHub-hosted Ubuntu 24.04 runner で、Geofabrik Kanto PBF を bbox `138.90,35.45,140.00,35.90` に切り出し、time metric / seed `424242` / 300 identical queries で測定しました。

Graph: **2,314,133 nodes / 4,855,881 edges**。native 4 solver と CH/CCH/ALT の全300 queryが正解し、preprocessed solver のconsensus checkも成功しています。

| solver | query mean | p50 | p95 | p99 | cold preprocess | update cost | 300-query cold amortized mean |
|---|---:|---:|---:|---:|---:|---:|---:|
| RoutingKit CH | **0.294 ms** | 0.289 ms | **0.570 ms** | **0.713 ms** | 31.513 s | 31.513 s | 105.339 ms |
| RoutingKit CCH | 0.400 ms | 0.397 ms | 0.740 ms | 0.828 ms | 110.949 s | **0.310 s** | 370.230 ms |
| ALT | 53.109 ms | 25.336 ms | 199.881 ms | 277.512 ms | 20.352 s | 20.352 s | 120.951 ms |
| Aegis | 75.672 ms | 55.230 ms | 215.679 ms | 274.933 ms | 0 | 0 | **75.672 ms** |
| BiDijkstra | 130.848 ms | 108.096 ms | 344.777 ms | 374.045 ms | 0 | 0 | 130.848 ms |
| Dijkstra | 230.980 ms | 204.991 ms | 477.568 ms | 486.927 ms | 0 | 0 | 230.980 ms |
| A* | 252.967 ms | 136.217 ms | 748.069 ms | 790.690 ms | 0 | 0 | 252.967 ms |

この旧Tokyo runはpersistent CH cache導入前のcold evidenceです。300-query / update 0 のcold horizonでは、meanはAegis、p95/p99はCHを選びます。最新コードではTokyoのwarm CH load timeも別途再測定し、cold/warmを分けて扱います。

cold Aegisとの概算break-even:

- mean / update 0: CH 約419 queries
- p95 / update 0: CH 約147 queries
- p99 / update 0: CH 約115 queries
- mean / update 0: CCH 約1,474 queries
- mean / update 4: CH 約2,091 queries
- mean / update 4: CCH 約1,491 queries

## 注意

performance profile は graph だけでなくCPU、memory、compiler、runner loadにも依存します。graph fingerprintは正確性上のidentityを保証しますが、別hardwareで同じ性能を保証するものではありません。Kanto/Japan、異なるhardware、異なるquery distributionでも同じ手順で再測定します。

現在の証拠から「全graphで常に最速」「世界最速」とは主張しません。目的は、exactnessを維持したまま、そのworkloadで実測上もっとも合理的なsolverを選ぶことです。
