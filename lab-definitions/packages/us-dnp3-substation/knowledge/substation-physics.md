If you came to this lab from software, networking, or security rather than from a power-engineering background, you have probably wondered what is actually *happening* electrically when the HMI shows the critical-load voltage chip dropping from 120 V to 0 V. This article is a working mental model. Not enough to design a substation, but enough to understand why the numbers move the way they do, what OpenDSS is, and why it is the right engine for the job.

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

- **Breaker (`relay-sim`)** sits at the source end of the feeder. Open the breaker → the feeder is disconnected from the source bus → everything downstream goes to 0 V immediately. This is the worst-case single-action outcome in the lab.
- **Recloser (`recloser-sim`)** sits mid-feeder. Open the recloser → everything *downstream of the recloser* loses power (general load, voltage regulator, critical load) but the substation bus stays energized. The breaker is still closed at the source.
- **Voltage regulator (`regulator-sim`)** is a transformer with an adjustable tap. Move the tap up → the secondary voltage goes up. Move it down → it goes down. The tap has ±16 positions covering ±10% voltage adjustment, so each tap step is 0.625% (`TAP_STEP_PU = 0.00625` in the lab's OpenDSS wrapper).
- **Fault** is a short circuit somewhere on the feeder. The recloser detects the abnormal current, opens to interrupt it, then (if auto-reclose is enabled) tries to close again after a brief delay. This is the "reclosing" cycle. If the fault persists after `maxShots = 3` attempts, the recloser goes to **lockout** and stays open until a human resets it.

### Why a Power-Flow Simulator? Enter OpenDSS

Computing voltage and current at every bus on the feeder, given the current device states and load values, is a non-trivial math problem. It involves solving a system of nonlinear equations because power = voltage × current × power factor, but each load's current depends on the voltage at that load, which depends on the upstream impedance and other loads' currents, which depend on *their* voltages, and so on. Real distribution-engineering software solves this iteratively.

**OpenDSS** is the industry standard for that math. It is an open-source distribution-system simulator from EPRI (the Electric Power Research Institute, the US utility R&D consortium). Utilities use it for hosting-capacity analysis ("where can we add solar without overloading the feeder?"), feeder-loss studies, protection coordination, and exactly the kind of power-flow snapshots the lab needs.

> EPRI's OpenDSS reference: https://www.epri.com/pages/sa/opendss
>
> DSS-Extensions (which packages OpenDSS for Python/Julia/Rust/etc.): https://dss-extensions.org

The lab embeds OpenDSS via the `opendssdirect.py` Python binding, served as a FastAPI service at `opendss-sim:8080` on `physics_net` (10.50.50.20). It runs a real 3-phase unbalanced power-flow solve on every device-state change.

### What the Lab's OpenDSS Model Includes

The DSS circuit file at `services/opendss-sim/dss/substation_feeder.dss` models:

- A **12.47 kV, 3-phase, 60 Hz source bus** with realistic short-circuit MVA values
- A **breaker** (modeled as a switchable line element controlled by the lab's relay-sim state)
- A **2,000-foot main feeder segment** using IEEE 13-bus reference line code 601 (336 ACSR overhead conductor)
- A **recloser** (another switchable line element controlled by recloser-sim)
- A **general load** of 500 kW at 0.9 power factor
- A **500-foot lateral** using IEEE 13-bus line code 602 (4/0 ACSR)
- A **load-tap-changer transformer** modeling the voltage regulator (5,000 kVA, ±10% tap range, 32 steps)
- A **critical load** of 200 kW at 0.95 power factor - the "hospital and fire station" load on the customer-service tile

The IEEE 13-bus line codes (`Linecode.601` and `Linecode.602` in the lab's `linecodes.dss` file) are reference impedance data the power-engineering world has used for decades. They are exactly what you would find in any distribution-planning textbook.

### The Closed Loop: Cyber Attack → Physics Recompute → HMI Numbers

When a student fires a `dnp3cmd` packet that trips the recloser, here is what actually happens:

1. The DNP3 packet hits the wire. Wireshark with the DNP3 dissector can see the CROB request frame.
2. `recloser-sim` parses the frame via the in-tree `dnp3go` library and updates its state: `recloser_closed = false`.
3. The RTAC's poll loop (every 2 seconds for HTTP, 5 seconds for DNP3 master, 3 seconds for Modbus) picks up the new state.
4. The RTAC POSTs the aggregated device state to OpenDSS: `{ breaker_closed: true, recloser_closed: false, tap_position: 0, fault_seen: false }`.
5. OpenDSS issues `Open Line.Recloser 1` against the in-memory compiled circuit and runs `Solution.Solve()`.
6. The new bus voltages, line currents, and load powers come back as JSON: `downstream_voltage_v: 0`, `critical_load_voltage_v: 0`, `feeder_current_a: 0`.
7. The HMI reads those values from the RTAC's `/api/state` endpoint and re-renders. The voltage chip turns red. The customer-service tile flips to "ALL CUSTOMERS WITHOUT POWER."

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
The lab gives you two ways to look at the OpenDSS-computed feeder state. The [`/substation` panel](#hmi-scada-fuxa)'s **Feeder One-Line** tab shows the operator-facing alarm summary; the **Electrical Detail** tab shows per-bus voltages, feeder current, and load kW in numeric form. The two tabs read the same data, just at different abstraction levels.
:::

### See Also

- [HMI, SCADA, and the Lab's Substation Panel](#hmi-scada-fuxa) - where the numbers from this article get rendered
- [Power Factor and Reactive Power](#power-factor-reactive-power) - the inductive / capacitive side of the math
- [Reclosers](#reclosers), [Protective Relays](#protective-relays), [Voltage Regulators](#voltage-regulators) - the devices whose state OpenDSS reads
- [The OpenDSS project at EPRI](https://www.epri.com/pages/sa/opendss) - the engine itself