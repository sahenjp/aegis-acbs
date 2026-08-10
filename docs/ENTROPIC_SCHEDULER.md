# 二相エントロピー正則化付き証明 scheduler

この文書は、production scheduler `edge-efficiency-v3` と並行評価する実験的 variant `aegis-entropic`、scheduler version `entropic-proof-rate-v2` を定義します。既存の `aegis` は比較基準として変更しません。

> [!IMPORTANT]
> この候補は正確性試験と複数道路グラフの評価を通過していますが、Tokyo の公開 workload を変更せずに再評価するまでは production へ昇格しません。

## 1. 不変な正確性境界

scheduler が変更できるのは、有限 edge-work chunk を前向き・後ろ向きのどちらへ割り当てるかだけです。以下は変更しません。

\[
L_2=\min OPEN_F+\min OPEN_B,
\]

\[
U_2=2U+\phi_2(t)-\phi_2(s),
\]

\[
L_2\ge U_2\Longrightarrow U=\delta(s,t).
\]

balanced feasible potential、non-negative reduced cost、`g` label、接続候補、incumbent、結合下界、停止条件は production ACBS と共通です。

## 2. 二相制御

v1 は探索開始直後からエントロピー則を使いました。地域道路グラフの実測では、証明閉鎖ではなく最初の有限上界を見つけるまでの探索を遅らせる場合がありました。v2 は役割を分離します。

### Phase A: incumbent acquisition

有限上界が存在しない間は、実績のある production scheduler をそのまま使います。

\[
d_k=S_{\mathrm{edge\text{-}efficiency\text{-}v3}}(x_k),
\qquad U_k=\infty.
\]

したがって、最初の上界が得られるまでの方向選択、chunk budget、効率推定、状態遷移は `aegis` と同一です。

### Phase B: proof closure

有限上界を得た次の chunk 境界から、結合下界を上界まで押し上げる証明閉鎖にエントロピー正則化則を使います。

\[
d_k=S_{\mathrm{entropic}}(x_k),
\qquad U_k<\infty.
\]

上界を chunk 内で発見した瞬間には強制中断しません。実測では event-triggered な即時中断が余分な chunk と展開を増やしたため、相転移は再現可能な chunk 境界に限定します。

## 3. production 推定値による warm start

Phase A の scheduler は方向 \(d\in\{F,B\}\) ごとに、結合下界の増分 \(\Delta L_{2,d}\) と work \(w_d\) から固定小数点効率を保持します。

\[
s_d=\left\lfloor
10^6\frac{\Delta L_{2,d}}{w_d}
\right\rfloor.
\]

Phase B の初期 prior は、この十分統計量を Laplace smoothing 付きで再利用します。

\[
\widetilde r_d=
\frac{s_d+1}{10^6+1}.
\]

これにより、Phase B は探索履歴を捨てて二方向を再 bootstrap せず、production が既に学習した方向効率から開始します。

## 4. 方向別の証明報酬

Phase B では、chunk 前後の有効な最小 reduced key を \(k_d,k'_d\) とし、選択方向だけへ進行量を帰属させます。

\[
\Delta k_d=\max\{0,k'_d-k_d\}.
\]

正規化 work は確認辺数 \(E_d\)、展開頂点数 \(V_d\)、選択 queue の正の増加量を用います。

\[
w_d=E_d+4V_d+2\max\{0,Q'_d-Q_d\}.
\]

観測証明率は

\[
r_d=\frac{\Delta k_d+1}{w_d+1}
\]

です。反対側 frontier の stale-entry 清掃などを、選択方向の報酬へ混入させません。

## 5. log-domain のロバスト推定

証明率は桁単位で変動するため、log 空間で更新します。

\[
z_d=\log r_d,
\]

\[
m_d\leftarrow m_d+
\alpha\operatorname{clip}(z_d-m_d,-\log4,\log4),
\qquad \alpha=\frac14.
\]

単一 chunk が推定率を動かせる倍率は最大

\[
4^{1/4}=\sqrt2
\]

です。最初の観測は `(gain, work)` の十分統計量として保持し、次の意思決定が必要になるまで `log` を計算しません。

## 6. online utility

方向 \(d\) の観測回数を \(n_d\)、合計を \(N=n_F+n_B\) とすると、探索不足を補う項を

\[
b_d=0.45\sqrt{\frac{\log(N+2)}{n_d+1}}
\]

とします。

結合下界 \(L_2=k_F+k_B\) の小さい key 側を bottleneck とみなし、proof pressure を

\[
\eta_F=\frac{k_B-k_F}{\max(k_F,k_B)+1},
\qquad \eta_B=-\eta_F
\]

とします。v2 の entropy phase は上界取得後だけなので、係数は

\[
\lambda=0.80
\]

です。switch penalty \(\kappa=0.08\) を含む utility は

\[
u_d=m_d+b_d+\lambda\eta_d
-\kappa\mathbf1[d\ne d_{previous}].
\]

## 7. エントロピー正則化配分

二方向 simplex

\[
\Delta_2=\{q\in\mathbb R_{\ge0}^2:q_F+q_B=1\}
\]

上で、

\[
q^*=\arg\max_{q\in\Delta_2}
\left(\langle q,u\rangle+\tau H(q)\right),
\qquad \tau=0.80,
\]

\[
H(q)=-\sum_dq_d\log q_d
\]

を解きます。閉形式解は Gibbs 分布です。

\[
q_d^*=\frac{\exp(u_d/\tau)}
{\exp(u_F/\tau)+\exp(u_B/\tau)}.
\]

一方向の starvation を構造的に排除するため、\(\rho=1/8\) の一様分布と混合します。

\[
\pi_d=\rho+(1-2\rho)q_d^*,
\]

したがって Phase B では

\[
\frac18\le\pi_d\le\frac78.
\]

## 8. 決定論的 low-discrepancy rounding

forward debt \(a_k\) を

\[
a_{k+1}=a_k+\pi_F(k)-\mathbf1[d_k=F]
\]

として更新します。累積 debt が \(1/2\) 以上なら `F`、それ以外なら `B` を選びます。任意の prefix \(K\) について

\[
\left|N_F(K)-\sum_{k=1}^{K}\pi_F(k)\right|<1
\]

となり、乱数なしで再現可能な配分を得ます。

## 9. エントロピーによる chunk 制御

確信度を

\[
c=1-\frac{H(q^*)}{\log2}\in[0,1]
\]

とし、base budget \(B_0\) から

\[
B=B_0(1+3c^2)
\]

を計算します。Phase B は停止条件を頻繁に確認する必要があるため、常に

\[
B\le2B_0
\]

へ制限します。

## 10. 小規模グラフの scale gate

辺数が最初の ACBS budget tier 未満の場合、entropy state の更新を償却できる前に探索が終了します。そのため

\[
|E|<10{,}000
\]

では production ACBS の実行経路へ完全に退避します。CLI上の algorithm と scheduler version は `aegis-entropic` / `entropic-proof-rate-v2` を保ちますが、距離、展開、緩和、chunk、方向切替、初回上界は production と同一です。

この境界は新しい任意定数ではなく、既存 `acbsBaseEdgeBudget` の最初の tier 境界と一致させています。

## 11. 停止性

Phase A は既存 production scheduler と同一です。Phase B では各方向の配分が \(1/8\) 以上で、rounding 誤差は一 chunk 未満です。非停止実行を仮定すると両方向は無限回 chunk を受け取ります。有限グラフ上の label-setting frontier は有限個の状態しか確定できないため、scheduler 起因の starvation は停止を妨げません。

最短性は既存の feasible potential と \(L_2\ge U_2\) が担います。interior allocation は scheduler の公平性だけを保証します。

## 12. 実測結果

### 正確性

以下の評価で、全 query が baseline と同じ距離を返し、到達可能 query の `optimalityGap` は 0 でした。

- bundled Hatfield: 18,000 comparisons
- Andorra / Liechtenstein / Monaco: 12,000 comparisons
- Luxembourg / Bremen: 6,000 comparisons

### v1 から v2 への改善

v1 は Andorra で約 6–8%、Liechtenstein で約 2–4%遅く、初回上界を平均 180–697 展開遅らせる場合がありました。

v2 では全評価グラフで初回上界の平均差が 0 になりました。Andorra、Liechtenstein、Monaco の per-query geometric mean は production 比で約 `+0.09%` から `+0.63%`、Luxembourg と Bremen の seed 合算値は約 `+0.05%` から `+0.73%` でした。

Hatfield は 233 辺しかなく、scale gate 適用後は探索量が完全一致し、per-query geometric mean の差は約 `+0.01%` から `+0.19%` でした。

これらは候補の退行を大幅に抑えた証拠ですが、production より高速であるという主張ではありません。

## 13. 評価 gate

`aegis-entropic` をproductionへ昇格するには、少なくとも次を満たす必要があります。

1. exhaustive・randomized test の全 queryで Dijkstra と距離が一致する。
2. 到達可能な全 queryで `optimalityGap = 0`。
3. Tokyo の公開 workload を変更せず、mean・median・p95・p99・relaxed・expanded のいずれも 1% を超えて悪化しない。
4. 既知の scheduler tail を改善し、別 seed で新しい重大 tail を作らない。
5. Tokyo 以外の複数道路グラフでも同じ判定を行う。
6. allocation、race、Go 1.23 compatibility、主要 OS の CI を通過する。

## 14. 位置づけ

この設計は incumbent acquisition と proof closure の分離、online convex optimization、UCB型探索、entropy regularization、error-diffusion rounding を ACBS の証明進行制御へ組み合わせた engineering synthesis です。個々の数学要素の新規性を主張しません。
