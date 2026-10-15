The feeder physics article introduces power factor in one bullet. This article unpacks it - useful background for the voltage regulator, capacitor bank, and "why does low voltage at heavy load actually happen" questions.

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

- `Load.GeneralLoad` at **0.9 pf** - typical for a mixed commercial/industrial feeder load
- `Load.CriticalLoad` at **0.95 pf** - typical for sensitive critical-services loads where utilities specify cleaner power

When OpenDSS solves the feeder, it computes voltage drop using both the real and reactive current. This is why opening the recloser causes a *downstream voltage of zero* (no current flowing, including reactive) and why a regulator tap change at `-16` produces a voltage drop large enough to alarm - the LTC is offsetting against reactive demand that the line cannot freely absorb.

### Capacitor Banks and Voltage Regulators

Both work by adjusting the local reactive-power balance:

- A **capacitor bank** injects reactive power locally (it has a *leading* power factor). Switching one in on a feeder with low-pf load raises the local voltage and reduces the kVA the substation has to transport.
- A **voltage regulator** is an LTC transformer that simply adjusts the voltage ratio. It does not change Q; it changes V directly, which is why the lab's tap-attack drops voltage even at constant pf.

Real utility distribution engineering spends a lot of time on Q. The lab abstracts it away under sensible default values, but a power-engineering colleague will immediately ask "what is the power factor on those loads?" if you mention the OpenDSS model - and now you have the answer.