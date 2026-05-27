# Releasing

This document describes how to cut a RangerDanger release. It is the
runbook for tagging, publishing images to GHCR, and producing the
release-flavor compose file students consume.

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
3. **`docker compose config -q`** validates with no warnings.
4. **`docker compose up -d --build`** comes up clean on a workstation
   with the configured RAM (≥8 GB) and disk (≥30 GB).
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
`v*` tag push and:

1. Builds all 14 first-party images in parallel via a matrix
   (`linux/amd64` + `linux/arm64`, except `openplc` which is
   amd64-only - upstream `tuttas/openplc_v3` is amd64-only).
2. Injects `VERSION=vX.Y.Z`, `COMMIT=<short sha>`, `DATE=<utc rfc3339>`
   via `--build-arg` (consumed by `Dockerfile.backend`'s ldflags).
3. Pushes each image to `ghcr.io/tonylturner/rangerdanger-<svc>:vX.Y.Z`.
   Pre-release tags (anything containing `-`, e.g. `v0.0.1-alpha`)
   do **not** retag `:latest`, so an alpha can never accidentally
   replace the stable `:latest` pointer.
4. Caches build layers per-image via GHA cache for faster subsequent
   runs.

## docker-compose.release.yml

`docker-compose.release.yml` (committed alongside `docker-compose.yml`)
is the image-only flavor for users who don't want the build toolchain.
Every `build:` block from the dev compose is replaced with `image:
ghcr.io/tonylturner/rangerdanger-<svc>:${VERSION:-latest}`. Users:

```sh
# default - :latest
docker compose -f docker-compose.release.yml up -d

# pin to a specific release
VERSION=v0.1.0 docker compose -f docker-compose.release.yml up -d
```

When you bump `docker-compose.yml`, mirror the change into the release
file too.

## containd image policy

RangerDanger and [containd](https://github.com/tonylturner/containd)
are co-developed by the same maintainer. Both compose files reference
`ghcr.io/tonylturner/containd:latest` rather than a per-release pinned
tag. The contract is **"fix containd, not the pin"** - if a containd
release breaks RangerDanger behavior, the fix lands in containd, and
RangerDanger picks it up on the next `docker compose pull`. This keeps
both repos honest about regressions instead of accumulating workarounds
in the lab.

The trade-off is workshop-day determinism. `:latest` resolves at pull
time, so a containd push between your pre-flight check and the morning
of class can shift behavior under you. **Mitigations for instructors:**

1. **Pre-pull the night before** and lock the resolved digest:

   ```sh
   docker compose -f docker-compose.release.yml pull
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
   `setup.sh --from-tarballs` consumes via `docker-compose.offline.yml`
   (`pull_policy: never`). Once staged, the SSD is immutable.

If a workshop scenario demands hard determinism (regulatory audit,
certified curriculum), pin a known-good `containd:vX.Y.Z` tag in both
compose files for that engagement and document the pin in the
engagement's README. The default `:latest` posture is for the public
project where currency-of-fixes outweighs frozen-behavior.

## Manual workflow run

To re-trigger the release workflow for an existing tag (e.g. after
fixing a transient registry blip):

1. GitHub → Actions → `Release` → "Run workflow"
2. Provide the existing tag name
3. The workflow checks out at that tag and re-publishes.

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
