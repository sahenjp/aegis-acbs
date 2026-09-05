#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ROUTINGKIT_DIR="${ROUTINGKIT_DIR:-${1:-}}"
OUT="${2:-$ROOT/bin/aegis-routingkit-cch-server}"

if [[ -z "$ROUTINGKIT_DIR" ]]; then
  echo "usage: ROUTINGKIT_DIR=/path/to/RoutingKit bash scripts/build-routingkit-cch-server.sh [RoutingKit-dir] [output]" >&2
  exit 2
fi
if [[ ! -f "$ROUTINGKIT_DIR/include/routingkit/customizable_contraction_hierarchy.h" ]]; then
  echo "RoutingKit CCH headers were not found under $ROUTINGKIT_DIR" >&2
  exit 2
fi
if [[ ! -f "$ROUTINGKIT_DIR/lib/libroutingkit.a" && ! -f "$ROUTINGKIT_DIR/lib/libroutingkit.so" ]]; then
  echo "RoutingKit library was not found under $ROUTINGKIT_DIR/lib; build RoutingKit first" >&2
  exit 2
fi

mkdir -p "$(dirname "$OUT")"
g++ -O3 -DNDEBUG -std=c++17 \
  -I"$ROUTINGKIT_DIR/include" \
  "$ROOT/experimental/routingkit/cch_server.cpp" \
  -L"$ROUTINGKIT_DIR/lib" -lroutingkit \
  -lz -fopenmp -pthread -lm \
  -o "$OUT"

echo "$OUT"
