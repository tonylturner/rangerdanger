**EU scope — IEC 61850 is background only; this package does not simulate IEC 61850 devices or MMS, GOOSE or Sampled Values traffic.** The concepts below help compare station-automation designs; they are not claims about an available endpoint, exercise or firewall capability in this range.

IEC 61850 is a family of standards for substation automation, not one wire protocol. It combines information models and engineering configuration with communications suited to different tasks. Keep its three commonly discussed services distinct: MMS, GOOSE and Sampled Values (SV).

### Three communication patterns

**MMS (Manufacturing Message Specification)** provides client/server services for information exchange with intelligent electronic devices (IEDs), including reports, reads and controls. IEC 61850-8-1 maps MMS onto TCP/IP using ISO-on-TCP; TCP port 102 is the conventional endpoint. That makes an MMS connection routable in an IP design, unlike base Ethernet GOOSE and SV. A port number alone does not establish which application is present or what actions an allowed client may perform.

**GOOSE (Generic Object Oriented Substation Event)** distributes configured event and state information between IEDs, often for protection and interlocking schemes. Base GOOSE is carried directly in Ethernet frames using multicast, rather than as a TCP or UDP application flow. Its purpose is time-critical local coordination, but no single latency figure describes every deployment: the engineered performance target and achieved delivery depend on the profile, devices and network design.

**Sampled Values (SV)** carries streams of sampled current and voltage measurements, commonly between instrument-transformer interfaces and protection or measurement IEDs on a process bus. Base SV also uses Ethernet multicast. Sampling and frame rates depend on the selected profile and application; a single rate should not be assumed for every installation.

Substation Configuration Language (SCL) files describe IED capabilities and aspects of the engineered system and communications configuration. They help engineers align device and network configuration, but a configuration file is not evidence that a live message was delivered or that a security boundary is effective.

### Layer 2 and routed boundaries

Base GOOSE and SV belong to an Ethernet Layer-2 switching domain; they are not ordinary IP packets that an IP router forwards between subnets. VLANs can help organize or isolate those domains, but a VLAN label alone is not proof of a secure design. Switch configuration, membership, port access, redundancy, multicast handling and monitoring all affect the actual boundary. Routed extensions such as routable GOOSE or SV are separate designs and must not be presumed present.

MMS, by contrast, can cross an IP-routed boundary when the network permits it. A firewall can apply network rules to that flow; application-aware inspection or command-aware enforcement depends on a particular product's supported features and configuration. Do not infer it from the IEC 61850 label or from seeing decoded packets.

### Compare with this range

The EU package models the Control Centre's northbound IEC 104 connection to the RTU and the RTU's southbound Modbus TCP polling; it does not model IEC 61850. See the [telecontrol architecture](#eu-telecontrol-architecture), [IEC 104 wire model](#iec104-wire-model) and [southbound Modbus map](#eu-southbound-modbus) for those implemented paths.

The reference firewall policy disables DPI and enforces source/port boundaries at Layer 4. This is a deliberate design choice, not missing evidence: the range can demonstrate its configured network-level decisions for modeled flows, but it cannot demonstrate IEC 61850 inspection or filtering. In particular, the package does not enforce MMS object or command predicates, nor does it inspect or filter GOOSE/SV frames. Its DPI-off policy says nothing about whether a different, suitably designed system could support application-aware controls. See the [policy boundary](#eu-port-source-boundary) for what the EU rules do and do not establish.
