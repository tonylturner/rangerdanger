An API's role matters as much as its route name. An operator command, a read-only physical-state export and a simulation action can all use HTTP while representing completely different authority. Keep these surfaces separate when collecting evidence: otherwise a convenient management read can conceal the very telecontrol outage the exercise is meant to demonstrate.

### Native CC and portal operator routes

The RTU offers IEC 104 northbound and Modbus acquisition/control southbound. It has no operator REST API. The native CC management listener is `10.99.99.21:8080` and serves:

- `GET /api/health` for link/observation health and historian publishing status.
- `GET /api/state` for schema-3 observed state.
- `GET /api/journal?after=N` for bounded link, GI, status-change, point-quality and command evidence.
- `POST /api/commands` for one operator command.
- `POST /api/gi` with no body to queue operator interrogation: 202 with `trigger: operator` and UTC `requested_at`; 409 if running/pending, 503 if link not started. Acceptance is not GI completion.

The portal proxies state, journal, commands and GI as `/api/operator/state`, `/api/operator/journal`, `/api/operator/commands` and `/api/operator/gi`. The **Interrogate station** button uses the GI relay. State adds `package_id` and `range_generation`; these identify the backend range generation and do not replace the CC's `cache_epoch`.

A command body contains exactly point and operation, for example `{"point":"rmu-1-command","operation":"open"}`. Breaker/RMU operations are open/close, OLTC step lower/raise, OLTC mode auto/manual. An operation incompatible with the command kind returns 400. A command already in flight, missing current-connection GI integrity, missing feedback or feedback IV/NT returns 409. Quiet status receipt age alone does not refuse a command; full GOOD and non-transient matching feedback are still required for completion; a link not started returns 503. A returned command result still needs the [SBO interpretation](#iec104-sbo-commands), not merely an HTTP-success check.

State uses schema 3; points include source `device` and voltage `limits` (or null). `link.gi` has `state`, `trigger` (connect/periodic/operator or null), `started_at`, `last_complete_at` and `next_due_at`. Integrity is a non-null `last_complete_at` on this connection and survives a later running GI; disconnect clears it. Each point has stable `kind`, last received `asdu` (null before receipt), and `reporting` (null for status, `{cyclic_ms, deadband}` for floats). Float silence beyond three cyclic periods sets NT/`not_updated`; quiet status does not expire.

### Read-only physical and historian exports

`GET /api/field/state` returns `devices` keyed by node id, a separate `clock` for the GPS time source, plus `process`. The breaker key is `breaker-1`, although its device id is `feeder-breaker`. GPS is not a feeder device, so it is reported under `clock`, not `devices`. Any failing upstream makes the aggregate fail with 502 naming the upstream, rather than synthesising a partial “healthy” state.

`GET /api/field/audit?after=N` currently proxies the **breaker** audit only. It is not a combined audit for RMUs, OLTC and GPS. Those devices' own management APIs provide their separate audits when accessed through an authorised maintenance context.

`GET /api/historian/points` and `/api/historian/history?…` relay the historian's management read API. They do not expose ingestion or write-back. A successful portal read is not proof of the enterprise-to-DMZ conduit.

### Lab actions are not another operator channel

Authored steps execute through `POST /api/scenarios/:id/steps/:stepIdx/execute`. The backend resolves the package recipe, device and allowed action. Field devices receive `POST /api/sim/actions` with an action name; process fault/load/source actions go to `/api/process/actions`; GPS offset actions use its existing `/api/command`.

The UI has no instructor control plane to bypass these authored steps. Reset Lab uses the ordered [reset contract](#eu-reset-acceptance). Lab actions change simulated conditions; they do not prove an operator's IEC 104 command path works.

### Evidence and limits

Save the CC journal cursor before an attempt, then export after it. The journal holds 500 entries; check `cache_epoch`, `oldest_seq` and `latest_seq` before claiming completeness. `snapshot_seq` advances on every state render, so polling an API can itself create sequence gaps in historian snapshots.

The range is a local training environment, not an authenticated production control API. Separate authority by purpose even where management reachability is shared. Never use field or solver truth to populate missing CC observations, and never automatically retry an ambiguous command.

Implementation: backend `operator.go`, `field.go`, `historian.go`, `eu_recipe.go`; `services/iec104/src/cc_api.c` and `cc_state.c`.
