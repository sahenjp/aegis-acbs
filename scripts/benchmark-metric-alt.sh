#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 ]]; then
  echo "usage: $0 GRAPH [OUTPUT_DIR]" >&2
  exit 2
fi

graph=$1
out_dir=${2:-artifacts/metric-alt}
queries=${QUERIES:-1000}
repeats=${REPEATS:-9}
seeds=(${SEEDS:-1010 424242 20260717})

if [[ ! -f "$graph" ]]; then
  echo "graph not found: $graph" >&2
  exit 1
fi
if (( queries <= 0 )); then
  echo "QUERIES must be positive" >&2
  exit 2
fi
if (( repeats <= 0 || repeats % 2 == 0 )); then
  echo "REPEATS must be a positive odd number" >&2
  exit 2
fi

mkdir -p "$out_dir"
go build -o "$out_dir/aegis" ./cmd/aegis

for seed in "${seeds[@]}"; do
  run_dir="$out_dir/seed-$seed"
  mkdir -p "$run_dir"
  "$out_dir/aegis" benchmark \
    --graph "$graph" \
    --queries "$queries" \
    --repeats "$repeats" \
    --order interleaved \
    --measure-memory \
    --suite mixed \
    --seed "$seed" \
    --algorithms aegis,aegis-alt \
    --output "$run_dir/report.json" \
    --html "$run_dir/report.html" \
    2> >(tee "$run_dir/preprocess.log" >&2)
done
