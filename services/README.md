# RangerDanger field-device simulators

Each simulator is a single Go binary that exposes the **same
in-memory state across three protocols simultaneously**:

- **HTTP REST** on `:8080` - `GET /api/state`, `POST /api/command`,
  `GET /api/audit`, `GET /api/health`
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

## Services

| Service | DNP3 addr | Description |
|---------|-----------|-------------|
| `relay-sim`     | 1  | Feeder breaker / protective relay (trip, close, lockout, fault inject) |
| `recloser-sim`  | 2  | Auto-reclose with shot counting and lockout |
| `regulator-sim` | 3  | Load tap changer, ±16 tap range, voltage regulation |
| `capbank-sim`   | 4  | Switched capacitor bank, lockout-after-N-operations |
| `rtac-sim`      | 10 | Supervisory controller. Polls field devices via DNP3 master and HTTP; exposes aggregated state. Read-only DNP3 outstation. |
| `historian-sim` | -  | Time-series collector polling the RTAC. HTTP-only. |
| `gps-sim`       | -  | NTP/IRIG-B time source. UDP NTP server. |
| `opendss-sim`   | -  | Feeder physics engine (Python, OpenDSS). Calculates energization and voltage from the device states. HTTP-only. |

## Local development

The Go simulators (relay/recloser/regulator/capbank/historian/gps/
rtac) share a single module (`services/go.mod`) and a single
multi-stage Dockerfile (`services/Dockerfile`). `opendss-sim` is
Python/FastAPI and builds from its own `services/opendss-sim/Dockerfile`.

```sh
# Run a single sim outside the lab
cd services
go run ./relay-sim          # binds :8080, :502, :20000

# Run tests (a few sims have main_test.go; more coverage is welcome)
go test ./...
```

Each sim has its own `cmd`-style directory with `main.go`. The four
field sims (relay/recloser/regulator/capbank) each add a `modbus.go`
for the Modbus outstation and (for relay/recloser/regulator) a
`dnp3.go` for the DNP3 outstation; capbank exposes Modbus only.
`historian-sim` and `gps-sim` have `main.go` + `modbus.go` (no DNP3
outstation). `rtac-sim` runs both client and outstation roles
(`dnp3.go`, `dnp3_poll.go`, `modbus.go`, `modbus_poll.go`). Shared
primitives (audit log, command source detection, JSON helpers) live
in `services/shared/`.

## Adding a new sim

1. Create `services/<name>-sim/main.go`. Define a `<Name>State` struct
   protected by a `sync.RWMutex`, with `snapshot()` returning a map
   for HTTP serialisation.
2. Implement HTTP handlers - `handleState`, `handleCommand`,
   `handleAudit`, `handleHealth`. Use `shared.AuditLog` to record
   every write with its source.
3. Add `services/<name>-sim/modbus.go` - copy the relay/recloser
   pattern. Map register addresses to state fields.
4. Add `services/<name>-sim/dnp3.go` - define
   `BinaryInputs` / `BinaryOutputs` / `AnalogInputs` and assign a
   DNP3 outstation address (next free integer in the table above).
5. Add a target in `services/Dockerfile`:
   ```Dockerfile
   FROM sim-base AS <name>-sim
   COPY --from=builder /bin/<name>-sim /usr/local/bin/<name>-sim
   EXPOSE 8080 502 20000
   CMD ["sh", "-c", "set-gateway.sh && <name>-sim"]
   ```
6. Add a service block in `docker-compose.yml` (and mirror in
   `docker-compose.release.yml`) with appropriate zone networks,
   healthcheck, and `depends_on`.
7. Add the node type to `backend/internal/labs/catalog.go` and any
   relevant lab-definition YAMLs.
8. Add a job entry to `.github/workflows/release.yml` matrix.

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
