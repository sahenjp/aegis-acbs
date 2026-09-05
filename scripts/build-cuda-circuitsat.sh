#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="${1:-$ROOT/bin/aegis-circuitsat-cuda}"

if ! command -v nvcc >/dev/null 2>&1; then
  echo "nvcc was not found; install a CUDA toolkit to build the optional Circuit-SAT backend" >&2
  exit 2
fi

mkdir -p "$(dirname "$OUT")"
nvcc \
  -O3 \
  -std=c++17 \
  --use_fast_math=false \
  "$ROOT/experimental/cuda/circuitsat.cu" \
  -o "$OUT"

echo "$OUT"
