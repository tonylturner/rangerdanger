Network Time Protocol might seem mundane in an IT context, but in OT networks, accurate time synchronization is a critical operational requirement. Protective relays, fault recorders, sequence-of-events recorders, and phasor measurement units all depend on precise timestamps. When a fault occurs on a feeder, the sequence of events (which relay tripped first, which breaker opened when, how the fault propagated) must be reconstructed with millisecond accuracy.

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
- The firewall should permit NTP (UDP 123) only from the designated time source, blocking NTP from unauthorized sources that could inject false time