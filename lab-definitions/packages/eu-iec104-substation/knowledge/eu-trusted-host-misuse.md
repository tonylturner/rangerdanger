A source allowlist answers “which host may connect?” It cannot decide whether that host is healthy, whether a human intended a command or whether a legitimate-looking operation is appropriate for the electrical conditions. A compromised authorized host is therefore a different threat from an unauthorized workstation.

### Three trusted paths in this range

**The Control Centre.** Improved policy permits `10.60.60.20` to RTU `10.30.30.20:2404`. That flow carries observation and switching. If the CC is misused, source pinning does not distinguish its harmful command from its authorized command. The package has no command-aware IEC 104 filtering by ASDU, CA or IOA.

**The tap PLC.** OpenPLC `10.30.30.30` reaches OLTC `10.40.40.23:502`. The normal program requests adjacent taps after a sustained voltage deviation. The network rule does not constrain the session to one register or function code; device-side logic supplies the automatic-request checks.

**The engineering workstation.** Engineering `10.20.20.20` reaches the PLC's maintenance web interface. Even without direct field access, it reaches a controller with downstream authority. A maintenance conduit is therefore part of the control-system trust chain.

### Distinguish independent controls

SBO checks selection and execution sequencing. Remote/local mode decides whether the field device accepts remote requests. Auto/manual separates PLC requests from CC manual steps. Live-live, earth and fault-current interlocks restrict operations in particular conditions.

These controls reduce different risks. None proves that the source host is uncompromised. A valid command to open an otherwise healthy feeder can still be operationally harmful; not every undesirable action violates an electrical interlock.

Likewise, the PLC's arming handshake prevents retained-request and restart races. It is not cryptographic authentication of the PLC. Avoid describing a robust control handshake as an attacker-proof authorization protocol.

### What the historical cases support

[Dragos's CRASHOVERRIDE analysis](https://nsarchive.gwu.edu/sites/default/files/documents/3869008/Dragos-CRASHOVERRIDE-Analyzing-the-Threat-to.pdf) warns against relying on architecture or a protocol swap alone once an intruder is present. [ESET's Industroyer2 report](https://www.welivesecurity.com/2022/04/12/industroyer2-industroyer-reloaded/) describes replacing legitimate communications activity with configured IEC 104 behavior.

The teaching inference is that network restrictions should be paired with endpoint integrity, command oversight, maintenance accountability and recovery planning. This range does not implement a full identity system, host detection stack or production remote-access service, so those are design considerations rather than demonstrated controls.

The historian limits another trust path by accepting CC pushes and exposing separate reads, without polling inward or writing back. That reduces the authority available through an archive compromise. It does not make the allowed CC's snapshots inherently truthful: a consumer still depends on the integrity of the observation source and must preserve its quality and provenance.

### Detect and respond without losing the process

Baseline the normal masters, destinations, command rates, IOAs and maintenance changes. Investigate new connections, command bursts, repeated negative confirmations, unexpected mode changes and loss of the legitimate communications process. A packet pattern is a lead, not proof of compromise; correlate with command and device records.

For the lab, compare the allowed CC command with the denied new Kali attempt. Record that the same source rule cannot prove the CC's intent. Do not add offensive host-compromise steps to demonstrate this limitation.

If a trusted controller becomes suspect, containment must account for local protection, voltage regulation and restoration needs. Use the [change plan](#eu-change-resource-plan) to name decision-makers and fallback conditions. A broad shutdown may remove malicious access while also removing the operator's safest recovery path.
