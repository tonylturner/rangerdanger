# services/iec104 — IEC 60870-5-104 programs (GPLv3)

This directory is the increment-2b spike for the European IEC104 workshop
(see `docs/plans/iec104/README.md` §3.5 and
`docs/plans/iec104/spike-iec104.md`). It builds three small programs on
top of [lib60870-C](https://github.com/mz-automation/lib60870) (MZ
Automation):

| Program | Role | IEC 104 station |
|---|---|---|
| `iec104-rtu` | substation RTU / gateway | controlled (server), CA 1 and CA 2 |
| `iec104-cc` | control-centre SCADA front end | controlling (client) |
| `iec104cmd` | student / attack CLI for Lab 2.3 | controlling (client) |

## Licensing — read before you touch this directory

**This directory is GPLv3. The rest of the RangerDanger repository is
Apache-2.0.** `LICENSE` here is the GNU GPL v3 text; it governs
`services/iec104/` only.

The reason is lib60870-C: it is dual-licensed GPLv3-or-commercial, so any
program linked against it is a GPLv3 combined work. These three programs
link it statically, so they are GPLv3 and are built into their own image.

The boundary that keeps the rest of the repo Apache-2.0 is that these
programs talk to Apache-2.0 components **only over network protocols**
(IEC 104 / TCP 2404 northbound, and Modbus TCP / HTTP southbound in
increment 3). No Apache-2.0 Go or TypeScript module imports or links this
code. A lint that enforces "no Apache-2.0 module imports `services/iec104`"
is planned for increment 3 (plan §6, "GPL services").

GPL compliance for released images (see the spike report for the full
checklist): the image ships the corresponding source for every binary it
contains — the pinned lib60870 tarball and these sources live under
`/usr/share/src` in the image, and the licence under
`/usr/share/licenses/iec104`.

## Build and run

```sh
# Build the image (multi-arch capable; see the Dockerfile header).
docker build -t rd-iec104:spike services/iec104

# RTU server on 2404
docker run --rm -p 127.0.0.1:2404:2404 rd-iec104:spike iec104-rtu

# Control centre against a running RTU
docker run --rm rd-iec104:spike iec104-cc <rtu-host>

# Student CLI: general interrogation, then a double command with select
docker run --rm rd-iec104:spike iec104cmd <rtu-host> gi
docker run --rm rd-iec104:spike iec104cmd <rtu-host> dc 3001 open -sbo
```

## Point map (spike)

In-memory, defined in `src/points.h`. A station GI returns the monitoring
points for the requested CA. Increment 3 moves this into package data
(`lab-definitions/packages/<id>/protocols/`).

| CA | IOA | Type | Meaning |
|---:|---:|---|---|
| 1 | 1001 | `M_DP_NA_1` / `M_DP_TB_1` | feeder breaker position (GI / spontaneous) |
| 1 | 2001 | `M_ME_NC_1` | feeder current (A) |
| 1 | 2002 | `M_ME_NC_1` | busbar voltage (kV) |
| 1 | 3001 | `C_DC_NA_1` | breaker double command, select-before-execute only; not returned by GI |
| 1 | 65535 | `M_ME_NC_1` | test measurement at the 16-bit boundary |
| 1 | 65536 | `M_ME_NC_1` | test measurement immediately above the 16-bit boundary |
| 1 | 16777215 | `M_ME_NC_1` | test measurement at the maximum 24-bit IOA |
| 2 | 4001 | `M_ME_NC_1` | auxiliary-station frequency (Hz) |

IOA 0 is not a configured monitoring point. It is used as the information
object address of the `C_IC_NA_1` station-interrogation command. A double
command sent to the unassigned `(CA 1, IOA 0)` is answered negatively with
COT 47, “unknown information object address.” That response is the
standard's unknown-address cause, not a special rule that every use of
address zero is invalid. See IEC 60870-5-101:2003+A1:2005, cause of
transmission code 47 (“unknown information object address”), as used by
IEC 60870-5-104:2006+A1:2016; the `C_IC_NA_1` Type ID 100 definition
specifies the interrogation command.

The RTU accepts a breaker command only as select-before-execute: a direct
execute, an execute after the select timeout, an execute whose value
differs from the selection, or an execute from a connection other than the
one that selected, is answered with a negative confirmation. `SIGUSR1` simulates a
local protection trip (a spontaneous open that no client commanded).

## Correctness checks

`check/run.sh` builds the image, runs the RTU, the control centre and
`iec104cmd` on a throwaway Docker network, captures TCP/2404 on that
network's bridge, and asserts each check on the tshark decode. Evidence
lands in `build/iec104-spike/`. Results are recorded in
`docs/plans/iec104/spike-iec104.md`.

## Reference captures

The capture bundle is test data licensed under Apache-2.0; see
`captures/LICENSE`. The IEC104 programs and their linked lib60870-C
remain GPLv3 as described above. The pcaps contain protocol traffic, not
lib60870 source or binaries. See [`captures/README.md`](captures/README.md)
for the point map, capture contents, harness streams, version/configuration
details, SHA256 checksums, and the exact regeneration command.

## Layout

```
src/points.h     shared point map (CA/IOA/type)
src/common.{h,c} logging, quality/time formatting, arg parsing
src/client.{h,c} synchronous wrapper over a lib60870 CS104_Connection
src/rtu.c        iec104-rtu: server, GI, select-before-execute
src/cc.c         iec104-cc: client, GI, SBO, stale-on-link-loss cache
src/iec104cmd.c  iec104cmd: one-shot GI / single / double command
Dockerfile       multi-arch image carrying all three programs
check/           rerunnable correctness checks (run.sh, capture helper)
captures/        Apache-2.0 reference pcaps, checksums and regeneration
```
