The Segmentation drawer on the [Network Map](/console) displays firewall events alongside the active policy name. The stream can contain rule, protocol-decoder and IDS records; it is not exclusively a DPI feed.

The panel polls every 2.5 seconds. The backend requests events since the
latest of five minutes ago, the current range generation's start and the
last policy change. It follows filtered pages of up to 5,000 events, keeps
traffic touching a teaching-zone subnet and returns at most 50 rows. The
drawer displays up to five rows in compact mode or ten in the full view.
This is a time-bounded view, not a complete event archive.

### Reading the Stream

- **Rule events** describe logged L4 decisions. Correlate source, destination, port and rule ID with your probe.
- **Protocol events** describe decoded requests and responses, including operation details when available.
- **IDS events** report detected anomalies. A detection does not establish enforcement.

containd retains DENY rule events in a count-bounded bank with no age expiry.
They survive policy commits, but not process restarts; later DENYs can evict
older ones. Retention does not widen the strip's time window: historical DENYs
before its `since` boundary are excluded. Rows within the window can still
predate your latest probe. Check each timestamp, tuple and rule ID against
your probe boundary and saved policy, not just the label beside the stream.

The API returns `since`, `historyTruncated` and `engineError`, but the drawer
does not display that metadata. An incomplete history or engine read error
can therefore leave a partial or empty strip without an on-screen warning.
Preserve the API response when completeness matters.

**The L4 source pin enforces; DPI detects and logs.** In Lab 2.3, use connectivity tests and device-state observations to prove the hardened policy's result. Do not treat a protocol event as proof that a command was blocked.

### Evidence Assembly

Run prescribed checks one at a time. Record the probe's start time, then save the active policy, command results, device state and corresponding fresh timestamped events for Lab 2.4. Old matching DENYs are not proof that this probe was denied. Verify both required connectivity and denied connectivity.

### When the Strip Stays Empty

An empty strip can mean that the matching rule does not log, the probe stayed within a zone, or the event source is unavailable. Check the source status and active policy before drawing conclusions. The weak baseline does not enable the hardened reference's logging. No events is not evidence of enforcement.
