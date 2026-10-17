EU scope: NTP, PTP and IRIG-B below are general OT time-distribution background. The EU range does not model or expose these services, and its GPS simulator is not an NTP server. Its implemented station-time path uses HTTP; see [Station time records observation provenance](#eu-station-clock).

Time lets operators and investigators compare observations from different devices. A log timestamp is useful only when its source and quality are understood: a device may record when it observed a change, when a gateway reported it, or when a collector received it. Those moments are not automatically the same as when a physical event began.

### Time distribution concepts

An OT design normally identifies a trusted reference, the services or devices that distribute time, and the clients that consume it. The reference may itself depend on an external source, a local clock or a holdover oscillator. Distribution technology and accuracy requirements depend on the application and equipment; a protocol name alone does not guarantee a particular precision.

- **NTP** is a client/server protocol commonly carried over UDP port 123. It is widely supported for clocks and logs that do not require tightly coordinated sub-millisecond event timing. NTP's stratum describes distance from a reference in the synchronization hierarchy; it is not a measurement of clock accuracy.
- **IEEE 1588 PTP** can provide tighter synchronization when devices, timestamping support, network design and a suitable profile work together. Grandmasters and boundary or transparent clocks may participate in a PTP domain. A network that merely transports Ethernet does not thereby provide the timing performance an application requires.
- **IRIG-B** is a time-code family used over dedicated electrical or optical interfaces, rather than an IP clock service. Its suitability depends on the signal, connected equipment and installation.

These approaches are not interchangeable labels for one service. Choose and validate a distribution method against the actual equipment and the timing requirement. Do not infer that a substation has a GPS clock, NTP server, PTP domain or IRIG-B wiring simply because such designs are common elsewhere.

### Why time is an OT security concern

Incorrect or inconsistent time can make an event sequence appear contradictory. It can weaken incident reconstruction, maintenance records and comparisons between protection, gateway, operator and historian logs. Where an application genuinely depends on synchronized measurements, loss or manipulation of synchronization may also affect that application's operation; that dependency must be established for the specific system rather than assumed.

Time evidence has more than one dimension. Preserve the reported source timestamp and its validity separately from gateway receipt and collector ingestion times. A healthy-looking value does not establish that its timestamp is trustworthy, and synchronized clocks do not prove that a measurement itself is valid. In the EU range, the implemented clock path and timestamp-validity behavior are documented in the [station-clock article](#eu-station-clock).

### Restrict and monitor time paths

Treat time distribution as an explicit service dependency in the zone and conduit design:

- Identify the approved reference and distribution points, their owners, client populations and fallback behavior.
- Permit clients to use only the designated time service for their zone. Avoid direct field-device synchronization from arbitrary enterprise or external sources.
- Allow only the required direction and protocol between named sources and clients. UDP 123 is typical for NTP, but confirm the deployed version, security options and profile before writing a rule. PTP and IRIG-B have different transport and physical-path requirements.
- Protect administrative changes to time sources and configurations. Monitor synchronization state, clock offset, loss of reference and unexpected source changes; retain enough independent receipt-time evidence to investigate a shift.
- Test what clients do when a source is unavailable or disagrees with another source. A fallback can preserve availability while changing the confidence or provenance of timestamps.

These are design principles, not a description of firewall rules or time services implemented in this training range. The EU reference policy is L4-only; it does not inspect or enforce time values inside application messages.

### Limits when interpreting evidence

A device's “synchronized” status is evidence about that device's reported state, not cryptographic proof of UTC accuracy or proof that every peer shares the same clock. A displayed timestamp can also be stale, invalid or generated at a different observation point. Record the clock source, synchronization status, event timestamp and independent receipt time before drawing a causal timeline.

The EU simulation's HTTP station-clock path is a teaching model for time provenance, not a simulation of NTP, PTP, IRIG-B, satellite reception, clock-distribution hardening or substation-wide time synchronization. It does not demonstrate a time service that can be reached or constrained through the firewall.
