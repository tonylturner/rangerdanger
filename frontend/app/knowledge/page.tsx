"use client";

import { useState, useMemo } from "react";
import { ChevronDown, ChevronRight, Search, BookOpen } from "lucide-react";
import Markdown from "marked-react";

/* ------------------------------------------------------------------ */
/*  Article data                                                       */
/* ------------------------------------------------------------------ */

interface Article {
  id: string;
  title: string;
  body: string; // markdown content
}

interface Section {
  heading: string;
  articles: Article[];
}

const sections: Section[] = [
  /* ====== Substation Equipment & Operations ====== */
  {
    heading: "Substation Equipment & Operations",
    articles: [
      {
        id: "distribution-substation",
        title: "What is a Distribution Substation?",
        body: `A distribution substation converts high-voltage bulk power (69 kV to 345 kV) down to the distribution-level voltages (4 kV to 34.5 kV) that actually serve homes and businesses. Transmission lines come in, power transformers step the voltage down, and distribution feeders radiate outward to deliver electricity through additional step-down transformers closer to the customer.

### Key Components

Inside a typical distribution substation you will find:

- **Power transformers** that perform the voltage conversion
- **Circuit breakers** that interrupt fault current
- **Disconnect switches** for isolation during maintenance
- **Protective relays** that monitor current, voltage, and frequency
- **Voltage regulators** and **capacitor banks** for power quality
- **A control house** containing the RTAC/RTU and communication equipment

The protective relay system is the backbone of substation safety. It continuously monitors electrical conditions and automatically trips breakers to isolate faulted sections. Getting this protection coordination right is critical. If the wrong breaker trips, or if a breaker fails to trip, you get equipment damage or cascading outages.

### Automation and Communication

Modern substations are heavily automated. An RTAC or RTU talks to every relay, recloser, and regulator over serial or Ethernet links using **DNP3** and **Modbus**. The RTAC reports status upstream to the utility SCADA system and receives operator commands for switching, voltage control, and fault isolation. This communication network is a high-value target for attackers because compromising it gives direct access to breaker controls, protection settings, and voltage regulation.

### How This Maps to the Lab

In the RangerDanger lab, the substation model includes:

- An **RTAC** at 10.30.30.20
- A **protective relay** at 10.40.40.20
- A **recloser** at 10.40.40.21
- A **voltage regulator** at 10.40.40.22
- A **physics simulation engine** at 10.50.50.20

These components communicate over Modbus TCP (port 502) and DNP3 TCP (port 20000), reproducing the traffic patterns you would see on a real substation LAN. The containd NGFW enforces zone boundaries between enterprise, vendor, OT operations, and field device networks, just as a properly segmented utility would deploy them.`,
      },
      {
        id: "rtac",
        title: "RTAC (Real-Time Automation Controller)",
        body: `The RTAC is the central supervisory node in a distribution substation. Built by companies like SEL (Schweitzer Engineering Laboratories), it aggregates data from every Intelligent Electronic Device (IED) in the substation: protective relays, reclosers, voltage regulators, and capacitor bank controllers. It is the single point of communication between the substation and the utility SCADA/EMS system.

### What the RTAC Does

The RTAC polls field devices using DNP3 and Modbus at regular intervals, collecting:

- **Analog measurements**: current, voltage, power, tap position
- **Binary status**: breaker open/closed, relay tripped, lockout active

It stores this data internally and forwards it upstream to the control center via DNP3, IEC 61850, or other protocols. When an operator issues a command (open a breaker, raise a tap, enable reclose), the RTAC translates it into the device-level protocol and sends it to the target IED.

### Why the RTAC Matters for Security

In a properly segmented network, the RTAC is the **only** device authorized to communicate across the OT operations zone and the field device zone. This is a deliberate design choice. Rather than letting the HMI or engineering workstation talk directly to field devices, all command traffic goes through the RTAC. This creates a natural chokepoint where firewall rules, protocol filtering, and audit logging can all be applied.

> If an attacker compromises the HMI, they should not be able to reach field devices directly. The firewall should only permit RTAC-to-field traffic.

### Lab Implementation

The RTAC simulator (\`rtac-sim\`) runs at **10.30.30.20** on the \`ot_ops_net\`. It polls the relay, recloser, and regulator over HTTP and aggregates their state. It also talks to the OpenDSS physics engine on the \`physics_net\` to incorporate feeder electrical state.

The RTAC is **multi-homed**, connecting to:

1. \`ot_ops_net\` (10.30.30.0/24)
2. \`field_net\` (10.40.40.0/24)
3. \`physics_net\` (10.50.50.0/24)

Under the hardened firewall policy, the RTAC is the only device permitted to send Modbus or DNP3 traffic into the \`field_net\` zone.`,
      },
      {
        id: "protective-relays",
        title: "Protective Relays",
        body: `Protective relays detect abnormal electrical conditions and trip circuit breakers to isolate faulted sections of the power system. Without them, a short circuit on a distribution feeder would produce sustained fault current that destroys transformers, burns conductors, and endangers workers and the public.

### How Relays Work

Modern microprocessor-based relays (SEL-751, SEL-351, GE Multilin 750) pack multiple protection functions into a single device. A typical feeder relay implements:

- **Overcurrent protection** (50/51 elements)
- **Reclosing logic**
- **Undervoltage** (27) and **overvoltage** (59)
- **Frequency elements** (81)

The relay continuously samples current and voltage waveforms, runs protection algorithms, and makes trip/close decisions in milliseconds. When a fault is detected, the relay asserts its trip output, which energizes the trip coil on the circuit breaker to open it.

### Why Relay Compromise is Dangerous

Unauthorized access to a protective relay is one of the highest-impact attacks in a substation. An attacker who can talk to the relay over Modbus or DNP3 could:

- **Disable protection elements**, preventing the relay from tripping on faults
- **Change pickup settings**, so faults go undetected
- **Directly command the breaker** to open (causing an outage) or close onto a faulted line (causing arc flash or equipment destruction)

Disabling overcurrent protection and then creating a fault condition means sustained fault current with no automatic interruption. That causes real physical damage.

### Lab Implementation

The relay simulator (\`relay-sim\`) runs at **10.40.40.20** on \`field_net\`. It models a feeder breaker with trip/close control, lockout state, and fault injection. It exposes state over three interfaces:

| Protocol | Port | Details |
|----------|------|---------|
| HTTP REST | 8080 | \`GET /api/state\`, \`POST /api/command\` |
| Modbus TCP | 502 | FC01/FC03/FC05/FC06 |
| DNP3 TCP | 20000 | Outstation address **1** |

Under the weak firewall policy, any host on any zone can send Modbus write commands to the relay. Under the hardened policy, only the RTAC at 10.30.30.20 can.`,
      },
      {
        id: "reclosers",
        title: "Reclosers",
        body: `Reclosers are automatic circuit-interrupting devices installed at mid-feeder locations or at the head of lateral taps. Their defining feature is **auto-reclose**: after tripping on a fault, they automatically re-energize the circuit. This matters because 70-80% of distribution faults are transient (tree branches, animal contacts, lightning) and clear themselves once the line is briefly de-energized.

### Shot Sequence

A typical recloser uses a programmed shot sequence. For example:

1. **Fast trip** on initial fault, reclose after short delay
2. If fault persists, **delayed trip** and reclose again
3. After exhausting reclose attempts, **lockout** (stays open, requires manual reset)

This balances service continuity (clearing transient faults automatically) with safety (not repeatedly re-energizing a permanent fault).

### The Stealth Attack: Disabling Auto-Reclose

Disabling the auto-reclose function is a particularly insidious attack because **nothing visibly changes**. The recloser keeps operating normally until the next fault occurs. At that point, it trips and stays open permanently instead of automatically restoring service.

Every transient fault becomes a sustained outage requiring a crew dispatch. If an attacker disables auto-reclose on multiple devices across a feeder during a storm, the result is widespread prolonged outages that overwhelm the utility's restoration process.

### Lab Implementation

The recloser simulator (\`recloser-sim\`) runs at **10.40.40.21** on \`field_net\`. It models auto-reclose with shot counting and lockout behavior. The \`reclose-enabled\` flag can be toggled via:

- **Modbus**: coil write (FC05) on port 502
- **DNP3**: Direct Operate command to outstation address **2** on port 20000

One of the lab exercises demonstrates how an attacker on the enterprise network (10.10.10.50) can reach through the weak firewall policy to disable auto-reclose. The change has no immediate observable effect but degrades grid resilience for the next fault event.`,
      },
      {
        id: "voltage-regulators",
        title: "Voltage Regulators",
        body: `Voltage regulators maintain distribution feeder voltage within acceptable limits as load varies throughout the day. The standard is ANSI C84.1 Range A: 114-126 V on a 120 V base. They work by adjusting a tap position on an autotransformer, with each tap step representing about 0.625% voltage change.

### Tap Range and Operation

A standard 32-step regulator uses positions **-16 to +16**, giving plus or minus 10% regulation range. The controller continuously monitors load-side voltage and compares it to a setpoint. When voltage drifts outside the bandwidth for longer than a configurable time delay, the controller initiates a tap change.

Each tap change involves mechanical switching under load, which causes a brief transient. Tap changers have a finite mechanical life (hundreds of thousands of operations), so unnecessary tap changes are a maintenance concern.

### Attack Scenarios

Unauthorized tap manipulation can cause real harm:

- **Driving to extreme position** (+16 or -16) creates overvoltage or undervoltage on the feeder
- **Overvoltage** damages customer equipment, especially sensitive electronics
- **Undervoltage** causes motors to draw excessive current and overheat
- **Rapid cycling** back and forth accelerates mechanical wear and can cause tap changer failure

An attacker who can write to the regulator tap position register via Modbus or DNP3 can execute all of these.

### Lab Implementation

The regulator simulator (\`regulator-sim\`) runs at **10.40.40.22** on \`field_net\`. It models a load tap changer with a plus or minus 16 tap range and voltage regulation logic.

- **Modbus**: holding register read/write on port 502
- **DNP3**: analog output commands to outstation address **3** on port 20000

The physics engine adjusts the reported feeder voltage based on tap position. You can observe the electrical impact of unauthorized tap changes in real time on the HMI.`,
      },
      {
        id: "capacitor-banks",
        title: "Capacitor Banks",
        body: `Capacitor banks provide reactive power compensation on distribution feeders. As inductive loads (motors, transformers) draw reactive power, the voltage at the end of the feeder drops. Shunt capacitor banks inject reactive power locally, offsetting the inductive demand and reducing voltage drop. This lets the utility serve more load without upgrading conductors or transformers.

### Fixed vs. Switched

Capacitor banks come in two flavors:

- **Fixed**: always connected, providing constant reactive support
- **Switched**: controlled by a capacitor bank controller that monitors voltage, reactive power, or time of day

Switched banks use vacuum or oil switches rated for capacitor inrush current. Each switching operation stresses the contacts and capacitor cells, so controllers implement switch-count lockout protection to prevent excessive cycling.

### Cybersecurity Risks

An attacker who can communicate with the capacitor bank controller could:

- **Rapidly switch the bank in and out**, exceeding the switch-count limit and triggering lockout
- **Switch it in during light load**, causing overvoltage
- **Prevent switching during heavy load**, causing undervoltage

The switch-count lockout is a protective feature, but an attacker who understands it can deliberately trigger it to disable the bank entirely.

### Relevance to This Lab

The RangerDanger lab does not include a dedicated capacitor bank simulator, but the concepts apply directly to the voltage regulator and recloser. The same Modbus and DNP3 protocols control capacitor bank switches. The same segmentation principle applies: restrict direct field device access to the RTAC only. Understanding capacitor bank operations rounds out your knowledge of feeder voltage and power quality management devices.`,
      },
    ],
  },

  /* ====== Network Segmentation Concepts ====== */
  {
    heading: "Network Segmentation Concepts",
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

The containd NGFW provides Deep Packet Inspection (DPI) for ICS protocols. It does not just filter by IP and port. It inspects Modbus function codes and DNP3 application layer content. This allows policies like "allow Modbus reads (FC03) from the HMI to the RTAC but block Modbus writes (FC05/FC06)" or "allow DNP3 reads but block Direct Operate commands from non-RTAC sources." That level of protocol-aware filtering is the gold standard for OT firewalls.`,
      },
      {
        id: "purdue-model",
        title: "Purdue Model Levels (L0–L5)",
        body: `The **Purdue Enterprise Reference Architecture (PERA)** — usually shortened to "the Purdue Model" — is the canonical *architectural* reference for how industrial control systems are layered. It originated in the early 1990s at Purdue University and was adopted by ISA-95 as the basis for modeling enterprise-control integration. When practitioners talk about "Level 3" or "L3.5 DMZ" in an OT context, they're talking about Purdue levels.

This is **not** the same thing as IEC 62443's "Security Levels." Both frameworks use the word "levels" but they answer different questions:

- **Purdue levels** answer *"where in the architecture does this asset live?"* — a hierarchy from physical process up to enterprise IT.
- **IEC 62443 Security Levels (SL 0–4)** answer *"how strong must the security controls be for this zone?"* — a threat-capability scale.

You use both together: Purdue to decide *what's in which zone*, IEC 62443 to decide *how hard to harden each zone*.

### The Levels

| Level | Name | What lives here |
|-------|------|-----------------|
| **L0** | Process | Physical process — sensors, actuators, motors, valves, the actual breaker mechanism |
| **L1** | Basic Control | Direct controllers — PLCs, RTUs, protective relays, IEDs |
| **L2** | Area Supervisory Control | Local HMIs, SCADA front-ends, alarm systems, area-level historians |
| **L3** | Site Operations | Site-wide SCADA, historian, engineering workstations, MES; the "OT operations" zone |
| **L3.5** | Industrial DMZ (IDMZ) | Proxies, jump hosts, patch staging, anti-virus distribution; the gatekeeper between OT and IT |
| **L4** | Site Business Planning & Logistics | Plant-level business systems, ERP edge, file shares |
| **L5** | Enterprise | Corporate IT, internet-facing systems, central directory, email |

### The L3.5 DMZ Matters

The Industrial DMZ at L3.5 is the single most important architectural element in modern ICS security. It's what enforces "no direct path from corporate to control." Every protocol crossing between L3 (OT operations) and L4 (business) must terminate in the IDMZ — broker patterns, reverse proxies, replicated historians — never a direct tunnel. The IDMZ is where you put the controls that catch lateral movement before it reaches the plant floor.

A flat network where corporate workstations can ping the PLC is "L4 talking directly to L1" with no L3.5 in between. That's the architectural anti-pattern this lab demonstrates with the **weak baseline** policy.

### How the RangerDanger Lab Maps to Purdue Levels

| Lab zone | Subnet | Purdue level |
|----------|--------|---|
| Field Devices | \`10.40.40.0/24\` | **L1** — relays, recloser, regulator, capacitor bank |
| OT Operations | \`10.30.30.0/24\` | **L3** — HMI, RTAC, OpenPLC, historian, GPS time server |
| Vendor / Engineering | \`10.20.20.0/24\` | **L3.5** (IDMZ) — vendor jump box, engineering workstation |
| Enterprise | \`10.10.10.0/24\` | **L4** — corporate workstation, attacker simulation |

L0 (the physical process — the actual breaker mechanism, the feeder conductor, the connected load) is the OpenDSS simulation engine that consumes commands from the relays/recloser/regulator and computes the resulting voltages and currents. L2 (area supervisory) is collapsed into L3 for the lab — small distribution substations often don't have a distinct local HMI tier. L5 (enterprise) sits outside the lab — represented only by the idea that the corporate workstation could be reaching out to the internet.

### Why Both Frameworks at Once

When you produce evidence for a change board:

1. **Purdue** answers *"is your design layered correctly?"* — does every cross-tier flow terminate in the L3.5 DMZ? Are there any L4-to-L1 direct paths that bypass L3?
2. **IEC 62443** answers *"is the security strength per zone proportional to the risk?"* — is your L1 conduit enforcing SL-3 controls (protocol-aware filtering, source restriction, logging) or just port filtering?

A correct design needs both. A flat network can be technically "secured" with strong controls on every host (high SL-A everywhere) but still violate Purdue layering because there's no architectural firebreak. Conversely, a beautifully Purdue-layered network with no controls at any layer fails IEC 62443 SL assessment.

### A Common Mistake

Treating L3 and L3.5 as the same thing. Many real deployments put the vendor jump box directly in the OT operations subnet — that collapses the IDMZ and turns vendor remote access into a direct path into L3. The hardened policy in this lab fixes that by restricting which protocols and source IPs can traverse the L3.5→L3 boundary, even though the network architecture still has them as adjacent subnets.`,
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

In the RangerDanger lab, the containd NGFW functions as the EAP for all zone boundaries. The \`field_net\` is the ESP boundary for the most critical assets (relays, reclosers, regulators). The \`vendor_net\` simulates the vendor remote access path, which in a CIP-compliant environment requires IRA controls.

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
    ],
  },

  /* ====== Protocols & Communication ====== */
  {
    heading: "Protocols & Communication",
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

Modbus TCP has **no authentication, no encryption, and no authorization**. Any device that can establish a TCP connection to port 502 can read or write any register. The server has no way to distinguish a legitimate RTAC command from a malicious one sent by an attacker. This is not a bug. Modbus was designed in 1979 for isolated serial networks where physical access was the security boundary. Deploying it on routable IP networks without compensating controls is inherently dangerous.

### The Compensating Control: Segmentation + DPI

The primary defense for Modbus TCP is network segmentation with protocol-aware firewalling. The containd NGFW inspects Modbus traffic at the application layer and filters by function code. The hardened policy allows FC03 (read holding registers) from the HMI to field devices but blocks FC05 and FC06 (write operations) from any source except the RTAC. This is the best available protection for a protocol with no native security.

### Lab Implementation

Every field device simulator exposes Modbus TCP on port 502:

- **Relay** (10.40.40.20): breaker position, trip count, lockout status in coils and holding registers
- **Recloser** (10.40.40.21): auto-reclose state as a Modbus coil
- **Regulator** (10.40.40.22): tap position as a Modbus holding register

You use \`mbpoll\` on the Kali box to read and write these registers. The difference between weak and hardened policies is the difference between full read/write access and being blocked at the firewall.`,
      },
      {
        id: "dnp3",
        title: "DNP3 in Substations",
        body: `DNP3 (Distributed Network Protocol version 3) is the dominant SCADA protocol in North American electric utilities. Developed in the 1990s based on IEC 60870-5, it was designed specifically for utility communication: reliable delivery over unreliable links, time-stamped event reporting, and a data model that maps well to power system objects. It runs over both serial and TCP/IP, with TCP typically using **port 20000**.

### Master-Outstation Architecture

DNP3 uses a master-outstation model. The master (RTAC or SCADA system) initiates requests to outstations (field devices). Key operations:

- **Read** (FC01): retrieve data objects from the outstation
- **Direct Operate** (FC05): command an action (open breaker, change setpoint)
- **Select-Before-Operate** (FC03/FC04): two-step confirmation for safety-critical operations. The master selects the control point, the outstation confirms readiness, then the master issues the operate. SBO is recommended for critical controls but not universally deployed.

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

You can use DNP3 tools on the Kali box to send Direct Operate commands, tripping breakers and changing tap positions. The hardened firewall policy restricts DNP3 command traffic to the RTAC only.`,
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
    ],
  },

  /* ====== Command References & Lab Tools ====== */
  {
    heading: "Command References & Lab Tools",
    articles: [
      {
        id: "tool-mbpoll",
        title: "mbpoll - Modbus TCP Command-Line Tool",
        body: `\`mbpoll\` is a command-line Modbus master (client) that lets you read from and write to Modbus TCP devices. In the lab, it is pre-installed on the Kali box at 10.10.10.50 and is your primary tool for interacting with field device simulators over Modbus.

> **Mode flag is optional.** \`mbpoll\` defaults to TCP whenever the target is an IP address, so \`-m tcp\` is redundant in this lab. Older write-ups elsewhere may include it — it's harmless, just noise. The lab YAMLs standardize on the no-mode form to match this article.

### Reading Holding Registers (FC03)

To read holding registers from a device:

\`\`\`bash
mbpoll -a 1 -r 1 -c 5 -t 4 -1 10.40.40.20
\`\`\`

Flag breakdown:

- \`-a 1\` : Modbus slave address (1 is typical for TCP devices)
- \`-r 1\` : starting register number
- \`-c 5\` : number of registers to read
- \`-t 4\` : register type (4 = holding registers, FC03)
- \`-1\` : single poll, then exit (without this, mbpoll polls continuously)
- Last argument: target IP address

### Reading Coils (FC01)

\`\`\`bash
mbpoll -a 1 -r 1 -c 4 -t 0 -1 10.40.40.20
\`\`\`

- \`-t 0\` : coil type (FC01)

### Writing a Single Coil (FC05)

To write a coil value (e.g., trip the relay breaker):

\`\`\`bash
mbpoll -a 1 -r 1 -t 0 -1 10.40.40.20 1
\`\`\`

The value at the end (1 or 0) sets the coil ON or OFF.

### Writing a Single Register (FC06)

To write a holding register (e.g., set regulator tap position):

\`\`\`bash
mbpoll -a 1 -r 1 -t 4 -1 10.40.40.22 5
\`\`\`

The value at the end is written to the register.

### Common Lab Targets

| Device | IP | What to Read/Write |
|--------|----|--------------------|
| Relay | 10.40.40.20 | Breaker coil, trip count, lockout |
| Recloser | 10.40.40.21 | Reclose-enabled coil, shot count |
| Regulator | 10.40.40.22 | Tap position register |

> When the hardened firewall policy is active, \`mbpoll\` commands from the Kali box will time out because containd blocks Modbus traffic from the enterprise zone to field devices.`,
      },
      {
        id: "tool-dnp3",
        title: "dnp3poll & dnp3cmd - DNP3 Tools",
        body: `The Kali box includes command-line tools for interacting with DNP3 outstations. \`dnp3poll\` reads data from an outstation, and \`dnp3cmd\` sends control commands.

### Reading Outstation Data with dnp3poll

To poll the relay outstation for its current data:

\`\`\`bash
dnp3poll -m 100 -s 1 10.40.40.20:20000
\`\`\`

Flag breakdown:

- \`-m 100\` : master address (use 100 for the Kali box)
- \`-s 1\` : outstation (slave) address
- Last argument: target IP and port

### Outstation Addresses in the Lab

| Device | IP:Port | Address |
|--------|---------|---------|
| Relay | 10.40.40.20:20000 | 1 |
| Recloser | 10.40.40.21:20000 | 2 |
| Regulator | 10.40.40.22:20000 | 3 |
| RTAC | 10.30.30.20:20000 | 10 (read-only) |

### Sending Commands with dnp3cmd

To send a Direct Operate (CROB) command to trip the relay breaker:

\`\`\`bash
dnp3cmd -m 100 -s 1 -o trip 10.40.40.20:20000
\`\`\`

To disable auto-reclose on the recloser:

\`\`\`bash
dnp3cmd -m 100 -s 2 -o disable-reclose 10.40.40.21:20000
\`\`\`

To change the regulator tap position:

\`\`\`bash
dnp3cmd -m 100 -s 3 -o set-tap -v 10 10.40.40.22:20000
\`\`\`

### Direct Operate vs. Select-Before-Operate

- **Direct Operate** (FC05): single-step command execution. The outstation acts immediately.
- **Select-Before-Operate** (FC03/FC04): two-step. The master first selects the control point, the outstation confirms, then the master issues operate. Safer for critical operations.

The lab simulators accept Direct Operate commands, which is the more dangerous (and more commonly exploited) mode. A real substation should use SBO for safety-critical controls, but many do not.

> Under the hardened policy, DNP3 commands from any source other than the RTAC (10.30.30.20) are blocked by containd at the firewall.`,
      },
      {
        id: "tool-tshark",
        title: "tshark - Network Protocol Analysis",
        body: `\`tshark\` is the command-line version of Wireshark. In the lab, you use it to analyze network traffic and verify what protocols are flowing between zones. It is available on the Kali box and (depending on configuration) on the firewall itself.

### Protocol Volume Summary (io,stat)

To see a summary of protocol traffic over time:

\`\`\`bash
tshark -r capture.pcap -q -z io,stat,10,modbus,dnp3
\`\`\`

This gives you a table showing packet counts for Modbus and DNP3 traffic in 10-second intervals. Useful for seeing traffic patterns and spotting when attack traffic starts.

### Host Pair Summary (conv,ip)

To see which IP addresses are talking to each other:

\`\`\`bash
tshark -r capture.pcap -q -z conv,ip
\`\`\`

This produces a table of all IP conversation pairs with packet counts and byte totals. You can quickly identify which hosts are generating the most traffic and spot unexpected communication paths (like enterprise hosts talking directly to field devices).

### TCP Connection Detail (conv,tcp)

To see individual TCP connections:

\`\`\`bash
tshark -r capture.pcap -q -z conv,tcp
\`\`\`

This shows source/destination IP:port pairs for every TCP session. Useful for confirming which specific services are being accessed (port 502 = Modbus, port 20000 = DNP3, port 8080 = HTTP REST).

### Display Filters

tshark supports Wireshark display filter syntax for narrowing results:

\`\`\`bash
# Only Modbus traffic
tshark -r capture.pcap -Y "modbus"

# Modbus writes only (FC05 or FC06)
tshark -r capture.pcap -Y "modbus.func_code == 5 || modbus.func_code == 6"

# DNP3 traffic to the relay
tshark -r capture.pcap -Y "dnp3 && ip.dst == 10.40.40.20"

# All traffic from the Kali box
tshark -r capture.pcap -Y "ip.src == 10.10.10.50"
\`\`\`

### Live Capture

To capture live traffic on an interface:

\`\`\`bash
tshark -i eth0 -w capture.pcap
\`\`\`

Add \`-f "port 502"\` as a BPF capture filter to limit what gets written to the file. Use \`-c 100\` to stop after 100 packets.

> The three views (io,stat, conv,ip, conv,tcp) give you a quick top-down picture of network activity without scrolling through individual packets. Start there before diving into packet-level analysis.`,
      },
      {
        id: "tool-tcpdump",
        title: "tcpdump - Packet Capture",
        body: `\`tcpdump\` captures raw packets on a network interface. In the lab, you run it on the containd firewall (via SSH on port 2222) to capture traffic as it transits between zones. This is how you see exactly what the firewall is processing.

### Basic Capture on the Firewall

SSH into the firewall and run:

\`\`\`bash
ssh -p 2222 containd@localhost
tcpdump -i eth3 -w /tmp/field_capture.pcap
\`\`\`

- \`-i eth3\` : capture on the field_net interface (lan2)
- \`-w /tmp/field_capture.pcap\` : write raw packets to file

Press Ctrl+C to stop. You can then copy the file out or analyze it with tshark.

### BPF Filter Syntax

BPF (Berkeley Packet Filter) expressions limit what tcpdump captures:

\`\`\`bash
# Only Modbus TCP traffic
tcpdump -i eth3 port 502

# Only DNP3 traffic
tcpdump -i eth3 port 20000

# Traffic from a specific host
tcpdump -i eth3 host 10.40.40.20

# Modbus traffic to the relay specifically
tcpdump -i eth3 host 10.40.40.20 and port 502

# Traffic from the Kali box to any field device
tcpdump -i eth3 src host 10.10.10.50
\`\`\`

### Common BPF Operators

- \`host 10.40.40.20\` : matches source or destination
- \`src host\` / \`dst host\` : matches only source or destination
- \`port 502\` : matches source or destination port
- \`and\`, \`or\`, \`not\` : boolean combinators
- \`net 10.40.40.0/24\` : matches an entire subnet

### Useful Flags

| Flag | Purpose |
|------|---------|
| \`-i eth3\` | Interface to capture on |
| \`-w file.pcap\` | Write to file (for later analysis with tshark) |
| \`-c 100\` | Stop after 100 packets |
| \`-n\` | Do not resolve hostnames (faster) |
| \`-v\` / \`-vv\` | Verbose output |
| \`-X\` | Print packet contents in hex and ASCII |

### Which Interface Is Which?

On the containd firewall, interfaces map to zones:

| Interface | Zone | Subnet |
|-----------|------|--------|
| eth0 (wan) | enterprise_net | 10.10.10.0/24 |
| eth1 (dmz) | vendor_net | 10.20.20.0/24 |
| eth2 (lan1) | ot_ops_net | 10.30.30.0/24 |
| eth3 (lan2) | field_net | 10.40.40.0/24 |

> Capture on the interface closest to the traffic you care about. If you want to see attack traffic hitting field devices, capture on eth3 (field_net). If you want to see what leaves the enterprise zone, capture on eth0.`,
      },
      {
        id: "tool-curl",
        title: "curl - HTTP REST API Interaction",
        body: `Every field device simulator in the lab exposes an HTTP REST API on **port 8080**. \`curl\` is the simplest way to read device state, send commands, and check health. It is available on the Kali box and most other lab containers.

### Reading Device State

\`\`\`bash
# Relay state
curl -s http://10.40.40.20:8080/api/state | jq .

# Recloser state
curl -s http://10.40.40.21:8080/api/state | jq .

# Regulator state
curl -s http://10.40.40.22:8080/api/state | jq .

# RTAC aggregated state (all devices)
curl -s http://10.30.30.20:8080/api/state | jq .
\`\`\`

Pipe through \`jq\` for readable JSON output. The \`-s\` flag silences the progress bar.

### Sending Commands

Commands go via POST to \`/api/command\`:

\`\`\`bash
# Trip the relay breaker
curl -s -X POST http://10.40.40.20:8080/api/command \\
  -H "Content-Type: application/json" \\
  -d '{"action": "trip"}'

# Close the relay breaker
curl -s -X POST http://10.40.40.20:8080/api/command \\
  -H "Content-Type: application/json" \\
  -d '{"action": "close"}'

# Disable recloser auto-reclose
curl -s -X POST http://10.40.40.21:8080/api/command \\
  -H "Content-Type: application/json" \\
  -d '{"action": "disable-reclose"}'

# Set regulator tap position
curl -s -X POST http://10.40.40.22:8080/api/command \\
  -H "Content-Type: application/json" \\
  -d '{"action": "set-tap", "value": 10}'
\`\`\`

### Health Checks

\`\`\`bash
curl -s http://10.40.40.20:8080/api/health
\`\`\`

Returns a simple status response. Useful for verifying a device is running before trying protocol-level interaction.

### Audit Log

\`\`\`bash
curl -s http://10.40.40.20:8080/api/audit | jq .
\`\`\`

Returns the command audit trail showing all commands the device has received. This is useful for confirming that your Modbus or DNP3 commands actually reached the device and what effect they had.

### When to Use curl vs. Modbus/DNP3

The HTTP API is convenient for quick checks and is not subject to the same firewall rules as industrial protocols. However, the exercises focus on Modbus and DNP3 because those are the protocols used in real substations. Use \`curl\` for:

- Verifying device state before and after attacks
- Confirming that commands sent via Modbus/DNP3 had the expected effect
- Quick troubleshooting when something is not working as expected`,
      },
      {
        id: "tool-nmap",
        title: "nmap - Port Scanning & Reconnaissance",
        body: `\`nmap\` is the standard network scanner for host discovery and port enumeration. In the lab, it is pre-installed on the Kali box and is used in the reconnaissance phase of attack exercises to discover what is reachable from the enterprise zone.

### Basic Host Discovery

To find live hosts on a subnet:

\`\`\`bash
nmap -sn 10.40.40.0/24
\`\`\`

The \`-sn\` flag does a ping sweep without port scanning. Fast way to see which field devices are up.

### Port Scanning a Specific Host

\`\`\`bash
nmap -sT -p 502,8080,20000 10.40.40.20
\`\`\`

- \`-sT\` : TCP connect scan (full three-way handshake, reliable)
- \`-p 502,8080,20000\` : scan only the ports used by lab simulators

### Scanning for ICS Protocols Across a Zone

\`\`\`bash
nmap -sT -p 502,20000 10.40.40.0/24
\`\`\`

This finds all hosts on the field network with open Modbus (502) or DNP3 (20000) ports. In a real engagement, this is how you identify ICS devices on a network segment.

### Service Version Detection

\`\`\`bash
nmap -sV -p 502,8080,20000 10.40.40.20
\`\`\`

The \`-sV\` flag probes open ports to identify the running service and version. Useful for fingerprinting, though the lab simulators may not return detailed version strings.

### Scanning Through the Firewall

The key observation in the lab exercises is how nmap results change between firewall policies:

- **Weak policy**: scanning from the Kali box (10.10.10.50) shows ports 502, 8080, and 20000 open on field devices
- **Hardened policy**: the same scan shows ports filtered or unreachable because containd blocks enterprise-to-field traffic

This is a concrete, visible demonstration of what segmentation does. The nmap output is the proof.

### Common Flags Reference

| Flag | Purpose |
|------|---------|
| \`-sn\` | Ping sweep, no port scan |
| \`-sT\` | TCP connect scan |
| \`-sS\` | TCP SYN scan (requires root, stealthier) |
| \`-sV\` | Service version detection |
| \`-p 502,20000\` | Specific ports |
| \`-p-\` | All 65535 ports (slow) |
| \`--open\` | Only show open ports in output |
| \`-oN file.txt\` | Save output to file |

> Start reconnaissance with a targeted port scan (\`-p 502,8080,20000\`) rather than scanning all ports. You already know what the lab simulators expose, and targeted scans are much faster.`,
      },
    ],
  },

  /* ====== Lab-Specific Context ====== */
  {
    heading: "Lab-Specific Context",
    articles: [
      {
        id: "lab-architecture",
        title: "The RangerDanger Lab Architecture",
        body: `The RangerDanger lab models a distribution substation with four network zones separated by the containd Next-Generation Firewall (NGFW), plus an unfirewalled physics simulation network.

### Network Zones

| Zone | Subnet | Key Hosts |
|------|--------|-----------|
| Enterprise | 10.10.10.0/24 | Corporate workstation (10.10.10.10), Kali attacker (10.10.10.50) |
| Vendor | 10.20.20.0/24 | Jump box (10.20.20.10), Engineering workstation (10.20.20.20) |
| OT Operations | 10.30.30.0/24 | HMI/FUXA (10.30.30.10), RTAC (10.30.30.20), OpenPLC (10.30.30.30) |
| Field Devices | 10.40.40.0/24 | Relay (10.40.40.20), Recloser (10.40.40.21), Regulator (10.40.40.22) |
| Physics | 10.50.50.0/24 | OpenDSS engine (10.50.50.20), not firewalled |

All inter-zone traffic flows through containd, which acts as the default gateway for every zone.

### Three Protocol Interfaces

The field device simulators are custom Go services that implement three protocol interfaces simultaneously on the same shared state:

1. **HTTP REST** on port 8080: \`GET /api/state\`, \`POST /api/command\`, \`GET /api/audit\`, \`GET /api/health\`
2. **Modbus TCP** on port 502: function codes FC01/FC03/FC04 for reads, FC05/FC06 for writes
3. **DNP3 TCP** on port 20000: Read (FC01), Direct Operate (FC05), Select-Before-Operate (FC03/FC04)

All three interfaces operate on the same device state. A Modbus write to the relay breaker coil is immediately reflected in HTTP and DNP3 responses. Use whichever protocol suits the exercise.

### The Simulation Loop

The RTAC polls all field devices and the physics engine at regular intervals, aggregating their state into a unified view. The HMI displays this on a one-line diagram showing feeder topology, breaker positions, voltage levels, and device health. The physics engine calculates electrical quantities (voltage, energization) based on current device states, creating a **closed-loop simulation** where opening a breaker actually de-energizes the downstream feeder section.

### Background Traffic

The RTAC polling cycle generates baseline autonomous traffic on the OT and field networks. This is realistic background traffic that you can observe and analyze. When you execute attack exercises, your malicious traffic is superimposed on this baseline. You can use the containd event feed and the HMI to observe both the network-level and process-level effects of your actions.`,
      },
      {
        id: "weak-vs-hardened",
        title: "Weak vs Hardened Firewall Policy",
        body: `The lab provides two firewall configurations representing opposite ends of the segmentation spectrum. Both use the exact same network topology. The only difference is the containd firewall rules.

### Weak Baseline (substation-weak.json)

The weak policy is intentionally permissive, reflecting the reality of many substations where firewalls exist but are configured with broad allow rules. Under this policy:

- The enterprise zone can communicate freely with OT operations and field devices
- The Kali attacker at 10.10.10.50 can directly reach the relay at 10.40.40.20 on Modbus (port 502) and DNP3 (port 20000)
- The vendor zone has similarly broad access
- There is **no protocol-level filtering**, so Modbus writes and DNP3 Direct Operate commands pass through unchallenged

This configuration enables all attack exercises to succeed.

### Hardened Policy (substation-improved.json)

The hardened policy represents the target state after a segmentation improvement project:

- Enterprise zone **cannot reach** the field device zone at all
- Vendor zone has limited access to OT operations for engineering purposes, but **no direct field device access**
- Only the RTAC (10.30.30.20) can send Modbus write commands (FC05, FC06) to field devices
- Only the RTAC can send DNP3 Direct Operate commands to field devices
- Other OT hosts (HMI, PLC) can read from field devices but cannot write

Even if an attacker compromises the HMI or an enterprise host, they cannot directly manipulate field devices.

### The Progressive Learning Model

The exercises follow a deliberate sequence:

1. Run attacks against the **weak baseline** and successfully compromise field devices from the enterprise zone
2. Observe the impact on the simulated power system via the HMI
3. Switch to the **hardened policy**
4. Attempt the same attacks again and watch them fail
5. Inspect the firewall logs to see the blocked connection attempts

This direct comparison builds intuition for why segmentation matters and exactly which rules provide the protection.`,
      },
      {
        id: "traffic-matrix",
        title: "Understanding the Traffic Matrix",
        body: `A traffic matrix documents all expected communication flows between zones and devices in an OT network. For each flow, you record the source, destination, protocol, port, direction, frequency, and purpose. Creating an accurate traffic matrix is the essential first step in designing firewall rules, because you cannot write effective allow rules without knowing what legitimate traffic looks like.

### Baseline vs. Exercise Traffic

The lab distinguishes between two categories:

**Baseline traffic** is the normal operational communication that runs continuously:
- RTAC polling field devices via HTTP
- HMI fetching aggregated state from the RTAC
- Physics engine exchanging data with the RTAC

This represents the legitimate communication that the firewall must always permit.

**Exercise traffic** is the additional communication generated during attack steps:
- Modbus writes from the Kali box to field devices
- DNP3 commands from unauthorized sources
- Port scans from the enterprise zone into field device zone

### Using the Matrix for Anomaly Detection

When reviewing containd events or packet captures, compare observed traffic against the matrix. For example, if the matrix shows that only the RTAC (10.30.30.20) should send Modbus traffic to the relay (10.40.40.20), then any Modbus traffic from 10.10.10.50 (Kali) to 10.40.40.20 is immediately suspicious.

### From Matrix to Firewall Rules

The traffic matrix is the specification for the hardened firewall policy. Each row representing a legitimate, necessary flow becomes an explicit allow rule. Everything not in the matrix is denied by default. This approach, called a positive security model or whitelisting, is the recommended practice per IEC 62443 and NERC CIP-005.

You can use the traffic matrix as a reference when reviewing the hardened firewall rules to verify that each rule corresponds to a documented legitimate flow.

### In a Real Engagement

In a real substation, building the traffic matrix is often the most time-consuming step. It requires interviewing operations engineers, reviewing relay settings, understanding SCADA polling configurations, and capturing baseline traffic for analysis. The lab provides the matrix as a given, but in practice, developing it is a significant portion of the segmentation assessment work.`,
      },
    ],
  },
];

/* ------------------------------------------------------------------ */
/*  Component                                                          */
/* ------------------------------------------------------------------ */

export default function KnowledgePage() {
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [search, setSearch] = useState("");

  const toggle = (id: string) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  const filtered = useMemo(() => {
    if (!search.trim()) return sections;
    const q = search.toLowerCase();
    return sections
      .map((s) => ({
        ...s,
        articles: s.articles.filter((a) =>
          a.title.toLowerCase().includes(q),
        ),
      }))
      .filter((s) => s.articles.length > 0);
  }, [search]);

  return (
    <main className="flex h-[calc(100vh-0px)] flex-col overflow-hidden">
      {/* Header */}
      <header className="flex shrink-0 items-center gap-3 border-b border-slate-800 bg-slate-950/80 px-6 py-4">
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img
          src="/rook-quarter-turn-wink-transparent-web.png"
          alt="Rook"
          className="h-9 w-9 shrink-0"
        />
        <div className="flex items-center gap-2">
          <BookOpen className="h-5 w-5 text-sky-400" />
          <h1 className="text-lg font-semibold text-slate-100">
            Knowledge Base
          </h1>
        </div>
        <span className="text-xs text-slate-500">
          {sections.reduce((n, s) => n + s.articles.length, 0)} articles
        </span>
      </header>

      {/* Search bar */}
      <div className="shrink-0 border-b border-slate-800 bg-slate-950/60 px-6 py-3">
        <div className="relative max-w-xl">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-500" />
          <input
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Filter articles by title..."
            className="w-full rounded-lg border border-slate-700 bg-slate-900 py-2 pl-10 pr-4 text-sm text-slate-200 placeholder:text-slate-600 focus:border-sky-600 focus:outline-none focus:ring-1 focus:ring-sky-600"
          />
        </div>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-y-auto px-6 py-6">
        <div className="mx-auto max-w-4xl space-y-8">
          {filtered.length === 0 && (
            <p className="py-12 text-center text-sm text-slate-500">
              No articles match your search.
            </p>
          )}

          {filtered.map((section) => (
            <div key={section.heading}>
              <h2 className="mb-3 text-xs font-semibold uppercase tracking-widest text-amber-400/80">
                {section.heading}
              </h2>

              <div className="space-y-2">
                {section.articles.map((article) => {
                  const isOpen = expanded.has(article.id);
                  return (
                    <div
                      key={article.id}
                      className="rounded-lg border border-slate-800 bg-slate-900/60"
                    >
                      <button
                        onClick={() => toggle(article.id)}
                        className="flex w-full items-center gap-2 px-4 py-3 text-left text-sm font-medium text-slate-200 transition-colors hover:bg-slate-800/50"
                      >
                        {isOpen ? (
                          <ChevronDown className="h-4 w-4 shrink-0 text-sky-400" />
                        ) : (
                          <ChevronRight className="h-4 w-4 shrink-0 text-slate-500" />
                        )}
                        {article.title}
                      </button>

                      {isOpen && (
                        <div className="knowledge-article border-t border-slate-800 px-5 py-4 text-sm leading-relaxed text-slate-300">
                          <Markdown>{article.body}</Markdown>
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            </div>
          ))}
        </div>
      </div>
    </main>
  );
}
