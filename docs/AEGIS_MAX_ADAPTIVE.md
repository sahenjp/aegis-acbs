# Aegis Max Adaptive Exact Solver

この文書は `research/aegis-max-exact` の adaptive exact solver selector の設計と、同一 query で得た実測証拠を記録します。

## 目的

CH/CCH/ALT は query 自体が速くても前処理を必要とします。native Aegis は前処理なしで走れます。そのため「query latency が最小の solver を常に使う」のではなく、期待する workload horizon 全体のコストで選びます。

throughput を重視する既定の `mean` objective は、各 graph で測定した profile を使い、solver ごとに次を計算します。

```text
mean_score = preprocess
           + queries * query_mean
           + metric_updates * update_cost
```

- native exact runner: `preprocess = 0`, `update_cost = 0`
- RoutingKit CH: metric/weight 更新時は再構築が必要なので `update_cost = preprocess`
- RoutingKit CCH: topology/order を維持できる更新では `update_cost = customize`
- ALT: 現在の実装では landmark table 再構築として `update_cost = preprocess`

固定の node 数しきい値は使いません。graph size/topology/hardware の影響は、その graph で測定した `query/preprocess/update` profile に含めます。

## mean / p95 / p99 objective

リアルタイム用途では平均だけ速くても、まれに非常に遅い query がある solver は扱いづらいことがあります。そのため selector は `mean` に加えて `p95` と `p99` を選択できます。

```text
risk_score(stat) = preprocess
                 + queries * query_stat
                 + metric_updates * update_cost

stat = mean | p95 | p99
```

`p95` / `p99` の score は、観測された query 時間をそのまま合計した wall-clock time ではありません。**tail latency を重く評価するための risk objective** です。SLO や応答時間の安定性を優先する workload では、平均値より早い段階で CH/CCH を選ぶために使います。

CLI:

```bash
bin/aegis-max-select \
  --benchmark summary.json \
  --queries 1000 \
  --metric-updates 0 \
  --selection-stat p95
```

persistent batch でも同じ objective を直接使えます。

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
  --verify --summary-only
```

`aegis-max-road-bench.sh` は同じ benchmark profile から `mean/p95/p99 × static/4 updates` の選択結果を別 JSON として出力します。従来の `selection-static.json` と `selection-updates.json` は mean objective の互換 alias です。

## graph identity

`aegis-max-road-bench.sh` は `from/to/cost` と node/edge count から得た SHA-256 graph fingerprint を `summary.json` に保存します。

`aegis-max-batch --auto-select-benchmark` は実行対象 graph の fingerprint と benchmark profile の fingerprint が一致しない場合、solver を選択せずエラーにします。別地域、別metric、古いgraphの測定値を誤って流用しないためです。

selector は候補を全部初期化してから選びません。profile から先に一つ選択し、選択された CH/CCH/ALT だけを初期化します。native solver が選ばれた場合は preprocessed sidecar を起動しません。

## Andorra smoke

37,191 nodes / 66,104 edges / time metric / 120 identical queries の CI smoke では、全 solver が正解し、CH/CCH/ALT は BiDijkstra consensus を通過しました。

代表的な修正後実測:

| solver | query mean | preprocess | update cost |
|---|---:|---:|---:|
| RoutingKit CH | 0.178 ms | 67.581 ms | 67.581 ms |
| RoutingKit CCH | 0.191 ms | 462.395 ms | 1.591 ms |
| Aegis | 0.904 ms | 0 | 0 |
| ALT | 1.024 ms | 85.883 ms | 85.883 ms |

同じ120 queryで、実際の `aegis-max-batch` mean adaptive経路は次を選択しました。

- metric update 0回: `routingkit-ch`
- metric update 4回: `aegis`

CI は mean だけでなく `p95` / `p99` objective でも、選ばれた solver を persistent batch で実際に起動し、全 query を検証するようにしています。

## Tokyo benchmark

2026-09-06 の GitHub-hosted Ubuntu 24.04 runner で、Geofabrik Kanto PBF を bbox `138.90,35.45,140.00,35.90` に切り出し、time metric / seed `424242` / 300 identical queries で測定しました。

Graph: **2,314,133 nodes / 4,855,881 edges**。native 4 solver と CH/CCH/ALT の全300 queryが正解し、preprocessed solver のconsensus checkも成功しています。

| solver | query mean | p50 | p95 | p99 | preprocess | update cost | 300-query amortized mean |
|---|---:|---:|---:|---:|---:|---:|---:|
| RoutingKit CH | **0.294 ms** | 0.289 ms | **0.570 ms** | **0.713 ms** | 31.513 s | 31.513 s | 105.339 ms |
| RoutingKit CCH | 0.400 ms | 0.397 ms | 0.740 ms | 0.828 ms | 110.949 s | **0.310 s** | 370.230 ms |
| ALT | 53.109 ms | 25.336 ms | 199.881 ms | 277.512 ms | 20.352 s | 20.352 s | 120.951 ms |
| Aegis | 75.672 ms | 55.230 ms | 215.679 ms | 274.933 ms | 0 | 0 | **75.672 ms** |
| BiDijkstra | 130.848 ms | 108.096 ms | 344.777 ms | 374.045 ms | 0 | 0 | 130.848 ms |
| Dijkstra | 230.980 ms | 204.991 ms | 477.568 ms | 486.927 ms | 0 | 0 | 230.980 ms |
| A* | 252.967 ms | 136.217 ms | 748.069 ms | 790.690 ms | 0 | 0 | 252.967 ms |

300-query / update 0 の horizon では、objective によって選択が変わります。

| objective | selected | 理由 |
|---|---|---|
| mean | **Aegis** | CH の31.5秒前処理を300 queryでは平均コスト上まだ償却できない |
| p95 | **RoutingKit CH** | Aegis p95 215.679 ms に対して CH p95 0.570 ms。tail削減が前処理コストを上回る |
| p99 | **RoutingKit CH** | Aegis p99 274.933 ms に対して CH p99 0.713 ms |

Aegisとの概算break-evenは:

- mean / update 0: CH 約419 queries
- p95 / update 0: CH 約147 queries
- p99 / update 0: CH 約115 queries
- mean / update 0: CCH 約1,474 queries
- mean / update 4: CH 約2,091 queries
- mean / update 4: CCH 約1,491 queries

したがって同じ graph でも、「短いバッチの総処理時間を優先する」のか「個々の query のtailを抑えたい」のかで、合理的な exact solver は変わります。これは固定した node 数しきい値だけでは表現できません。

## 注意

これらは特定runner image、特定graph、特定metricの実測です。世界最速や全graphでの優位性を示すものではありません。Kanto/Japan、異なるhardware、異なるquery distributionでも同じ手順で再測定し、profileをgraph fingerprintに束縛して使います。
