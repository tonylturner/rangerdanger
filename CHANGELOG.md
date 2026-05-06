# Changelog

All notable changes to RangerDanger are documented here. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the
project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- `internal/version` package and `GET /api/version` endpoint.
- GitHub Actions CI workflow (`backend`, `services`, `dnp3go`,
  `frontend`, `compose-validate`, advisory `govulncheck`).
- `dnp3go/README.md` documenting the in-tree DNP3 module.
- `docs/release-plan.md` tracking the path to first public release.

### Changed
- Frontend now uses npm with a committed `package-lock.json`
  (`Dockerfile.frontend` switched to `npm ci`, dropped pnpm fallback).
- `docker-compose.yml`: pinned `containd`, `fuxa`, and `openplc_v3`
  base images by digest for reproducible builds.
- `CLAUDE.md`, `docs/architecture.md`, and `README.md` corrected to
  match current 7-exercise lab inventory and `mgmt_net` topology.
- Backend binary renamed `otlab` → `rangerdanger-backend`.

### Removed
- Stale `agents.md` (referred to a prior project name).
- 20 tracked runtime/build artifacts (fuxa appdata with secrets,
  ARM-only dnp3go binaries, oil-plant lab leftovers under `data/`).

### Fixed
- Two failing tests in `backend/internal/containd/` that still
  expected the legacy `/api/v1/config/import` flow (client now uses
  `candidate → commit`).

[Unreleased]: https://github.com/tonylturner/rangerrocks/compare/HEAD
