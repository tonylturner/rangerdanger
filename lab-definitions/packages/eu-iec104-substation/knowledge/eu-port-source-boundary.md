The EU reference policies answer a network question: which source may open a connection to which destination and service? They do not answer the application question: which command may that source issue? Keeping those questions separate is essential when explaining what a segmentation change achieved.

### What the weak policy exposes

The weak policy permits TCP/2404 from enterprise (`wan`), vendor/engineering (`dmz`) and the Control Centre (`lan4`) into the station zone (`lan1`). Any station-zone node may also reach field Modbus. Enterprise and vendor/engineering, including Kali, vendor-jump and eng-ws, get direct field TCP/502 access through `weak-foreign-field-modbus`, the intentional teaching exposure. A workstation can attempt field reads and writes directly, or use the right IEC 104 client to become another telecontrol master rather than merely observing the legitimate master.

This is an intentional teaching baseline, not an approved production design. Do not copy it to another environment or interpret a working feed at boot as evidence of least privilege.

### What improved actually permits

The improved policy source-pins the CC at `10.60.60.20` to RTU `10.30.30.20:2404`. It permits RTU `10.30.30.20` to CB1 breaker `10.40.40.20`, RMUs `.21` and `.22`, OLTC `.23` and F2 breaker `.24` on TCP/502. F2 is indication-only in the IEC 104 point map; this is not a read-only firewall rule. The separate PLC rule permits `10.30.30.30` to OLTC `.23:502`.

The archive has two different services. CC snapshots go to `10.70.70.40:8081`; enterprise reads go to `.40:8080`. Engineering `10.20.20.20` may reach OpenPLC `10.30.30.30:8080` for maintenance. The vendor host `.10` is not interchangeable with engineering `.20`.

Other new cross-zone connections are denied by the policy's explicit denies or default deny. There is no permitted historian-initiated OT conduit. These rules are a reference design, not the only possible acceptable communications matrix.

### Reading Network console evidence

Selected-flow evidence keeps three questions separate: **Topology** says the package declares a path; **Observed liveness** reports its available observations; **Authorization** reports policy evidence. Liveness is neither authorization nor proof that a command succeeded. A `DENY` zone-pair summary is shown as denied by that summary, with its rule detail. `MIXED`, `ALLOW` or absent summaries leave **Authorization unknown**: none evaluates the exact source, destination and port or proves a source pin. Same-zone flows do not traverse containd, so firewall policy does not establish their authorization.

Use the actual rules and attempt-local evidence for the tuple under test, rather than upgrading a zone summary or a live feed into endpoint permission. The [zone/conduit matrix](#eu-zone-conduit-design) names the operational dependencies; the following limits explain why even a permitted connection is not command-level permission.

### A port is not a permission level

Telemetry, general interrogation and switching share TCP/2404. Allowing that port does not create a read-only IEC 104 session. Improved observes selected ASDU types, CA and IOAs with explicit monitor rules, but does not enforce a command allowlist. A capture that decodes C_DC_NA_1 proves observability, not command-aware blocking.

Similarly, TCP/502 can carry input-register reads and holding-register writes. The permitted PLC-to-OLTC flow is necessary for regulation but is not an FC/register restriction. This is the Network console's read/write warning: command-level filtering needs a separately designed enforcing policy; the EU IEC 104 observations are monitor-only and Modbus DPI remains planning-only. Device logic supplies further checks such as remote mode, auto/manual state and interlocks; those are different controls with different failure modes.

The historian's read-only property comes from its GET-only read listener and lack of write-back behavior, combined with network separation from its ingest service. It is not a generic property of port 8080.

### Prove both halves of the change

From Kali, attempt a new GI under weak and improved policies, preserving command output and the policy active during each attempt. Then demonstrate that legitimate CC observation and PLC regulation still work. A denied attacker connection with a broken authorized controller is not a successful operational change.

Account for [established sessions](#eu-established-sessions): a connection opened before the change may survive it. Test new sessions separately from existing ones and record the exact source/destination tuple.

Finally, document the remaining trusted-host risk. The permitted CC still opens and closes equipment. [SBO](#iec104-sbo-commands) checks sequencing, not the operator's identity or the business purpose of the command. Review [trusted-host misuse](#eu-trusted-host-misuse) before calling a source allowlist a complete authorization solution.
