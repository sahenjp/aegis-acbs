#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="${AEGIS_DATA_DIR:-$ROOT/.data/osm}"
TARGET="${1:-all}"
mkdir -p "$OUT"

fetch() {
  local id="$1" bbox="$2"
  local dest="$OUT/$id.osm"
  local url="https://api.openstreetmap.org/api/0.6/map?bbox=$bbox"
  echo "[$id] $url"
  curl --fail --location --retry 5 --retry-all-errors \
    --connect-timeout 20 --max-time 600 \
    --user-agent "AegisOneRoad/12.2.0 (+https://github.com/sahenjp/aegis-acbs)" \
    "$url" -o "$dest.tmp"
  mv "$dest.tmp" "$dest"
  sha256sum "$dest" > "$dest.sha256"
}

case "$TARGET" in
  all)
    fetch tokyo "139.7625,35.6785,139.7720,35.6865"
    fetch london "-0.1320,51.4970,-0.1180,51.5060"
    fetch shanghai "121.4660,31.2250,121.4800,31.2350"
    fetch seoul "126.9700,37.5600,126.9850,37.5710"
    fetch paris "2.3370,48.8530,2.3520,48.8640"
    ;;
  tokyo) fetch tokyo "139.7625,35.6785,139.7720,35.6865" ;;
  london) fetch london "-0.1320,51.4970,-0.1180,51.5060" ;;
  shanghai) fetch shanghai "121.4660,31.2250,121.4800,31.2350" ;;
  seoul) fetch seoul "126.9700,37.5600,126.9850,37.5710" ;;
  paris) fetch paris "2.3370,48.8530,2.3520,48.8640" ;;
  *) echo "unknown city: $TARGET" >&2; exit 2 ;;
esac

cat > "$OUT/DATA-MANIFEST.txt" <<MANIFEST
Downloaded: $(date -u +%Y-%m-%dT%H:%M:%SZ)
Source: OpenStreetMap API 0.6
Attribution: © OpenStreetMap contributors
Licence: ODbL 1.0
Manifest: $ROOT/benchmarks/cities.json
MANIFEST
