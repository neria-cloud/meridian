#!/usr/bin/env bash
# Shared helpers for the Meridian module release chain.
# Every function assumes the current directory is the repository root.
set -euo pipefail

MODULE_PREFIX="${MODULE_PREFIX:-github.com/neria-cloud/meridian}"
RELEASE_BRANCH="${RELEASE_BRANCH:-${GITHUB_REF_NAME:-$(git rev-parse --abbrev-ref HEAD)}}"

# Modules in release order: a module is tagged only after every internal
# module it requires has been tagged and pushed.
ordered_modules() {
  if [ -f core/version ]; then echo core; fi
  if [ -f mrncore/version ]; then echo mrncore; fi
  if [ -f framework/version ]; then echo framework; fi
  if [ -d plugins ]; then
    find plugins -mindepth 2 -maxdepth 2 -name version | sort | while read -r v; do dirname "$v"; done
  fi
  if [ -f transports/version ]; then echo transports; fi
  if [ -f cli/version ]; then echo cli; fi
}

mod_version() { tr -d '[:space:]' < "$1/version"; }

tag_of() { printf '%s/v%s\n' "$1" "$(mod_version "$1")"; }

tag_exists() { git rev-parse -q --verify "refs/tags/$1^{commit}" >/dev/null 2>&1; }

require_clean_tree() {
  if ! git diff --quiet || ! git diff --cached --quiet; then
    echo "ERROR: working tree not clean:" >&2
    git status --short >&2
    return 1
  fi
}

# Retry wrapper: a tag pushed seconds ago may not be fetchable yet.
retry() {
  local i
  for i in 1 2 3 4 5; do
    if "$@"; then return 0; fi
    echo "retry $i/5: '$*' failed; sleeping $((i * 5))s" >&2
    sleep $((i * 5))
  done
  return 1
}
