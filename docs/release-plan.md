# RangerDanger Release Plan

Working punch list for the path from current state → first public release.
Updated as items land. Severity follows the audit: **BLOCKER** (must
resolve before flipping public), **REQUIRED** (must land before tagging
v0.1.0), **POLISH** (nice-to-have, post-MVP fine).

Status legend: `[ ]` open · `[~]` in progress · `[x]` done · `[?]` needs decision

---

## A. Pre-public hard blockers

### A1. Secrets and credentials

- [x] Verify the three leaked private keys in commits `ba34183` /
  `6d9c1c9` / `0261366` are throwaway lab self-signed certs (issuer ==
  subject; webtop and containd defaults). Confirmed on 2026-05-06: zero
  real-world risk if they leak.
- [x] Untrack `fuxa_appdata/node-red/.config.runtime.json` (Node-RED
  `_credentialSecret`).
- [x] Untrack `fuxa_appdata/users.fuxap.db` (bcrypt hash for FUXA admin).
- [x] Confirm `.claude/settings.local.json` is not tracked (covered by
  user-global gitignore; project gitignore now also excludes `.claude/`
  for safety).
- [?] **Decision needed:** rewrite git history with `git filter-repo` to
  scrub the 3 `.key` blobs from `ba34183` / `6d9c1c9` / `0261366`?
  - **Argument to do it:** clean history, no awkward "this was
    secret-ish at one point" footnote. Free since no external clones
    exist (repo is still private).
  - **Argument to skip:** keys are throwaway self-signed lab certs,
    never deployed; harm from leak is zero. Rewriting changes every
    commit hash going forward.
  - **Recommendation:** rewrite now while the repo is still private and
    nobody else has cloned. See `A1.scrub.md` (TODO) for the exact
    `filter-repo` command and disruption notes.
- [ ] Document the `CONTAIND_JWT_SECRET=rangerdanger-dev` default and
  default lab creds (`containd/containd`, `openplc/openplc`) explicitly
  as **lab-only** in README.

### A2. Tracked files that shouldn't be in repo

- [x] `.gitignore` updated: `.claude/`, `.cursor/`, `.idea/`, `.vscode/`,
  `*.tsbuildinfo`, `dnp3go/dnp3cmd`, `dnp3go/dnp3poll`, `fuxa_appdata/`,
  `fuxa_db/`, `fuxa_logs/`, `deploy/`.
- [x] `git rm --cached` on:
  - `fuxa_appdata/` (9 files including the credential-secret + bcrypt DB)
  - `fuxa_db/currentTagReadings.db`
  - `dnp3go/dnp3cmd`, `dnp3go/dnp3poll` (3.4 MB ARM64 mach-O each, built
    fresh in Dockerfiles anyway)
  - `frontend/tsconfig.tsbuildinfo`
  - 7 stale `data/` files from the prior oil-plant lab (firewall config,
    fuxa_control/view appdata, plc_compressor/process/safety/utilities)
- [x] Removed empty `deploy/` directory.
- [ ] Commit the untrack changeset (separate commit from any code changes).

### A3. Security: command-injection + auth posture

- [ ] **`backend/internal/server/exec.go:69`** — `handleWorkshopExec` runs
  `[]string{"/bin/sh", "-c", req.Command}`; allowlist only checks the
  first token, so `nmap; rm -rf /` passes. Either:
  - drop `-c`, pass argv directly with no shell (preferred), or
  - reject any input containing `;|&\``$( ><` in the allowlist.
- [ ] **No auth middleware** on any backend endpoint
  (`backend/internal/server/server.go:232-261` only sets CORS). This is
  intentional for single-student-on-laptop deployment, but README must
  state explicitly: "binds to localhost only, no auth, never expose to a
  network you don't fully trust."
- [ ] Fix CORS contradiction at `server.go:248-252`:
  `Access-Control-Allow-Origin: *` plus `Allow-Credentials: true` is
  rejected by browsers. Pick one.

### A4. CI is red on day 1

- [x] Fixed 2 failing tests in `backend/internal/containd/`:
  `TestImportConfigSuccess` and `TestSeedConfigIfNeededSuccess` updated
  to expect the candidate/commit flow. Added
  `TestImportConfigLegacyFallback` to cover the 404→`/config/import`
  fallback path. `go test ./...` now green.

### A5. Stale `agents.md` actively misleads

- [x] Deleted `agents.md`. (Was titled "Agent Instructions for OT Lab
  Trainer", referenced `it_net`/`dmz_net`/`ot_safety_net`, OPNsense,
  Suricata-in-DMZ — none of which exist.)

### A6. Documentation drifted from reality

- [x] **`docs/architecture.md:38-40`** — backend/frontend/proxy network
  table corrected. All three pinned to `mgmt_net` only with explanatory
  paragraph about which lab nodes get the optional mgmt leg.
- [x] **`CLAUDE.md`** — fixed:
  - Exercise count "3 attack scenarios" → "7 exercises"
    (`lab-definitions/scenarios/` actually contains 7 YAMLs:
    baseline, remediation-planning, segmentation-requirements,
    vendor-rdp, modbus-override, dnp3-injection, validation-evidence)
  - Added `historian_sim`, `gps_sim`, `capbank_sim` to node table
  - `OTLAB_DB_PATH` default fixed to `/data/rangerdanger.db`
  - Removed "RTAC as DNP3 master" from Remaining Gaps (done — see
    `services/rtac-sim/dnp3_poll.go`)
  - Kept "dnp3go publish" in Remaining Gaps with note that the GitHub
    repo `tonylturner/dnp3go` does not yet exist (verified via
    `gh repo view`); README badges link to that future repo.
  - Note: `/hmi` route claim is **correct** —
    `frontend/app/hmi/page.tsx` does exist (the original audit agent
    missed it in its enumeration).
- [x] **`README.md`** — exercise table updated from 6 → 7 entries
  (was missing `remediation-planning` at order=1).

### A7. `:latest` image pins

- [x] Pinned `docker-compose.yml` `ghcr.io/tonylturner/containd:latest`
  → `:v0.1.18@sha256:4674396e309447a2ce4f84d3feb42750cc6c5719825c49e32d341e621db894d6`.
- [x] Pinned `docker-compose.yml` `frangoteam/fuxa:latest`
  → `@sha256:025e693971f72de9fabf2c811296f9dca3854dc501cb309b02302c7b94717d0f`
  (multi-arch index, fuxa 1.3.1 series).
- [x] Pinned `Dockerfile.openplc` `tuttas/openplc_v3:latest`
  → `@sha256:94fb9e8387340af716211454664980dc0e97924067712cf573a1e467c3a37722`.
- [ ] Future: also pin `linuxserver/webtop:ubuntu-{mate,xfce}` (used by
  corp_ws and as Dockerfile bases for eng-ws and vendor-jump). These
  ship floating distro tags only — pin by digest. Out of A7's original
  scope, tracked in C polish.

---

## B. Release mechanics (required before v0.1.0 tag)

### B1. CI

- [ ] `.github/workflows/ci.yml`: trigger on push + PR; jobs for
  `backend`, `services`, `dnp3go`, `frontend`, `compose-validate`,
  `govulncheck`, `trivy fs`.
- [ ] `.github/workflows/release.yml`: trigger on tag `v*`; build + push
  13 first-party images to GHCR (multi-arch where possible).

### B2. GHCR — images to publish on `release.yml`

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

Tag each `:vX.Y.Z` and `:latest`. Login via `${{ secrets.GITHUB_TOKEN }}`.

### B3. Release-flavor compose

- [ ] `docker-compose.release.yml` — every `build:` replaced with
  `image: ghcr.io/tonylturner/rangerdanger-<svc>:vX.Y.Z`. Students /
  evaluators run `docker compose -f docker-compose.release.yml up -d`
  with no toolchain.

### B4. Versioning

- [ ] `backend/internal/version/version.go` with `Version` const,
  populated via `-ldflags "-X .../version.Version=$(git describe)"`.
- [ ] `GET /api/version` endpoint in backend.
- [ ] `frontend/package.json` `version` field.
- [ ] `CHANGELOG.md` (Keep-a-Changelog format).
- [ ] `RELEASING.md` runbook.
- [ ] First tag: `v0.1.0`.

### B5. Scripted installation

- [ ] `setup.sh` (mac/linux) + `setup.ps1` (windows): pre-flight (Docker
  version, free disk ≥ 30 GB, RAM ≥ 8 GB, ports 8088/9080/9443/2222
  free), `docker compose -f docker-compose.release.yml pull && up -d`,
  smoke check (`curl http://localhost:8088/api/health`).
- [ ] `--from-tarballs <PATH>` mode for SSD/airgap path.

### B6. SSD / `docker save` flow tested end-to-end

- [ ] Build all 13 images for both arches on a reference machine.
- [ ] `docker save` per arch into `images-amd64.tar` and
  `images-arm64.tar`, plus `rangerdanger.tgz` of the repo.
- [ ] Validate full SSD → laptop → `docker load` → `up -d` flow on:
  - 1 Apple Silicon Mac
  - 1 Intel Mac
  - 1 Windows + Docker Desktop (WSL2)

### B7. README badges

Once CI exists, add:

```markdown
[![CI](https://github.com/tonylturner/rangerdanger/actions/workflows/ci.yml/badge.svg)](…)
[![Release](https://img.shields.io/github/v/release/tonylturner/rangerdanger)](…)
[![License](https://img.shields.io/github/license/tonylturner/rangerdanger)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/tonylturner/rangerdanger?filename=backend%2Fgo.mod)](…)
```

### B8. Public-repo metadata

- [ ] `SECURITY.md` (vulnerability reporting address)
- [ ] `CONTRIBUTING.md` (test commands, code style, PR checklist)
- [ ] `.github/dependabot.yml` (gomod ×3, npm, docker, github-actions)
- [ ] `.github/ISSUE_TEMPLATE/` (optional)
- [ ] Confirm `LICENSE` (Apache 2.0 currently — fine)

### B9. Frontend reproducibility

- [ ] Pick package manager (npm or pnpm), commit its lockfile, drop
  `Dockerfile.frontend:3-4` `|| pnpm install` fallback.
- [ ] Run `npx next lint --strict` once, commit `.eslintrc.json`.

### B10. `dnp3go` replace directive

- [x] **Decision: keep monorepo.** `dnp3go/` stays vendored as a
  standalone Go module within RangerDanger, consumed via the
  `replace` directive in `services/go.mod` and direct `COPY` in
  `Dockerfile.kali` / `Dockerfile.eng-ws`. Verified the GitHub repo
  `tonylturner/dnp3go` does not exist; updated README links 124 +
  229 to point to `dnp3go/` in-tree, added `dnp3go/README.md`, and
  documented the choice in CLAUDE.md "Deliberate non-gaps".

---

## C. Polish (post-MVP)

- [ ] Tests for `exec.go` allowlist, firewall apply, scenario validators,
  dnp3go round-trip; frontend smoke tests.
- [ ] `healthcheck:` blocks for every sim (`/api/health` already exists);
  switch `depends_on` to `condition: service_healthy`.
- [ ] Resource limits in compose (esp. webtop containers).
- [ ] Delete or `_disabled.go` `handleContaindTerminal`
  (`backend/internal/server/terminal.go:180-301`) — 120 lines of
  unwired SSH-fallback with hardcoded creds + `InsecureIgnoreHostKey`.
- [ ] Move hardcoded `http://localhost:9080` out of source:
  `backend/internal/server/server.go:811`,
  `frontend/app/labs/page.tsx:82`,
  `frontend/components/nav-sidebar.tsx:58`.
- [ ] Comment in `backend/internal/server/pcap.go:514-539` noting that
  hardcoded zone IPs come from the YAML topology.
- [ ] Rename `OTLAB_*` env vars → `RANGERDANGER_*` (with deprecation
  aliases for one release).
- [ ] Add `frontend/README.md`, `services/README.md`,
  `lab-definitions/README.md`.
- [ ] `docs/api-spec.md` — document `/api/firewall/apply-custom`.
- [ ] Build cache mounts on services/kali/eng-ws Go stages and frontend
  pnpm stage.
- [ ] Strip "OT Lab Trainer" / "lab trainer" / "oil-plant" references in
  `scripts/dev-up.sh:7` and
  `backend/internal/orchestrator/orchestrator.go:26`.
- [ ] Verify `lab-networks.yml` at repo root is used or remove.
- [ ] Add `docs/workshop-overview.md` summarizing the 9 exercises.

---

## Decisions outstanding

1. **A1 history rewrite**: do it now (recommended, while repo is still
   private) or skip?
2. **A3 auth/exec posture**: harden `handleWorkshopExec` properly, or
   ship as "lab-only, localhost-bound, no auth" with prominent README
   warning?
3. ~~**B10 dnp3go**: keep monorepo, or publish as standalone module?~~
   **Resolved 2026-05-06:** keep monorepo; `dnp3go/` stays vendored.
4. **Repo name mismatch**: GitHub remote is
   `git@github.com:tonylturner/rangerrocks.git` but the project is
   called rangerdanger throughout the code, README, badges, and image
   names. Rename the GitHub repo to `rangerdanger` before going public,
   or rename the project? Renaming the repo on GitHub is supported
   (auto-redirects), so renaming-the-repo is the lower-friction path.

---

## Appendix — history rewrite procedure (PENDING SIGN-OFF)

**Why now:** repo is private, 0 forks, 1 collaborator (owner). Anyone
who has previously cloned will need to re-clone — but only the owner has
ever cloned it, so blast radius is zero.

**What to scrub:** 6 blobs across 3 commits.

| Commit  | Date       | Blobs                                                      |
| ------- | ---------- | ---------------------------------------------------------- |
| ba34183 | 2026-01-30 | `data/firewall/tls/server.{crt,key}` (containd self-signed) |
| 6d9c1c9 | 2026-02-02 | `data/{ews,jumpbox}_config/ssl/cert.{pem,key}` (webtop self-signed) |
| 0261366 | 2026-03-16 | (touched the same paths in a build commit)                |

**Procedure:**

```bash
# 1. Install git-filter-repo if needed
brew install git-filter-repo

# 2. From a clean clone (NOT this working tree)
cd /tmp
git clone --mirror git@github.com:tonylturner/rangerrocks.git rangerdanger-scrub.git
cd rangerdanger-scrub.git

# 3. Run filter-repo
git filter-repo --invert-paths \
  --path data/ews_config/ssl/cert.key \
  --path data/ews_config/ssl/cert.pem \
  --path data/jumpbox_config/ssl/cert.key \
  --path data/jumpbox_config/ssl/cert.pem \
  --path data/firewall/tls/server.key \
  --path data/firewall/tls/server.crt

# 4. Verify the blobs are gone
git rev-list --all --objects | grep -E '\.(key|pem|crt)$' || echo "scrubbed"

# 5. Force-push the scrubbed history back to origin
git remote add origin git@github.com:tonylturner/rangerrocks.git
git push --mirror --force

# 6. In the working tree, fetch + reset so local matches scrubbed remote
cd ~/Documents/GitHub/rangerdanger
git fetch origin
git reset --hard origin/distribution-mvp   # or whichever branch is current
```

**Disruption:** all commit SHAs from the earliest affected commit
forward change. Any other branches, tags, or PRs would need
re-creating. Currently zero branches besides `main` and
`distribution-mvp`, no tags, no PRs.

**Should we also scrub:** the dead-weight blobs we just untracked
(`fuxa_appdata/`, `dnp3go/dnp3cmd`, etc.)? They aren't secrets, but
they bloat `.git`. Adding their paths to the same `--path` list is
~free during the rewrite and shrinks the repo.
