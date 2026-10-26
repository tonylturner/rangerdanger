**IEC 61850** is the international standard for substation automation. Where DNP3 evolved as the SCADA reporting protocol, IEC 61850 was designed as a complete substation communication architecture: device modeling, configuration files, and three different protocols for different speed requirements.

For greenfield substation builds today - especially in Europe, parts of Asia, and increasingly North American transmission - IEC 61850 is the new-build standard. The DNP3 dominance in this lab is real for *installed* distribution gear, especially in North America; for *new* projects you should expect IEC 61850 to be on the wire alongside or instead of DNP3.

### The Three Protocols in the 61850 Family

**MMS (Manufacturing Message Specification)** is the TCP-based client/server protocol that handles slower SCADA-style operations: configuration reads, status polling, control commands, file transfers, report uploads. It is roughly the IEC 61850 equivalent of DNP3 - the SCADA-master-talks-to-IED protocol - and rides on **TCP 102**.

**GOOSE (Generic Object Oriented Substation Event)** is the headline protocol. It is a **multicast Ethernet** message (not TCP, not IP - raw layer-2 multicast) used for **protection-class messaging** between IEDs. A protective relay detects a fault, sends a GOOSE message announcing "trip" to the network, and other relays receive it in **microseconds**. The whole point of GOOSE is sub-cycle protection: faster than the protection scheme can be implemented in hardwired trip circuits between relays. GOOSE rides directly on Ethernet, typically on a tagged VLAN reserved for protection traffic.

**Sampled Values (SV / IEC 61850-9-2)** is the third leg: digital streaming of analog instrument-transformer measurements (current and voltage) at high frequency (4 kHz or 4.8 kHz). It replaces the copper wires that traditionally carry CT/PT signals to protective relays with a digital multicast stream. SV is the "process bus" use case - typically deployed inside a single substation cabinet, not the wide-area network.

### Why IEC 61850 Is Hard to Attack the Same Way

GOOSE and SV are **layer-2 multicast**, not TCP. A network firewall sitting between zones at L3 (which is what containd does) does not see GOOSE messages flowing on a different VLAN's broadcast domain at all. To inspect or filter GOOSE traffic you need a layer-2 switch with VLAN-aware filtering, port mirroring, or an inline appliance - fundamentally a different architecture than the IT-style L3 firewall this lab models. The threat model for GOOSE is more about local-cabinet access, VLAN hopping, and rogue device insertion than about cross-zone routing.

The MMS protocol is L3-routable and *could* be DPI'd by a containd-class firewall the same way Modbus and DNP3 are. The IEC 61850 community has done less work standardizing DPI predicates for MMS than the DNP3 / Modbus community has done for theirs, so commercial ICS DPI engines vary in how much MMS-aware filtering they support. This is improving as IEC 61850 deployments mature.

### Why the Lab Sticks With DNP3 (For Now)

A few practical reasons:

- **Installed-base reality.** The lab is a *distribution* substation, where DNP3 is the dominant protocol on installed gear in North America. The scenarios the workshop is designed to teach (cross-zone segmentation, ICS DPI, vendor remote-access compromise) match the threat model of the installed base.
- **Docker-bridge constraints.** Simulating GOOSE realistically requires layer-2 multicast on a tagged VLAN. Docker bridge networks do not do that well - bridges are layer-2 but not VLAN-aware in the way GOOSE testing needs. A separate L2 testbed (or a Mininet/CORE-style L2 simulator) would be the right vehicle.
- **DPI lesson generalizes.** The L4 source-pin enforcement + function-code-DPI detection and logging pattern the lab teaches against DNP3 maps cleanly to MMS once an IEC 61850-aware engine is available. The L4 source pin enforces; DPI detects and logs. Students who internalize the lab's segmentation model will not have to relearn it for an MMS environment.

If your environment is IEC 61850-heavy, the gap between this lab and your reality is roughly: substitute MMS for DNP3 in the firewall rules, add a layer-2 strategy for GOOSE that this lab does not exercise, and read about process-bus architectures (SV deployment) separately. The segmentation, DPI, and zone-conduit lessons all carry over.
