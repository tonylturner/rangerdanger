**Deep Packet Inspection (DPI)** is the firewall reading *inside* the TCP payload - past the source IP, port, and TCP flags that L4 firewalls inspect - to make decisions based on the application-layer protocol. **ICS DPI** is DPI specifically for industrial protocols: Modbus, DNP3, IEC 61850, CIP, S7Comm, OPC UA, BACnet, and the dozen-or-so others that show up on OT networks.

### Why L4 Filtering Is Not Enough

A traditional L4 firewall sees a Modbus connection as "TCP from 10.30.30.20 to 10.40.40.20, port 502, established." It cannot tell whether that connection is reading a holding register or writing a setpoint that opens a breaker. The Modbus protocol uses **function codes (FC)** to distinguish these operations - FC1/FC3/FC4 are reads, FC5/FC6/FC15/FC16 are writes - but the function code lives in the payload, not in the TCP header.

So an L4 "allow port 502" rule is a yes/no on the whole conversation. It cannot separate the legitimate poll traffic (FC3 reads from the RTAC every 3 seconds) from a malicious FC5 write that opens a breaker. To distinguish them you have to look inside the packet.

### What ICS DPI Actually Filters

Modern ICS DPI engines inspect:

- **Modbus**: function code (read vs write, single-coil vs multiple-register), unit ID, register addresses, written values
- **DNP3**: function code (read, direct operate, write, freeze), object groups, CROB op codes, internal indications
- **IEC 61850**: GOOSE multicast source MAC, dataset, app ID, sequence numbers
- **CIP/Ethernet/IP**: service codes, class IDs, instance/attribute paths (used in Allen-Bradley, Rockwell environments)

For each, the engine maintains a per-flow state machine that tracks the protocol conversation and can apply policy on individual transactions, not just the TCP connection.

### How containd Implements ICS DPI

containd uses a two-layer architecture for DPI:

1. **Kernel-side L4 filtering** via nftables - fast, decides allow/deny on the TCP 5-tuple based on the active policy. Most packets are accepted or denied here without ever leaving the kernel.
2. **Userspace DPI** via NFQUEUE - for packets matching a rule that has `dpiMode: enforce` set, the kernel hands the packet up to containd's userspace process via the Linux netfilter queue. containd parses the payload, evaluates the ICS predicate (e.g., "is this Modbus function code in the allowed list `[1, 3, 4]`?"), and verdicts the packet back to the kernel: ACCEPT or DROP.

The DPI verdict path is slower than pure-L4 verdicts (microseconds vs nanoseconds), so the policy is structured to use DPI only where it adds value - the **rtac-to-field-modbus** rule in `substation-improved.json` is the canonical example: allow only the RTAC's source IP at L4, *and* allow only function codes 1–6 at the DPI layer. Two layers of defense in depth on the same flow.

### How to See It Working in the Lab

When a student in Lab 2.3 fires an unauthorized Modbus FC5 write from kali, the packet hits containd. At L4, the hardened policy already denies enterprise → field on TCP/502 - so the packet is dropped at the kernel layer and the student sees a TCP timeout, not a Modbus error. Watch the [Live DPI Events strip](/console) on the Segmentation drawer; you should see a row with `category: l4`, `verdict: DENY`, and the matching rule ID.

When that same write is sent from inside an *allowed* flow (the eng-ws → RTAC monitoring path, for example), the L4 rule lets it through but the DPI verdict drops the write because FC5 is not in the allow list. The event row shows `category: ics`, `verdict: DENY`, and the specific function code that was rejected. That contrast - L4-only DENY vs ICS-DPI DENY - is the lesson the lab is built around.

### The Limit of ICS DPI

DPI cannot save you from a *compromised RTAC* sending legitimate-looking Modbus writes from its authorized source IP. If the attacker is the RTAC, the firewall sees authorized traffic.

:::warning DPI is not a substitute for host hardening
This is why the lab pairs containd's DPI with the kernel-level RTAC routing pin (`scripts/rtac-harden.sh`) - the RTAC cannot bridge zones, and the firewall enforces what it is. DPI plus L4 plus host-level hardening together is the defense-in-depth story. **DPI alone is not.**
:::

### See Also

- [How containd Enforces Policy (Kernel-Level View)](#how-containd-enforces-policy) - the nftables + NFQUEUE plumbing the DPI path uses
- [Reading the Live DPI Events Strip](#live-dpi-events-strip) - how to interpret L4 vs ICS verdict rows during an exercise
- [Modbus TCP in Substations](#modbus-tcp) and [DNP3 in Substations](#dnp3) - the protocols this DPI inspects
- [Weak vs Hardened Firewall Policy](#weak-vs-hardened) - the configurations the lab uses to demonstrate L4 + DPI together