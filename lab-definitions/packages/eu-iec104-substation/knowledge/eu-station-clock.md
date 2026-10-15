Time is part of observation provenance. A correct value with an uncertain timestamp supports different conclusions from a value whose timing is trusted. In particular, a timestamp assigned by a gateway records when that gateway observed or reported a change; it is not automatically the time a field contact moved or a fault began.

### The implemented clock path

The RTU polls `http://10.30.30.50:8080/api/state` on gps-sim every 1000 ms. The configured synchronized status is `locked`, with a 5000 ms freshness threshold. It reads `reported_time` as the station-time base and advances that base using monotonic elapsed time. With no base yet available, it uses host time but reports unsynchronized timing.

The source is `gps-1` in the station zone, with management simulation endpoint `10.99.99.27:8080`. Its state includes reported and actual time, offset and synchronization status. The EU Compose configuration disables its optional outbound NTP traffic generator. **gps-sim is not an NTP server.** This telecontrol time path uses HTTP, not NTP, PTP, IRIG-B or IEC Type-103 clock synchronization. State flags named for those technologies are not evidence that their protocols are implemented on this path.

RTU-to-GPS polling is same-subnet station traffic. It is not a separate inter-zone conduit whose enforcement can be demonstrated by editing the station-to-field firewall rule.

### Timestamp invalidity versus point invalidity

Spontaneous single-point, double-point and step-position reports carry CP56Time2a assigned by the RTU station clock. Its invalid-time bit follows synchronization. The CC exposes this as `source_time_invalid`, separately from quality `iv`. GI responses and floating-point measurements are untimed and have no source timestamp to validate.

A stale clock can therefore coexist with good point quality. Conversely, a recently synchronized timestamp does not prove that the point's underlying electrical measurement is valid. Preserve both fields instead of collapsing them into one green/red status. See [GI and quality](#iec104-gi-cot-quality) for the independent data-validity rules.

### Demonstrating time provenance

Before an authored offset step, export CC state, record its `cache_epoch`, and save the current field/process evidence. The lab vocabulary allows GPS `set_offset` with a seconds value and `reset_offset`; the backend translates these to the GPS management command endpoint. These are simulation actions, not operator IEC 104 commands.

After changing the offset, trigger or observe a status transition so there is a new timed report to inspect. Compare `source_time` with CC `received_at` and the audit/process times. Merely reading an unchanged point can show its old source timestamp. A GI may replace a timed cached report with an untimed observation, so it cannot prove that the altered source clock was used for a timed event.

The [historian](#eu-historian) preserves source-time validity and CC receipt time separately from ingestion time. Its query bounds use ingestion time, which remains the right window selector when source time has been shifted.

### Limits

A locked status is a simulator assertion, not cryptographic proof of UTC accuracy. Applying an offset need not remove lock, so plausible-looking time can still be wrong. This is not satellite reception, oscillator accuracy testing, time-distribution hardening or device SOE capture. Do not sort all evidence solely by source time after a clock manipulation. Retain epochs, sequence numbers, receipt times and attempt windows so causal claims remain defensible.

Implementation: package `protocols/points.json`, `compose.source.yml`; `services/iec104/src/stationclock.c`; `services/gps-sim/main.go`.
