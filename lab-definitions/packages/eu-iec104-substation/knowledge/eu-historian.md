A historian preserves observations for later analysis. That does not make it an independent source of physical truth. If its upstream observer loses contact with a station, an honest archive preserves the observer's stale values and quality. Replacing them with zeros or fresh field measurements would make a cleaner-looking trend but destroy the evidence of what the operator actually knew.

### Push into an operations DMZ

The range's `historian-1` sits at `10.70.70.40` in the Operations DMZ, firewall zone `lan5`. The CC pushes its schema-2 state once per second and for every applied timed report to `POST /api/ingest` on TCP/8081, binding its source to `10.60.60.20`. A timed snapshot is rendered before a following untimed GI can clear its source tag. Pushes have a 500 ms total timeout and one in flight. A bounded queue holds at most 16 pending snapshots; overflow drops the oldest and increments CC health's `historian.dropped_snapshots`. Failed pushes are dropped, never replayed. Historian failure must not block IEC 104 processing.

Read listeners are separate: `10.70.70.40:8080` and management `10.99.99.40:8080`. They expose GET points, history and health only; neither serves ingestion. The historian never polls the CC, field devices or process, never sends a command, and has no write-back path into OT.

Ingestion strictly validates the CC state. Malformed schemas, unknown or duplicate keys, invalid types and other invalid bodies receive 400 and increment the rejection counter. Permitting the network conduit is therefore necessary but not sufficient for an accepted sample.

### What a sample means

Each point sample retains `snapshot_seq`, value, quality flags, stale reason, source time, source-time validity, `cc_received_at` and `ingested_at`. Step values retain their `{value, transient}` object. The CC point's receipt time becomes `cc_received_at`; it is not rewritten to the historian's receipt time.

A changed `cache_epoch` starts a new segment. More than three seconds between accepted ingests becomes a gap from the previous ingest to the next one. A continuous stream of stale CC points is **not** an ingest gap: the feed is working while its observations are degraded. Current silence appears in health before a completed gap can be recorded on recovery.

The in-memory ring holds 7201 snapshots, covering a full two-hour interval at 1 Hz. Faster ingestion shortens the retained time span. Historian restart loses the archive; Reset Lab does not clear it.

### Read it without losing provenance

From an allowed enterprise source, `GET http://10.70.70.40:8080/api/points` lists retained points and their latest samples. History requires `point`, `since` and `until`, each exactly once, with UTC RFC3339 timestamps. For example, choose `point=oltc-1-tap` and a window spanning the exercise. The bounds are inclusive **ingestion times**, not source-event times.

The portal relays `GET /api/historian/points` and `/api/historian/history` through the management read listener. That convenience does not prove the enterprise-to-DMZ firewall rule; test that conduit from the enterprise source separately. Correlate the trend with [CC state and journals](#eu-rtu-control-centre-apis), physical evidence and [evidence lifecycle](#eu-evidence-lifecycle).

### Limits

This is not an SOE archive. Cadence snapshots can miss intermediate untimed changes; timed snapshots can be lost on queue overflow or failed pushes and are not replayed. Sequence jumps can also reflect other CC state renders, not necessarily lost ingestion. A healthy feed does not certify point quality or source-clock accuracy. Preserve epoch boundaries, nulls, stale flags and gaps in exports; a smoothed line alone cannot establish event ordering, command causality or a complete operational history.

Implementation: `services/historian-eu/store.go`, `schema.go`, `http.go`; `services/iec104/src/cc_historian.c`.
