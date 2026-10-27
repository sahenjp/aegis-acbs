#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

[[ "$(cat VERSION)" == "0.2.0" ]] || {
  echo "unexpected VERSION: $(cat VERSION)" >&2
  exit 1
}

gofmt_files="$(gofmt -l cmd internal)"
[[ -z "$gofmt_files" ]] || {
  echo "gofmt required:" >&2
  echo "$gofmt_files" >&2
  exit 1
}

go test ./...
go vet ./...
go test -race ./internal/search ./internal/graph ./internal/bench ./internal/server
python3 scripts/check-release-evidence.py research/tokyo-time-2026-07-18
bash -n scripts/*.sh
python3 -m py_compile scripts/*.py

echo "v0.2.0 release validation: PASS"