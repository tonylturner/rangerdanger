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

## P2 — soon

Important but not workshop-blocking and not on v0.1.2's critical path.

- [ ] **Backend tests** for scenario validators (the per-step pass /
  fail logic the lab UI hinges on), firewall apply / compare paths,
  and `services/capbank-sim` HTTP handlers.
- [~] **Frontend smoke tests.** Decision-graph contract test
  (`frontend/lib/scenario-decision-graph.test.ts`) walks every
  scenario YAML and asserts every `:::findings-panel` and
  `default-from` reference resolves to a real `:::decision id=`.
  Catches the silent-rename failure mode that motivated this
  bullet without an `@testing-library/react` dep tree. Render
  smokes (scenario-list renders, lab-detail start / stop) still
  open.
- [ ] **`setup.sh` / `setup.ps1`** improvements driven by workshop
  feedback — better error messages on common failure modes, retry
  on transient `docker pull` failures, friendlier "checking your
  machine" output.

## P3 — later

Polish + documentation that don't move correctness.

- [ ] **README screenshots / GIFs** walking through the exercise
  runner, network console, FUXA HMI, and containd policy view.
  Requires running stack to capture.

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
