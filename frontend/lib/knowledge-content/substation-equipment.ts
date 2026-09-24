import type { Section } from "../knowledge-types";

export const substationEquipment: Section =
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

RangerDanger includes a dedicated capacitor bank simulator (**capbank-sim**, a 300 kVAR switched bank on the field network at 10.40.40.23). It appears in the substation HMI: the One-Line shows its switched-in/out state, **Supervisory Control** lets you Switch In / Switch Out and toggle Auto/Manual - the same Modbus and DNP3 paths an attacker would use, restricted by the containd firewall to the RTAC - and **Electrical Detail** shows its full status. Because the bank is wired into the OpenDSS power flow, switching it in raises the local voltage, drops feeder current, and corrects the power factor; switching it out reverses that. The switch-count lockout is a real protective feature an attacker can deliberately trip to disable the bank, exactly as described above. The same segmentation principle as the other field devices applies: restrict direct field-device access to the RTAC only.`,
      },
      {
        id: "substation-physics",
        title: "Distribution Feeder Physics for Tech Workers",
        body: `If you came to this lab from software, networking, or security rather than from a power-engineering background, you have probably wondered what is actually *happening* electrically when the HMI shows the critical-load voltage chip dropping from 120 V to 0 V. This article is a working mental model. Not enough to design a substation, but enough to understand why the numbers move the way they do, what OpenDSS is, and why it is the right engine for the job.

### The Three Quantities That Matter

A distribution feeder is a wire. Like any wire, three things are going on at once:

- **Voltage (V)** is the "pressure" pushing electricity along, measured in volts. The lab's feeder runs at **12.47 kV** (12,470 V line-to-line, 3-phase, 60 Hz, the standard North American distribution-primary voltage). Customer outlets see **120 V** after a final step-down transformer at the service drop.
- **Current (A)** is the *flow rate* through the wire, measured in amperes. More load, more current.
- **Power (kW)** is the product of voltage and current. In a balanced 3-phase system at line-to-line voltage V_LL, the math is **P = √3 × V_LL × I × pf** - the √3 factor comes from how the three phases add. For the lab's critical load (200 kW, 0.95 pf, 12.47 kV LL), that works out to about **9.7 A** on the feeder. Served the same way at residential voltage instead, the current would be vastly higher and the conductor correspondingly thicker. This is why utilities transmit at high voltage and step it down at the customer's transformer: for the same power, higher voltage means lower current means smaller conductor and less line loss.

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
- A **critical load** of 200 kW at 0.95 power factor - the "hospital and fire station" load on the customer-service tile

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

- [HMI, SCADA, and the Lab's Substation Panel](#hmi-scada-fuxa) - where the numbers from this article get rendered
- [Power Factor and Reactive Power](#power-factor-reactive-power) - the inductive / capacitive side of the math
- [Reclosers](#reclosers), [Protective Relays](#protective-relays), [Voltage Regulators](#voltage-regulators) - the devices whose state OpenDSS reads
- [The OpenDSS project at EPRI](https://www.epri.com/pages/sa/opendss) - the engine itself`,
      },
      {
        id: "hmi-scada-fuxa",
        title: "HMI, SCADA, and the Lab's Substation Panel",
        body: `**HMI** (Human-Machine Interface) is the operator's view of an industrial process. In a substation control room the HMI is the screen that shows breakers as open / closed circles, voltages as numeric tiles, and alarms as colored banners. It is what the operator looks at all day.

**SCADA** (Supervisory Control and Data Acquisition) is the larger system: the data-acquisition infrastructure that polls field devices, normalizes their values, stores them in a historian, and forwards selected operator commands back down to the equipment. SCADA is the *plumbing*; the HMI is the *screen*.

**DCS** (Distributed Control System) is the related architecture for plant-wide automation (refineries, chemical plants) where the control logic itself is distributed among many controllers. In substations the architecture is usually called SCADA rather than DCS, but the device-and-protocol surface looks similar.

:::tip The lab's primary HMI is at /substation
Throughout the workshop, every reference to "the Feeder HMI" or "the operational consequence at the HMI" means the **\`/substation\` panel** built into the RangerDanger web app - not FUXA. Open it at <http://localhost:8088/substation>. The Lab 2.3 alarm chain ("RECLOSER OPEN - downstream loads lost") and the customer-service tile both live there.
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

We built it custom because we needed tight control over the alarm logic - the \`LOW VOLTAGE\` and \`RECLOSER OPEN - downstream loads lost\` banners fire on the exact lab conditions we wanted to teach - and we wanted a clean kinetic-feedback view that updates within seconds of an attack. The lab's [Distribution Feeder Physics](#substation-physics) closed loop ends here: an attack mutates a [recloser](#reclosers) state, the RTAC polls it, OpenDSS recomputes the feeder, the panel re-renders the alarm chain. That is the operational consequence the labs keep referring to.

Two tabs to know:

- **Feeder One-Line** - the operator-facing summary view with alarm banners, breaker / recloser symbols, customer-service tile. This is what every Lab 2.3 / 2.3-bonus / 2.4 "Operational consequence at the HMI" callout references.
- **Electrical Detail** - the engineering-precision view with per-bus voltage, feeder current, kW / kVAR. Useful when you want to *quantify* an attack rather than read its alarm-level summary; the same view becomes evidence material in Lab 2.4.

### The FUXA Sidecar (Context, Not Load-Bearing)

The lab also runs [FUXA](https://www.frangoteam.com) - an open-source HMI/SCADA platform - at <http://localhost:8088/apps/fuxa-hmi/>. FUXA is wired up: it has a "Substation One-Line Diagram" view configured, polls the RTAC over Modbus, and has the \`hmi_poller\` sidecar generating its baseline traffic. It is the closest thing in the lab to "the HMI you'd encounter at a small utility or in a vendor demo."

:::note FUXA is contextual, not part of the exercises
None of the exercises depend on FUXA being open or correctly configured. The lab's alarm chain, decision questions, and validation chips all read from the \`/substation\` panel. FUXA is included so students who want to see what the open-source HMI ecosystem looks like - and how a traditional Modbus-polled HMI compares to the lab's purpose-built React panel - can spend a few minutes poking around. If FUXA looks empty or misbehaves, ignore it and use \`/substation\`.
:::

### Why a Custom Panel vs. Configuring FUXA

We picked the React-based panel over building the labs around a FUXA project for three reasons:

1. **Alarm-rule control.** The \`/substation\` panel computes alarms ("voltage out of ANSI Range A → red banner") with a few lines of TypeScript. FUXA would require building an equivalent alarm spec inside FUXA's configuration model, which is opaque to students reading the lab source.
2. **Source-controlled reproducibility.** Every byte of the \`/substation\` UI lives in \`frontend/components/substation-panel*.tsx\` and can be diff-reviewed in PRs. FUXA's project state lives in a SQLite database; changes are awkward to review.
3. **Reaction time.** The \`/substation\` panel polls the RTAC every couple of seconds and re-renders within ~100 ms. It is responsive to attacks in a way that feels live. FUXA's Modbus polling cycle adds a second or two of additional latency; the labs are tighter without it.

The trade-off is that the \`/substation\` panel is *not* a representative example of what a real OT HMI looks like architecturally - it is a single-page React app, not a config-driven HMI runtime. FUXA is closer to that reality, which is why it stays around as context.

### See Also

- [Distribution Feeder Physics for Tech Workers](#substation-physics) - what the numbers on the HMI actually mean and where they come from
- [RTAC (Real-Time Automation Controller)](#rtac) - the data source the HMI polls
- [What Outages Cost (SAIDI, SAIFI, and the Customer-Service Tile)](#outage-costs-saidi-saifi) - what the customer-service tile maps to in real-utility metrics`,
      },
      {
        id: "plc-ladder-openplc",
        title: "PLCs, Ladder Logic, and What OpenPLC Does",
        body: `A **Programmable Logic Controller (PLC)** is a ruggedized industrial computer that reads inputs, runs a fixed control program on a repeating scan cycle, and writes outputs. It is the workhorse of industrial automation. PLCs run conveyor belts in warehouses, pumping stations at water utilities, batch reactors in chemical plants, and protection-and-control logic in substations.

PLCs differ from servers and from RTACs in a few important ways:

- **Hard real-time scan cycle.** A typical PLC scan is 10 to 100 milliseconds - read all inputs, execute the program, write all outputs, repeat. Missing a scan is a fault.
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

Modern substations may have multiple PLCs (or PLC-like devices such as SEL RTACs running embedded protection-logic apps) alongside protective relays and a central RTAC. The lines blur - some SEL RTACs run IEC 61131-3 logic apps and are PLCs in everything but the marketing name.

### What OpenPLC Is

[OpenPLC](https://openplcproject.com) is an **open-source PLC runtime** implementing IEC 61131-3. It runs on Linux, supports ladder, structured text, and function block diagram via the OpenPLC Editor, and is widely used for ICS security research and education because it provides a free, inspectable PLC environment.

In the RangerDanger lab, the \`openplc\` node at \`10.30.30.30\` runs OpenPLC with a simple substation-automation program (\`data/openplc/substation_automation.st\`). It demonstrates a PLC's role in the OT operations zone - accessible via Modbus and the OpenPLC web UI at \`http://localhost:8088/apps/openplc/\` - and gives students a third device class to think about beyond the RTAC and the field-device IEDs. The hardened firewall policy treats it like any other OT-Ops host: cross-zone traffic flows through containd, intra-zone is allowed.`,
      },
      {
        id: "power-factor-reactive-power",
        title: "Power Factor and Reactive Power",
        body: `The feeder physics article introduces power factor in one bullet. This article unpacks it - useful background for the voltage regulator, capacitor bank, and "why does low voltage at heavy load actually happen" questions.

### Real, Reactive, and Apparent Power

Alternating-current circuits have three power quantities, not one:

- **Real power (P)**, measured in **kilowatts (kW)**. This is the power that actually does useful work - heats elements, turns motor shafts, lights bulbs. It is what shows up on the customer bill.
- **Reactive power (Q)**, measured in **kilovolt-amperes-reactive (kVAR)**. This is power that flows back and forth between the source and inductive or capacitive elements. It does no net useful work, but it has to be transported on the same wires as real power.
- **Apparent power (S)**, measured in **kilovolt-amperes (kVA)**. The vector sum of P and Q. Conductors and transformers are sized to handle this - not just the real-power demand.

These three relate by a right triangle: **S² = P² + Q²**. The ratio **P/S** is the **power factor (pf)**, a value between 0 and 1.

### Why Reactive Power Exists

Motors and transformers store energy in magnetic fields. Each AC cycle, energy flows *into* the magnetic field on one half-cycle and back *out* on the other. That sloshing energy is reactive power. The motor only converts a portion of the electrical energy into mechanical work (the real-power portion); the rest is the magnetic field doing its dance.

Capacitors do the same thing with electric fields - but in the *opposite* phase direction. This is why capacitor banks are used to compensate for inductive loads: they push reactive power one way at the moment the motors are pulling it the other way, and the two cancel locally.

### Why Power Factor Matters

A 200 kW load at **pf = 1.0** draws 200 kVA. The same 200 kW at **pf = 0.7** (a heavily inductive motor load) draws 286 kVA - 43% more current on the feeder for the same useful work. The utility has to size conductors and transformers for the kVA, not the kW.

Low power factor also depresses voltage at the load end of the feeder. Reactive current flowing through line reactance drops voltage; the further down a feeder with low pf you go, the lower the voltage at the customer.

### How the Lab Models This

The lab's OpenDSS model uses:

- \`Load.GeneralLoad\` at **0.9 pf** - typical for a mixed commercial/industrial feeder load
- \`Load.CriticalLoad\` at **0.95 pf** - typical for sensitive critical-services loads where utilities specify cleaner power

When OpenDSS solves the feeder, it computes voltage drop using both the real and reactive current. This is why opening the recloser causes a *downstream voltage of zero* (no current flowing, including reactive) and why a regulator tap change at \`-16\` produces a voltage drop large enough to alarm - the LTC is offsetting against reactive demand that the line cannot freely absorb.

### Capacitor Banks and Voltage Regulators

Both work by adjusting the local reactive-power balance:

- A **capacitor bank** injects reactive power locally (it has a *leading* power factor). Switching one in on a feeder with low-pf load raises the local voltage and reduces the kVA the substation has to transport.
- A **voltage regulator** is an LTC transformer that simply adjusts the voltage ratio. It does not change Q; it changes V directly, which is why the lab's tap-attack drops voltage even at constant pf.

Real utility distribution engineering spends a lot of time on Q. The lab abstracts it away under sensible default values, but a power-engineering colleague will immediately ask "what is the power factor on those loads?" if you mention the OpenDSS model - and now you have the answer.`,
      },
    ],
  }
;
