# Changelog

All notable changes to RangerDanger are documented here. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the
project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v0.1.26] - 2026-06-01

A DNP3 protocol sweep — fixing a wire-format bug that corrupted field-device
polls, adding `dnp3cmd` control modes, and aligning the lab content and DPI —
plus a round of lab-exercise refinements (Labs 1.2 / 2.2 / 2.3 / 2.4) and a
validation-report correctness fix.

### Fixed

- **DNP3 link-frame CRC corruption.** `dnp3go`'s `WriteLinkFrame` appended
  each 16-byte data block's CRC into the payload's own backing array,
  overwriting the first two bytes of every following block. Any DNP3
  response longer than one link block was corrupted on the wire — so
  `dnp3poll` against the RTAC (15 binary + 8 analog inputs) returned
  garbage analog values and dropped binary inputs past the first block.
  Field-device polls now return correct multi-block data. Added a
  payload-mutation regression guard and wire-level integration tests
  (`services/dnp3wire`) using the real sim point maps.
- **Validation report no longer claims PASS when probes were skipped.**
  A probe whose source container couldn't be exec'd into (renamed,
  stopped, or missing) was excluded from both the pass counts and the
  totals, so a partially-running lab could report PASS while part of the
  validation matrix never ran. Skipped probes are now counted and force
  the result to FAIL, surfaced in the summary with a "bring the full
  stack up and re-run" message.

### Added

- **DNP3 control modes in `dnp3cmd`.** Beyond the default Direct Operate
  (FC05), the tool now supports `-sbo` (Select-Before-Operate, FC03 then
  FC04) and `-no-ack` (Direct Operate No Ack, FC06). The outstation
  already implemented SBO but no client could reach it; FC06 is a new
  fire-and-forget control path.
- **DNP3 outstation realism.** Outstations now report IIN1.7 (Device
  Restart) once per device on the first response after start (device-wide
  state, so a master that reconnects per poll sees it exactly once) and
  accumulate `IIN2 ObjectUnknown` on multi-object requests instead of
  discarding earlier valid output.
- **Lab 2.3 FC06 evasion hint.** The hardened re-test step now teaches
  Direct Operate No Ack as a DPI-evasion technique — students run
  `dnp3cmd ... -no-ack` and see it blocked at both L4 and DPI (containd's
  Direct Operate rule covers FC05 and FC06). Pairs with the containd IDS
  fix that makes this claim true.
- **Lab-exercise refinements (Labs 1.2 / 2.2 / 2.3 / 2.4).** Guided/Advanced
  track chips with inline switching on Lab 2.2, copy-only command blocks for
  interactive/containd-CLI tools, an in-app "Generate Validation Report"
  flow on Lab 2.4 backed by a new `/api/firewall/validation-report`
  endpoint that validates the *active* policy, a Lab 1.2 golden-path
  compression, and assorted content-accuracy fixes. See commit history for
  the full list.

### Changed

- **Knowledge base DNP3 docs corrected.** The `dnp3poll`/`dnp3cmd` command
  syntax in the tool reference never matched the real CLIs (nonexistent
  `-o trip` / `-s` flags); replaced with the actual
  `-a <addr> crob <index> <action>` syntax the lab scenarios use, and
  documented the new `-sbo`/`-no-ack` modes and FC06 in the DNP3 protocol
  section.

## [v0.1.25] - 2026-06-01

Adds the **Load Simulator** — a training-infrastructure panel for exploring the
OpenDSS physics engine by driving feeder load — plus a feeder-realism and
power-flow accuracy pass.

### Added

- **Load Simulator panel** on the Substation Process View (below the Voltage
  Trend chart). A bonus free-play control — OFF by default, so it never changes
  baseline lab behavior — that drives the feeder's general/critical load and
  power factor through a new `lab-control` override into OpenDSS. Five grid-state
  presets (overnight → hot day) ramp the load smoothly; a single-shot
  **"Large load drop"** models a NERC Level 3 large-load reliability event;
  manual sliders fine-tune. Every action logs to the Command Audit tab as
  `lab-control`, and automatic regulator / cap-bank responses now appear there
  tagged `auto`.

### Changed

- **Power factor is read from the OpenDSS source solve** (real P/Q) instead of a
  modeled estimate — it now reflects the true reactive flow (load Q, cap
  injection, line charging, reactive losses).
- **Feeder lengthened to a realistic ~12 mi** so the unregulated load-bus
  voltage responds to demand. At this <10%-of-ampacity loading the feeder is
  electrically stiff, so the response is modest *by design* — the large-load
  drop produces a real but undramatic voltage step rather than a manufactured
  one, and the demand ±3% walk now runs under the override too.
- **Voltage Trend chart**: shaded 114–126 V safe-zone band with dashed edges, a
  zoomed Y-axis, and an amber trace + "OUT OF BAND" badge whenever a trace
  leaves the band (any cause). One-Line / Supervisory / Electrical Detail device
  order is source→load.

### Fixed

- **Cap bank could auto-brick itself** — the auto-control loop is now rate-
  limited (30 s dwell) so it can't rack up the 6-operation contact-wear lockout
  by hunting. Manual / attacker rapid-cycling still reaches the lockout.
- **`substation-smoke` re-energizes the feeder first**, so it no longer fails
  when run after smokes that trip the breaker / open the recloser / drive the
  tap (the cause of the v0.1.24 release-Smoke failure).
- Load Simulator timer/animation cleanup on unmount; unified customer-impact
  count and lime accent across views; honest fault-current display when
  protection has already cleared; reset-modal and slider accessibility.

## [v0.1.24] - 2026-05-31

Capacitor-bank HMI integration plus a supervisory-control "make every
button real" pass. The switched cap bank is now modeled in the OpenDSS
power flow and driveable from the React HMI; the voltage regulator and
cap bank gain real RTAC closed-loop auto control; the feeder breaker's
fault injection actually trips the breaker; and the One-Line / Supervisory
/ Electrical Detail views are reordered to match the feeder topology. Adds
an end-to-end command validator that exercises every supervisory command.

### Added

- **Capacitor bank in the HMI + OpenDSS physics.** The 300 kVAR switched
  shunt cap bank (10.40.40.23) is modeled in the feeder power flow and
  controllable from the React HMI (switch in/out, manual/auto, reset
  lockout). Switching it in injects VARs — power factor rises toward unity,
  feeder current drops, voltage lifts slightly. Rendered on the Feeder
  One-Line, Supervisory Control, and Electrical Detail tabs alongside the
  breaker, recloser, and regulator.
- **Real RTAC closed-loop auto control.** In AUTO the regulator now holds
  its voltage setpoint (AVR) and the cap bank corrects power factor, via the
  RTAC's 2 s control loop — previously the AUTO/MANUAL toggles were inert
  labels. MANUAL defeats the loop (the intended cyber-to-physical lesson),
  and the loop no-ops on a de-energized bus so it never chases a dead feeder.
- **End-to-end supervisory command validator** (`scripts/substation-validate.sh`):
  drives every command through the real HMI API path and asserts device
  state, OpenDSS-backed telemetry, audit entries, and safe rejection of
  invalid/unsafe commands (30 checks, all green). Substation physics +
  auto-loop smoke (`scripts/substation-smoke.sh`) is wired into CI.

### Changed

- **Feeder breaker "Inject Fault" now trips the breaker.** It previously set
  a status flag with no physical effect; the 50/51 overcurrent element now
  trips the 52 breaker for a total feeder outage — distinct from the
  recloser's mid-feeder trip-and-reclose sequence.
- **HMI topology consistency.** The Feeder One-Line renders the voltage
  regulator as the series node feeding the (post-regulator) critical load,
  and the Supervisory Control / Electrical Detail device lists are ordered
  source→load (Breaker → Recloser → Cap Bank → Regulator) to match the
  OpenDSS circuit. Manual actuation buttons are greyed when a device is in
  AUTO, and One-Line kW values are rounded for display.
- **Knowledge wiki** corrected the false "no dedicated capacitor bank
  simulator" claim now that the cap bank is real.

## [v0.1.23] - 2026-05-31

Cross-platform lifecycle hardening from a full Windows + Linux/macOS
testing pass: fixes the WSL2 DPI-kernel install on Windows, surfaces a
silently-rolled-back hardened policy, makes the workshop reset
idempotent, pins shell scripts to LF, completes the Node-24 action
migration, and adds one-command lifecycle test harnesses for every
platform.

### Added

- **Multi-arch lifecycle test harnesses.** `scripts/test-lifecycle.sh`
  (Linux/macOS) and `scripts/test-lifecycle.ps1` (Windows) run the real
  `setup` + uninstaller and assert the full setup → execute → teardown
  lifecycle (stack healthy, OpenPLC running, workshop APIs respond, clean
  teardown). `scripts/test-arm-linux-emulation.sh` covers the arm64
  emulation register → run → revert round-trip. `docs/testing-multiarch.md`
  documents the platform × install-path matrix, the Multipass arm64 recipe,
  and the Windows procedure. Both harnesses were exercised end-to-end on
  Windows and Linux/macOS during this release's testing pass — which is
  where the fixes below came from.

### Changed

- **CI: all workflow actions moved to Node 24.** Bumped the docker /
  artifact / codeql actions to their Node-24 majors (build-push v7,
  login/buildx/qemu v4, metadata v6, upload-artifact v7, download-artifact
  v8, setup-go v6, codeql v4; trivy left as a composite action), completing
  the Node-20-removal prep started in v0.1.22. Verified by a green release
  test-build with zero deprecation warnings — the Sept-2026 Node-20 removal
  won't break the release pipeline.
- **`setup.ps1` can now actually install the WSL2 DPI kernel.** Two problems,
  both Windows-only: (1) setup splatted the installer args as a PowerShell
  **array** (`@("-ReleaseTag","latest")`), which mis-binds — `-ReleaseTag`
  landed positionally in the installer's `$KernelPath`, so it died with
  `-KernelPath does not exist: -ReleaseTag` and the kernel never installed
  (the online *and* `-FromTarballs` paths both hit this; it surfaced only when
  the kernel was actually missing). Switched to a **hashtable** splat, which
  binds by name. (2) setup didn't forward consent, so even once binding
  worked it blocked on the installer's `[y/N]` prompt (`-CheckOnly` only
  warns, never installs). Running setup is already the go-ahead, so it now
  passes `-Yes`: a single `.\setup.ps1` installs the kernel and brings the
  stack up with nothing to babysit. The installer still prints its
  `About to: … wsl --shutdown` banner; `-SkipKernelFix` skips the kernel.
- **Backend health-check budget bumped 60 s → 120 s in `setup.ps1`.** A cold
  start after a fresh image pull on Windows / WSL2 routinely takes 60–90 s,
  which tripped the old 60 s budget into a "Backend didn't report healthy"
  warning even though the readiness gate seconds later passed. Mirrored to
  `setup.sh` (same 120 s budget).

### Fixed

- **Web/management tier now survives a WSL2 / Docker restart.**
  `backend`, `frontend`, and `proxy` were the only 3 of 19 compose
  services without a restart policy, so any Docker/WSL2 VM bounce (a
  Windows reboot, a Docker Desktop update, or `setup.ps1`'s own
  `wsl --shutdown` for the DPI kernel) left the `restart: unless-stopped`
  sims + firewall self-healing while the entire web tier stayed
  `Exited(255)` — a running-but-headless stack with no portal or API at
  `:8088`, and nothing flagging it. Added `restart: unless-stopped` to all
  three in `docker-compose.release.yml` and `docker-compose.yml`.
  Cross-platform safe; verified on Windows by a real `wsl --shutdown`
  after which all 19 services (web tier included) auto-recovered.
- **Silent hardened-policy failure now surfaces loudly.** When the WSL2
  kernel lacks `CONFIG_NFT_QUEUE`, the hardened policy's `queue num` DPI
  rules fail to load and — nft being atomic — the whole hardened ruleset
  rolls back, yet `POST /api/firewall/apply` still returns HTTP 200 with a
  `warnings` array. `setup.ps1`'s workshop-readiness gate piped that
  response to `Out-Null`, so the install passed green while segmentation
  silently did not enforce (`kali → rtac:502` still reachable under the
  "applied" hardened policy). The gate now inspects the improved-apply
  body and prints an unmissable warning with the exact fix
  (`install-wsl-kernel.ps1`); `scripts/test-lifecycle.ps1` likewise fails
  its execute assertion instead of green-lighting it.
- **Workshop reset is idempotent again.** `POST /api/workshop/reset`
  reported overall `success:false` on an already-clean stack: the
  capbank `switch_in` was the only non-idempotent reset command, returning
  `rejected` / "already switched in" when the bank was already energized
  (the default reset state), which the reset ANDed into an overall
  failure. The capbank sim now reports an idempotent no-op success when
  already in (switch count unchanged), matching every other reset command.
  `setup.ps1`'s readiness gate also stopped masking real reset failures —
  it matched the substring `"success":true` against the whole body (which
  embeds per-action `"success":true` entries) and now parses the
  top-level field, the way `test-lifecycle.ps1` already did.
- **Bash side at parity.** `setup.sh` and `scripts/test-lifecycle.sh` get
  the same two gate fixes: inspect the `firewall/apply` body for the silent
  hardened-policy rollback, and parse the top-level `workshop/reset`
  `success` via `python3` instead of a substring match. (Linux native
  usually has `nfnetlink_queue`, so the warning is mostly a safety net there.)
- **Shell scripts pinned to LF for Windows builds.** `.gitattributes` was
  only `* text=auto`, so a Windows checkout (`core.autocrlf=true`) gave
  every `*.sh` CRLF endings. Baked into the Linux sim images, a CRLF
  shebang makes the kernel look for `/bin/sh\r` and the container dies
  with `set-gateway.sh: not found`, crash-looping — breaking
  `docker compose build` and `scripts/smoke-test.ps1` on Windows (the GHCR
  images are unaffected; CI builds them on Linux). Added `*.sh text eol=lf`.
  Existing Windows working copies need a one-time `git rm --cached -r . &&
  git reset --hard` (or re-clone) to pick up LF endings.

## [v0.1.22] - 2026-05-30

Multi-arch reliability hardening on top of v0.1.21: catches a silent
OpenPLC failure at setup time, makes arm64-Linux emulation survive
reboots, pins the emulation helper for reproducibility, and stops a
failed release from silently leaving an incomplete draft.

### Added

- **OpenPLC readiness probe in `setup.sh`.** The workshop-readiness gate
  now verifies the OpenPLC container actually reaches `running` rather
  than crash-looping with `exec format error` (the silent failure mode
  when amd64 emulation is missing on arm64 Linux). Non-fatal — OpenPLC is
  isolated — but warns loudly with the exact fix instead of passing the
  install green.
- **`scripts/persist-emulation.sh`** — opt-in (`sudo`) helper that
  installs a systemd oneshot unit re-registering amd64 emulation on every
  boot, so OpenPLC survives reboots on arm64 Linux. `--uninstall` removes
  it. `tonistiigi/binfmt`'s own registration is runtime-only; this makes
  it persistent for multi-day workshops.
- **Release-failure alert.** `release.yml` gains a `release-failed` job
  that files (or comments on) a tracking issue whenever a release build
  doesn't complete — the silent-draft failure mode that left v0.1.19 and
  v0.1.20 as empty drafts.

### Changed

- **Pinned `tonistiigi/binfmt` to `qemu-v10.2.1`** (was `:latest`) across
  `setup.sh`, the uninstaller, and the SSD stage scripts. A version tag
  (not a raw digest) so the SSD-staged image still resolves offline;
  reproducible online and offline.
- **CI: bumped `actions/checkout` and `actions/setup-node` to v5**
  (Node 24) ahead of GitHub's Node 20 removal. Docker/third-party actions
  still pending a verified bump.
- README and `docs/workshop-ssd.md` now document arm64-Linux OpenPLC
  emulation (qemu-x86_64 via `tonistiigi/binfmt`, bundled in
  `images-arm64.tar`), not just Apple-Silicon Rosetta.

## [v0.1.21] - 2026-05-30

ARM64-Linux workshop support plus post-workshop tooling. Adds automatic
amd64 emulation for OpenPLC on arm64 Linux laptops (where Docker Engine
has no Rosetta), a granular uninstaller, and the multi-arch
release-pipeline fix that unblocks publishing. Rolls up the seg-drawer
and lab-content fixes that landed after v0.1.20.

### Added

- **Automatic amd64 emulation for OpenPLC on arm64 Linux.** OpenPLC is
  amd64-only upstream (`tuttas/openplc_v3`); macOS Apple Silicon runs it
  under Rosetta via Docker Desktop, but Docker Engine on arm64 Linux has
  no such shim. `setup.sh` now detects arm64 Linux, checks for an
  existing `qemu-x86_64` binfmt handler, and registers one via
  `tonistiigi/binfmt` if absent — best-effort and non-fatal (nothing but
  OpenPLC depends on it). `scripts/uninstall-rangerdanger.sh` reverts the
  handler, but only when setup installed it (tracked by a
  `.setup-binfmt-amd64` marker). Note: binfmt registration is runtime
  only and does not survive a reboot.
- **`tonistiigi/binfmt` staged onto the offline SSD.** `stage-ssd.sh`
  bundles the helper into `images-arm64.tar` (arm64 only — amd64 hosts
  run OpenPLC natively), and `stage-ssd-delta.sh` cross-includes it in
  `delta-arm64.tar`, so a `--from-tarballs` install registers emulation
  with no network access.
- **Granular uninstaller image controls.** `--remove-dev-images`
  (locally-built `rangerdanger-*` images), `--remove-base-images`
  (shared alpine/nginx/fuxa/webtop — never force-removed, images in use
  by another container are kept), and `--purge` (release + dev images, a
  clean slate for redeploy testing). Mirrored in the Windows
  `uninstall-rangerdanger.ps1`.
- **`scripts/test-arm-linux-emulation.sh`** — validates the arm64
  detect → install → run → revert path end to end in a throwaway VM.

### Fixed

- **Multi-arch release pipeline: frontend arm64 build.** The Next.js SWC
  native binary fails to build under QEMU arm64 emulation
  (`@next/swc-linux-arm64-gnu: __res_init: symbol not found`), which left
  the v0.1.19 and v0.1.20 releases as incomplete drafts (no frontend
  image, empty notes). `release.yml` now builds the frontend per-arch on
  native runners (`ubuntu-24.04-arm` for arm64) and stitches the manifest
  list via push-by-digest, removing emulation from that build.
- **Seg-drawer policy banner** syncs the Active Policy banner with the
  top-right PolicyBadge, shows a policy-aware empty state for Live DPI
  Events, and renders implicit-deny rows under the weak baseline.
- **Lab content** — Lab 1.3 / 1.4 review-pass corrections, plus Lab 1.2
  curl-scaffolding trim and the enterprise-traffic answer fix.

### Changed

- `frontend/next-env.d.ts` is now gitignored and untracked (Next.js
  regenerates it; its boilerplate comment churned across versions).

## [v0.1.20] - 2026-05-30

Workshop-launch release. Bundles the dual-track firewall lab structure
(non-technical guided path + technical author-in-containd path), a
containd manual-commit observer that lets the lab UI detect direct
CLI/UI commits, a knowledge wiki redesign with admonition syntax and
cross-link routing, and a batch of smoke / CI reliability fixes. No
breaking schema changes; existing lab progress in localStorage is
preserved.

### Added

- **Dual-track firewall lab path (Labs 2.2 / 2.3 / 2.3-bonus / 2.4).**
  New explicit fork between a **Guided** track (apply policies via
  side-panel buttons, walk the containd interfaces for understanding
  but don't author rules) and a **Technical** track (author and
  commit rules in containd's web UI or CLI directly, no rangerdanger
  buttons). Mechanism:
  - New `useFirewallTrack` hook with `localStorage` persistence
    (`rangerdanger.firewall-track`).
  - New `:::track-picker` description directive renders a two-card
    picker on Lab 2.2 step 1. Mark Complete is force-gated until a
    track is selected.
  - New `:::guided` / `:::technical` description directives let lab
    authors fork only the lines that vary, not whole descriptions.
    When no track is picked, both blocks render so students see both
    perspectives before deciding.
  - Side-panel chip on every firewall lab shows the current track
    with a one-click switch.
  - On the Technical track, the apply buttons render dim + small,
    labelled "guided fallback" — visible as an escape hatch without
    dominating the panel.
- **`PolicyStatusBanner` — sticky per-step indicator of what firewall
  policy is currently running.** Reads `active_config` + the new
  `policy_source` field. Matched variant shows the policy in a
  color-coded card; mismatched (step's `expected_config` differs)
  shows an amber warning with the action prompt inline. Custom
  policies are labelled `(Lab 1.4 plan)` vs `(your containd commit)`.
- **`policyObserver` backend goroutine — detects manual containd
  commits.** Polls containd's running-config hash every 5s, compares
  against the last applied hash (recorded by the apply handlers). On
  divergence past a 5s grace window: sets `activeConfig=custom` +
  `policy_source=manual-custom` so the banner correctly labels a
  student-authored policy committed directly through containd's
  CLI/UI. Five unit tests with `-race` cover no-change / divergence /
  grace-window / baseline-seed / concurrent-apply scenarios.
- **`policy_source` field on firewall API responses.** New field on
  `GET /api/firewall/active`, `POST /api/firewall/apply`, and
  `POST /api/firewall/apply-custom`. Values: `"weak"`,
  `"hardened-reference"`, `"plan-custom"`, `"manual-custom"`, `""`.
  Lets the frontend banner accurately distinguish the canned weak
  baseline, the canned hardened reference, the student's Lab 1.4
  plan, and a directly-committed-in-containd policy.
- **Knowledge wiki: admonition syntax + cross-link routing.**
  Articles can now use `:::tip`, `:::note`, `:::warning`,
  `:::caution` blocks for visual breakup. Markdown links of the
  form `[text](#article-id)` route in-page to the target article;
  the URL bar mirrors the current article (`/knowledge#article-id`)
  with `pushState` so the browser Back button walks article history
  correctly. Lab YAMLs can now deep-link readers in one hop instead
  of three.
- **Knowledge wiki: per-section color identity across cards +
  article views.** Six-section accent palette (sky / emerald /
  violet / amber / slate / rose) carries through landing tiles,
  category cards, search results, and the article reading view via
  new `cardBg`, `cardBorder`, `leftBar`, and `topBar` fields. Each
  topic tile registers visually distinct at a glance without being
  loud.
- **Knowledge article: "Default-Deny: Implicit vs Explicit Across
  Firewalls."** Covers the two mechanism models, a per-vendor table
  (containd, Cisco ASA/FTD, Palo Alto, FortiGate, Juniper SRX, Check
  Point, pfSense/OPNsense, iptables/nftables, Linux bridge/OVS), why
  this matters in OT, and three audit questions for spotting
  default-deny misconfigurations.

### Changed

- **Lab 2.2 (Firewall Policy Implementation) Phase 2 reframed to
  teach the principle, not just containd's mechanism.** Old wording
  said "Set default action to DROP" as if it were a separate switch,
  which led students to look for a UI control that doesn't exist
  (containd's policy already has `defaultAction: DENY` as a
  top-level field, set out of the box). New framing explains
  implicit vs explicit default-deny, names which vendors fall on
  each side, links to the new `/knowledge#default-deny` article, and
  preserves the containd-specific guidance as a `:::hint` calling
  out that raw iptables / OVS deployments would need additional
  chain-policy or explicit cleanup-rule work.
- **Knowledge wiki: FUXA reframed — `/substation` is THE lab HMI.**
  Audit established the FUXA service exists and `hmi_poller` is
  polling, but every lab exercise points students at `/substation`
  for the alarm chain, customer-service tile, and "Operational
  consequence at the HMI" callouts. Rewrote the HMI/SCADA article
  to lead with `/substation` as the primary HMI, with FUXA framed
  as a contextual reference. Renamed to "HMI, SCADA, and the Lab's
  Substation Panel" to set expectations up front.
- **Side-panel policy actions restructured to buttons-only across
  the firewall labs.** The status indicator now lives in the
  `PolicyStatusBanner` at the top of each step. Apply Hardened
  shows whenever the active policy isn't already improved; Reset to
  Weak shows whenever it isn't already weak; Apply Your Plan is
  disabled-with-tooltip until Lab 1.4 has a saved remediation plan.
  The buttons now appear on every firewall lab (Labs 2.2 / 2.3 /
  2.3-bonus / 2.4) instead of just the implementation lab.

### Fixed

- **`events-smoke.sh` polling-window race resolved + `.ps1` sibling
  ported to the same pattern.** The original single-shot read after
  `PROBE_WAIT` (default 4s) was racing the nflog consumer + engine
  event store + REST endpoint on CI runner load — the gate sometimes
  declared "nflog consumer regressed" on what was actually just
  propagation latency. Now polls within an `EVENT_POLL_BUDGET`
  budget (default 20s, inclusive of the upper bound after Codex
  review on PR #72 caught the off-by-one), once per second, with
  a fail-loud at the budget edge. PowerShell sibling rewritten to
  use the same polling loop so the two scripts stay in lockstep
  (`PROBE_WAIT + EVENT_POLL_BUDGET` total budget on both).
- **`lab-commands-smoke.{sh,ps1}` no longer silently inherits weak
  policy on `hardened` steps.** Both scripts only handled literal
  `weak` or `improved`; when a step's `expected_config` was
  `hardened` (the user-facing alias used extensively in
  `hardening-configurations.yml` and `validation-evidence.yml`), the
  policy apply was skipped, so re-test commands ran under whatever
  the previous step's policy was — typically weak — and the smoke
  reported a clean PASS for attacks that were supposed to be
  blocked. Now maps `hardened` → `improved` before the apply call.
- **Kali apt-cache update cycles 5 mirrors instead of retrying one.**
  The previous three-attempt retry against a single mirror couldn't
  ride out cases where Cloudflare's edge served stale package
  metadata. Now cycles through `kali.download`, `http.kali.org`,
  `mirror.csclub.uwaterloo.ca`, `mirrors.ocf.berkeley.edu`, and
  `archive-4.kali.org` — different upstreams, different cache
  paths.

### CI

- **`audit-oss` removed from smoke triggers (`smoke.yml` +
  `smoke-windows.yml`).** The branch was a v0.1.4 audit-cycle
  release-tracking branch — long since merged into main and not
  pushed in weeks. Trigger now only fires on `main` and
  `oss-release`. No coverage regression (no remaining caller). The
  branch itself was deleted in the same change.
- **`smoke-windows.yml` `$LASTEXITCODE` reset.** The
  `setup.ps1 -CheckOnly` step's leaking exit code was failing
  otherwise-passing PRs whenever the hosted Windows runner happened
  to lack a working Docker engine. The step's existing comment
  said "Exit code is not the gate; the parse step above is" but
  the intent wasn't enforced; now is.

### Dependencies

- `@react-pdf/renderer` 4.4.0 → 4.5.1 (PR #66, rebased after lockfile cascade)
- `@tanstack/react-query` 5.100.11 → 5.100.14 (PR #65)
- `postcss` 8.5.14 → 8.5.15 (PR #64)
- `linuxserver/webtop` digest bump (PR #63)

### Operator notes

- **containd image:** workshop-launch builds depend on containd
  v0.1.26+ for the `mdlayher/netlink` v1.11.1 fix that resolves a
  commit-pipeline hang on macOS Docker Desktop's LinuxKit kernel
  (visible as `POST /api/v1/config/commit` hanging for 30+ seconds
  on direct CLI/UI commits — i.e. the technical-track workflow).
  The current `ghcr.io/tonylturner/containd:latest` tag points at
  v0.1.28; `docker compose pull firewall` confirms.
- **Workshop scope:** this is the 2026-06-03 workshop launch
  release. The dual-track firewall lab structure is the headline
  feature; the observer + banner work supports it; the knowledge
  redesign supports student self-service during the lab.

## [v0.1.19] - 2026-05-29

Pre-workshop lab simplification pass. Lab 2.3 (Protocol-Hardened
Configurations) cut from 10 steps to 6 by collapsing three full
attack flows into a single primary attack (DNP3 Direct Operate
against the recloser — the canonical distribution-substation attack
vector). Modbus FC5/FC6 variants move into optional `:::hint`
sidebars inside the same step so instructors can extend depth
without lengthening the floor. Lab 2.4 and Lab 1.2 receive matching
adjustments so the workshop reads as DNP3-primary throughout, with
Modbus available as a parallel demonstration of the same defense
generalizing across protocols. No simulator, Dockerfile, compose,
or backend code changes — purely YAML and docs.

### Changed

- **Lab 2.3 (Protocol-Hardened Configurations) streamlined to a
  single primary attack.** The previous 10-step structure walked
  students through three full attacks (Modbus FC6 regulator
  override → DNP3 Direct Operate → Modbus FC5 breaker trip) plus
  three full re-tests under the hardened policy. The new 6-step
  structure leads with a single primary attack — DNP3 Direct
  Operate against the recloser (the canonical distribution-
  substation attack vector against actual installed gear from SEL,
  G&W, S&C, Cooper, etc.) — and consolidates the re-test into one
  step. The Modbus FC5 / FC6 variants live as optional `:::hint`
  sidebars in the same attack step, with a matching optional
  re-test sidebar, so instructors who want the cross-protocol DPI-
  generality lesson still have it without making it mandatory
  cognitive load.
- **Lab 2.4 (Testing & Validation) negative tests now lead with
  DNP3.** Same motivation as Lab 2.3: DNP3 is the authentic
  distribution-substation SCADA protocol, so the primary negative-
  test flow uses DNP3 + nmap port 20000 against field. Modbus
  port-502 / `mbpoll` probes move into an "also confirm Modbus is
  blocked" sidebar for instructors building a two-protocol evidence
  package. The PCAP capture, evidence-package assembly, and
  reflection steps are unchanged — they already covered both
  protocols at the L4-rule level.
- **Lab 1.2 (Baseline Traffic Analysis) protocol framing
  corrected.** The PCAP analysis section previously labeled Modbus
  as "the primary SCADA control protocol" and DNP3 as "the
  secondary SCADA polling protocol" — backwards for distribution
  substations. Relabeled DNP3 as the dominant protocol (and listed
  first), with Modbus as widely-supported alongside it. The lab's
  "confirm internal control relationships exist" sub-step now
  leads with the lab-convenience `curl /api/state` and mentions
  DNP3 + Modbus protocol probes as parenthetical alternatives,
  rather than presenting Modbus as the only protocol-level option.

### Docs

- README.md and docs/workshop-overview.md Lab 2.3 descriptions
  rewritten to reflect the single-primary-attack framing. The
  optional-Modbus-sidebar framing is called out so reviewers
  reading the README understand the change in scope vs v0.1.18.

## [v0.1.18] - 2026-05-28

Pre-workshop polish + CI reliability + security patch. Bundles four
co-landing PRs: x/net@v0.55.0 vuln-bump (#59) with the Kali mirror
fix it depended on for CI, the chmod fix for `scripts/uninstall-
rangerdanger.sh` (#58), the `docs/_internal/` untrack (#60), and a
repo-wide markdown accuracy pass (#61). Also captures the
auto-publish + reproducible-WSL2-kernel CI improvements that landed
between v0.1.17 and the rest of this set.

### Changed

- **`.github/workflows/release.yml` auto-publishes the GitHub Release.**
  Previously the workflow created the release in `draft=true` state so
  `build-wsl-kernel.yml` could attach the kernel asset before the
  release went public. The release job now always passes
  `--draft=false` to `gh release edit` (and `--latest` for non-
  prerelease tags), so once the matrix-built images + release notes
  finish the release becomes public automatically without a manual
  "publish" click in the GitHub UI. Idempotent: setting
  `--draft=false` on an already-published release is a no-op.
- **`.github/workflows/build-wsl-kernel.yml` produces byte-reproducible
  kernels.** The build now pins `SOURCE_DATE_EPOCH`,
  `KBUILD_BUILD_HOST`, and `KBUILD_BUILD_USER`, so the same source
  tag + overlay yields a bit-identical `rangerdanger-wsl2-kernel`
  binary across runs. Documented in `wsl-kernel/README.md` under
  "Reproducing the build locally."

### Fixed

- **`Dockerfile.kali` no longer flakes on Kali rolling-distro apt
  cache misses.** The previous build used `http.kali.org`, a round-
  robin including CDN backends whose `apt-get update` metadata lags
  the actual pool/ directory; the resulting 404s on individual `.deb`
  fetches were intermittently breaking CI smoke and could break
  workshop-day builds. Switched to the Cloudflare-backed
  `kali.download` mirror (same packages, fresher metadata) and
  layered a 3-attempt retry loop on top as defense in depth.

### Security

- Bumped `golang.org/x/net` v0.54.0 → v0.55.0 in `backend/` to
  clear 6 new findings from the 2026-05-26/27 vuln-db refresh
  (`GO-2026-5025`..`5030`): five `net/html` XSS/DoS hardenings and
  one `net/idna` Punycode-decode fix. Backend does not call
  `net/html` parse/Render on attacker-controlled input (HTML is in
  the Next.js frontend), so practical exposure was zero; bumped to
  keep `govulncheck` green. See `docs/security-known-issues.md`.

### Docs

- Repo-wide accuracy pass on every project markdown file. Notable
  corrections: lab time budgets in `docs/workshop-overview.md` (5 of
  7 were wrong, total off by 30 min), `tar -C` calls in
  `docs/workshop-ssd.md` now `mkdir -p` first, `docs/architecture.md`
  RTAC interface inventory corrected to 4 networks (was claimed 2),
  Modbus FC matrix in `services/README.md` corrected to actual
  1/3/4 read + 5/6 write (was claimed 1/2/3/4 + 5/6/15/16),
  `docs/api-spec.md` exec body field corrected to `command` (was
  `cmd`), and the SSH-based terminal path documented in
  `docs/api-spec.md` removed (the path no longer exists in the
  backend). Several broken `docs/tasks.md` / `docs/release-plan.md`
  cross-references retired (those files have been gone since v0.1.10).
- `docs/_internal/` is now `.gitignore`'d. The
  `pdf-update-prompt.md` was intended as a maintainer-only doc but
  was being published on github.com regardless of the leading-
  underscore Jekyll convention.

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

- `substation-improved.json` already shipped `dpiMode: enforce` +
  `nflogGroup` from the v0.1.12 release. v0.1.13 layers the new
  `nfqueueGroup` + `dpiEnabled` on top of the improved policy only.
  `substation-weak.json` carries only `nflogGroup` (no `dpiMode`
  field) by design — without an explicit `dpiMode` it stays in
  containd's implicit learn mode, which is what makes the weak
  baseline weak.

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
  prompt for regenerating the workshop-handout PDF via the
  maintainer's preferred long-context drafting tool when the
  install flow drifts. Single source of truth for what the PDF
  should say, checked against `docs/quickstart.md` and `setup.sh`
  at PDF-regen time.

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

[Unreleased]: https://github.com/tonylturner/rangerdanger/compare/v0.1.26...HEAD
[v0.1.26]: https://github.com/tonylturner/rangerdanger/releases/tag/v0.1.26
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
