The **Purdue Enterprise Reference Architecture (PERA)** - usually shortened to "the Purdue Model" - is the canonical *architectural* reference for how industrial control systems are layered. It originated in the early 1990s at Purdue University and was adopted by ISA-95 as the basis for modeling enterprise-control integration. When practitioners talk about "Level 3" or "L3.5 DMZ" in an OT context, they're talking about Purdue levels.

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
| Field Devices | `10.40.40.0/24` | **L1** - relays, recloser, regulator, capacitor bank |
| OT Operations | `10.30.30.0/24` | **L3** - HMI, RTAC, OpenPLC, historian, GPS time server |
| Vendor / Engineering | `10.20.20.0/24` | **L3.5** (IDMZ) - vendor jump box, engineering workstation |
| Enterprise | `10.10.10.0/24` | **L4** - corporate workstation, attacker simulation |

L0 (the physical process - the actual breaker mechanism, the feeder conductor, the connected load) is the OpenDSS simulation engine that consumes commands from the relays/recloser/regulator and computes the resulting voltages and currents. L2 (area supervisory) is collapsed into L3 for the lab - small distribution substations often don't have a distinct local HMI tier. L5 (enterprise) sits outside the lab - represented only by the idea that the corporate workstation could be reaching out to the internet.

### Why Both Frameworks at Once

When you produce evidence for a change board:

1. **Purdue** answers *"is your design layered correctly?"* - does every cross-tier flow terminate in the L3.5 DMZ? Are there any L4-to-L1 direct paths that bypass L3?
2. **IEC 62443** answers *"is the security strength per zone proportional to the risk?"* - is your L1 conduit enforcing SL-3 controls (protocol-aware filtering, source restriction, logging) or just port filtering?

A correct design needs both. A flat network can be technically "secured" with strong controls on every host (high SL-A everywhere) but still violate Purdue layering because there's no architectural firebreak. Conversely, a beautifully Purdue-layered network with no controls at any layer fails IEC 62443 SL assessment.

### A Common Mistake

Treating L3 and L3.5 as the same thing. Many real deployments put the vendor jump box directly in the OT operations subnet - that collapses the IDMZ and turns vendor remote access into a direct path into L3. The hardened policy in this lab fixes that by restricting which protocols and source IPs can traverse the L3.5→L3 boundary, even though the network architecture still has them as adjacent subnets.