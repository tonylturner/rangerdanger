# API Specification

Base URL: `/api` (when accessed through the nginx proxy at `http://localhost:8088/api`)

All endpoints return JSON unless otherwise noted. Most request and response bodies use `snake_case` field names. Exceptions include `GET /pcap/list`, which returns `sizeBytes` and `createdAt`, and PCAP downloads, which return binary data.

**Range-bound routes.** Routes that reach the range (its containers, its firewall or its services) are served only while a range generation serves: phase `ready`, or `preflight` while the previous range is still untouched (see [Range](#range)). Otherwise they answer `503 {"error":"range not ready","phase":"<phase>"}`. When a switch starts stopping the range, in-flight range-bound requests, terminal sessions and SSE streams are cancelled and the switch waits up to 15 s for them to return; a request that loses its range mid-flight may answer `503 {"error":"range is stopping"}`. The range-bound routes are: all of `/firewall/*`, `/substation/*`, `/pcap/*`, `/traffic/*`, `/workshop/*`, `/operator/*`, `/field/*`, `/historian/*` and `/containd/*`; `POST /plan/compile`, `GET /scenarios/:id/validate` and `POST /scenarios/:id/steps/:stepIdx/execute`. Everything else is served whatever the range is doing.

**Identities.** Range-bound routes take every container, address and endpoint of the range from the serving package's manifest: nodes (`:nodeId`, probe sources) resolve by topology node, the firewall and RTAC by role and named endpoint. A node the manifest does not know answers `404`. Named firewall policies are package data (`package.yml` `policies`), not recipe code. `POST /firewall/apply` resolves the requested policy from that data and does not require a workshop recipe. `GET /firewall/compare` requires a recipe comparison definition (policy names and zone pairs), then loads the named policies from package data. Compare, workshop reset, test suite, validation report and traffic generation answer `404 {"error":"not supported by this package"}` when the package has no recipe or its recipe does not define that operation.

## Health and build info

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Liveness probe. Returns `{"status": "ok"}` |
| `GET` | `/build` | Build metadata for this binary. Returns `{"version", "commit", "date"}`. Values are injected at build time via ldflags (see `backend/internal/version`); for an unstamped local build they're `"dev"` / `"none"` / `"unknown"`. Note: this is **not** at `/version` because the nginx proxy reserves `/api/version` for FUXA's own HMI version endpoint. |

## Package knowledge

`GET /knowledge` is catalog-only and remains available without a healthy range.
It returns the active package's validated snapshot, never another package's content:

```json
{"package_id":"eu-iec104-substation","revision":1,"sections":[
  {"id":"telecontrol","heading":"IEC104 Telecontrol","description":"...",
   "icon":"radio","accent":"violet","articles":[
     {"id":"iec104-wire-model","title":"IEC104 frames","body":"Markdown body"}
   ]}
]}
```

Section and article order is authored order. File paths are not exposed. With no
loaded active package, the response is `409 {"error":"no active knowledge package"}`.
Loading or reseeding rejects invalid assets and same-package article references
before replacing the catalog; a failed reseed preserves the previous snapshot.
The browser keys this response by package ID and curriculum revision and checks
both before display. `/knowledge#article-id` links resolve within that package.

## Admin

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/admin/seed` | Reload every package from `lab-definitions/`: the package list, each package's topology and default policy (used from the next range start; the serving range keeps the package it started with), and the scenario rows in the database. The active package (see `GET /packages`) must still exist. A load error answers `500` and keeps the previous packages. |

## Range

The range is the one Compose project (`rangerdanger`) that runs the active package's containers; the platform (backend, frontend, proxy) runs separately and keeps serving while the range switches. The backend never starts a range on its own: setup, or the UI, asks for one with `POST /range`.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/range` | Lifecycle status, `200` with the status body below. |
| `POST` | `/range` | Start a switch to a package. Body `{"package":"<id>"}`; the body or the field may be omitted, which restarts the active package. Returns `202` with the status body (phase `preflight`). `409 {"error","phase"}` while a switch or the startup reconcile runs; `400 {"error"}` for an unknown package or a malformed body (unknown fields are rejected). Poll `GET /range` until `ready` or `failed`. |
| `GET` | `/packages` | Package summaries: `[{"id","title","revision","active","operator_view","policies","curriculum"?}]`. `active` marks the range's recorded package, or `RANGERDANGER_PACKAGE` before any range was started. `curriculum` is optional and, when present, includes that package's planning model and binding (including `roles`, `budgetHours`, `teams` and `catalog`). Scenario, workshop and seed routes serve the same package. The response fields are defined in [`packages.go`](../backend/internal/server/packages.go#L14-L26) and populated from the active catalog at [`packages.go`](../backend/internal/server/packages.go#L82-L101). |

Status body:

```json
{"generation": 3, "phase": "ready", "package": "us-dnp3-substation", "target": "",
 "mode": "release", "error": "", "updated_at": "2026-10-09T14:30:00Z"}
```

| Field | Meaning |
|-------|---------|
| `generation` | Increments with every switch that gets past preflight. Range-bound work belongs to one generation; results that land after it stopped are dropped. |
| `phase` | `none` (no range was ever started), `preflight`, `stopping`, `starting`, `configuring`, `ready`, `failed`. |
| `package` | The recorded package. From `stopping` on it is the new package; in `preflight` it is still the old one. Empty in `none`. |
| `target` | The requested package, only in `preflight`; otherwise empty. |
| `mode` | `source` or `release`: which Compose file of the package runs (`RANGERDANGER_MODE`). |
| `error` | Why the last switch failed. A failed preflight leaves the phase where it was (for example `ready`, with the old range still serving) and sets `error`; a failure from `stopping` on removes the attempted range and sets phase `failed`. The previous package is not restored automatically; retry or switch. |
| `updated_at` | When the status last changed; `null` in phase `none`. |

A switch runs these phases, one switch at a time:

1. `preflight` (old range untouched and still served): the package exists, its manifest loads, `docker compose config` succeeds for the mode's Compose file, every image it needs is already present locally (nothing is pulled or built), and its proxy routes and default firewall policy exist.
2. `stopping`: in-flight range-bound work drains, then the old range is removed by its Compose project label together with its anonymous volumes (`down -v`; range models declare no named volumes), and the removal is verified: no container or network with the label, and none of the volumes its containers mounted, remains.
3. `starting`: `compose up --wait` with no build and no pull (healthchecks have 300 s).
4. `configuring`: the package's proxy routes are installed (`nginx -t`, then reload; a rejected file is rolled back), the firewall is waited for, and the package's default policy is imported. Every range start imports it, so a student's policy does not survive a switch.
5. `ready`.

On backend start, a range recorded as `ready` is adopted when every container of its manifest exists and the proxy runs its routes (no restart, no policy import); otherwise the phase becomes `failed` with `range missing: ...`. A switch that was cut short (`stopping`, `starting`, `configuring`) is torn down and recorded as `failed` with `interrupted during <phase>`.

## Workshop

The workshop endpoints operate on the serving range, with nodes from the topology of the package that range started with, and are what the exercise runner uses.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/workshop/graph` | Serving generation's topology: `nodes`, `edges`, package-owned `flows`, `liveness`, `networks`, and `operations.traffic_generation`; zones and per-network `interface_ips` come from the manifest |
| `GET` | `/workshop/status` | Status of all workshop nodes |
| `GET` | `/workshop/nodes/:nodeId/terminal` | WebSocket terminal to a node (see Terminals) |
| `POST` | `/workshop/nodes/:nodeId/exec` | Run a one-shot command on a node. Body: `{"command": "...", "timeout_sec": 30}`. The first token of `command` must be in the backend allowlist: `nmap, mbpoll, dnp3poll, dnp3cmd, curl, tshark, tcpdump, nc, ping, traceroute, wget, cat, ls, ip, ss, netstat` - anything else returns `403 {"error":"command not allowed"}`. On success returns `{"stdout", "stderr", "exit_code", "duration_ms"}`. |
| `POST` | `/workshop/reset` | Reset substation state (clear lockouts, restore voltage, re-enable reclose, etc.). Returns `{"success","actions":[{"action","success","detail"}]}`; any failed step returns `502`. |
| `POST` | `/workshop/test-suite` | Run all exercise validators programmatically |

For `eu-iec104-substation`, `POST /workshop/reset` runs process `reset`, then device `reset` for the OLTC, RMU2, RMU1 and breaker, then GPS `reset_offset`, and waits up to 5 s until every process device reports `solved_revision == revision` at or after its post-reset revision. The PLC is not reset and historian data is kept. Shared firewall-baseline and cleanup steps follow.

Graph node `data.status` is Docker-derived for the active range: `running`,
`exited`, `restarting`, `paused`, `missing`, or `unknown`. Missing means a
successful Docker inventory did not contain the manifest container. An
unavailable Docker client/daemon or an unrecognised Docker state means
`unknown`, never `running`. Zone grouping nodes retain `status: "zone"`.

The graph's `flows` entries have `id`, `source`, `target` (node IDs),
`protocol` (display label), `port`, `cadence`, `category` (`baseline` or
`scenario`) and `liveness_key`. `liveness` is a package-local binding map:
`provider` is `workshop-supervisor`, `workshop-device` (with `device`),
`traffic-generator`, `operator-link`, `operator-device` (with `points` IDs),
`session-presence` (with `transport: tcp|udp`) or `unobserved`. Session evidence
matches the original source/destination addresses, transport and destination
port of the flow, not port alone. `networks` contains the topology's network
IDs, names, subnets and containd `zone` mappings. Operation availability comes
from the serving package's workshop recipe. Arrays and maps are empty rather
than null when the package declares no inventory. No default flow set is
substituted.

These are configured conversations, not packet counters. A consumer may
report active, idle, down or unknown. CC link state establishes IEC104 link
liveness; received status-point quality can only *infer* downstream device
polling. Unknown clock polling is not a failed clock. Session presence does not
prove application success or authorization under the current policy.

## Scenarios (exercises)

Exercises are stored internally as "scenarios" for historical reasons - the user-facing term is "exercise".
Scenario rows belong to the active package, and their `steps` field is a
JSON-encoded ordered array with stable authored step IDs
([`models.go`](../backend/internal/models/models.go#L7-L23)).
The IDs are curriculum identities; step execution remains index-based,
so automation should look up the ID in the served array before posting.
The API applies the supplied index to that same active-package scenario
([`scenarios.go`](../backend/internal/server/scenarios.go#L11-L30),
[`scenario_execute.go`](../backend/internal/server/scenario_execute.go#L45-L80)).

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/scenarios` | List the active package's exercises |
| `GET` | `/scenarios/:id` | Get exercise detail with steps |
| `POST` | `/scenarios/:id/steps/:stepIdx/execute` | Execute the action at zero-based `stepIdx` in the served scenario's ordered `steps` array. The route takes an array index, not a step ID; callers that start from an authored ID must resolve its current index from `GET /scenarios/:id`. The response includes `step_index`, `step_title`, `action_type`, `success`, `results` and `timestamp` (see [`scenario_execute.go`](../backend/internal/server/scenario_execute.go#L20-L38)). |
| `GET` | `/scenarios/:id/validate` | Run all validators for the exercise. Returns `{"scenario_id", "outcome", "checks": [...], "timestamp"}` |

### Validation response

```json
{
  "scenario_id": "baseline-assessment",
  "outcome": "PASS",
  "checks": [
    {
      "name": "PCAP captured",
      "status": "pass",
      "detail": "Baseline capture file found - traffic was recorded"
    }
  ],
  "timestamp": "2026-04-10T02:41:09Z"
}
```

Check status is one of `pass`, `fail`, or `warn`. Outcome is `PASS` if every check is `pass`, `FAIL` if any check is `fail`, and `PARTIAL` if at least one check is `warn` and none are `fail`.

## Substation

Proxied reads from the RTAC and control commands to individual field devices.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/substation/tags` | Flat namespace of all SCADA tags |
| `GET` | `/substation/state` | Full aggregated state with devices, electrical, comms |
| `GET` | `/substation/health` | Device health summary |
| `GET` | `/substation/audit` | Audit log of supervisory commands |
| `GET` | `/substation/network-events` | Up to 50 teaching-zone events since the latest of five minutes ago, generation start and last policy change. Returns `events`, `source`, `since`, `historyTruncated`, `engineError`; on query failure returns empty `events`, `source:"unavailable"` and `message`. |
| `POST` | `/substation/command/:device` | Send a command to a field device. Body: `{"command": "set_tap", "value": -16, "source": "operator"}` |
| `POST` | `/substation/lab-control` | Proxy a Load Simulator override (`active`, `general_load_pct`, `critical_load_pct`, `power_factor`) and optional audit entry (`audit_command`, `audit_target`, `audit_detail`, `source`) to RTAC `/api/lab-control`. |

## Control centre and field

For packages whose manifest has a control centre and a field device simulator (`eu-iec104-substation`). Other packages answer `404 {"error":"not supported by this package"}`. These are read routes plus the operator command and interrogation relays; the student changes field process state only through scenario step actions.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/operator/state` | Control-centre state with `package_id` and `range_generation` added |
| `POST` | `/operator/commands` | Relay one command to the control centre. The CC runs select, execute and feedback; the backend sends it once and never retries. A CC refusal keeps its status. |
| `POST` | `/operator/gi` | No body. Queue CC interrogation; 202 `{"trigger":"operator","requested_at":"<UTC>"}` is acceptance, not completion. 409 `{"error":"interrogation already running"}` if pending/running; 503 `{"error":"link not started"}` if not started. Other CC failures are upstream failures. |
| `GET` | `/operator/journal?after=<seq>` | Bounded CC link, GI, quality, status-change and command journal after a sequence number |
| `GET` | `/field/state` | `{"devices":{<node-id>:<device state>},"clock":<time-source state>|null,"process":<electrical state>}`. `devices` holds every manifest `sim` endpoint except the time source, keyed by topology node ID; the `time-source` sim (GPS) is `clock`, `null` only when none is declared, and more than one is a `502`. Any upstream failure, including a timeout, returns `502 {"error":"..."}` naming the failed upstream; it never returns partial state. |
| `GET` | `/field/audit?after=<seq>&device=<key>` | Per-device audit. Keys: `breaker-1`, `rmu-1`, `rmu-2`, `oltc-1`; defaults to `breaker-1`. Other keys (including an explicit empty key) return `400`. |

### Physical System (schema 3)

Device `/api/state` and process `/api/electrical` (inside `/field/state`)
accept schema 3 only. The student-facing Physical System view shows simulation
observations independent of IEC 104, not operator telemetry. Device
`measurements` includes nullable `p_mw`, `q_mvar`, `pf`; the removed topological fault Boolean is not accepted.
Positive P/Q means flow from source to section; P/Q may be negative. PF is
`abs(P)/sqrt(P²+Q²)` at apparent power ≥0.001 MVA, otherwise null. Invalid
measurements have null P/Q/PF.

Each device has `protection`:
`{function,pickup_a,delay_ms,instantaneous_a,picked_up,picked_up_since,last_trip_current_a}`.
Functions are `overcurrent` (breaker), `fpi` (RMU), `oltc_block` (OLTC).
Instantaneous pickup, picked-up timestamp (UTC), and last-trip current are
nullable. Defaults: breaker 400 A / 1000 ms / 2000 A instantaneous; RMU
300 A / 0 ms / no instantaneous; OLTC 866 A / 0 ms / no instantaneous.
Timers are sample-quantised; I> operates on the first sample at or after
its configured delay (rounded up to the solve cadence). OLTC truth
`blocked` is manual block; effective block also includes protection pickup.

Process state additionally carries
`profile: {enabled,load_factors:{residential,commercial,industrial},source_pu}`
and read-only `f2_tripped` projected from F2 field truth (null before the first successful solve).
F2 protection diagnostics are on `devices["f2-breaker"].protection` in field state;
there is no process-owned F2 latch or `reset_f2` action.
Each process `devices[id]` entry contains delivery/revision fields plus
nullable `current_a`, `p_mw`, `q_mvar`. Before the first solve, numeric
profile values and device metering are null; `solved_at` and delivery
revisions may also be null. Controls `load_multiplier` and top-level
`source_pu` multiply the running profile; reset restores controls to 1.0
without changing whether the profile is enabled. Revisions, generations,
epochs and solve/delivery diagnostics are in collapsed disclosures in the UI.
Audit before/after snapshots use the same device schema 3.

### Operator state and reporting (schema 3)

The CC observes only IEC 104, never field/process truth. The outer state keeps
`schema`, `snapshot_seq`, `rendered_at`, `cache_epoch`, `link` and `points`, plus
backend-added `package_id` and `range_generation`.

`link.gi` is `{state, trigger, started_at, last_complete_at, next_due_at}`.
`state` is `none|running|complete`, `trigger` is `connect|periodic|operator|null`,
and timestamps are UTC or null. State/trigger/start describe the current or
most recent GI on this connection. `last_complete_at` means positive ACT-TERM
on this connection, not complete point coverage or GOOD quality; it remains
set during a later running GI. Disconnect clears integrity and `next_due_at`;
the latter is null unless the link is started.

Each point includes `id`, `device` (source device identity), `limits`
(`{low, high}` in kV, or null), `ioa`, stable `kind`
(`single-point|double-point|step-position|float`), last received `asdu` (null
before receipt), `value`, `quality` (`iv`, `nt`, `sb`, `bl`, `ov`),
`stale_reason`, `source_time`, `source_time_invalid`, `received_at`, `cot` and
`reporting`. `reporting` is null for status or `{cyclic_ms, deadband}` for
floats. GI can replace a timed status type with an untimed type and clear
source time. Link loss retains values/type/COT with IV+NT/`link_lost`.

This selected teaching profile is modelled on WEMAG/SvK practice, not an EU
default: GI on connect, every 15 minutes from the preceding GI start, or via
**Interrogate station**. Status changes are timed spontaneous COT 3 (command
return COT 11); GI is untimed COT 20. Floats are untimed M_ME_NC_1: cyclic
COT 1 every 10 seconds for measurements, 60 seconds for PLC parameters, plus
deadband/quality-driven COT 3. Quiet status never ages out. A currently GOOD
float on a started link silent for more than three cyclic periods gets NT
and `not_updated`; a new report replaces quality. Other stale reasons are
`never_received`, `link_lost`, `device_unreachable`, `heartbeat` and
`measurement_invalid` (or null).

The 42 monitored points include F2 position/trip/current/MW (1501/1502/2501/2502,
indication-only; five supervisory commands unchanged), feeder P/Q/PF (IOAs 2003/2004/2005),
T1 P/Q/PF (2303/2304/2305), and `oltc-1-overcurrent-block` (1307,
OLTC input register 1 bit 7). P is MW, Q is Mvar, PF is dimensionless.
PF with IV displays “—” alongside quality, never the wire placeholder 0.
`plc-1-delay-s` (2403) is the **configured delay**, 30 s; no countdown is
telemetered. PLC setpoint is 20.4 kV with 1% deadband (20.196–20.604 kV).

Voltage limits are 19.80–21.00 kV for `oltc-1-bus-kv`, and 19.00–21.00 kV
for `feeder-voltage` and the four RMU section/source voltage points. A GOOD
point above 0.1 kV alarms strictly outside its limits; equality is normal.
Zero/de-energised, bad quality, or a return within limits clears that process
alarm; bad quality remains visible separately in Points/Alarms.
`feeder-voltage` is the CB1 line-side VT at f1, before the 2.82 km Section1;
the S1 bus voltage is reported by RMU1's source-side point
`rmu-1-source-kv`.

Device comms health uses points grouped by `device`: a non-started link
means `link_down`; every point `device_unreachable`/`never_received` means
`lost`; any `device_unreachable`, `never_received`, `heartbeat` or
`not_updated` means `degraded`; otherwise `ok`. This precedence is strict.
`measurement_invalid` is process quality, not a comms failure. Overview
and Workshop Status show this health. Overview voltage history reads only
the historian's CC observations, never field truth.

Commands require a started link (503 otherwise), current-connection GI
integrity, present feedback and neither IV nor NT (409 otherwise); receipt
age alone does not refuse quiet status. One command in flight, RTU acquisition
and interlock checks and full-GOOD non-transient completion still apply.
`GOOD` requires a value, no true quality flag and null stale reason.

GI journal entries include `trigger`. `status_event` is a COT 3/11 status
value/transient change with `{id, ioa, asdu, cot, value, source_time,
source_time_invalid}`, not a float or a quality-only change. The journal is
a bounded 500-entry ring, not a durable SOE archive. The workshop status
summary’s GI string is integrity: `complete` if `last_complete_at` is set,
otherwise `running` if running, else `none` (null when unavailable).

## Historian

Range-bound read routes to the serving manifest's historian `api` endpoint over the management network. Packages without that endpoint answer `404 {"error":"not supported by this package"}`. There is no ingest or write route.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/historian/points` | The historian's `[{id,ioa,kind,asdu,last}]` (stable kind, last received ASDU or null) array, unchanged |
| `GET` | `/historian/history?point=<id>&since=<rfc3339>&until=<rfc3339>` | `{point,segments:[{cache_epoch,samples:[...]}],gaps:[{from,to}]}`, unchanged, with quality, timestamps and gaps |

A historian `400` keeps its status and body. Other upstream failures return `502`, or `504` on timeout, naming the historian.

## Firewall

Direct operations against the containd NGFW.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/firewall/health` | Management-plane liveness and diagnostics: `status`, `component`, `build`, `time`, `ipv4Forwarding`, `managementBinding`, plus authenticated `managementReservation` or `managementReservationError`. HTTP 200/status `ok` remains liveness even when degraded; containd health read failure returns `503`. |
| `GET` | `/firewall/rules` | Currently-loaded rules |
| `GET` | `/firewall/flows` | containd engine flow table (proxied from containd `GET /api/v1/flows`). Optional `?limit=` 1..5000, default 200; anything else is `400`. Returns `{"flows":[{"flowId","firstSeen","lastSeen","srcIp","dstIp","srcPort","dstPort","transport","application","eventCount","avDetected","avBlocked"}]}`; containd omits empty `srcIp`/`dstIp`/ports/`transport`/`application` and false `av*` fields. `503` with `{"flows":[],"error":"..."}` when containd is unreachable or errors. |
| `GET` | `/firewall/active` | Active policy identity: `{"active_config":"weak"\|"improved"\|"custom", "policy_source":"weak"\|"hardened-reference"\|"plan"\|"manual-custom"}` for the workshop policies. When the source is `plan`, also returns `plan: {label, actionIds, packageId, revision, policyHash, appliedAt}`; otherwise `plan` is omitted. `manual-custom` means the range's policy observer detected a divergent containd commit outside the backend. A new range starts on its package's default policy, with source `weak` or `hardened-reference` for those named defaults (empty for other default names). |
| `GET` | `/firewall/compare` | Diff between weak and improved configs |
| `POST` | `/firewall/apply` | Apply a named configuration. Body: `{"config": "improved"}` (or `"weak"`). Returns `{"status":"applied","active_config":"weak"\|"improved"}` and includes `"warnings": [...]` when containd returned any. Sets server-side `policy_source` to `"weak"` (for weak) or `"hardened-reference"` (for improved). |
| `POST` | `/plan/compile` | Compile selected catalog actions without applying them. Body: `{"packageId":string,"revision":int,"actionIds":string[]}` (max 64 KiB; unknown fields rejected). Returns `{packageId, revision, label, actions, rules, expectations, policyHash, config}`; see below. `409` for a package/revision mismatch with the active range; `400` for malformed requests or unknown action IDs. |
| `POST` | `/firewall/apply-plan` | Same request as compile. Compiles, lints, checks management safety, imports into containd and verifies the running firewall hash. Returns `200 {"applied":true,"warnings":string[],"plan":<compile response>}` and records `active_config=custom`, `policy_source=plan` and provenance. `409`/`400` as for compile; `422 {"error":string,"warnings":string[]}` for compilation, lint, safety, import or running-hash verification failures. Warnings are returned even on success. |
| `POST` | `/firewall/validation-report` | No request body. Runs the segmentation validation matrix against the currently active policy and captures a short PCAP without applying a policy. Returns `markdown`, `summary` (`authorized_pass`, `authorized_total`, `unauthorized_pass`, `unauthorized_total`, `skipped`, `result`), `planProof`, `acceptance`, `active_config`, `policy_source`, `pcap_path`, `policy_hash`, `window_start`, `window_end` and `policy_stable`. `summary.result` follows Hardened acceptance, not plan proof. |

`/api/firewall/health` includes containd's `ipv4Forwarding` (`enabled`, `disabled`, `unknown`)
and `managementBinding.state` (`configured`, `unbound-appliance`, `unknown`).
HTTP 200/status `ok` is management liveness, not forwarding readiness.
Its `managementReservation` preserves the raw authenticated
`GET /api/v1/system/management-reservation` status, including `bootFault`,
`recovery`, `listenerFaults` and guard fault history. A failed diagnostic read
sets `managementReservationError`, not a liveness outage. Both packages reserve
management access for backend `10.99.99.10` and proxy `10.99.99.3`.
Recovery can be pending/failed; staging alone does not clear a boot fault.
Review and commit a reservation-safe candidate, then recheck recovery and
forwarding. Historical guard faults are not necessarily current failures;
optional listener faults require corrected settings and a restart, not just
a commit.

Policy applies use only `POST /api/v1/config/candidate` followed by
`POST /api/v1/config/commit`; both must succeed. There is no legacy
`/config/import` fallback. Before staging, the backend copies appliance-owned
NIC `device` bindings from the running configuration by logical interface name.
The backend enforces the containd 0.1.33 minimum at seed readiness and range
adoption after a backend restart; a 0.1.33 prerelease is below that floor.

The compile response's `label` is `weak`, `partial` or `hardened`.
`actions` lists `{id, kind, chosen}` in catalog order (`kind` is `enforce`,
`detect` or `plan`). Each `rules` entry pairs a `rule` with its `origin`.
`expectations` includes plan and Hardened verdicts and resolved `fromIp`/`toIp`
addresses. `config` is the full containd configuration for export, not
client-authored input; `policyHash` hashes its firewall section.
Provenance retains chosen `actionIds` in catalog order and is cleared by a
named-policy apply, observer divergence or range activation.

Validation returns two independent judgments:

- `planProof: {applicable, pass, policyHash, hashStable, skipped, rows}` checks
  observations against the applied plan. It is applicable only with source
  `plan` and retained provenance; otherwise `rows` is empty and `pass` is false.
- `acceptance: {pass, skipped, rows, preventionGaps}` checks observations
  against Hardened. `preventionGaps` lists IDs expected to be denied by
  Hardened but observed allowed. A faithful partial plan can pass proof
  while failing acceptance.

Each judgment row is `{id, expected, observed, pass}`. The strict TCP
classifier counts a successful connection or explicit refusal as `allow`,
a connect timeout as `deny`, and other failures as `skipped`; skipped rows
fail the judgment. Both judgments require a stable policy identity;
acceptance additionally requires successful PCAP capture startup.

## Traffic generation

Used by Lab 1.2 (Baseline Traffic Analysis) to produce representative substation traffic during a capture window.

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/traffic/generate` | Start traffic generation. Body: `{"duration_sec": 50}` (defaults to `30` when omitted or `<= 0`). Returns `{"status": "generating", "duration_sec"}` |
| `GET` | `/traffic/status` | Current traffic generation state. Returns `{"generating", "started_at", "flows_generated"}` |

## PCAP capture

Unified PCAP API. US uses native containd capture with explicit zones from
the authored topology. Start-only overrides are ephemeral, not staged policy.
An already active backend capture returns `409`. Missing capture zones return
`422`, native start errors (including selector failures) return `502`, and a start
acknowledgement without resolved interfaces (including a queued start) returns
`503`. A `200`/`201` with resolved interfaces
counts as started even if `running` is still false. The backend explicitly stops
native capture at `duration_sec` (or on user stop), waits at most 15 seconds for
shutdown confirmation, then lists files matching the capture prefix. If shutdown
is not confirmed, it logs the timeout and reports completion with available files.
US capture startup never falls back to tcpdump. EU deliberately uses `tcpdump`
for its four-port OR filter, which containd's single port/range filter cannot express.

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/pcap/start` | Start a capture. Body: `{"duration_sec": 60, "name": "baseline"}`. Returns `{"status": "capturing", "file_prefix"}` |
| `POST` | `/pcap/stop` | Stop the current capture |
| `GET` | `/pcap/status` | Current capture state. Returns `{"capturing", "file_ready", "files"}` |
| `GET` | `/pcap/list` | List all capture files available on the firewall |
| `GET` | `/pcap/download` | Download the most recent capture |
| `GET` | `/pcap/download/:name` | Download a specific capture by filename |

## Containd proxy

| Method | Path | Description |
|--------|------|-------------|
| `*` | `/containd/*path` | Transparent HTTP(S) proxy to the containd web UI at the range manifest's firewall `api` endpoint (`http://10.99.99.2:8080/` for the US package). Used for same-origin iframe embedding |

## Terminals (WebSocket)

Terminal endpoints upgrade to a WebSocket and proxy between xterm.js clients and a Docker `exec` session. SSH-based terminals were removed; every node terminal - including the firewall - now goes through Docker exec.

### Endpoints

- `GET /workshop/nodes/:nodeId/terminal` - Workshop node terminal

### Message protocol

The WebSocket carries binary terminal data in both directions. Additionally, the client may send a JSON control message for PTY resize:

```json
{"type": "resize", "cols": 120, "rows": 30}
```

The backend detects text-mode messages starting with `{` and parses them as control messages. Non-JSON text messages and all binary messages are written straight to the shell's stdin. Binary messages from the shell's stdout are forwarded to the client.

### Shell selection

Most nodes get a generic bash/sh selector:

```
sh -c "command -v bash >/dev/null 2>&1 && exec bash -il || exec sh -i"
```

This prefers bash as an interactive login shell when available, falling
back to sh on minimal images. The environment includes
`TERM=xterm-256color`.

**The firewall node is different.** Its terminal first lands in the
containd appliance CLI (matching what `ssh containd@localhost -p 2222`
gives you), and only drops into bash after the CLI exits:

```
sh -c "containd cli; exec bash -il || exec sh -i"
```

So when you open the `fw-1` terminal you'll see the `containd# `
prompt first. Type `shell` (or `exit`) to drop into the underlying
Linux bash (containd runs `CONTAIND_SSH_SHELL_MODE=linux`, so the
bash drop-in carries the same tools as the SSH path). The host port
mapping `127.0.0.1:2222:2222` still exposes the SSH path for users
who want it (`ssh -p 2222 containd@localhost`, password `containd`).

### Resize propagation

The backend calls `ContainerExecResize` on the Docker exec session for every resize message. The frontend sends a resize on initial connect, whenever the container resizes, and whenever the terminal becomes visible again (detected via `IntersectionObserver` for tab switching).

## Data contracts

Struct-level documentation lives in `backend/internal/models/models.go`. Topology, scenario steps, and firewall configs are stored as JSON strings in the database and shaped to match what the frontend expects directly - React Flow graphs for topology, ordered step arrays for scenarios, and the containd policy schema for firewall configurations.
