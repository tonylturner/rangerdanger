# IEC104 recon: devices, protocols, process model

Scope: claims 3, 4, 10, 11; read-only inspection of RangerDanger on `iec104`,
HEAD `b65374380cd550793bd214016ab732c4095b66ea` (v0.1.34).
No source changes, container execution, network/web research, or protocol tests.
Repository-root AGENTS.md read; no nested project guides found for inspected source.
Labels: **V** = verified source/configuration; **I** = inferred consequence, not
runtime-tested; **P** = proposed interface/design; **Q** = unresolved choice.
Paths below are repository-relative. SQLite evidence uses table/key/rowid, not
invented file line numbers. Runtime `data/` observations are local-only, not
reproducible release defaults (`.gitignore:70-76`).

## 1. Ranked findings / numbered claims

1. **HIGH — claim 11 CONFIRMED.** RTAC's authoritative *observed* device values
   come from HTTP `/api/state`, not DNP3 or Modbus. HTTP success populates
   `agg.Devices` and `agg.DeviceComms` (`services/rtac-sim/main.go:104-121`).
   DNP3 results only feed a count log; errors are ignored
   (`services/rtac-sim/dnp3_poll.go:54-72`). Modbus replies are discarded and
   errors likewise ignored (`services/rtac-sim/modbus_poll.go:60-73,97-105`).
   **I:** blocking only DNP3 does not make portal/FUXA values stale or raise
   RTAC comm-loss while HTTP remains allowed. Merely adding IEC104 wire traffic
   would reproduce this teaching defect, not create IEC104-dependent visibility.

2. **HIGH — physical truth is not independent of the operator path.** Device
   state is authoritative for actuator position, but the physics solver receives
   RTAC's cached HTTP observations (`services/rtac-sim/main.go:123-140`;
   `services/opendss-sim/main.py:55-67`). **I:** if device HTTP is blocked after
   an unseen trip, RTAC keeps the old breaker position and feeds that into a
   new, converged, timestamped solve. OpenDSS then models the old position:
   it is not an independently advancing physical-truth service.
   The un-firewalled physics network alone does not solve this dependency
   (`docker-compose.yml:574-587`). Separating truth and visibility is a real
   architecture change, not a relabeling of existing RTAC state.

3. **HIGH — claim 10 CONFIRMED, narrowly.** RTAC upstream DNP3 outstation
   address 10 has 15 BI + 8 AI, no outputs
   (`services/rtac-sim/dnp3.go:92-137`). Its Modbus server is also read-only:
   only FC1/2/3/4 are dispatched (`services/rtac-sim/modbus.go:127-139`).
   But the RTAC is *not* read-only overall: HTTP `/api/command/{device}`
   forwards HTTP commands to devices (`services/rtac-sim/main.go:222-285,670`),
   and RTAC auto controls send HTTP writes (`main.go:475-505`).
   An IEC104 control-centre command cannot presently be translated through an
   upstream DNP3/Modbus control API; that API does not exist.

4. **MEDIUM — claim 3 CONFIRMED for the five DNP3 sims, not every sim.**
   Relay/recloser/regulator/capbank start HTTP + Modbus + DNP3 against their own
   in-process state (`services/relay-sim/main.go:158-167`;
   `services/recloser-sim/main.go:184-193`;
   `services/regulator-sim/main.go:151-160`;
   `services/capbank-sim/main.go:182-191`). RTAC starts all three too plus
   both wire pollers (`services/rtac-sim/main.go:660-674`). Its three device
   inventories each encode the same four named devices:
   `main.go:58-70`, `dnp3_poll.go:28-38`, `modbus_poll.go:84-89`.
   Historian/GPS do not start DNP3 (`services/historian-sim/main.go:257-267`;
   `services/gps-sim/main.go:209-218`).

5. **MEDIUM — claim 4 CONFIRMED, with an important units qualification.**
   Source and linecodes explicitly use 60 Hz; source/loads/cap/regulator use
   12.47 kV; Python multiplies solved bus pu voltage by a 120 V display base
   (`services/opendss-sim/dss/substation_feeder.dss:14-15,41,49,57-61,72,75`;
   `dss/linecodes.dss:7,13`; `circuit.py:28-34,229-235`).
   **V:** no customer 12.47 kV-to-120 V service transformer is modeled in DSS.
   The `_voltage_v` outputs are equivalent/display-base voltages, not separately
   solved low-voltage customer buses. A European UI base is not just the source
   kV, and IEC104 itself does not require 50 Hz or a particular voltage.

## 2. Current device/protocol inventory and truth ownership

**V:** `services/shared/types.go` provides command payload, bounded audit,
source/zone labeling, JSON, HTTP/CORS utilities (`:12-63,65-113,129-135`).
It is **not** a shared device-state store or protocol-neutral device model.
Each sim's `state` and mutex is local to its process; the protocol files are
in the same `package main` and mutate that same object.

| Component | Actual process behavior / protocols / peers |
|---|---|
| Relay | Breaker position, remote-control gate, lockout, fault flag, fixed initial current/voltage (`services/relay-sim/main.go:11-46`). HTTP trip/close/lockout/fault semantics (`:70-129`); DNP3 and Modbus mutate the same position (`dnp3.go:49-100`; `modbus.go:229-275,302-344`). |
| Recloser | Local 2 s reclose delay, 500 ms retrip, three-shot lockout; this timer runs independently of RTAC communication (`services/recloser-sim/main.go:24,41-74,143-159`). HTTP/Modbus/DNP3 expose local closed/reclose state. |
| Regulator | Tap ±16, auto/manual flag, setpoint, estimated-voltage alarm (`services/regulator-sim/main.go:12-46,73-132`). Actual automatic tap decisions live in RTAC (`services/rtac-sim/main.go:434-449`), not in the regulator or OpenDSS RegControl. |
| Capbank | 300 kVAR metadata, switch count, six-operation lockout, auto/manual and thresholds (`services/capbank-sim/main.go:15-57,81-163`). RTAC auto decisions use solved PF/voltage (`services/rtac-sim/main.go:451-469`); DSS physically fixes cap size to 300 kVAR (`services/opendss-sim/dss/substation_feeder.dss:49`). |
| RTAC | Cached device observations + electrical solve + HTTP comms + physics freshness (`services/rtac-sim/main.go:33-50`). HTTP poll ~2 s, DNP3 ~5 s, Modbus ~3 s (`main.go:99-101`; `dnp3_poll.go:47`; `modbus_poll.go:91`). These are serial per-target loops, so timeouts extend effective cycle duration. |
| OpenDSS | HTTP/FastAPI only; gets named device payload, computes switch/tap/cap effects and returns electrical state (`services/opendss-sim/main.py:55-80`; `circuit.py:139-215`). No autonomous background solve or direct field-device connection is started in `main.py:27-45,83-86`. |
| Historian | HTTP polls RTAC `http://10.30.30.20:8080/api/state` (default 5 s); stores up to 500 electrical points (`services/historian-sim/main.go:45-58,75-88,180-254`). Also HTTP + Modbus read/write controls (`main.go:257-267`; `modbus.go:7-25`). `write_back_enabled` is a flag, not implemented RTAC setpoint delivery: HTTP command switch only toggles it (`main.go:132-142`); polling loop only reads. |
| GPS | HTTP + Modbus flags/offset status; no DNP3 (`services/gps-sim/main.go:31-79,209-218`; `modbus.go:5-22`). Sends empty NTPv3 **client** packets at device UDP/123 + RTAC, not an NTP server (`ntp_poll.go:3-19,35-46,62-69`). PTP/IRIG-B are status flags, not clock distribution. |

**V:** relay's measured current/voltage are initialized to 120 A / 12.47 kV,
and HTTP can directly set them (`services/relay-sim/main.go:43-44,131-136`).
OpenDSS's current is independently extracted from `Line.Breaker`
(`services/opendss-sim/circuit.py:244-245`). RTAC Modbus exposes *both* electrical
feeder measurements and relay measurements (`services/rtac-sim/modbus.go:255-266`).
**I:** a breaker trip can yield solved feeder current 0 while relay DNP3 AI
still reports 120 A; no measurement feedback to the relay exists in the traced
update path. A dual-protocol device model must address measurements, not only
binary commands.

## 3. One-breaker data path, operator view, and outage semantics

### Verified path today (direction corrected)

1. A simulated physical transition originates in relay local state: HTTP
   `trip`, injected local overcurrent trip, Modbus coil/register write, or DNP3
   CROB BO0. Local overcurrent ignores remote-control enable
   (`services/relay-sim/main.go:70-129`; `dnp3.go:41-100`;
   `modbus.go:229-275,302-344`). There is no hardware input or independent
   OpenDSS breaker-state feedback.
2. RTAC obtains `breaker_closed` from relay HTTP `/api/state`
   (`services/rtac-sim/main.go:107-120`) and sends that cached position to
   physics (`:126-132`). OpenDSS opens `Line.Breaker` if false
   (`services/opendss-sim/circuit.py:174-176`); a breaker-open result is
   analytically de-energized downstream even on solver nonconvergence
   (`:200-207,291-314`).
3. RTAC exposes the observation in raw JSON/tags, DNP3 BI0 and Modbus coil0/
   holding0 (`services/rtac-sim/main.go:159-163,207-219`;
   `dnp3.go:98`; `modbus.go:159-161,37-39`). The RTAC electrical result also
   contains breaker position; the principal portal diagram uses that version
   (`frontend/components/substation-one-line.tsx:20-24`).
4. Portal browser polls backend JSON `/substation/state` every 2 s (metrics)
   or 3 s (panel), not DNP3/Modbus (`frontend/lib/api.ts:299-306`;
   `components/metrics-overview.tsx:21-43`;
   `components/substation-panel.tsx:25-44`).
   Backend proxies HTTP to RTAC on its management leg
   (`backend/internal/server/substation.go:14-34,59-72`).
5. Operator control reverses this over **HTTP**: portal POST → backend proxy →
   RTAC HTTP forwarding → relay HTTP command (`frontend/lib/api.ts:303-307`;
   `backend/internal/server/substation.go:37-41,119-132`;
   `services/rtac-sim/main.go:234-256`). DNP3 upstream is not this control path.

### Blocking behavior (source-derived inference, NOT runtime-tested)

| Failure | Today's expected consequence and evidence |
|---|---|
| Only field DNP3 TCP/20000 denied | Poll errors silently skipped; DNP3 commands over the blocked path fail to deliver, but allowed HTTP commands/state/physics/UI continue. `services/rtac-sim/dnp3_poll.go:63-71`; `main.go:107-140,253-256`. No DNP3-specific quality/alarm. |
| Device HTTP denied too | Previous `agg.Devices[device]` survives; `DeviceComms=false` only on HTTP transport error (`main.go:108-119`). Physics continues solving old input; stale observations may look physically current. Auto controls use retained state and gate on **physics** staleness, not device comms (`main.go:427-469`). |
| HTTP bad status/invalid JSON | Status is not checked; decode failure does not explicitly clear comms; a JSON error object can replace device state and mark it healthy (`main.go:113-120`). Missing devices default to closed in physics models (`services/opendss-sim/models.py:10-19,44-50`). |
| Physics request fails / nonconverges | RTAC retains last good electrical result, marks stale initially or after three consecutive failures, suppresses auto controls (`services/rtac-sim/physics.go:9,46-64`; `main.go:427-429`). Python also retains last good solve (`runtime_state.py:25-64`). This detects failed solves, not stale physical inputs. |
| Browser/backend poll fails | Existing React state remains; catch only says `// offline`, no elapsed-age quality update (`frontend/components/substation-panel.tsx:25-37`; `metrics-overview.tsx:21-37`). Electrical detail displays RTAC physics-stale only if returned (`substation-electrical.tsx:143-150`). |
| Historian keeps reaching RTAC but field path fails | Records cached electrical values with a new historian polling timestamp; point format has no quality/solved-at (`services/historian-sim/main.go:45-56,205-247`). Can turn stale observations into apparently fresh historical samples. |

**P:** an independent truth plane should solve from authoritative device models
on an explicit non-student-blockable process interface; devices receive solved
measurements there. A separate RTAC/control-centre observation cache must ingest
the selected workshop protocol only and retain value + quality + source timestamp
+ receipt time when traffic stops. An instructor truth view can read truth;
the operator portal must read observed state, not silently bypass it over HTTP.
Preserve management reset/injection as explicitly instructor-only interfaces.
**Q:** location/transport of that truth interface requires a package-specific
topology decision; do not change the shipped networking invariants implicitly.

## 4. OpenPLC and FUXA: what actually connects to whom?

**V — FUXA local configuration:** mounted directory is
`data/fuxa_hmi_appdata` (`docker-compose.yml:288-291`).
Read-only SQLite inspection of `project.fuxap.db`, table `devices`, rowid **2**,
key **rtac**, finds `type=ModbusTCP`, enabled, address `10.30.30.20`, port 502,
slaveid `"1"`, polling 3000, 45 read-only tags. Breaker coil address `"1"`
(FUXA numbering) corresponds to RTAC zero-based coil0
(`services/rtac-sim/modbus.go:9-10,159-161`).
Only other device records are FuxaServer entries (rowids 1, 3).
**I:** this local FUXA operator view reads RTAC Modbus, not DNP3, and does not
control the breaker. It is not evidence of what a fresh checkout will seed:
the DB is ignored and the tracked Dockerfile/entrypoint only start upstream
FUXA (`Dockerfile.fuxa-hmi:3,9-15`; `scripts/fuxa-entrypoint.sh:4-5`).
Tracked-file search found no FUXA project seeder.

**V:** there is also an independent `hmi_poller` sharing FUXA's network namespace,
sending Modbus FC3 to RTAC `10.30.30.20:502`, discarding replies every ~2 s
(`docker-compose.yml:297-320`). Traffic from this sidecar proves neither that
FUXA is configured nor that an operator display updated. It is same-zone and
does not transit the firewall.

**V — OpenPLC:** compose *claims* it is a Modbus client of RTAC, not field
devices (`docker-compose.yml:400-406`), and mounts ignored `data/openplc`
(`:416`). Entrypoint copies/compiles the first ST file, edits runtime DB
program settings, starts PLC and verifies its **Modbus server** on 502
(`scripts/openplc-entrypoint.sh:15-52,89-107`).
It does **not** provision remote Modbus slave/client mappings.
The local `data/openplc/substation_automation.st:3-48` declares located
inputs/outputs and `:126-163` computes raise/lower/reclose flags. These are
memory bindings, not a TCP client implementation/configuration.

**Q / contradiction:** no OpenPLC slave-device DB mapping is supplied in the
checkout's mounted workdir (only ST found). Its upstream image/runtime DB
was not inspected. A configured OpenPLC client could read RTAC, but cannot
deliver the ST's control coils through RTAC's read-only Modbus implementation
(`services/rtac-sim/modbus.go:127-139`). ST input conventions also differ:
comms `%IX0.4..6` and downstream `%IW0` versus RTAC comms coils7..9 and
downstream input register1 (`data/openplc/substation_automation.st:9-11,23-28`;
`services/rtac-sim/modbus.go:17-19,51-55`). Mapping could reconcile this;
without it, the compose automation claim is unverified and cannot be copied
as a working IEC104 control-centre implementation.
OpenPLC carries DNP3 CLI tools for students, not automatic DNP3 integration
(`Dockerfile.openplc:1-12,37-38`).

## 5. DNP3 point maps and protocol-adapter seam

**V:** maps are executable Go callback tables plus comments, not loaded data.

| Source | DNP3 address and map |
|---|---|
| `services/relay-sim/dnp3.go:29-109` | 1; BI0..3 remote/lockout/fault/comms; BO0 breaker; AI0 current A, AI1 voltage kV. |
| `services/recloser-sim/dnp3.go:29-133` | 2; BI0..3 lockout/fault/auto/comms; BO0 closed, BO1 reclose enabled; AI0 shots. |
| `services/regulator-sim/dnp3.go:31-128` | 3; BI0 alarm, BI1 comms; BO0 manual; AI0 setpoint V, AI1 voltage offset V; AO0 tap. Comment says clamp, implementation rejects out-of-range (`:98-103`). |
| `services/capbank-sim/dnp3.go:34-178` | 4; BI0..4 switched/auto/lockout/alarm/comms; BO0 switched, BO1 auto, BO2 lockout reset; AI0 kVAR, AI1 switch count. Thresholds HTTP-only (`:25`). |
| `services/rtac-sim/dnp3.go:92-137` | 10; 15 BI + 8 AI, read-only; capbank absent. “Mirrors Modbus” comment (`:7`) is stale: Modbus has capbank entries (`modbus.go:20-22,46-49,60`). |

**V:** DNP3 library seam is `OutstationConfig` callbacks (`dnp3go/outstation.go:11-51`).
Device state structs and command concepts exist, but there is no reusable
`OpenBreaker` domain operation: relay HTTP, Modbus FC5/6 and DNP3 CROB each
duplicate remote/lockout checks and mutation (`services/relay-sim/main.go:70-98`;
`modbus.go:247-275,316-344`; `dnp3.go:49-100`).
Audit source on wire commands is generic `"dnp3-tcp"`/`"modbus-tcp"` rather than
peer identity; zone attribution becomes unknown
(`dnp3.go:89-98`; `modbus.go:273-274`; `services/shared/types.go:65-94`).

**P — clean seam (new files, not implemented):**
- Put typed models in e.g. `services/devices/relay.go`, `recloser.go`,
  `regulator.go`, `capbank.go`: coherent snapshot, measurement update,
  command execution with domain interlocks, local protection/timers, change
  subscription, audit context `{peer, protocol, zone, source}`. HTTP,
  Modbus, DNP3 and IEC104 adapters all invoke this same model.
- DNP3 CROB BO0 → semantic `OpenBreaker`/`CloseBreaker`; IEC104 double command
  IOA → the same operation. Wire select/session/ack handling belongs in
  adapters; final execute rechecks model interlocks. Model must emit changes
  for spontaneous events, including local protection and measurement updates.
- Modify each field `main.go`, `dnp3.go`, `modbus.go` and tests; add each
  `iec104.go` or shared adapter package. Preserve shipped JSON names through
  explicit DTOs. `services/shared/types.go` can retain audit/HTTP utilities.
- Replace duplicate RTAC target lists with resolved device descriptors and
  protocol client adapters; modify `rtac-sim/main.go`, `dnp3_poll.go`,
  `modbus_poll.go`, `dnp3.go`, `modbus.go`, `physics.go`; add IEC104 client/
  server files. DNP3 read results must actually map into observed tags if
  DNP3 is selected; do not keep HTTP fallback as operator truth.
- Update `services/Dockerfile:6-17,19-59` builder COPY/dependency envelopes,
  module declarations and release build-input inventory
  (`services/README.md:107-115`). Domain packages currently would not be
  copied by these narrowly scoped builders.

## 6. IEC104 server/client requirements (proposal, not existing capability)

**V:** no IEC104 Go implementation/dependency found in tracked Go source,
three go.mod files, or project vendor trees (none found).
`services/go.mod:7-9` only requires local `dnp3go`; `dnp3go/go.mod:1-5`
has no dependencies; `backend/go.mod:7-17` is API/infrastructure dependencies.
“Vendored” DNP3 here means an in-tree module + replace, not Go vendor/.
No web/library selection attempted. **Q:** choose a reviewed pure-Go library
supporting *both* roles, or implement an in-tree module; API, license,
multi-arch builds, malformed-input behavior and interoperability are unverified.

**P — minimum functional profile:**
- TCP server RTU/gateway and persistent client control-centre sessions, normally
  port2404; I/S/U frames, STARTDT/STOPDT/TESTFR, sequence/ack windows, t0/t1/t2/t3
  timers, reconnect/backoff, partial TCP reads and bounded buffers.
  Existing one-shot `dnp3go.Poll` connects/sends/reads/closes
  (`dnp3go/master.go:38-59`); it is not an IEC104 session implementation.
- Versioned point-map data referencing semantic device points, not callbacks
  or raw DNP3 indices: common address (CA), 24-bit IOA, telemetry TypeID,
  command TypeID/command binding, engineering units/base, scale/offset,
  invalid/not-topical quality rules, timestamp policy, deadband, event policy,
  interrogation group and select/direct control policy.
  Validate duplicate `(CA,IOA)`/conflicting types, unknown semantic points,
  ranges and write capability. Generate adapter maps and HMI metadata from
  the same resolved map; distinguish state IOA from command IOA.
- Breaker slice: double-point state (`M_DP_NA_1`, optionally timestamped
  `M_DP_TB_1`), double command (`C_DC_NA_1`), measured voltage/current
  short-float (`M_ME_NC_1`, optional `M_ME_TF_1`). Exact IOAs/CA/type profile
  are **Q**, not current code. Do not encode an unknown/stale position as closed.
- Select/execute: track exact point/value/qualifier/peer/session and expiry;
  select does not actuate; execute without valid selection rejects when SBO
  required; revalidate interlocks and return activation confirmation/negative
  cause and termination as applicable. Handle cancel/timeout/disconnect and
  duplicate execution safely. DNP3 provides a limited precedent, not reusable
  IEC104 semantics: one pending per connection, 5 s timeout
  (`dnp3go/outstation.go:73-87,299-303,355-364`); analog SBO does not compare
  selected value (`:379-383`), so do not copy it verbatim.
- Spontaneous events: coherent model-change snapshots, cause=spontaneous,
  quality + optional CP56Time2a timestamps, bounded queue, overflow policy,
  connection recovery/resynchronization, analog deadband/rate limiting.
  Existing DNP3 server is request-driven; class reads have no event buffering
  (`dnp3go/outstation.go:122-156,214-219`). Encoders mark every value online
  (`dnp3go/application.go:286,310,334,358`); new quality cannot be implemented
  by a stale bool alone.
- General interrogation `C_IC_NA_1` with QOI, activation confirmation,
  a consistent point snapshot using interrogation COT, termination; client
  performs GI after STARTDT/reconnect and then applies spontaneous updates.
  Freeze coherent value/quality/time at snapshot boundaries.
- Client converts reports into the existing portal observed-state/tag API
  and issues commands over IEC104 (not RTAC HTTP-to-device bypass), reports
  protocol/session failure independently of solver health. Add headless
  `iec104poll`/command diagnostic tools for curriculum/verification.

**Q — placement:** decide whether IEC104 ends at each field RTU with RTAC as
client, at RTAC gateway with a *new* control-centre client, or both. For a true
gateway profile the upstream server reads observed state and translates commands
downstream; server BI/AI-only DNP3 is not enough. Keep portal/instructor HTTP
distinct. FUXA IEC104-driver availability is unknown from its opaque pinned
image; a first-party client avoids assuming that capability. HTTP portal JSON
can remain, but its producer must be the control-centre observation cache.

## 7. Electrical/UI regional coupling — exact locations

| Coupling | Verified locations / required decision |
|---|---|
| 60 Hz | `services/opendss-sim/dss/substation_feeder.dss:15`; `dss/linecodes.dss:7,13`. Python has no frequency constant/parameter. Inspect engine defaults/solution frequency during future 50 Hz validation; changing source label alone is insufficient evidence. |
| 12.47 kV | DSS source14, load41/72, cap49, both transformer windings57-58, voltagebases75; `services/opendss-sim/circuit.py:29-30,270,297`; relay initial measurement `services/relay-sim/main.go:44`. |
| 120 V display base | `services/opendss-sim/circuit.py:31,101-102,229-235,298`; inert RegControl `dss/substation_feeder.dss:61-68` uses vreg120/band2/ptratio60/ctprim300. Do not imply real 120 V customer transformer. |
| Imperial/US feeder data | IEEE13-bus ACSR601/602, miles (`services/opendss-sim/dss/linecodes.dss:1-17`); DSS feet22/30/34/53, 64,000 ft main and 16,000 ft lateral. Metric conversion must convert impedance units as well as lengths; IEC104 requires neither a different conductor nor these dimensions. |
| Voltage control/alarm math | Regulator `main.go:25-26,46,81-82,94-95,124-126`; DNP3 duplicate `dnp3.go:110-111`; Modbus duplicate `modbus.go:302-303`. Hardwired 0.75 V/tap, 117.6 V estimate, 108/132 V alarm bounds are not OpenDSS measurements. |
| RTAC/regional thresholds | `services/rtac-sim/main.go:399-404,438-448,458-468,642-655`: 120 default setpoint, 1.5 V deadband, 126 cap cutout, 114/126 critical alarms. Capbank defaults `services/capbank-sim/main.go:53-54`. |
| One-line / metrics | `frontend/components/substation-one-line.tsx:28-29,54,213-215`: 120 fallback, 114/126 thresholds, literal 12.47 kV bus. `metrics-overview.tsx:58-65,100,123,138-141`: ANSI bounds, feeder label and chart band. |
| Electrical detail | `frontend/components/substation-electrical.tsx:24-31,55,154-158`: literal 12.47 kV, 120 V, 600 A rating, 120 setpoint fallback. ±5% comparisons `:72` are dimensionless, but pu divisor is hardcoded. |
| ANSI identifiers/curriculum | Portal breaker52 (`frontend/components/substation-electrical.tsx:222-225`); equipment prose explicitly teaches ANSI C84.1 114–126 V and North American12.47 kV/60 Hz (`frontend/lib/knowledge-content/substation-equipment.ts:149,214,228-235,260`). These are curriculum/package choices, not protocol constraints. |
| Local PLC | `data/openplc/substation_automation.st:23,47,57-61,120-123`: x10 measurements/setpoint and ANSI114/126/120 thresholds. Local ignored config; needs deterministic packaging, not an untracked edit. |

**P:** resolve independent `{frequency_hz, primary_kv_ll, display_voltage_base_v,
voltage_limits, tap_step_pu, controller_deadband, feeder_rating_a, units,
circuit_file, regional_labels}` per process/package. Derive equivalent voltage
and volts-per-tap from bases; do not blanket-replace every `120` (relay120 A
is current). A 50 Hz/230 V display EU package still needs an explicit primary
voltage, feeder geometry and educational alarm standard; Europe is not one
fixed national distribution profile.

## 8. Missed couplings, observed debt, and validation ownership

- **HIGH debt:** truth derives from operator HTTP cache; selected protocol can
  fail without a process-visible comm alarm (`services/rtac-sim/main.go:107-140`;
  `dnp3_poll.go:63-71`). Build the truth/observation split before claiming
  protocol-blocking-to-staleness fidelity.
- **HIGH debt:** no observed-state age/quality; RTAC holds its aggregate mutex
  across serial HTTP calls/solve (`main.go:105-145`). LastPoll advances even on
  failures; physics stale tests count failures, not elapsed time
  (`physics.go:62-64`). Portal snapshot freshness and protocol freshness need
  separate tests.
- **MEDIUM debt:** duplicated protocol/domain mutations and tests. DNP3 wire
  tests use **mirrored** point maps, not actual simulator adapters
  (`services/dnp3wire/wire_test.go:5-7,53-124`); they cannot prove map/model
  parity. Even fixture voltages are arbitrary13.8 kV/13800 V (`:57,121`).
- **MEDIUM debt:** local FUXA/PLC data has no tracked seed, RTAC Modbus writes
  unsupported; OpenPLC automation comment overclaims (`docker-compose.yml:400-406`;
  `services/rtac-sim/modbus.go:127-139`). Determine shipped fresh-start behavior
  before assigning it an IEC104 control-centre role.
- **MEDIUM debt:** GPS is traffic theater; audits use host `time.Now()`, not GPS
  reported time (`services/shared/types.go:38-40`;
  `services/gps-sim/main.go:50-57`; `ntp_poll.go:43-46`). IEC104 timestamp/time-sync
  scenarios need a real shared clock abstraction; do not claim GPS spoofing
  currently corrupts remote event timestamps.
- **MEDIUM debt:** historian stamps cached values without quality
  (`services/historian-sim/main.go:205-247`) and marks communication healthy
  without checking HTTP/decode result (`:195-206`). Write-back flag is not an
  implemented command path.
- **LOW debt:** stale public guide says historian HTTP-only and GPS UDP NTP
  server (`services/README.md:31-32`); actual startup/protocol evidence above
  contradicts it. Knowledge prose still says 2,000/500 ft feeder/lateral
  (`frontend/lib/knowledge-content/substation-equipment.ts:262,265`) versus
  DSS64,000/16,000 ft (`substation_feeder.dss:30,53`).

**Affected paths/interfaces beyond the seam:** Python `models.py`, `main.py`,
`circuit.py`, `runtime_state.py`, DSS files and tests for independent truth,
measurement feedback and regional bases; RTAC DTO/tag/health APIs; historian
quality/history API; GPS clock if time exercises required; backend
`internal/server/substation.go` destination/command routing; frontend
`lib/api.ts`, `components/substation-*`, `metrics-overview.tsx`, load simulator
bases and knowledge content; package-owned PLC/FUXA seed config; Docker build
envelopes and source/release compose variants. Networking/policy changes remain
explicit package-topology work for the orchestrator, not this recon lane.

**Validation ownership, all tests NOT RUN here (0 tests executed):**
- Device/model lane: `cd services && go test -race -count=1 ./relay-sim
  ./recloser-sim ./regulator-sim ./capbank-sim` plus actual adapter-parity tests.
- Protocol lane: `cd services && go test -race -count=1 ./dnp3wire`; targeted
  dnp3go SBO/round-trip tests; new IEC104 headless loopback/interoperability,
  segmentation/timeouts, GI/events/order/quality, malformed TCP, select mismatch,
  expiry, rejection and reconnect tests. Existing wire tests cover only RTAC/
  relay fixture maps (`services/dnp3wire/wire_test.go:130,235,254,272,297`).
- Truth/RTAC lane: `cd services && go test -race -count=1 ./rtac-sim`;
  `cd services/opendss-sim && python -m pytest -q` (CI owns Python tests too:
  `.github/workflows/ci.yml:79-92`). Existing staleness tests cover bad solve
  retention, not lost field protocol with independently advancing physics
  (`services/rtac-sim/physics_test.go:13,68,116,150`;
  `services/opendss-sim/tests/test_physics_staleness.py:28-85`).
- UI/history lane: test old-value-with-bad-quality rendering, both truth and
  observation views, bases/units/alarms, deterministic FUXA seed and historian
  quality. Do not treat packet sidecars as HMI correctness tests.
- Orchestrator after integration: serial full CI (all Go modules/race/build,
  gofmt, frontend lint/vitest/build, Python tests, compose config, vulnerability
  gates), source/release amd64/arm64 image builds, fresh-reset shipped-package
  baseline, selected IEC104 outage/control slice, firewall/events/lab smoke.
  Full suites/live containers/UI were deliberately not run for planning-only recon.

**Brief defects/limits:** numbered claims remain current despite old proposal
baseline, but “physical truth separate” is a goal rather than existing behavior;
claim3 must exclude historian/GPS; claim10 is DNP3-only; claim4 confuses display
base with a physical secondary if read literally. New protocol/library choice,
regional electrical profile, RTU/gateway/control-centre placement, instructor
truth transport, FUXA-driver support and clean-install seed ownership are open
design questions. No claim of tested firewall outcomes or external IEC104
library availability is made. HEAD/source tree remained untouched.
