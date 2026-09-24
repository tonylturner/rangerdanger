import type { Section } from "../knowledge-types";

export const protocols: Section =
  /* ====== Protocols & Communication ====== */
  {
    heading: "Protocols & Communication",
    description:
      "Modbus, DNP3, IEC 61850, OPC UA, NTP. What they do, what they look like on the wire, and how the lab exercises them.",
    icon: "radio",
    accent: "violet",
    articles: [
      {
        id: "modbus-tcp",
        title: "Modbus TCP in Substations",
        body: `Modbus is one of the oldest and most widely deployed industrial communication protocols, originally developed by Modicon in 1979. Modbus TCP wraps Modbus application data units in TCP/IP packets on **port 502**. Its simplicity and ubiquity make it the default choice for communicating with PLCs, RTUs, meters, and field devices. It is also one of the least secure protocols in widespread use.

### Function Codes

Modbus uses a request-response model with function codes defining the operation:

| Code | Name | Type |
|------|------|------|
| FC01 | Read Coils | Read binary outputs |
| FC02 | Read Discrete Inputs | Read binary inputs |
| FC03 | Read Holding Registers | Read analog values |
| FC04 | Read Input Registers | Read analog inputs |
| FC05 | Write Single Coil | Write binary output |
| FC06 | Write Single Register | Write analog value |
| FC15 | Write Multiple Coils | Write binary outputs |
| FC16 | Write Multiple Registers | Write analog values |

Read operations retrieve data. Write operations change device state. In a substation context, write operations mean opening breakers, changing tap positions, or disabling protection functions.

### Zero Security by Design

:::caution Modbus TCP has no native security
**No authentication, no encryption, no authorization.** Any device that can establish a TCP connection to port 502 can read or write any register. The server has no way to distinguish a legitimate RTAC command from a malicious one sent by an attacker. Modbus was designed in 1979 for isolated serial networks where physical access was the security boundary; deploying it on routable IP networks without compensating controls is inherently dangerous.
:::

### The Compensating Control: Segmentation + DPI

The primary defense for Modbus TCP is network segmentation with protocol-aware firewalling. The containd NGFW inspects Modbus traffic at the application layer and filters by function code. The hardened policy allows FC03 (read holding registers) from the HMI to field devices but blocks FC05 and FC06 (write operations) from any source except the RTAC. This is the best available protection for a protocol with no native security.

### Lab Implementation

Every field device simulator exposes Modbus TCP on port 502:

- **Relay** (10.40.40.20): breaker position, trip count, lockout status in coils and holding registers
- **Recloser** (10.40.40.21): auto-reclose state as a Modbus coil
- **Regulator** (10.40.40.22): tap position as a Modbus holding register

You use \`mbpoll\` on the Kali box to read and write these registers. The difference between weak and hardened policies is the difference between full read/write access and being blocked at the firewall.

### See Also

- [What is ICS DPI?](#ics-dpi) - function-code filtering is the DPI lesson the lab teaches against Modbus
- [\`mbpoll\` - Modbus TCP Command-Line Tool](#tool-mbpoll) - the CLI used in the labs
- [DNP3 in Substations](#dnp3) - the protocol DNP3 plays to Modbus's role in distribution`,
      },
      {
        id: "dnp3",
        title: "DNP3 in Substations",
        body: `DNP3 (Distributed Network Protocol version 3) is the dominant SCADA protocol in North American electric utilities. Developed in the 1990s based on IEC 60870-5, it was designed specifically for utility communication: reliable delivery over unreliable links, time-stamped event reporting, and a data model that maps well to power system objects. It runs over both serial and TCP/IP, with TCP typically using **port 20000**.

### Master-Outstation Architecture

DNP3 uses a master-outstation model. The master (RTAC or SCADA system) initiates requests to outstations (field devices). Key operations:

- **Read** (FC01): retrieve data objects from the outstation
- **Direct Operate** (FC05): command an action (open breaker, change setpoint) in a single message, with the outstation returning a status response
- **Direct Operate No Ack** (FC06): the same single-message control write, but the outstation sends **no response**. Attackers favor it because it is quieter (no reply packet to notice) and because a naive function-code filter that only lists FC05 will miss it - which is exactly why a correct DPI rule must cover both FC05 and FC06
- **Select-Before-Operate** (FC03/FC04): two-step confirmation for safety-critical operations. The master selects the control point, the outstation confirms readiness, then the master issues the operate. SBO is recommended for critical controls but not universally deployed. The lab's \`dnp3cmd\` supports all three control modes (\`-sbo\` for Select-Before-Operate, \`-no-ack\` for FC06, and Direct Operate by default).

### Security (or Lack Thereof)

Standard DNP3 has no built-in authentication or encryption. DNP3 Secure Authentication (SA, IEEE 1815-2012) adds challenge-response authentication, but adoption remains limited. Most fielded implementations accept commands from any source that can establish a TCP connection and send properly formatted messages. This makes DNP3 outstations vulnerable to command injection from any host with network access to port 20000.

### Protocol Layers

DNP3 is more complex than Modbus, with three layers:

1. **Data link layer**: CRC error checking and frame synchronization
2. **Transport layer**: message reassembly for large data
3. **Application layer**: SCADA data objects organized by group and variation

This complexity makes DPI harder than Modbus, but the containd NGFW implements application-layer inspection to filter by function code.

### Lab Implementation

Each field device runs a DNP3 TCP outstation on port 20000 via the \`dnp3go\` library:

| Device | IP | Outstation Address |
|--------|----|--------------------|
| Relay | 10.40.40.20 | **1** |
| Recloser | 10.40.40.21 | **2** |
| Regulator | 10.40.40.22 | **3** |
| RTAC | 10.30.30.20 | **10** (read-only) |

You can use DNP3 tools on the Kali box to send Direct Operate commands, tripping breakers and changing tap positions. The hardened firewall policy restricts DNP3 command traffic to the RTAC only.

### See Also

- [What is ICS DPI?](#ics-dpi) - the DNP3 Direct Operate restriction (FC05 and FC06 No Ack) is what containd's DPI rule for DNP3 enforces
- [\`dnp3poll\` & \`dnp3cmd\` - DNP3 Tools](#tool-dnp3) - the CLI tools used in the labs
- [IEC 61850 and GOOSE](#iec-61850-goose) - the modern standard sometimes deployed alongside or instead of DNP3
- [The OT Kill Chain](#ot-kill-chain) - the Industroyer historical example mirrors the Lab 2.3 DNP3 attack`,
      },
      {
        id: "ntp-ot",
        title: "NTP in OT Networks",
        body: `Network Time Protocol might seem mundane in an IT context, but in OT networks, accurate time synchronization is a critical operational requirement. Protective relays, fault recorders, sequence-of-events recorders, and phasor measurement units all depend on precise timestamps. When a fault occurs on a feeder, the sequence of events (which relay tripped first, which breaker opened when, how the fault propagated) must be reconstructed with millisecond accuracy.

### Time Distribution in a Substation

Substation time synchronization typically comes from a **GPS clock** that distributes time via:

- **IRIG-B**: dedicated analog/digital time signal for sub-millisecond accuracy
- **IEEE 1588 PTP** (Precision Time Protocol): Ethernet-based, microsecond accuracy
- **NTP**: backup source for devices that do not require sub-millisecond accuracy (HMIs, historians, network equipment)

The RTAC typically acts as an NTP server for the substation LAN, distributing GPS-derived time to connected devices.

### Why Time Matters for Security

Compromising time synchronization has several effects:

- **Forensic analysis breaks down**: if relay event logs have wrong timestamps, reconstructing fault sequences becomes unreliable
- **Time-coordinated protection fails**: schemes relying on synchronized measurements (line differential, synchrophasor applications) can be disrupted
- **Audit trails become useless**: correlating events across devices requires consistent timestamps

While distribution substations rarely use time-critical protection schemes, the principle applies broadly. Accurate time is foundational to both OT operations and incident investigation.

### Segmentation for NTP

From a segmentation perspective, NTP traffic should be treated as a controlled conduit:

- The RTAC or a dedicated time server should be the **only** NTP source for OT zones
- Field devices should **not** synchronize from external sources on the enterprise network
- The firewall should permit NTP (UDP 123) only from the designated time source, blocking NTP from unauthorized sources that could inject false time`,
      },
      {
        id: "iec-61850-goose",
        title: "IEC 61850 and GOOSE (Why the Lab Doesn't Simulate Them)",
        body: `**IEC 61850** is the international standard for substation automation. Where DNP3 evolved as the SCADA reporting protocol, IEC 61850 was designed as a complete substation communication architecture: device modeling, configuration files, and three different protocols for different speed requirements.

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
- **DPI lesson generalizes.** The L4 source-pin + function-code-DPI defense pattern the lab teaches against DNP3 maps cleanly to MMS once an IEC 61850-aware engine is available. Students who internalize the lab's segmentation model will not have to relearn it for an MMS environment.

If your environment is IEC 61850-heavy, the gap between this lab and your reality is roughly: substitute MMS for DNP3 in the firewall rules, add a layer-2 strategy for GOOSE that this lab does not exercise, and read about process-bus architectures (SV deployment) separately. The segmentation, DPI, and zone-conduit lessons all carry over.`,
      },
      {
        id: "opc-ua",
        title: "OPC UA Basics",
        body: `**OPC UA** (OPC Unified Architecture) is a modern, vendor-neutral, platform-independent protocol for industrial communication, increasingly the go-to choice for **IT/OT integration**: getting OT data into MES, ERP, historians, analytics platforms, and cloud services. Where DNP3 and IEC 61850 are substation-and-utility specific, OPC UA is broader - it shows up in manufacturing, oil and gas, building automation, and water/wastewater.

### What OPC UA Replaces

**Classic OPC (sometimes called OPC DA, HDA, A&E)** was the previous generation: Windows-only, built on DCOM, painful to firewall, painful to debug, painful to secure. Anyone who has tried to traverse a firewall with DCOM has a story. OPC UA replaces all of that with a single specification that works across Windows, Linux, and embedded devices, runs over TCP (port 4840) or HTTPS, and includes security as a first-class concern rather than a bolt-on.

### The Information Model

OPC UA's distinctive feature is its **address space** - a graph of nodes with types, attributes, and references that describes the data semantically, not just by tag name. A node can be marked as a "Temperature" with units, scaling, and engineering range; another node can be a "PumpController" with predefined methods like \`Start()\` and \`Stop()\`. Clients query the address space, discover what is there, and bind to it without prior schema knowledge.

This is a sharp contrast with DNP3 and Modbus, where the meaning of "holding register 17" lives in a vendor's datasheet rather than in the protocol itself. OPC UA is self-describing.

### Where You Encounter OPC UA on an OT Network

- **Historians.** PI System, Wonderware Historian, Cogent DataHub, and most modern historians use OPC UA to collect data from controllers.
- **Modern PLCs.** Siemens S7-1500, Beckhoff TwinCAT, Rockwell ControlLogix, Schneider M580 - all expose OPC UA servers natively.
- **MES / ERP integration.** Data from the plant floor flowing up to manufacturing-execution and enterprise-resource-planning systems usually rides on OPC UA.
- **IT/OT brokers.** Cloud-bound OT data (AWS IoT SiteWise, Azure IoT Hub, GE Predix) often passes through an OPC UA aggregator.
- **Wind / solar SCADA.** Renewable-generation control rooms increasingly use OPC UA for aggregation across fleets.

### Security Built In

OPC UA has security baked into the spec rather than retrofitted:

- **TLS / certificate-based authentication.** Each client and server presents an X.509 certificate; the connection is mutually authenticated. No clear-text protocol mode is encouraged.
- **User-level access control** on individual nodes. The protocol distinguishes anonymous, username/password, and certificate-bound identities.
- **Message-level signing and encryption.** Per-message integrity protection on top of TLS.

In practice OPC UA deployments still vary widely - many devices ship with self-signed certs, expired certs, or anonymous-allowed configs, so the "security built in" is only as good as the deployment hygiene. But the protocol itself is a substantial improvement over the legacy ICS protocols.

### Why the Lab Doesn't Simulate It

A few reasons:

- The lab focuses on distribution-substation segmentation, where the predominant SCADA protocols are DNP3 and (increasingly) IEC 61850. OPC UA is more common at the IT/OT *boundary* and inside *industrial-automation* environments than inside utility distribution substations.
- Simulating a faithful OPC UA server with a meaningful address space is substantially more work than simulating a Modbus or DNP3 outstation - the protocol stack is heavier, the address-space modeling matters, and the security model needs to be authentic to teach the protocol's strengths.
- The cross-protocol firewall lessons the lab teaches (L4 + DPI defense in depth, source-pinning, segmentation by zone) all carry over to OPC UA cleanly once you map "Modbus function code" to "OPC UA service code" and "DNP3 Direct Operate" to "OPC UA Write/CallMethod."

For IT/OT integration architectures, the OPC UA server is usually positioned in a DMZ between the OT network and the IT/cloud side. Treat that DMZ with the same defense-in-depth posture the lab teaches for the field-zone conduit, layered with OPC UA's own certificate-and-user-level controls. The segmentation logic generalizes; the protocol surface is different.`,
      },
    ],
  }
;
