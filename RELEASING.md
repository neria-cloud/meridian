# Releasing Meridian modules

Releases are produced by `bifrost-prep release` from an upstream checkout. Nothing in this
repository runs the release; the former `scripts/` and the `release-modules` workflow were
retired on 2026-09-09 because they re-pinned every module to the base's version files, which
is not what upstream ships.

## Prerequisites

- A checkout of upstream `maximhq/bifrost` with HEAD at the base tag:
  `git -C <upstream> checkout ent-vX.Y.Z-base`
- A clone of this repository on `main`, clean.
- `bifrost-prep` built from `neria-cloud/bifrost-prep` (`go build -o bifrost-prep ./cmd/bifrost-prep`).
- Go at least as new as the `go` directive of the upstream modules, or `GOTOOLCHAIN=auto`.
- Push rights to `origin`.

## Steps

```sh
# 1. see what the base needs
./bifrost-prep release --dry-run <upstream> <this clone>

# 2. produce the sync commits and tags locally
GOTOOLCHAIN=auto ./bifrost-prep release -v --exclude ':memory:' --exclude redis-certs <upstream> <this clone>

# 3. review
git -C <this clone> log --oneline -3
git -C <this clone> tag --points-at HEAD~1   # nothing: main is never tagged
git -C <this clone> show <dir>/v<ver>:<dir>/go.mod

# 4. push tags then main (or add --push to step 2)
GOTOOLCHAIN=auto ./bifrost-prep release --push --exclude ':memory:' --exclude redis-certs <upstream> <this clone>
```

The `--exclude` flags drop git-ignored test leftovers that live in the upstream checkout.

A re-run on the same base is a no-op: existing tags are classified and skipped, `main`
reproduces itself. A stopped run is resumed by running again.

## Reading the report

- `new` / `created`: tags this run makes.
- `existing-parity`: already published with upstream's pins.
- `existing-divergent`: already published with other pins; left alone, listed with the
  differing pins under `-v`.
- `no-upstream`: a version file whose tag upstream never made; nothing is invented.
- `parity:` what `transports` on `main` links versus upstream's pins, with the tag to blame.

## Meridian-only fixes

Tag a pre-release of the next patch (`core/v1.7.14-m1`) by hand on a commit parented on the
current sync commit; never reuse an upstream number. Consumers pin the `-m1` version
explicitly.

## What must never happen

- Moving, deleting or re-creating a pushed tag. `sum.golang.org` has it forever.
- Editing Go files on `main` by hand; `main` is a mirror.
- Bumping a pin to a version file.
