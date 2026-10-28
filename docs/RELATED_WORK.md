<div align="center">

# 関連研究システムマップ

**ACBSを双方向探索研究史のどこに置くか。系統別の文献マップと、各文献が何を主張したか。**

![Scope](https://img.shields.io/badge/scope-bidirectional%20search-2563eb)
![Refs](https://img.shields.io/badge/verified%20references-41-16a34a)
![Claims](https://img.shields.io/badge/claims-conservative-16a34a)

[ドキュメント一覧](README.md) · [論文](PAPER.md) · [アルゴリズム](ALGORITHM.md) · [トップ](../README.ja.md)

</div>

---

> [!IMPORTANT]
> 本文書は優先権や新規性を主張するものではありません。文献のDOIとarXiv IDはCrossref / arXiv APIで照合済みですが、記述は各論文の要旨・導入に基づく簡潔な要約であり、数式レベルの忠実な再現ではありません。

## 使い方

[番号] は `docs/PAPER.md` の References の通し番号です。「ACBSとの関係」列は、本実装がその文献をどう踏まえたかを一行で示します。

---

## A. 最短路探索の基礎（1949-1968）

| # | 文献 | 何を主張したか | ACBSとの関係 |
|---|---|---|---|
| [1] | Dijkstra 1959 | 非負重みグラフの単一始点最短路をO(m log n)級で解くラベル確定法 | 全アルゴリズムのbaseline。双方向版も含めて比較対象 |
| [2] | Hart, Nilsson & Raphael 1968 | 評価関数f=g+hと、許容性・単調性（整合性）の条件 | 本論文のreduced costとlabel settingの前提 |
| [3] | Nicholson 1966 | 2点間最短路を両端から解く双方向形式の初期提案 | 双方向の起源。停止条件の議論は未整備 |

## B. 双方向探索の黎明（1969-1997）

| # | 文献 | 何を主張したか | ACBSとの関係 |
|---|---|---|---|
| [4] | Pohl 1971 | 双方向ヒューリスティック探索を設計・実装した最初の研究。カージナリティ比較 | 本論文§3.6のside-selectionの直接の先行研究 |
| [5] | Pohl 1969（博士論文） | 双方向探索の理論的枠組みとカージナリティ基準の定義 | [4]の原典。境界条件の議論の出所 |
| [6] | Sint & de Champeaux 1977 | BHFFA。front-to-frontヒューリスティックで双方向A*の展開を減らす | front-to-front系の起点。本論文はfront-to-end |
| [7] | de Champeaux 1983 | BHFFA2。より厳密な停止条件を導入し、BHFFAの不備を修正 | 「停止条件が難所」である最初の明確化 |
| [8] | Kwa 1989 | BS*。段階的な双方向探索で許容性を保つ | 段階的アプローチ。本論文は一段階 |
| [9] | Kaindl & Kainz 1997 | 従来の評価が誤りだったとし、frontierすれ違い仮説を否定。双方向探索の再評価 | 双方向の優位がheuristic精度に依存する点の古典的根拠 |

## C. meet-in-the-middle と停止条件の理論（2015-2025）

| # | 文献 | 何を主張したか | ACBSとの関係 |
|---|---|---|---|
| [10] | Holte et al. 2016 (AAAI) | MM。両方向のfrontier最小値を用い、meet-in-the-middleを保証する停止条件 | MM/MMeの予稿版 |
| [11] | Holte et al. 2017 (AIJ) | MMの完全版。gminF+gminB+ε型の停止条件と優先度の理論 | 本論文§4で「g-boundではない」と比較した対象 |
| [12] | Chen et al. 2017 (NBS) | must-expand pairを定義し、頂点被覆VCの2倍以内で済むfront-to-end手法 | s.e. pairの概念。本論文は上界探索なので直接比較外 |
| [13] | Eckerle et al. 2017 | 双方向探索で必ず展開されるノードの十分条件 | s.e.性の理論的土台 |
| [14] | Shaham et al. 2017 | must-expand pairの最小集合を特徴づける | 下界の定義に影響 |
| [15] | Barker & Korf 2015 | front-to-end双方向探索の限界を示す負の結果 | 「双方向が常に得とは限らない」根拠 |
| [16] | Shperberg et al. 2019a | 下界の伝播（bounds propagation）で双方向探索を改善 | 下界の強め方の先行研究 |
| [17] | Shperberg et al. 2019b | 非パラメトリックな双方向探索を拡充 | パラメータ選択の柔軟性 |
| [18] | Shaham et al. 2019 | 最適効率の双方向探索 | 最適性の理論的基準 |
| [19] | Shperberg et al. 2020 (IJCAI) | 下界でノードを展開する手法。well-behavednessの概念 | 本論文§6.4でheuristic強度依存の説明に使用 |
| [20] | Alcázar, Riddle & Barley 2020 | BAE*。個別上界とheuristic不正確性を統一的に扱う | BAE*系の基準実装 |
| [21] | Sewell & Jacobson 2021 | DVCBS。閉ループで下界を動的に改善する双方向探索 | 下界の動的更新という発想の近縁 |
| [22] | Pavlik, Sewell & Jacobson 2022 | DVCBSの反復深化版（IDBA*） | メモリ制約下の双方向 |
| [23] | Rice & Tsotras 2021 | 加法的近似上界付き双方向A* | 有界劣最適の双方向版 |
| [24] | Hu & Speck 2022 | 古典プランニングにおけるBAE*の実験的解析 | BAE*の実務的評価 |
| [25] | Siag et al. 2023 (IJCAI) | 17個の非劣グローバル下界を列挙し、最大値が最も強いと証明。3決定フレームワーク | 本論文§3.6・§4の直接の枠組み |
| [26] | Siag et al. 2023 (SOCS) | front-to-frontとfront-to-endの比較 | 本論文がfront-to-endを選んだ理由づけ |
| [27] | Wang et al. 2025 (IJCAI) | MMの停止条件をより強くし、全候補を尽くさず早期終了。MMPと最適性を保つ | 「停止条件はまだ改善余地がある」最新の到達点 |
| [28] | Siag & Shperberg 2025 (AIJ) | 理論と実践の橋渡し。front-to-end整合ヒューリスティクスの体系化 | 本論文のfront-to-end設計の理論的位置 |
| [29] | Shperberg et al. 2025 (SOCS) | 有界劣最適な双方向探索（整合ヒューリスティクス） | 劣最適への拡張 |

## D. サーベイと後続研究

| # | 文献 | 何を主張したか | ACBSとの関係 |
|---|---|---|---|
| [30] | Sturtevant & Felner 2018 | 双方向探索の簡潔な研究史と近年の成果 | 本論文の歴史記述の出典 |
| [31] | Sturtevant et al. 2020 | 双方向探索の有効性を事前に予測する指標 | 「いつ双方向が効くか」の判定 |
| [32] | Siag et al. 2024 | 並列・外部メモリ双方向探索（PEM-BAE*） | 本論文は単一プロセス・in-memory |
| [33] | Wang et al. 2026 | [27]の停止条件をサンプリングベース運動計画へ適用（BTIT*） | tight terminationの応用展開 |
| [34] | Shubi et al. 2026 | 最長路問題への双方向探索（front-to-front） | 最大化問題への拡張 |

## E. 道路ネットワークの高速化技法

| # | 文献 | 何を主張したか | ACBSとの関係 |
|---|---|---|---|
| [35] | Goldberg & Harrelson 2005 | ALT法。A*と前処理ランドマークを組み合わせる | 本論文は前処理なしで比較 |
| [36] | Geisberger et al. 2008 | Contraction Hierarchies（CH）。前処理で階層を作る | 前処理ありの比較対象外 |
| [37] | Geisberger et al. 2012 | CHの大規模道路網での厳密性と性能 | 実務ベースライン |
| [38] | Bast et al. 2016 | 経路計画アルゴリズムの大規模サーベイ | 実務的定位 |
| [39] | Abraham et al. 2010 | Highway dimension。道路網で効く理論的理由 | なぜ道路網が特殊か |
| [40] | Felner et al. 2010 | Single-frontier bidirectional search | 単一優先度キューでの双方向 |
| [41] | Al-Ayyoub 2005 | 分散環境での単方向・双方向ヒューリスティック探索 | 並列化の文脈 |

---

## 本論文（ACBS）の位置づけ

### 借りているもの（新規性なし）

- **双方向＋許容heuristic**：[2][4][9]
- **上界（incumbent）によるpruning**：[1][2]
- **meet-in-the-middleを保証する考え方**：[10][11]
- **side-selectionの基本方針（カージナリティ・交互）**：[4][5]
- **3決定フレームワーク**：[25]

### 置かれている位置

ACBSのスケジューラは、**[25] Siag et al. 2023 のフレームワークが「開いている」方向決定フック**の中に位置します。彼らは方向決定をper-nodeで行いますが、ACBSはchunk単位で行い、下界の上昇量を作業量で正規化したスコアで選びます。この「作業量で正規化する」点と「閉ループでbudgetを変える」点が、[25] のフレームワークには存在しない要素です。

### 主張しないこと

- 新しい停止条件であること。[27] Wang et al. 2025 が停止条件の改善方向を示しており、本論文はそれに到達していない。
- MM / NBS / BAE* / DVCBS を上回ること。これらは実装比較していない。
- 道路網一般で優位であること。単一都市・単一マシン。

### 未解決の比較課題

| 比較対象 | 何を比較すべきか | 現状 |
|---|---|---|
| [12] NBS | must-expand pairに対する展開数 | 未実装 |
| [20] BAE* | 個別上界の精度と計算コスト | 未実装 |
| [21] DVCBS | 動的下界更新との重複度 | 未実装 |
| [27] tight termination | 停止判定の回数と tail への影響 | 未実装 |
| [25] Siag系 | per-node方向決定との厳密な比較 | 未実装 |

これらが埋まるまで、「ACBSのスケジューラが既存の方向決定ポリシーより良い」とは言えません。

---

## 文献検証の記録

- DOI: Crossref API（`api.crossref.org/works/<DOI>`）で書籍名・著者・巻・ページを照合
- arXiv: arXiv API（`export.arxiv.org/api/query?id_list=...`）でタイトル・著者・公開日を照合
- DOIのない文献（[4], [35]など初期の会議録・論文集）：二次インデックス経由で書誌を確認。初版で `[40]` に付けたIkeda et al. 1994のDOIは照合の結果**別論文のDOI**であったため、当該項目を撤回し、検証済みのFelner et al. 2010とAl-Ayyoub 2005に置き換えました。

---

<div align="center">

[論文](PAPER.md) · [アルゴリズム](ALGORITHM.md) · [コントリビューション](../CONTRIBUTING.md)

</div>
