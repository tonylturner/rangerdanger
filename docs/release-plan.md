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

**What's blocking public flip:** *nothing.* All A-section blockers
resolved 2026-05-06.

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
- [x] Lab-only security callout added to README quickstart in commit
  `d584932`. Mirrors the `SECURITY.md` model: no auth, default
  creds, self-signed TLS, privileged firewall — all called out
  before the `docker compose up -d --build` line.

### A2. Tracked files that shouldn't be in repo

- [x] `.gitignore` updated.
- [x] `git rm --cached` 20 files (fuxa_appdata, fuxa_db, dnp3go
  binaries, tsbuildinfo, legacy `data/` files).
- [x] Removed empty `deploy/` directory.
- [x] Untrack changeset committed.

### A3. Security: command-injection + auth posture

- [x] **Resolved 2026-05-06 (commit `23eef4b`) via Option 3 — loopback
  binding.** Compose port mappings changed to `127.0.0.1:` for all
  four host-exposed ports (8088 proxy, 9080/9443/2222 containd). The
  lab is now genuinely unreachable from any interface but loopback,
  so the unauthenticated `handleWorkshopExec` endpoint and the
  WebSocket terminals stop being a network-exposed risk. The
  exec-endpoint allowlist is preserved as the UI auto-run guardrail
  it was always intended to be, not a security boundary. Added
  `SECURITY.md` "Exposing the lab beyond localhost" runbook (SSH
  local-forward, Tailscale, specific-LAN, 0.0.0.0-don't) for users
  who need to deliberately share the stack.
- [x] No-auth posture documented in `SECURITY.md` and the README
  lab-only callout, framed correctly under the loopback binding.
- [x] CORS contradiction fixed in commit `e75a8d2` —
  `Allow-Credentials: true` now only set when `Allow-Origin` is a
  specific host, never with the wildcard.

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
- [x] Pinned `linuxserver/webtop:ubuntu-mate` and
  `linuxserver/webtop:ubuntu-xfce` by digest in commit `c1a3110`.

---

## B. Release mechanics (required before v0.1.0 tag)

### B1. CI

- [x] `.github/workflows/ci.yml` live with 6 jobs: backend, services,
  dnp3go, frontend, compose-validate, advisory govulncheck. **Proven
  working** against 5 dependabot PRs on 2026-05-06.
- [x] `.github/workflows/release.yml` scaffolded (placeholder; full
  publish wiring tracked in B2).

### B2. GHCR publishing — wire up `release.yml`

- [x] **Done in commit `9a4deb6`.** 14-job matrix in
  `.github/workflows/release.yml`. Triggers on any `v*` tag push;
  builds linux/amd64+linux/arm64 (openplc amd64-only); injects
  VERSION/COMMIT/DATE build-args; pre-release tags don't retag
  `:latest`; per-image GHA cache scope.
- [ ] Run a `v0.0.1-alpha` dry-run tag to validate the pipeline
  end-to-end before tagging real `v0.1.0`.

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

- [x] **Done in commit `9a4deb6`.** `docker-compose.release.yml`
  uses `image: ghcr.io/tonylturner/rangerdanger-<svc>:${VERSION:-latest}`
  for every previously-built service; topology, env vars, sysctls,
  and volumes match `docker-compose.yml` line-for-line. Users:
  `VERSION=v0.1.0 docker compose -f docker-compose.release.yml up -d`.
  Bind-mounts on lab-definitions/, scripts/, proxy/, data/ still
  require the cloned repo or release tarball.

### B4. Versioning

- [x] `backend/internal/version/version.go` with `Version`/`Commit`/
  `Date` ldflags-injected.
- [x] `Dockerfile.backend` plumbs `--build-arg VERSION/COMMIT/DATE`.
  Binary renamed `otlab` → `rangerdanger-backend`.
- [x] `frontend/package.json` has `"version": "0.0.0"`.
- [x] `CHANGELOG.md` (Keep-a-Changelog format).
- [x] `RELEASING.md` runbook.
- [x] `/api/version` proxy collision fixed in commit `e75a8d2` —
  backend endpoint renamed to `/api/build`. Version stamping
  unchanged; FUXA's own `/api/version` is untouched.
- [ ] First tag: `v0.1.0` (gated on B2/B3/B5).

### B5. Scripted installation

- [x] `setup.sh` and `setup.ps1` shipped in commit `d162768`.
  Pre-flight checks: Docker reachable, Compose v2 present, arch
  detection (arm64/amd64), free disk ≥ 30 GB (warns), Docker memory
  ≥ 8 GB (warns), loopback ports 8088/9080/9443/2222 free (hard
  fail). Default mode pulls from GHCR; `--version vX.Y.Z` pins a
  release. Health-checks `/api/health` after `up -d`. Prints next
  steps + SECURITY.md pointer.
- [x] `--from-tarballs <PATH>` mode wired into both scripts: detects
  host arch, loads `images-<arch>.tar` via `docker load`, then runs
  `up -d`. Tarball staging itself is B6.

### B6. SSD / `docker save` flow tested end-to-end

- [ ] Build all 14 images for both arches on a reference machine.
- [ ] `docker save` per arch into `images-amd64.tar` and
  `images-arm64.tar`, plus `rangerdanger.tgz` of the repo.
- [ ] Validate full SSD → laptop → `docker load` → `up -d` flow on:
  - 1 Apple Silicon Mac
  - 1 Intel Mac
  - 1 Windows + Docker Desktop (WSL2)

### B7. README badges

- [x] Added in commit `d584932`. Live CI badge, release version
  badge, and a Go-version badge that reads from `backend/go.mod`.

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

- [x] Healthchecks added to all 7 alpine sims + opendss (commit
  `79673d6`). `rtac_sim` and `backend` `depends_on` use
  `condition: service_healthy` to gate startup ordering.
- [ ] Resource limits (`mem_limit`, `cpus`) — esp. webtop containers.
- [x] Build cache mounts added to `services/Dockerfile`,
  `Dockerfile.kali`, `Dockerfile.eng-ws` Go stages and
  `Dockerfile.frontend` npm stage (commit `c52918e`).

### Code

- [x] Deleted `handleContaindTerminal` (commit `3fe8b09`) — 124
  lines of unwired SSH-fallback with hardcoded creds + `Insecure
  IgnoreHostKey`. Active in-app terminal path uses docker exec via
  `connectTerminal`; external SSH path (`ssh -p 2222`) untouched.
- [x] Hardcoded `http://localhost:9080` removed (commit `f8f4cea`).
  All four sites (backend `server.go`, `orchestrator.go`,
  `nav-sidebar.tsx`, `labs/page.tsx`) now use the same-origin
  `/containd/` path. Bonus: orchestrator's firewall `interface_ips`
  map keys updated from legacy zone names to current ones.
- [x] Comment added to `backend/internal/server/pcap.go`
  (commit `8aa4575`) listing the hardcoded zone IPs and noting they
  come from `lab-definitions/substation-segmentation.yml` and
  `docker-compose.yml`.
- [x] `OTLAB_*` env vars renamed to `RANGERDANGER_*` with
  deprecation alias (commit `7d72865`). `promoteLegacyEnv()` in
  `internal/config/config.go` copies legacy vars at startup with a
  warning log. Compose files + CLAUDE.md + architecture.md
  updated; CONTAIND_JWT_SECRET unchanged (containd-owned).

### Documentation

- [x] Added `frontend/README.md`, `services/README.md`,
  `lab-definitions/README.md` (commit `00e5db2`).
- [x] `docs/api-spec.md` updated (commit `c45d392`) — added
  `/api/build`, `/api/firewall/apply-custom`, refreshed the
  WebSocket terminal section to match the post-`3fe8b09` reality
  (Docker exec for all nodes including the firewall, no SSH path).
- [x] Stripped "OT Lab Trainer" / "lab trainer" / "oil-plant"
  references (commit `bf653f1`). Removed `scripts/dev-up.sh`/
  `dev-down.sh` "lab trainer" banners; deleted 4 legacy network
  mappings in `orchestrator.go`; deleted 8 legacy node types in
  `labs/catalog.go`; deleted 4 legacy zone colors in
  `topology-nodes.tsx`; deleted 2 dead `TestDefaultOilPlant*`
  tests in `client_test.go` (the substation configs are fully
  covered by `firewall_config_test.go`).
- [x] `lab-networks.yml` removed (commit `bf653f1`) — vestigial
  oil-plant topology file with zero references.
- [x] `docs/workshop-overview.md` added (commit `d70ea33`) —
  visitor-facing summary of all 9 exercises with time budgets,
  what's simulated vs. not, the weak-baseline → hardened-target
  arc, and a pointer to SECURITY.md for deployment posture.

### Audit gaps from the merge — reviewed 2026-05-06

- [x] `services/capbank-sim/{main,dnp3,modbus}.go` — clean.
  Follows the same HTTP+Modbus+DNP3 outstation pattern as
  relay/recloser/regulator. DNP3 outstation address 4 (consistent
  with relay=1, recloser=2, regulator=3). Lockout-after-6-operations
  matches the recloser pattern (real-world utility behavior).
  Audit-logged commands via `shared.AuditLog`. No tests — same gap
  as the other sims (tracked under Tests + scanning above).
- [x] `lab-definitions/scenarios/capbank-switching-attack.yml` —
  well-written bonus exercise. Demonstrates a realistic attack
  (rapid switching to exhaust contact wear), uses the lockout
  mechanic the simulator implements, hardened policy correctly
  blocks it.
- [x] `lab-definitions/scenarios/firewall-implementation.yml` —
  30-minute hands-on exercise where the student builds a
  least-privilege containd policy from scratch. Aligns with the
  baseline → requirements → remediation → implementation flow.
- [ ] Quick re-read of updates to existing exercises (baseline,
  dnp3-injection, modbus-override, remediation-planning,
  segmentation-requirements, validation-evidence, vendor-rdp) —
  punted to a content-review pass closer to v0.1.0.

---

## Decisions outstanding

1. ~~A1 history rewrite~~ — **Resolved 2026-05-06: done.**
2. ~~A3 auth/exec posture~~ — **Resolved 2026-05-06: Option 3.**
   Loopback binding via compose port pinning makes the unauth
   endpoints unreachable; `SECURITY.md` documents safe patterns for
   deliberate external access.
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
| `c1a3110` | Pin `linuxserver/webtop:ubuntu-mate` and `:ubuntu-xfce` by digest (A7 follow-up) |
| `e75a8d2` | Rename `/api/version` → `/api/build` (B4 fix); CORS-credentials fix |
| `d584932` | README CI/release badges + lab-only security callout (B7, A1 follow-up) |
| `23eef4b` | **A3 resolved**: loopback-bind all host ports + SECURITY.md external-access runbook |
| `9a4deb6` | **B2 + B3 done**: GHCR multi-arch release workflow + `docker-compose.release.yml` |
| `19208c9` | Trim Kali to minimal package set (4.63 GB → 161 MB amd64 / 643 MB arm64) |
| `88b424e` | Kali qemu fix: install `systemd-standalone-sysusers` to satisfy tcpdump+ssh deps |
| `d162768` | **B5 done**: setup.sh / setup.ps1 installers with --from-tarballs offline mode |
| `3fe8b09` | Removed dead `handleContaindTerminal` (124 lines, hardcoded creds + InsecureIgnoreHostKey) |
| `c52918e` | Build cache mounts on services/, kali, eng-ws, frontend Dockerfiles |
| `79673d6` | Healthchecks on all 8 sims; rtac/backend depends_on use service_healthy |
| `bf653f1` | Oil-plant lab vestiges removed (-163 lines, -1 file) |
| `00e5db2` | Subdirectory READMEs (frontend, services, lab-definitions) |
| `8aa4575` | Memory limits on heavy containers (corp_ws/vendor_jump/eng_ws/kali/fuxa); pcap.go zone IPs comment |
| `f8f4cea` | Hardcoded `localhost:9080` removed at all 4 sites; same-origin `/containd/` proxy used |
| `d70ea33` | `docs/workshop-overview.md` — visitor-facing walkthrough of all 9 exercises |
| `c45d392` | `docs/api-spec.md` — added `/api/build`, `/api/firewall/apply-custom`, refreshed terminal section |
| `7d72865` | `OTLAB_*` env vars → `RANGERDANGER_*` with deprecation alias |

History rewrite procedure preserved in git log; backup mirror clone
at `/tmp/rangerdanger-scrub.git`.
