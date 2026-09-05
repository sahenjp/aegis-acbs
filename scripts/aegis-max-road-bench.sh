#!/usr/bin/env bash
set -euo pipefail

DATASET="${1:-andorra}"
QUERIES="${2:-300}"
METRIC="${3:-time}"
ALT_LANDMARKS="${4:-16}"
OUT_DIR="${5:-road-bench-out}"
SEED="${AEGIS_BENCH_SEED:-424242}"
ROUTINGKIT_DIR="${ROUTINGKIT_DIR:-${RUNNER_TEMP:-/tmp}/RoutingKit}"
BIN_DIR="${AEGIS_BENCH_BIN_DIR:-${RUNNER_TEMP:-/tmp}/aegis-max-road-bin}"

mkdir -p "$OUT_DIR" "$BIN_DIR"

case "$DATASET" in
  andorra)
    URL="https://download.geofabrik.de/europe/andorra-latest.osm.pbf"
    PBF="$OUT_DIR/andorra.osm.pbf"
    OSM="$OUT_DIR/andorra.osm"
    ;;
  tokyo)
    URL="https://download.geofabrik.de/asia/japan/kanto-latest.osm.pbf"
    PBF="$OUT_DIR/kanto.osm.pbf"
    OSM="$OUT_DIR/tokyo.osm"
    ;;
  japan)
    URL="https://download.geofabrik.de/asia/japan-latest.osm.pbf"
    PBF="$OUT_DIR/japan.osm.pbf"
    OSM="$OUT_DIR/japan.osm"
    ;;
  *)
    echo "unknown dataset: $DATASET (expected andorra, tokyo, or japan)" >&2
    exit 2
    ;;
esac

for tool in curl osmium go g++ make python3; do
  command -v "$tool" >/dev/null || { echo "missing required tool: $tool" >&2; exit 2; }
done

if [[ ! -d "$ROUTINGKIT_DIR/.git" ]]; then
  git clone https://github.com/RoutingKit/RoutingKit.git "$ROUTINGKIT_DIR"
fi
git -C "$ROUTINGKIT_DIR" fetch --depth=1 origin 54d49bb0cdea56dde182357522e4e86a03c57852
git -C "$ROUTINGKIT_DIR" checkout --detach 54d49bb0cdea56dde182357522e4e86a03c57852
make -C "$ROUTINGKIT_DIR" -j"${AEGIS_BENCH_MAKE_JOBS:-2}"

bash scripts/build-routingkit-ch-server.sh "$ROUTINGKIT_DIR" "$BIN_DIR/aegis-routingkit-ch-server"
bash scripts/build-routingkit-cch-server.sh "$ROUTINGKIT_DIR" "$BIN_DIR/aegis-routingkit-cch-server"
go build -o "$BIN_DIR/aegis" ./cmd/aegis
go build -o "$BIN_DIR/aegis-max-batch" ./cmd/aegis-max-batch
go build -o "$BIN_DIR/aegis-routingkit-export" ./cmd/aegis-routingkit-export
go build -o "$BIN_DIR/aegis-routingkit-cch-export" ./cmd/aegis-routingkit-cch-export

if [[ ! -s "$PBF" ]]; then
  curl -fL --retry 6 --retry-all-errors --retry-delay 2 "$URL" -o "$PBF"
fi

if [[ "$DATASET" == "tokyo" ]]; then
  # Mainland Tokyo + the immediately connected metro fringe. Keeping the box
  # stable makes benchmark runs comparable even though the Geofabrik source is
  # refreshed over time.
  osmium extract --bbox 138.85,35.45,140.05,35.95 "$PBF" -o "$OUT_DIR/tokyo.osm.pbf" --overwrite
  osmium cat "$OUT_DIR/tokyo.osm.pbf" -o "$OSM" --overwrite
else
  osmium cat "$PBF" -o "$OSM" --overwrite
fi

GRAPH="$OUT_DIR/$DATASET-$METRIC.aegis"
BASELINE="$OUT_DIR/baseline.json"
QUERIES_FILE="$OUT_DIR/queries.txt"
CH_GRAPH="$OUT_DIR/graph.routingkit-ch"
CCH_GRAPH="$OUT_DIR/graph.routingkit-cch"

"$BIN_DIR/aegis" import-osm --input "$OSM" --output "$GRAPH" --profile car --metric "$METRIC"
"$BIN_DIR/aegis" benchmark \
  --graph "$GRAPH" \
  --queries "$QUERIES" \
  --repeats 1 \
  --order interleaved \
  --suite mixed \
  --pair-mode strongly-connected \
  --seed "$SEED" \
  --algorithms dijkstra,bidijkstra,astar,aegis \
  --output "$BASELINE" --html ""

python3 - "$BASELINE" "$QUERIES_FILE" <<'PY'
import json, sys
src, dst = sys.argv[1:]
d = json.load(open(src, encoding='utf-8'))
qs = d.get('queryPairs') or []
if not qs:
    raise SystemExit('baseline report contains no queryPairs')
with open(dst, 'w', encoding='utf-8') as out:
    for q in qs:
        out.write(f"{q['source']} {q['target']}\n")
print(f'wrote {len(qs)} shared query pairs to {dst}')
PY

"$BIN_DIR/aegis-routingkit-export" --graph "$GRAPH" --output "$CH_GRAPH"
"$BIN_DIR/aegis-routingkit-cch-export" --graph "$GRAPH" --output "$CCH_GRAPH"

export LD_LIBRARY_PATH="$ROUTINGKIT_DIR/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"

"$BIN_DIR/aegis-max-batch" \
  --graph "$GRAPH" --queries "$QUERIES_FILE" \
  --routingkit-ch-server "$BIN_DIR/aegis-routingkit-ch-server" \
  --routingkit-ch-graph "$CH_GRAPH" \
  --algorithms routingkit-ch --verify --summary-only \
  > "$OUT_DIR/ch.json"

"$BIN_DIR/aegis-max-batch" \
  --graph "$GRAPH" --queries "$QUERIES_FILE" \
  --routingkit-cch-server "$BIN_DIR/aegis-routingkit-cch-server" \
  --routingkit-cch-graph "$CCH_GRAPH" \
  --algorithms routingkit-cch --verify --summary-only \
  > "$OUT_DIR/cch.json"

"$BIN_DIR/aegis-max-batch" \
  --graph "$GRAPH" --queries "$QUERIES_FILE" \
  --alt-landmarks "$ALT_LANDMARKS" \
  --algorithms alt --verify --summary-only \
  > "$OUT_DIR/alt.json"

python3 - "$DATASET" "$METRIC" "$BASELINE" "$OUT_DIR/ch.json" "$OUT_DIR/cch.json" "$OUT_DIR/alt.json" "$OUT_DIR/summary.json" <<'PY'
import json, sys
name, metric, base_p, ch_p, cch_p, alt_p, out_p = sys.argv[1:]
base = json.load(open(base_p, encoding='utf-8'))
ch = json.load(open(ch_p, encoding='utf-8'))
cch = json.load(open(cch_p, encoding='utf-8'))
alt = json.load(open(alt_p, encoding='utf-8'))
q = int(base['config']['queries'])
rows = []
for s in base['summary']:
    rows.append({
        'algorithm': s['algorithm'],
        'meanNs': s['meanNs'], 'p50Ns': s['medianNs'], 'p95Ns': s['p95Ns'], 'p99Ns': s['p99Ns'],
        'preprocessNs': 0, 'amortizedMeanNs': s['meanNs'],
        'correct': s['correct'], 'runs': s['runs'],
    })
for alg, d, meta_key in [
    ('routingkit-ch', ch, 'routingKitCH'),
    ('routingkit-cch', cch, 'routingKitCCH'),
    ('alt', alt, 'alt'),
]:
    s = d['report']['summary']
    m = d[meta_key]
    prep = int(m['preprocessNs'])
    rows.append({
        'algorithm': alg,
        'meanNs': s['meanNs'], 'p50Ns': s['p50Ns'], 'p95Ns': s['p95Ns'], 'p99Ns': s['p99Ns'],
        'preprocessNs': prep,
        'amortizedMeanNs': s['meanNs'] + prep // max(q, 1),
        'correct': s['queries'], 'runs': s['queries'],
    })
rows.sort(key=lambda r: r['meanNs'])
report = {
    'dataset': name, 'metric': metric, 'seed': base['config']['seed'],
    'nodes': base['nodes'], 'edges': base['edges'], 'queries': q,
    'allCorrect': base['allCorrect'] and all(r['correct'] == r['runs'] for r in rows),
    'rankingByQueryMean': rows,
}
json.dump(report, open(out_p, 'w', encoding='utf-8'), indent=2)
print(json.dumps(report, indent=2))
PY
