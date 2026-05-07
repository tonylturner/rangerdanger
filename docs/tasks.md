# RangerDanger Tasks

Active prioritized backlog. For longer-horizon direction see
[`../ROADMAP.md`](../ROADMAP.md); for shipped work see
[`../CHANGELOG.md`](../CHANGELOG.md).

Status: `[ ]` open · `[~]` in progress · `[x]` done

## P1 — next up

Items on the v0.1.2 critical path or blocking the upcoming DefendICS
workshop.

### v0.1.2 release path

- [x] **Orchestrator fail-fast on unmapped network zones.**
  Resolved via `resolveNetworkName` helper in
  `backend/internal/orchestrator/orchestrator.go`; both the primary
  and secondary network attachment paths now return an error listing
  valid zones when the lookup fails. Pinned with
  `orchestrator_test.go` (12 cases). Surfaced by Codex on PR #26.
- [x] **`govulncheck` advisory → hard gate.** Flipped to a hard gate
  via an allowlist filter: `govulncheck -format json` runs against
  each module, the wrapper subtracts the documented OSV IDs in
  `security-known-issues.md` (GO-2026-4887, GO-2026-4883,
  GO-2025-4233), and the build fails if anything new appears. To add
  a new exception: add the triage entry to `security-known-issues.md`
  and append the GOID to `ALLOWED` in `.github/workflows/ci.yml` in
  the same PR.
- [ ] **End-to-end SSD validation.** Run `./stage-ssd.sh /Volumes/SSD v0.1.1`
  and exercise the full SSD → laptop → `docker load` → `up -d` flow
  on:
  - 1 Apple Silicon Mac
  - 1 Intel Mac
  - 1 Windows + Docker Desktop (WSL2)

  Manual hardware step.

### Workshop (DefendICS) blockers

- [~] **Dynamic remediation pipeline — extend to attack exercises.**
  The pipeline already exists end-to-end for `firewall-implementation`
  (Ex4): Ex3 selections persist via `frontend/lib/remediation-plan.ts`,
  `remediation-to-rules.ts` translates them into rule tables /
  validation tests / a containd config, and `injectDynamicContent`
  rewrites Phase 3/5/6 step text. The 5 attack & validation exercises
  (`modbus-override`, `dnp3-command-injection`, `vendor-rdp-compromise`,
  `capbank-switching-attack`, `validation-evidence`) currently only
  show the consequence banner — their step text doesn't change with
  the plan, breaking the narrative promise in `remediation-planning.yml`
  ("if you do not block a path, that attack will remain easier").
  **Deferred** until the manual playthrough below; if exercises get
  rescoped or removed, Track B should be re-scoped against the new
  shape.
- [ ] **Manual exercise playthrough + agenda alignment.** Re-walk all
  9 exercises end-to-end against the DefendICS workshop schedule,
  capturing timing / sequencing / depth mismatches and any rescoping
  decisions. Output: a list of exercise content edits + the input
  needed to scope Track B above.
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
