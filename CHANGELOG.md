# Changelog

All notable changes to RangerDanger are documented here. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the
project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- `orchestrator.createContainer` now fails fast with a clear error
  when a node references an unknown network zone, instead of silently
  no-op'ing and letting the container land on Docker's default bridge.
  Pinned with `backend/internal/orchestrator/orchestrator_test.go`.
  Surfaced by Codex review on PR #26.

### CI

- `release.yml` retries the `Build and push` step up to twice on
  failure (with 30s and 60s delays) to absorb transient GHCR 5xx
  during layer uploads. Each retry reuses already-pushed blobs via
  buildkit's layer dedup, so only the failed layer actually re-uploads.
- `ci.yml`'s `govulncheck` job is now a hard gate (was advisory).
  An allowlist of two `docker/docker` OSV IDs (GO-2026-4887,
  GO-2026-4883) — the only findings without an upstream fix — keeps
  known exceptions passing; any new finding fails the build. Closes
  a v0.1.x stabilization item from `docs/tasks.md`.

### Security

- `backend/go.mod`: `quic-go` v0.54.0 → v0.57.0 (clears
  `GO-2025-4233`, `GO-2025-4017`); `golang.org/x/crypto` v0.44.0 →
  v0.50.0 (clears `GO-2025-4134`, `GO-2025-4135`). Surfaced when the
  govulncheck gate flipped above.

### Documentation

- Replaced `docs/release-plan.md` (a v0.1.0-cutover working doc) with
  `docs/tasks.md`, a prioritized P1/P2/P3 backlog. `ROADMAP.md`
  remains the longer-horizon view.

## [v0.1.1] - 2026-05-07

Polish release that lands the work that didn't make the v0.1.0 cut.
Same lab content as v0.1.0; the differences are entirely under the
hood (security posture, test coverage, repo hygiene, contributor
ergonomics).

### Security

- Go toolchain bumped **1.24.13 → 1.25.9** clearing the seven
  remaining stdlib `govulncheck` findings whose `Fixed in` was on
  the 1.25.x line. CI's vulnerability scan output reduced from 18
  findings to 3 (2 `docker/docker` no-upstream-fix + 1 `quic-go`
  transitive in unused HTTP/3 path), all documented in
  `docs/security-known-issues.md`.
- **Trivy image scan** added as a second advisory CI job
  (`.github/workflows/dep-scan.yml`) covering OS-package CVEs in
  the published images that `govulncheck` can't see (Kali rolling,
  Linuxserver webtop bases, `python:3.12-slim`). SARIF output to
  the GitHub Security tab.
- `fuxa_appdata/`, `fuxa_db/`, and seven legacy `data/` files —
  re-introduced by the v0.1.0 distribution-mvp merge — re-untracked.
  Same lab-default-credential class as the existing `containd`/
  `openplc` defaults documented in `SECURITY.md`; cleanup is
  hygiene rather than vulnerability response.
- Legacy oil-plant network mappings (`it_net`, `dmz_net`,
  `ot_control_net`, `ot_safety_net`) finally removed from
  `orchestrator.go` (an earlier "removed" commit message was a no-op).

### Tests

- `backend/internal/server/exec_test.go` — 33 cases pinning the
  command-allowlist behavior on `/api/workshop/exec`, including
  the documented shell-injection bypass and a regression guard
  ensuring every tool the scenarios auto-run stays in the
  allowlist.
- `dnp3go/roundtrip_test.go` — link-frame round-trip across 7 size
  classes, garbage-skipping, CRC rejection, APDU round-trip,
  encoder shape checks. Coverage moved from CRC-only to all four
  protocol layers.

### Tooling

- `setup.sh --check-only` and `setup.ps1 -CheckOnly` — runs
  pre-flight checks (Docker, Compose, ports, disk, memory) and
  exits without installing. Pre-workshop "is my laptop ready?"
  verification.
- `.github/workflows/smoke.yml` — bring-up smoke test on every PR
  and push to main. Builds the stack, hits `/api/health` and
  `/api/build`, confirms 9 exercises load and ≥8 services report
  healthy, dumps logs on failure. Catches startup regressions
  unit tests don't see.

### Community / OSS polish

- `ROADMAP.md` — public forward look (v0.1.x, v0.2.0, v0.3.0,
  backlog). Linked from `README.md`.
- `SUPPORT.md` — where to ask questions, what to expect from
  maintainers, separate channel for security vs commercial
  workshop support.
- `CITATION.cff` — for academic / training / research use; GitHub
  renders this in the sidebar.
- `CODE_OF_CONDUCT.md` — Contributor Covenant 2.1 with
  `conduct@sentinel24.com` reporting address.
- `.github/PULL_REQUEST_TEMPLATE.md` and three issue templates
  (bug report, feature request, contact-routing config including
  the GitHub private security advisory link).

### Documentation

- `docs/architecture.md` frontend pages list refreshed: removed
  the `/hmi` and `/topology` routes that were deleted in the v0.1.0
  merge but never reflected here; removed the `advanced-hmi.tsx`
  reference; added `/knowledge` and an explicit note that the
  operator HMI is FUXA at `/apps/fuxa-hmi/` (proxy route, not a
  Next.js route).
- README's Documentation section now lists ROADMAP/SUPPORT/SECURITY/
  CONTRIBUTING/CHANGELOG; outdated `CLAUDE.md` link replaced with
  the workshop-overview and security-known-issues pointers.
- `dnp3go/README.md` — dropped the dangling "see CLAUDE.md" pointer.

### Repo hygiene

- `CLAUDE.md` untracked (added to `.gitignore`). Local-only AI agent
  context; not useful to public visitors.
- Dead code removed: `Dockerfile.labtools`, `scripts/tools-entrypoint.sh`,
  `scripts/scenarios/` (6 pre-YAML attack scripts), `scripts/seed-fuxa.sh`,
  `scripts/smoke-test-opendss.sh`, `scripts/configure-fuxa.py`,
  `scripts/configure-fuxa-substation.py`. Net: 30 paths removed,
  ~3000 lines deleted.
- Stale `rangerrocks` placeholder in `CONTRIBUTING.md` updated to
  the renamed repo URL.
- Repo profile populated via `gh repo edit`: description, 13 topics,
  GitHub Discussions enabled.

## [v0.1.0] - 2026-05-07

First public release. RangerDanger is an OT/ICS cyber range built
around containd's DPI-capable firewall, packaged as a single-laptop
Docker Compose stack with a 9-exercise substation segmentation lab.

### Lab content

- 9 exercises covering baseline traffic analysis, segmentation
  requirements, remediation planning under labor budget, hands-on
  firewall policy implementation, three attacks (Modbus override,
  DNP3 command injection, vendor RDP compromise), a bonus capacitor
  bank switching attack, and post-change validation with PCAP
  evidence collection.
- Field-device simulators: relay, recloser, regulator, capacitor
  bank, RTAC, historian, GPS clock — each speaking HTTP REST,
  Modbus TCP, and DNP3 TCP simultaneously against shared state.
- OpenDSS feeder physics engine surfacing real energization /
  voltage outcomes from device commands.
- FUXA HMI as the operator interface; OpenPLC for substation
  automation logic.
- Two reference firewall configurations: a permissive baseline
  (`substation-weak.json`) and the target hardened policy
  (`substation-improved.json`).

### Platform

- Backend: Go 1.24.13 + Gin, GORM + SQLite, Docker SDK orchestration.
  Exposes `/api/health`, `/api/build`, `/api/scenarios`,
  `/api/firewall/*`, `/api/workshop/*`, `/api/substation/*`,
  `/api/pcap/*`, `/api/traffic/*`, plus WebSocket terminals.
- Frontend: Next.js 14 + TypeScript with React Flow topology, xterm.js
  terminals, and the in-app exercise runner.
- containd NGFW (`ghcr.io/tonylturner/containd:v0.1.18`) provides
  zone-based firewalling, ICS DPI (Modbus function-code filtering,
  DNP3 protocol awareness), and IT DPI.
- DNP3: in-tree `dnp3go/` standalone Go module — zero external
  dependencies, supporting Read (FC1), Direct Operate (FC5), and
  Select/Operate (FC3/FC4).

### Distribution

- 14 first-party images published to
  `ghcr.io/tonylturner/rangerdanger-*` for `linux/amd64` +
  `linux/arm64` (except `openplc`, which is amd64-only because
  upstream `tuttas/openplc_v3` is amd64-only).
- All upstream images pinned by `@sha256:` digest for reproducible
  builds.
- `docker-compose.release.yml` for image-only deployments (no
  build toolchain required).
- `setup.sh` (mac/linux) and `setup.ps1` (Windows) installers with
  pre-flight checks (Docker reachable, Compose v2, ≥30 GB disk,
  ports 8088/9080/9443/2222 free) and `--from-tarballs` mode for
  offline / SSD installs.
- `stage-ssd.sh` produces an offline distribution bundle
  (`images-amd64.tar`, `images-arm64.tar`, `rangerdanger.tgz`,
  README) in one command.

### Security posture

- All host-exposed ports bound to `127.0.0.1` only — the lab is
  unreachable from any interface other than loopback by default.
- `SECURITY.md` documents the lab-only model and the supported
  patterns for deliberately exposing the stack (SSH local-forward
  recommended, Tailscale-style mesh-VPN supported, specific-LAN
  binding discouraged, `0.0.0.0` actively warned against).
- Default credentials (`containd/containd`, `openplc/openplc`,
  `CONTAIND_JWT_SECRET=rangerdanger-dev`) are baked-in lab
  conveniences and explicitly called out as not-secrets.
- Self-signed lab TLS regenerated by webtop / containd on first run.
- The `/api/workshop/nodes/:nodeId/exec` endpoint and WebSocket
  terminals are intentionally unauthenticated under the loopback
  binding; the command allowlist is documented as a UI auto-run
  guardrail, not a security boundary.

### CI / release

- **CI (`ci.yml`)** fires on every push and PR: backend, services,
  dnp3go, frontend, compose-validate, govulncheck (advisory).
- **Release (`release.yml`)** publishes 14 multi-arch images to
  GHCR on any `v*` tag push. Pre-release tags (containing `-`) do
  not retag `:latest`, so an alpha cannot replace the stable
  pointer.
- **Dependency scan (`dep-scan.yml`)** — Trivy scans the published
  images weekly and on tag push for OS-package CVEs (Kali rolling,
  Linuxserver webtop bases, Python 3.12-slim) that govulncheck
  can't see. Findings upload to the GitHub Security tab via SARIF.
- `.github/dependabot.yml` covers gomod (×3), npm, docker, and
  github-actions ecosystems on a weekly cadence with major-version
  bumps suppressed pre-1.0 for stability.
- Go toolchain pinned to **1.25.9** in all three modules — clears
  every stdlib finding govulncheck reported under earlier patches.
  Only the 2 `docker/docker` (no upstream fix) and 1 `quic-go`
  (transitive in unused HTTP/3 path) findings remain, all
  documented in `docs/security-known-issues.md`.

### Documentation

- `README.md`, `docs/architecture.md`, `docs/api-spec.md`,
  `docs/workshop-overview.md`, `docs/release-plan.md`,
  `docs/security-known-issues.md`, plus per-area READMEs in
  `frontend/`, `services/`, `lab-definitions/`, and `dnp3go/`.
- Community files: `CONTRIBUTING.md`, `SECURITY.md`,
  `CODE_OF_CONDUCT.md` (Contributor Covenant 2.1).
- GitHub workflow templates: `.github/PULL_REQUEST_TEMPLATE.md`,
  `.github/ISSUE_TEMPLATE/{bug_report,feature_request,config}.

### Tests

- `dnp3go/roundtrip_test.go` — link-frame round-trip across 7 size
  classes, garbage-skipping, CRC rejection, APDU round-trip,
  encoder shape checks. Coverage moved from CRC-only to all four
  protocol layers (link, transport, application, encoders).
- `backend/internal/server/exec_test.go` — 33 cases pinning the
  command-allowlist behavior on `/api/workshop/exec`, including
  the documented shell-injection bypass and a regression guard
  that every tool the scenario YAMLs auto-run stays in the
  allowlist.

[Unreleased]: https://github.com/tonylturner/rangerdanger/compare/v0.1.0...HEAD
[v0.1.0]: https://github.com/tonylturner/rangerdanger/releases/tag/v0.1.0
