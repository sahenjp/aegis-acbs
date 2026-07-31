# エントロピー正則化付き証明進行率 scheduler

この文書は、ACBS の production scheduler `edge-efficiency-v3` と並行して評価する実験的 variant `aegis-entropic` を定義します。既存の `aegis` は比較基準として変更しません。

> [!IMPORTANT]
> この variant は数学的構造と実装上の安定性を検証する候補です。大規模な複数都市・複数 seed の評価を通過するまで、production scheduler への昇格や性能優位は主張しません。

## 1. 不変な正確性境界

scheduler が変更するのは、次の有限 edge-work chunk を前向き・後ろ向きのどちらへ割り当てるかだけです。以下は変更しません。

\[
L_2=\min OPEN_F+\min OPEN_B,
\]

\[
U_2=2U+\phi_2(t)-\phi_2(s),
\]

\[
L_2\ge U_2\Longrightarrow U=\delta(s,t).
\]

balanced feasible potential、reduced edge cost、`g` label、接続候補、incumbent、停止条件は従来の ACBS と共通です。

## 2. 方向別の証明報酬

方向を \(d\in\{F,B\}\)、chunk 前後の有効な最小 reduced key を \(k_d,k'_d\) とします。選択した frontier にだけ進行量を帰属させます。

\[
\Delta k_d=\max\{0,k'_d-k_d\}.
\]

正規化 work は、確認辺数 \(E_d\)、展開頂点数 \(V_d\)、選択 queue の正の増加量を使います。

\[
w_d=E_d+4V_d+2\max\{0,Q'_d-Q_d\}.
\]

Laplace smoothing を入れた観測証明率は

\[
r_d=\frac{\Delta k_d+1}{w_d+1}
\]

です。反対側 queue の stale-entry 清掃などによる key 変化を、選択方向の報酬へ混入させません。

## 3. log-domain のロバスト推定

証明率は桁単位で変動するため、加法的平均ではなく log 空間で更新します。

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

です。例外的な一回の観測が直後の chunk を急激に拡大する正帰還を抑えます。

## 4. online utility

方向 \(d\) の観測回数を \(n_d\)、合計を \(N=n_F+n_B\) とすると、探索不足を補う項を

\[
b_d=\beta\sqrt{\frac{\log(N+2)}{n_d+1}},
\qquad \beta=0.45
\]

とします。

結合下界 \(L_2=k_F+k_B\) を押し上げる際、小さい key 側が bottleneck です。そこで

\[
\eta_F=\frac{k_B-k_F}{\max(k_F,k_B)+1},
\qquad \eta_B=-\eta_F
\]

を proof pressure とします。incumbent 発見前後で係数を変えます。

\[
\lambda_{phase}=
\begin{cases}
1.60,&U=\infty,\\
0.80,&U<\infty.
\end{cases}
\]

switch penalty \(\kappa=0.08\) を含む utility は

\[
u_d=m_d+b_d+\lambda_{phase}\eta_d
-\kappa\mathbf 1[d\ne d_{previous}].
\]

## 5. エントロピー正則化配分

二方向 simplex

\[
\Delta_2=\{q\in\mathbb R_{\ge0}^2:q_F+q_B=1\}
\]

上で、次を解きます。

\[
q^*=\arg\max_{q\in\Delta_2}
\left(\langle q,u\rangle+\tau H(q)\right),
\qquad \tau=0.80,
\]

\[
H(q)=-\sum_d q_d\log q_d.
\]

閉形式解は Gibbs 分布です。

\[
q_d^*=\frac{\exp(u_d/\tau)}
{\exp(u_F/\tau)+\exp(u_B/\tau)}.
\]

一方向の飢餓を構造的に排除するため、\(\rho=1/8\) の一様分布と混合します。

\[
\pi_d=\rho+(1-2\rho)q_d^*,
\]

したがって bootstrap 後は

\[
\frac18\le\pi_d\le\frac78.
\]

## 6. 決定論的 low-discrepancy rounding

実装は一つの chunk ごとに一方向しか処理できないため、forward debt \(a_k\) を保ちます。

\[
a_{k+1}=a_k+\pi_F(k)-\mathbf1[d_k=F].
\]

累積 debt が \(1/2\) 以上なら `F`、それ以外なら `B` を選びます。任意の prefix \(K\) について

\[
\left|N_F(K)-\sum_{k=1}^{K}\pi_F(k)\right|<1
\]

が成立し、確率乱数なしで再現可能な配分になります。

## 7. エントロピーによる chunk 制御

配分の確信度を

\[
c=1-\frac{H(q^*)}{\log2}\in[0,1]
\]

とし、base budget \(B_0\) から

\[
B=B_0(1+3c^2)
\]

を計算します。最大エントロピーでは \(B=B_0\)、一方向への確信が高いほど滑らかに最大 \(4B_0\) へ近づきます。incumbent 発見後は停止条件の再確認間隔を短くするため \(2B_0\) で上限を設けます。

## 8. 停止性

bootstrap 後は各方向の配分が \(1/8\) 以上であり、rounding 誤差は一 chunk 未満です。非停止実行を仮定すると、両方向は無限回 chunk を受け取ります。有限グラフ上の label-setting frontier は有限個の状態しか確定できないため、scheduler による一方向の starvation は停止を妨げません。

この議論は既存の最短性証明を置き換えるものではありません。既存の feasible potential と \(L_2\ge U_2\) が最短性を担い、interior allocation は scheduler 起因の starvation を排除します。

## 9. 評価 gate

`aegis-entropic` を `aegis` の代わりに採用するには、少なくとも次を満たす必要があります。

1. exhaustive・randomized test の全 query で Dijkstra と距離が一致する。
2. 到達可能な全 query で `optimalityGap = 0`。
3. Tokyo の公開 workload を変更せず、mean・median・p95・p99・relaxed・expanded のいずれも 1% を超えて悪化しない。
4. 既知の scheduler tail を改善し、別 seed で新しい重大 tail を作らない。
5. Tokyo 以外の道路グラフでも同じ判定を行う。
6. allocation、race、Go 1.23 compatibility、主要 OS の CI を通過する。

## 10. 位置づけ

この設計は online convex optimization、UCB 型探索、entropy regularization、error-diffusion rounding を ACBS の証明進行制御へ組み合わせた engineering synthesis です。個々の数学要素の新規性を主張しません。production 比較では、前処理を使わない同一探索基盤上の `aegis` を直接 baseline とし、CH・MLD・landmark 系ルータとは別軸で評価します。
