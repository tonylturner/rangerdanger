"Segment the network" is a goal, not a mechanism. There are several ways to actually enforce a zone boundary, and they differ enormously in strength, cost, and what they can inspect. Picking the wrong tool - or assuming one tool does a job it can't - is one of the most common ways OT segmentation projects ship a paper boundary that doesn't hold. This article walks the main approaches from weakest to strongest and says where each fits.

### The mental model: separation vs. inspection

Every segmentation technology does one or both of two jobs:

- **Separation** - keeping traffic in zone A from physically or logically reaching zone B at all (different broadcast domains, different IP subnets, no route between them).
- **Inspection** - when traffic *is* allowed to cross, deciding whether a specific flow is permitted, based on addresses, ports, or (best) the application protocol inside.

Weak approaches do only coarse separation. Strong approaches add fine-grained, protocol-aware inspection at the crossing. Defense in depth uses several layers so a failure of one doesn't expose the whole zone.

### Physical / air gap

The strongest separation: no network path exists between zones at all. Historically the OT "default," and still used for the most safety-critical systems (some protection and safety-instrumented systems). The catch is that true air gaps are rare in practice - the moment someone needs to pull historian data, patch a relay, or let a vendor in, a path appears (USB, a laptop, a "temporary" cable) and the gap is gone. Air gaps also provide *no* inspection: if the gap is bridged, there's nothing watching. Treat "air gapped" claims with suspicion and verify.

### Switches and VLANs (Layer 2)

A **switch** forwards Ethernet frames within a single broadcast domain. A **VLAN** (802.1Q) lets one physical switch carry several logically separate Layer-2 networks - traffic on VLAN 10 can't see VLAN 20 without something routing between them. VLANs are cheap, ubiquitous, and great for *organizing* hosts into zones.

But VLANs are **separation without inspection**, and they're a weak boundary on their own:

- They stop accidental cross-talk, not a determined attacker. **VLAN hopping** (double-tagging, switch-spoofing) and misconfigured trunk ports defeat them.
- A switch makes *no* decision about whether a flow is allowed - it just keeps VLANs apart until a router or firewall joins them.
- They carry no protocol awareness at all: a switch cannot tell a Modbus read from a Modbus write.

VLANs are a building block, not a security control. In OT they're best used to *define* zones that a firewall then polices - never as the only thing between enterprise and a relay.

### Routers and ACLs (Layer 3)

A **router** moves IP packets between subnets - it's what lets VLAN 10 talk to VLAN 20 when you want them to. A router **ACL** (access control list) filters that traffic by source/destination IP, protocol, and port. This is real inspection, and it's a genuine step up from a bare VLAN.

Its limits:

- ACLs are **stateless** by default (classic IOS ACLs) - they evaluate each packet in isolation, so you often have to hand-write return-path rules, which is error-prone.
- They inspect only L3/L4 (addresses and ports). An ACL can allow "TCP/502 from the RTAC" but **cannot see** whether that Modbus session is a read or a write that opens a breaker.
- ACL management drifts: long lists accrete, ordering bugs creep in, and "temporary" permits become permanent.

Router ACLs are fine for coarse zone-pair rules and are common as a second layer, but they can't make protocol-aware decisions.

### Stateful firewalls (Layer 4)

A **stateful firewall** tracks connection state (the TCP handshake, established sessions) so it can allow return traffic automatically and reject packets that don't belong to a known flow. This is the workhorse of network segmentation - source/destination zones, IPs, ports, with connection awareness and default-deny.

A stateful firewall is the right tool for the **separation + L4 inspection** job, and it's where most real OT segmentation lives. But by itself it still only sees addresses and ports. "Allow the RTAC to reach field devices on TCP/20000" is as specific as it gets - it cannot distinguish a DNP3 integrity poll (read) from a Direct Operate (a control that trips a breaker), because both ride the same port.

### NGFW with ICS deep packet inspection (Layer 7)

A **next-generation firewall (NGFW)** adds **deep packet inspection**: it reads *inside* the payload and decides based on the application protocol. For OT, **ICS DPI** understands Modbus function codes, DNP3 function codes and object groups, and the like - so a rule can say "allow Modbus reads (FC 1-4) from the HMI, but only the RTAC may write (FC 5/6)" or "allow DNP3 reads but block Direct Operate from anyone but the RTAC."

In this workshop, **the L4 source pin enforces; DPI detects and logs**. Do not confuse general NGFW capabilities with demonstrated lab enforcement. The trade-off is cost: DPI is slower than an L4 verdict (the packet is parsed in userspace) and the rules are more work to author, so you apply it surgically - on the high-value flows (RTAC -> field) rather than everywhere.

### Unidirectional gateways (data diodes)

A **data diode** enforces one-way flow in hardware - light across a fiber with a transmitter on one side and a receiver on the other, physically incapable of sending data back. Common for getting historian/telemetry data *out* of a high-criticality zone (field -> corporate) with zero risk of a command coming back *in*. Extremely strong for the one-way case, useless when you genuinely need bidirectional control traffic (which most supervisory polling is). A specialist tool, not a general firewall replacement.

### Host-based and microsegmentation

Everything above polices traffic *between* zones. Some risk is *inside* a zone - a compromised HMI talking to a peer HMI never crosses the firewall. **Host-based firewalls** (rules on the endpoint) and **microsegmentation** (identity- or workload-based policy, often enforced by an agent or hypervisor) push enforcement down to the host so intra-zone lateral movement is also controlled. In OT this is constrained by what the devices can run - you can't put an agent on a 20-year-old relay - so it's often applied to the Windows/Linux hosts (HMI, historian, engineering workstation) while the embedded field gear is protected by the network boundary plus host-level routing controls.

This is exactly why the lab keeps a control even for the RTAC, which is multi-homed into the field network: host-level hardening (forwarding disabled, policy routing through the firewall) forces its field traffic back through containd instead of bypassing the boundary - a compensating control where a pure network rule can't reach.

### Putting it together: defense in depth

No single layer is sufficient, and the strong ones are expensive, so real designs stack them:

| Layer | Technology | What it enforces | What it can't do |
|---|---|---|---|
| L2 | Switch / VLAN | Keep zones in separate broadcast domains | No inspection; defeated by VLAN hopping |
| L3 | Router / ACL | Filter by IP + port (stateless) | No connection state, no payload awareness |
| L4 | Stateful firewall | Zone/IP/port rules + connection state + default-deny | Can't tell a read from a write |
| L7 | NGFW + ICS DPI | Protocol-aware: function codes, object groups | Slower, more rules to maintain - apply surgically |
| Host | Host firewall / microseg | Intra-zone + lateral movement | Limited on embedded field devices |
| Physical | Data diode / air gap | Hardware-enforced one-way / no path | One-way only / brittle in practice |

The lab's hardened policy combines source-pinned L4 field access with host routing hardening and protocol observations. Verify the deployed result with the prescribed checks; a planned action or a detection alone does not prove enforcement.

### See Also

- [OT Network Segmentation Overview](#ot-segmentation-overview) - the zones-and-conduits framing these technologies enforce
- [What is ICS DPI? (and how containd does it)](#ics-dpi) - the L7 layer in depth
- [Default-Deny: Implicit vs Explicit Across Firewalls](#default-deny) - the per-vendor mechanism difference at L4
- [Purdue Model Levels (L0–L5)](#purdue-model) - where each zone sits architecturally
- [Weak vs Hardened Firewall Policy](#weak-vs-hardened) - the lab's concrete before/after