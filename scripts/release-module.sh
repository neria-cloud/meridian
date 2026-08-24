#!/usr/bin/env bash
# Create and push the annotated tag <module>/v<version> for one module.
# Refuses to skip silently when module content changed without a version bump.
# RELEASE_NO_PUSH=1 creates the tag locally without pushing (dry runs).
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/release-lib.sh

m="${1:?usage: release-module.sh <module-dir>}"
tag=$(tag_of "$m")

if tag_exists "$tag"; then
  if git diff --quiet "$tag" HEAD -- "$m"; then
    echo "$tag already released and content unchanged — skipping"
    exit 0
  fi
  echo "ERROR: $tag exists but $m/ differs from it. Released versions are immutable — bump $m/version." >&2
  exit 1
fi

msg="Release $m v$(mod_version "$m")"
body=""
if [ -f "$m/changelog.md" ]; then
  body=$(grep -v '^<!--' "$m/changelog.md" | grep -v '^-->' || true)
fi
if [ -n "$body" ]; then
  git tag -a "$tag" -m "$msg" -m "$body"
else
  git tag -a "$tag" -m "$msg"
fi
if [ "${RELEASE_NO_PUSH:-0}" != 1 ]; then
  git push -q origin "refs/tags/$tag"
fi
echo "released $tag"
