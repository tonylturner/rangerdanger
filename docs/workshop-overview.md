# Workshop overview

The portal selects one active package: `us-dnp3-substation` (DNP3) or
`eu-iec104-substation` (IEC 104). Each has seven package-bound exercises.
The platform stays up during range switches; switching resets range state
and firewall policy. The L4 source pin enforces; DPI detects and logs.
The timings and examples below describe the US workshop.

A high-level walkthrough of the **Substation Segmentation Validation
Lab** - what students do, what the system simulates, and how the
exercises chain together. Useful for evaluators reading the repo
without running the stack.

For the technical architecture, see `architecture.md`. For the
student-facing exercise content, see `lab-definitions/scenarios/`.

## What students operate

A simulated electric distribution substation: relays, reclosers,
voltage regulators, capacitor banks, an HMI, an RTAC supervisory
controller, and a feeder physics engine - all running as Docker
containers across five network zones, with a real DPI-capable
firewall (containd) at the edge.

The lab starts in a **deliberately permissive state** - the
"weak baseline" firewall configuration allows broad cross-zone
traffic. Students walk through:

1. observing how the system behaves under the weak baseline
2. designing what segmentation *should* look like
3. choosing what to fix first under labor constraints
4. implementing a least-privilege policy
5. exercising attacks that prove the policy works (or doesn't)
6. capturing evidence that the hardened state holds

## The exercises

Each exercise is one YAML file in its package’s scenarios directory (US: `lab-definitions/scenarios/`) and
runs in the in-app exercise runner - students get inline terminals,
auto-run buttons for CLI commands, and validators that check the
current substation state and running firewall policy. Students interpret the PCAP and sign the test record themselves.

Lab numbering follows the **DefendICS workshop deck** (sections 1.x and 2.x).

The three planning labs (1.2 / 1.3 / 1.4) are deliberately separate
*activity types*, not different cuts of the same task:

- **1.2 = OBSERVE.** Capture cross-zone traffic, run targeted probes, produce findings - what's actually present in the baseline?
- **1.3 = DECIDE design.** Turn findings into design verdicts (BLOCK / RESTRICT / ALLOW / BLOCK and LOG) and a resourcing-readiness reality check.
- **1.4 = AGREE the change.** Choose scope under a labor and per-role budget, then write the window, rule changes, acceptance tests, rollback and residual-risk owners in a change request. The coverage panel compares selected work with requirements; it does not test the firewall.

Each step's student input persists in browser localStorage and downstream labs render it via inline `:::findings-panel`, `:::notes-panel` and `:::plan-coverage` panels - see [`docs/lab-authoring.md`](lab-authoring.md) for the fence vocabulary.

Times are **estimates for the manual work, not timed walks**. The card chip and step-1 callout match each scenario. Optional depth and instructor PCAP Analysis take extra time; catch-up shortcuts do not replace observations or sign-off. Budget breaks separately. Both packages use the same activity design.

### US package

| Lab | ID | Name | Time | What it teaches |
|---|---|---|---|---|
| 1.2 | `baseline-assessment` | Baseline Traffic Analysis | ~20 min | Capture cross-zone traffic, identify required conduits and probe for latent exposure. |
| 1.3 | `segmentation-requirements` | Segmentation Requirements & Policy Design | ~10 min | Turn findings into source/zone requirements and check staffing and risk-acceptance readiness. |
| 1.4 | `remediation-planning` | Remediation Planning Under Constraint | ~30 min | Agree scope under the labor budget. Write a change request with ordered rules, window, acceptance tests, rollback, risk owners and approvals. |
| 2.2 | `firewall-implementation` | Firewall Policy Implementation | ~45 min | Author the approved request in containd: pre-change export, stage removals and replacements together, full diff, one rule-change commit, readback and named-source retest. Logging has a separate reviewed commit. Apply stays optional catch-up. |
| 2.3 | `hardening-configurations` | Protocol-Hardened Configurations | ~25 min | Run the attack and retest, restore operation, read device/CC values and record UTC time and policy identity. The L4 source pin enforces; DPI detects and logs. Monitoring is not a BLOCK control. |
| 2.3-bonus | `vendor-rdp-compromise` | Vendor Remote Access Compromise (optional) | ~25 min | Review both boundaries in containd, retest each hop independently, retain source attribution and sign the restored-state record. |
| 2.4 | `validation-evidence` | Testing & Validation | ~40 min | Write a test plan; baseline the policy and start one boundary capture; test named sources without control writes; read interface-attributed packets; check operator health; sign go/no-go. Current-state checks do not grade this hand record. |

Total ≈ 170 min for the 6 core US labs, +25 min for the optional bonus (estimates).

### EU package

The plant differs: IEC 104 CC/RTU points, RTU field polling and PLC→OLTC regulation replace the US DNP3/RTAC examples. Production-poller positives use fresh points and captured native traffic; no extra clients are installed.

| Lab | ID | Name | Time |
|---|---|---|---|
| 1.2 | `eu-baseline-assessment` | Baseline Traffic Analysis | ~15 min |
| 1.3 | `eu-segmentation-requirements` | Segmentation Requirements & Policy Design | ~12 min |
| 1.4 | `eu-remediation-planning` | Remediation Planning Under Constraint | ~30 min |
| 2.2 | `eu-firewall-implementation` | Firewall Policy Implementation | ~45 min |
| 2.3 | `eu-hardening-configurations` | Protocol-Hardened Configurations | ~25 min |
| 2.3-bonus | `eu-vendor-rdp-compromise` | Vendor Remote Access Compromise (optional) | ~25 min |
| 2.4 | `eu-validation-evidence` | Testing & Validation | ~40 min |

Total ≈ 167 min for the 6 core EU labs, +25 min for the optional bonus (estimates).

## What's simulated

The simulators are not just stub services - each implements the
protocol and behavior accurately enough that real OT tools
(`mbpoll`, `dnp3poll`, Wireshark with Modbus/DNP3 dissectors) work
against them, and a single fault, trip, or tap-change is observable
across all three protocols simultaneously:

- **Relay** - feeder breaker with trip/close, lockout, fault
  injection (DNP3 address 1)
- **Recloser** - auto-reclose with shot counting, lockout,
  disable-reclose attack surface (DNP3 address 2)
- **Regulator** - load tap changer with ±16 tap range, voltage
  regulation logic (DNP3 address 3)
- **Capacitor bank** - switched capbank with kVAR rating,
  switch-count contact wear, lockout (DNP3 address 4)
- **RTAC** - supervisory controller polling field devices via DNP3
  master + HTTP, exposing aggregated state, also a read-only DNP3
  outstation (DNP3 address 10)
- **OpenDSS feeder physics** - calculates energization and voltage
  from device states; what a real engineer sees as "the feeder is
  out" is the OpenDSS model returning de-energized
- **FUXA HMI** - operator UI talking to the RTAC over Modbus
- **OpenPLC** - substation automation PLC

Process consequence is a first-class outcome of every attack
exercise: students see the breaker open, the voltage drop, the
recloser fail to reclose. The substation panel UI surfaces these in
real time so the cyber action and the physical outcome are
inspectable side-by-side.

## What's *not* simulated

- **Hardware-in-the-loop**. Everything is software; there is no
  actual relay or RTU. The protocol behavior is real but the
  underlying device firmware is a Go program.
- **Realistic timing for hard real-time control**. The IED protective
  functions are coarse-grained (seconds, not microseconds).
- **Power system dynamics beyond the OpenDSS feeder model**. No
  generator dynamics, no transmission system, no load forecasting.
- **Multi-substation distribution networks**. One feeder, one
  substation.

This is intentional - the lab teaches segmentation, not power system
operations. The simulator depth is calibrated to make the *cyber*
content concrete: a DNP3 Direct Operate command produces an
observable outcome students can verify, rather than a print
statement saying "command received."

## Firewall: containd

The edge firewall is [containd](https://github.com/tonylturner/containd),
a separate project providing zone-based firewalling with nftables,
ICS DPI (Modbus/TCP function-code visibility, DNP3 protocol
awareness), IT DPI (DNS, TLS/SNI, HTTP, SSH, RDP, SMB), a web UI,
and SSH console access. Students don't write nftables rules
directly - they configure containd through its UI or CLI, and the
underlying enforcement is policy-aware.

Two reference configurations ship with the lab:

- **`substation-weak.json`** - the intentionally permissive baseline.
  Enterprise→field is allowed, which is what makes the attack
  exercises possible. Students start here.
- **`substation-improved.json`** - the target hardened state. Only
  the RTAC reaches field devices. The authored DPI predicates are
  Modbus FC1-6 and DNP3 FC1 (Read), not proof of function-code-level denial.
  The L4 source pin enforces; DPI detects and logs.
  Students compare their own designs against this and validate
  remediation work against it in Lab 2.4.

## Single-student deployment

The intended deployment is **one student per laptop**: each student
runs the full stack locally, bound to localhost, with no auth. A
single-laptop deployment runs ~19 containers and uses 6–8 GB of RAM.
See `SECURITY.md` for the full security model and the patterns for
deliberately exposing the stack to other machines (SSH local-forward
recommended, Tailscale also works, `0.0.0.0` binding actively
discouraged).
