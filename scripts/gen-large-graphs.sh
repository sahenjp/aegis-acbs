#!/usr/bin/env bash
# Generate synthetic large road networks and validate them end to end.
#
# Produces, in $OUT (default /tmp/aegis-graphs):
#   {10k,100k,1m}.dimacs        DIMACS shortest-path arcs, integer metre costs
#   {10k,100k,1m}.co            DIMACS coordinates (lon lat), required by A*
#   {10k,100k,1m}.aegis         imported Aegis graphs
#   {10k,100k,1m}.bench.json    200-query benchmark reports
#   graphs.json                 node/edge/benchmark summary
#
# The generator is an ordinary command in this module, so a contributor needs
# nothing beyond the toolchain that already builds bin/aegis.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="${AEGIS_GRAPH_DIR:-/tmp/aegis-graphs}"
BIN="${AEGIS_BIN_DIR:-$ROOT/bin}"
QUERIES="${QUERIES:-200}"
REPEATS="${REPEATS:-9}"
SEED="${SEED:-1010}"
ALGORITHMS="${ALGORITHMS:-dijkstra,bidijkstra,astar,aegis,aegis-static}"

# Sizes are requested by target count; the grid rounds down to a perfect square.
SIZES=("10000:10k" "100000:100k" "1000000:1m")

mkdir -p "$OUT" "$BIN"
cd "$ROOT"
go build -trimpath -o "$BIN/aegis" ./cmd/aegis
go build -trimpath -o "$BIN/gen-grid" ./scripts/gen

for spec in "${SIZES[@]}"; do
  nodes="${spec%%:*}"
  name="${spec##*:}"
  "$BIN/gen-grid" -nodes "$nodes" -out "$OUT" -name "$name"
  "$BIN/aegis" import-dimacs \
    --graph "$OUT/$name.dimacs" --coords "$OUT/$name.co" \
    --output "$OUT/$name.aegis" --name "grid-$name"
  "$BIN/aegis" benchmark \
    --graph "$OUT/$name.aegis" --queries "$QUERIES" --repeats "$REPEATS" --seed "$SEED" \
    --algorithms "$ALGORITHMS" --html "" --output "$OUT/$name.bench.json"
done

# Collect measured node/edge counts and per-algorithm medians into one file.
python3 "$ROOT/scripts/gen-summarize.py" "$OUT"
