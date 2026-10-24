Evidence has a lifetime. Live state is overwritten, bounded journals wrap, captures can be cleared and simulator resets change the very conditions under investigation. A defensible lab result records a bounded attempt with its starting conditions and preserves enough provenance to distinguish actual operations from coincidental final state.

### Different records answer different questions

The CC state says what the operator observed through IEC 104. It includes `cache_epoch`, render `snapshot_seq`, `rendered_at`, point values, quality, source times and receipt times. A state render is not an equipment event. `snapshot_seq` increments for every render, including reads unrelated to historian publishing.

The CC journal is a 500-entry ring for link, GI, quality, status-change and command evidence. `gi_started`/`gi_complete` identify their connect/periodic/operator trigger. `status_event` records received COT 3/11 status value or transient changes, never floats; quality-only transitions are not status events. Use its epoch and oldest/latest sequence bounds to detect truncation. Field-device audits are separate 500-entry rings with device generation, entry sequences, before/after state and actor context. The portal `/api/field/audit` exposes only the breaker's audit; do not assume it includes every RMU or tap request.

Process state carries `solver_epoch`, `solve_seq`, solved time, truth-stale status and per-device delivery revisions. It explains the electrical solve, not what the CC received. Device truth revision changes identify state changes within that device; they are not globally ordered event ids across all services.

The [historian](#eu-historian) preserves CC snapshots (one per second plus one per timed report) in a bounded in-memory ring of 7201 snapshots. Source, CC receipt and ingestion times remain distinct. Epoch segments and ingest gaps explain discontinuities, but a feed can continuously archive invalid observations.

### Capture before changing conditions

Before an attempt, record package id, range generation, active policy, CC epoch and journal cursor. Save the starting operator and field states with their receipt time at the collector. State the exact source, destination, command or probe, expected feedback and stop condition.

During the attempt, retain protocol exchanges and returned results, not only screenshots of the final state. Afterward, export the journal after the saved cursor, relevant device audit, process state and historian interval. For clock exercises, use receipt/ingestion bounds and sequence evidence rather than trusting the manipulated source clock to order everything.

A useful evidence bundle distinguishes:

1. The request was actually sent in the attempt window.
2. The protocol accepted, rejected or left its outcome ambiguous.
3. Fresh matching feedback arrived, or did not.
4. Physical consequence and operator visibility agreed, or intentionally diverged.

### Reset and retention

Export captures before Reset Lab. The shared reset handler clears capture readiness and removes stored PCAP files when Docker cleanup is available. It also restores the package firewall baseline. Device reset changes truth and adds audit evidence; it does not erase the device audit ring. The CC cache and historian are not reset by the EU truth-reset sequence.

A CC restart creates a new cache epoch; historian ingestion then starts another segment. Historian restart loses its in-memory archive. A quiet retained value cannot prove uninterrupted observation across either event. See [reset acceptance](#eu-reset-acceptance) before beginning the next attempt.

### Limits

No single range export is a complete SOE record. Ring truncation, snapshot cadence, failed publication and untimed GI reports limit reconstruction. Missing evidence is not a failed physical operation, and stale is not zero. If a cursor predates retained entries or a collector missed the critical interval, report that limitation instead of silently narrowing the claim to whatever survived. Keep CC observations separate from direct truth even when combining them in one report.

Implementation: `services/iec104/internal/controlcentre/state.go`; `services/devices/fielddevice/audit.go`; `services/historian-eu/store.go`; backend `reset.go`.
