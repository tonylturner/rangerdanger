A **Programmable Logic Controller (PLC)** is a ruggedized industrial computer that reads inputs, runs a fixed control program on a repeating scan cycle, and writes outputs. It is the workhorse of industrial automation. PLCs run conveyor belts in warehouses, pumping stations at water utilities, batch reactors in chemical plants, and protection-and-control logic in substations.

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

In the RangerDanger lab, the `openplc` node at `10.30.30.30` runs OpenPLC with a simple substation-automation program (`data/openplc/substation_automation.st`). It demonstrates a PLC's role in the OT operations zone - accessible via Modbus and the OpenPLC web UI at `http://localhost:8088/apps/openplc/` - and gives students a third device class to think about beyond the RTAC and the field-device IEDs. The hardened firewall policy treats it like any other OT-Ops host: cross-zone traffic flows through containd, intra-zone is allowed.