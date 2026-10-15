# RangerDanger field-device simulators

The legacy US relay, recloser, regulator, and capacitor-bank
simulators are single Go binaries that expose the **same in-memory
state across three protocols simultaneously**:

- **HTTP REST** on `:8080` - `GET /api/state`, `POST /api/command`,
  `GET /api/audit`, `GET /api/health`; `rtac-sim` also exposes
  `GET /api/tags`
- **Modbus TCP** on `:502` - hand-written outstation (no library),
  function codes 1/3/4 read (coils / holding registers / input
  registers) and 5/6 write (single coil / single register)
- **DNP3 TCP** on `:20000` - outstation via the in-tree
  [`dnp3go/`](../dnp3go/) library; supports Read (FC1), Direct
  Operate (FC5), Select/Operate (FC3/4)

That tri-protocol pattern is the point: the same fault, the same
trip, the same tap-change is observable by Modbus and DNP3 masters at
the same instant, and the audit log records the source of every
write so exercises can attribute attacks (`source: 10.10.10.50` vs.
`source: rtac-sim`).

The EU `field-device-sim` is separate: one binary owns breaker, RMU or OLTC
truth, serves Modbus plus separate process/sim HTTP APIs, and does not
expose DNP3.

## Services

| Service | DNP3 addr | Description |
|---------|-----------|-------------|
| `relay-sim`     | 1  | Feeder breaker / protective relay (trip, close, lockout, fault inject) |
| `recloser-sim`  | 2  | Auto-reclose with shot counting and lockout |
| `regulator-sim` | 3  | Load tap changer, ±16 tap range, voltage regulation |
| `capbank-sim`   | 4  | Switched capacitor bank, lockout-after-N-operations |
| `rtac-sim`      | 10 | Supervisory controller. Polls field devices via DNP3 master and HTTP; exposes aggregated state. Read-only DNP3 outstation. |
| `field-device-sim` | - | EU breaker, RMU or OLTC truth owner. Modbus TCP field interface and separate process / sim HTTP listeners. |
| `historian-sim` | -  | Time-series collector polling the RTAC. HTTP-only. |
| `gps-sim`       | -  | GPS/IRIG-B time source; sends synthetic NTP client packets to configured targets. Not an NTP server. |
| `opendss-sim`   | -  | Feeder physics engine (Python, OpenDSS). Calculates energization and voltage from the device states. HTTP-only. |

`gps-sim` reads `NTP_POLL_TARGETS` for its outbound UDP/123 generator:
when unset it uses the existing US target list, when empty it disables
the generator, and a comma-separated list supplies target hosts (the
generator uses UDP port 123 for each).

The RTAC includes OpenDSS freshness in `/api/state` as `physics` (`stale`,
`consecutive_failures`, `last_good_at`, `solved_at`, `last_error`), flattens
the freshness and solve details under `physics.*` in `/api/tags`, and includes
the same object in `/api/health`. The retained `electrical` result includes
`converged` and `solved_at`. Health `status` is `degraded` while physics is
stale and `ok` after a good solve.

## Local development

The Go simulators (relay/recloser/regulator/capbank/historian/gps/
rtac/field-device) share a single module (`services/go.mod`) and a multi-stage
Dockerfile (`services/Dockerfile`). It has a common module/dependency
stage, a shared-code builder, a DNP3-enabled builder for relay,
recloser, regulator, capbank, and RTAC, then one `<target>-builder`
leaf per simulator. Each leaf copies that simulator's Go files and
builds only its binary. Historian and GPS branch from the shared-code
builder without DNP3 package source. RTAC's `dnp3poll` and `dnp3cmd`
tools use a separate, narrow DNP3 tools stage. Final images still share
`sim-base` and copy in only their own simulator binary (plus RTAC's
existing tools and management-init script). `opendss-sim` is
Python/FastAPI and builds from its own
`services/opendss-sim/Dockerfile`.

The EU `field-device-sim` builder copies the in-tree `devices/`
packages and builds `./devices/cmd/field-device-sim` from
`sim-builder`.

```sh
# Run a single sim outside the lab
cd services
go run ./relay-sim          # binds :8080, :502, :20000

# Run tests (a few sims have main_test.go; more coverage is welcome)
go test ./...
```

Each sim has its own `cmd`-style directory with `main.go`. The four
field sims (relay/recloser/regulator/capbank) each add a `modbus.go`
for the Modbus outstation and a `dnp3.go` for the DNP3 outstation
(capbank is DNP3 address 4, controllable via Binary Outputs; see the
point map at the top of `capbank-sim/dnp3.go`).
`historian-sim` and `gps-sim` have `main.go` + `modbus.go` (no DNP3
outstation). `rtac-sim` runs both client and outstation roles
(`dnp3.go`, `dnp3_poll.go`, `modbus.go`, `modbus_poll.go`). Shared
primitives (audit log, command source detection, JSON helpers) live
in `services/shared/`.

## EU field devices

Build with `go -C services build ./devices/cmd/field-device-sim` (or the
`field-device-sim` Dockerfile target). The old binary and schema 1 are gone.
`devices/fielddevice` is the shared audit, revision, measurement and action
core; HTTP and Modbus are adapters over that same mutex-protected model.

Required environment:

| Variable | Values |
|----------|--------|
| `DEVICE_KIND` | `breaker`, `rmu`, `oltc` |
| `DEVICE_ID` | `feeder-breaker` for breaker; `rmu-1` / `rmu-2` for RMU; `oltc-1` for OLTC |
| `DEVICE_RMU_NORMAL` | Required for RMU: `closed` (rmu-1) or `open` (rmu-2) |

The breaker retains node id `breaker-1` but device id `feeder-breaker`.
New devices use the same node/device id. Configure all three explicit-IPv4
listeners per container; unchanged defaults are only suitable for the breaker:

| Variable | Default | rmu-1 | rmu-2 | oltc-1 |
|----------|---------|-------|-------|--------|
| `DEVICE_MODBUS_LISTEN` | `10.40.40.20:502` | `10.40.40.21:502` | `10.40.40.22:502` | `10.40.40.23:502` |
| `DEVICE_PROCESS_LISTEN` | `10.50.50.30:8080` | `10.50.50.31:8080` | `10.50.50.32:8080` | `10.50.50.33:8080` |
| `DEVICE_SIM_LISTEN` | `10.99.99.22:8080` | `10.99.99.24:8080` | `10.99.99.25:8080` | `10.99.99.26:8080` |

`DEVICE_ZONES` is a comma-separated `name=IPv4-CIDR` map for audit attribution;
it defaults to `field=10.40.40.0/24,station=10.30.30.0/24,process=10.50.50.0/24,mgmt=10.99.99.0/24`.
Unknown peers remain `unknown`; attribution is not authorization.

The process listener exposes only `GET /api/truth` and `POST /api/measurements`.
Both use schema 2. Measurements carry identity, generation, solved revision,
solver epoch/sequence/UTC time, `current_a`, `voltage_kv_ll`, `source_kv_ll`,
`section_live`, `source_live` and `fault_current`. A successful delivery returns
`{accepted:true, solved_revision, revision}`: protection/FPI can change the
revision during receipt. Stale truth and replay return 409; invalid input
returns 400. Measurements expire after 3 seconds, and any truth revision
immediately invalidates them. Last known values remain visible, not fresh zeros.

The sim listener exposes `GET /api/state`, `/api/health`, `/api/audit?after=N`
and `POST /api/sim/actions` with exactly `{"action":"<name>"}`. Audit contexts
retain the socket peer; sim actions use protocol `lab`.

- Breaker: `reset_lockout`, `reset`. Faults belong to the process, not the
  device. Measurement fault current trips a closed breaker and latches lockout.
- RMU: `set_local`, `set_remote`, `earth_close`, `earth_open`,
  `reset_fault_passage`, `reset`. Earth close needs an open way and a valid,
  dead section. Fault passage latches on receipt of fault current.
- OLTC: `set_local`, `set_remote`, `block`, `unblock`, `reset`.
- Reset bypasses interlocks, restores initial truth and invalidates measurements.
  Old `/api/sim/fault` and `/api/sim/reset` routes do not exist.

Modbus TCP uses unit 1, zero-based addresses, high-word-first f32/u32.
FC3 reads holding, FC4 reads input, FC6 writes one holding register. Switch
HR0 is 1 open / 2 close. Close needs valid measurements and refuses live-live;
RMU open also needs valid measurements and refuses valid fault current. This
includes the invalid interval between an FPI latch and the re-solve. Breaker
open remains allowed in remote even without valid measurements. Rejections use
exception 04 and audit peer/reason. Breaker IR1 bit 2 is measured fault current,
not owned fault state.

OLTC HR0 is 1 lower / 2 raise (remote/manual); HR2 is 1 manual / 2 auto
(remote only, also allowed during motion). HR1 is the PLC absolute request:
0 clears/arms, or `100 + target` for targets -8 through +8. FC16 is supported
**only for quantity 1 at HR1**. Well-formed HR1 requests always acknowledge;
IR13 reports none/accepted/not_remote/not_auto/moving/not_adjacent/limit/
blocked/unarmed/not_energized (0 through 9). No-op writes are not audited;
ignored writes are audited only when their reason changes. Entering
remote/auto and reset disarm HR1 until a new zero is written. Motion takes
2 seconds on a non-blocking timer; completed tap remains unchanged until it
finishes. Reset cancels motion. FC3 returns HR0=0, HR1=last request, HR2=mode.
IR14 is a wrapping 250 ms heartbeat, independent of process delivery.

Targeted checks:

```sh
go -C services test -race -count=1 ./devices/...
go -C services vet ./devices/...
gofmt -l services/devices
```

## Adding a new sim

1. Create `services/<name>-sim/main.go`. Define a `<Name>State` struct
   protected by a `sync.RWMutex`, with `snapshot()` returning a map
   for HTTP serialisation.
2. Implement HTTP handlers - `handleState`, `handleCommand`,
   `handleAudit`, `handleHealth`. Use `shared.AuditLog` to record
   every write with its source.
3. Add `services/<name>-sim/modbus.go` - copy the relay/recloser
   pattern. Map register addresses to state fields.
4. If the sim speaks DNP3, add `services/<name>-sim/dnp3.go` - define
   `BinaryInputs` / `BinaryOutputs` / `AnalogInputs` and assign a DNP3
   outstation address (next free integer in the table above).
5. Add a target-specific builder leaf and final target in
   `services/Dockerfile`. Derive DNP3 simulators from
   `dnp3-sim-builder`; for a simulator without DNP3 imports, derive from
   `sim-builder` instead:
   ```Dockerfile
   FROM dnp3-sim-builder AS <name>-sim-builder
   COPY services/<name>-sim/*.go ./<name>-sim/
   RUN --mount=type=cache,target=/root/.cache/go-build \
       --mount=type=cache,target=/go/pkg/mod \
       CGO_ENABLED=0 go build -o /bin/<name>-sim ./<name>-sim

   FROM sim-base AS <name>-sim
   COPY --from=<name>-sim-builder /bin/<name>-sim /usr/local/bin/<name>-sim
   EXPOSE 8080 502 20000
   CMD ["sh", "-c", "set-gateway.sh && <name>-sim"]
   ```
6. Add a service block to the range package's
   `lab-definitions/packages/<id>/compose.source.yml` (and mirror in its
   `compose.release.yml`) with appropriate zone networks, healthcheck,
   and `depends_on`, plus a service entry in the package's
   `manifest.json`. The root `docker-compose*.yml` files are the
   platform (backend, frontend, proxy) only.
7. Add the node to the package topology (the file `package.yml`'s
   `topology:` names) and give its
   manifest service the matching `node`; run
   `(cd backend && go run ./cmd/packagelint)`.
8. Add the image and its reviewed build-input envelope to
   [`.github/release-images.json`](../.github/release-images.json). The
   release matrix is generated from this inventory; do not add a workflow
   matrix row.

## DNP3 module

`dnp3go/` is a standalone Go module vendored in this repo. See
[`dnp3go/README.md`](../dnp3go/README.md) for details on the
implemented function codes, point map conventions, and the
`dnp3poll` / `dnp3cmd` CLI tools that ship in the Kali and
engineering-workstation images.

## Modbus outstation

The Modbus implementation is hand-written rather than using a
library. It supports the read/write function codes the exercises
exercise (FC 1/3/4 read, FC 5/6 write) and is intentionally explicit
about register addresses so students can correlate writes against
state changes during attack walkthroughs.
