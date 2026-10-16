A policy comparison should state who can initiate which exchange and what evidence establishes that boundary. A green “allowed” result is incomplete unless the required operational service still works; a red “blocked” result is incomplete unless the attempt actually came from the disallowed source during the test window. The goal is controlled availability, not indiscriminate denial.

### What weak permits

Both package policies have default DENY. The weak baseline nevertheless permits broad zone-to-zone access:

- Enterprise, Vendor/Engineering and Control Centre zones can initiate TCP/2404 toward Station.
- Every Station source can reach Field TCP/502.
- Enterprise and Vendor/Engineering sources, including Kali, vendor-jump and eng-ws, can reach Field TCP/502 through `weak-foreign-field-modbus`, the intentional teaching exposure.
- Every Control Centre-zone source can push toward Operations DMZ TCP/8081.
- Enterprise, Vendor/Engineering and Control Centre zones can read Operations DMZ TCP/8080.

These rules are broad in source and destination, not an unrestricted any-port network. The weak field TCP/502 allows carry both reads and writes; they do not filter Modbus function codes. A reachable device still applies authority and interlocks before accepting a write. Labs 2–4 use the foreign Modbus exposure for baseline reads and prescribed vendor writes, then deny those new connections under improved. Describe the actual rule rather than using “flat network” as a substitute for its meaning.

### Firewall management is a separate boundary

Under weak, any zone host can open containd's admin UI (TCP/8080 and
8443) and SSH (TCP/2222) on the firewall's addresses, including
`10.99.99.2`. Record this as a Lab 2 finding, not successful isolation.
The accepted policy disables `mgmt`, `http`, `https` and `ssh` on wan,
dmz, lan1, lan2, lan4 and lan5. This is INPUT-plane access, not a
FORWARD allow/deny rule. Management stays enabled on lan3 for
RangerDanger backend and proxy operations. Neither desktop has a lan3 leg.

The internal engine API on TCP/8081 binds only to loopback under both
policies; no zone host or desktop should reach it. Do not confuse it with
the historian ingest listener on `10.70.70.40:8081`.

### What improved pins

The improved policy permits the following named exchanges:

| Source | Destination | Purpose |
| --- | --- | --- |
| CC 10.60.60.20 | RTU 10.30.30.20:2404 | IEC 104 |
| RTU 10.30.30.20 | Field .20–.23 in 10.40.40.0/24:502 | Breaker/RMU/OLTC polling and control |
| PLC 10.30.30.30 | OLTC 10.40.40.23:502 | Automatic tap control |
| CC 10.60.60.20 | Historian 10.70.70.40:8081 | Snapshot push |
| Enterprise 10.10.10.0/24 | Historian 10.70.70.40:8080 | Read-only history |
| Engineering 10.20.20.20 | PLC 10.30.30.30:8080 | Engineering maintenance |

There is no permitted initiating path from `lan5` back toward an OT zone. Responses to an allowed connection are not new historian-initiated OT authority. Explicit unauthorized IEC 104 and Modbus denies log other attempts toward their protected destination zones; default deny covers remaining unmatched traffic.

### Prove positive and negative cases

After applying a change, establish fresh named CC operation, healthy southbound observations, PLC control and historian feed/read behaviour. Then attempt disallowed access from the actual enterprise or other test source. Preserve policy identity, source/destination tuple, times, capture or log evidence and service result. A backend management read cannot prove an enterprise rule, and a TCP handshake cannot prove successful IEC 104 command completion.

Existing sessions complicate temporal proof. Applying a deny does not itself prove an established connection stopped carrying traffic. Follow [established-session evidence](#eu-established-sessions) and use the authored session-cut step where required; do not delete unrelated conntrack entries.

### Limits of the control

The improved policy is source- and port-based, not per-IOA authorization or Modbus function filtering. Its DPI setting is disabled. An allowed CC can still send correctly sequenced commands, and an allowed Modbus source can attempt writes. [Device authority](#eu-control-authority), interlocks and trusted-host controls remain relevant.

RTU-to-PLC and RTU-to-GPS are same-subnet Station exchanges; this firewall does not enforce them as cross-zone conduits. The two package policies are reference designs, not the only valid site design or an IEC 62443 certification. Evaluate any alternative against required availability, denied initiation paths and explicitly bounded evidence rather than copying a rule count.

Implementation: package `firewall/weak.json`, `firewall/improved.json`; backend `eu_recipe.go` comparison pairs and labels.
