#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
mkdir -p bin artifacts artifacts/repro-graphs

go test ./...
go vet ./...
VERSION="$(cat VERSION)"
go build -trimpath -ldflags "-s -w -X github.com/sahenjp/aegis-acbs/internal/version.Version=$VERSION" -o bin/aegis ./cmd/aegis

bin/aegis import-osm --input benchdata/hatfield-uk.osm --output artifacts/repro-graphs/hatfield-distance.aegis --name hatfield-uk --profile car --metric distance
bin/aegis import-osm --input benchdata/hatfield-uk.osm --output artifacts/repro-graphs/hatfield-time.aegis --name hatfield-uk --profile car --metric time
cp artifacts/repro-graphs/hatfield-distance.aegis artifacts/hatfield-uk.aegis

GOMAXPROCS=1 bin/aegis benchmark --graph artifacts/repro-graphs/hatfield-distance.aegis --queries 100 --repeats 5 --batch 8 --order interleaved --measure-memory --research --suite mixed --seed 1010 --output artifacts/hatfield-uk-benchmark.json --html artifacts/hatfield-uk-benchmark.html
GOMAXPROCS=4 bin/aegis stress --graph artifacts/repro-graphs/hatfield-distance.aegis --queries 2000 --workers 4 --verify-every 10 --seed 7070 --output artifacts/hatfield-uk-stress.json
bin/aegis diagnose --input artifacts/hatfield-uk-benchmark.json --output artifacts/hatfield-uk-regret.json --csv artifacts/hatfield-uk-regret.csv --html artifacts/hatfield-uk-regret.html
AEGIS_BIN="$ROOT/bin/aegis" AEGIS_QUERIES=30 AEGIS_REPEATS=3 AEGIS_SEEDS="1010 20260717 424242" scripts/validate-tail.sh artifacts/repro-graphs/hatfield-time.aegis artifacts/tail-validation

AEGIS_BIN="$ROOT/bin/aegis" \
AEGIS_GRAPH_DIR="$ROOT/artifacts/repro-graphs" \
AEGIS_REPORT_DIR="$ROOT/artifacts/matrix" \
AEGIS_QUERIES=30 AEGIS_REPEATS=3 AEGIS_BATCH=8 \
AEGIS_ORDER=interleaved AEGIS_MEASURE_MEMORY=1 \
AEGIS_SEEDS="1010 20260717 424242" \
scripts/benchmark-matrix.sh

# Exercise isolated replay with deliberately permissive thresholds. This is a
# command/reporter fixture, not a claim that microsecond Hatfield differences
# are materially meaningful.
rm -rf artifacts/tail-replay
mkdir -p artifacts/tail-replay
bin/aegis validate-regret \
  --input-dir artifacts/tail-validation \
  --ratio-threshold 0.1 \
  --penalty-floor 1ns \
  --min-queries 1 \
  --max-meaningful-rate 1 \
  --top 3 \
  --fail-on-violation=false \
  --output artifacts/tail-replay/regret-validation.json \
  --csv artifacts/tail-replay/regret-validation.csv \
  --html artifacts/tail-replay/regret-validation.html
bin/aegis replay-regret \
  --graph artifacts/repro-graphs/hatfield-time.aegis \
  --validation artifacts/tail-replay/regret-validation.json \
  --input-root artifacts/tail-validation \
  --runs 7 \
  --warmup 2 \
  --ratio-threshold 0.1 \
  --penalty-floor 1ns \
  --top 3 \
  --output artifacts/tail-replay/regret-replay.json \
  --csv artifacts/tail-replay/regret-replay.csv \
  --html artifacts/tail-replay/regret-replay.html

bin/aegis profile-trigger \
  --graph artifacts/repro-graphs/hatfield-time.aegis \
  --validation artifacts/tail-replay/regret-validation.json \
  --replay artifacts/tail-replay/regret-replay.json \
  --input-root artifacts/tail-validation \
  --checkpoints 1,2,4,8 \
  --max-matches 5 \
  --output artifacts/tail-replay/trigger-profile.json \
  --csv artifacts/tail-replay/trigger-profile.csv \
  --html artifacts/tail-replay/trigger-profile.html
