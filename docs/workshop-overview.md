# Workshop overview

A high-level walkthrough of the **Substation Segmentation Validation
Lab** — what students do, what the system simulates, and how the
exercises chain together. Useful for evaluators reading the repo
without running the stack.

For the technical architecture, see `architecture.md`. For the
student-facing exercise content, see `lab-definitions/scenarios/`.

## What students operate

A simulated electric distribution substation: relays, reclosers,
voltage regulators, capacitor banks, an HMI, an RTAC supervisory
controller, and a feeder physics engine — all running as Docker
containers across five network zones, with a real DPI-capable
firewall (containd) at the edge.

The lab starts in a **deliberately permissive state** — the
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
runs in the in-app exercise runner — students get inline terminals,
auto-run buttons for CLI commands, and validators that check the
substation state, captured PCAPs, and applied firewall policy.

| # | ID | Name | Time | What it teaches |
|---|---|---|---|---|
| 1 | `baseline-assessment` | Baseline Traffic Analysis | 30 min | Capture cross-zone traffic at the firewall and identify the critical conduits operations depend on. The starting point — students need to know what's *required* before deciding what's allowed. |
| 2 | `segmentation-requirements` | Segmentation Requirements & Rule Design | 20 min | Define zone-pair communication requirements and compare the student's design to the hardened reference. Establishes the framework for "least privilege but still operational." |
| 3 | `remediation-planning` | Remediation Planning Under Constraint | 25 min | A finite labor budget forces tradeoffs between firewall changes, protocol controls, and architecture work. Mirrors what real OT teams face — you can't fix everything this quarter. |
| 4 | `firewall-implementation` | Firewall Policy Implementation | 30 min | Build a least-privilege containd policy from scratch, replacing the weak baseline with enforceable rules. The hands-on translation of design into nftables-backed policy. |
| 5 | `modbus-override` | Modbus Register Override Attack | 15 min | Direct Modbus writes from the enterprise zone bypass the RTAC to manipulate voltage regulator setpoints. Demonstrates why protocol-aware filtering matters — IP-level allow/deny isn't enough. |
| 6 | `dnp3-command-injection` | DNP3 Direct Operate Command Injection | 15 min | DNP3 Direct Operate from Kali disables recloser auto-reclose, causing a permanent outage on the next fault. Shows that DNP3 needs the same kind of function-code filtering Modbus does. |
| 7 | `vendor-rdp-compromise` | Vendor Remote Access Compromise (Bonus) | 15 min | Compromised vendor credentials pivot from the DMZ to field devices via Modbus. Optional. Most relevant when remediation choices in exercise 3 left vendor access broad. |
| 8 | `capbank-switching-attack` | Capacitor Bank Switching Attack (Bonus) | 15 min | Rapid capacitor bank switching causes voltage transients and triggers a contact-wear lockout — even "passive" assets can be weaponized. Optional. |
| 9 | `validation-evidence` | Post-Change Validation & Evidence Collection | 20 min | Run positive and negative segmentation tests, capture PCAPs, and build an evidence package. Closes the loop: "I changed the policy" → "I can prove the change does what I claim." |

Total ≈ 3.5 hours of guided content; bonuses optional.

## What's simulated

The simulators are not just stub services — each implements the
protocol and behavior accurately enough that real OT tools
(`mbpoll`, `dnp3poll`, Wireshark with Modbus/DNP3 dissectors) work
against them, and a single fault, trip, or tap-change is observable
across all three protocols simultaneously:

- **Relay** — feeder breaker with trip/close, lockout, fault
  injection (DNP3 address 1)
- **Recloser** — auto-reclose with shot counting, lockout,
  disable-reclose attack surface (DNP3 address 2)
- **Regulator** — load tap changer with ±16 tap range, voltage
  regulation logic (DNP3 address 3)
- **Capacitor bank** — switched capbank with kVAR rating,
  switch-count contact wear, lockout (DNP3 address 4)
- **RTAC** — supervisory controller polling field devices via DNP3
  master + HTTP, exposing aggregated state, also a read-only DNP3
  outstation (DNP3 address 10)
- **OpenDSS feeder physics** — calculates energization and voltage
  from device states; what a real engineer sees as "the feeder is
  out" is the OpenDSS model returning de-energized
- **FUXA HMI** — operator UI talking to the RTAC over Modbus
- **OpenPLC** — substation automation PLC

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

This is intentional — the lab teaches segmentation, not power system
operations. The simulator depth is calibrated to make the *cyber*
content concrete: a DNP3 Direct Operate command produces an
observable outcome students can verify, rather than a print
statement saying "command received."

## Firewall: containd

The edge firewall is [containd](https://github.com/tonylturner/containd),
a separate project providing zone-based firewalling with nftables,
ICS DPI (Modbus/TCP function-code filtering, DNP3 protocol
awareness), IT DPI (DNS, TLS/SNI, HTTP, SSH, RDP, SMB), a web UI,
and SSH console access. Students don't write nftables rules
directly — they configure containd through its UI or CLI, and the
underlying enforcement is policy-aware.

Two reference configurations ship with the lab:

- **`substation-weak.json`** — the intentionally permissive baseline.
  Enterprise→field is allowed, which is what makes the attack
  exercises possible. Students start here.
- **`substation-improved.json`** — the target hardened state. Only
  RTAC→field is allowed for controlled flows; Modbus write
  function codes are denied from any source other than the RTAC.
  Students compare their own designs against this and validate
  remediation work against it in exercise 9.

## Single-student deployment

The intended deployment is **one student per laptop**: each student
runs the full stack locally, bound to localhost, with no auth. A
single-laptop deployment runs ~19 containers and uses 6–8 GB of RAM.
See `SECURITY.md` for the full security model and the patterns for
deliberately exposing the stack to other machines (SSH local-forward
recommended, Tailscale also works, `0.0.0.0` binding actively
discouraged).
