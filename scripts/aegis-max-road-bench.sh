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
TOKYO_BBOX="${AEGIS_TOKYO_BBOX:-138.90,35.45,140.00,35.90}"
EXTRACT_BBOX=""

mkdir -p "$OUT_DIR" "$BIN_DIR"

case "$DATASET" in
  andorra) URL="https://download.geofabrik.de/europe/andorra-latest.osm.pbf" ;;
  tokyo)
    # GitHub-hosted runners have repeatedly timed out against the BBBike Tokyo
    # endpoint. Use the same reliable Geofabrik source as Kanto and cut a
    # deterministic Tokyo-area bbox before filtering roads.
    URL="https://download.geofabrik.de/asia/japan/kanto-latest.osm.pbf"
    EXTRACT_BBOX="$TOKYO_BBOX"
    ;;
  kanto) URL="https://download.geofabrik.de/asia/japan/kanto-latest.osm.pbf" ;;
  japan) URL="https://download.geofabrik.de/asia/japan-latest.osm.pbf" ;;
  *)
    echo "unknown dataset: $DATASET (expected andorra, tokyo, kanto, or japan)" >&2
    exit 2
    ;;
esac

[[ "$QUERIES" =~ ^[1-9][0-9]*$ ]] || { echo "queries must be a positive integer" >&2; exit 2; }
[[ "$ALT_LANDMARKS" =~ ^[1-9][0-9]*$ ]] || { echo "ALT landmarks must be a positive integer" >&2; exit 2; }
(( QUERIES <= 100000 )) || { echo "queries is capped at 100000" >&2; exit 2; }
(( ALT_LANDMARKS <= 32 )) || { echo "ALT landmarks is capped at 32" >&2; exit 2; }

for tool in git curl osmium go g++ make python3; do
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
go build -o "$BIN_DIR/aegis-max-select" ./cmd/aegis-max-select
go build -o "$BIN_DIR/aegis-routingkit-export" ./cmd/aegis-routingkit-export
go build -o "$BIN_DIR/aegis-routingkit-cch-export" ./cmd/aegis-routingkit-cch-export

RAW="$OUT_DIR/raw.osm.pbf"
DATASET_PBF="$RAW"
ROADS_PBF="$OUT_DIR/roads.osm.pbf"
ROADS_OSM="$OUT_DIR/roads.osm"
if [[ ! -s "$RAW" ]]; then
  curl -fL --connect-timeout 30 --max-time 900 \
    --retry 6 --retry-all-errors --retry-delay 2 "$URL" -o "$RAW"
fi
if [[ -n "$EXTRACT_BBOX" ]]; then
  DATASET_PBF="$OUT_DIR/dataset.osm.pbf"
  osmium extract -b "$EXTRACT_BBOX" "$RAW" -o "$DATASET_PBF" --overwrite
fi
# Keep referenced nodes for highway ways, then expand only the reduced routing
# dataset to XML. This avoids exploding Tokyo/Kanto/Japan all-feature PBFs.
osmium tags-filter "$DATASET_PBF" w/highway -o "$ROADS_PBF" --overwrite
osmium cat "$ROADS_PBF" -o "$ROADS_OSM" --overwrite

GRAPH="$OUT_DIR/$DATASET-$METRIC.aegis"
BASELINE="$OUT_DIR/baseline.json"
QUERIES_FILE="$OUT_DIR/queries.txt"
VERIFY_FILE="$OUT_DIR/verify-queries.txt"
CH_GRAPH="$OUT_DIR/graph.routingkit-ch"
CCH_GRAPH="$OUT_DIR/graph.routingkit-cch"

"$BIN_DIR/aegis" import-osm --input "$ROADS_OSM" --output "$GRAPH" --profile car --metric "$METRIC"
"$BIN_DIR/aegis" benchmark \
  --graph "$GRAPH" --queries "$QUERIES" --repeats 1 --order interleaved \
  --suite mixed --pair-mode strongly-connected --seed "$SEED" \
  --algorithms dijkstra,bidijkstra,astar,aegis \
  --output "$BASELINE" --html ""

python3 - "$BASELINE" "$QUERIES_FILE" "$VERIFY_FILE" <<'PY'
import json, sys
src, dst, verify = sys.argv[1:]
d = json.load(open(src, encoding='utf-8'))
qs = d.get('queryPairs') or []
if not qs:
    raise SystemExit('baseline report contains no queryPairs')
with open(dst, 'w', encoding='utf-8') as out:
    for q in qs:
        out.write(f"{q['source']} {q['target']}\n")
with open(verify, 'w', encoding='utf-8') as out:
    for q in qs[:min(50, len(qs))]:
        out.write(f"{q['source']} {q['target']}\n")
print(f'wrote {len(qs)} shared query pairs; consensus-checking {min(50, len(qs))}')
PY

"$BIN_DIR/aegis-routingkit-export" --graph "$GRAPH" --output "$CH_GRAPH"
"$BIN_DIR/aegis-routingkit-cch-export" --graph "$GRAPH" --output "$CCH_GRAPH"
export LD_LIBRARY_PATH="$ROUTINGKIT_DIR/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"

"$BIN_DIR/aegis-max-batch" \
  --graph "$GRAPH" --queries "$QUERIES_FILE" \
  --routingkit-ch-server "$BIN_DIR/aegis-routingkit-ch-server" --routingkit-ch-graph "$CH_GRAPH" \
  --algorithms routingkit-ch --verify --summary-only --timeout 120m > "$OUT_DIR/ch.json"
"$BIN_DIR/aegis-max-batch" \
  --graph "$GRAPH" --queries "$QUERIES_FILE" \
  --routingkit-cch-server "$BIN_DIR/aegis-routingkit-cch-server" --routingkit-cch-graph "$CCH_GRAPH" \
  --algorithms routingkit-cch --verify --summary-only --timeout 120m > "$OUT_DIR/cch.json"
"$BIN_DIR/aegis-max-batch" \
  --graph "$GRAPH" --queries "$QUERIES_FILE" --alt-landmarks "$ALT_LANDMARKS" \
  --algorithms alt --verify --summary-only --timeout 120m > "$OUT_DIR/alt.json"

"$BIN_DIR/aegis-max-batch" \
  --graph "$GRAPH" --queries "$VERIFY_FILE" \
  --routingkit-ch-server "$BIN_DIR/aegis-routingkit-ch-server" --routingkit-ch-graph "$CH_GRAPH" \
  --algorithms routingkit-ch,bidijkstra --verify --consensus --summary-only --timeout 120m > "$OUT_DIR/ch-consensus.json"
"$BIN_DIR/aegis-max-batch" \
  --graph "$GRAPH" --queries "$VERIFY_FILE" \
  --routingkit-cch-server "$BIN_DIR/aegis-routingkit-cch-server" --routingkit-cch-graph "$CCH_GRAPH" \
  --algorithms routingkit-cch,bidijkstra --verify --consensus --summary-only --timeout 120m > "$OUT_DIR/cch-consensus.json"
"$BIN_DIR/aegis-max-batch" \
  --graph "$GRAPH" --queries "$VERIFY_FILE" --alt-landmarks "$ALT_LANDMARKS" \
  --algorithms alt,bidijkstra --verify --consensus --summary-only --timeout 120m > "$OUT_DIR/alt-consensus.json"

python3 - "$DATASET" "$METRIC" "$URL" "$EXTRACT_BBOX" "$BASELINE" "$OUT_DIR" <<'PY'
import json, os, sys
name, metric, url, bbox, base_p, out = sys.argv[1:]
base = json.load(open(base_p, encoding='utf-8'))
q = int(base['config']['queries'])
rows = []
graph_fingerprint = None
for s in base['summary']:
    rows.append({'algorithm': s['algorithm'], 'meanNs': s['meanNs'], 'p50Ns': s['medianNs'],
                 'p95Ns': s['p95Ns'], 'p99Ns': s['p99Ns'], 'preprocessNs': 0, 'updateNs': 0,
                 'amortizedMeanNs': s['meanNs'], 'correct': s['correct'], 'runs': s['runs']})
for alg, file, meta_key in [('routingkit-ch','ch.json','routingKitCH'),
                            ('routingkit-cch','cch.json','routingKitCCH'), ('alt','alt.json','alt')]:
    d = json.load(open(os.path.join(out, file), encoding='utf-8'))
    s, m = d['report']['summary'], d[meta_key]
    fp = m.get('fingerprint')
    if fp:
        if graph_fingerprint is None:
            graph_fingerprint = fp
        elif fp != graph_fingerprint:
            raise SystemExit(f'{alg} graph fingerprint disagrees with another preprocessed solver')
    prep = int(m['preprocessNs'])
    if alg == 'routingkit-cch':
        update = int(m.get('customizeNs', prep))
    else:
        update = prep
    rows.append({'algorithm': alg, 'meanNs': s['meanNs'], 'p50Ns': s['p50Ns'], 'p95Ns': s['p95Ns'],
                 'p99Ns': s['p99Ns'], 'preprocessNs': prep, 'updateNs': update,
                 'amortizedMeanNs': s['meanNs'] + prep // max(q,1),
                 'correct': s['queries'], 'runs': s['queries']})
if graph_fingerprint is None or len(graph_fingerprint) != 64:
    raise SystemExit('benchmark did not produce a valid graph fingerprint')
for name2 in ('ch','cch','alt'):
    c = json.load(open(os.path.join(out, f'{name2}-consensus.json'), encoding='utf-8'))['report']['summary']
    if c['consensusReached'] != c['queries']:
        raise SystemExit(f'{name2} failed BiDijkstra consensus')
rows.sort(key=lambda r: r['meanNs'])
report = {'dataset': name, 'metric': metric, 'sourceUrl': url, 'sourceBbox': bbox or None,
          'seed': base['config']['seed'], 'nodes': base['nodes'], 'edges': base['edges'], 'queries': q,
          'graphFingerprint': graph_fingerprint,
          'allCorrect': base['allCorrect'] and all(r['correct'] == r['runs'] for r in rows),
          'rankingByQueryMean': rows}
with open(os.path.join(out, 'summary.json'), 'w', encoding='utf-8') as f:
    json.dump(report, f, indent=2, sort_keys=True)
print(json.dumps(report, indent=2, sort_keys=True))
PY

# Emit representative workload decisions as benchmark evidence. These are not
# hard-coded policies; they are computed from this graph's measured timings.
"$BIN_DIR/aegis-max-select" --benchmark "$OUT_DIR/summary.json" --queries "$QUERIES" --metric-updates 0 > "$OUT_DIR/selection-static.json"
"$BIN_DIR/aegis-max-select" --benchmark "$OUT_DIR/summary.json" --queries "$QUERIES" --metric-updates 4 > "$OUT_DIR/selection-updates.json"
