#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OLD_TAG="${OLD_TAG:-v0.5.0-experimental}"
OUT="${1:-$ROOT/artifacts/allocation-comparison}"
BENCHTIME="${BENCHTIME:-20x}"
COUNT="${COUNT:-3}"
TMP="$(mktemp -d)"
cleanup() {
  git -C "$ROOT" worktree remove --force "$TMP/old" >/dev/null 2>&1 || true
  git -C "$ROOT" worktree remove --force "$TMP/current" >/dev/null 2>&1 || true
  rm -rf "$TMP"
}
trap cleanup EXIT
mkdir -p "$OUT"

git -C "$ROOT" worktree add --detach "$TMP/old" "$OLD_TAG" >/dev/null
git -C "$ROOT" worktree add --detach "$TMP/current" HEAD >/dev/null

cat > "$TMP/probe_test.go" <<'PROBE'
package search

import (
  "context"
  "testing"
  "github.com/sahenjp/aegis-acbs/internal/graph"
)

func allocationComparisonGrid(b *testing.B, rows, cols int) *graph.Graph {
  b.Helper()
  n := rows * cols
  g := graph.New("allocation-grid", "generated", "car", graph.MetricDistance)
  g.Nodes = make([]graph.Node, n)
  g.Adj = make([][]graph.Edge, n)
  for r := 0; r < rows; r++ {
    for c := 0; c < cols; c++ {
      i := r*cols + c
      g.Nodes[i] = graph.Node{ID: int64(i+1), Lat: 35+float64(r)*0.001, Lon: 139+float64(c)*0.001}
      if c > 0 { g.Adj[i] = append(g.Adj[i], graph.Edge{To:i-1, Cost:100000}) }
      if c+1 < cols { g.Adj[i] = append(g.Adj[i], graph.Edge{To:i+1, Cost:100000}) }
      if r > 0 { g.Adj[i] = append(g.Adj[i], graph.Edge{To:i-cols, Cost:100000}) }
      if r+1 < rows { g.Adj[i] = append(g.Adj[i], graph.Edge{To:i+cols, Cost:100000}) }
    }
  }
  if err := g.Finalize(); err != nil { b.Fatal(err) }
  return g
}

func BenchmarkACBSAllocationComparison(b *testing.B) {
  g := allocationComparisonGrid(b, 180, 180)
  ctx := context.Background()
  if _, err := Run(ctx, g, 0, len(g.Nodes)-1, Aegis); err != nil { b.Fatal(err) }
  b.ReportAllocs()
  b.ResetTimer()
  for i := 0; i < b.N; i++ {
    r, err := Run(ctx, g, 0, len(g.Nodes)-1, Aegis)
    if err != nil || !r.Stats.Reachable { b.Fatalf("reachable=%v err=%v", r.Stats.Reachable, err) }
  }
}
PROBE

run_probe() {
  local dir="$1" output="$2"
  cp "$TMP/probe_test.go" "$dir/internal/search/allocation_comparison_temp_test.go"
  (cd "$dir" && GOMAXPROCS=1 go test ./internal/search -run '^$' -bench '^BenchmarkACBSAllocationComparison$' -benchmem -benchtime="$BENCHTIME" -count="$COUNT") | tee "$output"
  rm -f "$dir/internal/search/allocation_comparison_temp_test.go"
}

run_probe "$TMP/old" "$OUT/old.txt"
run_probe "$TMP/current" "$OUT/current.txt"

python3 - "$OUT/old.txt" "$OUT/current.txt" "$OUT/summary.json" "$OUT/summary.md" "$OLD_TAG" "$(cat "$ROOT/VERSION")" <<'PY'
import json, re, statistics, sys
old_file, current_file, json_out, md_out, old_version, current_version = sys.argv[1:]
pat = re.compile(r'BenchmarkACBSAllocationComparison(?:-\d+)?\s+\d+\s+(\d+) ns/op\s+(\d+) B/op\s+(\d+) allocs/op')
def parse(path):
    rows=[]
    for line in open(path):
        m=pat.search(line)
        if m: rows.append(tuple(map(int,m.groups())))
    if not rows: raise SystemExit(f'no benchmark rows in {path}')
    return {
      'runs': len(rows),
      'nsPerOpMedian': int(statistics.median(r[0] for r in rows)),
      'bytesPerOpMedian': int(statistics.median(r[1] for r in rows)),
      'allocsPerOpMedian': int(statistics.median(r[2] for r in rows)),
      'raw': [{'nsPerOp':r[0],'bytesPerOp':r[1],'allocsPerOp':r[2]} for r in rows],
    }
old=parse(old_file); cur=parse(current_file)
def reduction(a,b): return (a-b)*100/a if a else 0
def change_label(value):
    if value > 0: return f'{value:.1f}% lower'
    if value < 0: return f'{abs(value):.1f}% higher'
    return 'unchanged'
summary={
 'fixture':'180x180 generated directed-symmetric road grid, corner-to-corner ACBS route',
 'oldVersion':old_version,'currentVersion':current_version,
 'old':old,'current':cur,
 'improvement':{
   'latencyPercent': reduction(old['nsPerOpMedian'],cur['nsPerOpMedian']),
   'bytesPercent': reduction(old['bytesPerOpMedian'],cur['bytesPerOpMedian']),
   'allocationsPercent': reduction(old['allocsPerOpMedian'],cur['allocsPerOpMedian']),
 }
}
open(json_out,'w').write(json.dumps(summary,indent=2)+'\n')
text=f'''# ACBS allocation comparison

Fixture: {summary['fixture']}

| Metric | {old_version} | {current_version} | Change |
|---|---:|---:|---:|
| latency | {old['nsPerOpMedian']/1e6:.3f} ms/op | {cur['nsPerOpMedian']/1e6:.3f} ms/op | {change_label(summary['improvement']['latencyPercent'])} |
| allocated bytes | {old['bytesPerOpMedian']:,} B/op | {cur['bytesPerOpMedian']:,} B/op | {change_label(summary['improvement']['bytesPercent'])} |
| allocations | {old['allocsPerOpMedian']:,} allocs/op | {cur['allocsPerOpMedian']:,} allocs/op | {change_label(summary['improvement']['allocationsPercent'])} |

The current version retains priority-queue backing arrays in pooled workspaces. The remaining steady-state allocation is the exact-sized returned path.
'''
open(md_out,'w').write(text)
print(text)
PY
