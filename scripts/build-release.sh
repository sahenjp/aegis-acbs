#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
VERSION="$(cat VERSION)"
DIST="$ROOT/dist"
rm -rf "$DIST"
mkdir -p "$DIST" "$ROOT/artifacts"

go test ./...
go vet ./...
go test -race ./internal/search ./internal/graph ./internal/bench ./internal/server
scripts/reproduce.sh
INTERNAL_BASELINE_TAG="${AEGIS_INTERNAL_BASELINE_TAG:-v0.11.2-experimental}"
HAS_INTERNAL_BASELINE=false

if git rev-parse --verify "${INTERNAL_BASELINE_TAG}^{commit}" >/dev/null 2>&1; then
  HAS_INTERNAL_BASELINE=true
  OLD_TAG="$INTERNAL_BASELINE_TAG" \
    QUERIES=100 \
    REPEATS=5 \
    BATCH=8 \
    scripts/compare-tags.sh \
      artifacts/hatfield-uk.aegis \
      artifacts/tag-comparison
else
  echo "Skipping internal tag comparison: $INTERNAL_BASELINE_TAG is not present"
fi

build_one() {
  local os="$1" arch="$2" ext=""
  [[ "$os" == windows ]] && ext=".exe"
  local base="aegis-acbs_${VERSION}_${os}_${arch}"
  local stage="$(mktemp -d)"
  mkdir -p "$stage/$base"
  GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X github.com/sahenjp/aegis-acbs/internal/version.Version=$VERSION" -o "$stage/$base/aegis$ext" ./cmd/aegis
  cp README.md README.ja.md LICENSE RELEASE_NOTES.md "$stage/$base/"
  cp -r docs benchmarks scripts benchdata "$stage/$base/"
  rm -f "$stage/$base/benchdata/hatfield-uk.aegis"
  if [[ "$os" == windows ]]; then
    (cd "$stage" && zip -qr "$DIST/$base.zip" "$base")
  else
    tar -C "$stage" -czf "$DIST/$base.tar.gz" "$base"
  fi
  rm -rf "$stage"
}

for target in linux/amd64 linux/arm64 windows/amd64 darwin/amd64; do
  build_one "${target%/*}" "${target#*/}"
done

git archive --format=tar.gz --prefix="aegis-acbs-$VERSION/" -o "$DIST/aegis-acbs-$VERSION-source.tar.gz" HEAD
git archive --format=zip --prefix="aegis-acbs-$VERSION/" -o "$DIST/aegis-acbs-$VERSION-source.zip" HEAD
cp artifacts/hatfield-uk-benchmark.json artifacts/hatfield-uk-benchmark.html artifacts/hatfield-uk-stress.json artifacts/hatfield-uk-regret.json artifacts/hatfield-uk-regret.csv artifacts/hatfield-uk-regret.html "$DIST/"
cp artifacts/tail-validation/regret-validation.json artifacts/tail-validation/regret-validation.csv artifacts/tail-validation/regret-validation.html "$DIST/"
cp artifacts/tail-replay/regret-replay.json artifacts/tail-replay/regret-replay.csv artifacts/tail-replay/regret-replay.html "$DIST/"
cp artifacts/tail-replay/trigger-profile.json artifacts/tail-replay/trigger-profile.csv artifacts/tail-replay/trigger-profile.html "$DIST/"
for f in artifacts/tail-replay/guard-benchmark.json artifacts/tail-replay/guard-benchmark.html artifacts/tail-replay/release-gate.json; do [[ -f "$f" ]] && cp "$f" "$DIST/"; done
cp artifacts/matrix/benchmark-matrix.json artifacts/matrix/benchmark-matrix.csv artifacts/matrix/benchmark-matrix.html "$DIST/"
if [[ "$HAS_INTERNAL_BASELINE" == true ]]; then
  cp artifacts/tag-comparison/summary.md "$DIST/tag-comparison.md"
  cp artifacts/tag-comparison/old.html "$DIST/tag-comparison-v0.11.2.html"
  cp artifacts/tag-comparison/current.html "$DIST/tag-comparison-v0.1.0.html"

  OLD_TAG="$INTERNAL_BASELINE_TAG" \
    BENCHTIME=20x \
    COUNT=3 \
    scripts/compare-allocations.sh artifacts/allocation-comparison

  cp artifacts/allocation-comparison/summary.json \
     artifacts/allocation-comparison/summary.md \
     "$DIST/"

  cp artifacts/allocation-comparison/old.txt \
     "$DIST/allocation-v0.11.2.txt"

  cp artifacts/allocation-comparison/current.txt \
     "$DIST/allocation-v0.1.0.txt"
fi

if [[ -f research/tokyo-time-2026-07-18/trigger-profile.json ]]; then
  python3 scripts/check-release-evidence.py research/tokyo-time-2026-07-18
  (cd research && zip -qr "$DIST/tokyo-time-2026-07-18-evidence.zip" tokyo-time-2026-07-18)
else
  cp research/tokyo-time-2026-07-18/observed-summary.json "$DIST/tokyo-time-2026-07-18-observed-summary.json"
fi

python3 - <<PY
import json, os, platform, subprocess, datetime
root=${ROOT@Q}; dist=${DIST@Q}; version=${VERSION@Q}
commit=subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip()
info={
  'name':'Aegis ACBS','version':version,'commit':commit,
  'builtAt':datetime.datetime.now(datetime.timezone.utc).isoformat(),
  'builder':{'platform':platform.platform(),'python':platform.python_version()},
  'tests':'go test ./...','vet':'go vet ./...','benchmark':'Hatfield real OSM-derived fixture with deterministic interleaved order, ACBS ablations, allocation telemetry, a three-seed distance/time matrix, an optional internal v0.11.2/public v0.1.0 tag comparison, query-level regret diagnosis, an optional isolated internal v0.11.2/public v0.1.0 allocation regression comparison and multi-seed regret validation with confidence bounds, and isolated outlier replay with chunk-level traces, whole-suite trigger profiling, and concurrent stress verification'
}
open(os.path.join(dist,'BUILD-INFO.json'),'w').write(json.dumps(info,indent=2)+'\n')
sbom={'bomFormat':'CycloneDX','specVersion':'1.5','serialNumber':'urn:uuid:aegis-acbs-'+version,'version':1,'metadata':{'component':{'type':'application','name':'aegis-acbs','version':version}},'components':[]}
open(os.path.join(dist,'SBOM.cdx.json'),'w').write(json.dumps(sbom,indent=2)+'\n')
PY

if git rev-parse --git-dir >/dev/null 2>&1; then
  bundle_stage="$(mktemp -d)"
  git clone --bare "$ROOT" "$bundle_stage/repo.git" >/dev/null 2>&1
  while IFS= read -r tag; do
    [[ -n "$tag" ]] && git -C "$bundle_stage/repo.git" tag -d "$tag" >/dev/null
  done < <(git -C "$bundle_stage/repo.git" tag -l)
  git -C "$bundle_stage/repo.git" symbolic-ref HEAD refs/heads/main
  # The bundle is a convenience archive of the git history. It is not required
  # for the release to be usable, and "git bundle --all" refuses to run when the
  # clone has no refs left to include, which happens on a shallow CI checkout.
  # Treat it as best effort so an otherwise complete release is not blocked.
  if ! git -C "$bundle_stage/repo.git" bundle create "$DIST/aegis-acbs-$VERSION.bundle" --all; then
    echo "warning: git bundle could not be created from this checkout; continuing without it" >&2
    rm -f "$DIST/aegis-acbs-$VERSION.bundle"
  fi
  rm -rf "$bundle_stage"
fi

(cd "$DIST" && sha256sum $(find . -maxdepth 1 -type f ! -name SHA256SUMS ! -name '*complete-release.zip' -printf '%f\n' | sort) > SHA256SUMS)
(
  cd "$DIST"
  zip -q "aegis-acbs-v$VERSION-complete-release.zip" \
    $(find . -maxdepth 1 -type f ! -name '*complete-release.zip' -printf '%f\n' | sort)
  sha256sum "aegis-acbs-v$VERSION-complete-release.zip" >> SHA256SUMS
)