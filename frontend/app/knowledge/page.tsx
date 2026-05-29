"use client";

import React, { useCallback, useEffect, useMemo, useState } from "react";
import {
  ArrowLeft,
  BookOpen,
  ChevronRight,
  Clock,
  Home,
  Layers,
  Radio,
  Search,
  Shield,
  Target,
  Wrench,
  Zap,
} from "lucide-react";
import Markdown from "marked-react";

/* ------------------------------------------------------------------ */
/*  Article data                                                       */
/* ------------------------------------------------------------------ */

interface Article {
  id: string;
  title: string;
  body: string; // markdown content
}

type IconName =
  | "zap"
  | "shield"
  | "radio"
  | "wrench"
  | "layers"
  | "target"
  | "book";

interface Section {
  heading: string;
  description?: string;
  icon?: IconName;
  accent?: string; // tailwind-ish accent color shortcut (sky, emerald, violet, amber, slate, rose)
  articles: Article[];
}

const sections: Section[] = [
  /* ====== Substation Equipment & Operations ====== */
  {
    heading: "Substation Equipment & Operations",
    description:
      "What the devices are, what they do electrically, and how the lab simulates them. Start here if you have not seen a distribution substation up close before.",
    icon: "zap",
    accent: "sky",
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
      {
        id: "substation-physics",
        title: "Distribution Feeder Physics for Tech Workers",
        body: `If you came to this lab from software, networking, or security rather than from a power-engineering background, you have probably wondered what is actually *happening* electrically when the HMI shows the critical-load voltage chip dropping from 120 V to 0 V. This article is a working mental model. Not enough to design a substation, but enough to understand why the numbers move the way they do, what OpenDSS is, and why it is the right engine for the job.

### The Three Quantities That Matter

A distribution feeder is a wire. Like any wire, three things are going on at once:

- **Voltage (V)** is the "pressure" pushing electricity along, measured in volts. The lab's feeder runs at **12.47 kV** (12,470 V line-to-line, 3-phase, 60 Hz, the standard North American distribution-primary voltage). Customer outlets see **120 V** after a final step-down transformer at the service drop.
- **Current (A)** is the *flow rate* through the wire, measured in amperes. More load, more current.
- **Power (kW)** is the product of voltage and current. In a balanced 3-phase system at line-to-line voltage V_LL, the math is **P = √3 × V_LL × I × pf** — the √3 factor comes from how the three phases add. For the lab's critical load (200 kW, 0.95 pf, 12.47 kV LL), that works out to about **9.7 A** on the feeder. Served the same way at residential voltage instead, the current would be vastly higher and the conductor correspondingly thicker. This is why utilities transmit at high voltage and step it down at the customer's transformer: for the same power, higher voltage means lower current means smaller conductor and less line loss.

There is a small twist called **power factor**. Motors and transformers do not draw current perfectly in phase with voltage, so the "real" power (kW) is less than the "apparent" power (kVA). The ratio is power factor. The lab's loads use 0.9 and 0.95 power factor, typical real-world values.

### Why Voltage Drops as You Go Down the Feeder

The feeder wire has resistance and reactance. Together these are called **impedance**. When current flows through impedance, you lose voltage along the way. By the time current reaches the end of a 2,500-foot feeder, the voltage at the load end is measurably lower than at the substation bus.

A networking analogy: think of feeder voltage at the substation as available bandwidth at the source, and load current as concurrent connections. Each foot of conductor is a hop that costs some bandwidth. The further out you go, the less bandwidth you have left for the loads at the far end.

This matters because the end user has a minimum acceptable voltage. Motors stall, sensitive electronics misbehave, and protective relays can misoperate if voltage drops too low.

### ANSI Voltage Ranges (Why "Range A" Keeps Coming Up)

The American National Standards Institute defines two acceptable voltage bands at the customer's service:

- **Range A** is normal operating voltage: **114 V to 126 V**, a ±5% window around the 120 V nominal. The utility is expected to keep voltage in Range A under normal conditions.
- **Range B** is "occasional excursion acceptable, but bring it back to Range A soon": 110 V to 127 V.

Below 114 V is a problem. The lab's HMI alarm logic fires the **LOW VOLTAGE** banner when the critical-load voltage chip drops out of Range A. The "ANSI C84.1 Range A (114–126V)" line you see in the Lab 2.3 verification step refers to exactly this standard.

### What the Lab's Devices Actually Do, Electrically

- **Breaker (\`relay-sim\`)** sits at the source end of the feeder. Open the breaker → the feeder is disconnected from the source bus → everything downstream goes to 0 V immediately. This is the worst-case single-action outcome in the lab.
- **Recloser (\`recloser-sim\`)** sits mid-feeder. Open the recloser → everything *downstream of the recloser* loses power (general load, voltage regulator, critical load) but the substation bus stays energized. The breaker is still closed at the source.
- **Voltage regulator (\`regulator-sim\`)** is a transformer with an adjustable tap. Move the tap up → the secondary voltage goes up. Move it down → it goes down. The tap has ±16 positions covering ±10% voltage adjustment, so each tap step is 0.625% (\`TAP_STEP_PU = 0.00625\` in the lab's OpenDSS wrapper).
- **Fault** is a short circuit somewhere on the feeder. The recloser detects the abnormal current, opens to interrupt it, then (if auto-reclose is enabled) tries to close again after a brief delay. This is the "reclosing" cycle. If the fault persists after \`maxShots = 3\` attempts, the recloser goes to **lockout** and stays open until a human resets it.

### Why a Power-Flow Simulator? Enter OpenDSS

Computing voltage and current at every bus on the feeder, given the current device states and load values, is a non-trivial math problem. It involves solving a system of nonlinear equations because power = voltage × current × power factor, but each load's current depends on the voltage at that load, which depends on the upstream impedance and other loads' currents, which depend on *their* voltages, and so on. Real distribution-engineering software solves this iteratively.

**OpenDSS** is the industry standard for that math. It is an open-source distribution-system simulator from EPRI (the Electric Power Research Institute, the US utility R&D consortium). Utilities use it for hosting-capacity analysis ("where can we add solar without overloading the feeder?"), feeder-loss studies, protection coordination, and exactly the kind of power-flow snapshots the lab needs.

> EPRI's OpenDSS reference: https://www.epri.com/pages/sa/opendss
>
> DSS-Extensions (which packages OpenDSS for Python/Julia/Rust/etc.): https://dss-extensions.org

The lab embeds OpenDSS via the \`opendssdirect.py\` Python binding, served as a FastAPI service at \`opendss-sim:8080\` on \`physics_net\` (10.50.50.20). It runs a real 3-phase unbalanced power-flow solve on every device-state change.

### What the Lab's OpenDSS Model Includes

The DSS circuit file at \`services/opendss-sim/dss/substation_feeder.dss\` models:

- A **12.47 kV, 3-phase, 60 Hz source bus** with realistic short-circuit MVA values
- A **breaker** (modeled as a switchable line element controlled by the lab's relay-sim state)
- A **2,000-foot main feeder segment** using IEEE 13-bus reference line code 601 (336 ACSR overhead conductor)
- A **recloser** (another switchable line element controlled by recloser-sim)
- A **general load** of 500 kW at 0.9 power factor
- A **500-foot lateral** using IEEE 13-bus line code 602 (4/0 ACSR)
- A **load-tap-changer transformer** modeling the voltage regulator (5,000 kVA, ±10% tap range, 32 steps)
- A **critical load** of 200 kW at 0.95 power factor — the "hospital and fire station" load on the customer-service tile

The IEEE 13-bus line codes (\`Linecode.601\` and \`Linecode.602\` in the lab's \`linecodes.dss\` file) are reference impedance data the power-engineering world has used for decades. They are exactly what you would find in any distribution-planning textbook.

### The Closed Loop: Cyber Attack → Physics Recompute → HMI Numbers

When a student fires a \`dnp3cmd\` packet that trips the recloser, here is what actually happens:

1. The DNP3 packet hits the wire. Wireshark with the DNP3 dissector can see the CROB request frame.
2. \`recloser-sim\` parses the frame via the in-tree \`dnp3go\` library and updates its state: \`recloser_closed = false\`.
3. The RTAC's poll loop (every 2 seconds for HTTP, 5 seconds for DNP3 master, 3 seconds for Modbus) picks up the new state.
4. The RTAC POSTs the aggregated device state to OpenDSS: \`{ breaker_closed: true, recloser_closed: false, tap_position: 0, fault_seen: false }\`.
5. OpenDSS issues \`Open Line.Recloser 1\` against the in-memory compiled circuit and runs \`Solution.Solve()\`.
6. The new bus voltages, line currents, and load powers come back as JSON: \`downstream_voltage_v: 0\`, \`critical_load_voltage_v: 0\`, \`feeder_current_a: 0\`.
7. The HMI reads those values from the RTAC's \`/api/state\` endpoint and re-renders. The voltage chip turns red. The customer-service tile flips to "ALL CUSTOMERS WITHOUT POWER."

The numbers on the HMI are real OpenDSS computations of what would happen on this exact 12.47 kV feeder if you opened that switch. They are not lookup-table approximations.

### What Is Real vs. What Is Simplified

Genuinely real in the lab:

- The **OpenDSS engine** itself (same code utilities use for distribution planning)
- The **line impedance data** (IEEE 13-bus reference)
- The **3-phase unbalanced solve** with proper transformer modeling
- The **voltage-regulator tap math** (real LTC model)
- The **fault element** (real OpenDSS Fault component)

Simplified for the lab:

- **One feeder.** A real substation has multiple feeders branching out of a single bus.
- **Snapshot solve, not transient.** Voltages step instantly between solves rather than ringing through a fault clearing event. OpenDSS *can* do dynamic; this lab does not use that mode.
- **No motor dynamics.** Real distribution feeders have induction motors with inrush, stalling, and reactive-power swings during voltage sags. The lab's loads are constant-power.
- **No DERs.** No rooftop solar, no batteries, no EVs on the feeder.
- **Random-walk load variation (±3%)** rather than a real load profile from historical metering data.
- **RTAC-to-OpenDSS over HTTP.** Real RTACs do not talk to a power-flow solver this way; the bridge is a lab convenience to make the physics layer reachable. The cyber-side attack path does not depend on this bridge.

When you explain this lab to a power-engineering colleague, lead with what is real: the physics layer is genuinely a distribution-feeder power flow on industry-standard software. The simplifications are about scope (one feeder, snapshot solve, no transients), not about cutting corners on the math.

:::tip Two views into the same physics
The lab gives you two ways to look at the OpenDSS-computed feeder state. The [\`/substation\` panel](#hmi-scada-fuxa)'s **Feeder One-Line** tab shows the operator-facing alarm summary; the **Electrical Detail** tab shows per-bus voltages, feeder current, and load kW in numeric form. The two tabs read the same data, just at different abstraction levels.
:::

### See Also

- [HMI, SCADA, and the Lab's Substation Panel](#hmi-scada-fuxa) — where the numbers from this article get rendered
- [Power Factor and Reactive Power](#power-factor-reactive-power) — the inductive / capacitive side of the math
- [Reclosers](#reclosers), [Protective Relays](#protective-relays), [Voltage Regulators](#voltage-regulators) — the devices whose state OpenDSS reads
- [The OpenDSS project at EPRI](https://www.epri.com/pages/sa/opendss) — the engine itself`,
      },
      {
        id: "hmi-scada-fuxa",
        title: "HMI, SCADA, and the Lab's Substation Panel",
        body: `**HMI** (Human-Machine Interface) is the operator's view of an industrial process. In a substation control room the HMI is the screen that shows breakers as open / closed circles, voltages as numeric tiles, and alarms as colored banners. It is what the operator looks at all day.

**SCADA** (Supervisory Control and Data Acquisition) is the larger system: the data-acquisition infrastructure that polls field devices, normalizes their values, stores them in a historian, and forwards selected operator commands back down to the equipment. SCADA is the *plumbing*; the HMI is the *screen*.

**DCS** (Distributed Control System) is the related architecture for plant-wide automation (refineries, chemical plants) where the control logic itself is distributed among many controllers. In substations the architecture is usually called SCADA rather than DCS, but the device-and-protocol surface looks similar.

:::tip The lab's primary HMI is at /substation
Throughout the workshop, every reference to "the Feeder HMI" or "the operational consequence at the HMI" means the **\`/substation\` panel** built into the RangerDanger web app — not FUXA. Open it at <http://localhost:8088/substation>. The Lab 2.3 alarm chain ("RECLOSER OPEN — downstream loads lost") and the customer-service tile both live there.
:::

### What a Real Substation HMI Looks Like

A typical utility HMI shows:

- A **one-line diagram** of the substation with breakers, transformers, and feeders drawn schematically
- **Status indicators** colored by state (closed = green, open = red, tripped = flashing)
- **Analog measurements** for bus voltage, feeder current, MW/MVAR flow
- **Alarm summary** banner showing active alarms and acknowledged-but-unresolved alarms
- **Trend graphs** for recent history
- **Switching menus** the operator uses to dispatch commands (open this breaker, raise this regulator)

Commercial HMIs include Survalent, Wonderware, GE iFIX, ABB PCM600, and many vendor-specific systems. They are licensed software running on Windows or RHEL servers, polling field devices over DNP3, IEC 61850, or Modbus and storing data in a historian.

### The Lab's Primary HMI: \`/substation\`

The \`/substation\` panel is **a custom HMI built into the lab's web app**. It talks to the [RTAC](#rtac)'s REST API (\`GET /api/state\`) and renders the feeder one-line, the customer-service tile, and the alarm chain directly.

We built it custom because we needed tight control over the alarm logic — the \`LOW VOLTAGE\` and \`RECLOSER OPEN — downstream loads lost\` banners fire on the exact lab conditions we wanted to teach — and we wanted a clean kinetic-feedback view that updates within seconds of an attack. The lab's [Distribution Feeder Physics](#substation-physics) closed loop ends here: an attack mutates a [recloser](#reclosers) state, the RTAC polls it, OpenDSS recomputes the feeder, the panel re-renders the alarm chain. That is the operational consequence the labs keep referring to.

Two tabs to know:

- **Feeder One-Line** — the operator-facing summary view with alarm banners, breaker / recloser symbols, customer-service tile. This is what every Lab 2.3 / 2.3-bonus / 2.4 "Operational consequence at the HMI" callout references.
- **Electrical Detail** — the engineering-precision view with per-bus voltage, feeder current, kW / kVAR. Useful when you want to *quantify* an attack rather than read its alarm-level summary; the same view becomes evidence material in Lab 2.4.

### The FUXA Sidecar (Context, Not Load-Bearing)

The lab also runs [FUXA](https://www.frangoteam.com) — an open-source HMI/SCADA platform — at <http://localhost:8088/apps/fuxa-hmi/>. FUXA is wired up: it has a "Substation One-Line Diagram" view configured, polls the RTAC over Modbus, and has the \`hmi_poller\` sidecar generating its baseline traffic. It is the closest thing in the lab to "the HMI you'd encounter at a small utility or in a vendor demo."

:::note FUXA is contextual, not part of the exercises
None of the exercises depend on FUXA being open or correctly configured. The lab's alarm chain, decision questions, and validation chips all read from the \`/substation\` panel. FUXA is included so students who want to see what the open-source HMI ecosystem looks like — and how a traditional Modbus-polled HMI compares to the lab's purpose-built React panel — can spend a few minutes poking around. If FUXA looks empty or misbehaves, ignore it and use \`/substation\`.
:::

### Why a Custom Panel vs. Configuring FUXA

We picked the React-based panel over building the labs around a FUXA project for three reasons:

1. **Alarm-rule control.** The \`/substation\` panel computes alarms ("voltage out of ANSI Range A → red banner") with a few lines of TypeScript. FUXA would require building an equivalent alarm spec inside FUXA's configuration model, which is opaque to students reading the lab source.
2. **Source-controlled reproducibility.** Every byte of the \`/substation\` UI lives in \`frontend/components/substation-panel*.tsx\` and can be diff-reviewed in PRs. FUXA's project state lives in a SQLite database; changes are awkward to review.
3. **Reaction time.** The \`/substation\` panel polls the RTAC every couple of seconds and re-renders within ~100 ms. It is responsive to attacks in a way that feels live. FUXA's Modbus polling cycle adds a second or two of additional latency; the labs are tighter without it.

The trade-off is that the \`/substation\` panel is *not* a representative example of what a real OT HMI looks like architecturally — it is a single-page React app, not a config-driven HMI runtime. FUXA is closer to that reality, which is why it stays around as context.

### See Also

- [Distribution Feeder Physics for Tech Workers](#substation-physics) — what the numbers on the HMI actually mean and where they come from
- [RTAC (Real-Time Automation Controller)](#rtac) — the data source the HMI polls
- [What Outages Cost (SAIDI, SAIFI, and the Customer-Service Tile)](#outage-costs-saidi-saifi) — what the customer-service tile maps to in real-utility metrics`,
      },
      {
        id: "plc-ladder-openplc",
        title: "PLCs, Ladder Logic, and What OpenPLC Does",
        body: `A **Programmable Logic Controller (PLC)** is a ruggedized industrial computer that reads inputs, runs a fixed control program on a repeating scan cycle, and writes outputs. It is the workhorse of industrial automation. PLCs run conveyor belts in warehouses, pumping stations at water utilities, batch reactors in chemical plants, and protection-and-control logic in substations.

PLCs differ from servers and from RTACs in a few important ways:

- **Hard real-time scan cycle.** A typical PLC scan is 10 to 100 milliseconds — read all inputs, execute the program, write all outputs, repeat. Missing a scan is a fault.
- **Field-rugged.** Industrial temperature range, vibration tolerance, redundant power, fanless designs.
- **Ladder logic primary.** Programmable in graphical languages designed for electricians (see below) rather than text-based languages.
- **Vendor-specific.** Allen-Bradley (Rockwell), Siemens, Schneider, Mitsubishi, and many others each have proprietary toolchains.

### Ladder Logic and IEC 61131-3

**Ladder logic** is a graphical programming language whose visual style descends directly from electrical-relay schematics. Each "rung" of the ladder represents a logical condition: contacts on the left, coils on the right. If the contacts are satisfied (closed switch states), the coil energizes (output bit goes true). It looks like an electrical schematic because that is exactly what it is replacing: hard-wired relay logic that used to fill cabinets is now a few rungs of ladder code in a PLC.

The international standard for PLC programming languages is **IEC 61131-3**, which defines five languages: Ladder Diagram (LD), Function Block Diagram (FBD), Structured Text (ST, similar to Pascal), Instruction List (IL, similar to assembly), and Sequential Function Chart (SFC). Most utility PLC programmers work in LD with occasional FBD.

### How PLCs Differ From RTACs

This often confuses tech-side workshop attendees: a PLC and an RTAC are *both* industrial computers, but they have different jobs.

- A **PLC runs control logic.** It reads field-device inputs, executes IEC 61131-3 code, and drives outputs that turn things on and off. The control program is the point.
- An **RTAC aggregates and forwards.** It does not generally run control logic; it polls field devices via DNP3/Modbus, normalizes their data, talks to the SCADA system, and translates operator commands into device-level protocols. It is the substation's communications hub.

Modern substations may have multiple PLCs (or PLC-like devices such as SEL RTACs running embedded protection-logic apps) alongside protective relays and a central RTAC. The lines blur — some SEL RTACs run IEC 61131-3 logic apps and are PLCs in everything but the marketing name.

### What OpenPLC Is

[OpenPLC](https://openplcproject.com) is an **open-source PLC runtime** implementing IEC 61131-3. It runs on Linux, supports ladder, structured text, and function block diagram via the OpenPLC Editor, and is widely used for ICS security research and education because it provides a free, inspectable PLC environment.

In the RangerDanger lab, the \`openplc\` node at \`10.30.30.30\` runs OpenPLC with a simple substation-automation program (\`data/openplc/substation_automation.st\`). It demonstrates a PLC's role in the OT operations zone — accessible via Modbus and the OpenPLC web UI at \`http://localhost:8088/apps/openplc/\` — and gives students a third device class to think about beyond the RTAC and the field-device IEDs. The hardened firewall policy treats it like any other OT-Ops host: cross-zone traffic flows through containd, intra-zone is allowed.`,
      },
      {
        id: "power-factor-reactive-power",
        title: "Power Factor and Reactive Power",
        body: `The feeder physics article introduces power factor in one bullet. This article unpacks it — useful background for the voltage regulator, capacitor bank, and "why does low voltage at heavy load actually happen" questions.

### Real, Reactive, and Apparent Power

Alternating-current circuits have three power quantities, not one:

- **Real power (P)**, measured in **kilowatts (kW)**. This is the power that actually does useful work — heats elements, turns motor shafts, lights bulbs. It is what shows up on the customer bill.
- **Reactive power (Q)**, measured in **kilovolt-amperes-reactive (kVAR)**. This is power that flows back and forth between the source and inductive or capacitive elements. It does no net useful work, but it has to be transported on the same wires as real power.
- **Apparent power (S)**, measured in **kilovolt-amperes (kVA)**. The vector sum of P and Q. Conductors and transformers are sized to handle this — not just the real-power demand.

These three relate by a right triangle: **S² = P² + Q²**. The ratio **P/S** is the **power factor (pf)**, a value between 0 and 1.

### Why Reactive Power Exists

Motors and transformers store energy in magnetic fields. Each AC cycle, energy flows *into* the magnetic field on one half-cycle and back *out* on the other. That sloshing energy is reactive power. The motor only converts a portion of the electrical energy into mechanical work (the real-power portion); the rest is the magnetic field doing its dance.

Capacitors do the same thing with electric fields — but in the *opposite* phase direction. This is why capacitor banks are used to compensate for inductive loads: they push reactive power one way at the moment the motors are pulling it the other way, and the two cancel locally.

### Why Power Factor Matters

A 200 kW load at **pf = 1.0** draws 200 kVA. The same 200 kW at **pf = 0.7** (a heavily inductive motor load) draws 286 kVA — 43% more current on the feeder for the same useful work. The utility has to size conductors and transformers for the kVA, not the kW.

Low power factor also depresses voltage at the load end of the feeder. Reactive current flowing through line reactance drops voltage; the further down a feeder with low pf you go, the lower the voltage at the customer.

### How the Lab Models This

The lab's OpenDSS model uses:

- \`Load.GeneralLoad\` at **0.9 pf** — typical for a mixed commercial/industrial feeder load
- \`Load.CriticalLoad\` at **0.95 pf** — typical for sensitive critical-services loads where utilities specify cleaner power

When OpenDSS solves the feeder, it computes voltage drop using both the real and reactive current. This is why opening the recloser causes a *downstream voltage of zero* (no current flowing, including reactive) and why a regulator tap change at \`-16\` produces a voltage drop large enough to alarm — the LTC is offsetting against reactive demand that the line cannot freely absorb.

### Capacitor Banks and Voltage Regulators

Both work by adjusting the local reactive-power balance:

- A **capacitor bank** injects reactive power locally (it has a *leading* power factor). Switching one in on a feeder with low-pf load raises the local voltage and reduces the kVA the substation has to transport.
- A **voltage regulator** is an LTC transformer that simply adjusts the voltage ratio. It does not change Q; it changes V directly, which is why the lab's tap-attack drops voltage even at constant pf.

Real utility distribution engineering spends a lot of time on Q. The lab abstracts it away under sensible default values, but a power-engineering colleague will immediately ask "what is the power factor on those loads?" if you mention the OpenDSS model — and now you have the answer.`,
      },
    ],
  },

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

- [What is ICS DPI? (and how containd does it)](#ics-dpi) — the DPI technique this article gestures at
- [Purdue Model Levels (L0–L5)](#purdue-model) — the architectural reference for where each zone sits
- [IEC 62443 Zones and Conduits](#iec-62443-zones-conduits) and [Security Levels](#iec-62443-security-levels) — the standards behind the zone vocabulary
- [Weak vs Hardened Firewall Policy](#weak-vs-hardened) — the two concrete configurations the lab compares
- [The OT Kill Chain](#ot-kill-chain) — the threat model these defenses address`,
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
      {
        id: "ics-dpi",
        title: "What is ICS DPI? (and how containd does it)",
        body: `**Deep Packet Inspection (DPI)** is the firewall reading *inside* the TCP payload — past the source IP, port, and TCP flags that L4 firewalls inspect — to make decisions based on the application-layer protocol. **ICS DPI** is DPI specifically for industrial protocols: Modbus, DNP3, IEC 61850, CIP, S7Comm, OPC UA, BACnet, and the dozen-or-so others that show up on OT networks.

### Why L4 Filtering Is Not Enough

A traditional L4 firewall sees a Modbus connection as "TCP from 10.30.30.20 to 10.40.40.20, port 502, established." It cannot tell whether that connection is reading a holding register or writing a setpoint that opens a breaker. The Modbus protocol uses **function codes (FC)** to distinguish these operations — FC1/FC3/FC4 are reads, FC5/FC6/FC15/FC16 are writes — but the function code lives in the payload, not in the TCP header.

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

1. **Kernel-side L4 filtering** via nftables — fast, decides allow/deny on the TCP 5-tuple based on the active policy. Most packets are accepted or denied here without ever leaving the kernel.
2. **Userspace DPI** via NFQUEUE — for packets matching a rule that has \`dpiMode: enforce\` set, the kernel hands the packet up to containd's userspace process via the Linux netfilter queue. containd parses the payload, evaluates the ICS predicate (e.g., "is this Modbus function code in the allowed list \`[1, 3, 4]\`?"), and verdicts the packet back to the kernel: ACCEPT or DROP.

The DPI verdict path is slower than pure-L4 verdicts (microseconds vs nanoseconds), so the policy is structured to use DPI only where it adds value — the **rtac-to-field-modbus** rule in \`substation-improved.json\` is the canonical example: allow only the RTAC's source IP at L4, *and* allow only function codes 1–6 at the DPI layer. Two layers of defense in depth on the same flow.

### How to See It Working in the Lab

When a student in Lab 2.3 fires an unauthorized Modbus FC5 write from kali, the packet hits containd. At L4, the hardened policy already denies enterprise → field on TCP/502 — so the packet is dropped at the kernel layer and the student sees a TCP timeout, not a Modbus error. Watch the [Live DPI Events strip](/console) on the Segmentation drawer; you should see a row with \`category: l4\`, \`verdict: DENY\`, and the matching rule ID.

When that same write is sent from inside an *allowed* flow (the eng-ws → RTAC monitoring path, for example), the L4 rule lets it through but the DPI verdict drops the write because FC5 is not in the allow list. The event row shows \`category: ics\`, \`verdict: DENY\`, and the specific function code that was rejected. That contrast — L4-only DENY vs ICS-DPI DENY — is the lesson the lab is built around.

### The Limit of ICS DPI

DPI cannot save you from a *compromised RTAC* sending legitimate-looking Modbus writes from its authorized source IP. If the attacker is the RTAC, the firewall sees authorized traffic.

:::warning DPI is not a substitute for host hardening
This is why the lab pairs containd's DPI with the kernel-level RTAC routing pin (\`scripts/rtac-harden.sh\`) — the RTAC cannot bridge zones, and the firewall enforces what it is. DPI plus L4 plus host-level hardening together is the defense-in-depth story. **DPI alone is not.**
:::

### See Also

- [How containd Enforces Policy (Kernel-Level View)](#how-containd-enforces-policy) — the nftables + NFQUEUE plumbing the DPI path uses
- [Reading the Live DPI Events Strip](#live-dpi-events-strip) — how to interpret L4 vs ICS verdict rows during an exercise
- [Modbus TCP in Substations](#modbus-tcp) and [DNP3 in Substations](#dnp3) — the protocols this DPI inspects
- [Weak vs Hardened Firewall Policy](#weak-vs-hardened) — the configurations the lab uses to demonstrate L4 + DPI together`,
      },
      {
        id: "vendor-remote-access",
        title: "Vendor Remote Access Patterns",
        body: `Vendor remote access is the persistent thorn in OT security: utility staff cannot keep up with every vendor-specific protective relay, SCADA system, and PLC controller, so vendors need *some* path into the OT network to support their gear. How you let them in defines a large chunk of your attack surface.

### The Common Patterns

**1. Direct VPN.** Each vendor gets a site-to-site or remote-access VPN into the OT network, typically landing in a vendor zone. Easy to set up, hard to scope. The vendor's whole engineering team can usually reach more than they need to. Lab 2.3-bonus simulates this pattern.

**2. Vendor jump host.** A dedicated server in a DMZ that the vendor logs into (typically via RDP, SSH, or Citrix), runs the vendor-specific tools on, and uses to reach the OT devices. Better than direct VPN because the surface from the vendor's network to your DMZ is narrow (one host, a few protocols), and you can monitor the jump host's outbound to OT. This is what the lab's \`vendor-jump\` node models.

**3. Privileged Access Management (PAM) broker.** A specialized appliance — BeyondTrust Privileged Remote Access, CyberArk PSM, Cyolo, Claroty Secure Remote Access, Dispel, Xage, or similar — that the vendor authenticates against. The broker enforces just-in-time access (the path opens for a scheduled window and closes after), MFA, session recording, and click-stream auditing. This is the modern utility standard for high-criticality access.

**4. Zero-Trust Network Access (ZTNA).** An access broker (Zscaler ZPA, Cloudflare Access, Tailscale, Twingate) that builds a per-application tunnel from the vendor's identity to the specific service they need — without exposing the network. The vendor's laptop talks to the broker, the broker talks to the target device, and the network between them stays invisible. Newer, IT-centric, increasingly adopted on the OT/IT boundary.

### Where Vendor Access Usually Fails

The audit findings on real utility engagements tend to cluster in a few areas:

- **Scope creep.** A vendor needs access to "their" devices, so they get access to the OT-Ops VLAN — and from there can reach things that are not theirs.
- **Shared credentials.** "\`vendor\` / \`vendor\`" on the jump host, used by ten people across two organizations. No way to attribute an action to a specific human.
- **No session recording.** The vendor connects, does something to a relay, disconnects. The audit trail is "vendor logged in at 14:03, logged out at 14:42." That is not enough to investigate an incident.
- **Persistent access.** The vendor's path is always open instead of opened on demand for a maintenance window.
- **East-west blindness.** The firewall watches enterprise-to-DMZ but not DMZ-to-OT. A compromised vendor jump host can pivot freely into OT.

### How the Lab Demonstrates This

Lab 2.3-bonus models the persistent-access plus east-west-blindness failure modes together. The vendor's RDP path is always open from the enterprise zone (the attacker exploits this with stolen credentials), and from the vendor jump host the Modbus path to the field zone is also open. The attacker laundered through the vendor session never appears to be the attacker — the field device sees the command coming from the vendor's trusted IP. The hardened policy closes both links of the kill chain: enterprise → vendor RDP is denied at the perimeter, and vendor → field Modbus is denied at the DMZ-to-Field conduit. Either one alone breaks the attack; together is defense in depth.

The PAM-broker and ZTNA patterns are *not* simulated in the lab — they would require more infrastructure than fits in a docker compose — but the same defensive lesson generalizes: scope each access path narrowly, log who did what, and assume any path you do not actively constrain will be exploited.`,
      },
    ],
  },

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

- [What is ICS DPI?](#ics-dpi) — function-code filtering is the DPI lesson the lab teaches against Modbus
- [\`mbpoll\` — Modbus TCP Command-Line Tool](#tool-mbpoll) — the CLI used in the labs
- [DNP3 in Substations](#dnp3) — the protocol DNP3 plays to Modbus's role in distribution`,
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

You can use DNP3 tools on the Kali box to send Direct Operate commands, tripping breakers and changing tap positions. The hardened firewall policy restricts DNP3 command traffic to the RTAC only.

### See Also

- [What is ICS DPI?](#ics-dpi) — DNP3 Direct Operate (FC05) restriction is what containd's DPI rule for DNP3 enforces
- [\`dnp3poll\` & \`dnp3cmd\` — DNP3 Tools](#tool-dnp3) — the CLI tools used in the labs
- [IEC 61850 and GOOSE](#iec-61850-goose) — the modern standard sometimes deployed alongside or instead of DNP3
- [The OT Kill Chain](#ot-kill-chain) — the Industroyer historical example mirrors the Lab 2.3 DNP3 attack`,
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

For greenfield substation builds today — especially in Europe, parts of Asia, and increasingly North American transmission — IEC 61850 is the new-build standard. The DNP3 dominance in this lab is real for *installed* distribution gear, especially in North America; for *new* projects you should expect IEC 61850 to be on the wire alongside or instead of DNP3.

### The Three Protocols in the 61850 Family

**MMS (Manufacturing Message Specification)** is the TCP-based client/server protocol that handles slower SCADA-style operations: configuration reads, status polling, control commands, file transfers, report uploads. It is roughly the IEC 61850 equivalent of DNP3 — the SCADA-master-talks-to-IED protocol — and rides on **TCP 102**.

**GOOSE (Generic Object Oriented Substation Event)** is the headline protocol. It is a **multicast Ethernet** message (not TCP, not IP — raw layer-2 multicast) used for **protection-class messaging** between IEDs. A protective relay detects a fault, sends a GOOSE message announcing "trip" to the network, and other relays receive it in **microseconds**. The whole point of GOOSE is sub-cycle protection: faster than the protection scheme can be implemented in hardwired trip circuits between relays. GOOSE rides directly on Ethernet, typically on a tagged VLAN reserved for protection traffic.

**Sampled Values (SV / IEC 61850-9-2)** is the third leg: digital streaming of analog instrument-transformer measurements (current and voltage) at high frequency (4 kHz or 4.8 kHz). It replaces the copper wires that traditionally carry CT/PT signals to protective relays with a digital multicast stream. SV is the "process bus" use case — typically deployed inside a single substation cabinet, not the wide-area network.

### Why IEC 61850 Is Hard to Attack the Same Way

GOOSE and SV are **layer-2 multicast**, not TCP. A network firewall sitting between zones at L3 (which is what containd does) does not see GOOSE messages flowing on a different VLAN's broadcast domain at all. To inspect or filter GOOSE traffic you need a layer-2 switch with VLAN-aware filtering, port mirroring, or an inline appliance — fundamentally a different architecture than the IT-style L3 firewall this lab models. The threat model for GOOSE is more about local-cabinet access, VLAN hopping, and rogue device insertion than about cross-zone routing.

The MMS protocol is L3-routable and *could* be DPI'd by a containd-class firewall the same way Modbus and DNP3 are. The IEC 61850 community has done less work standardizing DPI predicates for MMS than the DNP3 / Modbus community has done for theirs, so commercial ICS DPI engines vary in how much MMS-aware filtering they support. This is improving as IEC 61850 deployments mature.

### Why the Lab Sticks With DNP3 (For Now)

A few practical reasons:

- **Installed-base reality.** The lab is a *distribution* substation, where DNP3 is the dominant protocol on installed gear in North America. The scenarios the workshop is designed to teach (cross-zone segmentation, ICS DPI, vendor remote-access compromise) match the threat model of the installed base.
- **Docker-bridge constraints.** Simulating GOOSE realistically requires layer-2 multicast on a tagged VLAN. Docker bridge networks do not do that well — bridges are layer-2 but not VLAN-aware in the way GOOSE testing needs. A separate L2 testbed (or a Mininet/CORE-style L2 simulator) would be the right vehicle.
- **DPI lesson generalizes.** The L4 source-pin + function-code-DPI defense pattern the lab teaches against DNP3 maps cleanly to MMS once an IEC 61850-aware engine is available. Students who internalize the lab's segmentation model will not have to relearn it for an MMS environment.

If your environment is IEC 61850-heavy, the gap between this lab and your reality is roughly: substitute MMS for DNP3 in the firewall rules, add a layer-2 strategy for GOOSE that this lab does not exercise, and read about process-bus architectures (SV deployment) separately. The segmentation, DPI, and zone-conduit lessons all carry over.`,
      },
      {
        id: "opc-ua",
        title: "OPC UA Basics",
        body: `**OPC UA** (OPC Unified Architecture) is a modern, vendor-neutral, platform-independent protocol for industrial communication, increasingly the go-to choice for **IT/OT integration**: getting OT data into MES, ERP, historians, analytics platforms, and cloud services. Where DNP3 and IEC 61850 are substation-and-utility specific, OPC UA is broader — it shows up in manufacturing, oil and gas, building automation, and water/wastewater.

### What OPC UA Replaces

**Classic OPC (sometimes called OPC DA, HDA, A&E)** was the previous generation: Windows-only, built on DCOM, painful to firewall, painful to debug, painful to secure. Anyone who has tried to traverse a firewall with DCOM has a story. OPC UA replaces all of that with a single specification that works across Windows, Linux, and embedded devices, runs over TCP (port 4840) or HTTPS, and includes security as a first-class concern rather than a bolt-on.

### The Information Model

OPC UA's distinctive feature is its **address space** — a graph of nodes with types, attributes, and references that describes the data semantically, not just by tag name. A node can be marked as a "Temperature" with units, scaling, and engineering range; another node can be a "PumpController" with predefined methods like \`Start()\` and \`Stop()\`. Clients query the address space, discover what is there, and bind to it without prior schema knowledge.

This is a sharp contrast with DNP3 and Modbus, where the meaning of "holding register 17" lives in a vendor's datasheet rather than in the protocol itself. OPC UA is self-describing.

### Where You Encounter OPC UA on an OT Network

- **Historians.** PI System, Wonderware Historian, Cogent DataHub, and most modern historians use OPC UA to collect data from controllers.
- **Modern PLCs.** Siemens S7-1500, Beckhoff TwinCAT, Rockwell ControlLogix, Schneider M580 — all expose OPC UA servers natively.
- **MES / ERP integration.** Data from the plant floor flowing up to manufacturing-execution and enterprise-resource-planning systems usually rides on OPC UA.
- **IT/OT brokers.** Cloud-bound OT data (AWS IoT SiteWise, Azure IoT Hub, GE Predix) often passes through an OPC UA aggregator.
- **Wind / solar SCADA.** Renewable-generation control rooms increasingly use OPC UA for aggregation across fleets.

### Security Built In

OPC UA has security baked into the spec rather than retrofitted:

- **TLS / certificate-based authentication.** Each client and server presents an X.509 certificate; the connection is mutually authenticated. No clear-text protocol mode is encouraged.
- **User-level access control** on individual nodes. The protocol distinguishes anonymous, username/password, and certificate-bound identities.
- **Message-level signing and encryption.** Per-message integrity protection on top of TLS.

In practice OPC UA deployments still vary widely — many devices ship with self-signed certs, expired certs, or anonymous-allowed configs, so the "security built in" is only as good as the deployment hygiene. But the protocol itself is a substantial improvement over the legacy ICS protocols.

### Why the Lab Doesn't Simulate It

A few reasons:

- The lab focuses on distribution-substation segmentation, where the predominant SCADA protocols are DNP3 and (increasingly) IEC 61850. OPC UA is more common at the IT/OT *boundary* and inside *industrial-automation* environments than inside utility distribution substations.
- Simulating a faithful OPC UA server with a meaningful address space is substantially more work than simulating a Modbus or DNP3 outstation — the protocol stack is heavier, the address-space modeling matters, and the security model needs to be authentic to teach the protocol's strengths.
- The cross-protocol firewall lessons the lab teaches (L4 + DPI defense in depth, source-pinning, segmentation by zone) all carry over to OPC UA cleanly once you map "Modbus function code" to "OPC UA service code" and "DNP3 Direct Operate" to "OPC UA Write/CallMethod."

For IT/OT integration architectures, the OPC UA server is usually positioned in a DMZ between the OT network and the IT/cloud side. Treat that DMZ with the same defense-in-depth posture the lab teaches for the field-zone conduit, layered with OPC UA's own certificate-and-user-level controls. The segmentation logic generalizes; the protocol surface is different.`,
      },
    ],
  },

  /* ====== Command References & Lab Tools ====== */
  {
    heading: "Command References & Lab Tools",
    description:
      "Quick references for the CLI tools the exercises use: mbpoll, dnp3poll, dnp3cmd, tshark, tcpdump, curl, nmap.",
    icon: "wrench",
    accent: "amber",
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
    heading: "Lab Internals",
    description:
      "How the lab is wired together: architecture, weak vs hardened policy, the plan-coverage pipeline, the Live DPI Events strip, and how containd enforces policy at the kernel.",
    icon: "layers",
    accent: "slate",
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
      {
        id: "plan-coverage-pipeline",
        title: "The Plan Coverage Pipeline (1.3 → 1.4 → 2.2 → 2.3 → 2.4)",
        body: `The seven labs are not a flat sequence of independent exercises. The choices you make in Labs 1.3 and 1.4 actually rewrite the content you see in Labs 2.2, 2.3, 2.3-bonus, and 2.4. This article explains the data pipeline behind that, because it is not obvious from the surface and a lot of the lab's pedagogical payoff comes from noticing it.

### The Three Inline Markdown Fences

Lab YAML descriptions support three custom fence blocks that the exercise runner parses out:

**\`:::decision\`** captures a multiple-choice answer from the student into browser localStorage. Lab 1.3 uses these to record design verdicts (BLOCK / RESTRICT / ALLOW per requirement). Lab 1.4 uses them to record selected remediation actions and per-role labor budgets. Each fence has an \`id\` so later labs can read the recorded answer.

\`\`\`
:::decision id=enterprise-to-field options=BLOCK,RESTRICT,ALLOW correct=BLOCK
What is your design verdict for Enterprise → Field traffic?
:::
\`\`\`

**\`:::findings-panel\`** displays what the student recorded in an earlier lab as a structured panel. Lab 2.4 uses these to surface the design verdicts from Lab 1.3 and the observations from Lab 1.2 inside the validation step, so the student can compare current evidence against original intent without flipping back through tabs.

**\`:::plan-coverage\`** is the runtime engine. It reads the student's Lab 1.4 selections from localStorage and renders a live "this requirement is fully addressed / partial / deferred / not applicable" matrix. Lab 2.3 uses it to show which of the three attack defenses the student's plan actually closes. Lab 2.4 uses it for the final reflection.

### The Closed Loop

Concretely, here is what happens when a student makes a choice in Lab 1.4:

1. The student clicks a remediation action in Lab 1.4 — say \`pin-rtac-to-field\`.
2. \`scenario-runner.tsx\` writes the selection to localStorage under \`decision:remediation-planning:pin-rtac-to-field\` with the value \`SELECTED\`.
3. Subsequent labs read the same key when their description is rendered. The \`injectDynamicContent()\` helper in \`scenario-runner.tsx\` looks for \`:::plan-coverage\` fences and computes coverage live based on what is in localStorage.
4. Lab 2.2's Phase 3 text adapts based on whether the student selected DPI actions or not. Lab 2.3's "Apply the hardened policy" plan-coverage panel shows which attacks the student's plan actually closes. Lab 2.4's final reflection shows the full coverage matrix.

### Why This Matters Pedagogically

A lab that says "build a hardened firewall policy" without forcing the student to commit to specific design verdicts and remediation choices first lets the student skip the thinking. The plan-coverage pipeline makes the thinking visible: by Lab 2.4 the student can see, "I committed to BLOCK on enterprise-to-field in Lab 1.3, I deferred adding Modbus DPI in Lab 1.4, and now Lab 2.4's panel tells me my plan does close the enterprise-to-field attack but leaves the eng-ws-to-RTAC-on-DNP3 surface open."

The same workshop can be run two different ways — straight through, treating the labs as independent exercises, or with explicit attention to the pipeline. Both work. The pipeline is the deeper read.

### Resetting the State

Student progress lives in localStorage on each student's browser, scoped by exercise ID. To reset a single lab's recorded decisions during a workshop, open the browser console on the lab page and run \`localStorage.clear()\` (clears everything) or \`Object.keys(localStorage).filter(k => k.startsWith("decision:remediation-planning")).forEach(k => localStorage.removeItem(k))\` to reset just one lab. The instructor-facing \`/api/workshop/reset\` endpoint resets simulator state (substation devices, firewall config) but does not touch student-side localStorage.`,
      },
      {
        id: "live-dpi-events-strip",
        title: "Reading the Live DPI Events Strip",
        body: `The Segmentation drawer on the [Network Map](/console) has a **Live DPI Events** strip that surfaces firewall events in real time — every accept, every deny, every DPI verdict. This article is the operator's manual for reading it.

### Where the Events Come From

containd emits two kinds of events that feed the strip:

1. **L4 events** — every packet that matches a rule with logging enabled. These come from the kernel's **nflog** facility on the \`nflogGroup\` configured in the active policy (group 100 in the lab's hardened config). A userspace consumer inside containd reads them and forwards them via Server-Sent Events to the backend, which republishes them on \`/api/substation/network-events\`.
2. **DPI events** — when a rule has \`dpiMode: enforce\` set, the packet is also queued to userspace via **NFQUEUE** (group 101 in the lab). The DPI engine parses the payload, applies the predicate, and emits a structured event with the verdict and the protocol-specific reason.

### Reading a Row

Each row in the strip looks roughly like:

\`\`\`
12:34:56.789  category: l4    verdict: DENY   src: 10.10.10.50  dst: 10.40.40.20  port: 502   rule: enterprise-deny
\`\`\`

The fields you actually care about for lab evidence:

- **timestamp** — when the packet arrived. Useful for correlating with your probe commands.
- **category** — either \`l4\` (decided by the kernel-side rule) or \`ics\` (decided by the userspace DPI verdict). The distinction is the lab's whole "L4 + DPI defense in depth" lesson.
- **verdict** — \`ACCEPT\` or \`DENY\`. (Plus \`BlockFlowTemp\` for the case where DPI temporarily blocks a flow to throttle abuse.)
- **src / dst / port** — the L4 5-tuple.
- **rule** — the rule ID that matched. Cross-reference against the active policy's JSON to see what the rule actually allows.
- **attributes** — for DPI events, the protocol-specific reason: \`functionCode: 5\` for a Modbus FC5 reject, \`crobOpCode: trip\` for a DNP3 trip reject, etc.

### Common Patterns in the Lab

**An L4 deny on a hardened policy.** You ran an enterprise → field probe. The kernel dropped the packet before DPI was reached. You see:

\`\`\`
category: l4   verdict: DENY   src: 10.10.10.50   dst: 10.40.40.20   port: 502   rule: enterprise-deny
\`\`\`

That is the L4 source-pin in action.

**An ICS DPI deny on a partially-allowed flow.** You ran a probe from a source the L4 rule allows but the DPI rule does not (e.g., an eng-ws-to-RTAC Modbus write). You see:

\`\`\`
category: l4   verdict: ACCEPT   src: 10.20.20.20   dst: 10.30.30.20   port: 502   rule: eng-ws-to-rtac
category: ics  verdict: DENY    src: 10.20.20.20   dst: 10.30.30.20   port: 502   rule: rtac-dpi  attributes: { functionCode: 5 }
\`\`\`

Both rows appear because the packet passed L4 and then got rejected by DPI. This is the *exact* condition the lab's "DPI matters even when L4 is loose" hint block in Lab 2.3 is trying to make visible.

**Lab 2.4 evidence assembly.** When you assemble the evidence package, run your negative-test probes one at a time and screenshot or copy out the matching event rows. The change board sees both the policy intent (your \`student-policy.json\` export) and the policy reality (these event rows). Two-source attestation.

### When the Strip Stays Empty

If you fire a probe and no row appears, the usual causes are:

- The probe never actually hit containd (kernel did not see it — check your source). If you probed from the wrong container, the packet may have stayed intra-zone.
- The matching rule has logging disabled. Lab policies enable logging on the deny-class rules; if you have applied your own custom policy without logging, your rules may match without emitting events.
- The \`nflogGroup\` is not set on the active profile. The lab's improved policy sets \`dataplane.nflogGroup: 100\`; if you exported and re-imported a policy that lost that key, the L4 events never reach containd's consumer.
- The frontend strip is filtered. The drawer has a category-and-verdict filter at the top; make sure it is not filtering out the rows you want to see.

The strip is the closest thing the lab has to a real-time firewall watch. Use it.`,
      },
      {
        id: "how-containd-enforces-policy",
        title: "How containd Enforces Policy (Kernel-Level View)",
        body: `For students curious about how the firewall actually works under the hood — what happens between "click Apply Hardened" in the lab UI and a TCP packet getting dropped — this article is the tour. It is not strictly necessary for working through the labs, but it pays off when you are reading containd events, debugging policy edge cases, or evaluating other ICS firewalls against containd.

### The Layered Stack

containd is built on **nftables** (the Linux kernel's modern packet-filtering framework, the successor to iptables). On top of nftables, containd adds:

- A **policy compiler** that translates the JSON policy file (zones, rules, ICS predicates) into a set of nftables rules
- A **DPI userspace engine** that handles per-protocol packet inspection
- An **events pipeline** that emits decisions back up to user space

The interesting layers are the kernel-userspace boundary and how packets actually move between them.

### Compilation: From JSON to nftables

When you POST a policy to \`/api/v1/policy/import\` (or use the web UI), containd parses the JSON and generates the corresponding nftables ruleset. A \`Allow enterprise → vendor SSH\` rule becomes something like \`tcp dport 22 ip saddr 10.10.10.0/24 ip daddr 10.20.20.0/24 accept\` in nftables syntax. Rules are organized into chains by zone (input from wan, forward wan → dmz, etc.) and applied with \`nft replace ruleset\` so the swap is atomic.

You can see the compiled ruleset by running \`nft list ruleset\` inside the \`fw-1\` container (Lab 2.4 evidence-package guidance has examples). It looks like a normal nftables output, with extra named sets for the things containd needs to look up dynamically (\`block_flows\` for DPI-blocked source/dest pairs, \`learn_flows\` for learn-mode observations).

### The L4 Path (Fast)

A packet arrives at containd's network interface. The kernel runs it through the compiled nftables rules:

1. Match the source/destination IP and port against the chain for that direction.
2. If a rule matches:
   - \`accept\` → packet is forwarded out the egress interface
   - \`drop\` → packet is silently discarded
   - \`reject\` → packet is dropped and a TCP RST or ICMP unreachable is sent back
3. If the rule has logging enabled, copy the packet header to **nflog group N** (the kernel's userspace-logging facility). containd's userspace consumer reads from nflog and emits an event.

This whole path runs in kernel space at line rate. Latency is sub-microsecond per packet. Almost all packets in the lab take this fast path.

### The DPI Path (Slower, More Powerful)

For rules with \`dpiMode: enforce\`, the rule's action is instead **\`queue num N\`** — the kernel hands the packet to userspace via the **NFQUEUE** netfilter facility on queue number N.

1. The kernel queues the packet and stalls the flow waiting for a verdict.
2. containd's DPI engine reads the packet from the queue (\`AF_NETLINK\` socket, \`NFQUEUE\` protocol).
3. The engine identifies the protocol (Modbus on 502, DNP3 on 20000), parses the payload, and applies the rule's ICS predicate (\`functionCode in [1,3,4,5,6]\`, \`crobOpCode != trip\`, etc.).
4. The engine writes a verdict back to the kernel: \`NF_ACCEPT\` lets the packet through, \`NF_DROP\` drops it, \`NF_QUEUE\` re-queues it for further inspection.
5. The DPI engine also emits an event for the operator-visible record.

DPI latency is microseconds-to-milliseconds per packet — fine for SCADA polling cycles (which are seconds-class), nowhere near fast enough for IEC 61850 GOOSE (which is microsecond-class).

### Events: nflog and DPI Both Feed One Stream

Both the kernel-side nflog and the userspace DPI engine emit events to containd's internal event store. The store deduplicates and timestamps them, then publishes them via Server-Sent Events on the containd REST API. RangerDanger's backend subscribes to that stream and re-publishes via \`/api/substation/network-events\`. The frontend reads from there and renders the Live DPI Events strip. This is the chain from \`packet hit on the kernel\` to \`row appears in the strip\` — roughly 100–500 ms end to end depending on event backlog.

### Why This Layered Approach

The fast L4 path lets containd handle line-rate traffic for the 99% of cases where the source/destination/port pair already decides the verdict. The DPI path handles the remaining 1% where the protocol payload matters. Pure-userspace firewalls (Snort, Suricata in inline mode) have lower throughput because every packet pays the user-kernel crossing cost. Pure-kernel firewalls (basic iptables) cannot inspect protocol payloads at all. The hybrid model is why containd can do ICS DPI on production traffic without becoming the bottleneck.`,
      },
    ],
  },

  /* ====== ICS Threats & Operational Practice ====== */
  {
    heading: "ICS Threats & Operational Practice",
    description:
      "Real ICS incidents, the OT kill chain, living off the land, change management, and what outages actually cost. Why this matters beyond the lab.",
    icon: "target",
    accent: "rose",
    articles: [
      {
        id: "ot-kill-chain",
        title: "The OT Kill Chain (Mitre ATT&CK for ICS + Real Incidents)",
        body: `The exercises in this lab are not hypothetical attack patterns. They mirror real ICS incidents that have happened to real utilities. This article walks through the framework that organizes those incidents (Mitre ATT&CK for ICS) and the specific historical events that map most directly to the lab.

### Mitre ATT&CK for ICS

[Mitre ATT&CK for ICS](https://attack.mitre.org/matrices/ics/) is a knowledge base of adversary techniques observed in industrial control system intrusions. It is the OT counterpart to the more widely-known Enterprise ATT&CK and is structured as a matrix of **tactics** (the attacker's goals) and **techniques** (how they achieve those goals).

The tactics in the ICS matrix include: Initial Access, Execution, Persistence, Privilege Escalation, Evasion, Discovery, Lateral Movement, Collection, Command and Control, **Inhibit Response Function**, **Impair Process Control**, and **Impact** (loss of view, loss of control, loss of availability, denial of safety, damage to property). The Inhibit Response, Impair Process Control, and Impact tactics are the ICS-specific column the matrix adds beyond Enterprise ATT&CK — these are the things an attacker can do *only* if they have reached the OT environment.

### The Real Incidents the Lab Mirrors

**Industroyer (CRASHOVERRIDE) — Ukrainian Substation, December 2016.** Sandworm operators used stolen credentials to pivot from IT into a Kyiv-area transmission substation's OT network, then ran custom malware (Industroyer) that spoke IEC 60870-5-101 / -104, IEC 61850, and OPC DA natively. The malware opened breakers, causing a brief regional outage. The attack pattern — IT-to-OT lateral movement, then a protocol-aware command against field devices — is the same shape as the lab's Lab 2.3 (DNP3 Direct Operate against the recloser).

**Industroyer2 — Ukrainian Substation, April 2022.** A follow-on Sandworm campaign targeted Ukrainian high-voltage substations with a refined version of Industroyer focused on IEC 60870-5-104. Intercepted and disrupted before causing significant impact. Same shape, same lesson, with the addition that ICS-aware perimeter monitoring (which the targeted utility had deployed by then) was a meaningful part of the defense.

**Triton / TRISIS — Petrochemical SIS, Saudi Arabia, 2017.** The TEMP.Veles operators (sometimes attributed to Russia's TsNIIKhM) used a long IT compromise to reach a petrochemical plant's safety instrumented system (SIS), then deployed Triton, the first publicly-known malware designed to manipulate a safety controller (Triconex). The attack was discovered when the Triconex tripped the plant — possibly due to a Triton bug. This is an "Impact: Denial of Safety" event in the ATT&CK matrix. The lab does not simulate SIS specifically, but the IT-to-OT lateral path leading to a protocol-aware controller attack is the same pattern.

**Stuxnet — Iranian Centrifuge Cascades, ~2009–2010.** The original public ICS attack. US/Israeli operators (according to the public attribution) used USB-borne malware to reach Siemens S7-315 PLCs controlling uranium centrifuges, then issued protocol-level write commands that subtly damaged the centrifuges over months. The Inhibit Response Function tactic — Stuxnet manipulated the HMI to show normal operations while damage was occurring — is one of the most influential moves in the ATT&CK matrix. The lab does not simulate HMI manipulation directly, but the "by the time the field device sees the command, it looks legitimate" lesson from Lab 2.3-bonus is the same observation.

**Volt Typhoon — Critical Infrastructure Pre-positioning, 2023–2024.** PRC-linked operators pre-positioned access to US critical-infrastructure OT networks (water, electric, transportation) without immediately causing impact. CISA and partner agencies published advisories detailing living-off-the-land tradecraft (LOTL TTPs) — the threat surface is not "attackers run novel malware" but "attackers use legitimate Windows admin tools to live inside OT networks for months." This is the lab's [Living off the Land in OT](#living-off-the-land-ot) lesson made historical.

:::warning Industroyer and Lab 2.3 are the same attack shape
The 2016 Kyiv substation outage was Sandworm sending IEC 60870-5-104 / DNP3 command sequences from a compromised IT host into the OT zone. The Lab 2.3 primary attack — kali firing \`dnp3cmd ... crob 0 trip\` against the recloser from the enterprise zone — is the same shape with the same root cause: cross-zone control surface that was not denied at the perimeter. When a student asks "is this a real thing?" the answer is yes, with a CISA advisory and an attributed threat actor.
:::

### How the Lab Exercises Map to ATT&CK

| Lab step | ATT&CK technique |
|---|---|
| Lab 2.3 unauthorized DNP3 Direct Operate against recloser | T0859 Valid Accounts (RTAC source spoofing) + T0855 Unauthorized Command Message |
| Lab 2.3 Modbus FC5 breaker trip | T0855 Unauthorized Command Message + T0879 Damage to Property (if it persisted) |
| Lab 2.3 Modbus FC6 regulator tap override | T0836 Modify Parameter |
| Lab 2.3-bonus RDP pivot through vendor-jump | T0817 Drive-by Compromise → T0822 External Remote Services → T0859 Valid Accounts |
| Lab 1.2 baseline traffic capture (defender side) | The defender-side counterpart to T0801 Monitoring (which an attacker also does for reconnaissance) |

When you walk a workshop attendee through Lab 2.3 and they ask "is this a real thing?" — yes. The Industroyer pattern. Show them the ATT&CK matrix entry for [Impair Process Control / Unauthorized Command Message](https://attack.mitre.org/techniques/T0855/) and the [Industroyer threat group page](https://attack.mitre.org/software/S0604/). That contextualization changes how seriously students take the exercise.

### See Also

- [Living off the Land in OT](#living-off-the-land-ot) — Volt Typhoon's signature pattern explained
- [Vendor Remote Access Patterns](#vendor-remote-access) — the entry vector for several real incidents
- [What is ICS DPI?](#ics-dpi) — the defensive control that closes the unauthorized-command-message attack
- [Change Management for Substation Firewall Rules](#change-management-firewall-rules) — how the defensive change actually gets approved at a utility`,
      },
      {
        id: "living-off-the-land-ot",
        title: "Living off the Land in OT",
        body: `**Living off the Land (LOTL)** is the attacker tradecraft of using tools and credentials that already exist in the target environment rather than introducing custom malware. In OT specifically, this means using the legitimate OT tools that engineering workstations and vendor jump hosts already have installed — \`mbpoll\`, \`dnp3poll\`, vendor configuration utilities, RDP, native SCADA functions — to accomplish the attacker's goals without ever dropping a file that an antivirus might catch.

### Why OT Attackers Love LOTL

Several reasons converge:

- **Detection.** Antivirus and endpoint detection are vastly weaker in OT than in IT. But the things that DO exist (network monitoring, anomalous-traffic detection) are even more allergic to *new* binaries than to anomalous network use of existing binaries. Using \`mbpoll\` does not flag any AV alert anywhere.
- **Persistence.** Custom malware needs persistence mechanisms. Legitimate tools are already persistent — they are part of the engineering workstation's image.
- **Plausible deniability.** A protocol-level attack that uses \`mbpoll\` and a real RTU's source IP looks indistinguishable from legitimate operator activity. Attribution is harder.
- **Lateral movement.** A compromised engineering workstation has the credentials, network paths, and tools to reach everything an engineer reaches. The attacker inherits the engineer's authorization scope without needing to escalate.

### What the Tools Look Like

Some of the legitimate tools commonly abused in OT environments:

- **\`mbpoll\`, \`mbtget\`, \`MBASE\`** — Modbus clients shipped with most engineering Linux distros and many Windows ICS toolkits.
- **\`dnp3poll\`, \`dnp3cmd\`, OpenDNP3, SEL AcSELerator\`** — DNP3 clients, some commercial, some open-source.
- **\`xfreerdp\`, \`mstsc\`, VNC clients** — used for vendor jump-host pivots (Lab 2.3-bonus).
- **\`psexec\`, \`wmiexec\`, \`PowerShell Remoting\`** — Windows admin tools that attackers use to lateralize after reaching an engineering workstation.
- **Vendor configuration utilities** — SEL AcSELerator Architect, ABB MicroSCADA, Siemens TIA Portal, Rockwell Studio 5000, Wonderware InTouch. Each is a legitimate engineering tool that, in attacker hands, becomes a control-system command-line interface.

### Why Behavioral Detection Beats Signature Detection in OT

Signature-based detection (this hash is bad, this string is bad, this binary is malware) does not work on LOTL because the binary is legitimate. What works is **behavioral detection** — flagging traffic patterns and command sequences that legitimate activity should not exhibit.

In OT specifically, behavioral detection is plausible *because* legitimate behavior is so constrained. The RTAC polls the recloser every 5 seconds via DNP3 reads. Nothing else should be sending DNP3 to the recloser. A single DNP3 packet from the engineering workstation to the recloser at 02:47 on a Tuesday is anomalous on its face. You do not need to know whether the engineering workstation's binary is malicious to know that this packet is.

This is why the lab's defense-in-depth lesson stresses both segmentation (source-pin: only the RTAC can reach the field) AND monitoring (every cross-zone packet shows up in the Live DPI Events strip). Segmentation removes the easy paths; monitoring catches what slips through. Antivirus does not enter the conversation.

### How the Lab's Attacks Use LOTL

Every attack in Lab 2.3 and 2.3-bonus uses LOTL by design. \`mbpoll\` and \`dnp3cmd\` are real OT tools that any engineering workstation has. \`xfreerdp\` is a legitimate desktop client. \`sshpass\` is a legitimate scripting tool. None of these are malware. They are exactly what a vendor's engineer would use to do legitimate work — and exactly what an attacker who has compromised that engineer's workstation would use to do illegitimate work.

The pedagogical point: when you tell students "the firewall has to deny enterprise → field on Modbus," it is not because there is a particular piece of malware to block. It is because the *legitimate* tool, used from the *wrong* place, is the attack. Network policy is the right place to draw the line; endpoint malware detection is not where this fight is fought.

### Mitigation Posture

For OT environments specifically, the LOTL-mitigation posture looks like:

- **Strict network segmentation** with per-conduit allow rules (no broad zone allows)
- **Source-pinning** so a tool used from the wrong place is denied at L4
- **DPI for protocol surfaces** so the right tool used the wrong way is denied at L7
- **Behavioral monitoring** for traffic that does not match the baseline operational pattern
- **Just-in-time vendor access** (open the path for a maintenance window, close it after)
- **Per-session audit** so an investigation can pin actions to a specific human and timeframe

The lab exercises the first three. The remaining items are organizational practice that no docker compose can simulate, but they are where the real defense lives.`,
      },
      {
        id: "change-management-firewall-rules",
        title: "Change Management for Substation Firewall Rules",
        body: `Lab 2.4's evidence package is not a lab artifact for its own sake. It is a deliberately compressed version of what a real utility *change board* expects to see before approving a segmentation change. This article explains what a change board actually is, what they want, and how the lab's outputs map to that.

### What a Utility Change Board Is

A change board (sometimes called CAB — Change Advisory Board) is the operational governance body that reviews and approves changes to production systems. In OT environments these reviews are more stringent than in IT because:

- **Outages cost money and reputation** (the customer-service tile in the lab's HMI is the operational version of this — minutes of outage, customers affected, regulatory implications).
- **Some changes can damage equipment** (a misconfigured firewall rule that prevents the RTAC from reaching the recloser will fail to recover from a fault, which can damage the recloser or the upstream feeder).
- **Some are regulated.** NERC CIP-005 R1 requires utilities to document and review electronic-security-perimeter changes; CIP-010 requires baseline configurations and change-management evidence. A change board's records are part of the audit trail the regulator will eventually inspect.

A typical change board for a substation firewall change includes: a control-room operator (verifies operations impact), a protection engineer (verifies the change does not break protection coordination), a cybersecurity engineer (verifies the security posture is improved or unchanged), and an IT/network engineer (verifies the firewall configuration itself). The board reviews the proposed change package, asks questions, and votes to approve, defer, or reject.

### What the Change Package Has to Contain

A complete substation-firewall change package usually includes:

1. **Statement of intent.** What problem is this change solving? What is the operational or security gap? Why now?
2. **Configuration diff.** The current policy, the proposed policy, and the line-by-line difference. For a containd policy, this is the JSON diff plus the human-readable summary of "rule X added, rule Y removed, rule Z modified."
3. **Test evidence.** Proof that the proposed policy was tested before being proposed for production. For each new or modified rule: a positive test (the legitimate traffic still works) and a negative test (the previously-allowed bad traffic is now blocked). The evidence is typically a combination of PCAP captures, firewall logs, and screenshots of the test result.
4. **Rollback plan.** If the change goes badly in production, how do you revert? For a containd change, this is "restore the prior policy file, run \`containd cli> import config\`, verify the active policy hash matches the previous version."
5. **Maintenance window.** When will the change be applied? What other activities are scheduled in the same window? What is the impact on operations during the change itself (does the firewall need to be restarted)?
6. **Post-change monitoring plan.** What will you watch for in the hours after the change to confirm operations are healthy? Which dashboards, which alarms, which log queries?

### How the Lab Maps to This

Lab 2.4's evidence-assembly step produces almost exactly this package:

- **Statement of intent** — Lab 1.3's design verdicts and Lab 1.4's plan together are the *intent* document. The student wrote them.
- **Configuration diff** — \`containd cli> export config > student-policy.json\` is the proposed policy. The weak baseline is the current. The diff is \`diff substation-weak.json student-policy.json\`.
- **Test evidence** — Lab 2.4's positive-tests step is the legitimate-traffic-works half. The negative-tests step is the bad-traffic-blocked half. Both produce log entries on the Live DPI Events strip that constitute the firewall-log evidence.
- **PCAP** — Lab 2.4's PCAP-capture step writes \`/data/captures/validation.pcap\` showing only RTAC sources reaching field on Modbus/DNP3 after the policy is applied. This is direct network-level evidence.
- **Audit log** — \`containd cli> show audit\` snapshots the \`config.commit\` entries that prove the policy was actually applied.

A real change board would expect more (rollback plan, maintenance window, monitoring plan), but the lab covers the *technical* portion of the evidence package end to end. The operational portions (when, who, what else) are organizational practice no lab can simulate.

### Why This Matters Beyond the Lab

Most cyber-trained workshop attendees never see a change-board package and have never had to defend a firewall change against operators who would rather not change anything. The lab compresses this experience into a 15-minute exercise on purpose. Walking out of the workshop with one assembled evidence package in your Exercise Notes gives you a tangible template to point at when your real organization asks "what would a good change package look like?"

The \`scripts/validation-report.sh\` helper produces a markdown deliverable equivalent to the manually-assembled package — operator-facing rather than student-facing, suitable for attaching directly to a change request.`,
      },
      {
        id: "outage-costs-saidi-saifi",
        title: "What Outages Cost (SAIDI, SAIFI, and the Customer-Service Tile)",
        body: `The customer-service tile on the lab's Feeder HMI shows "ALL CUSTOMERS WITHOUT POWER" or "N kW serving M customers" depending on the feeder state. This is not just flavor. It is the lab's representation of the metrics utilities actually report to their regulators — and the metrics a cyber attack against a substation would actually move.

### The Standard Reliability Metrics

Utilities report distribution-system reliability using two widely-used indices:

- **SAIDI** (System Average Interruption Duration Index) — the *average* total outage time per customer per year. If a utility serves a million customers and the total outage-customer-minutes across the year was 100 million, SAIDI = 100 minutes per customer.
- **SAIFI** (System Average Interruption Frequency Index) — the *average* number of outage events per customer per year. SAIFI = 1.5 means the average customer experiences 1.5 outage events per year.

A third commonly-tracked metric is **CAIDI** (Customer Average Interruption Duration Index) — the average outage duration per outage event = SAIDI / SAIFI. It is what your average outage feels like to a customer, in minutes.

These are reported to **state-level public utility commissions** in the US (each state has its own) and equivalent regulators in other countries. They are public information. They are tracked over time. They feed rate-case decisions, performance-based ratemaking, and utility executive bonuses. They are not abstract.

### Typical Values

For a North American distribution utility:

- SAIDI around **90 to 150 minutes per year** is normal in good weather years.
- Major-event days (hurricanes, ice storms) push SAIDI higher; sometimes the regulator excludes them from the official reporting figure to avoid penalizing for weather.
- The best-performing utilities in fair-weather climates run SAIDI under 60 minutes. Long-rural utilities in harsh climates can be 200+.
- SAIFI typically runs **0.7 to 1.5 events per year per customer**.

A single substation event that takes 1,000 customers offline for 60 minutes contributes 60,000 customer-minutes — visible in the SAIDI calculation. A whole feeder event taking 10,000 customers offline for 4 hours is 2.4 million customer-minutes — the kind of event that triggers an executive after-action review.

### How Outage Costs Get Quantified

Beyond the regulatory reporting, utilities calculate the economic cost of outages for cost-benefit analysis of grid investments. The standard reference is the **DOE Interruption Cost Estimator (ICE) Calculator** (Berkeley Lab). It uses customer-survey-derived willingness-to-pay-to-avoid-outage data to produce per-event cost estimates:

- A 1-hour outage for a typical small commercial customer: roughly **\\$200 to \\$500**
- A 1-hour outage for a medium industrial customer: **\\$5,000 to \\$30,000** depending on production sensitivity
- A 1-hour outage for a hospital or fire station: **incalculable in the direct dollar sense**, which is why the lab calls these out separately on the customer-service tile

For a 1,000-customer-hour outage event across a typical mix, the all-in economic cost is in the **\\$200,000 to \\$1,000,000** range. This is before regulatory penalties.

### Cyber Attacks vs. Weather as Outage Causes

For context: about **70% of customer-minutes of outage** at a typical distribution utility comes from weather (storms, ice, lightning, vegetation contact). The next-largest categories are equipment failure, animal contact, and human error. Cyber attack is, today, a tiny fraction of the actual outage tally.

But the cyber-attack risk profile is different from the weather risk profile in two important ways:

1. **Concurrency.** A targeted cyber attack can take down many substations *simultaneously*, in a way that weather events at this scale (regional ice storm, hurricane) usually require luck or a multi-day weather pattern. A coordinated Industroyer-class attack could in principle affect tens of substations in a window of minutes.
2. **Cascading.** A weather event is bounded by the geography of the weather. A cyber event is bounded by the attacker's reach — which, if the IT-OT boundary is porous, can be region-wide or larger.

These two characteristics are why utility executives and regulators care about cyber attacks against distribution far in excess of the historical-outage-share argument. The risk is not what cyber attacks *have* done; it is what cyber attacks *could* do under a coordinated campaign.

### How the Customer-Service Tile Maps to All This

The customer-service tile is the lab's representation of the operator's awareness of outage impact. When the tile flips from "120 kW serving ~200 customers" to "ALL CUSTOMERS WITHOUT POWER," that is the operational visibility a real control-room operator has. The lab's "hospital and fire station without power" annotation is the *critical-load* category — the loads whose outage triggers an immediate emergency response.

When you tell a workshop attendee "this single packet caused a complete feeder outage," you can put a dollar figure on it: 200 customers × 1 hour at typical mix ≈ **\\$50,000 to \\$200,000** of economic impact, plus the regulatory reporting visibility, plus the (incalculable) critical-services consequences. The cyber-attack-against-substation threat is not theoretical. The lab does not simulate the economics directly, but the customer-service tile is the bridge to that framing.`,
      },
    ],
  },
];

/* ------------------------------------------------------------------ */
/*  Component                                                          */
/* ------------------------------------------------------------------ */

/* ------------------------------------------------------------------ */
/*  Helpers                                                            */
/* ------------------------------------------------------------------ */

/**
 * Per-section accent palette. Each color name maps to a bundle of
 * Tailwind class strings used by the landing tiles, article cards,
 * search results, and the article reading view's section header.
 *
 * Design intent: each section reads as visually distinct from the
 * landing page (you can see at a glance which tile is which), and
 * that same color identity follows the article through category and
 * reading views.
 */
const ACCENT_CLASSES: Record<
  string,
  {
    ring: string;        // hover-state border
    chip: string;        // small section pill background
    chipText: string;
    iconBg: string;      // 40x40 icon container background
    iconText: string;
    cardBg: string;      // landing-tile background (subtle accent tint)
    cardBorder: string;  // landing-tile resting border
    leftBar: string;     // 4px left bar color for article cards
    topBar: string;      // wide top bar inside article reading view
    headerWash: string;  // soft wash behind the article header
  }
> = {
  sky: {
    ring: "hover:border-sky-500",
    chip: "bg-sky-900/60",
    chipText: "text-sky-100",
    iconBg: "bg-sky-500/25",
    iconText: "text-sky-200",
    cardBg: "bg-gradient-to-br from-sky-950/60 via-slate-900/80 to-slate-900/60",
    cardBorder: "border-sky-800/60",
    leftBar: "border-l-sky-500",
    topBar: "bg-sky-500",
    headerWash: "bg-gradient-to-b from-sky-950/40 to-transparent",
  },
  emerald: {
    ring: "hover:border-emerald-500",
    chip: "bg-emerald-900/60",
    chipText: "text-emerald-100",
    iconBg: "bg-emerald-500/25",
    iconText: "text-emerald-200",
    cardBg: "bg-gradient-to-br from-emerald-950/60 via-slate-900/80 to-slate-900/60",
    cardBorder: "border-emerald-800/60",
    leftBar: "border-l-emerald-500",
    topBar: "bg-emerald-500",
    headerWash: "bg-gradient-to-b from-emerald-950/40 to-transparent",
  },
  violet: {
    ring: "hover:border-violet-500",
    chip: "bg-violet-900/60",
    chipText: "text-violet-100",
    iconBg: "bg-violet-500/25",
    iconText: "text-violet-200",
    cardBg: "bg-gradient-to-br from-violet-950/60 via-slate-900/80 to-slate-900/60",
    cardBorder: "border-violet-800/60",
    leftBar: "border-l-violet-500",
    topBar: "bg-violet-500",
    headerWash: "bg-gradient-to-b from-violet-950/40 to-transparent",
  },
  amber: {
    ring: "hover:border-amber-500",
    chip: "bg-amber-900/60",
    chipText: "text-amber-100",
    iconBg: "bg-amber-500/25",
    iconText: "text-amber-200",
    cardBg: "bg-gradient-to-br from-amber-950/60 via-slate-900/80 to-slate-900/60",
    cardBorder: "border-amber-800/60",
    leftBar: "border-l-amber-500",
    topBar: "bg-amber-500",
    headerWash: "bg-gradient-to-b from-amber-950/40 to-transparent",
  },
  slate: {
    ring: "hover:border-slate-500",
    chip: "bg-slate-700/60",
    chipText: "text-slate-100",
    iconBg: "bg-slate-500/25",
    iconText: "text-slate-200",
    cardBg: "bg-gradient-to-br from-slate-800/60 via-slate-900/80 to-slate-900/60",
    cardBorder: "border-slate-700/60",
    leftBar: "border-l-slate-400",
    topBar: "bg-slate-400",
    headerWash: "bg-gradient-to-b from-slate-800/40 to-transparent",
  },
  rose: {
    ring: "hover:border-rose-500",
    chip: "bg-rose-900/60",
    chipText: "text-rose-100",
    iconBg: "bg-rose-500/25",
    iconText: "text-rose-200",
    cardBg: "bg-gradient-to-br from-rose-950/60 via-slate-900/80 to-slate-900/60",
    cardBorder: "border-rose-800/60",
    leftBar: "border-l-rose-500",
    topBar: "bg-rose-500",
    headerWash: "bg-gradient-to-b from-rose-950/40 to-transparent",
  },
};

const accentOf = (s: Section) => ACCENT_CLASSES[s.accent || "slate"] || ACCENT_CLASSES.slate;

function SectionIcon({ name, className }: { name?: IconName; className?: string }) {
  const cls = className || "h-5 w-5";
  switch (name) {
    case "zap":     return <Zap     className={cls} />;
    case "shield":  return <Shield  className={cls} />;
    case "radio":   return <Radio   className={cls} />;
    case "wrench":  return <Wrench  className={cls} />;
    case "layers":  return <Layers  className={cls} />;
    case "target":  return <Target  className={cls} />;
    default:        return <BookOpen className={cls} />;
  }
}

function stripMarkdownForExcerpt(md: string): string {
  // Strip code fences, headings, link syntax, emphasis markers, bullet markers,
  // and HTML for a clean snippet — first paragraph only.
  const noFences = md.replace(/```[\s\S]*?```/g, "");
  const firstPara = noFences.split(/\n\n+/).find((p) => p.trim().length > 0) || "";
  return firstPara
    .replace(/^#{1,6}\s+/gm, "")
    .replace(/[*_`>~]/g, "")
    .replace(/\[([^\]]+)\]\([^)]+\)/g, "$1")
    .replace(/\s+/g, " ")
    .trim();
}

function readingMinutes(md: string): number {
  const words = md.replace(/```[\s\S]*?```/g, "").split(/\s+/).filter(Boolean).length;
  return Math.max(1, Math.round(words / 220));
}

interface SearchHit {
  article: Article;
  section: Section;
  snippet: string;
}

function buildSnippet(body: string, q: string): string {
  const lower = body.toLowerCase();
  const i = lower.indexOf(q);
  if (i < 0) return stripMarkdownForExcerpt(body).slice(0, 180);
  const start = Math.max(0, i - 60);
  const end = Math.min(body.length, i + q.length + 120);
  const slice = body.slice(start, end).replace(/\s+/g, " ").trim();
  return (start > 0 ? "… " : "") + slice + (end < body.length ? " …" : "");
}

/* ------------------------------------------------------------------ */
/*  Admonitions + cross-link routing                                   */
/* ------------------------------------------------------------------ */

const ADMONITION_STYLES: Record<
  string,
  { border: string; bg: string; label: string; labelColor: string }
> = {
  tip:     { border: "border-l-emerald-500", bg: "bg-emerald-950/30", label: "TIP",     labelColor: "text-emerald-300" },
  note:    { border: "border-l-sky-500",     bg: "bg-sky-950/30",     label: "NOTE",    labelColor: "text-sky-300" },
  warning: { border: "border-l-amber-500",   bg: "bg-amber-950/30",   label: "WARNING", labelColor: "text-amber-300" },
  caution: { border: "border-l-rose-500",    bg: "bg-rose-950/30",    label: "CAUTION", labelColor: "text-rose-300" },
};

function Admonition({
  kind,
  title,
  children,
}: {
  kind: string;
  title?: string;
  children: React.ReactNode;
}) {
  const s = ADMONITION_STYLES[kind] || ADMONITION_STYLES.note;
  return (
    <div
      className={`my-5 overflow-hidden rounded-r-md border border-l-4 border-slate-800 ${s.border} ${s.bg} px-4 py-3`}
    >
      <div
        className={`mb-1.5 text-[10px] font-bold uppercase tracking-wider ${s.labelColor}`}
      >
        {s.label}
        {title ? ` · ${title}` : ""}
      </div>
      <div className="text-sm leading-relaxed text-slate-300">{children}</div>
    </div>
  );
}

/**
 * Split the article body on `:::tip|note|warning|caution [Title]\n…\n:::`
 * fences and render each segment via marked-react with internal-link routing
 * for `#article-id` href patterns.
 */
function renderArticleBody(
  body: string,
  linkRenderer: (href: string, text: React.ReactNode) => React.ReactElement,
): React.ReactNode[] {
  const segments: React.ReactNode[] = [];
  const re = /^:::(tip|note|warning|caution)(?:[ \t]+(.+))?\n([\s\S]+?)\n:::[ \t]*$/gm;
  let lastIdx = 0;
  let key = 0;
  let m: RegExpExecArray | null;
  while ((m = re.exec(body)) !== null) {
    if (m.index > lastIdx) {
      const md = body.slice(lastIdx, m.index);
      if (md.trim()) {
        segments.push(
          <Markdown key={key++} renderer={{ link: linkRenderer }}>
            {md}
          </Markdown>,
        );
      }
    }
    segments.push(
      <Admonition key={key++} kind={m[1]} title={m[2]}>
        <Markdown renderer={{ link: linkRenderer }}>{m[3]}</Markdown>
      </Admonition>,
    );
    lastIdx = m.index + m[0].length;
  }
  if (lastIdx < body.length) {
    const md = body.slice(lastIdx);
    if (md.trim()) {
      segments.push(
        <Markdown key={key++} renderer={{ link: linkRenderer }}>
          {md}
        </Markdown>,
      );
    }
  }
  return segments;
}

/* ------------------------------------------------------------------ */
/*  Component                                                          */
/* ------------------------------------------------------------------ */

type View =
  | { kind: "landing" }
  | { kind: "category"; section: Section }
  | { kind: "article"; section: Section; article: Article };

export default function KnowledgePage() {
  const [search, setSearch] = useState("");
  const [view, setView] = useState<View>({ kind: "landing" });

  const totalArticles = useMemo(
    () => sections.reduce((n, s) => n + s.articles.length, 0),
    [],
  );

  const articlesById = useMemo(() => {
    const m = new Map<string, { article: Article; section: Section }>();
    sections.forEach((s) =>
      s.articles.forEach((a) => m.set(a.id, { article: a, section: s })),
    );
    return m;
  }, []);

  const goArticle = (s: Section, a: Article) =>
    setView({ kind: "article", section: s, article: a });

  const linkRenderer = useCallback(
    (href: string, text: React.ReactNode): React.ReactElement => {
      // Internal cross-link: href like #article-id
      if (href.startsWith("#")) {
        const target = articlesById.get(href.slice(1));
        if (target) {
          return (
            <a
              href={href}
              onClick={(e) => {
                e.preventDefault();
                goArticle(target.section, target.article);
              }}
              className="text-sky-400 underline-offset-2 hover:underline"
            >
              {text}
            </a>
          );
        }
      }
      // External link: open in new tab
      return (
        <a
          href={href}
          target="_blank"
          rel="noopener noreferrer"
          className="text-sky-400 underline-offset-2 hover:underline"
        >
          {text}
        </a>
      );
    },
    [articlesById],
  );

  const hits: SearchHit[] = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return [];
    const out: SearchHit[] = [];
    for (const section of sections) {
      for (const article of section.articles) {
        const inTitle = article.title.toLowerCase().includes(q);
        const inBody = article.body.toLowerCase().includes(q);
        if (inTitle || inBody) {
          out.push({
            article,
            section,
            snippet: inBody
              ? buildSnippet(article.body, q)
              : stripMarkdownForExcerpt(article.body).slice(0, 180),
          });
        }
      }
    }
    return out;
  }, [search]);

  const goLanding = () => {
    setView({ kind: "landing" });
    setSearch("");
    if (typeof window !== "undefined" && window.location.hash) {
      history.replaceState(null, "", window.location.pathname);
    }
  };
  const goCategory = (s: Section) => setView({ kind: "category", section: s });

  // Deep-link support: read `#article-id` on mount and whenever the browser
  // hash changes (e.g., back/forward navigation). Lab YAMLs can now use
  // [link](/knowledge#article-id) and clicking jumps straight into the article.
  useEffect(() => {
    const apply = () => {
      if (typeof window === "undefined") return;
      const hash = window.location.hash.replace(/^#/, "");
      if (!hash) return;
      const target = articlesById.get(hash);
      if (target) {
        setView({
          kind: "article",
          section: target.section,
          article: target.article,
        });
      }
    };
    apply();
    window.addEventListener("hashchange", apply);
    return () => window.removeEventListener("hashchange", apply);
  }, [articlesById]);

  // Whenever view changes to article-mode, mirror the article id into the URL
  // hash so the address bar is shareable and the back button works. Hash
  // stripping on landing/category is handled by goLanding() / goCategory()
  // explicitly so this effect stays single-purpose.
  useEffect(() => {
    if (typeof window === "undefined") return;
    if (view.kind === "article") {
      const wanted = "#" + view.article.id;
      if (window.location.hash !== wanted) {
        history.replaceState(null, "", wanted);
      }
    }
  }, [view]);

  const isSearching = search.trim().length > 0;

  return (
    <main className="flex h-[calc(100vh-0px)] flex-col overflow-hidden bg-slate-950">
      {/* Header */}
      <header className="flex shrink-0 items-center gap-3 border-b border-slate-800 bg-slate-950/80 px-6 py-4">
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img
          src="/rook-quarter-turn-wink-transparent-web.png"
          alt="Rook"
          className="h-9 w-9 shrink-0"
        />
        <button
          onClick={goLanding}
          className="flex items-center gap-2 text-left transition-opacity hover:opacity-80"
          aria-label="Back to knowledge home"
        >
          <BookOpen className="h-5 w-5 text-sky-400" />
          <h1 className="text-lg font-semibold text-slate-100">
            Knowledge Base
          </h1>
        </button>
        <span className="text-xs text-slate-500">{totalArticles} articles</span>

        <div className="ml-auto flex items-center gap-2">
          {(view.kind !== "landing" || isSearching) && (
            <button
              onClick={goLanding}
              className="flex items-center gap-1.5 rounded-md border border-slate-700 bg-slate-900 px-2.5 py-1 text-xs text-slate-300 transition-colors hover:bg-slate-800 hover:text-slate-100"
            >
              <Home className="h-3.5 w-3.5" />
              Home
            </button>
          )}
        </div>
      </header>

      {/* Search bar */}
      <div className="shrink-0 border-b border-slate-800 bg-slate-950/60 px-6 py-3">
        <div className="relative mx-auto max-w-2xl">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-500" />
          <input
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search across all articles (titles and bodies)…"
            className="w-full rounded-lg border border-slate-700 bg-slate-900 py-2 pl-10 pr-4 text-sm text-slate-200 placeholder:text-slate-600 focus:border-sky-600 focus:outline-none focus:ring-1 focus:ring-sky-600"
          />
          {isSearching && (
            <button
              onClick={() => setSearch("")}
              className="absolute right-2 top-1/2 -translate-y-1/2 rounded px-2 py-0.5 text-xs text-slate-500 hover:text-slate-300"
              aria-label="Clear search"
            >
              clear
            </button>
          )}
        </div>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-y-auto px-6 py-8">
        <div className="mx-auto max-w-5xl">
          {/* SEARCH RESULTS — take precedence over any nav view */}
          {isSearching ? (
            <div>
              <h2 className="mb-1 text-sm font-semibold text-slate-200">
                {hits.length === 0
                  ? "No results"
                  : hits.length === 1
                    ? "1 result"
                    : `${hits.length} results`}{" "}
                <span className="text-slate-500">
                  for &ldquo;{search.trim()}&rdquo;
                </span>
              </h2>
              <p className="mb-6 text-xs text-slate-500">
                Searches every article title and body. Click a result to jump
                into the article.
              </p>

              {hits.length === 0 ? (
                <p className="py-10 text-center text-sm text-slate-500">
                  Try a different query, or{" "}
                  <button
                    onClick={() => setSearch("")}
                    className="text-sky-400 hover:underline"
                  >
                    clear the search
                  </button>{" "}
                  to browse topics.
                </p>
              ) : (
                <ul className="space-y-3">
                  {hits.map((hit) => {
                    const acc = accentOf(hit.section);
                    return (
                      <li key={hit.article.id}>
                        <button
                          onClick={() => {
                            setSearch("");
                            goArticle(hit.section, hit.article);
                          }}
                          className={`block w-full rounded-lg border border-l-4 ${acc.cardBorder} ${acc.leftBar} bg-slate-900/60 p-4 text-left transition-colors ${acc.ring}`}
                        >
                          <div className="mb-1 flex items-center gap-2">
                            <span
                              className={`inline-flex items-center gap-1 rounded-full ${acc.chip} px-2 py-0.5 text-[10px] font-medium uppercase tracking-wider ${acc.chipText}`}
                            >
                              <SectionIcon
                                name={hit.section.icon}
                                className="h-3 w-3"
                              />
                              {hit.section.heading}
                            </span>
                            <span className="flex items-center gap-1 text-[10px] text-slate-600">
                              <Clock className="h-3 w-3" />
                              {readingMinutes(hit.article.body)} min
                            </span>
                          </div>
                          <h3 className="text-sm font-semibold text-slate-100">
                            {hit.article.title}
                          </h3>
                          <p className="mt-1 line-clamp-2 text-xs text-slate-400">
                            {hit.snippet}
                          </p>
                        </button>
                      </li>
                    );
                  })}
                </ul>
              )}
            </div>
          ) : view.kind === "landing" ? (
            /* LANDING: hero + topic tiles */
            <div>
              <div className="mb-8">
                <h2 className="text-2xl font-semibold text-slate-100">
                  Explore the knowledge base
                </h2>
                <p className="mt-2 max-w-2xl text-sm leading-relaxed text-slate-400">
                  Background reading for the workshop labs. Pick a topic to
                  browse, or use the search bar above to jump straight to a
                  specific article. Each article is short enough to read in a
                  few minutes during an exercise.
                </p>
              </div>

              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
                {sections.map((section) => {
                  const acc = accentOf(section);
                  return (
                    <button
                      key={section.heading}
                      onClick={() => goCategory(section)}
                      className={`group relative flex flex-col rounded-xl border ${acc.cardBorder} ${acc.cardBg} p-5 text-left transition-all ${acc.ring} hover:shadow-lg hover:shadow-slate-950/50`}
                    >
                      <div
                        className={`mb-4 flex h-10 w-10 items-center justify-center rounded-lg ${acc.iconBg} ${acc.iconText}`}
                      >
                        <SectionIcon name={section.icon} className="h-5 w-5" />
                      </div>
                      <h3 className="text-base font-semibold text-slate-100">
                        {section.heading}
                      </h3>
                      <p className="mt-2 line-clamp-3 text-xs leading-relaxed text-slate-400">
                        {section.description || ""}
                      </p>
                      <div className="mt-4 flex items-center justify-between text-[11px]">
                        <span className="text-slate-500">
                          {section.articles.length} article
                          {section.articles.length === 1 ? "" : "s"}
                        </span>
                        <span className="flex items-center gap-1 text-slate-500 group-hover:text-slate-300">
                          Browse <ChevronRight className="h-3 w-3" />
                        </span>
                      </div>
                    </button>
                  );
                })}
              </div>

              <div className="mt-10">
                <h3 className="mb-3 text-xs font-semibold uppercase tracking-widest text-amber-400/80">
                  All articles, A–Z
                </h3>
                <ul className="columns-1 gap-x-8 sm:columns-2 lg:columns-3">
                  {[...sections]
                    .flatMap((section) =>
                      section.articles.map((article) => ({ article, section })),
                    )
                    .sort((a, b) => a.article.title.localeCompare(b.article.title))
                    .map(({ article, section }) => (
                      <li
                        key={article.id}
                        className="mb-2 break-inside-avoid text-xs leading-snug"
                      >
                        <button
                          onClick={() => goArticle(section, article)}
                          className="text-left text-slate-400 transition-colors hover:text-sky-300"
                        >
                          {article.title}
                        </button>
                      </li>
                    ))}
                </ul>
              </div>
            </div>
          ) : view.kind === "category" ? (
            /* CATEGORY: one section's articles, card-style */
            <div>
              <button
                onClick={goLanding}
                className="mb-4 flex items-center gap-1 text-xs text-slate-500 transition-colors hover:text-slate-300"
              >
                <ArrowLeft className="h-3 w-3" />
                All topics
              </button>
              <div className="mb-6 flex items-start gap-4">
                <div
                  className={`flex h-12 w-12 shrink-0 items-center justify-center rounded-lg ${accentOf(view.section).iconBg} ${accentOf(view.section).iconText}`}
                >
                  <SectionIcon name={view.section.icon} className="h-6 w-6" />
                </div>
                <div className="min-w-0">
                  <h2 className="text-2xl font-semibold text-slate-100">
                    {view.section.heading}
                  </h2>
                  <p className="mt-2 max-w-2xl text-sm leading-relaxed text-slate-400">
                    {view.section.description}
                  </p>
                  <p className="mt-3 text-[11px] text-slate-500">
                    {view.section.articles.length} article
                    {view.section.articles.length === 1 ? "" : "s"}
                  </p>
                </div>
              </div>

              <ul className="space-y-3">
                {view.section.articles.map((article) => {
                  const acc = accentOf(view.section);
                  return (
                    <li key={article.id}>
                      <button
                        onClick={() => goArticle(view.section, article)}
                        className={`block w-full rounded-lg border border-l-4 ${acc.cardBorder} ${acc.leftBar} bg-slate-900/60 p-4 text-left transition-colors ${acc.ring}`}
                      >
                        <div className="flex items-start justify-between gap-3">
                          <h3 className="text-sm font-semibold text-slate-100">
                            {article.title}
                          </h3>
                          <span className="flex shrink-0 items-center gap-1 text-[10px] text-slate-500">
                            <Clock className="h-3 w-3" />
                            {readingMinutes(article.body)} min
                          </span>
                        </div>
                        <p className="mt-1 line-clamp-2 text-xs text-slate-400">
                          {stripMarkdownForExcerpt(article.body).slice(0, 200)}
                        </p>
                      </button>
                    </li>
                  );
                })}
              </ul>
            </div>
          ) : (
            /* ARTICLE: clean reading view */
            <div>
              <nav className="mb-4 flex items-center gap-1.5 text-xs text-slate-500">
                <button
                  onClick={goLanding}
                  className="hover:text-slate-300"
                >
                  Knowledge
                </button>
                <ChevronRight className="h-3 w-3" />
                <button
                  onClick={() => goCategory(view.section)}
                  className="hover:text-slate-300"
                >
                  {view.section.heading}
                </button>
              </nav>

              <article className="mx-auto max-w-3xl">
                {/* Color identifier for the section: 3-px top accent bar
                    plus a soft wash behind the header zone. */}
                <div className="-mx-2 mb-6 overflow-hidden rounded-lg">
                  <div className={`h-1 w-full ${accentOf(view.section).topBar}`} />
                  <div
                    className={`${accentOf(view.section).headerWash} px-4 pb-5 pt-4`}
                  >
                    <div className="mb-3 flex items-center gap-2">
                      <span
                        className={`inline-flex items-center gap-1.5 rounded-full ${accentOf(view.section).chip} px-2.5 py-1 text-[10px] font-semibold uppercase tracking-wider ${accentOf(view.section).chipText}`}
                      >
                        <SectionIcon
                          name={view.section.icon}
                          className="h-3 w-3"
                        />
                        {view.section.heading}
                      </span>
                      <span className="flex items-center gap-1 text-[10px] text-slate-500">
                        <Clock className="h-3 w-3" />
                        {readingMinutes(view.article.body)} min read
                      </span>
                    </div>
                    <h2 className="text-3xl font-semibold leading-tight text-slate-100">
                      {view.article.title}
                    </h2>
                  </div>
                </div>
                <div className="knowledge-article text-[15px] leading-relaxed text-slate-300">
                  {renderArticleBody(view.article.body, linkRenderer)}
                </div>

                {view.section.articles.length > 1 && (
                  <div className="mt-12 border-t border-slate-800 pt-6">
                    <h3 className="mb-3 text-xs font-semibold uppercase tracking-widest text-amber-400/80">
                      More in {view.section.heading}
                    </h3>
                    <ul className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                      {view.section.articles
                        .filter((a) => a.id !== view.article.id)
                        .slice(0, 6)
                        .map((other) => (
                          <li key={other.id}>
                            <button
                              onClick={() => goArticle(view.section, other)}
                              className="flex w-full items-center gap-2 rounded-md border border-slate-800 bg-slate-900/60 px-3 py-2 text-left text-xs text-slate-300 transition-colors hover:border-slate-700 hover:text-slate-100"
                            >
                              <ChevronRight className="h-3 w-3 shrink-0 text-slate-600" />
                              <span className="truncate">{other.title}</span>
                            </button>
                          </li>
                        ))}
                    </ul>
                  </div>
                )}
              </article>
            </div>
          )}
        </div>
      </div>
    </main>
  );
}
