# Releasing Meridian modules

This repository is the rebranded fork of Bifrost, published as Go modules under `github.com/neria-cloud/meridian/*`. This document describes the release machinery in `scripts/` and `.github/workflows/release-modules.yml`: how a change to a module's `version` file turns into a pushed, immutable, pinned Go module release.

## Model

- **Version files are the source of truth**, exactly as upstream: `core/version`, `framework/version`, `transports/version`, `plugins/<name>/version`, `cli/version` hold bare semvers (`1.7.13`).
- **Tags are path-prefixed** as Go requires for subdirectory modules: `core/v1.7.13`, `framework/v1.5.10`, `plugins/governance/v1.6.14`, `transports/v1.6.11`, `cli/v0.10.6`. A consumer writes `require github.com/neria-cloud/meridian/core v1.7.13` — the prefix lives only in the tag.
- **Versions mirror upstream's** for pristine syncs; the different module path prevents any collision, and the 1:1 mapping is self-documenting. Name the upstream base (`ent-vX.Y.Z-base`) in the sync commit message.
- **Release order** is dependency order: `core → framework → plugins/* → transports → cli`, auto-discovered from the `version` files (`scripts/release-lib.sh: ordered_modules`). A module is tagged only after everything it requires is tagged and pushed.
- **Tags are immutable.** `release-module.sh` fails when a tag exists but the module directory changed since — the "bump the version file" guard. Never delete or move a pushed tag; module zips are cached and checksummed downstream.
- **Respins** (a local fix between upstream releases): tag a prerelease of the next patch, e.g. `core/v1.7.14-m1` — sorts above `v1.7.13` and below upstream's future `v1.7.14`, so MVS stays sane.
- When upstream reaches v2 (they already tag `v2.0.0-prerelease*`), the module path needs the `/v2` suffix (`github.com/neria-cloud/meridian/transports/v2`).

## What the chain does per module (`scripts/release-chain.sh`)

1. **Drops local filesystem `replace` directives** for internal modules (`go mod edit -dropreplace`). The absolute-path replaces in this tree are a development convenience; `go.work` (gitignored) is the supported dev mechanism, and a released go.mod must resolve by version alone.
2. **Pins internal requires** to the sibling `version` files (`go mod edit -require=…@vX.Y.Z`) and runs `go mod tidy`. Stale pins (e.g. `core v1.7.10` while `core/version` says `1.7.13`) are corrected here automatically.
3. **Builds and vets** with `GOWORK=off` — the release-parity check the workspace would otherwise mask. For `transports`, a throwaway stub satisfies the `//go:embed all:ui` in package `main` (the real UI is injected at Docker-image build; the dir is gitignored, so the stub is never committed).
4. **Commits the pin bump** (`chore(release): <module>: pin internal deps for <tag>`) and pushes it to the release branch.
5. **Tags** `<module>/v<version>` (annotated; `<module>/changelog.md` becomes the tag message body) and pushes the tag.

The chain is idempotent: already-tagged, unchanged modules are skipped, so a partial failure resumes where it stopped, and a wave that bumps only `transports/version` tags only transports.

Tests are deliberately not part of the chain — core tests need provider API keys, framework tests need docker services. Keep them in dedicated workflows and make those required checks on `main`.

## Running

CI: `.github/workflows/release-modules.yml` triggers on pushes to `main` touching any `*/version` (plus manual `workflow_dispatch`). Pin-bump commits touch only `go.mod`/`go.sum`, so they cannot retrigger the workflow; a concurrency group serializes overlapping runs.

### CI authentication

The workflow pushes commits and tags using a repository secret `RELEASE_TOKEN`, **not** the default `GITHUB_TOKEN`. Two situations both lead here:

- an org (or enterprise) Actions policy enforces a read-only ceiling on `GITHUB_TOKEN`, so "Read and write permissions" is unavailable at the repo level even to a repo admin, or
- it's available, but a PAT scoped to this one repo has a smaller blast radius than raising the org-wide default token permission for every repo.

Set it up once:

1. Create a token scoped to **only this repository** with **Contents: Read and write**: GitHub → Settings → Developer settings → **Fine-grained personal access tokens** → New token → Resource owner `neria-cloud` → Repository access: "Only select repositories" → `meridian` → Repository permissions → Contents: Read and write.
2. Add it as a secret on this repo: Settings → Secrets and variables → Actions → New repository secret → name `RELEASE_TOKEN`, value the token from step 1. (Or `gh secret set RELEASE_TOKEN --repo neria-cloud/meridian` from a shell that has the token — never paste a token into a chat session.)
3. Re-run the workflow. The "Require RELEASE_TOKEN" step fails fast with a clear message if the secret is still unset.

If instead an org admin raises the org-level default to "Read and write permissions" (Organization Settings → Actions → General → Workflow permissions — note this changes the default for every repo in the org, not just this one), switch the workflow's top-level `permissions:` block to `contents: write` and the git-remote step back to `${{ github.token }}`; the PAT is then unnecessary.

Locally:

```bash
scripts/release-chain.sh                    # full release against origin
RELEASE_NO_PUSH=1 scripts/release-chain.sh  # dry run: local commits + tags, nothing pushed
```

Requirements: `go`, `git` push rights, `jq`.

## What a release produces

Each tagged module resolves as an ordinary Go module dependency, e.g.:

```go
require github.com/neria-cloud/meridian/transports v1.6.11
```

`go mod tidy`/`go get` on that pin pulls the rest of the internal graph (`core`, `framework`, the plugins transports depends on) transitively, at the exact versions the release chain pinned — no `replace` directives are needed by anything importing this repository's modules, because the chain removes them before tagging (see step 1 above).

If this repository is private, `GOPRIVATE=github.com/neria-cloud/*` and git auth (`git config --global url."https://x-access-token:${GH_TOKEN}@github.com/neria-cloud/".insteadOf "https://github.com/neria-cloud/"`, or SSH) are needed wherever these modules are fetched. If public, proxy.golang.org and the checksum DB work with zero configuration (a brand-new tag can take a few minutes to appear through the proxy; `GOPRIVATE` or `GOPROXY=direct` bypasses the wait).

## Failure modes

- "tag exists but module differs" — content changed without bumping `<module>/version`; bump it.
- `go mod tidy` cannot find an internal version — the dependee's tag is not pushed yet; release in order (the chain does) or wait out propagation (the scripts retry).
- Workflow cannot push — enable read/write workflow permissions or switch the token to a PAT.
- A broken module got tagged — fix forward and respin as `vX.Y.(Z+1)-m1`; never delete the tag.
- Example modules under `examples/plugins/` keep their local replaces on purpose: they are never released as modules, and the framework `.so`-plugin fixture is version-bumped (not de-replaced) by the chain, mirroring upstream's `release-framework.sh`.
