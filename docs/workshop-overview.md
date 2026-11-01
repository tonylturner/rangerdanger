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

Each exercise is one YAML file in `lab-definitions/scenarios/` and
runs in the in-app exercise runner - students get inline terminals,
auto-run buttons for CLI commands, and validators that check the
substation state, captured PCAPs, and applied firewall policy.

Lab numbering follows the **DefendICS workshop deck** (sections 1.x and 2.x).

The three planning labs (1.2 / 1.3 / 1.4) are deliberately separate
*activity types*, not different cuts of the same task:

- **1.2 = OBSERVE.** Capture cross-zone traffic, run targeted probes, produce findings - what's actually present in the baseline?
- **1.3 = DECIDE design.** Turn findings into design verdicts (BLOCK / RESTRICT / ALLOW / BLOCK and LOG) and a resourcing-readiness reality check.
- **1.4 = DECIDE implementation.** Pick remediation actions under a labor + per-role budget; the design verdicts drive coverage; readiness reshapes role caps, effort and action eligibility. Use default budget compares against the unconstrained model.

Each step's student input persists in browser localStorage and downstream labs render it via inline `:::findings-panel` and `:::plan-coverage` panels - see [`docs/lab-authoring.md`](lab-authoring.md) for the fence vocabulary.

Times below are the **Guided-path golden-path** estimates (the in-app card chip and step-1 "Time budget" callout) - Advanced-track hints and optional bonus activities run longer. They match the `estimated_minutes` field in each scenario YAML.

| Lab | ID | Name | Time | What it teaches |
|---|---|---|---|---|
| 1.2 | `baseline-assessment` | Baseline Traffic Analysis | ~20 min | Capture cross-zone traffic + identify the critical conduits operations depend on. Step 6 commits five passive-observation findings; step 7 runs active probes that reveal latent exposure passive monitoring missed. |
| 1.3 | `segmentation-requirements` | Segmentation Requirements & Policy Design | ~10 min | Translate Lab 1.2 findings into design verdicts (BLOCK / RESTRICT / ALLOW / BLOCK and LOG) per risk category, then commit a resourcing-readiness reality check (OT engineering capacity, firewall admin skills, vendor change windows, risk-acceptance authority). Closes with concrete success criteria for the hardened state. |
| 1.4 | `remediation-planning` | Remediation Planning Under Constraint | ~12 min | A finite labor budget with per-role capacity caps forces tradeoffs between firewall changes, protocol controls, and architecture work. The design verdicts from 1.3 drive requirement badges and coverage; readiness adjusts role caps and effort and excludes blocked actions from draft coverage and Apply Your Plan without deleting saved choices. Use default budget restores the unconstrained model; already-applied provenance is unchanged. Apply Your Plan in Lab 2.2 compiles and applies eligible selections; deferred controls leave their Weak exposures open. Saving selections alone does not apply policy. |
| 2.2 | `firewall-implementation` | Firewall Policy Implementation | ~22 min | Review the compiled rules and Apply Your Plan, or choose Apply Hardened for the reference policy. Advanced-track manual policies are judged against Hardened; validation separates applied-plan proof from Hardened acceptance. |
| 2.3 | `hardening-configurations` | Protocol-Hardened Configurations | ~12 min | Stress-test the policy against a DNP3 Direct Operate injection against the recloser (the canonical distribution-substation attack vector). The L4 source pin enforces; DPI detects and logs. Students verify enforcement by device state (auto-reclose still enabled), not by policy labels. Optional Modbus FC5/FC6 sidebar attacks are closed by the same source pin. |
| 2.3-bonus | `vendor-rdp-compromise` | Vendor Remote Access Compromise *(optional)* | ~12 min | RDP/VNC pivot from enterprise → vendor → field. Hardening blocks both links of the kill chain (perimeter + second hop). Optional, time permitting. |
| 2.4 | `validation-evidence` | Testing & Validation | ~7 min | Generate a change-board validation report in one click (positive/negative test matrix + PCAP evidence, the same backend report exposed by `scripts/validation-report.sh`), read it like a reviewer, and reflect on residual risk. The report is a TCP reachability matrix - no row sends a control write - so it complements Lab 2.3's device-state check rather than replacing it. The `:::plan-coverage` panel surfaces, in real time, which Lab 1.3 requirements your Lab 1.4 plan addressed vs deferred. The by-hand evidence-assembly steps remain available as optional Advanced hints. |

Total ≈ 83 min for the 6 core labs (Guided path), +12 min for the bonus.

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
