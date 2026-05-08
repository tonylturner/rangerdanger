# RangerDanger Tasks

Active prioritized backlog. For longer-horizon direction see
[`../ROADMAP.md`](../ROADMAP.md); for shipped work see
[`../CHANGELOG.md`](../CHANGELOG.md).

Status: `[ ]` open · `[~]` in progress · `[x]` done

## P1 — next up

### Workshop (DefendICS) blockers

- [x] **Manual exercise playthrough + agenda alignment.** Done for
  all seven labs (1.2 / 1.3 / 1.4 / 2.2 / 2.3 / 2.3-bonus / 2.4) —
  see `CHANGELOG.md` `[Unreleased]`. The most recent pass landed
  the 2.3 verify-step source-container fix and the 2.3-bonus
  kill-chain framing.
- [x] **Dynamic remediation pipeline — extend to attack labs.**
  All four execute-mode labs now read the student's Lab 1.4 plan
  inline:
  - `firewall-implementation` (Lab 2.2): `injectDynamicContent`
    rewrites Phase 3/5/6 step text from the 1.4 selections.
  - `hardening-configurations` (Lab 2.3): `:::plan-coverage` fence
    before the "Apply hardened policy" step + an inline note
    flagging which actions (pin-rtac-to-field, modbus-dpi,
    dnp3-dpi) close the two attacks.
  - `vendor-rdp-compromise` (Lab 2.3-bonus): same `:::plan-coverage`
    fence + a kill-chain note naming the two requirements
    (vendor-to-ot, non-rtac-to-field) that close the deck case
    study.
  - `validation-evidence` (Lab 2.4): `:::plan-coverage` panel
    surfaces real-time coverage of the student's 1.4 plan against
    their 1.3 design verdicts.
- [ ] **Single-student-laptop lab build polish.** Final round of
  "is this comfortable for a student running the whole stack on
  their own laptop" smoothing — startup time, error-state UX,
  rollback after a wrong policy. Workshop-feedback driven.

### v0.1.3 release path

- [ ] **End-to-end SSD validation.** Run
  `./stage-ssd.sh /Volumes/SSD v0.1.3` and exercise the full
  SSD → laptop → `docker load` → `up -d` flow on:
  - 1 Apple Silicon Mac
  - 1 Intel Mac
  - 1 Windows + Docker Desktop (WSL2)

  Manual hardware step.

## P0 — pending audit-pass-3 fixes (in progress 2026-05-08)

Codex audit pass 3 surfaced two reproducible workshop blockers and
twelve smaller findings on top of v0.1.4. Items below were validated
against a clean rebuild on 2026-05-08; "Not reproduced" findings are
parked under **Open questions** below.

- [~] **P0-A reset.go capbank/clear_alarm.** `/api/workshop/reset`
  returned `success:false` because `clear_alarm` was sent to capbank-sim
  which has no such handler (rejected as unknown command;
  `reset_lockout` already clears alarm). Both `reset.go` and
  `test_runner.go` had the same hardcoded list — extracted to a single
  `resetDeviceCommands` var, dropped the bogus action, and pinned with
  `TestResetCommandsAreSupported` which scans each sim's `case "X":`
  handlers and asserts every reset command resolves. **Status:**
  changes in working tree, awaiting re-audit.
- [~] **P0-B Interface-determinism for PCAP.** Three sites hardcoded
  `eth3 = field`, which broke after F-002 added the 5th network — Docker
  ethN ordering is non-deterministic across hosts (alphabetical network
  name, not compose order). Fixed:
  `backend/internal/server/pcap.go:42` drops the broken `Interfaces`
  pin (containd's PCAP API takes literal kernel iface names, not zone
  names — see open-question below); the backend's `tcpdump -i any`
  fallback in the same file is fully drift-proof and is what's actually
  used at runtime.
  `docker-compose.yml` and `docker-compose.release.yml` drop
  `CONTAIND_CAPTURE_IFACES` for the same reason (containd was
  failing at boot with `interface wan not found` once we tried zone
  names there).
  `lab-definitions/scenarios/validation-evidence.yml` switches student
  tcpdump to `tcpdump -i any -nn 'net 10.40.40.0/24 ...'` so it works
  regardless of which `ethN` field-net lands on. Bonus: release
  compose was missing `CONTAIND_AUTO_LAN3_SUBNET=10.99.99.0/24`
  (lockstep drift codex caught) — added back.

## P1 — pending audit-pass-3 fixes

- [~] **P1-A `lab-commands-smoke.sh` apply-failure detection.**
  Previous `apply_policy()` swallowed curl errors with `2>/dev/null`
  and returned 0 unconditionally; the script could report 65/65 PASS
  with broken policy state. Now checks curl exit code AND verifies
  `/api/firewall/active` reflects the requested config, exiting 1
  on either failure. Probe-rc 1-7-as-PASS rule for actual probes
  is unchanged (intentional for "host unreachable" / "connection
  refused").
- [~] **P1-B Offline / SSD path no longer pulls from GHCR.** New
  `docker-compose.offline.yml` overlay sets `pull_policy: never` on
  every release-image service. `setup.sh --from-tarballs` and
  `setup.ps1 -FromTarballs` now compose `-f release.yml -f offline.yml`
  automatically.
- [~] **P1-C `stage-ssd.sh` fail-fast on missing image.** Was
  `warn ... skipping` then `docker save` against the unfiltered input
  list (could include stale local copies or fail mid-save). Now
  `die ...` on any pull failure; `docker save` operates on an
  explicit pulled-this-run list.
- [~] **P1-D Setup workshop-readiness gate.** `setup.sh` and
  `setup.ps1` now probe `/api/firewall/health`, apply weak +
  improved, and `/api/workshop/reset` after the existing
  `/api/health` check, with a `--skip-firewall-gate` flag for
  developer iteration. Catches containd drift / mgmt-subnet
  misconfigurations / sim-warmup races at setup time rather than
  at lab time.

## P2 — soon (audit-pass-3 + carried-over)

Important but not workshop-blocking and not on v0.1.2's critical path.

- [~] **P2-A `docs/architecture.md` rewrite.** Removed the stale
  "RTAC field polling does not transit firewall" claim — `rtac-harden.sh`
  has forced firewall transit since v0.1.2. Corrected the multi-
  homed table (2 networks, not 3 — no physics_net leg). Corrected the
  source-pin IP (10.30.30.20, not 10.40.40.10). Replaced the
  fragile eth-index column with a containd zone-name column and
  added an explicit note that ethN ordering is non-deterministic
  across hosts. Added a "Multi-homed RTAC with kernel-pinned
  routing" section explaining the compensating control.
- [~] **P2-B Smoke runs on `audit-oss` and `oss-release`.** Was
  `[main]` only — release-branch work shipped without the workshop-
  critical Docker smoke gate. Both branches added to push/PR
  triggers in `.github/workflows/smoke.yml`.
- [~] **P2-C `CONTRIBUTING.md` adds `npm test`.** Was missing while
  CI ran it; contributors could pass the documented local checks
  and still trip CI.
- [~] **P2-E containd:latest policy documented in `RELEASING.md`.**
  New section explains the "fix containd, not the pin" contract,
  the workshop-day determinism trade-off, and the three mitigations
  (pre-pull + digest lock, setup-time firewall gate, stage-to-SSD).
- [~] **Frontend smoke tests.** Decision-graph contract test
  (`frontend/lib/scenario-decision-graph.test.ts`) walks every
  scenario YAML and asserts every `:::findings-panel` and
  `default-from` reference resolves to a real `:::decision id=`.
  Catches the silent-rename failure mode that motivated this
  bullet without an `@testing-library/react` dep tree. Render
  smokes (scenario-list renders, lab-detail start / stop) still
  open.
- [ ] **`setup.sh` / `setup.ps1`** workshop-feedback polish (separate
  from the P1-D readiness gate above) — friendlier error messages
  on common failure modes, "checking your machine" output ergonomics,
  port-conflict diagnostics improvements.

## P3 — later

Polish + documentation that don't move correctness.

- [~] **README screenshots / GIFs** walking through the exercise
  runner, network console, FUXA HMI, and containd policy view.
  Hero-image polish landed (RangerDanger lockup +
  documentation-table icons + dropped duplicated zone table).
  Real screenshots of the running stack still outstanding —
  requires running stack to capture.

## Recently shipped (since the last refresh of this file)

Highlights — see `../CHANGELOG.md` for the full list:

- **Lab restructure** — 9 sequentially-numbered exercises → 7 labs
  aligned to the DefendICS deck (1.2 / 1.3 / 1.4 / 2.2 / 2.3 /
  2.3-bonus / 2.4). Removed: `modbus-override`,
  `dnp3-command-injection`, `capbank-switching-attack`. Added:
  `hardening-configurations`. Rebuilt: `vendor-rdp-compromise` as
  an actual RDP/VNC pivot exercise.
- **`Scenario.Order` field** changed from `int` to `string` across
  backend + frontend so `order:` in YAML is the workshop lab
  number directly (`"1.2"`, `"2.3-bonus"`, etc).
- **Lab 2.2 / 2.4 CLI / UI rewrite** — replaced curl-as-CLI hints
  with real containd Web UI walkthrough + appliance CLI commands
  (`show running-config`, `set firewall rule`, `commit`, `show
  audit`, `export config`).
- **`fw-1` in-app terminal** auto-launches `containd cli` (with
  bash fallback on CLI error). Wraps containd v0.1.19's
  `shell` / `bash` / `exit` behavior so all four leave the loop.
- **Containd dependency** moved from a digest-pinned per-release
  bump to `containd:latest` — both repos move together by
  convention; `docker compose pull` picks up containd security
  fixes automatically.
- **Smoke test** updated for the new lab inventory (`scripts/smoke-test.sh`
  + `.github/workflows/smoke.yml`); validates exact (order, id)
  tuples + per-lab step counts.
- **Orchestrator fail-fast on unmapped network zones**
  (Codex-flagged). Pinned with `orchestrator_test.go`.
- **`govulncheck` advisory → hard gate** with allowlist of
  documented exceptions. Bumped `quic-go` and `golang.org/x/crypto`
  to clear non-allowlisted findings.
- **GHCR push retry** in `release.yml` — three attempts with
  backoff to absorb transient 5xx during layer uploads.

## Open questions (audit-pass-3)

- **Codex P0-001 (firewall apply 502/403 on fresh rebuild)** — did
  not reproduce on a clean `docker compose down -v --remove-orphans`
  + `up -d --build` against current source on Docker Engine 29.2.0
  with containd image digest `sha256:c73d163...`. Apply weak/improved
  + firewall/health all returned 200; firewall-smoke 52/52,
  lab-commands-smoke 65/65. Suspected codex environment had a stale
  containd image cache or raced sim startup. **No code change made**;
  the new P1-D setup-time gate would catch this class of failure
  if it ever recurs. Re-investigate only if reproduced.
- **containd CAPTURE_IFACES zone-name support** — containd's policy
  engine autobinds zone names (wan/dmz/lan1/lan2/lan3) to ethN by
  subnet (commit 5f31128), but the PCAP path resolves
  `CONTAIND_CAPTURE_IFACES` and `pcap.start.interfaces[]` as literal
  kernel interface names via netlink. Setting zone names there fails
  at boot with "interface wan not found". This is a containd-side
  inconsistency. RangerDanger works around it by relying on the
  backend's `tcpdump -i any` fallback. **Upstream issue to file:**
  make containd's PCAP path autobind zone names like the policy
  path does, then re-pin
  `Interfaces: []string{"wan","dmz","lan1","lan2","lan3"}` in
  `pcap.go` and restore `CONTAIND_CAPTURE_IFACES` to both compose
  files.
- **`exec.go` allowlist hardening** — pre-existing parked decision
  (lab-only loopback-bound, allowlist documented as UI guardrail
  not security boundary). Re-evaluate if the deployment model
  changes.

## Out of scope here

Bigger features tracked in [`../ROADMAP.md`](../ROADMAP.md):

- **v0.2.0** — FUXA HMI screens, Suricata IDS feed, PCAP replay,
  command-audit ↔ process-consequence UI
- **v0.3.0** — multi-user RBAC, instructor / student split, workshop
  console, stage-SSD enhancements
- **Backlog** — water / wastewater lab, more protocols (OPC UA, GOOSE,
  Profinet), HIL bridge, cloud-hosted labs, curriculum-control mapping

## Process

When you close a task here, move the bullet into the appropriate
`CHANGELOG.md` section (`Unreleased` → `vX.Y.Z` on tag) rather than
just deleting it. `ROADMAP.md` gets updated when scope or priority
changes between releases.
