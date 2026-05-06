# RangerDanger Release Plan

Working punch list for the path from current state → first public
release (`v0.1.0`). Reconciled 2026-05-06 evening.

Status legend: `[ ]` open · `[~]` in progress · `[x]` done · `[?]` needs decision

---

## Status snapshot

**Where we are:** repo is private but ready for public flip in most
respects. CI is wired and giving real signal (proven against 5 dependabot
PRs that fired today). Lab content is current (9 exercises, capbank-sim
DNP3 outstation, firewall implementation exercise) after the
`distribution-mvp` → `main` consolidation. Two-branch model in place.

**What's blocking public flip:** documented lab-only security posture
(A1 follow-up), `handleWorkshopExec` shell-injection decision (A3),
`/api/version` proxy collision (B4 follow-up).

**What's blocking `v0.1.0` tag:** GHCR publish workflow (B2/B3),
release-flavor compose (B3), install scripts (B5), README badges (B7),
SSD/airgap validation (B6).

## Branch model

- **`main`** — primary, stable, releasable. Where new OSS-prep work
  generally lands.
- **`oss-release`** — user's active working branch. Diverges as work
  progresses, fast-forwards or PRs back to `main`.
- No other long-lived branches. `distribution-mvp` was merged into
  main on 2026-05-06 and deleted; its content is permanently reachable
  as the second parent of merge commit `1b6366f`.

---

## A. Pre-public hard blockers

### A1. Secrets and credentials

- [x] Verified the three leaked private keys are throwaway lab
  self-signed certs (issuer == subject).
- [x] Untracked `fuxa_appdata/` (incl. Node-RED `_credentialSecret` and
  bcrypt FUXA admin DB) and `fuxa_db/`.
- [x] `.claude/settings.local.json` not tracked (covered by user-global
  gitignore + project gitignore).
- [x] **History rewrite executed 2026-05-06.** `git filter-repo`
  scrubbed all 6 cert/key blobs across all branches. Force-pushed to
  origin. Backup mirror at `/tmp/rangerdanger-scrub.git`.
- [ ] **Document lab-only security posture in README quickstart.** Add
  a callout: "Binds to localhost only. No authentication. Default
  credentials (`containd/containd`, `openplc/openplc`,
  `CONTAIND_JWT_SECRET=rangerdanger-dev`) are for the lab and must
  never be reused. Never expose this stack to a network you do not
  fully trust." This is the user-facing complement to `SECURITY.md`.

### A2. Tracked files that shouldn't be in repo

- [x] `.gitignore` updated.
- [x] `git rm --cached` 20 files (fuxa_appdata, fuxa_db, dnp3go
  binaries, tsbuildinfo, legacy `data/` files).
- [x] Removed empty `deploy/` directory.
- [x] Untrack changeset committed.

### A3. Security: command-injection + auth posture

- [?] **Decision parked — user wants implications walkthrough first.**
  - **`backend/internal/server/exec.go:69`** — `handleWorkshopExec`
    runs `[]string{"/bin/sh", "-c", req.Command}`; allowlist only
    checks first token, so `nmap; rm -rf /` passes. Either harden
    (drop `-c`, pass argv directly with no shell) or document
    explicitly as lab-only (covered by A1).
- [x] No-auth posture is intentional for single-student-on-laptop
  deployment. Documented in `SECURITY.md`. README documentation
  pending (covered by A1).
- [ ] **CORS contradiction** at `server.go:248-252`:
  `Access-Control-Allow-Origin: *` plus `Allow-Credentials: true` is
  rejected by browsers. Pick one.

### A4. Tests pass

- [x] Fixed 2 stale `containd` tests after candidate/commit refactor.
  Added `TestImportConfigLegacyFallback` for the 404 fallback path.
  Backend tests green.

### A5. Stale agents.md

- [x] Deleted.

### A6. Documentation drift

- [x] `docs/architecture.md` — backend/frontend/proxy network table
  corrected to `mgmt_net` only.
- [x] `CLAUDE.md` — exercise count corrected to **9** (after the
  `distribution-mvp` merge added `capbank-switching-attack` and
  `firewall-implementation`); removed obsolete `/hmi` reference (the
  page was deleted in the merge — FUXA is the canonical HMI); added
  historian/gps/capbank to node table; fixed `OTLAB_DB_PATH` default;
  cleaned Remaining Gaps list.
- [x] `README.md` — exercise table updated to 9 entries with Time
  column (came in via the distribution-mvp merge resolution).

### A7. Image pin policy

- [x] `docker-compose.yml`: pinned `containd:latest` →
  `:v0.1.18@sha256:4674…`.
- [x] `docker-compose.yml`: pinned `frangoteam/fuxa:latest` →
  `@sha256:025e…`.
- [x] `Dockerfile.openplc`: pinned `tuttas/openplc_v3:latest` →
  `@sha256:94fb…`.
- [ ] Pin `linuxserver/webtop:ubuntu-mate` and
  `linuxserver/webtop:ubuntu-xfce` by digest. Both are floating
  rolling tags (used by `corp_ws`, `eng_workstation`, `vendor_jump`).
  Highest-bytes images in the stack — risk of silent upstream
  breakage at student install time. Out of A7's original strict
  scope, but should land before `v0.1.0`.

---

## B. Release mechanics (required before v0.1.0 tag)

### B1. CI

- [x] `.github/workflows/ci.yml` live with 6 jobs: backend, services,
  dnp3go, frontend, compose-validate, advisory govulncheck. **Proven
  working** against 5 dependabot PRs on 2026-05-06.
- [x] `.github/workflows/release.yml` scaffolded (placeholder; full
  publish wiring tracked in B2).

### B2. GHCR publishing — wire up `release.yml`

- [ ] Build first-party images for `linux/amd64` + `linux/arm64`
  (except openplc which is amd64-only).
- [ ] Tag each `:vX.Y.Z` and `:latest`, push to
  `ghcr.io/tonylturner/rangerdanger-<svc>`.
- [ ] Login via `${{ secrets.GITHUB_TOKEN }}`.

| GHCR repo                                         | Source                       | Multi-arch |
| ------------------------------------------------- | ---------------------------- | ---------- |
| `ghcr.io/tonylturner/rangerdanger-backend`        | `Dockerfile.backend`         | amd64+arm64 |
| `ghcr.io/tonylturner/rangerdanger-frontend`       | `Dockerfile.frontend`        | amd64+arm64 |
| `ghcr.io/tonylturner/rangerdanger-kali`           | `Dockerfile.kali`            | amd64+arm64 |
| `ghcr.io/tonylturner/rangerdanger-vendor-jump`    | `Dockerfile.vendor-jump`     | amd64+arm64 |
| `ghcr.io/tonylturner/rangerdanger-eng-ws`         | `Dockerfile.eng-ws`          | amd64+arm64 |
| `ghcr.io/tonylturner/rangerdanger-openplc`        | `Dockerfile.openplc`         | amd64 only |
| `ghcr.io/tonylturner/rangerdanger-rtac-sim`       | `services/Dockerfile`        | amd64+arm64 |
| `ghcr.io/tonylturner/rangerdanger-relay-sim`      | `services/Dockerfile`        | amd64+arm64 |
| `ghcr.io/tonylturner/rangerdanger-recloser-sim`   | `services/Dockerfile`        | amd64+arm64 |
| `ghcr.io/tonylturner/rangerdanger-regulator-sim`  | `services/Dockerfile`        | amd64+arm64 |
| `ghcr.io/tonylturner/rangerdanger-capbank-sim`    | `services/Dockerfile`        | amd64+arm64 |
| `ghcr.io/tonylturner/rangerdanger-historian-sim`  | `services/Dockerfile`        | amd64+arm64 |
| `ghcr.io/tonylturner/rangerdanger-gps-sim`        | `services/Dockerfile`        | amd64+arm64 |
| `ghcr.io/tonylturner/rangerdanger-opendss-sim`    | `services/opendss-sim/...`   | amd64+arm64 |

### B3. Release-flavor compose

- [ ] `docker-compose.release.yml` — every `build:` replaced with
  `image: ghcr.io/tonylturner/rangerdanger-<svc>:vX.Y.Z`. Lets
  students/evaluators do `docker compose -f docker-compose.release.yml
  up -d` with no toolchain.

### B4. Versioning

- [x] `backend/internal/version/version.go` with `Version`/`Commit`/
  `Date` ldflags-injected.
- [x] `Dockerfile.backend` plumbs `--build-arg VERSION/COMMIT/DATE`.
  Binary renamed `otlab` → `rangerdanger-backend`.
- [x] `frontend/package.json` has `"version": "0.0.0"`.
- [x] `CHANGELOG.md` (Keep-a-Changelog format).
- [x] `RELEASING.md` runbook.
- [ ] **Bug: `/api/version` proxy collision.** Backend handler
  registered at `server.go:150` is shadowed by `proxy/nginx.conf:55`
  which routes `/api/version` to FUXA. Fix: either rename backend
  endpoint (suggest `/api/build`) or add an nginx override before
  the FUXA route. The version stamping itself works; just the
  exposing path needs fixing.
- [ ] First tag: `v0.1.0` (gated on B2/B3/B5).

### B5. Scripted installation

- [ ] `setup.sh` (mac/linux) + `setup.ps1` (windows): pre-flight
  (Docker version, free disk ≥ 30 GB, RAM ≥ 8 GB, ports 8088/9080/
  9443/2222 free), `docker compose -f docker-compose.release.yml
  pull && up -d`, smoke check (`curl http://localhost:8088/api/health`).
- [ ] `--from-tarballs <PATH>` mode for SSD/airgap path.

### B6. SSD / `docker save` flow tested end-to-end

- [ ] Build all 14 images for both arches on a reference machine.
- [ ] `docker save` per arch into `images-amd64.tar` and
  `images-arm64.tar`, plus `rangerdanger.tgz` of the repo.
- [ ] Validate full SSD → laptop → `docker load` → `up -d` flow on:
  - 1 Apple Silicon Mac
  - 1 Intel Mac
  - 1 Windows + Docker Desktop (WSL2)

### B7. README badges

- [ ] Add now that CI is live and proven:

```markdown
[![CI](https://github.com/tonylturner/rangerdanger/actions/workflows/ci.yml/badge.svg)](…)
[![Release](https://img.shields.io/github/v/release/tonylturner/rangerdanger)](…)
[![License](https://img.shields.io/github/license/tonylturner/rangerdanger)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/tonylturner/rangerdanger?filename=backend%2Fgo.mod)](…)
```

### B8. Public-repo metadata

- [x] `SECURITY.md` (vuln reporting at `security@sentinel24.com`).
- [x] `CONTRIBUTING.md` (test commands, PR checklist).
- [x] `.github/dependabot.yml` (gomod ×3, npm, docker, github-actions;
  major-version bumps suppressed pre-1.0).
- [x] `LICENSE` confirmed Apache 2.0.
- [ ] `.github/ISSUE_TEMPLATE/` (optional polish).

### B9. Frontend reproducibility

- [x] `frontend/package-lock.json` committed; `Dockerfile.frontend`
  uses `npm ci` (no pnpm fallback).
- [x] `frontend/.eslintrc.json` (`next/core-web-vitals`); CI lint
  passes.

### B10. dnp3go vendoring

- [x] **Decision: keep monorepo.** Documented in CLAUDE.md
  "Deliberate non-gaps". `dnp3go/README.md` added. README links
  point to in-tree `dnp3go/`.

---

## C. Polish (post-MVP-OK)

### Tests + scanning

- [ ] **Triage govulncheck findings.** CI is currently
  `continue-on-error: true` because the scan flagged 12 issues (mostly
  stdlib + transitive `quic-go`). Once cleared, flip to
  hard-fail. Suggest splitting into a separate `dep-scan.yml`
  workflow that runs weekly.
- [ ] Tests for: `backend/internal/server/exec.go` allowlist,
  firewall apply/compare, scenario validators, `dnp3go` round-trip,
  `services/capbank-sim` (no tests yet).
- [ ] Frontend smoke tests (no test framework configured currently).

### Compose hygiene

- [ ] `healthcheck:` blocks for every sim (`/api/health` already
  exists in the binary); switch `depends_on` to
  `condition: service_healthy`.
- [ ] Resource limits (`mem_limit`, `cpus`) — esp. webtop containers.
- [ ] Build cache mounts on `services/Dockerfile`, `Dockerfile.kali`,
  `Dockerfile.eng-ws` Go stages and `Dockerfile.frontend` npm stage
  (only `Dockerfile.backend` has them currently).

### Code

- [ ] Delete or `_disabled.go` `handleContaindTerminal`
  (`backend/internal/server/terminal.go:180-301`) — 120 lines of
  unwired SSH-fallback with hardcoded creds + `InsecureIgnoreHostKey`.
- [ ] Move hardcoded `http://localhost:9080` out of source:
  `backend/internal/server/server.go`,
  `frontend/components/nav-sidebar.tsx`,
  `frontend/app/labs/page.tsx`.
- [ ] Comment in `backend/internal/server/pcap.go` noting that
  hardcoded zone IPs come from the YAML topology.
- [ ] Rename `OTLAB_*` env vars → `RANGERDANGER_*` (with deprecation
  aliases for one release).

### Documentation

- [ ] Add `frontend/README.md`, `services/README.md`,
  `lab-definitions/README.md`.
- [ ] `docs/api-spec.md` — document `/api/firewall/apply-custom` and
  workshop endpoints (`/api/workshop/*`).
- [ ] Strip "OT Lab Trainer" / "lab trainer" / "oil-plant"
  references in `scripts/dev-up.sh:7` and
  `backend/internal/orchestrator/orchestrator.go:26`.
- [ ] Verify `lab-networks.yml` at repo root is used or remove.
- [ ] Add `docs/workshop-overview.md` summarizing the 9 exercises.

### Audit gaps from the merge

The 2026-05-06 merge brought in lab content that wasn't part of the
original audit. These should get a quick review pass before `v0.1.0`:

- [ ] `lab-definitions/scenarios/capbank-switching-attack.yml`
- [ ] `lab-definitions/scenarios/firewall-implementation.yml`
- [ ] `services/capbank-sim/dnp3.go` — DNP3 outstation; does it
  follow the same patterns as relay/recloser/regulator?
- [ ] `services/capbank-sim/main.go`
- [ ] Updates to existing exercises (baseline, dnp3-injection,
  modbus-override, remediation-planning, segmentation-requirements,
  validation-evidence, vendor-rdp).

---

## Decisions outstanding

1. ~~A1 history rewrite~~ — **Resolved 2026-05-06: done.**
2. **A3 auth/exec posture** — harden `handleWorkshopExec` properly,
   or document as lab-only? *(Parked — user wants implications
   walkthrough.)*
3. ~~B10 dnp3go publish~~ — **Resolved: keep monorepo.**
4. ~~Repo name mismatch~~ — **Resolved: renamed to `rangerdanger`.**
5. **Govulncheck strict mode** — when do we flip
   `continue-on-error: false`? After a triage pass on the 12 findings.

---

## Recently completed (2026-05-06)

For session continuity / changelog drafting:

| Commit  | What                                                          |
|---------|---------------------------------------------------------------|
| (multiple) | A1 secrets scrub, A2 untrack, A4 test fixes, A5 agents.md, A6 doc drift, A7 image pins |
| `1b6366f` | Merge `distribution-mvp` into `oss-release`/`main` (71 files, 9 exercises, capbank-sim) |
| `1919a33` | Versioning scaffold + CI workflows + community files (B1, B4, B8, B9) |
| `294b9d9` | Mark A1 / repo-rename resolved in plan                       |
| `794a3c8` | Tighten dependabot policy (no major bumps until v0.1.0)      |
| `66a022b` | Bump 5 frontend deps via local commit (closes 5 dependabot PRs) |

History rewrite procedure preserved in git log; backup mirror clone
at `/tmp/rangerdanger-scrub.git`.
