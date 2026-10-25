Network segmentation in OT/ICS environments means dividing the network into discrete security zones with controlled communication paths (conduits) between them. Unlike IT segmentation, which is mostly about performance and access control, OT segmentation is fundamentally about **safety and operational reliability**. A compromised device in the enterprise zone must not be able to send control commands to a protective relay in the field device zone.

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

The containd NGFW decodes Modbus and DNP3 operations for protocol context. **The L4 source pin enforces; DPI detects and logs.** Verify the running policy with connectivity tests and device-state evidence.

### See Also

- [Segmentation Approaches and Technologies](#segmentation-approaches) - the concrete tools (VLANs, routers, firewalls, DPI, diodes) that enforce these zones
- [What is ICS DPI? (and how containd does it)](#ics-dpi) - the DPI technique this article gestures at
- [Purdue Model Levels (L0–L5)](#purdue-model) - the architectural reference for where each zone sits
- [IEC 62443 Zones and Conduits](#iec-62443-zones-conduits) and [Security Levels](#iec-62443-security-levels) - the standards behind the zone vocabulary
- [Weak vs Hardened Firewall Policy](#weak-vs-hardened) - the two concrete configurations the lab compares
- [The OT Kill Chain](#ot-kill-chain) - the threat model these defenses address