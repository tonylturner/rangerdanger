# Releasing

This document describes how to cut a RangerDanger release. It is the
runbook for tagging, publishing images to GHCR, and producing the
release-flavor compose file students consume.

## Versioning

RangerDanger follows [Semantic Versioning](https://semver.org/):

- `MAJOR` — breaking changes to the lab topology, container layout, or
  the API surface students rely on.
- `MINOR` — new exercises, new node types, new endpoints, additive
  compose changes.
- `PATCH` — bug fixes, doc updates, image bumps, internal refactors.

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
6. **`docs/release-plan.md`** reflects the actual state of the work.

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

The `release.yml` workflow takes over from there once it is wired up
(see `docs/release-plan.md` section B2).

## What the release workflow will do (when implemented)

1. Build all 13 first-party images for `linux/amd64` and `linux/arm64`
   (except `openplc`, which is amd64-only — upstream constraint).
2. Inject `VERSION=vX.Y.Z`, `COMMIT=$(git rev-parse --short HEAD)`,
   `DATE=$(date -u +%FT%TZ)` via `--build-arg` for the backend image.
3. Push each image to `ghcr.io/tonylturner/rangerdanger-<svc>:vX.Y.Z`
   and re-tag `:latest`.
4. Generate `docker-compose.release.yml` from `docker-compose.yml` by
   replacing every `build:` block with `image: ghcr.io/.../<svc>:vX.Y.Z`.
5. Attach the rendered compose file plus changelog excerpt to the
   GitHub release.

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
