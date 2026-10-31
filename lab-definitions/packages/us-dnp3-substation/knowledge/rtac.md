The RTAC is the central supervisory node in a distribution substation. Built by companies like SEL (Schweitzer Engineering Laboratories), it aggregates data from every Intelligent Electronic Device (IED) in the substation: protective relays, reclosers, voltage regulators, and capacitor bank controllers. It is the single point of communication between the substation and the utility SCADA/EMS system.

### What the RTAC Does

The RTAC polls field devices using DNP3 and Modbus at regular intervals, collecting:

- **Analog measurements**: current, voltage, power, tap position
- **Binary status**: breaker open/closed, relay tripped, lockout active

It stores this data internally and forwards it upstream to the control center via DNP3, IEC 61850, or other protocols. When an operator issues a command (open a breaker, raise a tap, enable reclose), the RTAC translates it into the device-level protocol and sends it to the target IED.

### Why the RTAC Matters for Security

In a properly segmented network, the RTAC is the **only** device authorized to communicate across the OT operations zone and the field device zone. This is a deliberate design choice. Rather than letting the HMI or engineering workstation talk directly to field devices, all command traffic goes through the RTAC. This creates a natural chokepoint where firewall rules, protocol filtering, and audit logging can all be applied.

> If an attacker compromises the HMI, they should not be able to reach field devices directly. The firewall should only permit RTAC-to-field traffic.

### Lab Implementation

The RTAC simulator (`rtac-sim`) runs at **10.30.30.20** on the `ot_ops_net`. It polls the relay, recloser, and regulator over HTTP and aggregates their state. It also talks to the OpenDSS physics engine on the `physics_net` to incorporate feeder electrical state.

The RTAC is **multi-homed**, connecting to:

1. `ot_ops_net` (10.30.30.0/24)
2. `field_net` (10.40.40.0/24)
3. `physics_net` (10.50.50.0/24)

Under the hardened firewall policy, the RTAC is the only device permitted to send Modbus or DNP3 traffic into the `field_net` zone.