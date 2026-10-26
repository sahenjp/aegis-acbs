#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GRAPH="${1:-$ROOT/artifacts/hatfield-uk.aegis}"
OUT="${2:-$ROOT/artifacts/tag-comparison}"
OLD_TAG="${OLD_TAG:-v0.2.0-experimental}"
QUERIES="${QUERIES:-100}"
REPEATS="${REPEATS:-5}"
BATCH="${BATCH:-8}"
mkdir -p "$OUT"
GRAPH="$(realpath "$GRAPH")"
TMP="$(mktemp -d)"
trap 'git -C "$ROOT" worktree remove --force "$TMP/old" >/dev/null 2>&1 || true; rm -rf "$TMP"' EXIT

git -C "$ROOT" worktree add --detach "$TMP/old" "$OLD_TAG" >/dev/null
CURRENT_VERSION="$(cat "$ROOT/VERSION")"
OLD_VERSION="$(cat "$TMP/old/VERSION")"
go build -trimpath -ldflags "-X github.com/sahenjp/aegis-acbs/internal/version.Version=$CURRENT_VERSION" -o "$TMP/current-aegis" "$ROOT/cmd/aegis"
(
  cd "$TMP/old"
  go build -trimpath -ldflags "-X github.com/sahenjp/aegis-acbs/internal/version.Version=$OLD_VERSION" -o "$TMP/old-aegis" ./cmd/aegis
)

run_one() {
  local binary="$1" output="$2" order_arg="${3:-}"
  local extra=()
  [[ -n "$order_arg" ]] && extra+=(--order "$order_arg")
  GOMAXPROCS=1 "$binary" benchmark \
    --graph "$GRAPH" --queries "$QUERIES" --repeats "$REPEATS" --batch "$BATCH" \
    --suite mixed --pair-mode strongly-connected --seed 1010 \
    --algorithms dijkstra,bidijkstra,astar,aegis \
    "${extra[@]}" \
    --output "$output" --html "${output%.json}.html"
}
run_one "$TMP/old-aegis" "$OUT/old.json"
# Rotated mode isolates implementation/telemetry changes from the new default
# interleaving methodology when comparing against v0.3.
run_one "$TMP/current-aegis" "$OUT/current.json" rotated

python3 - "$OUT/old.json" "$OUT/current.json" "$OUT/summary.md" <<'PY'
import json, sys
old=json.load(open(sys.argv[1])); cur=json.load(open(sys.argv[2]))
def row(d, name): return next(x for x in d['summary'] if x['algorithm']==name)
o=row(old,'aegis'); c=row(cur,'aegis')
def latency_change(a,b):
    if not a: return 'n/a'
    pct=(b-a)*100/a
    if pct < 0: return f'{abs(pct):.1f}% faster'
    if pct > 0: return f'{pct:.1f}% slower'
    return 'unchanged'
def work_change(a,b):
    if not a: return 'n/a'
    pct=(b-a)*100/a
    if pct < 0: return f'{abs(pct):.1f}% lower'
    if pct > 0: return f'{pct:.1f}% higher'
    return 'unchanged'
text=f'''# ACBS tag comparison

| Metric | {old["version"]} | {cur["version"]} | Change |
|---|---:|---:|---:|
| p50 | {o["medianNs"]/1e3:.2f} µs | {c["medianNs"]/1e3:.2f} µs | {latency_change(o["medianNs"],c["medianNs"])} |
| p95 | {o["p95Ns"]/1e3:.2f} µs | {c["p95Ns"]/1e3:.2f} µs | {latency_change(o["p95Ns"],c["p95Ns"])} |
| median relaxed | {o["medianRelaxed"]} | {c["medianRelaxed"]} | {work_change(o["medianRelaxed"],c["medianRelaxed"])} |
| correct | {o["correct"]}/{o["runs"]} | {c["correct"]}/{c["runs"]} | — |
'''
open(sys.argv[3],'w').write(text)
print(text)
PY
