import type { Section } from "../knowledge-types";

export const segmentationConcepts: Section =
  /* ====== Network Segmentation Concepts ====== */
  {
    heading: "Network Segmentation Concepts",
    description:
      "Zones, conduits, defense in depth, ICS DPI, NERC CIP, IEC 62443. The conceptual framework the labs are built around.",
    icon: "shield",
    accent: "emerald",
    articles: [
      {
        id: "ot-segmentation-overview",
        title: "OT Network Segmentation Overview",
        body: `Network segmentation in OT/ICS environments means dividing the network into discrete security zones with controlled communication paths (conduits) between them. Unlike IT segmentation, which is mostly about performance and access control, OT segmentation is fundamentally about **safety and operational reliability**. A compromised device in the enterprise zone must not be able to send control commands to a protective relay in the field device zone.

### The Defense-in-Depth Model

OT networks use concentric zones of increasing criticality:

1. **Enterprise/Corporate IT** (outermost)
2. **DMZ or Vendor Access** for remote support
3. **OT Operations** with HMIs, historians, and supervisory controllers
4. **Field Devices** (innermost), containing relays, PLCs, and RTUs that directly control the physical process

Each zone boundary is enforced by a firewall that restricts traffic to only the protocols, ports, and communication pairs that are operationally necessary.

### Defining Legitimate Communication

Effective segmentation requires understanding the real communication patterns. In a substation:

- The RTAC needs to poll field devices via DNP3/Modbus
- The HMI needs to display data from the RTAC
- The engineering workstation needs occasional access to configure relays
- Vendor support needs controlled remote access

Each of these flows is a conduit that must be explicitly defined. Everything else is denied by default.

### How the Lab Demonstrates This

The lab uses identical network topologies with two different firewall policies. Under the **weak baseline**, the firewall allows broad cross-zone communication, mimicking the flat networks commonly found in real substations. Under the **hardened policy**, only the RTAC can reach field devices, and enterprise-to-OT traffic is heavily restricted.

The containd NGFW provides Deep Packet Inspection (DPI) for ICS protocols. It does not just filter by IP and port. It inspects Modbus function codes and DNP3 application layer content. This allows policies like "allow Modbus reads (FC03) from the HMI to the RTAC but block Modbus writes (FC05/FC06)" or "allow DNP3 reads but block Direct Operate commands from non-RTAC sources." That level of protocol-aware filtering is the gold standard for OT firewalls.

### See Also

- [Segmentation Approaches and Technologies](#segmentation-approaches) - the concrete tools (VLANs, routers, firewalls, DPI, diodes) that enforce these zones
- [What is ICS DPI? (and how containd does it)](#ics-dpi) - the DPI technique this article gestures at
- [Purdue Model Levels (L0–L5)](#purdue-model) - the architectural reference for where each zone sits
- [IEC 62443 Zones and Conduits](#iec-62443-zones-conduits) and [Security Levels](#iec-62443-security-levels) - the standards behind the zone vocabulary
- [Weak vs Hardened Firewall Policy](#weak-vs-hardened) - the two concrete configurations the lab compares
- [The OT Kill Chain](#ot-kill-chain) - the threat model these defenses address`,
      },
      {
        id: "segmentation-approaches",
        title: "Segmentation Approaches and Technologies",
        body: `"Segment the network" is a goal, not a mechanism. There are several ways to actually enforce a zone boundary, and they differ enormously in strength, cost, and what they can inspect. Picking the wrong tool - or assuming one tool does a job it can't - is one of the most common ways OT segmentation projects ship a paper boundary that doesn't hold. This article walks the main approaches from weakest to strongest and says where each fits.

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

This is the gold standard for the crossings that matter, and it's what **containd** does in this lab. The trade-off is cost: DPI is slower than an L4 verdict (the packet is parsed in userspace) and the rules are more work to author, so you apply it surgically - on the high-value flows (RTAC -> field) rather than everywhere.

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

The lab's hardened policy is a concrete example of stacking: VLAN-style zones *define* the boundaries, a stateful L4 rule source-pins field access to the RTAC, an L7 ICS-DPI rule restricts DNP3 Direct Operate, and host-level hardening on the multi-homed RTAC keeps it from bypassing the firewall. Each layer alone breaks the canonical attack; together they're defense in depth - which is the whole point, because any one layer can be misconfigured or fail.

### See Also

- [OT Network Segmentation Overview](#ot-segmentation-overview) - the zones-and-conduits framing these technologies enforce
- [What is ICS DPI? (and how containd does it)](#ics-dpi) - the L7 layer in depth
- [Default-Deny: Implicit vs Explicit Across Firewalls](#default-deny) - the per-vendor mechanism difference at L4
- [Purdue Model Levels (L0–L5)](#purdue-model) - where each zone sits architecturally
- [Weak vs Hardened Firewall Policy](#weak-vs-hardened) - the lab's concrete before/after`,
      },
      {
        id: "purdue-model",
        title: "Purdue Model Levels (L0–L5)",
        body: `The **Purdue Enterprise Reference Architecture (PERA)** - usually shortened to "the Purdue Model" - is the canonical *architectural* reference for how industrial control systems are layered. It originated in the early 1990s at Purdue University and was adopted by ISA-95 as the basis for modeling enterprise-control integration. When practitioners talk about "Level 3" or "L3.5 DMZ" in an OT context, they're talking about Purdue levels.

This is **not** the same thing as IEC 62443's "Security Levels." Both frameworks use the word "levels" but they answer different questions:

- **Purdue levels** answer *"where in the architecture does this asset live?"* - a hierarchy from physical process up to enterprise IT.
- **IEC 62443 Security Levels (SL 0–4)** answer *"how strong must the security controls be for this zone?"* - a threat-capability scale.

You use both together: Purdue to decide *what's in which zone*, IEC 62443 to decide *how hard to harden each zone*.

### The Levels

| Level | Name | What lives here |
|-------|------|-----------------|
| **L0** | Process | Physical process - sensors, actuators, motors, valves, the actual breaker mechanism |
| **L1** | Basic Control | Direct controllers - PLCs, RTUs, protective relays, IEDs |
| **L2** | Area Supervisory Control | Local HMIs, SCADA front-ends, alarm systems, area-level historians |
| **L3** | Site Operations | Site-wide SCADA, historian, engineering workstations, MES; the "OT operations" zone |
| **L3.5** | Industrial DMZ (IDMZ) | Proxies, jump hosts, patch staging, anti-virus distribution; the gatekeeper between OT and IT |
| **L4** | Site Business Planning & Logistics | Plant-level business systems, ERP edge, file shares |
| **L5** | Enterprise | Corporate IT, internet-facing systems, central directory, email |

### The L3.5 DMZ Matters

The Industrial DMZ at L3.5 is the single most important architectural element in modern ICS security. It's what enforces "no direct path from corporate to control." Every protocol crossing between L3 (OT operations) and L4 (business) must terminate in the IDMZ - broker patterns, reverse proxies, replicated historians - never a direct tunnel. The IDMZ is where you put the controls that catch lateral movement before it reaches the plant floor.

A flat network where corporate workstations can ping the PLC is "L4 talking directly to L1" with no L3.5 in between. That's the architectural anti-pattern this lab demonstrates with the **weak baseline** policy.

### How the RangerDanger Lab Maps to Purdue Levels

| Lab zone | Subnet | Purdue level |
|----------|--------|---|
| Field Devices | \`10.40.40.0/24\` | **L1** - relays, recloser, regulator, capacitor bank |
| OT Operations | \`10.30.30.0/24\` | **L3** - HMI, RTAC, OpenPLC, historian, GPS time server |
| Vendor / Engineering | \`10.20.20.0/24\` | **L3.5** (IDMZ) - vendor jump box, engineering workstation |
| Enterprise | \`10.10.10.0/24\` | **L4** - corporate workstation, attacker simulation |

L0 (the physical process - the actual breaker mechanism, the feeder conductor, the connected load) is the OpenDSS simulation engine that consumes commands from the relays/recloser/regulator and computes the resulting voltages and currents. L2 (area supervisory) is collapsed into L3 for the lab - small distribution substations often don't have a distinct local HMI tier. L5 (enterprise) sits outside the lab - represented only by the idea that the corporate workstation could be reaching out to the internet.

### Why Both Frameworks at Once

When you produce evidence for a change board:

1. **Purdue** answers *"is your design layered correctly?"* - does every cross-tier flow terminate in the L3.5 DMZ? Are there any L4-to-L1 direct paths that bypass L3?
2. **IEC 62443** answers *"is the security strength per zone proportional to the risk?"* - is your L1 conduit enforcing SL-3 controls (protocol-aware filtering, source restriction, logging) or just port filtering?

A correct design needs both. A flat network can be technically "secured" with strong controls on every host (high SL-A everywhere) but still violate Purdue layering because there's no architectural firebreak. Conversely, a beautifully Purdue-layered network with no controls at any layer fails IEC 62443 SL assessment.

### A Common Mistake

Treating L3 and L3.5 as the same thing. Many real deployments put the vendor jump box directly in the OT operations subnet - that collapses the IDMZ and turns vendor remote access into a direct path into L3. The hardened policy in this lab fixes that by restricting which protocols and source IPs can traverse the L3.5→L3 boundary, even though the network architecture still has them as adjacent subnets.`,
      },
      {
        id: "iec-62443-zones-conduits",
        title: "IEC 62443 Zones and Conduits",
        body: `IEC 62443 is the international standard series for Industrial Automation and Control System (IACS) security. Part 3-2 defines **zones and conduits** as the foundational framework for OT network segmentation.

### Core Definitions

- A **zone** is a grouping of logical or physical assets that share common security requirements.
- A **conduit** is the communication pathway connecting zones. It must provide the security functions necessary to protect the connected zones.

Each zone gets a target Security Level (SL-T) based on risk assessment. The conduits connecting zones must enforce the higher SL of the two zones they connect.

### Applying Zones to This Lab

The lab zones map directly to network segments:

| Zone | Subnet | Purpose |
|------|--------|---------|
| Enterprise | 10.10.10.0/24 | Corporate IT, Kali attacker |
| Vendor | 10.20.20.0/24 | Remote support, engineering |
| OT Operations | 10.30.30.0/24 | HMI, RTAC, PLC |
| Field Devices | 10.40.40.0/24 | Relays, reclosers, regulators |

The containd firewall interfaces define zone boundaries. Each firewall rule that permits traffic between zones is a conduit that must be documented, justified, and controlled.

### Why This Model Works

The zone and conduit model forces explicit decision-making about every cross-zone communication path. Instead of allowing broad network access and hoping application-level controls are sufficient, you must justify each conduit by operational need and restrict it to the minimum necessary protocols and data flows. This dramatically reduces the attack surface available to an adversary who gains a foothold in any single zone.

### A Common Mistake

One frequent error in IEC 62443 implementations is defining zones too broadly. If the entire OT network is a single zone, segmentation provides no protection against lateral movement within OT. The lab demonstrates this with the split between \`ot_ops_net\` and \`field_net\`. Even though both are "OT," they have different security requirements. Field devices directly control the physical process and need tighter protection than the HMI and RTAC.`,
      },
      {
        id: "iec-62443-security-levels",
        title: "IEC 62443 Security Levels Explained",
        body: `IEC 62443 defines four Security Levels (SLs) based on the capability of the threat actor a zone must defend against.

### The Four Levels

| Level | Threat | Example |
|-------|--------|---------|
| **SL-1** | Casual or coincidental violation | Accidental access, misconfiguration, untargeted malware |
| **SL-2** | Intentional violation with simple means | Disgruntled employee, attacker using public tools |
| **SL-3** | Sophisticated attack with moderate resources | Skilled attacker with ICS-specific knowledge |
| **SL-4** | State-sponsored with extended resources | Nation-state actor with deep domain expertise |

### Three Types of SL Designation

The standard distinguishes between:

- **SL-T (target)**: the desired security level from risk assessment
- **SL-C (capability)**: the level a system or component can actually provide
- **SL-A (achieved)**: the actual measured level in the deployed environment

The goal is to configure systems so SL-A meets or exceeds SL-T for every zone. When SL-A falls short, the gap is residual risk that needs compensating controls or explicit risk acceptance.

### Assigning Levels in Practice

For each zone, you consider:

1. **Consequences of compromise** (safety, environmental, financial, operational)
2. **Threat landscape** (who targets this zone and what capabilities they have)
3. **Existing vulnerabilities** in the deployed systems

A distribution substation field device zone might get SL-T of 3 because compromise could affect public safety (overcurrent protection disabled), sophisticated actors target utilities, and the devices run unauthenticated protocols like Modbus.

### How This Connects to the Lab

The conduit connecting two zones must enforce the higher SL. If the enterprise zone is SL-1 and the field device zone is SL-3, the firewall rules between them must enforce SL-3 controls. That means more than simple port filtering. You need protocol-aware inspection, logging, and potentially application-layer authentication.

In the lab, the **weak policy** provides roughly SL-1 conduit security: traffic flows freely with no protocol inspection. The **hardened policy** approaches SL-3: cross-zone traffic is restricted to specific source-destination pairs, Modbus function codes are filtered, and DNP3 command traffic is limited to the RTAC.`,
      },
      {
        id: "nerc-cip-segmentation",
        title: "NERC CIP and Segmentation",
        body: `NERC CIP (North American Electric Reliability Corporation Critical Infrastructure Protection) is the mandatory cybersecurity standard for the bulk electric system in North America. Distribution substations are not always subject to NERC CIP (it depends on voltage level and system impact), but the CIP framework provides segmentation concepts that apply broadly to any utility OT environment.

### CIP-005: Electronic Security Perimeters

CIP-005 is the standard most directly relevant to segmentation. It requires:

- **CIP-005 R1**: All applicable BES Cyber Systems must reside within a defined Electronic Security Perimeter (ESP), with all external routable communication through identified Electronic Access Points (EAPs)
- **CIP-005 R2**: Each EAP must permit only known and necessary communication. Everything else is denied by default.

### Key Terminology

- **ESP** (Electronic Security Perimeter): the logical boundary around BES Cyber Systems
- **EAP** (Electronic Access Point): any device controlling traffic at the ESP boundary (firewall interface, router ACL)
- **ERC** (External Routable Connectivity): any routable IP communication crossing the ESP boundary
- **IRA** (Interactive Remote Access): user-initiated sessions crossing the ESP, requiring encryption, MFA, and an intermediate system

### Mapping to the Lab

In the RangerDanger lab, the containd NGFW functions as the EAP for all zone boundaries. The \`ot_ops_net\` and \`field_net\` together sit inside the ESP - with the field devices (relays, reclosers, regulators) as its most critical core - while \`enterprise_net\` and \`vendor_net\` sit outside it. The \`vendor_net\` simulates the vendor remote access path, which in a CIP-compliant environment requires IRA controls. (See [ESP, EAP, and ERC Explained](#esp-eap-erc) for the same boundary broken down per acronym.)

The exercises demonstrate why CIP-005 R2 (deny by default) is essential. The weak baseline violates this principle by allowing broad access. The hardened policy enforces it.

### Beyond Mandatory Compliance

Even for utilities not subject to NERC CIP, the framework provides an excellent segmentation template. Defining security perimeters, identifying all access points, and maintaining explicit allow-lists for cross-perimeter communication are universal best practices. The lab exercises build hands-on experience with these concepts so you understand both why the controls matter and what happens when they are absent.`,
      },
      {
        id: "esp-eap-erc",
        title: "ESP, EAP, and ERC Explained",
        body: `These three acronyms from NERC CIP-005 define the building blocks of OT network perimeter security. Understanding them concretely helps you map abstract compliance requirements to real network architecture.

### ESP (Electronic Security Perimeter)

The ESP is the logical boundary enclosing network segments that contain BES Cyber Systems. Think of it as drawing a line on a network diagram around all devices that control or monitor the physical power system. Every IP address inside that boundary must be protected according to CIP requirements.

In this lab, the \`field_net\` (10.40.40.0/24) and \`ot_ops_net\` (10.30.30.0/24) together form the ESP, containing the relay, recloser, regulator, RTAC, HMI, and PLC.

### EAP (Electronic Access Point)

An EAP is any device or interface that controls traffic crossing the ESP boundary. In practice, these are firewall interfaces, router ACLs, or managed switches with access control. Each EAP must enforce an explicit policy permitting only documented, necessary communication.

In this lab, the containd NGFW interfaces on \`ot_ops_net\` (eth2/lan1) and \`field_net\` (eth3/lan2) are the EAPs. The firewall rules on these interfaces determine what enters or leaves the ESP.

### ERC (External Routable Connectivity)

ERC is any IP-routable communication crossing the ESP boundary. This includes automated data flows (SCADA polling, historian transfers) and interactive sessions (operator access, engineering configuration, vendor support). CIP requires all ERC to be documented with EAPs permitting only the documented flows.

In this lab, \`enterprise_net\` (10.10.10.0/24) and \`vendor_net\` (10.20.20.0/24) are outside the ESP. Any traffic from these networks into \`ot_ops_net\` or \`field_net\` is ERC.

### IRA (Interactive Remote Access)

IRA is a specific type of ERC where a human initiates a session crossing the ESP. This is the highest-risk form because it involves real-time interactive control from outside the perimeter. CIP-005 requires IRA to use an **Intermediate System** (jump host) that terminates the external session and initiates a new internal one.

The \`vendor_jumpbox\` at 10.20.20.10 serves exactly this function. Vendor personnel connect to the jump box, then access devices inside the ESP from there.

### The Lab Demonstration

Under the weak config, the ESP boundary is effectively unenforced. The Kali box at 10.10.10.50 can send Modbus commands directly to the relay at 10.40.40.20. Under the hardened config, the EAP policies on containd block this, forcing all field device communication through the RTAC. That is the difference between an ESP that exists on paper and one that actually works.`,
      },
      {
        id: "ics-dpi",
        title: "What is ICS DPI? (and how containd does it)",
        body: `**Deep Packet Inspection (DPI)** is the firewall reading *inside* the TCP payload - past the source IP, port, and TCP flags that L4 firewalls inspect - to make decisions based on the application-layer protocol. **ICS DPI** is DPI specifically for industrial protocols: Modbus, DNP3, IEC 61850, CIP, S7Comm, OPC UA, BACnet, and the dozen-or-so others that show up on OT networks.

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
2. **Userspace DPI** via NFQUEUE - for packets matching a rule that has \`dpiMode: enforce\` set, the kernel hands the packet up to containd's userspace process via the Linux netfilter queue. containd parses the payload, evaluates the ICS predicate (e.g., "is this Modbus function code in the allowed list \`[1, 3, 4]\`?"), and verdicts the packet back to the kernel: ACCEPT or DROP.

The DPI verdict path is slower than pure-L4 verdicts (microseconds vs nanoseconds), so the policy is structured to use DPI only where it adds value - the **rtac-to-field-modbus** rule in \`substation-improved.json\` is the canonical example: allow only the RTAC's source IP at L4, *and* allow only function codes 1–6 at the DPI layer. Two layers of defense in depth on the same flow.

### How to See It Working in the Lab

When a student in Lab 2.3 fires an unauthorized Modbus FC5 write from kali, the packet hits containd. At L4, the hardened policy already denies enterprise → field on TCP/502 - so the packet is dropped at the kernel layer and the student sees a TCP timeout, not a Modbus error. Watch the [Live DPI Events strip](/console) on the Segmentation drawer; you should see a row with \`category: l4\`, \`verdict: DENY\`, and the matching rule ID.

When that same write is sent from inside an *allowed* flow (the eng-ws → RTAC monitoring path, for example), the L4 rule lets it through but the DPI verdict drops the write because FC5 is not in the allow list. The event row shows \`category: ics\`, \`verdict: DENY\`, and the specific function code that was rejected. That contrast - L4-only DENY vs ICS-DPI DENY - is the lesson the lab is built around.

### The Limit of ICS DPI

DPI cannot save you from a *compromised RTAC* sending legitimate-looking Modbus writes from its authorized source IP. If the attacker is the RTAC, the firewall sees authorized traffic.

:::warning DPI is not a substitute for host hardening
This is why the lab pairs containd's DPI with the kernel-level RTAC routing pin (\`scripts/rtac-harden.sh\`) - the RTAC cannot bridge zones, and the firewall enforces what it is. DPI plus L4 plus host-level hardening together is the defense-in-depth story. **DPI alone is not.**
:::

### See Also

- [How containd Enforces Policy (Kernel-Level View)](#how-containd-enforces-policy) - the nftables + NFQUEUE plumbing the DPI path uses
- [Reading the Live DPI Events Strip](#live-dpi-events-strip) - how to interpret L4 vs ICS verdict rows during an exercise
- [Modbus TCP in Substations](#modbus-tcp) and [DNP3 in Substations](#dnp3) - the protocols this DPI inspects
- [Weak vs Hardened Firewall Policy](#weak-vs-hardened) - the configurations the lab uses to demonstrate L4 + DPI together`,
      },
      {
        id: "default-deny",
        title: "Default-Deny: Implicit vs Explicit Across Firewalls",
        body: `**Default-deny** is the bedrock rule of network segmentation: anything not explicitly allowed is dropped. It's how you make "least privilege" enforceable instead of aspirational. But the *mechanism* for default-deny varies a lot across firewalls - and getting it wrong on an OT network can either (a) leave you wide open, or (b) take down a substation. Worth understanding which model your firewall uses.

### Two Models

**Implicit default-deny.** The policy engine appends a "deny everything not matched" verdict at the bottom of every rule list automatically. You write only ALLOW rules; anything that falls off the end is dropped. This is how most modern NGFWs work.

**Explicit default-deny.** Nothing is appended for you. You must either (a) configure the chain/policy default to DENY/DROP, or (b) write a final deny rule yourself. If you forget, the default is to *allow* and your policy is the inverse of what you intended. This is how iptables/nftables, raw Linux kernel netfilter, some Linux bridge setups, and a couple of older perimeter firewalls work.

The failure mode for explicit default-deny is silent: you build out your allow rules, you do not write a final deny, traffic appears to be working - but it is working because *all* traffic is allowed, not because your rules are correct.

### How the Vendors Handle It

| Firewall | Default-deny model | Notes |
|---|---|---|
| **containd** (this lab) | Implicit (default = DENY) | Policy has a top-level \`defaultAction: DENY\` field, already set out of the box. Adding broad ALLOW rules over the top creates a "weak" posture; deleting them re-exposes the implicit DENY. |
| **Cisco ASA / FTD** | Implicit | Every ACL has an implicit \`deny any any\` at the end. Best practice still adds an explicit \`deny ip any any log\` for visibility. |
| **Palo Alto NGFW** | Implicit (configurable) | Has two predefined rules at the bottom: \`intrazone-default\` (allow) and \`interzone-default\` (deny). Cannot delete them; can clone and modify (e.g., to add logging). |
| **Fortinet FortiGate** | Implicit | Implicit deny at the end of every policy table per VDOM. \`set match-vip enable\` and a final explicit deny is a common audit-friendly pattern. |
| **Juniper SRX** | Implicit | Implicit deny at the end of every from-zone/to-zone policy list. |
| **Check Point** | Implicit BUT best practice is explicit | Has an implicit cleanup rule but it does not log. Almost every Check Point install adds an explicit final \`cleanup rule\` of \`any any drop log\` so denies are visible. |
| **pfSense / OPNsense** | Mixed | Default-deny on WAN, default-allow on LAN (via auto-generated rules). Easy to misconfigure on multi-interface designs. |
| **iptables / nftables (raw)** | Explicit | Chains default to ACCEPT. You must run \`iptables -P FORWARD DROP\` (set the chain policy) or end every chain with an explicit drop rule. Forget either, and the firewall is allow-all. |
| **Linux bridge / OVS** | Explicit | Same problem class as raw iptables. Bridges pass traffic unless explicitly filtered. |

The thing students consistently get wrong on engagements is conflating the *models*: assuming a Cisco-style implicit deny is also there on their iptables jump host, or assuming a Palo Alto-style "interzone-default" rule exists on a fresh Linux bridge. The rule of thumb: if you cannot point to *where* the default-deny is - either as an implicit policy engine behavior with vendor documentation, or as an explicit rule you wrote yourself - assume there is no default-deny.

### Why This Matters in OT

In enterprise IT, the worst case of an unintended default-allow is data exposure or a security finding. In OT, it can be a kinetic event. A firewall sitting in front of a substation that *should* be denying enterprise → field Modbus, but is silently allowing it because someone forgot the explicit drop rule, leaves the recloser and the substation breaker reachable from an attacker's laptop. That is the configuration that the canonical Lab 2.3 attack exploits.

The flip side is also painful: a misconfigured default-deny that drops the RTAC's poll traffic takes the SCADA view of the substation offline. Operators lose telemetry. If protective relays cannot reach the historian, alarm correlation breaks. So OT segmentation work is always: get the default-deny right *first*, then carefully open the narrow set of flows the substation actually needs, then verify both halves (allowed flows work, denied flows do not) before signing off.

### How to Audit It

Three checks that catch most default-deny misconfigurations:

1. **Where is the implicit deny documented?** If it is vendor-implicit, link to the vendor's docs in your change record. If it is explicit, point at the rule by ID or line number.
2. **What does a packet for an undocumented flow do?** Generate one and check the firewall log. If the log is silent, the firewall is probably allowing - implicit-deny vendors typically don't log implicit denies unless you turn it on (Palo Alto's interzone-default rule, for example, does not log by default).
3. **What happens when you remove the last allow rule?** On a properly configured default-deny firewall, removing an allow rule should break the corresponding flow. If everything still works, the firewall is either bypassing the policy entirely (NAT/bridge misconfig) or has a hidden allow-all rule somewhere.

### What containd Does

containd's policy schema has a top-level \`defaultAction\` field on the firewall object. In every shipped configuration - including the deliberately-weak \`substation-weak.json\` - this is set to \`"DENY"\`. The weak baseline is "permissive" only because it adds broad cross-zone ALLOW rules above the default. The hardened policy keeps the same \`defaultAction: DENY\` and trims the allow list to the six flows the substation requires. The actual transition from weak to hardened is *removing rules*, not *changing the default*. That is the model the labs are built on.`,
      },
      {
        id: "vendor-remote-access",
        title: "Vendor Remote Access Patterns",
        body: `Vendor remote access is the persistent thorn in OT security: utility staff cannot keep up with every vendor-specific protective relay, SCADA system, and PLC controller, so vendors need *some* path into the OT network to support their gear. How you let them in defines a large chunk of your attack surface.

### The Common Patterns

**1. Direct VPN.** Each vendor gets a site-to-site or remote-access VPN into the OT network, typically landing in a vendor zone. Easy to set up, hard to scope. The vendor's whole engineering team can usually reach more than they need to. Lab 2.3-bonus simulates this pattern.

**2. Vendor jump host.** A dedicated server in a DMZ that the vendor logs into (typically via RDP, SSH, or Citrix), runs the vendor-specific tools on, and uses to reach the OT devices. Better than direct VPN because the surface from the vendor's network to your DMZ is narrow (one host, a few protocols), and you can monitor the jump host's outbound to OT. This is what the lab's \`vendor-jump\` node models.

**3. Privileged Access Management (PAM) broker.** A specialized appliance - BeyondTrust Privileged Remote Access, CyberArk PSM, Cyolo, Claroty Secure Remote Access, Dispel, Xage, or similar - that the vendor authenticates against. The broker enforces just-in-time access (the path opens for a scheduled window and closes after), MFA, session recording, and click-stream auditing. This is the modern utility standard for high-criticality access.

**4. Zero-Trust Network Access (ZTNA).** An access broker (Zscaler ZPA, Cloudflare Access, Tailscale, Twingate) that builds a per-application tunnel from the vendor's identity to the specific service they need - without exposing the network. The vendor's laptop talks to the broker, the broker talks to the target device, and the network between them stays invisible. Newer, IT-centric, increasingly adopted on the OT/IT boundary.

### Where Vendor Access Usually Fails

The audit findings on real utility engagements tend to cluster in a few areas:

- **Scope creep.** A vendor needs access to "their" devices, so they get access to the OT-Ops VLAN - and from there can reach things that are not theirs.
- **Shared credentials.** "\`vendor\` / \`vendor\`" on the jump host, used by ten people across two organizations. No way to attribute an action to a specific human.
- **No session recording.** The vendor connects, does something to a relay, disconnects. The audit trail is "vendor logged in at 14:03, logged out at 14:42." That is not enough to investigate an incident.
- **Persistent access.** The vendor's path is always open instead of opened on demand for a maintenance window.
- **East-west blindness.** The firewall watches enterprise-to-DMZ but not DMZ-to-OT. A compromised vendor jump host can pivot freely into OT.

### How the Lab Demonstrates This

Lab 2.3-bonus models the persistent-access plus east-west-blindness failure modes together. The vendor's RDP path is always open from the enterprise zone (the attacker exploits this with stolen credentials), and from the vendor jump host the Modbus path to the field zone is also open. The attacker laundered through the vendor session never appears to be the attacker - the field device sees the command coming from the vendor's trusted IP. The hardened policy closes both links of the kill chain: enterprise → vendor RDP is denied at the perimeter, and vendor → field Modbus is denied at the DMZ-to-Field conduit. Either one alone breaks the attack; together is defense in depth.

The PAM-broker and ZTNA patterns are *not* simulated in the lab - they would require more infrastructure than fits in a docker compose - but the same defensive lesson generalizes: scope each access path narrowly, log who did what, and assume any path you do not actively constrain will be exploited.`,
      },
    ],
  }
;
