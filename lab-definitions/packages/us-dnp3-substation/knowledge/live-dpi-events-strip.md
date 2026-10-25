The Segmentation drawer on the [Network Map](/console) displays live firewall events alongside the active policy name. The stream can contain rule, protocol-decoder and IDS records; it is not exclusively a DPI feed.

### Reading the Stream

- **Rule events** describe logged L4 decisions. Correlate source, destination, port and rule ID with your probe.
- **Protocol events** describe decoded requests and responses, including operation details when available.
- **IDS events** report detected anomalies. A detection does not establish enforcement.

**The L4 source pin enforces; DPI detects and logs.** In Lab 2.3, use connectivity tests and device-state observations to prove the hardened policy's result. Do not treat a protocol event as proof that a command was blocked.

### Evidence Assembly

Run prescribed checks one at a time. Save the active policy, command results, device state and corresponding timestamped events for Lab 2.4. Verify both required connectivity and denied connectivity.

### When the Strip Stays Empty

An empty strip can mean that the matching rule does not log, the probe stayed within a zone, or the event source is unavailable. Check the source status and active policy before drawing conclusions. The weak baseline does not enable the hardened reference's logging. No events is not evidence of enforcement.
