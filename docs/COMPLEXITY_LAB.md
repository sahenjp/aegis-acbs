# Aegis Complexity Lab

`research/complexity-lab` は、Aegis の厳密探索・検証思想を計算量研究へ持ち込むための独立した研究ブランチです。

## 現在の到達点

このブランチは三つの基準器と一つの任意GPU backendを持ちます。

1. **MCSP exact solver** — 真理値表から最小 NAND 回路をゲート数順に完全探索する。
2. **Boolean function catalog** — 1〜4入力の関数について、発見済み最小ゲート数の分布を一度の探索で収集する。
3. **Circuit-SAT baseline** — NAND 回路を64割り当てずつビット並列評価し、CPU並列で厳密に SAT を解く。
4. **CUDA sidecar** — Circuit-SAT の割り当て評価をGPUへ移す任意backend。通常のGoビルドにはCUDAを要求しない。

いずれも「速そうだから枝を捨てる」ことと「安全に枝を捨てられる」ことを分離します。有限実験を P vs NP の証明として扱いません。

## MCSP

真理値表は `uint64` で持つため、単一ターゲット探索は1〜6入力です。探索はゲート数ごとの幅優先探索なので、状態上限・時間制限に達せずに解が得られた場合、その NAND 回路サイズはこのモデルで最小です。

```bash
go build -o bin/aegis-lab ./cmd/aegis-lab

bin/aegis-lab mcsp \
  --inputs 2 \
  --target 0x8 \
  --max-gates 4 \
  --max-states 100000 \
  --workers 8 \
  --verify-minimal
```

`0x8` は2入力 AND の真理値表です。NAND 基底では2ゲートが最小です。

### 厳密下界

現在の最初の下界は、出力が依存する入力変数の個数を使います。出力が `k` 個の異なる入力に本質的に依存するなら、fan-in 2 の回路でそれらを一つの出力へ接続するには少なくとも `k-1` 個のゲートが必要です。

この値は弱いことがありますが、安全です。`--max-gates` がこの下界より小さければ、探索を行わずに不可能と判定できます。

### CPU 並列

同じ深さの状態展開を複数 worker で行います。結果の統合順序は元の frontier 順に固定しているため、worker 数を増やしても最小性の意味は変わりません。

## 全 Boolean 関数カタログ

```bash
bin/aegis-lab catalog \
  --inputs 3 \
  --max-gates 8 \
  --max-states 2000000 \
  --workers 8
```

1〜4入力に限定しています。4入力では関数総数が65,536、5入力では4,294,967,296になるため、無条件の全列挙を通常コマンドの対象にしません。

出力には、各真理値表の最小ゲート数、構造下界、ゲート数分布、探索した状態数、カタログが完全かどうかを含みます。`--include-circuits` を付けると、各関数の代表最小回路も保存します。

## Circuit-SAT

NAND 回路 JSON を入力し、64個の割り当てを1個の `uint64` に詰めて同時評価します。複数 worker は連続した assignment block を wave 単位で処理するため、SAT の場合は数値的に最小の充足割り当てを再現可能に返します。

```bash
bin/aegis-lab circuit-sat \
  --circuit research/examples/and-circuit.json \
  --backend cpu \
  --workers 8
```

SAT の場合は返した割り当てを独立評価します。UNSAT の場合は全 `2^n` 割り当てを評価し終えたときだけ `complete=true` になります。

## CUDA backend

CUDAは通常ビルドから完全に分離しています。CUDA Toolkitがある環境だけでsidecarをビルドします。

```bash
bash scripts/build-cuda-circuitsat.sh

bin/aegis-lab circuit-sat \
  --circuit research/examples/and-circuit.json \
  --backend cuda \
  --cuda-bin bin/aegis-circuitsat-cuda \
  --cuda-chunk 1048576
```

`experimental/cuda/circuitsat.cu` は割り当てを昇順のchunkに分け、各chunkをCUDA threadへ割り当てます。chunk内では `atomicMin` で最小の充足割り当てを集約し、chunk自体も昇順に処理するため、最初に見つかった結果は全体でも最小の充足割り当てです。

Go側とGPU側は `AEGIS_CIRCUITSAT_CUDA_V1` という小さい標準入出力protocolで接続します。GPUがSATを返した場合、Go側の `VerifyAssignment` で必ず証人を再評価してから受理します。

UNSATは短い証人を返せないので、GPU sidecarの完全走査結果を追加確認したい場合は `--cross-check-unsat` を使ってCPU referenceでも全探索できます。これは検証コストと引き換えの研究用オプションです。

CUDA sourceは通常CIの対象外です。CUDAコンパイラ・GPUが利用できる環境でのビルドとCPU/GPU差分テストを行うまでは、GPU性能や実機互換性を確認済みとは扱いません。

## なぜ Circuit-SAT を置くのか

Williams 型の「アルゴリズムから回路下界へ」という研究では、制限回路クラスに対する Circuit-SAT を自明な `2^n` より速くすることが重要になります。現在の bit-parallel CPU/GPU solver はその定理を満たす新しい漸近的アルゴリズムではなく、比較対象となる厳密な基準器です。

今後は回路クラスごとにbackendを分け、改善を `checked assignments`、実時間、回路サイズ、入力数で測ります。

## P = NP が解決された場合

`Solver` と verifier を分離しているため、将来 SAT や関連問題に実用的な多項式時間アルゴリズムが得られた場合、その実装を新backendとして追加できます。

ただし `P = NP` だけでは実用最速を意味しません。次数、定数、メモリ、並列性、前処理、証明生成コストが残ります。この Lab は新旧backendを同じ検証条件で比較する場所として維持します。

## 研究上の境界

- NAND 回路モデルは一般の MCSP の全回路モデルを代表しない。
- 6入力制限は現在の真理値表表現による実装上の制限。
- `MaxStates` や timeout に達した結果を最小性の証拠に使わない。
- 全関数カタログが部分的な場合、未発見関数の回路サイズを推測しない。
- 有限サイズの成長曲線から漸近的回路下界を主張しない。
- 相対化・自然証明・代数化の障壁を回避したと、実験だけで主張しない。

## 近年の関連方向

- Ryan Williams, **Self-Improvement for Circuit-Analysis Problems**, ECCC TR23-082。
- Rahul Ilango, **SAT Reduces to the Minimum Circuit Size Problem with a Random Oracle**, ECCC TR23-165、2025改訂。
- Edward Hirsch / Ilya Volkovich, **A Note on Avoid vs MCSP**, ECCC TR25-220。
- Hanlin Ren / Ryan Williams, **Near-Maximum Circuit Lower Bounds for Exponential Time with Merlin-Arthur Queries**, ECCC TR26-118。

MCSP の一般的な NP-hardness は現在も未解決です。したがって、このブランチの目的は「P vs NP を解いたように見せる」ことではなく、反例・完全探索結果・下界候補を再現可能な形で積み上げ、一般定理へ変換できる材料を作ることです。
