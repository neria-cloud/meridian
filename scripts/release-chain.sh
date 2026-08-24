#!/usr/bin/env bash
# Release every module whose version file is not yet tagged, in dependency
# order: pin internal deps -> build/vet -> commit pins -> tag -> push tag.
# Idempotent: an already-tagged, unchanged module is skipped, so the chain can
# be re-run after a partial failure and continues where it stopped.
#
# Tests are deliberately NOT run here: core tests need provider API keys and
# framework tests need docker services. Gate this workflow on the repository's
# test workflows instead.
#
# Requirements: git push rights to the release branch and tags, go, jq.
# Env knobs: MODULE_PREFIX, RELEASE_BRANCH, RELEASE_NO_PUSH=1 (dry run),
#            GOPRIVATE / GOPROXY (see defaults below).
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/release-lib.sh

export GOWORK=off
export GOFLAGS="${GOFLAGS:--mod=mod}"
# Internal modules bypass proxy+sumdb (fresh tags are not there yet); external
# dependencies still come from the fast public proxy.
export GOPRIVATE="${GOPRIVATE:-${MODULE_PREFIX%/*}/*}"
export GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}"

require_clean_tree
git fetch -q --tags origin || true

# Preflight: a module zip may not contain ':' '"' '*' '<' '>' '?' '|' in any
# path (test artifacts like an accidental ':memory:' SQLite file are the usual
# culprits). Catch them before a tag immortalizes them.
bad=$(git ls-files | grep -E '[:"*<>?|]' || true)
if [ -n "$bad" ]; then
  echo "ERROR: tracked paths invalid for Go module zips — remove them first:" >&2
  echo "$bad" >&2
  exit 1
fi

push_commits() {
  if [ "${RELEASE_NO_PUSH:-0}" = 1 ]; then return 0; fi
  git pull --rebase -q origin "$RELEASE_BRANCH"
  git push -q origin "HEAD:$RELEASE_BRANCH"
}

for m in $(ordered_modules); do
  tag=$(tag_of "$m")
  if tag_exists "$tag" && git diff --quiet "$tag" HEAD -- "$m"; then
    echo "== $m: $tag already released — skip"
    continue
  fi
  echo "== $m → $tag"

  # framework's .so-plugin test builds the hello-world example against the
  # released core; keep that fixture pinned exactly like upstream's
  # release-framework.sh (only hello-world — the other examples keep their
  # local replaces and are never released).
  if [ "$m" = framework ] && [ -f examples/plugins/hello-world/go.mod ]; then
    cv="v$(mod_version core)"
    (cd examples/plugins/hello-world && retry go get "$MODULE_PREFIX/core@$cv" && retry go mod tidy)
    git add examples/plugins/hello-world/go.mod
    if [ -f examples/plugins/hello-world/go.sum ]; then git add examples/plugins/hello-world/go.sum; fi
  fi

  scripts/bump-internal-deps.sh "$m"
  # transports' package main embeds all:ui, and the UI build is injected only in
  # the Docker image (the dir is gitignored). A stub keeps `go build ./...`
  # honest for every importable package without committing anything.
  if [ "$m" = transports ] && [ ! -e transports/bifrost-http/ui/index.html ]; then
    mkdir -p transports/bifrost-http/ui
    echo '<html>ui is injected at image build time</html>' > transports/bifrost-http/ui/index.html
  fi
  (cd "$m" && go build ./... && go vet ./...)

  git add "$m/go.mod"
  if [ -f "$m/go.sum" ]; then git add "$m/go.sum"; fi
  if ! git diff --cached --quiet; then
    git commit -q -m "chore(release): $m: pin internal deps for $tag"
    push_commits
  fi

  scripts/release-module.sh "$m"
done

echo "release chain complete"
