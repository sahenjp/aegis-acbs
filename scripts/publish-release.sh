#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

REPO="${AEGIS_GITHUB_REPO:-sahenjp/aegis-acbs}"
VERSION="$(cat VERSION)"
TAG="v$VERSION"
REMOTE_URL="https://github.com/$REPO.git"

[[ "$VERSION" == "0.1.0" ]] || {
  echo "refusing publication from unexpected version: $VERSION" >&2
  exit 1
}
[[ "${AEGIS_PUBLISH_CONFIRM:-}" == "YES" ]] || {
  echo "refusing publication: set AEGIS_PUBLISH_CONFIRM=YES after reviewing the evidence" >&2
  exit 2
}
command -v gh >/dev/null || { echo "GitHub CLI (gh) is required" >&2; exit 1; }
gh auth status

git diff --quiet && git diff --cached --quiet || {
  echo "working tree must be clean" >&2
  exit 1
}

scripts/validate-release.sh
scripts/build-release.sh

current="$(git branch --show-current)"
if [[ "$current" != "main" ]]; then
  git branch -M main
fi

if ! gh repo view "$REPO" >/dev/null 2>&1; then
  gh repo create "$REPO" --public \
    --description "Experimental exact coupled-bound shortest-path search for real road networks"
fi

# A clone from a bundle records the bundle path as origin. Always replace it.
if git remote get-url origin >/dev/null 2>&1; then
  git remote set-url origin "$REMOTE_URL"
else
  git remote add origin "$REMOTE_URL"
fi

echo "origin: $(git remote get-url origin)"
git push -u origin main

if gh release view "$TAG" --repo "$REPO" >/dev/null 2>&1; then
  echo "release already exists: $TAG"
else
  gh release create "$TAG" dist/* \
    --repo "$REPO" \
    --target main \
    --title "Aegis ACBS $TAG" \
    --notes-file RELEASE_NOTES.md
fi

git fetch --tags origin
echo "published repository and release: https://github.com/$REPO/releases/tag/$TAG"
