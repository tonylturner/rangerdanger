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
| cJSON | v1.7.19 (sha256 `7fa616e3…54562`) | MIT | tarball fetched + checksummed at build; `cJSON.c` compiled into `iec104-rtu`; source + `LICENSE.cJSON` under `/usr/share/{src,licenses}/iec104` |

cJSON (MIT) is GPLv3-compatible and is used only by the RTU's strict point-map
loader. `iec104cmd` parses no JSON, so the Kali static build needs only
lib60870.

GPL compliance for released images: the image ships the corresponding source
for every binary — the pinned lib60870 and cJSON tarballs and these sources
live under `/usr/share/src`, and the licences under
`/usr/share/licenses/iec104`.

## Point map (contract §3.1)

The RTU and control centre are driven by a JSON point map authored per
package at `lab-definitions/packages/<id>/protocols/points.json` and mounted
read-only at `/etc/rangerdanger/points.json`. The shared loader
(`src/pointmap.{c,h}`) is **strict**: an unknown key, a wrong type, or an
out-of-range value makes the program exit non-zero with a message naming the
offending key, rather than booting on a half-understood configuration.

The map declares the common address, APCI timers, the SBO select timeout and
reconnect interval, the station-clock poll (`clock`), the Modbus devices, the
monitored points and the commands. Each point names its device, register
table/address and format (`dp`, `bit` or `f32`); each command names its
Modbus write target and the monitored point that provides its feedback. See
`check/points.test.json` for a complete example.

## iec104-rtu

```
iec104-rtu --config /etc/rangerdanger/points.json --bind 10.30.30.20 --port 2404
```

Behaviour (contract §3.2):

- **Modbus southbound.** Polls each device's input block (FC4) every
  `poll_ms`; executes commands as a single-register write (FC6), never
  retried. The in-tree Modbus master is `src/modbus.{c,h}`.
- **Quality per point, per cause.** No successful poll within
  `stale_after_ms` → `IV`+`NT` (device unreachable). A `measurement:true`
  point while the device's valid bit is 0 → `IV` (floats only). A point is
  GOOD only when no cause applies.
- **Spontaneous reporting (COT 3).** Every quality change is reported with
  the last value, regardless of deadband; the float deadband suppresses only
  value-only float changes. Status events use the timed type
  (`M_DP_TB_1` / `M_SP_TB_1`) with a CP56Time2a tag; floats use `M_ME_NC_1`.
- **Station clock.** A background thread polls gps-sim over HTTP
  (`src/stationclock.{c,h}`); station time is the last GPS `reported_time`
  advanced monotonically. CP56Time2a carries the invalid (IV) bit whenever
  the clock is not synced (GPS stale, or `sync_status` not in
  `synced_status`).
- **General interrogation (COT 20).** Returns the monitored points in the
  untimed types (`M_DP_NA_1` / `M_SP_NA_1` / `M_ME_NC_1`) with current
  quality, then ACT-TERM. Before the first poll a point answers GI as IV+NT.
- **Commands (SBO per peer).** Select expires after `select_timeout_ms`;
  execute must match the selected value and peer. On execute the RTU writes
  the register: a positive echo → positive ACT-CON, then it reads the
  feedback register back and sends a COT 11 feedback report followed by
  ACT-TERM; a Modbus exception (e.g. 04 interlock) or timeout → negative
  ACT-CON. A command to the current position is a no-op that still produces a
  COT 11 feedback report. Every command phase is logged to stdout.

## iec104cmd

```
iec104cmd HOST[:PORT] [-oa N] [-ca N] [--json] <operation>

  gi                            station general interrogation
  stopstart                     STOPDT then STARTDT on the open link
  dc <ioa> <open|close> [-sbo]  double command C_DC_NA_1
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
successful one-shot, **2** on connect failure, **3** on link loss during a
monitor hold. The gate uses `monitor` to hold a session across a policy apply
and a conntrack kill.

`iec104cmd` also ships as a fully static (musl) binary inside the Kali image,
built in `Dockerfile.kali`'s own stage (not copied from the iec104 image).

## iec104-cc

The control centre (controlling station with an HTTP read API for the portal)
is owned by lane C2; its design and usage are documented in its own section
when that lane lands.

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
(`check/fake_gps.py`) and `iec104cmd` on a throwaway Docker network
(`rd3-c1-*`), asserting GI values and quality, spontaneous COT 3 changes, SBO
open/close with COT 11 feedback and ACT-TERM, a no-op command, a negative
ACT-CON on interlock, Modbus-loss IV+NT, measurement-invalid IV on floats,
GPS-loss CP56 IV, strict-loader rejection, and `monitor` exit 3 on link loss.
Everything it creates is removed on exit. Needs docker only.

## Reference captures

`captures/` holds Apache-2.0 reference pcaps from the increment-2b spike; see
`captures/README.md` (regeneration against the real RTU is deferred).

## Layout

```
src/pointmap.{c,h}     strict point-map loader (shared with the CC)
src/modbus.{c,h}       in-tree Modbus TCP master (FC3/FC4/FC6)
src/stationclock.{c,h} GPS-fed station clock (HTTP poller)
src/common.{c,h}       logging, quality/time/ASDU formatting, arg parsing
src/client.{c,h}       synchronous wrapper over a lib60870 CS104_Connection
src/points.h           spike IOA constants still used by iec104-cc/iec104cmd
src/rtu.c              iec104-rtu: Modbus-driven controlled station
src/cc.c               iec104-cc: control-centre client (lane C2)
src/iec104cmd.c        iec104cmd: one-shot ops and the monitor session
Dockerfile             image carrying all three programs
entrypoint.sh          set-gateway.sh then exec the selected program
check/                 the headless RTU + iec104cmd checks
captures/              spike-era Apache-2.0 reference pcaps
```
