# Aegis Max Exact

`research/aegis-max-exact` は、既存の厳密最短路実装を置き換えず、入力ごとの相性差と長いテールを複数の厳密解法で吸収する研究ブランチです。

## 設計

Aegis Max はまず既存の `search.Select` と `search.Explain` から第一候補を決めます。その次に、第一候補と探索形状が異なる古典的厳密解法を hedge として置き、ACBS と残りの厳密解法を fallback にします。

代表的な順序は次のようになります。

```text
A* が第一候補       : A* -> 双方向 Dijkstra -> ACBS -> Dijkstra
双方向 Dijkstra     : BiDijkstra -> A* または Dijkstra -> ACBS -> ...
Dijkstra            : Dijkstra -> BiDijkstra -> ACBS -> ...
```

固定順を「常に最速」とは扱いません。selector の予測仕事量も plan に保存し、後から実測と比較できるようにしています。

## 三つの実行モード

### latency

最大 `--parallel` 本をすぐ起動し、最初に正しい厳密解を返した runner を採用します。単発レイテンシと p95/p99 を優先する代わりに、CPU とメモリを追加で使います。

```bash
bin/aegis-max --graph graph.aegis --source 100 --target 200 \
  --mode latency --parallel 3
```

### balanced

第一候補だけを開始し、`--hedge-delay` を超えても終わらない場合に次の候補を起動します。通常ケースの余分な仕事を抑えつつ、遅いクエリだけ hedge するためのモードです。delay はハードウェア・グラフ依存なので自動で決めず、明示指定を要求します。

```bash
bin/aegis-max --graph graph.aegis --source 100 --target 200 \
  --mode balanced --parallel 2 --hedge-delay 2ms
```

### efficient

一度に一つだけ実行します。第一候補が失敗した場合だけ次へ進みます。スループットや省メモリを優先するときの比較基準です。

```bash
bin/aegis-max --graph graph.aegis --source 100 --target 200 \
  --mode efficient
```

## 検証と合意

`--verify` は成功した経路を `search.Validate` で再検証します。

さらに新しい runner を導入するときは `--consensus` を使えます。最初の成功だけでは返さず、別の厳密 runner が到達可能性と距離に同意するまで待ちます。二つの runner が異なる距離を返した場合は成功扱いにせずエラーにします。

これは一般の最適性証明を二重化する万能手段ではありません。既存 runner はそれぞれのアルゴリズム側で厳密性を保証し、consensus は統合時の追加検査として使います。

## 外部 runner

`RunWithRunners` を追加しています。新しい解法は `Runner` interface を実装すれば、`search.Run` の組み込み case を変更せずポートフォリオ層へ接続できます。

```go
type Runner interface {
    Name() search.Algorithm
    Run(context.Context, *graph.Graph, int, int) (search.Result, error)
}
```

この入口は、既存の研究ブランチにある CH/CCH 系、別言語 sidecar、将来の新しい厳密最短路アルゴリズムを比較するためのものです。統合前には必ず同じ graph、query pair、検証器で測定します。

## plan の確認

実行せず候補順と selector の説明だけ確認できます。

```bash
bin/aegis-max --graph graph.aegis --source 100 --target 200 --plan-only
```

実験で順序を固定したい場合は、組み込みアルゴリズムを明示できます。

```bash
bin/aegis-max --graph graph.aegis --source 100 --target 200 \
  --algorithms astar,bidijkstra,aegis
```

## P = NP との関係

道路の非負重み最短路はすでに P に属するため、`P = NP` が証明されてもこの問題の計算量クラス自体が変わるわけではありません。

ただし、将来より強い最短路アルゴリズム、前処理法、ハードウェア特化実装が現れた場合でも、runner interface により既存実装と同じ検証・計測条件へ差し込めます。未知の将来アルゴリズムに対する「永遠の最速」は保証できませんが、交換可能なポートフォリオ層にすることで陳腐化しにくくしています。

P vs NP を直接扱う研究は `research/complexity-lab` に分離しています。

## 採用ゲート

本線へ入れる前に、結果を見る前に比較条件を固定します。

1. Dijkstra との到達可能性・距離一致を維持する。
2. 同一 query pair を複数 seed・複数 graph で使用する。
3. median だけでなく p95、p99、最大ペナルティを比較する。
4. wall time と同時に CPU 使用量、expanded、relaxed、割り当て量、RSS を測る。
5. latency / balanced / efficient を別設定として報告する。
6. 前処理型 runner を追加する場合は preprocessing time と index size を含める。
7. 既存の CH/CCH 等の強い baseline と比較してから「強い」と判断する。

現在のブランチは、強い比較を可能にする実装基盤です。独立した大規模ベンチ結果が出る前に「常に最速」「世界最速」とは主張しません。
