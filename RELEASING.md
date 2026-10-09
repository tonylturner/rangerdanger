# Releasing

This document describes how to cut a RangerDanger release. It is the
runbook for tagging, publishing images to GHCR, and producing the
release-flavor compose files students consume.

## Versioning

RangerDanger follows [Semantic Versioning](https://semver.org/):

- `MAJOR` - breaking changes to the lab topology, container layout, or
  the API surface students rely on.
- `MINOR` - new exercises, new node types, new endpoints, additive
  compose changes.
- `PATCH` - bug fixes, doc updates, image bumps, internal refactors.

Pre-1.0 releases use `0.MINOR.PATCH`; treat any `MINOR` bump as a
potential breaking change until `v1.0.0` ships.

## Pre-flight checklist

Before tagging:

1. **CI is green** on the branch you're cutting from.
2. **`go test -race ./...`** clean across `backend/`, `services/`, and
   `dnp3go/`.
3. **Compose validation** (the loop in `CONTRIBUTING.md` → Running
   tests) passes with no warnings for the platform files and every
   package's range files.
4. **`./scripts/dev-up.sh`** comes up clean on a workstation with the
   configured RAM (≥8 GB) and disk (≥30 GB): the platform starts and
   `GET /api/range` reports the US range `ready`.
5. **`CHANGELOG.md`** has an entry under `[Unreleased]` summarizing
   user-visible changes.

## Cutting the release

1. Move the `[Unreleased]` block in `CHANGELOG.md` under a new
   `## [vX.Y.Z] - YYYY-MM-DD` heading and update the link references at
   the bottom.
2. Commit on `main`:

   ```sh
   git add CHANGELOG.md
   git commit -m "release: vX.Y.Z"
   ```
3. Tag and push:

   ```sh
   git tag -a vX.Y.Z -m "RangerDanger vX.Y.Z"
   git push origin main vX.Y.Z
   ```

The `release.yml` workflow takes over from there. It triggers on any
`v*` tag push and first creates one plan for all 16 first-party images.
Fifteen images use the generated publish matrix; `rangerdanger-frontend`
uses its dedicated native `linux/amd64` and `linux/arm64` jobs
(`rangerdanger-openplc` is amd64-only because its upstream base,
`tuttas/openplc_v3`, is amd64-only). Each image is either built or
promoted from its exact recorded root digest.

For builds, the workflow writes an explicit artifact-build OCI label
whitelist: title, source, license, the full checked-out source revision,
and the UTC time that build's labels were generated. Build retries reuse
the same labels; each native frontend architecture has its own label
generation time. These labels describe when and from what revision an
artifact was built, not which release later selects it. The workflow does
not set `org.opencontainers.image.version`. It injects
`VERSION=vX.Y.Z`, `COMMIT=<short sha>`, and `DATE=<utc rfc3339>` via
`--build-arg` (consumed by `Dockerfile.backend`'s ldflags and reported by
`/api/build`). Builds use the per-image GHA cache; a full rebuild override
also pulls current bases and disables the cache.

The release tag is always applied. A tag containing a hyphen (for example
`v0.0.1-alpha`) does **not** move `:latest`, on either a build or promotion.
Assembly identity remains the GitHub release/tag, the version selected
through `.env` (and recorded in an SSD bundle's `.version`), and the
backend's `/api/build` response. It is separate from an image's original
artifact-build labels.

### Image promotion and trust model

Promotion is not authorized by a mutable image tag. The workflow reads
the previous published release's `release-images.json` asset and uses a
per-image record containing the repository, root digest, platforms,
original build revision, and policy version. The planner compares
reviewed image inputs and workflow control inputs against that original
`build_revision` (not the previous tag). It also checks that the digest
still resolves, the previous release tag still points to that digest,
and the root's platform set matches the inventory. A missing/unreadable
record, policy or platform mismatch, changed input, registry failure, or
tag/digest disagreement fails closed to a build; a missing record does
not block a release. Workflow dispatch runs always build and never
promote.

Every release asset named `release-images.json` records `schema`, the
release tag and full source commit, `previous_release`, whether a full
rebuild override was used, `policy_version`, and an entry for every
image. Each image entry records its repository, build/promote decision
and reason, verified root digest, platforms, original build revision and
creation time, source release, and policy version. A promoted image
retains its original build metadata; a build records the metadata from
that build. The frontend additionally retains each native architecture's
original label timestamp under `platform_builds`; its primary
`build_created` remains the amd64 timestamp for compatibility with the
single-time record fields. The release is held as a draft until all image
results are present and consistent, the record is uploaded and verified,
and the release can be published.

The **first release after this promotion workflow merges can only
record, never promote**: the release workflow and inventory are control
inputs changed by that release, and there is no prior record authorizing
reuse. Promotion can start with the following release.

Use a full rebuild when upstream bases or packages need refreshing.
Promoted images receive no upstream base/package updates. Cut a release
whose annotated tag message contains `[rebuild-all]` before a workshop
and after any upstream security advisory:

```sh
git tag -a vX.Y.Z -m "RangerDanger vX.Y.Z [rebuild-all]"
```

The other route is Actions → `Release` → "Run workflow", enter an
existing tag, and check `rebuild_all`. Manual runs already build rather
than promote; checking the input additionally enables `pull: true` and
`no-cache: true` on every build. The tag-message marker applies to a
tag-push run; the dispatch checkbox is the manual equivalent.

### Rehearsing promotion without touching a real tag

`scripts/rehearse_promotion.sh` runs the publish paths against a scratch
registry on loopback: it merges per-architecture manifests, promotes an
index root and a single-manifest root, and runs
`scripts/verify_published_image.sh` on each. It proves the three
properties the workflow depends on — `docker buildx imagetools create`
reports the pushed root digest only through `--metadata-file` (its stdout
is empty), re-tagging preserves the recorded root digest, and a
single-manifest source needs `--prefer-index=false` or buildx wraps it in
a new index with a new root — plus that verification rejects a wrong
platform set. It needs docker, buildx, jq and curl, and touches no real
registry. Run it before a release that will promote, and after a buildx
upgrade.

## Release Compose files

Each source Compose file has an image-only release twin for users who
don't want the build toolchain: `docker-compose.release.yml` for the
platform (Compose project `rangerdanger-platform`) and
`lab-definitions/packages/<id>/compose.release.yml` for each range
package (project `rangerdanger`). Every `build:` block is replaced with
`image: ghcr.io/tonylturner/rangerdanger-<svc>:${VERSION:-latest}`.
`setup.sh` writes `VERSION` and `RANGERDANGER_ROOT` to `.env`, pulls the
platform and every package, starts the platform, and asks the backend
for the range:

```sh
./setup.sh                       # default - :latest
./setup.sh --version v0.1.0      # pin to a specific release
```

Offline installs (`--from-tarballs`) use the same release files with
`--pull never`. When you change a source file, mirror the change into
its release twin; the package lint checks that the two agree.

## containd image policy

RangerDanger and [containd](https://github.com/tonylturner/containd)
are co-developed by the same maintainer. Both of the US package's range
Compose files reference `ghcr.io/tonylturner/containd:latest` rather
than a per-release pinned tag. The contract is **"fix containd, not the
pin"** - if a containd release breaks RangerDanger behavior, the fix
lands in containd, and RangerDanger picks it up on the next `./setup.sh`
(which pulls). This keeps both repos honest about regressions instead of accumulating workarounds
in the lab.

The trade-off is workshop-day determinism. `:latest` resolves at pull
time, so a containd push between your pre-flight check and the morning
of class can shift behavior under you. **Mitigations for instructors:**

1. **Pre-pull the night before** and lock the resolved digest:

   ```sh
   docker pull ghcr.io/tonylturner/containd:latest
   docker image inspect ghcr.io/tonylturner/containd:latest \
     --format '{{index .RepoDigests 0}}'
   ```

   Save that digest. If anything breaks the morning of class, compare
   against `:latest` and pin to the digest if they differ.

2. **Run `setup.sh` (or `setup.ps1`) right before class** - the
   workshop-readiness gate (`/api/firewall/health` + apply weak/
   improved + reset) fails loudly if containd drifted, so you find
   out at setup-time rather than student-time. `--skip-firewall-gate`
   bypasses for diagnosis.

3. **Stage to SSD** for an offline class. `stage-ssd.sh` snapshots
   whatever is currently `:latest` and produces a tarball that
   `setup.sh --from-tarballs` loads and starts with `--pull never`.
   Once staged, the SSD is immutable.

If a workshop scenario demands hard determinism (regulatory audit,
certified curriculum), pin a known-good `containd:vX.Y.Z` tag in both
of the package's range Compose files for that engagement and document the pin in the
engagement's README. The default `:latest` posture is for the public
project where currency-of-fixes outweighs frozen-behavior.

## Manual workflow run

To re-trigger the workflow for an existing tag (e.g. after fixing a
transient registry blip):

1. GitHub → Actions → `Release` → "Run workflow"
2. Provide the existing tag name
3. Leave `rebuild_all` unchecked for the regular build path, or
   check it to refresh moving upstream bases and packages.
4. The workflow checks out the tag's resolved commit. Dispatch runs
   never promote an existing artifact.

A dispatch still builds and pushes the requested tag. It refuses to run
when that tag already has a published GitHub Release; delete or unpublish
that release before intentionally republishing it. For a stable tag, the
existing hyphen rule also moves `:latest`; re-running an older stable tag
can therefore replace the current `:latest`.

## Hotfix releases

For a `vX.Y.Z+1` patch release off an existing tag, branch from the
tag rather than `main`:

```sh
git checkout -b release/vX.Y.Z+1 vX.Y.Z
# cherry-pick the fix
git tag -a vX.Y.Z+1 -m "RangerDanger vX.Y.Z+1"
git push origin release/vX.Y.Z+1 vX.Y.Z+1
```

Once the patch ships, fast-forward `main` if the fix also belongs on
`main`.
