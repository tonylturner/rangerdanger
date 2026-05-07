# RangerDanger Tasks

Active prioritized backlog. For longer-horizon direction see
[`../ROADMAP.md`](../ROADMAP.md); for shipped work see
[`../CHANGELOG.md`](../CHANGELOG.md).

Status: `[ ]` open · `[~]` in progress · `[x]` done

## P1 — next up

Items on the v0.1.2 critical path or blocking the upcoming DefendICS
workshop.

### v0.1.2 release path

- [ ] **Orchestrator fail-fast on unmapped network zones.**
  `createContainer` in `backend/internal/orchestrator/orchestrator.go`
  silently no-ops when `node.Networks[i]` isn't in `networkNameMap`,
  dropping the container onto Docker's default bridge instead of
  returning an error. Add a fail-fast branch returning the error with
  the list of valid zones. Surfaced by Codex on PR #26.
- [ ] **`govulncheck` advisory → hard gate.** Flip
  `continue-on-error: false` on the `govulncheck` job in
  `.github/workflows/ci.yml` once the documented exceptions in
  `security-known-issues.md` (2 docker/docker, 1 quic-go) stay stable
  across two scan cycles.
- [ ] **End-to-end SSD validation.** Run `./stage-ssd.sh /Volumes/SSD v0.1.1`
  and exercise the full SSD → laptop → `docker load` → `up -d` flow
  on:
  - 1 Apple Silicon Mac
  - 1 Intel Mac
  - 1 Windows + Docker Desktop (WSL2)

  Manual hardware step.

### Workshop (DefendICS) blockers

- [ ] **Exercise 2 → Exercise 3 dynamic remediation.** Selections a
  student makes in Exercise 2 (segmentation requirements & rule design)
  must modify Exercise 3's instructions and the firewall config it
  starts from. Currently Ex3 is static.
- [ ] **Workshop agenda alignment final pass.** Re-walk all 9
  exercises against the DefendICS workshop schedule; note any
  timing / sequencing / depth mismatches before the cohort runs them.
- [ ] **Single-student-laptop lab build polish.** Final round of
  "is this comfortable for a student running the whole stack on their
  own laptop" smoothing — startup time, error-state UX, rollback after
  a wrong policy.

## P2 — soon

Important but not workshop-blocking and not on v0.1.2's critical path.

- [ ] **Backend tests** for firewall apply/compare, scenario
  validators, and `services/capbank-sim` HTTP handlers.
- [ ] **Frontend smoke tests.** Pick a framework
  (Playwright or Vitest + Testing Library), wire one CI job, ship 3–4
  high-value tests (exercise runner page renders, network console
  renders, `lab-detail` starts / stops a lab).
- [ ] **Content review pass on the 7 pre-merge exercises:**
  `baseline`, `dnp3-injection`, `modbus-override`, `remediation-planning`,
  `segmentation-requirements`, `validation-evidence`, `vendor-rdp`.
  Read for clarity, time accuracy, and whether the auto-validators
  still match the expected play-through.
- [ ] **`setup.sh` / `setup.ps1`** improvements driven by workshop
  feedback — better error messages on common failure modes, retry on
  transient `docker pull` failures, friendlier "checking your machine"
  output.

## P3 — later

Polish + documentation that don't move correctness.

- [ ] **README screenshots / GIFs** walking through the exercise
  runner, network console, FUXA HMI, and containd policy view.
  Requires running stack to capture.
- [ ] **`docs/quickstart.md` split-out** from the README. README keeps
  the pitch + 5-line quickstart; `docs/quickstart.md` owns the full
  walkthrough (env vars, common errors, where to look when X breaks).

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
