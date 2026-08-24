#!/usr/bin/env bash
# Pin every internal dependency of <module-dir> to the version recorded in the
# dependee's version file, then tidy.
# Usage: scripts/bump-internal-deps.sh <module-dir>
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/release-lib.sh

m="${1:?usage: bump-internal-deps.sh <module-dir>}"

# Local filesystem replace directives are a development convenience only —
# they must never survive into a released module.
repls=$( (cd "$m" && go mod edit -json) | jq -r '.Replace // [] | .[].Old.Path' | grep "^$MODULE_PREFIX/" || true)
for r in $repls; do
  echo "  $m: drop replace $r"
  (cd "$m" && go mod edit -dropreplace="$r")
done

deps=$( (cd "$m" && go mod edit -json) | jq -r '.Require // [] | .[].Path' | grep "^$MODULE_PREFIX/" || true)
for d in $deps; do
  rel="${d#"$MODULE_PREFIX/"}"
  if [ ! -f "$rel/version" ]; then
    echo "ERROR: $m requires $d but $rel/version does not exist" >&2
    exit 1
  fi
  v="v$(mod_version "$rel")"
  echo "  $m: pin $d $v"
  (cd "$m" && go mod edit -require="$d@$v")
done
(cd "$m" && retry go mod tidy)
