# dnp3go

A zero-dependency Go implementation of the [DNP3](https://en.wikipedia.org/wiki/DNP3) protocol, hand-written for use in the RangerDanger ICS cyber range.

This is a **standalone Go module** (`github.com/tonylturner/dnp3go`) with its own `go.mod`. It is vendored inside the [RangerDanger](https://github.com/tonylturner/rangerdanger) repo for now and consumed via a `replace` directive in `services/go.mod`. Publishing it as a separate GitHub repo is a future option, not a current requirement.

## What it implements

- **CRC-16/DNP** — checksum verification on every link-layer frame
- **Data link framing** — fixed/variable user-data frames, sync bytes, length, control octets
- **Transport function** — fragment reassembly, sequence numbers
- **Application layer** — function codes, object headers, qualifiers, prefix codes, range fields
- **Outstation TCP server** — listens for masters, responds to reads and direct-operate commands
- **Master polling** — used by `services/rtac-sim` to poll field-device outstations

Supported function codes:

| FC | Direction | Purpose |
|----|-----------|---------|
| 1 (Read) | Master → Outstation | Class 0 / 1 / 2 / 3 polls |
| 3 (Select) | Master → Outstation | Pre-arm a control point |
| 4 (Operate) | Master → Outstation | Operate after select |
| 5 (Direct Operate) | Master → Outstation | Operate without select |

## CLI tools

The `cmd/` directory contains two operator-facing tools:

- `dnp3poll` — polls an outstation, prints decoded objects
- `dnp3cmd` — issues control commands (trip, close, set-tap, etc.)

These are built into the Kali (`Dockerfile.kali`) and engineering-workstation (`Dockerfile.eng-ws`) images and used in the lab exercises.

## Consumers in this repo

| Consumer | Role | Source |
|----------|------|--------|
| `services/relay-sim` | DNP3 outstation, address 1 | `services/relay-sim/dnp3.go` |
| `services/recloser-sim` | DNP3 outstation, address 2 | `services/recloser-sim/dnp3.go` |
| `services/regulator-sim` | DNP3 outstation, address 3 | `services/regulator-sim/dnp3.go` |
| `services/rtac-sim` | DNP3 master polling field devices + read-only outstation address 10 | `services/rtac-sim/dnp3.go`, `services/rtac-sim/dnp3_poll.go` |

All field outstations listen on TCP port 20000.

## Building and testing

```sh
cd dnp3go
go test ./...
go build ./cmd/dnp3poll
go build ./cmd/dnp3cmd
```

The Dockerfiles in this repo build the CLI tools fresh from this directory in their own multi-stage builders, so changes here flow through to the Kali and eng-ws images on next image build.

## License

Apache License 2.0 — same as RangerDanger.
