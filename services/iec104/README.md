# services/iec104 — IEC 60870-5-104 programs (GPLv3)

The European IEC 104 workshop's substation programs, built on
[lib60870-C](https://github.com/mz-automation/lib60870) (MZ Automation).
One image carries all of them; the compose command (or `docker run`
argument) selects which to run.

| Program | Role | IEC 104 station |
|---|---|---|
| `iec104-rtu` | substation RTU / gateway | controlled (server) |
| `iec104-cc` | control-centre SCADA front end | controlling (client) |
| `iec104cmd` | student / attack CLI | controlling (client) |

The increment-2b spike that bootstrapped this directory used a fake
in-process physics model; the spike write-up is in
`docs/plans/iec104/spike-iec104.md`. Increment 3 replaced that with a real
RTU: it reads a package point map, polls the feeder breaker over Modbus
TCP, tags data from the GPS-fed station clock, and executes commands as
Modbus writes.

## Licensing — read before you touch this directory

**This directory is GPLv3. The rest of the RangerDanger repository is
Apache-2.0.** `LICENSE` here is the GNU GPL v3 text; it governs
`services/iec104/` only.

The reason is lib60870-C: it is dual-licensed GPLv3-or-commercial, so any
program linked against it is a GPLv3 combined work. These programs link it
statically, so they are GPLv3 and are built into their own image.

The boundary that keeps the rest of the repo Apache-2.0 is that these
programs talk to Apache-2.0 components **only over network protocols** (IEC
104 / TCP 2404 northbound; Modbus TCP and HTTP southbound). No Apache-2.0 Go
or TypeScript module imports or links this code.

### Dependencies

| Dependency | Version | Licence | How it ships |
|---|---|---|---|
| lib60870-C | v2.4.1 (sha256 `d5708b98…c36c3a`) | GPL-3.0-or-later | tarball fetched + checksummed at build; source under `/usr/share/src` |
| cJSON | v1.7.19 (sha256 `7fa616e3…54562`) | MIT | tarball fetched + checksummed at build; `cJSON.c` compiled into `iec104-rtu` and `iec104-cc`; source + `LICENSE.cJSON` under `/usr/share/{src,licenses}/iec104` |

cJSON (MIT) is GPLv3-compatible and is used by the strict point-map loader
(RTU and CC) and the control centre's HTTP API. `iec104cmd` parses no JSON,
so the Kali static build needs only lib60870.

GPL compliance for released images: the image ships the corresponding source
for every binary — the pinned lib60870 and cJSON tarballs, these sources and
the `iec104cmd` build script live under `/usr/share/src`, and the licences
under `/usr/share/licenses/iec104`.

## Point map (schema 2)

The RTU and control centre are driven by a JSON point map authored per
package at `lab-definitions/packages/<id>/protocols/points.json` and mounted
read-only at `/etc/rangerdanger/points.json`. The shared loader
(`src/pointmap.{c,h}`) is **strict**: an unknown key, a wrong type, or an
out-of-range value makes the program exit non-zero with a message naming the
offending key, rather than booting on a half-understood configuration.

The map declares the common address, APCI timers, the SBO select timeout and
reconnect interval, the station-clock poll (`clock`), the Modbus devices, the
monitored points and the commands. Each point names its device, register
table/address and format (`dp`, `bit`, `f32`, `u16`, `i16`, or `step`); each command names its
Modbus write target and the monitored point that provides its feedback. See
`check/points.test.json` for a complete example. Schema 1 is rejected.
Each device declares a bounded `poll: {table, start, count}`; sources,
measurement-valid bits, transient bits and optional heartbeat addresses must
be inside that block. `u16`/`i16` optionally scale into a float. `step` reads
a signed tap plus a `transient: {address, bit}`; values outside −64…63 are
invalid, never wrapped into the 7-bit VTI. Feedback must have the command's
expected kind and belong to its device.

## iec104-rtu

```
iec104-rtu --config /etc/rangerdanger/points.json --bind 10.30.30.20 --port 2404
```

Behaviour:

- **Modbus southbound.** Polls each device's declared input (FC4) or holding
  (FC3) block every `poll_ms`, using one independent I/O worker per device.
  Socket connects are capped at 250 ms and reconnects back off up to
  `reconnect_interval_ms`; no device shares a serial polling queue. The main
  command state machine never waits for Modbus or motion. It executes commands as a single-register write (FC6), never
  retried. The in-tree Modbus master is `src/modbus.{c,h}`.
- **Quality per point, per cause.** No successful poll within
  `stale_after_ms` → `IV`+`NT` (device unreachable). A `measurement:true`
  point while the device's valid bit is 0 → `IV` (floats only). A point is
  GOOD only when no cause applies. A frozen device heartbeat makes all of
  its points IV after the heartbeat's `stale_after_ms`; PLC IV alone is
  rendered as `stale_reason: heartbeat`.
- **Spontaneous reporting (COT 3).** Every quality change is reported with
  the last value, regardless of deadband; the float deadband suppresses only
  value-only float changes. Status events use the timed type
  (`M_DP_TB_1` / `M_SP_TB_1` / `M_ST_TB_1`) with a CP56Time2a tag; floats use `M_ME_NC_1`.
- **Station clock.** A background thread polls gps-sim over HTTP
  (`src/stationclock.{c,h}`); station time is the last GPS `reported_time`
  advanced monotonically. CP56Time2a carries the invalid (IV) bit whenever
  the clock is not synced (GPS stale, or `sync_status` not in
  `synced_status`).
- **General interrogation (COT 20).** Returns the monitored points in the
  untimed types (`M_DP_NA_1` / `M_SP_NA_1` / `M_ST_NA_1` / `M_ME_NC_1`) with current
  quality, then ACT-TERM. Before the first poll a point answers GI as IV+NT.
- **Commands (SBO per peer).** `C_DC_NA_1`, `C_RC_NA_1`, and
  `C_SC_NA_1`, qualifier 0 only. Select expires after `select_timeout_ms`;
  execute must match its type, value and peer. A fresh good post-select
  poll is required before one FC6 is written, without retry. An exception
  produces negative ACT-CON; a lost echo produces **no ACT-CON**, logged
  `unknown`. A positive echo produces positive ACT-CON. Only a new good
  post-write poll can complete: DP equals the command, mode equals SCS, or
  completed tap equals start ±1 with transient false. The RTU sends COT 11
  on that feedback IOA **before** positive ACT-TERM. Feedback budget is 5 s
  for steps and device `timeout_ms` otherwise. Expiry sends negative
  ACT-TERM without COT 11. Other commands are rejected as busy throughout.

## iec104cmd

```
iec104cmd HOST[:PORT] [-oa N] [-ca N] [--json] <operation>

  gi                            station general interrogation
  stopstart                     STOPDT then STARTDT on the open link
  dc <ioa> <open|close> [-sbo]  double command C_DC_NA_1
  rc <ioa> <lower|raise> [-sbo] regulating-step command C_RC_NA_1
  sc <ioa> <on|off>     [-sbo]  single command C_SC_NA_1
  monitor --hold S [--gi-every S]   hold a session, stream received ASDUs
```

`-sbo` selects before executing; `-delay-ms N` forces select expiry;
`-mismatch` executes the opposite value. Without `-sbo` the command is a
direct execute, which an SBO-only point answers with a negative confirmation.

`--json` makes every operation emit JSON and routes the human log to stderr so
stdout is a clean JSONL stream. `monitor` connects, starts data transfer,
runs GI, then prints one object per line — `connected`, an `asdu` per received
monitoring object, `gi_complete` after each GI, `held` when the hold elapses,
and `link_down` if the link drops first. `--gi-every S` sends a periodic GI
(application traffic for the gate). Exit codes: **0** after `held` or a
successful one-shot (a command requires ACT-TERM), **1** when a one-shot
fails or an execute ACT-TERM does not arrive, **2** on connect failure, **3**
on link loss during a monitor hold. The gate uses `monitor` to hold a session
across a policy apply and a conntrack kill.

`iec104cmd` also ships as a fully static (musl) binary inside Kali and the
shared engineering-workstation images, built in each Dockerfile's pinned
stage (not copied from the iec104 image). Both images include the lib60870
tarball, CLI sources, build script, licence and this README.

## iec104-cc

```
iec104-cc --config /etc/rangerdanger/points.json --bind 10.60.60.20 \
          --target 10.30.30.20:2404 --http 10.99.99.21:8080 \
          --historian-url http://10.70.70.40:8081/api/ingest --historian-bind 10.60.60.20
```

Both historian flags are optional as a pair; an invalid URL/source bind is
fatal. There are no new environment variables. The container still requires
`GATEWAY=10.60.60.2` for its existing gateway entrypoint. The publisher opens
no listener: one schema-2 snapshot POST each second, one in flight, a 500 ms
**total** socket deadline, no retries or backlog, and a dedicated worker
that never holds the IEC104 I/O lock. `/api/health.historian` exposes
`{last_ok_at, failures}`.

The control centre is the trusted operator path. It learns
plant state **only through IEC 104**; the portal reads it through the HTTP
API, which the backend proxies verbatim (`/api/operator/*`).

- **One persistent session.** Connects from `--bind` (a non-local address
  is fatal) to `--target`, sends STARTDT and a station GI after every
  connect, holds the session, and reconnects `reconnect_interval_ms` after
  it ends. APCI timers (`t0`–`t3`, `k`, `w`) come from the point map.
  `connection_seq` counts TCP connections to the RTU. It refreshes
  unchanged observations with a real GI at most every 1 s (or half the
  smallest stale threshold), so freshness checks do not depend on plant
  movement. GIs never manufacture values or bypass RTU quality.
- **Per-point cache.** Value, quality (IV/NT/SB/BL/OV), RTU observation
  time (CP56Time2a; `null` for untimed ASDUs, so floats and GI answers carry
  none), receipt time and COT. The RTU's quality maps to `stale_reason`:
  NT set → `device_unreachable`, IV alone → `measurement_invalid`. On link
  loss every point keeps its value and times and goes IV+NT `link_lost`; it
  is GOOD again only when a fresh report for it arrives on the new session.
  Never-received points have `value: null`, `stale_reason: never_received`.
- **Commands.** Typed `open|close`, `lower|raise`, and `auto|manual`
  operations, select-before-execute, one at a time, at most 10 s, never
  retried. Feedback IV, NT, or older than the device stale threshold is
  refused with 409 **before select**. `completed` needs positive ACT-CON,
  positive ACT-TERM, and intervening good matching COT 11 feedback;
  `rejected` means negative ACT-CON; `failed` means negative ACT-TERM;
  missing confirmations, termination or matching feedback mean `unknown`.
  Results (also journaled) have exactly `{id, point, operation, result,
  reason, selected_at, executed_at, con_at, term_at, feedback}`; absent
  times are null. Feedback is null or `{ioa, value, at}`. Step values are
  `{value: int, transient: bool}`, never flattened into a float.
- **Journal.** A ring of 500 entries (`link_up`, `link_down`, `gi_started`,
  `gi_complete`, `point_stale`, `point_good`, `command`) tagged with the
  random per-boot `cache_epoch`. `link_down.reason`: `t1_timeout` (nothing
  received for t1 before lib60870 dropped the session), `peer_closed` (the
  RTU closed or reset the stream), `io_error` (the CC closed it after a GI
  failure), `connect_failed` (TCP or STARTDT failed; journaled once per
  outage, not per attempt).

| Endpoint | Answer |
|---|---|
| `GET /api/health` | `status`, `link`, `historian: {last_ok_at, failures}` |
| `GET /api/state` | `schema: 2`, `cache_epoch`, `snapshot_seq` (+1 per render), `rendered_at`, `link` (`state`, `peer`, `since`, `last_rx_at`, `connection_seq`, `gi`), `points` |
| `POST /api/commands` | body `{"point":"feeder-breaker-command","operation":"open\|close\|lower\|raise\|auto\|manual"}` → 200 command result; 409 while a command is in flight or feedback is stale; 503 when the link is not started; 400 on bad input |
| `GET /api/journal?after=N` | `cache_epoch`, `entries` with `seq > N`, `latest_seq`, `oldest_seq` (null when empty) |

The HTTP server is in-tree (`src/cc_http.c`): HTTP/1.0 and 1.1, GET and
POST, `Content-Length` bodies only (no chunked), headers ≤ 8 KiB, bodies
≤ 4 KiB, one request per connection, 5 s I/O timeouts, at most 32
concurrent connections. A failed bind of `--http` is fatal. Unknown paths
404, wrong methods 405, unknown query parameters or request fields 400.

## Build and run

```sh
# Build from the repository root (the image COPYs scripts/set-gateway.sh too).
docker build -f services/iec104/Dockerfile -t rd-iec104 .

# RTU against a points.json and a reachable Modbus breaker + GPS.
docker run --rm -v "$PWD/points.json:/etc/rangerdanger/points.json:ro" \
  rd-iec104 iec104-rtu --config /etc/rangerdanger/points.json --bind 0.0.0.0 --port 2404

# Student CLI.
docker run --rm rd-iec104 iec104cmd <rtu-host> gi --json
docker run --rm rd-iec104 iec104cmd <rtu-host> monitor --json --hold 60 --gi-every 2
```

The entrypoint runs `set-gateway.sh` (routes cross-zone traffic through the
containd firewall) before exec'ing the selected program.

## Checks

`check/run.sh` builds the image and runs the RTU against a fake Modbus
feeder breaker (`check/fake_breaker.py`), a fake GPS clock
(`check/fake_gps.py`), `iec104cmd` and the real control centre on throwaway
Docker networks (`$RD_CHECK_PREFIX-*`, default `rd-iec104-check-*`, subnets
172.28.104.0/24 and 172.28.105.0/24).

- RTU + `iec104cmd`: GI values and quality, spontaneous COT 3 changes, SBO
  open/close with COT 11 feedback and ACT-TERM, a no-op command, a negative
  ACT-CON on interlock, Modbus-loss IV+NT, measurement-invalid IV on
  floats, GPS-loss CP56 IV, strict-loader rejection, and `monitor` exit 3 on
  link loss.
- Control centre (`check/cc_checks.sh`, every assertion through its HTTP
  API via `check/ccprobe.py`): GI and the 31 points GOOD; open, close and
  no-op → `completed` with matched feedback; interlock → `rejected`; 409,
  503 and every 4xx/5xx input case; `measurement_invalid` and
  `device_unreachable` with the link started; RTU paused → `t1_timeout`,
  `link_lost` with values and times preserved, then recovery on a higher
  `connection_seq`; RTU stopped → `peer_closed`; an unreachable RTU →
  one `connect_failed` and `never_received` points; fatal binds; SIGTERM
  exit 0; journal ordering and detail shapes.

Increment 4 adds strict schema/format/block/feedback rejection cases, real
FC3 PLC reads, RMU and OLTC FC6 writes in the attempt window, moving and
settled step decoding, all three SBO types, qualifier/type/busy rejections,
negative ACT-TERM with no feedback, lost-echo unknown with no retry, VTI
range invalidity, heartbeat staleness, historian source binding/timeouts,
and a real packet DROP to one device while every healthy peer's actual
poll gaps remain ≤ `poll_ms + 250 ms`. `check/Dockerfile` adds Python and
iptables only to the throwaway test image; service images do not gain them.

Everything it creates (containers, networks, images) is removed on exit.
Needs docker only.

## Reference captures

`captures/` holds Apache-2.0 reference pcaps from the increment-2b spike; see
`captures/README.md` (regeneration against the real RTU is deferred).

## Layout

```
src/pointmap.{c,h}     strict point-map loader (shared with the CC)
src/modbus.{c,h}       in-tree Modbus TCP master (FC3/FC4/FC6)
src/stationclock.{c,h} GPS-fed station clock (HTTP poller)
src/common.{c,h}       logging, quality/time/ASDU formatting, arg parsing
src/client.{c,h}       synchronous wrapper over a lib60870 CS104_Connection (iec104cmd)
src/rtu.c              iec104-rtu: flags, startup, main loop, shutdown
src/rtu_internal.h     private RTU shared model, locking rules and module interfaces
src/rtu_server.c       RTU IEC 104 handlers, connection events, quality and reporting
src/rtu_poll.c         RTU independent Modbus poll workers and register decoding
src/rtu_command.c      RTU SBO validation, FC6 execution and fresh-feedback completion
src/cc.c               iec104-cc: flags, startup, shutdown
src/cc_state.{c,h}     CC shared model: point cache, link/GI status, journal, renderers
src/cc_link.{c,h}      CC IEC 104 session: connect, STARTDT, GI, reconnect, sends
src/cc_command.{c,h}   CC select-before-execute state machine
src/cc_http.{c,h}      CC minimal HTTP/1.1 server
src/cc_api.{c,h}       CC HTTP routes
src/cc_historian.{c,h} bounded snapshot publisher
src/iec104cmd.c        iec104cmd: one-shot ops and the monitor session
Dockerfile             image carrying all three programs
entrypoint.sh          set-gateway.sh then exec the selected program
check/                 the headless RTU, iec104cmd and control-centre checks
captures/              spike-era Apache-2.0 reference pcaps
```
