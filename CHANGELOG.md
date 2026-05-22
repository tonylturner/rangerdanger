# Changelog

All notable changes to RangerDanger are documented here. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the
project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v0.1.17] - 2026-05-22

First-class Windows support. The same setup script, smoke tests, and
maintainer SSD-staging tools that have worked on macOS / Linux since
v0.1.0 now run natively on Windows PowerShell 5.1+ with no WSL or
Git-Bash dependency, AND the ICS DPI rules in Labs 2.3 / 2.3-bonus
now actually enforce on Docker Desktop's WSL2 backend (which ships
without `CONFIG_NFT_QUEUE=y` and silently drops `queue num <N>` nft
rules without it).

### Added

- **PowerShell siblings for every host-side script.** Workshop
  students and Windows maintainers no longer need WSL or Git Bash:
  - `setup.ps1` -- already shipped; PS 5.1 fixes folded in this release
    (see "Fixed" below).
  - `scripts/{dev-up,dev-down,seed-labs}.ps1` -- dev wrappers.
  - `scripts/{smoke-test,firewall-smoke,events-smoke,lab-commands-smoke,validation-report}.ps1`
    -- full Windows parity for every smoke + health check.
  - `stage-ssd.ps1`, `stage-ssd-delta.ps1` -- maintainer offline-media
    helpers, including downloading the prebuilt WSL2 kernel into the
    SSD bundle so air-gapped Windows workshops also get ICS DPI.
  - All ASCII-only and BOM-free; PS 5.1 reads non-BOM .ps1 as CP1252
    so non-ASCII characters caused the v0.1.16 setup.ps1 to fail at
    parse time. The new `.github/workflows/smoke-windows.yml` parse-
    checks every .ps1 under both PS 5.1 and PS 7 on every PR.
- **Custom WSL2 kernel for ICS DPI.** Microsoft's stock WSL2 kernel
  does not enable `CONFIG_NFT_QUEUE=y`, so the lab's `nft ... queue
  num <N>` rules silently fail to load and the improved policy's
  ICS-DPI behavior reduces to L4-only. RangerDanger now ships a
  prebuilt vanilla `microsoft/WSL2-Linux-Kernel` (pinned to
  `linux-msft-wsl-5.15.153.1`) with a three-line Kconfig overlay
  (`CONFIG_NFT_QUEUE=y` + two related netfilter flags), built by
  `.github/workflows/build-wsl-kernel.yml` on every release tag and
  attached to the GitHub Release as `rangerdanger-wsl2-kernel` +
  `.sha256` + the resolved `.config`. `setup.ps1` detects the
  missing feature, downloads + sha256-verifies the kernel, merges
  `kernel=` into `%USERPROFILE%\.wslconfig` (preserving every other
  key, writing a `.wslconfig.bak`), runs `wsl --shutdown`, polls
  Docker Desktop to reconnect, and re-probes. See `wsl-kernel/README.md`
  for the full supply-chain story.
- **Post-workshop cleanup.** `scripts/uninstall-rangerdanger.{ps1,sh}`
  tears the stack down, removes lab volumes (DB / captures / sim
  state), optionally removes the ~6 GB of images, and on Windows
  reverts the custom WSL2 kernel + restores `.wslconfig.bak`. The
  setup-end banner now points students at this script so the custom
  kernel does not persist on a student's machine past the workshop.
- **Windows lint CI workflow.** `smoke-windows.yml` parses every
  `.ps1` under both `pwsh` and `powershell.exe`, runs an ASCII /
  no-BOM guard, and walks `setup.ps1 -CheckOnly` end-to-end. Guards
  against the encoding + native-exe-stderr regressions PS 5.1 catches
  that macOS dev does not.

### Fixed

- **`setup.ps1` parses under Windows PowerShell 5.1.** The previous
  release contained em-dashes and box-drawing characters in
  comments and `Die`/`Warn` strings. PS 5.1 reads .ps1 files as
  CP1252 when no UTF-8 BOM is present, mangling those characters
  into mojibake whose embedded quote-bytes prematurely terminated
  strings -- producing parser errors blaming lines hundreds of
  lines from the actual source. All non-ASCII chars in .ps1 files
  are replaced with ASCII equivalents (`--`, `-`, `->`), and the
  new lint workflow enforces this on every PR.
- **`setup.ps1` Docker-engine check no longer misfires.** The
  previous `try { docker info 2>&1 | Out-Null } catch { Die ... }`
  pattern combined with `$ErrorActionPreference = "Stop"` and the
  harmless `WARNING: No blkio throttle.read_bps_device support`
  that Docker emits on WSL2 caused the check to throw a
  `NativeCommandError` even when Docker was reachable and exited 0.
  Replaced with a scoped `& { $ErrorActionPreference =
  'SilentlyContinue'; docker info *>$null }` + `$LASTEXITCODE`
  check. Same pattern applied to the second `docker info` call
  (MemTotal lookup).
- **`install-wsl-kernel.ps1` sha256 verify on auto-download.**
  GitHub serves `.sha256` release assets with `Content-Type:
  application/octet-stream`, which makes `Invoke-WebRequest`'s
  `.Content` return `Byte[]` on PS 5.1 instead of a string. Splitting
  the byte array on whitespace yielded the first byte's ASCII code
  (`54` for `'6'`) instead of the hex digest, causing every public
  download to fail with a misleading `sha256 mismatch: expected: 54`.
  The .sha256 file now downloads to a temp file and reads back as
  text, matching the pattern already used for the kernel binary.
- **`install-wsl-kernel.ps1 -Restore` cleans up an effectively-empty
  `.wslconfig`.** When setup.ps1 created a `.wslconfig` on a machine
  that did not have one before, `-Restore` stripped our `kernel=`
  line but left an orphan `[wsl2]` section header. Now detects when
  no meaningful key/value entries remain and removes the file
  entirely, leaving `%USERPROFILE%\` exactly as a fresh Windows
  install would have it.
- **Backend: 13 new govulncheck findings cleared.** A 2026-05-22
  vuln-db refresh surfaced 13 CVEs across `golang.org/x/crypto/ssh`,
  `/ssh/agent`, and `/ssh/knownhosts`, all fixed in v0.52.0.
  `backend/go.mod` direct bumped `x/crypto v0.50.0 -> v0.52.0`;
  transitive bumps of `x/net v0.53.0 -> v0.54.0`, `x/sys v0.43.0 ->
  v0.45.0`, `x/text v0.36.0 -> v0.37.0` came along automatically
  and introduced no new findings. Practical exposure was zero
  (rangerdanger does not terminate SSH in the backend Go process);
  bumped to keep govulncheck green. See
  `docs/security-known-issues.md` for the per-GO-ID rollup.

### Maintenance

- Frontend dependency bumps shipped via dependabot:
  - `linuxserver/webtop` image digest (PR #51)
  - `@tanstack/react-query` 5.90.12 -> 5.100.11 (PR #52)
  - `vitest` 4.1.6 -> 4.1.7 (PR #53)
  - `typescript` 5.3.3 -> 5.9.3 (PR #54)
  - `marked` 18.0.0 -> 18.0.4 (PR #55)
  - `tailwindcss` 3.4.1 -> 3.4.19 (PR #56)

## [v0.1.16] - 2026-05-14

Two infrastructure fixes uncovered by the v0.1.15 smoke pass. No lab
content, schema, or API changes — drop-in upgrade from v0.1.15.

### Fixed

- **Containd UI font assets now load through the `/containd/` proxy
  path.** Next.js inlines absolute `url(/_next/static/media/…woff2)`
  refs inside its generated CSS, and the existing nginx `sub_filter`
  block didn't include `text/css` in `sub_filter_types`, so those
  url() refs were never rewritten to `/containd/_next/…`. The browser
  fetched them at the rangerdanger origin and 404'd — surfacing as a
  font-asset 404 storm + a React #418 hydration error in the iframe.
  `text/css` is now in `sub_filter_types`, with `url(/_next/` → `url(/containd/_next/`
  rewrites (plus the quoted variants other CSS-in-JS builds emit) so
  the typography on the containd UI renders correctly under any
  firewall policy. The hardened policy itself was never the problem
  here — the `/containd/` proxy path always worked via the
  management-network bypass (`eth3 → eth3` INPUT accept on tcp/8080).
- **Smoke suite no longer flakes on the cold-start weak → improved
  transition.** Both `firewall-smoke.sh` and `lab-commands-smoke.sh`
  trusted a fixed `SETTLE_SECS=3` between `POST /api/firewall/apply`
  and the first probe. On a cold stack the API can flip
  `active_config` before nft rules + NFQUEUE consumers reconcile —
  intermittently producing 13 false "allow" rows on the first
  improved-policy run (then 52/52 PASS on every subsequent run).
  Replaced the fixed sleep with a `kali → rtac:502` canary that
  polls every 500ms (1s timeout per probe, 15s budget) until the
  verdict matches the requested policy. Verified by deliberately
  restarting the firewall container immediately before the smoke
  run — 52/52 PASS where the old version would have shown 39/52.

## [v0.1.15] - 2026-05-14

Lab text accuracy pass plus the deferred Codex P2 fix on the v0.1.13
event-schema migration. No backend changes; safe drop-in for anyone
already on v0.1.14.

### Changed

- **Re-framed Labs 1.2 and 1.3 around the Purdue Model.** The canvas
  toggle is now "Purdue Model View"; the band layout is Purdue (L4 /
  L3.5 IDMZ / L3 / L1) and the IEC 62443 contribution is the SL
  overlay rendered on top of those bands. Lab 1.2 links to a new
  Knowledge article "Purdue Model Levels (L0–L5)" that explicitly
  distinguishes the architectural levels from IEC 62443 Security
  Levels.
- **Lab 1.3 "Target SL-3" hint** now reads SL as the IEC 62443
  capability rating overlaid on the Purdue band layout instead of
  conflating the two frameworks.
- **Lab 1.3 zone-pair reference** explains why HTTP/8080 is allowed
  RTAC → field (lab-only `/api/state` health endpoint on each field
  device sim; would not exist on a production relay).
- **Lab 2.3 Attack 3 (Modbus FC5)** gains a callout disambiguating
  Modbus FC5 (Write Single Coil, TCP/502) from DNP3 FC05 (Direct
  Operate, TCP/20000). The hardened policy in Lab 2.3 needs DPI
  rules for both.
- **Lab 2.3 baseline read step** explains the coils-vs-holding-
  registers asymmetry: write coils on the device (FC5), read
  aggregated holding registers on the RTAC (FC3). Auto-reclose is
  the documented exception (DNP3 CROB to a recloser binary output).
- **Lab 2.4 negative-test hint** rewritten — the deferred "once
  containd's L4 event-emission lands" block is replaced with current
  Live DPI Events guidance now that v0.1.25 shipped.
- **mbpoll form standardized.** Stripped redundant `-m tcp` from 12
  mbpoll invocations across `baseline-assessment.yml`,
  `firewall-implementation.yml`, and `validation-evidence.yml` so
  every lab matches the form already used elsewhere and in the
  Knowledge article. Added a one-line note in the mbpoll Knowledge
  entry that TCP is the default whenever the target is an IP.

### Fixed

- **IDS rows in the Live DPI Events strip now render amber, not
  red** (Codex P2 review on #47). `eventVerdict` no longer maps
  `kind: "anomaly"` to `DENY` before the `category === "ids"`
  rowTone branch can match, so anomaly rows keep their distinct
  amber tone and pure firewall denies stay red.
- **Dead source IP removed** from Lab 1.2's RTAC tshark filter
  (`ip.src==10.40.40.10`). Host routing always pushes the RTAC's
  field-bound traffic out the OT Ops interface, so the wire-visible
  source at the firewall is always `10.30.30.20`. The filter is
  now annotated explaining this.
- **Lab 1.2 packet-count claim tightened.** "8–10 packets per FC3"
  was per-transaction connection setup/teardown; persistent
  sessions are 2 packets per transaction. The 504-packets-in-30s
  example now maps to the RTAC's ~8/sec aggregate poll rate across
  the four field devices.

## [v0.1.14] - 2026-05-13

Closes [#34](https://github.com/tonylturner/rangerdanger/issues/34).
Lab 2.4 step 5 ("Assemble the evidence package") now requires the
per-rule deny event export that was previously deferred as a
`:::hint` block pending containd's L4 event-emission.

### Changed

- **Lab 2.4 evidence package gains a 5th artifact: per-rule deny
  event export from `/api/substation/network-events`.** Students
  capture `action: DENY` rows from the firewall's event stream
  (with `ruleId`, `srcIp`, `dstIp`, `dstPort`, timestamp) as
  kernel-side proof that the rules they wrote actually fired
  against the probes they ran. The lab change-board narrative
  becomes "policy intent (student-policy.json) + policy reality
  (event rows) side by side", instead of inferring enforcement
  from the absence of operational alarms. Same data drives the
  Live DPI Events strip in the `/console` Segmentation panel.

- **The deferred `:::hint Per-rule deny logs / DPI event evidence`
  block is removed.** Its precondition (containd L4 event-emission,
  containd #19) shipped in v0.1.25 and is now reinforced by the
  v0.1.26+ ICS DPI enforcement work.

## [v0.1.13] - 2026-05-13

Pairs with [containd v0.1.26](https://github.com/tonylturner/containd/releases/tag/v0.1.26).
End-to-end ICS DPI enforcement now works on macOS Docker Desktop (and
Linux / Windows WSL2): students can apply the hardened policy and see
function-code-level Modbus violations both blocked and surfaced in the
Live DPI Events strip.

### Added

- **ICS DPI enforcement in `substation-improved.json`.** Added
  `dataplane.nfqueueGroup: 101` and `dpiEnabled: true` alongside the
  existing `dpiMode: enforce` + `nflogGroup: 100`. With the matching
  containd v0.1.26 image, the `rtac-to-field-modbus` rule now actually
  enforces its function-code allowlist `[1..6]`: an attacker (or
  misbehaving RTAC) attempting Modbus FC7+ from `lan1` → `lan2`
  triggers a `BlockFlowTemp` verdict that drops subsequent packets
  via the `block_flows` nft set.

- **`privileged: true` on the firewall service** in
  `docker-compose.yml`. `CAP_NET_ADMIN` + `CAP_NET_RAW` +
  `seccomp=unconfined` are not sufficient on macOS Docker Desktop's
  LinuxKit kernel for the userspace nflog/nfqueue consumer to bind
  to a netfilter group — bind returns EPERM via netlink recv.
  Privileged is the smallest hammer that unblocks the right kernel
  surfaces. On Linux hosts it's a no-op widening (the container
  already has the caps it needs); the lab posture as a whole is
  loopback-only, so the additional capability surface stays inside
  the container.

- **`scripts/events-smoke.sh` wired into `.github/workflows/smoke.yml`**.
  Three gates: (1) L4 `firewall.rule.hit` DENY events surface in the
  engine event store after a kali probe, plus the backend's
  `/api/substation/network-events` returns events at all (catches
  schema drift between containd's emitted Event JSON and the
  backend's Event struct). (2) ICS template apply accepts the
  canonical hyphenated name from `GET /api/v1/templates`. (3) ICS
  DPI function-code allowlist actually enforces — FC8 from RTAC
  produces a `block_flows` nft set entry within seconds. Gate 3
  skips gracefully if the host kernel can't bind NFQUEUE (so the
  smoke stays green on older runners).

### Fixed

- **Backend `Event` struct now reads v0.1.25+ camelCase JSON keys**
  (`srcIp`/`dstIp`/`srcPort`/`dstPort`/`kind`/`attributes`) alongside
  the legacy snake_case fallbacks, normalized via `Event.Normalize()`
  called by `GetEvents`. Without this, `isSubstationRelevant("","")`
  returned false on every event and `/api/substation/network-events`
  silently filtered everything out. Backward-compatible with older
  containd builds (legacy `source`/`dest`/`src_port`/`dst_port`/
  `type` still parse).

- **Backend named `POST /api/firewall/apply` survives bind-mount
  read flakes.** New `readPolicyJSONWithRetry`: 3 reads × 100ms
  apart rides out macOS Docker Desktop's mid-write window when host
  edits to a bind-mounted lab-definition propagate non-atomically
  to the container. If JSON is still invalid after retries, the
  error now includes byte count + head/tail snippet so an actual
  syntax bug is distinguishable from a transient mount race.

- **`X-Containd-Warnings` propagates to apply response + scenario
  step results.** `ImportConfig` signature changed from
  `error` → `([]string, error)`; warnings array is included in
  the `POST /api/firewall/apply` and `/api/firewall/apply-custom`
  responses when non-empty, appended to the step `Detail` for
  scenario execution, and logged by the seed loop + orchestrator.
  Without this, partial commits (e.g. nft apply failed but commit
  returned 200) silently succeeded with a green UI hiding broken
  enforcement.

- **Policy file `functionCodes` → `functionCode`** (singular) on
  `rtac-to-field-modbus` in `substation-improved.json`. containd's
  ICSPredicate schema uses `functionCode`; the plural form was
  silently dropped on unmarshal, leaving an empty allowlist that
  matched ANY function code — making the rule semantically
  "allow all Modbus" instead of "allow only FC1..6".

### Configuration (already in v0.1.12, kept here for trail)

- `substation-improved.json` and `substation-weak.json` already
  ship `dpiMode` + `nflogGroup` from the v0.1.12 release. v0.1.13
  layers the new `nfqueueGroup` + `dpiEnabled` on top of the
  improved policy only — weak stays in learn mode by design.

## [v0.1.12] - 2026-05-12

### Configuration

- **`substation-improved.json` and `substation-weak.json` set
  `dpiMode` + `nflogGroup` in their dataplane blocks.** With these
  fields, the containd v0.1.25 image that includes
  [containd#19](https://github.com/tonylturner/containd/issues/19)
  emits `firewall.rule.hit` events to `/api/v1/events` for every
  L4 rule with `log:true` (every existing substation deny rule).
  The LiveEvents strip in `/console` Segmentation drawer and the
  Command Audit "Show DPI" button at `/substation` render live
  deny events automatically. The improved policy sets
  `dpiMode: "enforce"` (DPI rules actually block); the weak policy
  leaves dpiMode unset so it stays learn-mode (visible events but
  no blocking — matches "weak baseline" semantics).

## [v0.1.11] - 2026-05-11

Lab experience release: every shipped lab now uses platform surfaces
(topology console, drawers, HMI, Segmentation panel) for observation
and verification rather than relying on terminal output alone. Plus
one platform bug fix and one new feature in the Segmentation View
that unlocks a future containd capability without needing a re-edit
when that lands.

### Frontend / UI

- **`SegmentationView` — Live DPI Events strip.** New collapsible
  section between the policy controls and the per-zone-pair
  Evaluation table. Polls `/api/substation/network-events` every
  2.5s; renders each event as a row with ALLOW/DENY chip (derived
  from `type` + `severity` + `details`), `src → dst :port`, protocol
  badge, and details line. Deny rows tinted red. Header shows
  `N total · M deny` counters. Empty state: *"No recent events — run
  a probe from a terminal to see enforcement."* Unavailable state:
  *"containd unreachable."* Forward-compatible with the future
  containd event emission fix tracked in rangerdanger#34 — the
  component will start rendering live denies automatically once
  containd ships the producer-side hook (see *Known gaps* below).

- **`SegmentationView` — removed broken Test button.** The Test
  button hardcoded `executeScenarioStep("dnp3-command-injection", 7)`
  but the `dnp3-command-injection` scenario was deleted in the
  workshop restructure (commit 870665c); the backend returned
  `404 scenario not found` for every click since. Removed
  `handleTestConfig`, the `TEST_SCENARIO` / `TEST_STEP_INDEX` /
  `RESTORE_STEP_INDEX` constants, the testing state, and the
  result panel. Net `-94` lines. The Evaluation table (per-zone-
  pair ALLOW/DENY chips) is unchanged — that's the policy-
  description surface. Live enforcement evidence now flows through
  the new Live DPI Events strip + the existing Command Audit tab.

- **Scenario runner — `Apply Hardened` button now appears on
  `Apply the hardened policy` steps.** The `isSegmentationStep`
  title-matcher in `scenario-runner.tsx` only checked for
  `improve` / `segmentation`, which silently hid the Apply
  Hardened button on Lab 2.3 step 6 and Lab 2.3-bonus step 5 even
  though both steps' `action: { type: firewall, config: improved }`
  expected the student to apply hardening there. Extended the
  matcher to also accept `hardened` / `harden`. Pre-existing bug
  surfaced during v0.1.11 walk-through; not caused by the lab
  rewrites in this release but discovered via them.

### Tooling

- **`scripts/validation-report.sh` reviewer checklist** reworked.
  Removed the misleading row that asked reviewers to cross-
  reference per-rule deny events via `show audit` — that command
  shows admin/config audit, not packet-level rule hits, and the
  L4 deny event stream isn't wired yet (containd#19). Replaced
  with a `config.commit` cross-reference (`show audit` is the
  right command for *that*, and it works) plus an HMI Feeder
  One-Line check, and a forward-pointer for the missing
  per-rule event row to add once containd#19 lands.

### Lab content

Seven labs touched. The recurring pattern: each existing technical
step (mbpoll / nmap / dnp3cmd) now ends with a "look at the platform"
prompt — Network Map drawer, Segmentation View, or substation HMI
Feeder One-Line — plus a small `:::decision` block capturing the
student's observation. The goal is to teach segmentation as an
operational discipline (alarms, customer service, audit trails) not
just a packet-filter outcome.

- **Lab 1.2 (`baseline-assessment.yml`).** Step 1 rewritten from a
  static zone table into a `/console` investigation: walk all four
  zones, toggle the IEC 62443 view, inspect three named inter-zone
  edges via MapTooltip, capture observations in three new decisions.
  Step 2 adds a Traffic Matrix drawer "sneak preview" of the same
  flows step 4 PCAP-and-tsharks formally. Background link to the
  *IEC 62443 Zones and Conduits* article on `/knowledge`.

- **Lab 1.3 (`segmentation-requirements.yml`).** Step 1 now opens
  with a Segmentation View prompt at `/console`: walk the
  Evaluation table on the weak baseline, then use the dropdown to
  preview Hardened Segmentation without applying. Anchors design
  verdicts in observable current policy state. Optional P3 hint
  for the firewall-node IEC 62443 Security Level badge (Target SL-3 ·
  MET/GAP), cross-referencing the *Security Levels Explained*
  knowledge article.

- **Lab 1.4 (`remediation-planning.yml`).** Step 4 reflection now
  surfaces `:::plan-coverage` of the student's Lab 1.4 selections
  against the Lab 1.3 design verdicts — the same panel three
  downstream labs already render, but now also rendered inside the
  planning lab itself. Reflection questions reworded to anchor in
  the panel's GAP / PARTIAL rows. Description block-scalar switched
  from `>` to `|` so fence syntax works.

- **Lab 2.2 (`firewall-implementation.yml`).** Phase 4 and Phase 6
  reworked off broken `Events / Activity page` references. Phase 4
  now leans on `containd cli` + `show audit` (verified — returns
  real admin/commit log), `/substation` Command Audit, and a
  forward-pointer to the Live DPI Events strip. Phase 6 reframed
  around *TCP timeout + HMI unchanged* as the working evidence
  pattern, with the same forward-pointer.

- **Lab 2.3 (`hardening-configurations.yml`).** Every attack step
  (Modbus FC6 tap override, DNP3 CROB injection, Modbus FC5
  breaker trip) and every post-hardening re-test now ends with an
  *Operational consequence at the HMI* section calling out the
  specific Feeder One-Line state expected — alarm banner text,
  customer-service tile content, *"Hospital and fire station
  without power"* sub-text. Eight new `:::decision` blocks capture
  what students observed and which hardening layer (L4 source pin
  vs DPI) stopped each re-test. Background links to *Modbus TCP
  in Substations* and *DNP3 in Substations* knowledge articles.
  P3 hint for the Electrical Detail tab (per-unit voltages,
  feeder loading %, source/load/losses kW).

- **Lab 2.3-bonus (`vendor-rdp-compromise.yml`).** Same HMI-prompt
  pattern as Lab 2.3 — kinetic outcome of the vendor-pivoted
  recloser attack visualized at `/substation` Feeder One-Line.
  Adds a decision capturing the recloser's last-command-source
  attribution (`10.20.20.10` vendor-jump IP, not Kali IP) — the
  central insight of the laundered-through-vendor kill chain.
  Background link to *OT Network Segmentation Overview*.

- **Lab 2.4 (`validation-evidence.yml`).** Step 2 (positive tests)
  and step 3 (negative tests) now end with HMI Feeder One-Line
  confirmation: *"no alarms + customers served"* after positives,
  *"unchanged from baseline"* after negatives — the absence of
  change is the evidence. Step 5 evidence-package list rewritten:
  removed hallucinated *"containd rule hit counts from the
  Segmentation tab"* (no such feature exists) and *"Per-rule logs
  for the deny actions"* (depends on rangerdanger#34). Real
  replacements: PCAP, policy export via `containd cli` →
  `export config` (path documented in Lab 2.2 step 10),
  `show audit` snapshot, HMI screenshot of the negative-test
  endstate. Forward-looking hint describes adding the DPI event
  stream as a fifth artifact once containd#19 lands.

### Known gaps

- **DPI deny events not yet emitted by containd** for L4-only rules
  (the most common rule shape in segmentation policies). Tracked
  upstream as [containd#19](https://github.com/tonylturner/containd/issues/19)
  and downstream as
  [rangerdanger#34](https://github.com/tonylturner/rangerdanger/issues/34).
  Root cause: `EvaluateVerdict` (the only writer to containd's
  event store) is reached only via `enforceDPIEvents` in DPI
  enforce mode; plain L4 deny actions go directly through the
  enforcement primitive (nftables) without notifying the engine.
  The shipped `LiveEvents` UI strip and the existing Command Audit
  *Show DPI (N)* button are both consumers of this stream and
  will start rendering live denies automatically once the
  producer is wired. No rangerdanger code change required when
  that lands.

## [v0.1.10] - 2026-05-11

Positioning / documentation pass plus a small post-install ergonomics
fix. No runtime behavior change in the lab itself.

### Documentation

- **README rewrite.** Lead now positions RangerDanger as an
  interactive OT segmentation training environment rather than "an
  OT/ICS cyber range", emphasizing the topology console + embedded
  terminals + validation-driven exercises that the UI already
  surfaces. New "What you get" section itemizes the real product
  surface (interactive topology map, embedded container terminals,
  exercise runner, substation process view, containd integration,
  knowledge wiki at `/knowledge`, simulators, PCAP evidence
  workflow, validation-driven exercises). New "Student journey"
  table walks the 7-lab arc step by step. New "Designed for"
  section calls out the audience: open and auditable, source-
  controlled lab content, offline workshop-ready, segmentation-
  focused, physics-backed, native multi-arch, classroom delivery.
  Tightened "Why RangerDanger is different" table with new rows for
  validation, knowledge wiki, evidence package, and distribution.
  Screenshots reordered to student-journey order; HTML-comment
  TODO block flags four screenshots still to capture (dashboard,
  embedded terminal, containd policy view, validation chips).

- **`docs/README.md`** (new). Routed landing page for `docs/`
  organized by audience: Start here, Student experience,
  Instructor / workshop operator, Architecture and internals, Lab
  content, Security model, Extending RangerDanger. Links to
  existing docs; no content is duplicated. README's documentation
  table now points at this landing page as the entry point.

- **`docs/tasks.md` untracked.** Operator-side working backlog is
  no longer part of the public repo. `ROADMAP.md` remains the
  forward-looking doc and `CHANGELOG.md` remains the public
  history. The file stays on disk locally; `.gitignore` updated to
  prevent re-tracking.

### Setup ergonomics

- **`setup.sh` and `setup.ps1` write `VERSION=...` to `.env`** at
  install time. Without this, a student who ran
  `./setup.sh --from-tarballs <SSD>` and later invoked
  `docker compose -f docker-compose.release.yml -f docker-compose.offline.yml up -d`
  directly would hit
  `No such image: ghcr.io/tonylturner/rangerdanger-opendss-sim:latest`
  because compose interpolates `${VERSION:-latest}` and the SSD
  tarball is tagged `:vX.Y.Z`. Writing the resolved VERSION to
  `.env` (compose auto-loads `.env` from cwd) makes bare compose
  invocations work the same as setup did. Idempotent: replaces an
  existing `VERSION=` line if present, appends otherwise. `.env`
  is gitignored.

### CI

- **`release.yml` auto-creates the GitHub Release** on tag push.
  Previously the GHA workflow only built and pushed GHCR images;
  the GitHub Releases tab had to be populated by hand and drifted
  out of sync (v0.1.3 through v0.1.9 were backfilled
  retroactively). The new `github-release` job awk-extracts the
  matching CHANGELOG section between `## [vX.Y.Z] - YYYY-MM-DD`
  and the next heading, creates the release (or updates the notes
  if one already exists), and flags `--prerelease` for any tag
  containing a hyphen. v0.1.10 is the first release this job runs
  for live.

## [v0.1.9] - 2026-05-09

SSD-staging fixes uncovered during the first real workshop SSD stage
on Apple Silicon. Three coupled fixes plus self-describing version
metadata so students don't need to know the release tag.

### Workshop / ops

- **`stage-ssd.sh` and `stage-ssd-delta.sh` digest-pinned platform
  resolution.** `docker pull --platform=linux/amd64` followed by
  `docker save` against multi-arch tags fails on arm64 hosts (Apple
  Silicon) with `Error response from daemon: unable to create
  manifests file: NotFound: content digest ... not found`. Docker
  Desktop's content store keeps the manifest LIST locally (which
  references both platforms' sub-manifests) but only the requested
  platform's layers; `docker save` then walks the list and errors
  on the missing cross-platform sub-manifest. Fix: resolve each
  image to its platform-specific manifest digest via
  `docker buildx imagetools inspect`, pull by digest (which stores
  ONLY the single-arch manifest), then re-tag to the user-friendly
  reference before save. The saved tar carries clean
  `repo:tag`-keyed images that load and run correctly on any
  arch-matching host.
- **OpenPLC cross-arch inclusion.** Upstream `tuttas/openplc_v3` is
  amd64-only, and `docker-compose.release.yml` pins
  `platform: linux/amd64` on the openplc service so Apple Silicon
  hosts run it under Rosetta 2. Both stage scripts now
  cross-include the amd64 openplc image in the arm64 bundle so
  Apple Silicon students get a self-sufficient SSD without having
  to also load `images-amd64.tar`.
- **Self-describing SSD version.** `stage-ssd.sh` now writes a
  `.version` file alongside the tarballs containing the staged
  release tag. `setup.sh --from-tarballs` and `setup.ps1
  -FromTarballs` auto-read it and override the default
  `VERSION=latest`, so students don't have to pass `--version
  vX.Y.Z` to match the SSD. Fixes a workshop-blocker where
  `setup.sh --from-tarballs /Volumes/WORKSHOP` defaulted to
  `VERSION=latest`, compose substituted `${VERSION:-latest}`,
  and lookup failed with "No such image:
  ghcr.io/tonylturner/rangerdanger-opendss-sim:latest" because
  the loaded tarballs were tagged `:v0.1.7`.
- **Clearer load-step progress hint.** `setup.sh` /
  `setup.ps1`'s "Loading images from tarballs" banner now tells
  the student `~5-15 min on a fast SSD` and to watch for the
  `Loaded image:` lines (one per image, 14-19 total). Replaces
  the prior "this can take a few minutes" which left the student
  uncertain whether the script had hung.

## [v0.1.8] - 2026-05-09

Workshop-readiness release: SSD distribution operator runbook, delta-
patch tooling, and host-resource spec corrected against measured
reality. No behavior change in the running stack.

### Workshop / ops

- **`docs/workshop-ssd.md`** (new) - operator runbook for shipping
  RangerDanger to a roomful of laptops over a constrained-Wi-Fi
  workshop. Covers initial SSD stage, student first-run install,
  the three change patterns that arise mid-workshop (repo-only,
  image rebuild, containd update), the delta workflow, recovery
  scenarios (corrupt SSD, mid-load failure, drifted credentials,
  stale lab YAML), and an FAQ. Linked from README docs table.

- **`stage-ssd-delta.sh`** (new) - patch-tarball builder for
  pushing a fix mid-workshop without re-shipping the full ~6 GB
  bundle. Compares remote manifest digests of every first-party
  image at `<since-version>` vs `<new-version>`, saves only the
  ones that differ per architecture, always includes a fresh
  `rangerdanger.tgz`, and writes a per-stage `DELTA-README.md`
  with the exact apply commands the student should run.
  Flags: `--include image1,image2` to force-include images
  whose digest didn't change, `--all` to skip the digest
  comparison entirely, `--include-upstream` to delta-check
  containd/nginx/fuxa/webtop/alpine in addition to the
  rangerdanger-* set.

- **`docs/_internal/pdf-update-prompt.md`** (new) - canonical
  prompt for regenerating the workshop-handout PDF in
  Claude.ai web when the install flow drifts. Single source of
  truth for what the PDF should say, checked against
  `docs/quickstart.md` and `setup.sh` at PDF-regen time.

### Documentation - resource spec corrected

- **`docs/quickstart.md` + `README.md` Prerequisites table.** The
  prior "8 GB host RAM minimum" was wrong - it confused the
  Docker VM allocation (8 GB) with required host RAM. Live
  measurement against the running stack: idle ~4 GB across all
  containers, peak ~6-8 GB during workshop use. Host needs to
  cover that *plus* macOS/Windows itself plus the student's
  browser - 8 GB host swaps too aggressively. Corrected to
  "16 GB host RAM minimum, 32 GB recommended (with 8 GB
  allocated to the Docker VM)". Setup-script enforcement
  (`setup.sh` warns at < 7 GB Docker-VM, < 30 GB disk) was
  already correct and unchanged.

## [v0.1.7] - 2026-05-08

Polish release: proxy 404 fix for in-app containd navigation, README
visual refresh with screenshots, new Docker-architecture diagram, and
a global em-dash sweep across all prose. No code-behavior changes.

### Frontend / proxy

- **Containd UI in-app navigation no longer 404s.** The proxied
  containd UI emitted client-side links to root paths
  (`/templates`, `/wizard`, `/nat`, `/pcap`, `/sessions`, `/forbidden`,
  `/docs`) that nginx had no rewrite for. Added the missing 7
  `sub_filter` entries so JS-bundle root paths get rewritten to
  `/containd/<route>`. Also added a defensive regex `location` block
  that 302s direct root-level URL hits to `/containd/<route>` so
  bookmarks and external links to the bare path resolve too. List
  now mirrors `containd/ui/app/*` exactly.

### Documentation

- **`docs/architecture-diagram.md`** rewritten as **Docker
  architecture** - compose stack composition (services / images /
  build vs pull / `depends_on` chains), network wiring (which
  service attaches to which of the 6 Docker networks, including
  RTAC's three-net + firewall's five-net multi-homing), and
  request flow (host-bound loopback ports, nginx route map, backend
  fan-out via Docker SDK / containd JWT REST / WebSocket xterm,
  critical bind mounts). Three mermaid blocks pre-rendered to
  dark-theme transparent SVGs at `docs/images/docker-*.svg` so
  the page renders in any markdown viewer; mermaid source is kept
  in collapsed `<details>` for diff-friendliness.
- **README.md visual refresh.** Removed the bottom mascot block;
  tightened the lockup logo + tagline gap; replaced the broken
  ASCII architecture diagram with an inline mermaid block (lab/zone
  view, GitHub renders natively); added a "What it looks like"
  4-row screenshot table with forced 320 px thumbnails alongside
  inline captions (Network Map, Feeder HMI, Exercises, Lab runner).
  Screenshots committed at `docs/images/screenshot-*.png`.

### Polish

- **Em-dash sweep.** All 37 prose files (markdown, YAML, compose,
  workflow) had their em-dashes (`-`) normalized to hyphens (`-`).
  Verified the sweep didn't accidentally create any new `--`
  command-flag sequences (every replacement was in prose context
  with surrounding whitespace; existing `mbpoll --`, `nmap
  --max-retries`, `curl --connect-timeout` stayed untouched).
  `lab-commands-smoke` 75/75 still passes.

## [v0.1.6] - 2026-05-08

Lab realism + containd auth-flow patch. Two changes that make the
existing kinetic-chain plumbing visible to students, plus a
defense-in-depth fix for the containd lab-mode credential drift
that surfaced after v0.1.5 shipped. Requires `containd >= v0.1.22`
(the upstream lab-mode credential pin); containd v0.1.23 docs the
trade-off transparently in its Secure-by-Design pledge.

### Workshop / lab content

- **Lab 2.3 third attack: Direct breaker trip via Modbus FC5.**
  Single-packet kinetic outcome - Kali sends `mbpoll -t 0 -r 1
  10.40.40.20 0` to the relay's coil 0, the breaker opens, and
  OpenDSS de-energizes the entire feeder within ~3s
  (downstream_voltage_v: 119 → 0, all loads dark, feeder current
  → 0). The most dramatic cyber → physics chain the lab can
  demonstrate; previously the relay's FC5 path was wired through
  to OpenDSS but no lab exercised it. Re-test step under the
  hardened policy confirms the L4 source-pin closes the
  enterprise → field write at the perimeter.
- **Lab 2.3 attacks now tied to observable kinetic outcomes via
  Modbus.** Each attack step now ends with two `mbpoll` reads
  against the RTAC (FC3 holding registers for device states, FC4
  input registers for OpenDSS analog measurements). Students see
  the actual register values change - `critical_load_voltage_v ×
  10` drops from ~1240 to ~1075 after Attack 1's regulator tap
  override; `downstream_voltage_v × 10` and `feeder_current_a ×
  10` drop to 0 after Attack 2's recloser trip. No `curl` reads
  in attack labs - the protocols students attack are the
  protocols they verify with.
- **Lab 2.3 step 1 (Verify normal ops) replaces curl with mbpoll
  reads** of the same RTAC tags. Establishes the baseline using
  the same protocol surface the attacks exploit.
- **Cyber → state → physics propagation-lag `:::hint`** added to
  Lab 2.3 step 1 and Lab 2.3-bonus attack step. Documents the
  ~2-3s window between a Modbus/DNP3 write and the OpenDSS
  electrical-state update (RTAC polls field devices every 2
  seconds, then pushes aggregated state to the solver). Without
  this, students who re-read state immediately after the attack
  see device flags changed but not the kinetic outcome and
  conclude the attack failed.
- **Lab 2.3 Attack 1 mbpoll command bug fixes (audit follow-up).**
  Was `mbpoll -t 1 -r 1 ... -- -16` - `-t 1` is mbpoll's discrete-
  input data type (Modbus FC2, read-only), so the command failed
  with `Unable to write read-only element`. Lab "worked" only
  because the action stanza drove regulator state via the API
  path; the student's typed CLI was a no-op. Now `-t 4` (holding
  register, Modbus FC6) and value `65520` (two's-complement of
  -16 as the unsigned register accepts). Verification command was
  `-t 4 -r 3 ...` reading holding register 2 (`reclose_enabled`,
  not the claimed `critical_load_voltage`); now `-t 3 -r 3` for
  input register read (FC4).
- **Lab 2.3-bonus observe-impact step** mirrors the 2.3 changes -
  after the Modbus FC5 attack on the recloser from the vendor
  jump, students read RTAC tags via Modbus to see downstream
  de-energization. Same propagation-lag hint included.

### Backend / proxy / docs

- **Containd auth lab-mode workaround (defense in depth).**
  containd v0.1.22 added an upstream lab-mode lock that pins the
  canonical `containd/containd` credential and disables password
  change. RangerDanger now belt-and-suspenders the same surface
  in three places so a stack running an older containd image
  still gets the workshop-correct behavior:
  - **(A) `proxy/nginx.conf` blocks the password-change endpoints
    at the proxy.** A regex `location` for
    `/containd/api/v[0-9]+/(auth/me|users/[^/]+)/password$`
    returns 403 with a clear lab-mode message before the request
    reaches containd. Students get a clean explanation instead of
    the previous CORS / "password field" error.
  - **(B) `/api/workshop/reset` wipes containd's `users.db`** as a
    defensive backstop. With containd >= v0.1.22 this is a no-op
    (password change is locked at the API), but provides a clean
    recovery path for any pre-v0.1.22 stack where someone hit the
    direct `:9080` UI and changed the password.
  - **(C) `proxy/nginx.conf` injects a banner** at the top of the
    proxied containd UI explaining that credentials are pinned to
    `containd/containd` and password change is disabled in lab
    mode. Sets explicit student-facing expectations.

### Tests

- Smoke gauntlet now exercises 75 lab commands (was 65 in v0.1.5)
  - the new mbpoll observation reads in 2.3 and 2.3-bonus pick up
  automatically via `lab-commands-smoke.sh`'s YAML scan.
- All four fixes verified live against a clean rebuild with
  containd v0.1.22 image: A returns 403 at the proxy, containd
  direct returns 403 with the upstream lab-mode message,
  `/auth/me` no longer advertises `mustChangePassword`, the
  banner renders in the proxied login page, and `/api/workshop/reset`
  successfully removes `/data/users.db`.
- firewall-smoke 52/52 + lab-commands-smoke 75/75 + backend
  `go test -race ./...` clean.

## [v0.1.5] - 2026-05-08

Audit-pass-3 closeout. Codex surfaced two reproducible workshop
blockers and twelve smaller findings on top of v0.1.4. Validated
against three back-to-back clean rebuilds (each producing a different
docker `ethN` ordering on the firewall - confirming the determinism
fix holds): all three iterations passed `firewall-smoke 52/52`,
`lab-commands-smoke 65/65`, every workshop endpoint 200, reset
`success:true` with zero failed actions, no containd boot errors.

### Workshop / lab content

- **`/api/workshop/reset` no longer reports `success:false`** (audit
  P0-A). The reset path was sending `clear_alarm` to capbank-sim,
  which has no such handler - `reset_lockout` already clears the
  alarm flag. Both `reset.go` and `test_runner.go` had a duplicated
  reset-command list with the same bug; consolidated into a single
  `resetDeviceCommands` var. New `reset_test.go` (`TestResetCommandsAreSupported`)
  scans every sim's `case "X":` handlers and asserts every reset
  command resolves - catches this bug class going forward.
- **`lab-definitions/scenarios/validation-evidence.yml` PCAP
  capture** (audit P0-B) - student tcpdump switched from
  `tcpdump -i eth3 ...` to `tcpdump -i any -nn 'net 10.40.40.0/24
  and (tcp port 502 or tcp port 20000)' ...`. The `eth3` pin assumed
  a 4-network firewall; once F-002 added the mgmt zone (`lan3`),
  Docker started shuffling field across `eth1`/`eth2`/`eth4`
  depending on the host. The `-i any` form plus a BPF subnet filter
  is fully drift-proof.

### Backend / tests

- **`backend/internal/server/pcap.go`** drops the broken
  `Interfaces: []string{"eth0","eth1","eth2","eth3"}` pin (audit
  P0-B). containd's PCAP path resolves entries as literal kernel
  interface names via netlink (unlike the policy autobind in
  commit 5f31128 which DOES accept zone names), so any `ethN`
  pin was non-deterministic and zone names fail at boot with
  "interface wan not found". The backend's `tcpdump -i any`
  fallback in the same file is fully drift-proof and is what's
  actually used at runtime; the broken containd-PCAP path is
  no-op'd cleanly. Tracked as an upstream containd issue
  in `docs/tasks.md`.

### Setup + ops

- **`docker-compose.offline.yml`** (new, audit P1-B). Override
  setting `pull_policy: never` on every release-image service so
  `docker compose up` after `docker load` from the SSD does not
  reach out to GHCR. `setup.sh --from-tarballs` and `setup.ps1
  -FromTarballs` now compose `-f release.yml -f offline.yml`
  automatically; offline classes work without the override flag.
- **`setup.sh` / `setup.ps1` workshop-readiness gate** (audit
  P1-D). After the existing `/api/health` probe, both installers
  now probe `/api/firewall/health`, apply weak + improved, and
  `/api/workshop/reset`, failing with actionable diagnostics if
  any returns non-2xx or non-success. New `--skip-firewall-gate`
  / `-SkipFirewallGate` flag for developer iteration on a
  known-broken stack. Catches containd drift / mgmt-subnet
  misconfig / sim-warmup races at setup time rather than
  at student-time.
- **`stage-ssd.sh` fail-fast** (audit P1-C). Was warning-and-
  skipping on pull failure then `docker save` against the
  unfiltered input list (could include stale local copies or
  fail mid-save). Now dies on any pull failure and `docker save`
  operates on an explicit pulled-this-run list - bundle is either
  complete or absent.
- **`docker-compose.release.yml`** restored
  `CONTAIND_AUTO_LAN3_SUBNET=10.99.99.0/24` (lockstep drift
  codex caught - release-image users would have hit 502 on
  apply when the mgmt subnet fell outside containd's input
  chain). `CONTAIND_CAPTURE_IFACES` removed from both compose
  files since the static `ethN,...` pin was non-deterministic
  and zone names break containd at boot (see pcap.go above).
- **`scripts/lab-commands-smoke.sh` apply-failure detection**
  (audit P1-A). Was `2>/dev/null` swallowing curl errors on
  policy apply, so the script could report 65/65 PASS with
  broken policy state - exactly the false-confidence mode the
  audit caught. Now checks curl exit code AND verifies
  `/api/firewall/active` reflects the requested config; fails
  the run with a clear diagnostic on either error. The probe
  rc 1-7-as-PASS rule (intentional for "host unreachable" /
  "connection refused" on negative tests) is unchanged.

### CI

- **Smoke runs on `audit-oss` and `oss-release`** (audit P2-B).
  Was `[main]` only; release-branch work could ship without the
  workshop-critical Docker smoke gate.

### Documentation

- **`docs/architecture.md` rewrite** of the RTAC + interface
  sections (audit P2-A). Removed the stale "RTAC field polling
  does not transit firewall" claim - `rtac-harden.sh` has
  forced firewall transit since v0.1.2. Corrected the multi-
  homed table (2 networks, not 3 - there is no physics_net
  leg). Corrected the source-pin IP (10.30.30.20, not
  10.40.40.10). Replaced the fragile `eth0`/`eth1`/`eth2`/`eth3`
  column with containd zone names (`wan`/`dmz`/`lan1`/`lan2`/`lan3`)
  and added an explicit note that Docker's `ethN` ordering is
  non-deterministic across hosts (alphabetical network name,
  not compose order) - never rely on it. New "Multi-homed RTAC
  with kernel-pinned routing" subsection explains the
  compensating control (FORWARD DROP + replaced field route +
  `rtac-route-monitor.sh`).
- **`RELEASING.md` containd image policy section** (audit P2-E).
  Documents the "fix containd, not the pin" contract,
  workshop-day determinism trade-off, and three mitigations
  (pre-pull + digest lock, setup-time firewall gate, stage-to-SSD).
- **`CONTRIBUTING.md`** frontend command now includes
  `npm test` (audit P2-C). Was missing while CI ran it;
  contributors could pass the documented local checks and
  still trip CI.
- **`docs/tasks.md`** moved `firewall_apply` integration tests
  + capbank handler coverage from "Still open" to shipped
  (landed in v0.1.4 - was stale carry-over). Added the
  audit-pass-3 P0/P1/P2 items + an "Open questions" section
  parking codex P0-001 (firewall apply 502/403 - not
  reproduced) and the upstream containd PCAP zone-name
  issue.

## [v0.1.4] - 2026-05-08

Post-v0.1.3 audit follow-through. Closes the remaining P1/P2
findings from the release-readiness audit + four pass-2
N-findings + three CI smoke-harness regressions introduced by
the audit fixes themselves.

### Workshop / lab content

- **Lab 2.3 audit (`hardening-configurations`).** Bound the
  "Verify normal operations" step to `rtac-1` so the curl
  example dispatches to a real container; added a
  `:::plan-coverage` panel before "Apply the hardened policy"
  with a coverage-gap note naming the actions
  (`pin-rtac-to-field`, `modbus-dpi`, `dnp3-dpi`) that close
  both attacks.
- **Lab 2.3-bonus audit (`vendor-rdp-compromise`).** Same
  `:::plan-coverage` treatment before the kill-chain re-test,
  naming the two requirements (`vendor-to-ot`,
  `non-rtac-to-field`) that close the deck case study.
  Updated all RDP/SSH command examples from the deleted
  `vendor-user/vendor` creds to the canonical
  `rangerdanger/rangerdanger` (audit N-001 - would have
  workshop-blocked the bonus lab).
- **Lab 1.4 stray "five baseline findings" prose** removed
  from step 3 question 2 (the post-1.2-refactor finding count
  isn't fixed at five).
- **OpenPLC single-homed on `ot_ops_net`** (audit F-011).
  Dropped the unused `field_net` leg - the ladder logic in
  `data/openplc/substation_automation.st` is a Modbus client
  of the RTAC and never originates field-device traffic, so
  multi-homing was a lab artifact that doubled the firewall-
  bypass surface for no functional reason. Lab 2.4 narrative
  cleanup (multi-homed-OpenPLC callout dropped, hint
  simplified to "any single-homed OT host works"). Compose
  files, backend catalog, topology YAML, smoke + validation-
  report matrices, and architecture.md all updated.
- **Vendor → OT mgmt positive test** added to Lab 2.4 step 2.
  Lands now that rtac-sim hosts sshd + nginx for that
  allowance (audit F-004).

### Frontend / tests

- **Decision-graph contract test** (audit F-007).
  `frontend/lib/scenario-decision-graph.test.ts` walks every
  scenario YAML and asserts every `:::findings-panel` and
  `default-from` reference resolves to a real
  `:::decision id=` definition. Catches silent drift if a
  decision id gets renamed in one YAML and forgotten in
  downstream ones - the silent failure mode the audit warned
  about. Closes the gap without an `@testing-library/react`
  dep tree.
- **`frontend/lib/decision-storage.ts`** extracted from
  scenario-runner.tsx so the storage-key shape
  (`decision:<scenario>:<id>`) and the
  `REMEDIATION_PLAN_STORAGE_KEY` are testable in isolation.
- **70 frontend tests** (Vitest, was 61 in v0.1.3).

### Backend / tests

- **Firewall compare logic tests** (audit F-010 - the
  scenario-validators portion was already covered by
  `scenario_validate_test.go`). New
  `firewall_compare_test.go` (5 tests) pins
  `loadFirewallRules`, `zonePairLabel`, and
  `compareRuleSets` against the real policy JSONs, including
  the headline ALLOW→DENY tighten on Enterprise → Field, an
  identity-comparison invariant, and stable output order.
- **Firewall-apply handler integration tests** (audit F-010
  closeout). New `firewall_apply_test.go` (10 tests)
  exercises `handleFirewallApply` and
  `handleFirewallApplyCustom` end-to-end against an httptest
  fake of containd's candidate/commit endpoints. Pattern
  mirrors the existing `containd/client_test.go` - no
  interface refactor required; the real client's JWT
  injection + dataplane-enforcement shim run as in
  production.
- **capbank-sim HTTP handler tests** (audit F-010 closeout).
  New `services/capbank-sim/main_test.go` (12 tests) covers
  /api/state, /api/command (happy paths +
  double-switch-rejected, lockout-after-6-ops, reset-clears,
  unknown-command rejected, malformed JSON), /api/audit, and
  /api/health.

### Setup + ops

- **`scripts/firewall-smoke.sh`** waits for the new rtac-sim
  `:22` and `:443` listeners and the post-F-011 `openplc` IPs
  (`10.30.30.30:502 / 8080`) before probing. Replaced the
  `openplc->fw mgmt` lan2 probe with `relay-sim` since
  openplc is no longer on lan2.
- **`scripts/lab-commands-smoke.sh`** skips any plain `ssh`
  (interactive password prompt). The new
  `ssh ... rangerdanger@10.30.30.20 'hostname'` student
  command in Lab 2.4 uses an interactive auth flow that
  smoke can't satisfy non-interactively; `sshpass`-prefixed
  forms still run.
- **`vendor-to-ot-weak`** firewall rule now includes
  `tcp/443` so the weak baseline is a true superset of
  improved on `dmz → lan1` (smoke matrix expectation
  alignment).

### Docs

- **README polish** (audit F-015 partial). Hero lockup
  image, centered badges, dropped duplicated zone table
  (lives in architecture.md), icon-led documentation table.
  Real screenshots/GIFs of the running stack still
  outstanding.
- **`docs/tool-inventory.md`** (audit F-016). Cross-
  reference for what CLI tool lives in which Dockerfile by
  lab persona, plus a decision tree for "I need to add a
  tool - which image?" Should head off the apt-list drift
  the next audit would otherwise flag again.
- **`CONTRIBUTING.md`** documents `scripts/dev-up.sh` and
  `dev-down.sh` (audit F-017).
- **`docs/quickstart.md`** version pin bumped to `v0.1.3`
  (audit N-004 - students copy-pasting from quickstart
  would have pulled v0.1.2 images that lacked every fix
  in v0.1.3).
- **`CHANGELOG.md`** reference-link block now includes
  `[v0.1.3]` (audit N-003).
- **`docs/lab-credentials.md`** documents rtac-sim
  SSH/HTTPS access + flags `scripts/rtac-mgmt-init.sh`
  as the second creation site for the `rangerdanger` user.

## [v0.1.3] - 2026-05-08

Audit-driven hardening pass on top of v0.1.2's deck-aligned
restructure. Fixes a vendor-jump dual-bootstrap conflict, closes a
silent backend-test cache miss, gives the RTAC real SSH/HTTPS
listeners so the "vendor → OT for monitoring" lesson is actually
demonstrable, fixes setup.sh on Linux native Docker, and folds in a
month of lab-content audit work that landed since v0.1.2.

### Workshop / lab content

- **Lab 1.2 audit pass.** Split into separate observe-vs-decide
  activities - passive-observation findings on step 6, active probe
  step (7) that surfaces latent exposure passive monitoring missed.
  Step 6 now uses dropdown widgets with green/red feedback chips.
  Static findings list deconflicted from the actual 1.2 capture so
  Lab 1.4 step 1 doesn't show stale data.
- **Lab 1.3 audit pass.** Re-titled to "Segmentation Requirements
  & Policy Design", trimmed to 2 hands-on steps (cut Steps 5–7 that
  spoiled the Lab 2.2 build), added BLOCK-and-LOG verdict option,
  styled the dropdown for dark mode, restored the answer-key hint
  block, and added an "active confirm requirements from your
  assessment" step. New resourcing-readiness step.
- **Lab 1.4 audit pass.** Per-action requirement-source badges
  driven by 1.3 design verdicts and readiness flags. Sticky-bottom
  coverage summary so feedback stays in view as the student picks.
  Dynamic-content connection to 1.3 wired without removing the
  action picker.
- **Lab 2.2 audit pass.** Replaced bogus `curl ... /api/v1/policies`
  hints with real containd Web UI walkthrough + appliance CLI
  (`show running-config`, `set firewall rule`, `commit`,
  `show audit`, `export config`). Added missing dnp3 tools to
  vendor-jump for the Phase 6 commands.
- **Lab 2.4 audit pass.** Dynamic findings list, OpenPLC tooling
  fixes, and the new `:::plan-coverage` fence - surfaces in real
  time which Lab 1.3 requirements the student's Lab 1.4 plan
  addressed vs deferred.
- **Vendor → OT management listeners (audit F-004).** rtac-sim now
  hosts sshd on :22 and nginx on :443 with the standard
  `rangerdanger:rangerdanger` credentials. The improved policy's
  `vendor-to-ot-restricted` rule (dmz → lan1 on 22 + 443) now has
  real listeners to probe against, so the "encrypted-management-only"
  lesson is demonstrable not just declared. Lab 2.4 step 2 has an
  explicit positive-test row for this. `firewall-smoke.sh` and
  `validation-report.sh` matrices include the corresponding rows.
- **`:::plan-coverage` fence type.** New lab-authoring fence that
  renders, for any scenario that has access to the student's
  remediationPlan, the action-by-action coverage of 1.3 verdicts.
  See `docs/lab-authoring.md` for the full vocabulary.
- **Workshop overview rewrite.** `docs/workshop-overview.md` now
  uses the OBSERVE / DECIDE-design / DECIDE-implementation activity-
  type framing for the 1.2 / 1.3 / 1.4 planning labs and refreshed
  the lab table descriptions to match current YAML.
- **Em-dash normalization.** Replaced 108 em-dashes with hyphens
  across all 7 labs (xterm.js shell terminals don't render em-dash
  consistently; rendered as `?` on certain locales).

### Setup + ops

- **`setup.sh` cross-platform fixes (audit F-005, F-006).**
  - Disk check now uses `df --output=avail -BG` on Linux (always
    pure-numeric) with `df -g` as the BSD/mac fallback. Both
    branches floor to int via awk so the integer comparison can't
    choke on `30.0G` formatting.
  - Memory check falls back to `/proc/meminfo` when
    `docker info -f '{{.MemTotal}}'` returns 0 (Linux native Docker
    Engine, no VM memory limit). Source labelled in the output so
    the student knows whether Docker Desktop's slider applies.
- **Docker compose dual-bootstrap fix (audit F-001).**
  `docker-compose.release.yml` no longer mounts
  `scripts/start-rdp-vnc.sh` - the in-image `vendor-jump-services.sh`
  is the single source of truth, with `rangerdanger:rangerdanger`
  as the only vendor user. Removed the orphan script.
- **`scripts/lab-commands-smoke.sh` counter fix (audit F-009).**
  Pre-flight backend-health row no longer bumps the `passed`
  counter; "ran 64, passed 65" is gone.

### CI / tests

- **Backend test cache fix (audit F-002).** Backend `go test` runs
  with `-count=1` in CI to defeat Go's test cache for the firewall
  config tests. They `os.ReadFile` from `lab-definitions/firewall/`
  at runtime and Go's cache hashes only the binary plus env/args,
  not external data - so a JSON-only edit silently kept the cached
  PASS. Test names now reflect the lan3 mgmt-net addition (5 zones,
  not 4) with an inline comment explaining the workaround.
- **`scripts/firewall-smoke.sh` listener wait + matrix expansion.**
  Probes wait for canonical listeners to come up before running, so
  cold-start runs no longer false-positive a "deny" verdict on the
  no-listener race. Matrix now includes vendor-jump → rtac:22 / 443
  / 502 rows.
- **`scripts/lab-commands-smoke.sh`.** Smoke runs every command
  block documented in the 7 lab YAMLs - from the right source
  container, under the right policy. Wired into CI on every PR.
- **`scripts/validation-report.sh`.** Generates a change-board-ready
  evidence-package markdown from a clean run.
- **31 parser unit tests** for the lab-description parser, extracted
  to `frontend/lib/scenario-description.ts` and exercised via
  `frontend/lib/scenario-description.test.ts`.

### Docs

- **`docs/lab-authoring.md`.** Authoring guide for the runner-
  specific YAML extensions (`:::hint`, `:::decision`,
  `:::findings-panel`, `:::plan-coverage`, default-from inheritance).
- **`docs/lab-credentials.md`.** Documented rtac-sim SSH/HTTPS
  creds and the second creation site for `rangerdanger`.
- **`docs/tasks.md` refresh.** Workshop blockers now reflect that
  1.2 / 1.3 / 1.4 / 2.2 / 2.4 audits have landed; 2.3 + 2.3-bonus
  remain. Release path bumped to v0.1.3.
- **`docs/workshop-overview.md`.** Replaced "produce the 5 baseline
  findings" framing (pre-refactor wording) with the activity-type
  framing the planning labs actually use.
- **UX polish.** Silenced webtop terminal noise; clearer CLI / bash
  switching docs.

### Security

- **Go toolchain bumped to 1.25.10**, `golang.org/x/net` to 0.53.0
  (advisory clearances).

## [v0.1.2] - 2026-05-07

Workshop-deck-aligned restructure plus a security + tooling
sweep. Lab inventory shrank from 9 sequentially-numbered exercises
to 6 labs + 1 bonus, numbered to match the DefendICS workshop deck
(Lab 1.2, 1.3, 1.4, 2.2, 2.3, 2.3-bonus, 2.4). Vendor RDP lab
rebuilt to actually use RDP/VNC. In-app firewall terminal lands
directly in the containd CLI. Major doc + setup polish.

### Workshop / lab content

- **Restructured lab inventory to align with the DefendICS workshop
  deck.** The 9-exercise inventory (sequentially numbered 1–9) is now
  6 labs + 1 bonus, numbered by the deck's lab IDs:

  - Lab 1.2 `baseline-assessment` - Baseline Traffic Analysis
  - Lab 1.3 `segmentation-requirements` - Segmentation Requirements
    & Policy Design
  - Lab 1.4 `remediation-planning` - Remediation Planning Under
    Constraint
  - Lab 2.2 `firewall-implementation` - Firewall Policy Implementation
  - Lab 2.3 `hardening-configurations` - Protocol-Hardened
    Configurations (NEW; combines the prior Modbus-override and
    DNP3-injection exercises into a single DPI-focused stress test)
  - Lab 2.3-bonus `vendor-rdp-compromise` - Vendor Remote Access
    Compromise (rebuilt from the prior Modbus-via-vendor narrative
    to actually use RDP/VNC pivot, matching the deck case study)
  - Lab 2.4 `validation-evidence` - Testing & Validation

  Removed: `modbus-override.yml`, `dnp3-command-injection.yml`,
  `capbank-switching-attack.yml` (capbank-sim container stays - the
  RTAC keeps polling it; just no dedicated exercise targets it).

- **Lab 1.2 trimmed:** Step 4's `tshark` views narrowed to host-pair
  conversations on the main path (other two views moved to a hint).
  Step 7 (Define success criteria) moved into Lab 1.3 where it fits
  the design conversation.

- **Lab 1.3 trimmed:** Cut Steps 5–7 (preview improved / apply /
  revert) - they spoiled the hands-on build in Lab 2.2.

- **Lab 2.4 trimmed:** Per-attack repetitive validations dropped
  (those now live in Lab 2.3); kept the holistic positive/negative
  test pass and PCAP evidence assembly. Added a closing reflection
  step.

- **Lab 2.2 / 2.4 CLI + UI rewrite.** Replaced curl-as-CLI hints
  ("Creating rules via the CLI: `curl -X POST .../api/v1/policies`")
  with the actual containd appliance CLI commands (`show running-config`,
  `set firewall rule`, `delete firewall rule`, `commit`,
  `commit confirmed 60`, `show audit`, `export config`,
  `import config`). Web UI walkthroughs added alongside as the
  preferred path. Remaining `curl` references are explicitly
  labeled as lab conveniences (RTAC `/api/state`, `/api/health`,
  FUXA HMI smoke test, traffic generator).

### Frontend / UX

- **In-app `fw-1` terminal lands directly in the containd CLI.**
  Backend `ExecShell` special-cases the firewall container to launch
  via `containd cli` with bash as the fallback. On CLI exit (typing
  `shell`, `bash`, `exit`, `quit`, or `logout`) the wrapper drops
  the operator into bash for low-level diagnostics.
- New `docs/quickstart.md` carries the full install walkthrough
  (online / build-from-source / offline-SSD), common-error guide,
  and what a good bug report includes.
- `README.md` reorganized - Quick Start moved above the fold (was
  at line 179 of 298). Trimmed from 298 → 136 lines by removing
  duplicated Repository-Layout and Current-Status sections.

### Infrastructure

- **`vendor-jump` container** now runs `xrdp` (3389), `tigervnc`
  (5900, no-auth lab convenience), and `openssh-server` (22). User
  `vendor-user` (password `vendor`) is created at startup via
  `scripts/start-rdp-vnc.sh` mounted into `/custom-cont-init.d/`.
- **`kali` container** gains `freerdp-x11` (xfreerdp),
  `xtightvncviewer`, and `sshpass` for the vendor-rdp-compromise lab.
- **`substation-weak.json`** firewall policy adds `tcp/5900` (VNC)
  to the existing `enterprise-to-vendor` rule alongside SSH/HTTP/
  HTTPS/RDP; `substation-improved.json` already restricts to
  22/80/443 only, so hardening blocks RDP and VNC by
  absence-from-allow.
- **containd dependency** moved from a digest-pinned per-release
  bump to `containd:latest` in both compose files. Both repos are
  co-developed and stay in sync by convention; `docker compose pull`
  picks up containd security fixes (e.g., the Go 1.25.9 bump in
  containd v0.1.20) without a RangerDanger compose change.

### Changed

- `Scenario.Order` field changed from `int` to `string` across the
  backend, YAML loader, and frontend, so the lab YAML's `order:`
  field is the workshop lab number directly (`"1.2"`, `"2.3-bonus"`,
  etc.). Removed the parallel `frontend/lib/workbook-sections.ts`
  mapping; the UI now renders `Lab {scenario.order}` directly.
  **DB migration note:** delete `data/labs.db` if you have an
  existing local database from a prior build - the data is rebuilt
  from YAML on startup so no user-entered state is lost.

- `orchestrator.createContainer` now fails fast with a clear error
  when a node references an unknown network zone, instead of silently
  no-op'ing and letting the container land on Docker's default
  bridge. Pinned with `backend/internal/orchestrator/orchestrator_test.go`.
  Surfaced by Codex review on PR #26.

- Backend dispatch in `handleValidateScenario` cleaned up: removed
  case arms for the deleted exercise IDs, added a case +
  `validateHardeningConfigurations` validator for Lab 2.3, dropped
  ~200 lines of unreachable validator code.

### Setup scripts

- `setup.sh` / `setup.ps1` port-busy preflight now shows the
  holding process (name + PID) for each conflicting port - saves
  the "go run lsof / netstat" round-trip.
- `setup.sh` / `setup.ps1` `docker compose pull` wrapped in a
  3-attempt retry with 15s / 30s backoff for transient GHCR 5xx
  during layer fetches. On final failure the diagnostic enumerates
  the common causes (network blocks ghcr.io, GHCR down, disk
  full).

### Tests

- **Backend** - 14 new tests in `scenario_validate_test.go` covering
  the surviving validators (hardening-configurations, vendor-RDP,
  validation-evidence, remediation-planning, generic) + helper smoke
  tests (`mapGet`, `boolGet`, `intGet`, `countAuditByZoneAndCommand`).
- **Frontend** - Vitest framework wired in (`vitest.config.ts`,
  `npm test` script, CI step between lint and build). 27 new tests
  covering `frontend/lib/remediation-to-rules.ts` (the dynamic-
  remediation pipeline that drives Lab 2.2's adaptive content) and
  `frontend/lib/exercise-nodes.ts` (lab → terminal node mapping).
- **Smoke** - `scripts/smoke-test.sh` (host-runnable) +
  `.github/workflows/smoke.yml` (CI) updated for the 7-lab
  inventory: validates exact `(order, id)` tuples and per-lab step
  counts via `description` occurrence count.

### CI

- `release.yml` retries the `Build and push` step up to twice on
  failure (with 30s and 60s delays) to absorb transient GHCR 5xx
  during layer uploads. Each retry reuses already-pushed blobs via
  buildkit's layer dedup, so only the failed layer actually
  re-uploads.
- `ci.yml`'s `govulncheck` job is now a hard gate (was advisory).
  An allowlist of two `docker/docker` OSV IDs (GO-2026-4887,
  GO-2026-4883) - the only findings without an upstream fix - keeps
  known exceptions passing; any new finding fails the build.

### Security

- `backend/go.mod`: `quic-go` v0.54.0 → v0.57.0 (clears
  `GO-2025-4233`, `GO-2025-4017`); `golang.org/x/crypto` v0.44.0 →
  v0.50.0 (clears `GO-2025-4134`, `GO-2025-4135`). Surfaced when
  the govulncheck gate flipped above.

### Documentation

- Replaced `docs/release-plan.md` (a v0.1.0-cutover working doc)
  with `docs/tasks.md`, a prioritized P1/P2/P3 backlog. `ROADMAP.md`
  remains the longer-horizon view.
- Stale references to old exercise numbering (Exercise 0, 3, 4) and
  removed exercise IDs scrubbed from `docs/architecture.md`,
  `docs/api-spec.md`, `docs/workshop-overview.md`, and `ROADMAP.md`.
- GitHub repo description updated from "9 guided exercises" to
  "7 labs aligned to the DefendICS workshop".

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
- `fuxa_appdata/`, `fuxa_db/`, and seven legacy `data/` files -
  re-introduced by the v0.1.0 distribution-mvp merge - re-untracked.
  Same lab-default-credential class as the existing `containd`/
  `openplc` defaults documented in `SECURITY.md`; cleanup is
  hygiene rather than vulnerability response.
- Legacy oil-plant network mappings (`it_net`, `dmz_net`,
  `ot_control_net`, `ot_safety_net`) finally removed from
  `orchestrator.go` (an earlier "removed" commit message was a no-op).

### Tests

- `backend/internal/server/exec_test.go` - 33 cases pinning the
  command-allowlist behavior on `/api/workshop/exec`, including
  the documented shell-injection bypass and a regression guard
  ensuring every tool the scenarios auto-run stays in the
  allowlist.
- `dnp3go/roundtrip_test.go` - link-frame round-trip across 7 size
  classes, garbage-skipping, CRC rejection, APDU round-trip,
  encoder shape checks. Coverage moved from CRC-only to all four
  protocol layers.

### Tooling

- `setup.sh --check-only` and `setup.ps1 -CheckOnly` - runs
  pre-flight checks (Docker, Compose, ports, disk, memory) and
  exits without installing. Pre-workshop "is my laptop ready?"
  verification.
- `.github/workflows/smoke.yml` - bring-up smoke test on every PR
  and push to main. Builds the stack, hits `/api/health` and
  `/api/build`, confirms 9 exercises load and ≥8 services report
  healthy, dumps logs on failure. Catches startup regressions
  unit tests don't see.

### Community / OSS polish

- `ROADMAP.md` - public forward look (v0.1.x, v0.2.0, v0.3.0,
  backlog). Linked from `README.md`.
- `SUPPORT.md` - where to ask questions, what to expect from
  maintainers, separate channel for security vs commercial
  workshop support.
- `CITATION.cff` - for academic / training / research use; GitHub
  renders this in the sidebar.
- `CODE_OF_CONDUCT.md` - Contributor Covenant 2.1 with
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
- `dnp3go/README.md` - dropped the dangling "see CLAUDE.md" pointer.

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
  bank, RTAC, historian, GPS clock - each speaking HTTP REST,
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
- DNP3: in-tree `dnp3go/` standalone Go module - zero external
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

- All host-exposed ports bound to `127.0.0.1` only - the lab is
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
- **Dependency scan (`dep-scan.yml`)** - Trivy scans the published
  images weekly and on tag push for OS-package CVEs (Kali rolling,
  Linuxserver webtop bases, Python 3.12-slim) that govulncheck
  can't see. Findings upload to the GitHub Security tab via SARIF.
- `.github/dependabot.yml` covers gomod (×3), npm, docker, and
  github-actions ecosystems on a weekly cadence with major-version
  bumps suppressed pre-1.0 for stability.
- Go toolchain pinned to **1.25.9** in all three modules - clears
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

- `dnp3go/roundtrip_test.go` - link-frame round-trip across 7 size
  classes, garbage-skipping, CRC rejection, APDU round-trip,
  encoder shape checks. Coverage moved from CRC-only to all four
  protocol layers (link, transport, application, encoders).
- `backend/internal/server/exec_test.go` - 33 cases pinning the
  command-allowlist behavior on `/api/workshop/exec`, including
  the documented shell-injection bypass and a regression guard
  that every tool the scenario YAMLs auto-run stays in the
  allowlist.

[Unreleased]: https://github.com/tonylturner/rangerdanger/compare/v0.1.10...HEAD
[v0.1.10]: https://github.com/tonylturner/rangerdanger/releases/tag/v0.1.10
[v0.1.9]: https://github.com/tonylturner/rangerdanger/releases/tag/v0.1.9
[v0.1.8]: https://github.com/tonylturner/rangerdanger/releases/tag/v0.1.8
[v0.1.7]: https://github.com/tonylturner/rangerdanger/releases/tag/v0.1.7
[v0.1.6]: https://github.com/tonylturner/rangerdanger/releases/tag/v0.1.6
[v0.1.5]: https://github.com/tonylturner/rangerdanger/releases/tag/v0.1.5
[v0.1.4]: https://github.com/tonylturner/rangerdanger/releases/tag/v0.1.4
[v0.1.3]: https://github.com/tonylturner/rangerdanger/releases/tag/v0.1.3
[v0.1.2]: https://github.com/tonylturner/rangerdanger/releases/tag/v0.1.2
[v0.1.1]: https://github.com/tonylturner/rangerdanger/releases/tag/v0.1.1
[v0.1.0]: https://github.com/tonylturner/rangerdanger/releases/tag/v0.1.0
