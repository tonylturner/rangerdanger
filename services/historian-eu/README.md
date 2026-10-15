# EU observation historian

`go -C services run ./historian-eu` runs a stdlib-only, in-memory archive of
control-centre observations. It never polls the CC, field devices or solver,
never sends commands, and is not a sequence-of-events (SOE) archive.

## Listeners

| Environment variable | Default | Surface |
| --- | --- | --- |
| `HISTORIAN_INGEST_LISTEN` | `10.70.70.40:8081` | Only `POST /api/ingest` |
| `HISTORIAN_READ_LISTEN` | `10.70.70.40:8080,10.99.99.40:8080` | Only the GET routes below |

Bind addresses must be explicit IPs with nonzero numeric ports. Empty,
wildcard and duplicate bindings are errors. Every listener binds before any
serves requests; any bind failure fails startup. Separate handlers prevent
ingest on **every** read interface, including management.

Ingest requires the complete CC schema-2 state (including `link`,
`snapshot_seq`, `rendered_at` and each point's nullable fields). Missing,
unknown or duplicate keys, wrong types, unknown enums, non-UTC timestamps,
nonfinite numbers, duplicate point ids/IOAs and bodies over 64 KiB get 400
and increment `rejected_count`. At most 31 points are accepted per snapshot.
An accepted ingest returns 200 `{"accepted":true}`. Unknown routes return
404; wrong methods on known routes return 405 (including HEAD and OPTIONS).
These route/method errors do not increment the body-rejection counter.
All API errors return `{"error":"<message>"}` with
`Content-Type: application/json` and `Cache-Control: no-store`, preserving
their HTTP status for proxies. HEAD responses carry the same headers but no
body, as required by HTTP.

## Read API

- `GET /api/points` returns `[{id, ioa, asdu, last}]`, sorted by id.
  `last` is the last retained sample of that point.
- `GET /api/history?point=<id>&since=<UTC-RFC3339>&until=<UTC-RFC3339>`
  returns `{point, segments: [{cache_epoch, samples: [...]}], gaps: [{from,to}]}`.
  All three parameters are required exactly once; unknown parameters and
  reversed intervals return 400. Unknown/fully evicted points return 404.
  Known points with no samples in the interval return empty arrays.
- `GET /api/health` returns
  `{status, last_ingest_at, ingest_count, rejected_count}`. Before ingestion,
  `last_ingest_at` is null. Status is `degraded` until ingestion, `ok` for up to
  3 seconds after an accepted ingest, then `degraded`. This describes the
  feed, not telemetry quality. Counters are process-lifetime totals.

Each sample contains `snapshot_seq`, `value`, `quality` (`iv,nt,sb,bl,ov`),
`stale_reason`, `source_time`, `source_time_invalid`, `cc_received_at` and
`ingested_at`. `value` is preserved, including null and the step object
`{"value":-3,"transient":true}`. No parallel top-level transient flag is
synthesised. The CC point's `received_at` is renamed `cc_received_at`; source
and receipt times and all quality flags are otherwise unchanged.

History bounds are **inclusive historian ingestion times**, not device
source times (which can be invalid), CC receipt times, or CC render times.
A cache-epoch transition starts a segment, even when an earlier epoch name
reappears. A gap spans the last successful ingest to the next successful
ingest when their separation exceeds 3 seconds; it describes feed silence,
not a per-point telemetry outage. Rejected bodies do not close gaps.
Queries include completed gaps intersecting their interval with their
original endpoints. Current open-ended silence is exposed through health
and becomes a completed gap when the next accepted ingest arrives.

The bounded ring holds 7,201 snapshots: a full two-hour interval at 1 Hz,
with every point, epoch boundary and gap retained together. Faster ingestion
shortens the time span. Catalogs derive only from retained snapshots, so
point/epoch churn does not grow an unbounded side index. Nothing is persisted
across historian restarts. Reset Lab has no clearing endpoint: a CC restart
is just another epoch. Snapshot sequence numbers are stored as exact u64
values, not reinterpreted as delivery ordering or SOE completeness.

## Targeted checks

```sh
go -C services test -race -count=1 ./historian-eu/...
go -C services vet ./historian-eu/...
gofmt -l services/historian-eu
```
