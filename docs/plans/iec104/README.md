# Plan: selectable range packages and a European IEC104 workshop

Status: **approved 2026-10-09; increment 0 in progress.** Branch
`iec104`, cut from `v0.1.34` (`b653743`). `main` and the v0.1.34
release are not affected. Tony's answers are in section 5.

Evidence: four code recon reports and one external research report in
[`recon/`](recon/). Each finding there cites `file:line` at `b653743`.
This plan cites them as `[deploy]`, `[devices]`, `[curriculum]`,
`[containd]` and `[research]`. Two independent reviews critiqued the
first draft; this version includes their corrections.

## 1. Goal

- A **student** selects a **range package** with a toggle in the
  RangerDanger UI, and the range reprovisions on their machine. A
  package is one tested combination of topology, devices, protocols,
  process model, firewall policies, curriculum and verification.
- The first new package is a **European telecontrol segmentation
  workshop that uses IEC 60870-5-104 (IEC104)** in place of DNP3.
- Later packages: a vendor remote-access workshop, then a no-code
  workshop builder. Section 8 names only what the builder needs from
  the data model.
- Acceptance boundary: a new architecture that reuses supported
  capabilities is mostly composition and authored configuration. A new
  protocol, device or process still needs code.

## 2. What the code does today

The ChatGPT proposal read `v0.1.29`. We re-checked every claim at
`v0.1.34`. All 13 claims hold. The corrections and missed couplings
matter more than the confirmations:

| # | Finding at v0.1.34 | Consequence for the plan | Source |
|---|---|---|---|
| 1 | The RTAC gets field state over **HTTP**. The portal and historian read the RTAC's cache. The RTAC's DNP3 and Modbus field polls discard their results and ignore errors. | If IEC104 replaces DNP3 as is, blocking IEC104 changes nothing on screen. A new package needs a real protocol-fed operator view (3.4). | [devices] §1.1 |
| 2 | OpenDSS solves from the RTAC's cached observations. When field HTTP is blocked, the solver keeps producing fresh solves of stale breaker positions. DNP3-only loss has no effect. | The process must solve from device truth, independently of the operator path. | [devices] §1.2 |
| 3 | The RTAC's upstream DNP3 and Modbus servers are read-only. Operator control goes HTTP → RTAC → device. | An IEC104 gateway needs a real command path into device state. | [devices] §1.3 |
| 4 | Each field sim duplicates its interlocks in its HTTP, Modbus and DNP3 code. There is no shared "open breaker" operation. | New devices get one model with protocol adapters (3.4). | [devices] §5 |
| 5 | Two deployment authorities exist: Compose, and an unused SDK "lab instance" provisioner that ignores IPs, gateways and sysctls. | Compose owns package lifecycle. The SDK path stays out of scope and refuses package ranges. | [deploy] P0 |
| 6 | Scenario IDs **and** node IDs are global primary keys. The loader imports one shared `scenarios/` dir into every template. | Packages own their curriculum and get namespaced IDs (3.6). | [curriculum] §1, [deploy] P1 |
| 7 | Validation always fetches RTAC state and dispatches by seven fixed scenario IDs. Unknown IDs fall back to electrical checks. Some validators pass without evidence (plan saved; `custom` counts as hardened; nil audit counts as clean). | Validators are declared per check and selected by capability. This change ships in the same increment as ID namespacing (3.6). | [curriculum] validators |
| 8 | The frontend has 7 hardcoded `substation-segmentation` selectors, two global and two scenario-scoped browser storage keys, and a second policy generator (`remediation-to-rules.ts`). | Package-scoped queries, storage and rule catalogs. | [curriculum] frontend |
| 9 | nginx, the backend RTAC URL, event subnet filters, PCAP generators, the validation matrix and reset recipes all hardcode US names and IPs. | A package manifest must feed the backend and proxy, not only Compose. | [deploy] P1 |
| 10 | Release, SSD staging, the input guard and every smoke gate assume one Compose model. | Union image set plus per-package manifests (3.8). | [deploy] release |
| 11 | containd recognises the label `iec104` but has **no IEC104 decoder**. TCP/2404 port rules work with the existing generic L3/L4 machinery. containd also accepts `ics.protocol: iec104` silently and never evaluates it. | Milestone A (port-level segmentation) needs no containd change. EU policies must use pure L3/L4 rules. Milestone B (command-aware) is a containd project. | [containd] §1-2 |
| 12 | containd's NFQUEUE always accepts the packet that triggers a block. Only later packets drop. `events-smoke.sh` already notes this for DNP3. Established flows are accepted before L4 denies. | Blocking one IEC104 command needs a containd dataplane fix. Milestone A must test new and established sessions separately. | [containd] §1.3, §A |
| 13 | containd binds `lan4`-`lan6` through `CONTAIND_AUTO_LAN4..6_SUBNET`. | A control-centre zone is possible without containd work. | `containd/api/http/interface_autoassign.go:38-40` |

Curriculum reuse: about 70-80% of the learning structure and prose
carries over **for IEC104 on the same US feeder**. That is the upper
bound. Lab 2.3 (hardening) is 40-55%, because its central DNP3 attack
must be re-authored with real IEC104 semantics. A European feeder adds
more rewrite, and the knowledge base (`frontend/lib/knowledge-content/`)
was not counted. [curriculum] reuse table.

## 3. Target architecture

The US workshop stays exactly as it is. The new architecture is built
for the EU package. The US package adopts it later, as its own change
(decision D5).

### 3.1 Package layout

```text
lab-definitions/packages/<package-id>/
  package.yml          # id, schema version, title, capabilities, file references
  compose.source.yml   # complete, hand-authored Compose model per mode
  compose.release.yml
  compose.offline.yml
  nginx.conf
  manifest.json        # services, roles, IPs, zones, endpoints for the backend and smoke gates
  topology.yml         # the visible teaching topology
  process/             # electrical profile and DSS files
  protocols/           # point maps (CA/IOA/type) as data
  firewall/            # weak and improved policies
  scenarios/<slug>.yml
  verification.yml     # probe matrix, event checks, canary, reset recipe, capture filter
  seeds/               # OpenPLC program and FUXA project, if the package uses them
```

- The US package `us-dnp3-substation` is a `package.yml` and
  `manifest.json` that point at today's root `docker-compose*.yml`,
  `proxy/nginx.conf`, `lab-definitions/` files and `data/` roots. Those
  files do not move and do not change.
- New packages keep writable state under `data/<package-id>/`.
- No package database and no marketplace. A package is a directory.

### 3.2 Package selector, not a generator

- Each package ships **complete, hand-authored Compose files**. There
  is no Compose generation, and no profiles, `extends` or `include`.
  Profiles cannot change topology values, and merges leak US services
  into other packages. [deploy] options table.
- A lint checks each package: the manifest agrees with its Compose files
  (names, IPs, images, networks, binds); every referenced asset exists;
  IDs and IPs are unique; platforms are supported. It fails closed.
- Setup, the backend, smoke gates, SSD staging and the release guard
  read the selected package's manifest.
- A generator is deferred until a third package or the builder needs it.

### 3.3 Selection lifecycle

- **Students switch packages from a toggle in the UI** (Tony,
  2026-10-09). The instructor does not switch for them.
- The backend already holds the Docker socket and is a platform service
  on `mgmt_net` only (`docker-compose.yml:80-105`). The platform
  (proxy, frontend, backend) stays up during a switch. Only the **range
  layer** is replaced: containd, zone networks, devices, webtops.
- That needs today's root Compose model split into a platform part and
  a US range part. The split keeps every name, IP, network, port binding
  and sysctl. Unchanged US gates prove it, not byte-identical files.
- **One provisioning path.** Setup starts the platform and then the
  default range through the same mechanism the toggle uses. Which
  mechanism (the backend runs Compose for the range project, or drives
  the Engine API from the manifest) is settled by recon in increment 2a.
  Compose inside a container resolves bind paths in the container, not
  on the host, so this is not a one-line change.
- A switch is serial: check that the new package's images and assets
  are present (offline SSD installs carry the union), stop the old range
  by its recorded identity, start the new one, verify routes, policies
  and readiness. The UI shows progress and errors. Progress is stored
  per package, so switching back resumes it.
- `./setup.sh --package <id>` and `.\setup.ps1 -Package <id>` only set
  the first-start default. The default stays the US package.
- One range at a time. Concurrent ranges would need an identity
  redesign (fixed container names, loopback ports, subnets).

### 3.4 EU devices, physical truth and operator view

The EU package gets the new device model. The four US sims are not
refactored.

- New `services/devices/` with one model per EU device type: feeder
  breaker, RMU switch, OLTC. The model owns state, interlocks, local
  protection, change events and audit context `{peer, protocol, zone}`.
  Protocol adapters (Modbus, IEC104, HTTP for instructor use) call it.
- **Truth plane:** OpenDSS solves from device truth over a process
  network that students cannot block. The EU topology attaches field
  devices to that network. This is a new-package topology, so the US
  network invariants are not touched.
- **Observation path, two hops:** device → Modbus → RTU observation
  cache → IEC104 → control-centre cache → operator portal. Each value
  carries quality, source time and receipt time. A failure on either
  hop makes the values stale and shows it.
- **Command path:** operator → control centre → IEC104 → RTU → Modbus →
  device. There is no HTTP bypass. Instructor truth and reset
  interfaces are separate, and a test proves they cannot refresh the
  operator view.

### 3.5 IEC104

**Placement:**

```text
control-centre SCADA (IEC104 client)   control_centre_net (new, lan4)
        │  TCP/2404 through containd
substation RTU / gateway (IEC104 server)   station LAN
        │  Modbus TCP southbound (labelled as a simplification)
field devices (feeder breaker, RMU switches, OLTC)   field_net + process net
```

- This is the common European pattern: SCADA → IEC104 → RTU/gateway →
  IEC 61850, 101/103, Modbus or hardwired I/O inside the station.
  [research] §2. IEC 61850 and serial 101/103 are out of scope. The lab
  says so and does not put those labels on TCP traffic.
- **First profile:** double-point status `M_DP_NA_1`/`M_DP_TB_1`,
  double command `C_DC_NA_1` with select-before-execute, measured float
  `M_ME_NC_1`, general interrogation `C_IC_NA_1`, spontaneous events,
  STARTDT/STOPDT/TESTFR, t0-t3 timers, quality on link loss.
- **Stack: lib60870-C** (MZ Automation, GPLv3). Tony chose
  correctness over licence purity (D3): a reference stack, not our own
  session layer. Our IEC104 programs (RTU gateway, control-centre front
  end, student tool) link it and live in `services/iec104/` under
  GPLv3, built into their own images. They talk to the Apache-2.0
  components only over network protocols (Modbus, HTTP), so the rest of
  the repository stays Apache-2.0. Images carry the source location.
  The spike in increment 2b picks the binding: `c104` (Python, GPLv3,
  wraps lib60870-C) or C directly. A commercial lib60870 licence stays
  available if GPL ever becomes a problem.
- **Correctness gate, not only "it connects":** split and coalesced
  frames, window and ack handling, reconnect and GI, event-queue
  overflow, select expiry and value/peer matching, interlock rejection,
  feedback after execute, and a clean Wireshark decode of every frame.
- **Time:** time-tagged ASDUs (`M_DP_TB_1`, CP56Time2a) need a
  synchronized station clock. The EU package keeps `gps-sim` as the
  station clock for the RTU, and the time-spoofing lesson carries over:
  a shifted clock corrupts the sequence-of-events record. Synchrophasors
  are IEEE C37.118 / IEC 61850-90-5, not IEC104, and stay out of scope.
- **Student tools:** an `iec104cmd` CLI on the same library in the
  attacker and engineering images, and `tshark`, which decodes IOA, COT, the select bit and
  quality. [research] §4

### 3.6 Curriculum, validation and progress

Increment 1 ships these together. A review of the first draft showed
that qualified IDs (`<package>--<slug>`) would break the US gates,
workshop and terminal lookups, probes and many frontend comparisons for
no EU benefit, so the design is:

- Scenario IDs stay plain slugs, **unique across all packages**; the
  loader fails on a duplicate. Scenarios carry `package_id`. The
  template ID stays the topology ID. One active-package accessor
  replaces the hardcoded workshop template. The loader loads, validates
  and prunes per package inside one transaction; any package failure is
  fatal.
- Each scenario declares `validator: <key>`. A registry maps the key to
  Go code and the capabilities it needs. An unknown key or a missing
  capability is a load error; there is no generic fallback. US
  validator logic does not change. Declarative check kinds wait for the
  builder (§8).
- Steps get authored stable IDs. Browser keys become
  `rd:<package>:<revision>:<name>`, and progress is stored by step ID.
- `GET /api/packages` lists packages and the active one.
- Exercise IDs, title-based dispatch, requirement maps and the
  remediation catalog move from TypeScript into package metadata in
  increment 3, when EU content shows the shape.
- US students' existing browser progress resets once at this cutover
  (decision D8).

### 3.7 Firewall milestones

| Milestone | What students can do | containd work | When |
|---|---|---|---|
| **A: port-level** | Pin TCP/2404 so that only the control centre reaches the RTU. Block every other source and path for new connections. Show that telemetry survives the hardened policy. | None. Pure L3/L4 rules; a lint rejects any `ics` block in EU policies until B lands. | Increment 3 |
| **B: command-aware** | Allow monitoring ASDUs; alert on or block control ASDUs per CA/IOA. | IEC104 decoder and policy fields (about 2.8-5.2k LOC, provisional), plus dataplane fixes: first-packet verdict, TCP sequence propagation, multi-frame parsing. | Gates the first EU course (D4). containd repo, own branch |

- **Established sessions:** a session opened under the weak policy can
  survive the switch to hardened, because containd accepts established
  flows first. The lab must either tear sessions down when it applies a
  policy, or teach that policy governs new connections. Tests cover
  both cases, with a command attempt and device-state evidence.
- **Monitoring continuity under B:** containd blocks a whole flow for
  10 minutes, so dropping one command also stops telemetry. B needs a
  decision first: alert only, terminate and reconnect, or a
  protocol-aware proxy (decision D10).
- **containd tracks `:latest`.** B changes dataplane code that the US
  workshop also uses. B stays on a containd branch until the US gates
  pass against it.

### 3.8 Release, SSD and CI

- The release publishes the **union** of first-party images that all
  packages need. Shared images build once.
- The authored build inventory `.github/release-images.json` lists the
  union. The generated release asset `release-images.json` gains
  per-package membership and manifest hashes.
- SSD: still one `images-<arch>.tar` with the union. The bundle ships
  every package directory, because staging archives git HEAD and the
  package files are tracked. Setup checks that the selected package's
  images are present before start.
- CI runs smoke gates per package. US assertions stay as they are. EU
  adds its own matrix: 2404 permitted and forbidden, new and
  established sessions.

### 3.9 European process model

- OpenDSS stays the solver. The EU package sets `frequency_hz`,
  `primary_kv`, display base, voltage limits, tap step and feeder rating
  in its process profile. [devices] §7 lists every US literal.
- The EU feeder has a different shape: feeder breaker, RMUs and an OLTC
  in place of recloser, regulator and capacitor bank. The portal needs
  an EU process view and state contract. It is not a renamed US panel.
- The vertical slice starts on a minimal EU feeder: one 20 kV / 50 Hz
  feeder breaker and a flat load. Increment 4 grows it toward a
  simplified CIGRE MV benchmark with two RMUs and a primary OLTC.
- An MIT-licensed OpenDSS builder for the CIGRE MV network exists. Its
  data derives from BSD-3 CIGRE-MV-PSCAD, and the CIGRE brochure stays
  copyrighted. We keep that provenance and validate the model against
  numerical reference cases. [research] §3
- Protection is simplified, and the lab says so. Power flow does not
  model earth faults or relay timing.

## 4. Increments and exit gates

Each increment merges to `iec104` only when its gate passes. The
`v0.1.x` line keeps shipping from `main`. "US gates" means the five
smoke gates plus the Go and frontend suites, compared against the
increment-0 fixture.

| # | Increment | Exit gate |
|---|---|---|
| 0 | **Freeze the US baseline.** Record verbatim gate output, including skip lines and the containd image digest used. Reference: the v0.1.34 clean-clone run had firewall 54/54, lab-commands 69/69 (1 skipped), events 10/10, test-suite 40/40 (18 auto-passed). Fix the RTAC startup race (`services/Dockerfile:154`) and the backend's containd key mismatch (`proto`, `functionCode`); both are in the path of later work. | Fixture committed; US gates green on `iec104`. |
| 1 | **Curriculum and validation ownership.** Namespaced IDs, validator kinds, transactional per-package loading, package-scoped frontend queries and storage. No stack change. | Unit tests: every qualified US ID validates as before; wrong-package access, missing evidence, missing capability, failed import and empty-curriculum pruning all fail correctly. US gates unchanged. |
| 2 | **Package selector and IEC104 stack spike, in parallel.** (a) `package.yml`, manifest, lint, platform/range Compose split, one provisioning path, the student UI toggle, manifest consumed by backend, proxy and smoke gates. (b) Time-boxed lib60870 spike: binding (`c104` or C), the first profile of 3.5, GPL packaging. | (a) US gates unchanged; lint rejects bad manifests; a toggle round trip US → US restores a working range. (b) Spike report with the correctness checks of 3.5 and an effort estimate. |
| 3 | **EU vertical slice.** Minimal EU feeder, device model, truth plane, both observation hops, IEC104 command path, control-centre zone, milestone-A policies, one lab path. | Headless test: control centre → firewall → RTU → Modbus → breaker → OpenDSS → operator view → evidence. **Deny 2404 → the operator view goes stale with quality, OpenDSS keeps solving and local protection still trips.** New and established sessions tested. US gates unchanged. |
| 4 | **Complete EU package.** CIGRE-derived feeder, RMUs, OLTC, point maps, adapted labs and knowledge base, EU smoke matrix, release union, SSD, handout. | All EU and US gates green on Linux CI. Owner-run acceptance: clean-clone install on macOS and Windows/WSL2, plus an SSD install. |
| 5 | **containd milestone B** (containd repo, its own branch), after D10. Can start beside increment 3, with lib60870 as the traffic source. | First-command prevention proven on Linux; monitoring behaviour matches D10. US and EU gates pass against the branch before it merges. Required before the first EU course (D4). |
| 6 | **Remote-access package.** | No change to the selector, loader or manifest schema. Every new validator kind and image is listed in the package. |

Increment 3 is the first point where IEC104 runs end to end.

## 5. Decisions (Tony, 2026-10-09)

| # | Decision | Answer |
|---|---|---|
| D1 | EU process model | 20 kV / 50 Hz European feeder, grown from a minimal slice (3.9). |
| D2 | Topology | Dedicated control-centre zone on `lan4` (e.g. `10.60.60.0/24`). |
| D3 | IEC104 stack | lib60870 (GPLv3). Truthfulness first: "I would rather ship GPL code than get it wrong." (3.5) |
| D4 | First EU workshop firewall | Wait for command-aware filtering (milestone B). |
| D5 | US workshop | Unchanged until EU ships; its upgrade to the new device model is a later change. |
| D6 | EU southbound | Modbus TCP only, labelled as a simplification. |
| D7 | First EU course | Nov 7, 2026, with milestone B as its gate. Tony: "we can get it done in time." |
| D8 | US browser progress | May reset once at the increment-1 cutover. |
| D9 | EU nodes | Keep Kali, vendor jump, engineering workstation, historian, and GPS as the station clock (3.5). Replace FUXA with the first-party control-centre view; FUXA is little used today. Drop DNP3 tooling. OpenPLC stays as the station automation PLC with a seeded tap-changer voltage-control program; the RTU reads it over Modbus. |
| D10 | Monitoring under milestone B | Drop the command and reset the session; the control centre reconnects and re-runs general interrogation. |
| D11 | containd for EU courses | Track `:latest`; re-run US and EU gates before each course. |
| D12 | Who switches packages | Students, from a UI toggle that reprovisions the range (3.3). |
| D13 | Gate runs on Tony's Mac | Allowed to replace the running v0.1.34 stack with `iec104` builds; v0.1.34 is restored after each run. |

## 6. Risks

- **IEC104 integration effort.** lib60870 removes the session-layer
  risk; the spike bounds the binding and packaging work before
  increment 3 starts.
- **containd floats on `:latest`.** Milestone B changes shared dataplane
  code (3.7).
- **Schedule.** D7 is 29 days after approval and keeps milestone B as
  its gate. So milestone B, increment 1 and the increment-2 spike run in
  parallel from the start, not in sequence.
- **EU electrical credibility.** D1 and D7 decide how far the feeder
  goes before the first course.
- **GPL services.** `services/iec104/` must stay a separate program
  behind a network boundary. A lint can check that no Apache-2.0 module
  imports it.
- **Platform changes reach US code paths** in increments 1-2 (loader,
  validators, frontend storage, manifest consumers). The increment-0
  fixture and unchanged US gates control this.

## 7. Debt found during recon (current release)

The labs use OpenPLC and FUXA only as reachable endpoints. The DNP3
first-packet gap affects only an RTAC-sourced Direct Operate; the Lab
2.3 attack from Kali is stopped by L4 rules first. These items do not
change what the next US course teaches. They go on the board:

- Fresh installs ship no OpenPLC program and no FUXA project. `data/`
  is gitignored, and SSD staging archives git HEAD only.
- Validators pass without evidence (see 2, row 7).
- Generated negative-test prose says a refused connection proves a
  block, while the probe counts refusal as reachable.
- `lab-commands-smoke.sh` leaves skips out of its denominator, and
  `events-smoke.sh` can skip its DPI gates and still report all passed.
- Moved into increment 0 because later work depends on them: the RTAC
  startup race and the containd key mismatch.

## 8. What a no-code builder will need

Not designed here. The data model from increments 1-3 must give it a
strictly validated, versioned authoring schema; stable step, decision
and action IDs; typed targets and point maps; checks declared as data;
and package-scoped revisions for evidence. [curriculum] builder section.
