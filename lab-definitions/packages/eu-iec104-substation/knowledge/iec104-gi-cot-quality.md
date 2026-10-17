A telemetry value needs context: why it was sent, whether it is usable and when it was observed. General interrogation establishes a station snapshot, spontaneous reporting carries changes, and quality flags qualify either kind of report. None of these mechanisms turns an old cached value into present physical truth merely because an API can still display it.

### GI and causes of transmission

Station General Interrogation uses QOI 20. The normal sequence is activation COT 6, activation confirmation 7, monitored responses COT 20 and activation termination 10. Cyclic reports use COT 1 (not COT 2 background scan). Spontaneous reports use COT 3. Command return information uses COT 11. The negative flag is separate from the numeric cause: a negative ACT-CON is still an activation confirmation.

In the range, GI reads the RTU's acquired point state. It is not an atomic new power-flow solve or a synchronous poll of every device. A successful interrogation can therefore return invalid observations. Completion of the interrogation proves the exchange finished, not that the feeder or every southbound conduit is healthy.

### Selected reporting profile

This is a selected teaching profile modelled on [WEMAG’s DSO profile](https://www.wemag-netz.de/sites/default/files/2026-01/ergaenzende-anschlussbedingungen-ueber-iec-60870-5-104-v03-von-ttl-2025-03-17_final.pdf) (untimed analogs, cyclic and threshold reporting) and [SvK TR02-04-04E](https://www.svk.se/siteassets/aktorsportalen/entreprenorer-i-elnatet/tekniska-riktlinjer/tr02/tr02-04-04e-2-2022-10-25.pdf) (timed events, untimed GI/cyclic), not an EU default. The range’s periods and deadbands below are teaching choices, not copied utility defaults. The CC interrogates on connect, every 15 minutes from the previous GI start, or through **Interrogate station**. Status changes are spontaneous; unchanged statuses have no cyclic refresh and do not age out. Float measurements use untimed M_ME_NC_1 every 10 seconds, PLC parameters every 60 seconds, plus deadband/quality-driven spontaneous reports. GI and cyclic responses pack multiple information objects by type (SQ=0), splitting full ASDUs.

The value deadbands are 0.1 kV (0.5% of 20 kV, below a roughly 0.24–0.25 kV tap step) and 2 A (about 3% of nominal feeder current), teaching choices rather than equipment ratings. PLC parameter deadbands are zero. The baseline is the last spontaneous, cyclic or command-return value; GI does not move it. Value-driven float spontaneous reports are limited to one per point per second; a held change is rechecked against the baseline at expiry. Quality changes bypass both deadband and limiter.

### Quality and stale reasons

`GOOD` means a value is present, no quality flag is true and `stale_reason` is null. IV means invalid, NT not topical, SB substituted, BL blocked and OV overflow. The range preserves these fields but does not generate every possible quality condition.

The implemented distinctions are important:

- No successful Modbus poll for more than the configured three-second threshold produces IV+NT. The CC labels it `device_unreachable`.
- An expired or invalid field measurement marks measurement points IV. Device measurements expire three seconds after receipt; retained electrical numbers are not fresh measurements. The CC labels IV alone on these devices `measurement_invalid`.
- A PLC heartbeat unchanged for three seconds makes all PLC points IV, labelled `heartbeat`, even if Modbus reads still succeed.
- CC link loss preserves previously received values and times, adds IV+NT and records `link_lost`.
- A point never received is null with `never_received`, not zero.
- On a started link, a currently GOOD float with no report for more than three cyclic periods becomes NT with `not_updated`: 30 seconds for measurements, 180 seconds for PLC parameters. It retains value, received ASDU and COT; the next report replaces quality. This watchdog does not override RTU-set reasons, `never_received` or `link_lost`. Status points never age out.

A physical switch position can remain observable when its electrical measurements are invalid. Conversely, a live IEC 104 session can carry a southbound device's stale indications. Diagnose at point level, not just from the link badge.

### How to demonstrate recovery honestly

Export `GET /api/operator/state` before the change. Record `cache_epoch`, `snapshot_seq`, link/GI information and the selected point's value, quality, `received_at`, COT and stale reason. The displayed `asdu` is the last type actually received (null before first receipt), not the configured event type; `kind` is stable identity. Float `reporting` exposes `cyclic_ms` and `deadband`; status `reporting` is null. Capture the relevant traffic inside the same attempt window. Integrity means `link.gi.last_complete_at` is non-null on this connection, even during a later GI; it certifies positive ACT-TERM, not coverage or quality. For lab acceptance, retain a matched GI start/completion within ten minutes and after the attempt boundary, plus all 31 non-null GOOD points received at or after its start. If missing, press **Interrogate station** in the **Control Centre** and validate again. After restoration, require new IEC 104 reports or GI and inspect their quality; do not substitute `/api/field/state` for missing CC evidence.

Use [monitoring recipes](#tool-iec104cmd) to inspect point objects. A one-shot GI command's summary is not itself a complete point listing. Quality changes are reported spontaneously even when the value is unchanged; floating-point value changes can be suppressed by their configured deadband.

### Time is a different validity axis

GI and floating-point reports are untimed. Spontaneous and command-return statuses and step positions carry CP56Time2a based on the [station clock](#eu-station-clock). The timestamp's invalid flag becomes `source_time_invalid`; it is separate from point IV. A usable value can have uncertain source time, and a stale value can carry a well-formed timestamp. Preserve both axes in exports.

### Limits

GI is not an event replay or SOE completeness guarantee. A [historian](#eu-historian) snapshot preserves these quality distinctions but cannot recover events never observed. “No change” may mean a stable value, deadband suppression or lost observation. Only the surrounding quality, timing and protocol evidence can distinguish those explanations.

Implementation: `services/iec104/src/rtu.c`, `cc_state.c`; `services/devices/fielddevice/model.go`; package `protocols/points.json`.
