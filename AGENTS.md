# Meridian: agent and maintainer notes

Meridian is a rebranded fork of [Bifrost](https://github.com/maximhq/bifrost), published as
Go modules under `github.com/neria-cloud/meridian/*`. This repository holds no hand-written
code: `main` is a machine-made mirror of one upstream enterprise base, and every module tag
is machine-made from the upstream tag it mirrors. Do not edit Go files here by hand; fix
things upstream or in the overlay that consumes these modules.

## Layout

- `core`, `framework`, `plugins/*`, `transports`, `cli`: the released modules. Each has a
  `version` file (informational: it names the version upstream released from that directory).
- `examples`, `tests`, `docs/openapi`, `Makefile`, `recipes`: copied for completeness, never
  released as modules.
- `version` (root): the enterprise base this `main` mirrors, e.g. `1.5.12`.
- `go.work` is gitignored; create one locally for cross-module development.

## How a release is produced

The tool is `bifrost-prep` (repository `neria-cloud/bifrost-prep`, command `release`). One run
turns an upstream checkout at `ent-vX.Y.Z-base` into:

1. a sync commit on `main` (`sync: ent-vX.Y.Z-base (upstream <sha>)`): the rebranded tree,
   replace-free, pins exactly as upstream wrote them;
2. one annotated tag per module version the base pins, built from the upstream tag's own
   commit, each on a commit parented on the sync commit and reachable only through the tag;
3. a `go.sum` commit on `main` (`sync: go.sum against released tags for ent-vX.Y.Z-base`).

Tags are named `<dir>/v<version>` (`core/v1.7.10`, `plugins/mocker/v1.5.19`). The tag set is
the base's `version` files closed under the internal requires of each tag's `go.mod`, so
every pin resolves. See `RELEASING.md` for the steps.

## Rules that keep consumers working

- **Never move, delete or re-create a pushed tag.** The repository is public;
  `proxy.golang.org` and `sum.golang.org` record every version forever. A moved tag makes
  every `go` consumer fail with a checksum mismatch. This has happened once
  (`framework/v1.5.10`, 2026-09-08) and had to be restored to the recorded commit.
- **Never bump a pin to a version file.** Pins are upstream's; `go mod tidy` may raise one
  only when an already published tag with different pins forces it (MVS), and the tool
  reports that.
- **A Meridian-only fix** gets a pre-release of the next patch, e.g. `core/v1.7.14-m1`, made by
  hand on a commit parented on the current sync commit. Never reuse an upstream number for
  content upstream did not ship under it.
- **Version files are not the source of truth for tags.** They describe the base; the pin
  closure decides what gets tagged.

## Known divergence

`transports/v1.6.11`, `framework/v1.5.10` and the plugin tags of base 1.5.12 were published
with `core v1.7.13` pinned, while upstream pinned `core v1.7.10`. Those numbers are burned:
the tool reports them as `existing-divergent`, and `transports` on `main` links `core v1.7.13`
because of them. Parity with upstream returns with the next upstream wave that gives those
modules new numbers.
