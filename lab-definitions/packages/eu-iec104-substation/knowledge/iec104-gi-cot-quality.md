A telemetry value needs context: why it was sent, whether it is usable and when it was observed. General interrogation establishes a station snapshot, spontaneous reporting carries changes, and quality flags qualify either kind of report. None of these mechanisms turns an old cached value into present physical truth merely because an API can still display it.

### GI and causes of transmission

Station General Interrogation uses QOI 20. The normal sequence is activation COT 6, activation confirmation 7, monitored responses COT 20 and activation termination 10. Spontaneous reports use COT 3. Command return information uses COT 11. The negative flag is separate from the numeric cause: a negative ACT-CON is still an activation confirmation.

In the range, GI reads the RTU's acquired point state. It is not an atomic new power-flow solve or a synchronous poll of every device. A successful interrogation can therefore return invalid observations. Completion of the interrogation proves the exchange finished, not that the feeder or every southbound conduit is healthy.

### Quality and stale reasons

`GOOD` means no quality flags are set. IV means invalid, NT not topical, SB substituted, BL blocked and OV overflow. The range preserves these fields but does not generate every possible quality condition.

The implemented distinctions are important:

- No successful Modbus poll for more than the configured three-second threshold produces IV+NT. The CC labels it `device_unreachable`.
- An expired or invalid field measurement marks measurement points IV. Device measurements expire three seconds after receipt; retained electrical numbers are not fresh measurements. The CC labels IV alone on these devices `measurement_invalid`.
- A PLC heartbeat unchanged for three seconds makes all PLC points IV, labelled `heartbeat`, even if Modbus reads still succeed.
- CC link loss preserves previously received values and times, adds IV+NT and records `link_lost`.
- A point never received is null with `never_received`, not zero.

A physical switch position can remain observable when its electrical measurements are invalid. Conversely, a live IEC 104 session can carry a southbound device's stale indications. Diagnose at point level, not just from the link badge.

### How to demonstrate recovery honestly

Export `GET /api/operator/state` before the change. Record `cache_epoch`, `snapshot_seq`, link/GI information and the selected point's value, quality, `received_at`, COT and stale reason. Capture the relevant traffic inside the same attempt window. After restoration, require new IEC 104 reports or GI and inspect their quality; do not substitute `/api/field/state` for missing CC evidence.

Use [monitoring recipes](#tool-iec104cmd) to inspect point objects. A one-shot GI command's summary is not itself a complete point listing. Quality changes are reported spontaneously even when the value is unchanged; floating-point value changes can be suppressed by their configured deadband.

### Time is a different validity axis

GI and floating-point reports are untimed. Spontaneous statuses and step positions carry CP56Time2a based on the [station clock](#eu-station-clock). The timestamp's invalid flag becomes `source_time_invalid`; it is separate from point IV. A usable value can have uncertain source time, and a stale value can carry a well-formed timestamp. Preserve both axes in exports.

### Limits

GI is not an event replay or SOE completeness guarantee. A [historian](#eu-historian) snapshot preserves these quality distinctions but cannot recover events never observed. “No change” may mean a stable value, deadband suppression or lost observation. Only the surrounding quality, timing and protocol evidence can distinguish those explanations.

Implementation: `services/iec104/src/rtu.c`, `cc_state.c`; `services/devices/fielddevice/model.go`; package `protocols/points.json`.
