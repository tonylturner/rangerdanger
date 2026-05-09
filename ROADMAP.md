# Roadmap

This is a forward-looking sketch of where RangerDanger is going. Items
in **Committed** are scoped and likely to land in the named release.
Items in **Backlog** are wanted but not yet scheduled - open to
discussion in [Discussions](https://github.com/tonylturner/rangerdanger/discussions)
or pull requests.

For day-to-day open work, see [`docs/tasks.md`](docs/tasks.md).
For shipped work, see [`CHANGELOG.md`](CHANGELOG.md).

## Committed

### v0.1.x - stabilization

Patch / minor releases that round out v0.1.0 without changing the lab
content or topology.

- **Fail-fast on unmapped network zones in the orchestrator.**
  `createContainer` currently silently no-ops when `node.Networks[i]`
  isn't in `networkNameMap` - a node with a misspelled or legacy zone
  name lands on Docker's default bridge instead of erroring. Surfaced
  by Codex review on PR #26; theoretical for shipped lab content
  (`lab-definitions/**` is clean) but a defensive-engineering win for
  custom labs.
- More test coverage for security-critical paths in the backend
  (firewall apply/compare, scenario validators, the `services/capbank-sim`
  HTTP handlers).
- Frontend smoke-test framework + a handful of high-value tests
  (exercise runner page, network console).
- Content-review pass on the surviving labs after the workshop-deck
  alignment (baseline-assessment, segmentation-requirements,
  remediation-planning, firewall-implementation,
  hardening-configurations, vendor-rdp-compromise,
  validation-evidence) - the structural restructure shipped, but a
  deeper workshop-day playthrough may surface content edits.
- `setup.sh` / `setup.ps1` improvements driven by workshop feedback
  (better error messages, retry on transient pull failures).
- README screenshots / GIFs walking through the exercise runner,
  network console, FUXA HMI, and containd policy view.
- Flip govulncheck CI from advisory (`continue-on-error: true`) to a
  hard gate, once the documented exceptions in
  [`docs/security-known-issues.md`](docs/security-known-issues.md)
  are stable.

### v0.2.0 - lab realism

Bigger lifts that change what the lab simulates.

- **FUXA HMI screens** - one-line diagram, alarm view, and
  segmentation impact view. Currently FUXA loads with a blank canvas;
  configuring it is the largest known content gap.
- **Suricata IDS feed** - wire the optional Suricata sensor sidecar
  (already a containd capability) into the activity feed so students
  see IDS alerts alongside firewall events.
- **PCAP replay** - capture per-exercise PCAPs that students can
  step through after the live run, so a missed packet during attack
  execution can still be analyzed.
- **Command audit ↔ process correlation UI** - show cyber events
  (Modbus FC6 write from 10.10.10.50) side-by-side with their process
  consequence (recloser auto-reclose disabled at T+0.3s, breaker open
  at T+12s).

### v0.3.0 - instructor / workshop operations

Features specifically for running a multi-student workshop.

- **Multi-user RBAC** - instructor view vs. student view, with the
  instructor able to peek at any student's lab state without taking
  it over.
- **Stage-SSD enhancements** - pre-validate tarball integrity, dry-run
  mode, partial-stage updates so a content tweak doesn't require
  rebuilding all 14 images.
- **Workshop console** - a single dashboard showing the cohort's
  exercise progress, which segmentation policies students applied,
  and where they're stuck.

## Backlog

Open ideas, not yet committed. PRs welcome.

- **Additional lab topologies.** A water/wastewater lab is the most
  obvious next OT scenario; a manufacturing-line variant could share
  many simulators with substation but use a different segmentation
  story.
- **More protocols.** OPC UA, IEC 61850 GOOSE, Profinet - each is a
  real ICS protocol students benefit from seeing on the wire.
- **Hardware-in-the-loop bridge.** Attach a real relay or RTU via a
  serial-to-TCP gateway for a hybrid software/hardware lab.
- **Cloud-hosted pre-built labs** for students who can't or won't
  install Docker locally.
- **Curriculum alignment docs.** Map exercises to NERC CIP, IEC
  62443, NIST 800-82 controls so trainers can frame the lab in
  compliance terms without rewriting their own crosswalks.

## Known gaps

These are documented limitations of the current release, not
roadmap items per se - they're called out so users aren't surprised.

- The lab is **single-tenant**. There's no auth on the backend or
  WebSocket terminals; the security model relies on the loopback
  binding established in
  [`SECURITY.md`](SECURITY.md). Multi-user is a v0.3.0 item.
- **Apple Silicon students run OpenPLC under Rosetta** because
  `tuttas/openplc_v3` is amd64-only upstream. Works fine, just
  slower than native.
- **HTTP/3 vulnerability scanner finding** (quic-go transitive via
  gin) - the backend doesn't actually serve HTTP/3, so the
  reachability-flagged finding has no real-world impact. Tracked in
  [`docs/security-known-issues.md`](docs/security-known-issues.md).
