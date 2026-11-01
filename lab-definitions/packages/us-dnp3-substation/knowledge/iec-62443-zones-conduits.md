IEC 62443 is the international standard series for Industrial Automation and Control System (IACS) security. Part 3-2 defines **zones and conduits** as the foundational framework for OT network segmentation.

### Core Definitions

- A **zone** is a grouping of logical or physical assets that share common security requirements.
- A **conduit** is the communication pathway connecting zones. It must provide the security functions necessary to protect the connected zones.

Each zone gets a target Security Level (SL-T) based on risk assessment. Conduit requirements are derived from risk assessment and the security needs of the connected zones, not automatically from the higher of two zone labels. A firewall policy alone is not an IEC 62443 SL assessment.

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

One frequent error in IEC 62443 implementations is defining zones too broadly. If the entire OT network is a single zone, segmentation provides no protection against lateral movement within OT. The lab demonstrates this with the split between `ot_ops_net` and `field_net`. Even though both are "OT," they have different security requirements. Field devices directly control the physical process and need tighter protection than the HMI and RTAC.