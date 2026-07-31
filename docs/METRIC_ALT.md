# Metric ALT potential

`aegis-alt`は、production `aegis`のscheduler・incumbent・結合下界・停止条件を変更せず、balanced feasible potentialだけをlandmark下界で強化する実験的variantです。

> [!IMPORTANT]
> production `aegis`は変更しません。`aegis-alt`は明示指定した場合だけ使われ、地域横断評価とTokyo workloadの採用gateを通過するまでは研究候補です。

## 1. 対象グラフ

元の道路グラフを、有限・非負重みを持つ有向グラフ

\[
G=(V,E,c),\qquad c:E\to\mathbb R_{\ge0}
\]

とします。

metric ALTでは、各有向辺\((u,v)\in E\)を同じコストの無向辺として扱うrelaxation

\[
G_U=(V,E_U,c_U)
\]

を前処理専用に作ります。反対向きの辺が異なるコストで存在する場合は、無向多重辺として両方を保持したのと同じ最短距離になります。

`G`上の任意の有向pathは`G_U`上でもwalkなので、到達可能な任意の\(x,y\)について

\[
d_U(x,y)\le \delta_G(x,y)
\]

です。

## 2. Landmark下界

landmark集合を

\[
\mathcal L=\{\ell_1,\ldots,\ell_k\}
\]

とします。各landmarkから`G_U`上の単一始点最短距離を前計算します。

\[
D_i(v)=d_U(\ell_i,v)
\]

metricの逆三角不等式より、任意の頂点\(x,y\)について

\[
|D_i(x)-D_i(y)|\le d_U(x,y)\le\delta_G(x,y)
\]

です。したがって、次は有向グラフに対する許容下界です。

\[
h_{\mathcal L}(x,y)
=
\max_{\ell_i\in\mathcal L}
|D_i(x)-D_i(y)|
\]

ACBSの前向き・後向き下界には

\[
h_F(v)=\max\{h_{chord}(v,t),h_{\mathcal L}(v,t)\}
\]

\[
h_B(v)=\max\{h_{chord}(s,v),h_{\mathcal L}(s,v)\}
\]

を使います。

chord下界とlandmark下界はどちらも許容的なので、その最大値も許容的です。

## 3. Balanced potentialのfeasibility

ACBSが使う整数potentialは

\[
\phi_2(v)=h_F(v)-h_B(v)
\]

です。

`G_U`には元の各有向辺\((u,v)\)がコスト\(c(u,v)\)の無向辺として存在するため、各landmark関数は辺上で

\[
|D_i(v)-D_i(u)|\le c(u,v)
\]

を満たします。1-Lipschitz関数の最大値も1-Lipschitzなので、

\[
|h_F(v)-h_F(u)|\le c(u,v)
\]

\[
|h_B(v)-h_B(u)|\le c(u,v)
\]

です。よって

\[
|\phi_2(v)-\phi_2(u)|
\le
|h_F(v)-h_F(u)|+|h_B(v)-h_B(u)|
\le 2c(u,v)
\]

となります。

したがって両方向のreduced edge costは非負です。

\[
c'_F(u,v)=2c(u,v)+\phi_2(v)-\phi_2(u)\ge0
\]

\[
c'_B(v,u)=2c(u,v)+\phi_2(u)-\phi_2(v)\ge0
\]

既存のradix heap、label-setting性、結合下界、

\[
L_2\ge U_2
\]

による停止証明をそのまま使用できます。

## 4. Directed ALTを使わない理由

有向ALTの標準的な三角不等式

\[
d(\ell,t)-d(\ell,v)
\]

や

\[
d(v,\ell)-d(t,\ell)
\]

は、有向A*の片方向heuristicとしては有効です。しかしACBSのbalanced potentialには、同じ辺についてpotential差の正負両方を制限する対称なLipschitz条件が必要です。

実道路グラフでdirected ALTを試したところ、ある辺で

\[
2c(u,v)+\phi_2(u)-\phi_2(v)<0
\]

となり、radix heapの単調key条件を破りました。metric ALTは無向relaxationのmetric差を使うことで、この問題を構造的に排除します。

## 5. Landmark選択

実装は決定論的です。

1. 最初のlandmarkはforward degreeとreverse degreeの合計が最大の頂点。
2. 未被覆の連結成分があれば、その成分内でdegree最大の頂点。
3. それ以外は、既存landmark集合への最短距離が最大の頂点。

これはfarthest-point samplingに相当し、乱数seedに依存しません。

測定結果から、現在の推奨数は次です。

| 有向辺数 | landmark数 | 動作 |
|---:|---:|---|
| `< 10,000` | 0 | production ACBSへ退避 |
| `10,000 .. 399,999` | 4 | 小・中規模regional graph |
| `>= 400,000` | 8 | 大規模regional graph |

16 landmarksは探索量をさらに減らしましたが、queryごとのlandmark走査コストが増え、総実行時間では4・8 landmarksに劣りました。

## 6. 前処理とメモリ

landmark数を\(k\)とすると、前処理は`G_U`上で\(k\)回の単一始点最短経路探索を行います。

binary heap表記の上界では

\[
O\!\left(k(E+V)\log V\right)
\]

時間、距離表に

\[
O(kV)
\]

メモリを使います。実装は非負整数key向けのmonotone radix heapを再利用します。

query中は新しく触れた頂点ごとに最大\(k\)個の距離差を評価します。

前処理は`route`、`benchmark`、`stress`のtimed queryより前に実行されます。indexはimmutableで、同じgraph instanceを使う並行queryから安全に共有できます。

## 7. 評価結果

2026年7月31日に、Andorra・Bremen・Luxembourgのdistance/time graph、3 seed、各300 queryで比較しました。

- 4 landmarks: 5,400 query comparisons
- 8 landmarks: 5,400 query comparisons
- 16 landmarks: 5,400 query comparisons
- 合計: 16,200 query comparisons

全queryでproduction `aegis`と距離が一致し、到達可能な結果はすべて

\[
optimalityGap=0
\]

でした。

### Query runtime

値は`aegis-alt / aegis`のper-query geometric meanです。1未満が高速です。

| Workload | 4 landmarks | 8 landmarks | 16 landmarks |
|---|---:|---:|---:|
| Andorra distance | 0.466 | 0.511 | 0.681 |
| Andorra time | 0.428 | 0.466 | 0.627 |
| Bremen distance | 0.514 | 0.512 | 0.513 |
| Bremen time | 0.538 | 0.597 | 0.578 |
| Luxembourg distance | 0.607 | 0.490 | 0.524 |
| Luxembourg time | 0.462 | 0.288 | 0.308 |
| **全体** | **0.499** | **0.466** | **0.523** |

### 8-landmark平均探索量差

| Workload | `Δ expanded` | `Δ relaxed` |
|---|---:|---:|
| Andorra distance | -3,940 | -6,899 |
| Andorra time | -4,486 | -7,955 |
| Bremen distance | -8,903 | -17,116 |
| Bremen time | -7,513 | -14,302 |
| Luxembourg distance | -24,229 | -47,269 |
| Luxembourg time | -41,606 | -80,663 |

### 前処理例

| Graph | 4 landmarks | 8 landmarks | 16 landmarks |
|---|---:|---:|---:|
| Andorra | 約0.010秒 / 1.12MiB | 約0.018秒 / 2.25MiB | 約0.042秒 / 4.50MiB |
| Bremen | 約0.038秒 / 3.90MiB | 約0.063秒 / 7.79MiB | 約0.148秒 / 15.58MiB |
| Luxembourg | 約0.13秒 / 9.04MiB | 約0.20秒 / 18.08MiB | 約0.51秒 / 36.16MiB |

これらは3地域における観測値であり、普遍的な速度保証ではありません。

## 8. 実行方法

```bash
bin/aegis benchmark \
  --graph path/to/graph.aegis \
  --queries 1000 \
  --repeats 9 \
  --order interleaved \
  --suite mixed \
  --seed 1010 \
  --algorithms aegis,aegis-alt \
  --output artifacts/metric-alt/report.json \
  --html artifacts/metric-alt/report.html
```

CLIはgraph規模からlandmark数を選び、前処理時間・landmark数・概算距離表メモリをstderrへ表示します。

## 9. 採用gate

productionへ統合する前に、少なくとも次を満たす必要があります。

1. Tokyoの変更なしworkloadで全距離が一致する。
2. mean・median・p95・p99・expanded・relaxedの悪化が事前定義範囲内。
3. 複数seedで新しい重大tailを作らない。
4. 前処理時間と追加メモリを含めたbreak-even query数を提示する。
5. Go 1.23、主要OS、race、cross-buildを通過する。
6. indexの永続化形式を追加する場合、旧`.aegis`形式との互換性を維持する。

## 10. 現在の境界

- indexはmemory内だけで、`.aegis`ファイルへ永続化していません。
- `serve`への自動前処理統合はまだ行っていません。
- preprocessing cancellation APIは未実装です。
- Tokyoとより大きい複数地域での再評価が必要です。
- 学術的新規性は主張しません。ALTとlandmark routingの既知の考え方を、ACBSの対称feasible potentialへ適合させたengineering candidateです。
