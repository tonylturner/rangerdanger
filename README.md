<div align="center">
  <img src="docs/images/rangerdanger-lockup-web.png" alt="RangerDanger" width="520">
  <br>
  <strong>An interactive OT segmentation training environment for electric substation security.</strong>
</div>

<p align="center">
  <a href="https://github.com/tonylturner/rangerdanger/actions/workflows/ci.yml"><img src="https://github.com/tonylturner/rangerdanger/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/tonylturner/rangerdanger/releases"><img src="https://img.shields.io/github/v/release/tonylturner/rangerdanger?include_prereleases&sort=semver" alt="Release"></a>
  <a href="https://opensource.org/licenses/Apache-2.0"><img src="https://img.shields.io/badge/License-Apache_2.0-blue.svg" alt="License: Apache 2.0"></a>
  <a href="https://go.dev"><img src="https://img.shields.io/github/go-mod/go-version/tonylturner/rangerdanger?filename=backend%2Fgo.mod&logo=go&logoColor=white" alt="Go"></a>
  <a href="https://nextjs.org"><img src="https://img.shields.io/badge/Next.js-16-000000?logo=next.js&logoColor=white" alt="Next.js"></a>
  <a href="https://docs.docker.com/compose"><img src="https://img.shields.io/badge/Docker-Compose-2496ED?logo=docker&logoColor=white" alt="Docker"></a>
  <a href="https://github.com/tonylturner/containd"><img src="https://img.shields.io/badge/containd-NGFW-f97316" alt="containd"></a>
</p>

RangerDanger teaches network segmentation by putting students inside a working electric distribution substation. The whole experience runs in a single web portal: an interactive topology map shows the zones and devices, clicking a node opens its container terminal in the browser, structured exercises walk through each lab step-by-step with live validation, and a process view shows how cyber actions actually change voltages, breaker states, and load energization in real time. The firewall at the center is [containd](https://github.com/tonylturner/containd), a purpose-built NGFW with ICS deep-packet inspection, so students inspect Modbus and DNP3 events alongside per-zone policy decisions. The L4 source pin enforces; DPI detects and logs.

7 labs aligned to the DefendICS OT Network Segmentation workshop deck: identify required flows, design segmentation, plan under a labor budget, build the policy, stress-test it against a DNP3 Direct Operate injection (with optional Modbus sidebar attacks), validate with PCAP evidence.

> **Lab-only.** All host ports bind to loopback by default. No auth on terminals or the backend. Default credentials are baked in. Do not connect to production. See [`SECURITY.md`](SECURITY.md) for the full model and safe external-access patterns.

## What you get

- **Interactive topology map** at `/console`. IEC 62443 zone bands (Enterprise SL-1, DMZ SL-2, Supervisory SL-3, Field SL-2) with a live policy overlay that shows which conduits are allowed or blocked under the active firewall config. Click any node to inspect device state, open its web UI, or drop into a terminal. Drawers surface protocol details and live traffic context per node.
- **Embedded container terminals**. xterm.js plus WebSocket exec into manifest-mapped topology nodes. No SSH client, no port forwards. Terminal state persists across navigation via a shared React context, and PTY size propagates so `stty` queries return the right values.
- **Exercise runner** at `/exercises/[id]`. Each lab is a step-by-step walk-through with inline command blocks (one-click run), persistent per-exercise notes, and live state-reading validators that show green/red chips as students hit checkpoints. Apply Your Plan compiles selected actions on the backend; later checks compare results with the applied plan.
- **Process view** at `/substation`. Live feeder state from the RTAC via the OpenDSS power-flow solver. Modbus FC5/FC6 writes and DNP3 CROB attacks measurably change breaker positions, regulator taps, voltages, and load energization within ~3 seconds. Customer-service tile shows how many customers and how much critical load (hospital, fire station) is currently energized.
- **containd NGFW integration**. ICS-aware NGFW at the conduit. Modbus function-code filtering, DNP3 protocol awareness, IT-side DPI for TLS/SNI, HTTP, RDP. The L4 source pin enforces; DPI detects and logs. Students apply policy from the lab UI; containd's full web console is iframed at `/containd/` for direct inspection of rules, audit log, and session state.
- **Knowledge wiki** at `/knowledge`. Curated reference for substation equipment, ICS protocols, segmentation patterns, and the lab's specific architecture. Searchable, markdown-rendered, useful both during exercises and afterward.
- **Modbus and DNP3 simulators**. Every field device (relay, recloser, regulator, capacitor bank, RTAC) speaks Modbus TCP, DNP3 TCP, and HTTP simultaneously against shared state. Field-device controls work via any of the three protocols; the RTAC's Modbus and DNP3 outstations are read-only.
- **PCAP capture and change-board evidence**. Capture cross-zone traffic at the firewall during attack and validation runs. `scripts/validation-report.sh` requests the backend's validation report for the currently running policy, including its probe matrix, policy proof, Hardened acceptance comparison, and PCAP evidence. It does not apply a policy.
- **Validation-driven exercises**. Per-lab Go validators read live simulator state, electrical readings, audit log entries, and active firewall config. Green/red feedback is per-checkpoint, no instructor-side grading required.

## Student journey

Each lab builds on the previous. Your Lab 1.4 remediation plan is compiled and applied in Lab 2.2; subsequent checks test the running policy against that plan. ≈83 minutes (Guided path) for the six core labs (1.2 / 1.3 / 1.4 / 2.2 / 2.3 / 2.4), plus ~12 minutes for the optional 2.3-bonus.

| Step | Lab | What you do |
|---|---|---|
| 1 | **1.2 Baseline** | Capture cross-zone traffic at the firewall. Build a flow inventory. Probe for latent exposure that passive monitoring missed. |
| 2 | **1.3 Requirements** | Translate findings into a zone-pair rule grid. Pick BLOCK / RESTRICT / ALLOW per direction. Anchor the design to operational requirements. |
| 3 | **1.4 Plan** | Choose which findings to fix first under a finite labor budget. Per-role utilization updates in real time as you pick actions. |
| 4 | **2.2 Implement** | Compile and apply your Lab 1.4 plan. Review the generated rules in containd and verify the running policy against your plan. |
| 5 | **2.3 Harden** | Stress-test against a DNP3 Direct Operate injection that disables auto-reclose and trips the recloser — the canonical distribution-substation attack vector. Watch the process view as voltages drop, breakers open, downstream loads de-energize. Re-test under the hardened policy. Optional Modbus FC5/FC6 sidebars cover the same defense pattern. |
| 5b | **2.3-bonus** | Optional kill-chain exercise: vendor RDP/VNC pivot from enterprise to vendor to field. Demonstrates both the perimeter rule and defense-in-depth. |
| 6 | **2.4 Validate** | Capture PCAP under the hardened policy. Assemble change-board evidence. Confirm attacks fail and authorized flows still work. |

## v0.2.0 environments

v0.2.0 uses selectable lab packages; tagged commands below require its published images.
Choose **US distribution substation (DNP3)** (`us-dnp3-substation`) or
**EU telecontrol substation (IEC 104)** (`eu-iec104-substation`). The portal's
Range selector switches the active environment; only one range runs at a time.
The L4 source pin enforces; DPI detects and logs.

The persistent platform is `rangerdanger-platform` (root Compose files).
Each replaceable `rangerdanger` range owns its models, manifest, routes and
curriculum under `lab-definitions/packages/<id>/`. US source exercises remain
in `lab-definitions/scenarios/`; generated US guidance is package-owned.

Setup defaults to US on a fresh install. Select EU with
`./setup.sh --package eu-iec104-substation`; add `--from-source` for a local
build before release images exist. On Windows, use
`.\setup.ps1 -Package eu-iec104-substation` (or add `-FromSource`).
Re-running setup acquires all images before
migration teardown, then restarts the range and resets policy. Save evidence,
desktop work and local changes first; old browser progress is not migrated.
See [upgrading](docs/quickstart.md#upgrading-to-v020) and
[offline distribution](docs/workshop-ssd.md). Windows supports Docker Desktop
with the WSL2 backend and a physical local-drive checkout, not UNC, `\\wsl$`,
mapped network drives, junctions or Hyper-V. Setup checks the root mapping
before starting the platform; real-hardware Windows release acceptance remains
to be verified.

## Quick start

**Recommended capacity:** Docker Desktop 4.42+ or Docker Engine 28.1+ with Compose 2.36+ · 16 GB host RAM (32 recommended) with at least 8 GB allocated to the Docker VM · 30 GB free disk · Apple Silicon or x86_64 · loopback ports `8088 / 9080 / 9443 / 2222` free. The Docker versions are a hard floor that `setup.sh` checks; the rest are recommendations: `setup.sh` only warns on disk and Docker memory; it reads host RAM only on native Linux Docker. See [the quickstart](docs/quickstart.md#prerequisites) for what the preflight measures.

**macOS / Linux:**

```bash
git clone https://github.com/tonylturner/rangerdanger
cd rangerdanger
./setup.sh
```

**Windows (PowerShell, Docker Desktop WSL2; clone onto a local drive):**

```powershell
git clone https://github.com/tonylturner/rangerdanger
cd rangerdanger
.\setup.ps1
```

**Build from source instead** (developers):

```bash
./setup.sh --from-source     # or ./scripts/dev-up.sh; see CONTRIBUTING.md
```

**Offline / SSD** (for workshops where bandwidth is constrained):

```bash
./setup.sh --from-tarballs /Volumes/WORKSHOP_SSD
```

Use `./stage-ssd.sh /Volumes/WORKSHOP_SSD vX.Y.Z` to populate the SSD. The version argument selects the image tags whatever the repo's `.env` says, and the helper verifies both archives against it before reporting success. GitHub releases carry the WSL2 kernel asset, not image tarballs, so students install from an instructor-staged bundle. Full install walkthrough including the offline path and common errors: [`docs/quickstart.md`](docs/quickstart.md). Workshop operators running multi-laptop classroom deployments: [`docs/workshop-ssd.md`](docs/workshop-ssd.md) covers initial staging, mid-workshop delta patches, and recovery.

For an existing install, follow the [release update procedure](docs/quickstart.md#upgrading-to-v020). See the [v0.1.36 workshop errata](docs/v0.1.36-hotfix.md) for historical release-specific errata.

Once the stack is up:

| | URL | Credentials |
|---|---|---|
| **RangerDanger UI** | http://localhost:8088 | - |
| containd Web UI | http://localhost:8088/containd/ | containd / containd |
| containd SSH | `ssh -p 2222 containd@localhost` | containd / containd |
| FUXA HMI (US only) | http://localhost:8088/apps/fuxa-hmi/ | - |
| OpenPLC | http://localhost:8088/apps/openplc/ | openplc / openplc |

Open [http://localhost:8088/exercises](http://localhost:8088/exercises) and start with **Lab 1.2**.

## What it looks like

Screenshots in student-journey order. Click any image to view full size.

<table>
<tr>
<td width="340" valign="middle"><a href="docs/images/screenshot-network-map.png"><img src="docs/images/screenshot-network-map.png" alt="Substation Network Map" width="320"></a></td>
<td valign="middle"><b>Interactive topology console</b> at <code>/console</code>. IEC 62443 zone bands with a live policy overlay. The containd NGFW sits at the conduit; hardened-policy mode marks blocked flows with red ✗. Click any node to inspect or open a terminal.</td>
</tr>
<tr>
<td width="340" valign="middle"><a href="docs/images/screenshot-exercises.png"><img src="docs/images/screenshot-exercises.png" alt="Exercises dashboard" width="320"></a></td>
<td valign="middle"><b>Exercise dashboard</b> at <code>/exercises</code>. 7 labs aligned to the DefendICS deck (1.2 / 1.3 / 1.4 / 2.2 / 2.3 / 2.3-bonus / 2.4). Per-lab progress, time budgets, tags, and a bonus-lab toggle.</td>
</tr>
<tr>
<td width="340" valign="middle"><a href="docs/images/screenshot-lab-runner.png"><img src="docs/images/screenshot-lab-runner.png" alt="Lab runner" width="320"></a></td>
<td valign="middle"><b>Lab runner</b> at <code>/exercises/[id]</code>. Step-by-step instructions, inline command blocks (one-click run), persistent notes, live config-state checks. The yellow banner fires when the firewall is on a config the step doesn't expect, with a one-click reset.</td>
</tr>
<tr>
<td width="340" valign="middle"><a href="docs/images/screenshot-substation-hmi.png"><img src="docs/images/screenshot-substation-hmi.png" alt="Substation process view" width="320"></a></td>
<td valign="middle"><b>Substation process view</b> at <code>/substation</code>. Live feeder state from the RTAC via OpenDSS. When a student attacks a Modbus or DNP3 endpoint, voltage, breaker position, and load energization update here within ~3 seconds. Customer-service tile makes the kinetic consequence visible.</td>
</tr>
</table>

<!--
TODO screenshots still needed - capture after a workshop run and drop into docs/images/:
  - screenshot-dashboard.png         : portal home at / (metric tiles, workshop status, quick links)
  - screenshot-terminal.png          : close-up of an embedded xterm session attached to kali-1 mid-attack
  - screenshot-containd-policy.png   : the proxied /containd/ view showing applied rules
  - screenshot-validation.png        : a lab runner step with the green/red validation chips visible
Insert into the table above when ready, with similar inline captions.
-->

## Designed for

RangerDanger is designed for instructors and security teams who need reviewable lab content, offline classroom delivery, and hands-on segmentation work that students can validate themselves.

- **Fully open and auditable.** Apache 2.0 source for the backend (Go), frontend (TypeScript), simulators, and lab content. No closed binaries, no behavior that can't be inspected in the repo. Suitable for organizations whose security posture requires source review before a tool runs on student laptops.
- **Source-controlled lab content.** Exercises live as YAML in the active package's declared scenarios directory: US uses [`lab-definitions/scenarios/`](lab-definitions/scenarios/); EU uses [`lab-definitions/packages/eu-iec104-substation/scenarios/`](lab-definitions/packages/eu-iec104-substation/scenarios/). Forkable, diff-reviewable, instructor-customizable: replace the substation scenario with a water-treatment or manufacturing variant, retune the labor-budget model in Lab 1.4, or rewrite the validators against your own state machine.
- **Offline / SSD workshop-ready.** [`stage-ssd.sh`](stage-ssd.sh) stages the bundle an offline classroom needs and proves it: every image must resolve and save, and the archived first-party tags must match the bundle's `.version`. OpenPLC is amd64-only upstream, so the arm64 archive cross-includes its amd64 image. The two architecture archives are written one after the other, so never ship the output of a run that failed. [`setup.sh --from-tarballs`](setup.sh) brings the stack up with no network. Patch tooling ([`stage-ssd-delta.sh`](stage-ssd-delta.sh)) compares image digests between releases and includes changed images. `--include <image>` is additive: it force-adds a named image whose digest was unchanged; it does not exclude changed images. A delta is small only when few images differ between the two releases (v0.1.31 to v0.1.32 had 15 changed images and measured 8.1 GB). Delta rollback parks images behind overwritten mutable tags under `:before-<new version>`; deleting those tags forfeits rollback for those images.
- **Segmentation and DPI-policy focused.** The firewall is the protagonist. Every lab walks toward a least-privilege containd policy with ICS DPI; the topology console shows policy state in real time; validators read active policy and audit log alongside simulator state.
- **Physics-backed.** Attacks have measurable kinetic consequences via the OpenDSS power-flow solver. Modbus FC5/FC6 writes and DNP3 CROB commands change breaker positions, regulator taps, voltages, and load energization within ~3 seconds, surfaced in the substation process view.
- **Native multi-architecture where the upstream supports it.** First-party images ship native `linux/amd64` and `linux/arm64` builds where available; OpenPLC remains amd64-only upstream. On Apple Silicon, use a Docker Desktop backend with working amd64 emulation enabled (Rosetta is a Docker Desktop setting); `setup.sh` does not enable it. On arm64 Linux, `setup.sh` can register a `qemu-x86_64` emulation handler via `tonistiigi/binfmt`.
- **Designed for classroom delivery.** Single-laptop-per-student with loopback-only host binding, no auth, lab-only credentials, and an explicit security model. Instructor SSD distribution handles bandwidth-constrained venues. Unless skipped, the readiness gate exits on failed firewall API-health, policy-apply, or workshop-reset checks. A backend health timeout, DPI degradation, or non-running OpenPLC only warns; the final banner can still print.

## Why RangerDanger is different

Most OT segmentation labs hand students a PDF workbook, a Wireshark capture, and an iptables-and-vibes firewall. RangerDanger replaces all three with an interactive environment where the firewall is a real ICS-aware NGFW, segmentation decisions show up immediately on the topology map, and attacks against the lab produce measurable changes in the power-flow solver.

| | Generic ICS labs | RangerDanger |
|---|---|---|
| Firewall | iptables or OPNsense | containd NGFW with Modbus / DNP3 / CIP DPI |
| Segmentation work | Static topology, paper matrix | Interactive zone map with live policy overlay, click-to-implement, live deny/allow chips per conduit |
| Attack consequence | Container state changes | Power-flow solver: voltages, currents, breaker positions, load energization update in the process view within ~3 seconds |
| Validation | Instructor walks around checking screens | Per-lab Go validators read live simulator state and audit log; green/red chips per checkpoint, no manual grading |
| Lab content | PDF workbook | Inline command runner, embedded terminals, persistent notes, dynamic content from earlier-lab choices |
| Reference material | "Look it up in the textbook" | Integrated wiki at `/knowledge` with curated substation, protocol, and segmentation articles |
| Evidence | Wireshark exports manually correlated | One-command change-board evidence: probe matrix + policy fingerprint + PCAP source analysis |
| Distribution | Bring-your-own VMs | Single `./setup.sh`, or `setup.sh --from-tarballs` for offline workshops; runs on a laptop |

## Architecture at a glance

A Next.js dashboard fronts a Go + Gin backend that orchestrates Docker containers, talks to containd over REST, and runs the scenario validators. Field-device simulators implement Modbus TCP + DNP3 TCP + REST against shared state, with OpenDSS providing feeder physics. The US package has four teaching zones plus management and physics; isolated platform-owned bridges deliver the student desktops without a management NIC. Teaching inter-zone traffic transits the containd firewall; the US RTAC-to-field path is kernel-pinned via `rtac-harden.sh` so the multi-homed RTAC can't bypass policy.

The generated overview below shows the **US package**. Services appear once in their primary teaching zone; dotted desktop links are delivery bridges. See the detailed diagrams for both packages.

<!-- BEGIN GENERATED: overview -->
```mermaid
%%{init: {"flowchart": {"nodeSpacing": 22, "rankSpacing": 45, "padding": 10}}}%%
flowchart TB
    subgraph n_overview_zone_platform_105afc123b["Platform · management<br/>10.99.99.0/24 · lan3"]
    direction TB
    n_overview_backend_82688e5de7["backend"]
    n_overview_frontend_bc72e9f36e["frontend · Next.js 16"]
    n_overview_proxy_36dc8972f0["proxy"]
    end
    subgraph n_overview_zone_field_net_a57fab3daf["Field Device Zone ·<br/>10.40.40.0/24 · lan2"]
    direction TB
    n_overview_capbank_sim_7f36449b79["capbank_sim"]
    n_overview_recloser_sim_b20cff107b["recloser_sim"]
    n_overview_regulator_sim_c735f24179["regulator_sim"]
    n_overview_relay_sim_1b5f9d8159["relay_sim"]
    n_overview_capbank_sim_7f36449b79 ~~~ n_overview_recloser_sim_b20cff107b
    n_overview_recloser_sim_b20cff107b ~~~ n_overview_regulator_sim_c735f24179
    n_overview_regulator_sim_c735f24179 ~~~ n_overview_relay_sim_1b5f9d8159
    end
    subgraph n_overview_zone_enterprise_net_6f13584f70["Enterprise Zone ·<br/>10.10.10.0/24 · wan"]
    direction TB
    n_overview_corp_ws_b3a8da2870["corp_ws"]
    n_overview_kali_ef35dbcf25["kali"]
    n_overview_corp_ws_b3a8da2870 ~~~ n_overview_kali_ef35dbcf25
    end
    subgraph n_overview_zone_vendor_net_10dbb7aebe["Vendor / Engineering Zone ·<br/>10.20.20.0/24 · dmz"]
    direction TB
    n_overview_eng_workstation_98493649dc["eng_workstation"]
    n_overview_vendor_jump_52013698df["vendor_jump"]
    n_overview_eng_workstation_98493649dc ~~~ n_overview_vendor_jump_52013698df
    end
    subgraph n_overview_zone_ot_ops_net_01097360f9["OT Operations Zone ·<br/>10.30.30.0/24 · lan1"]
    direction TB
    n_overview_fuxa_hmi_c2772b5611["fuxa_hmi"]
    n_overview_gps_sim_8b175dc219["gps_sim"]
    n_overview_historian_sim_f3a44c7589["historian_sim"]
    n_overview_openplc_9b5c6f2535["openplc"]
    n_overview_rtac_sim_96a7a5b0c6["rtac_sim"]
    n_overview_fuxa_hmi_c2772b5611 ~~~ n_overview_gps_sim_8b175dc219
    n_overview_gps_sim_8b175dc219 ~~~ n_overview_historian_sim_f3a44c7589
    n_overview_historian_sim_f3a44c7589 ~~~ n_overview_openplc_9b5c6f2535
    n_overview_openplc_9b5c6f2535 ~~~ n_overview_rtac_sim_96a7a5b0c6
    end
    subgraph n_overview_zone_physics_net_7425e81995["Physics ·<br/>10.50.50.0/24"]
    direction TB
    n_overview_opendss_sim_cbba1e0964["opendss_sim"]
    end
    n_overview_firewall_d4f06fc78a["containd · firewall"]
    n_overview_dockerd_7619ae3dee["Docker daemon"]
    n_overview_browser_65218e3e32["Browser"]
    n_overview_browser_65218e3e32 -->|"portal · loopback 8088"| n_overview_proxy_36dc8972f0
    n_overview_zone_platform_105afc123b ---|"lan3"| n_overview_firewall_d4f06fc78a
    n_overview_firewall_d4f06fc78a ---|"wan"| n_overview_zone_enterprise_net_6f13584f70
    n_overview_firewall_d4f06fc78a ---|"dmz"| n_overview_zone_vendor_net_10dbb7aebe
    n_overview_firewall_d4f06fc78a ---|"lan1"| n_overview_zone_ot_ops_net_01097360f9
    n_overview_firewall_d4f06fc78a ---|"lan2"| n_overview_zone_field_net_a57fab3daf
    n_overview_proxy_36dc8972f0 -->|"API"| n_overview_backend_82688e5de7
    n_overview_proxy_36dc8972f0 -.->|"10.98.1.0/28"| n_overview_corp_ws_b3a8da2870
    n_overview_proxy_36dc8972f0 -.->|"10.98.3.0/28"| n_overview_eng_workstation_98493649dc
    n_overview_proxy_36dc8972f0 -->|"UI"| n_overview_frontend_bc72e9f36e
    n_overview_proxy_36dc8972f0 -.->|"10.98.2.0/28"| n_overview_vendor_jump_52013698df
    %% source: backend/internal/orchestrator/orchestrator.go:New
    n_overview_backend_82688e5de7 -.->|"Docker SDK · socket"| n_overview_dockerd_7619ae3dee
    %% source: backend/internal/containd/client.go:send
    n_overview_backend_82688e5de7 -.->|"JWT REST · management"| n_overview_firewall_d4f06fc78a
    %% source: scripts/rtac-harden.sh:93
    n_overview_rtac_sim_96a7a5b0c6 -.->|"Pinned field route"| n_overview_zone_field_net_a57fab3daf
    %% source: services/rtac-sim/main.go:132
    n_overview_rtac_sim_96a7a5b0c6 -.->|"HTTP push"| n_overview_opendss_sim_cbba1e0964
    style n_overview_zone_platform_105afc123b fill:#f1f5f9,stroke:#64748b,color:#172033
    style n_overview_zone_field_net_a57fab3daf fill:#f0fdf4,stroke:#16a34a,color:#172033
    style n_overview_zone_enterprise_net_6f13584f70 fill:#fff7ed,stroke:#c2410c,color:#172033
    style n_overview_zone_vendor_net_10dbb7aebe fill:#faf5ff,stroke:#9333ea,color:#172033
    style n_overview_zone_ot_ops_net_01097360f9 fill:#f0f9ff,stroke:#0284c7,color:#172033
    style n_overview_zone_physics_net_7425e81995 fill:#fffbeb,stroke:#d97706,color:#172033
```
<!-- END GENERATED: overview -->

Full breakdown including node IPs, service interactions, the RTAC kernel-pinning compensating control, and a deeper Docker-architecture diagram: [`docs/architecture.md`](docs/architecture.md) and [`docs/architecture-diagram.md`](docs/architecture-diagram.md).

## Documentation

Start at [`docs/`](docs/) for the routed landing page. Key entries:

| | |
|---|---|
| 📚 [`docs/README.md`](docs/README.md) | Documentation landing page - routes by audience (student / instructor / developer / security reviewer) |
| 📦 [`docs/quickstart.md`](docs/quickstart.md) | Full install walkthrough including offline/SSD path |
| 💾 [`docs/workshop-ssd.md`](docs/workshop-ssd.md) | Operator runbook: SSD distribution + mid-workshop delta patches |
| 🏗 [`docs/architecture.md`](docs/architecture.md) | Zone model, node inventory, service interactions, data flow |
| 🎓 [`docs/workshop-overview.md`](docs/workshop-overview.md) | Lab-by-lab walkthrough: what's simulated and what isn't |
| 📝 [`docs/lab-authoring.md`](docs/lab-authoring.md) | How to write a workshop lab (YAML shape, fences, decisions) |
| 🔌 [`docs/api-spec.md`](docs/api-spec.md) | REST + WebSocket reference |
| 🔐 [`SECURITY.md`](SECURITY.md) | Lab security model, external-access patterns, vuln reporting |
| 🐛 [`docs/security-known-issues.md`](docs/security-known-issues.md) | Triaged govulncheck/Trivy findings with rationale |
| 🛠 [`CONTRIBUTING.md`](CONTRIBUTING.md) | Local dev setup, tests, PR conventions |
| 💬 [`SUPPORT.md`](SUPPORT.md) | Where to ask questions and what to expect |
| 📜 [`CHANGELOG.md`](CHANGELOG.md) | Per-release notes |

## Related projects

- **[containd](https://github.com/tonylturner/containd)** - The ICS-aware NGFW at the heart of the lab
- **[`dnp3go/`](dnp3go/)** - Zero-dependency Go DNP3 library used by the field-device simulators (standalone module vendored in this repo)

## License

Apache License 2.0. See [LICENSE](LICENSE).
