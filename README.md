# RangerDanger

**An OT/ICS cyber range for hands-on network segmentation training.**

[![CI](https://github.com/tonylturner/rangerdanger/actions/workflows/ci.yml/badge.svg)](https://github.com/tonylturner/rangerdanger/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/tonylturner/rangerdanger?include_prereleases&sort=semver)](https://github.com/tonylturner/rangerdanger/releases)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Go](https://img.shields.io/github/go-mod/go-version/tonylturner/rangerdanger?filename=backend%2Fgo.mod&logo=go&logoColor=white)](https://go.dev)
[![Next.js](https://img.shields.io/badge/Next.js-14-000000?logo=next.js&logoColor=white)](https://nextjs.org)
[![TypeScript](https://img.shields.io/badge/TypeScript-5-3178C6?logo=typescript&logoColor=white)](https://www.typescriptlang.org)
[![Docker](https://img.shields.io/badge/Docker-Compose-2496ED?logo=docker&logoColor=white)](https://docs.docker.com/compose)
[![containd](https://img.shields.io/badge/containd-NGFW-f97316)](https://github.com/tonylturner/containd)

RangerDanger spins up a realistic electric distribution substation inside Docker — complete with multi-zone networks, field devices speaking Modbus and DNP3, a multi-homed supervisory controller, and a real deep-packet-inspection firewall at the edge. Students work through guided exercises to identify required traffic flows, design segmentation rules, execute attacks against a weak baseline, and validate that a hardened policy prevents them.

> This project is for isolated lab environments only. Do not connect these containers or networks to production systems.

## What makes it different

Most OT training labs fall back to iptables and pretend. RangerDanger uses [containd](https://github.com/tonylturner/containd), a purpose-built NGFW with ICS deep-packet inspection, so students can see and control Modbus function codes, DNP3 Direct Operate commands, and traffic flows at a level that matches what modern OT-aware firewalls actually do.

| Typical OT lab | RangerDanger |
|----------------|--------------|
| Basic iptables or OPNsense | containd NGFW with ICS DPI (Modbus, DNP3, CIP) |
| Separate tools for capture, IDS, SCADA | Unified web UI with embedded DPI visibility |
| Static, manually-provisioned containers | Dynamic docker-compose orchestration per exercise |
| Notion workbooks or PDFs for exercises | Interactive exercise runner with inline terminals and auto-validation |
| Real VMs per lab seat | Lightweight containers — runs on a laptop |

## Scenario: Substation Segmentation Validation Lab

Students operate an electric cooperative distribution substation and must validate and improve the network segmentation protecting critical control functions.

### Network zones

All traffic between zones transits the containd firewall for inspection and policy enforcement.

| Zone | Subnet | Hosts |
|------|--------|-------|
| Enterprise | 10.10.10.0/24 | Corporate workstation, Kali attacker |
| Vendor / DMZ | 10.20.20.0/24 | Vendor jump box, engineering workstation |
| OT Operations | 10.30.30.0/24 | FUXA HMI, RTAC, OpenPLC, historian, GPS clock |
| Field Devices | 10.40.40.0/24 | Relay, recloser, regulator, capacitor bank |
| Physics | 10.50.50.0/24 | OpenDSS feeder simulation engine (not firewalled) |

### Exercises

Aligned to the DefendICS OT Network Segmentation workshop agenda.

| # | Lab | Exercise | Time | Focus |
|---|-----|----------|------|-------|
| 1 | 1.2 | Baseline Traffic Analysis | 30 min | Capture and analyze normal flows at the firewall |
| 2 | 1.3 | Segmentation Requirements & Rule Design | 20 min | Write requirements, compare to improved config |
| 3 | 1.4 | Remediation Planning Under Constraint | 25 min | Choose what to fix with a finite labor budget |
| 4 | 2.2 | Firewall Policy Implementation | 30 min | Build least-privilege containd policy from your plan |
| 5 | 2.3 | Modbus Register Override Attack | 15 min | Direct writes bypassing the RTAC |
| 6 | 2.3 | DNP3 Direct Operate Command Injection | 15 min | Disable auto-reclose from the enterprise zone |
| 7 | 2.3 | Vendor Remote Access Compromise (bonus) | 15 min | Pivot from vendor DMZ to field devices via Modbus |
| 8 | 2.3 | Capacitor Bank Switching Attack (bonus) | 15 min | Manipulate capbank switching via Modbus |
| 9 | 2.4 | Post-Change Validation & Evidence Collection | 20 min | Verify the hardened policy with PCAP evidence |

Each exercise includes inline terminal access to every relevant node, auto-run buttons for CLI commands, per-exercise notes, and backend validators that check substation state, capture files, and firewall policy.

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│  Browser (localhost:8088)                                    │
│  Next.js UI — exercise runner, network console, HMI         │
└──────────────────────────────────────────────────────────────┘
                         │
                         ▼
┌──────────────────────────────────────────────────────────────┐
│  Nginx reverse proxy — /apps/*, /containd/, /api/            │
└──────────────────────────────────────────────────────────────┘
                         │
          ┌──────────────┼──────────────┐
          ▼              ▼              ▼
    ┌──────────┐   ┌──────────┐   ┌──────────────┐
    │ Backend  │   │ Frontend │   │ Containd NGFW│
    │ Go+Gin   │   │ Next.js  │   │ (port 2222   │
    │ :8080    │   │ :3000    │   │  SSH, :8080  │
    └──────────┘   └──────────┘   │  web UI)     │
          │                       └──────────────┘
          │                              │
          │                    ┌─────────┼─────────┬─────────┐
          │                    ▼         ▼         ▼         ▼
          │              enterprise  vendor    ot_ops    field
          │                  net       net       net       net
          │                    │         │         │         │
          │                  kali    eng-ws     rtac      relay
          │                 corp-ws  vendor-   fuxa-     recloser
          │                           jump      hmi      regulator
          │                                   openplc    capbank
          │                                  historian
          │                                    gps
          └────── Docker SDK ──────────────────────┘
```

### Backend (Go + Gin)

Gin HTTP API at `:8080` managing lab state, terminal sessions, firewall policy, traffic generation, PCAP capture, and scenario validation. Uses GORM + SQLite for persistence and the Docker SDK for container orchestration. Integrates with containd over REST and SSE for events.

Key endpoints:

- `GET /api/scenarios` — list exercises
- `GET /api/scenarios/:id/validate` — run validators for an exercise
- `POST /api/traffic/generate` — start representative OT traffic generation
- `POST /api/pcap/start` — start a firewall packet capture
- `POST /api/workshop/nodes/:nodeId/exec` — run a command on a lab node
- `GET /api/workshop/nodes/:nodeId/terminal` — WebSocket terminal session
- `POST /api/workshop/reset` — reset lab state

### Frontend (Next.js + TypeScript)

Dark-themed, command-and-control-style UI with React Flow topology visualization, xterm.js terminals, and inline exercise runner. Built with Tailwind CSS, shadcn/ui primitives, lucide-react icons, and @radix-ui for tooltips and other interactive components.

Pages:

- `/` — Dashboard with node status and activity feed
- `/exercises` — Exercise library with completion tracking
- `/exercises/[id]` — Step-by-step exercise runner with inline terminals
- `/console` — Network Map with clickable topology and per-node terminals
- `/hmi` — SCADA one-line diagram with interactive device controls

### Simulators (Go)

Custom Go services implementing hand-written Modbus TCP, DNP3 TCP (via the [dnp3go](dnp3go/) standalone library — zero external dependencies, vendored in this repo as a separate Go module), and REST APIs for each field device. All expose the same shared state across protocols:

- **relay-sim** — Feeder breaker with trip, close, lockout, fault injection
- **recloser-sim** — Auto-reclose with shot counting and lockout
- **regulator-sim** — Load tap changer with ±16 tap range
- **rtac-sim** — Supervisory controller polling all field devices via Modbus, DNP3, and HTTP
- **opendss-sim** — Feeder physics engine calculating energization and voltage

### Firewall: containd

[containd](https://github.com/tonylturner/containd) is a separate project providing zone-based firewalling with nftables, ICS DPI (Modbus/TCP function code filtering, DNP3 protocol awareness), IT DPI (DNS, TLS/SNI, HTTP, SSH, RDP, SMB), a web UI, and SSH console access. RangerDanger consumes it as a published container image (`ghcr.io/tonylturner/containd:latest`) and configures it through JSON policy files.

Two firewall configurations ship with the lab:

- **substation-weak.json** — Intentionally permissive baseline that allows the attack exercises to succeed
- **substation-improved.json** — Hardened policy that blocks all attack paths while preserving legitimate flows

## Getting started

### Prerequisites

- Docker Desktop or Docker Engine with Compose v2
- 8 GB RAM minimum, 16 GB recommended
- 30 GB free disk
- Apple Silicon or x86_64 host
- Host ports `8088`, `9080`, `9443`, `2222` available

> ### ⚠️ Lab-only deployment
>
> RangerDanger is designed to run on a **single student's laptop**.
> All host-exposed ports (`8088`, `9080`, `9443`, `2222`) are bound
> to **loopback only** in `docker-compose.yml`, so the lab is not
> reachable from the network by default.
>
> - **No authentication** on backend / terminal / proxy. The lab is
>   safe under loopback binding; it is **not** safe if you change
>   the bindings to expose it.
> - **Default credentials are baked in** for convenience:
>   `containd / containd`, `openplc / openplc`,
>   `CONTAIND_JWT_SECRET=rangerdanger-dev`. They are not secrets and
>   must never be reused outside the lab.
> - **Self-signed TLS** on webtop and containd; browser warnings are
>   expected.
> - **Privileged firewall container** runs with `NET_ADMIN`,
>   `NET_RAW`, and `SYS_TIME` so containd can manage nftables and
>   capture traffic.
>
> **Need to reach the lab from another machine?** Use an SSH local
> forward (`ssh -L 8088:127.0.0.1:8088 you@lab-laptop`) or a mesh
> VPN like Tailscale. Do **not** change the compose port bindings to
> `0.0.0.0`. See [`SECURITY.md`](SECURITY.md#exposing-the-lab-beyond-localhost)
> for the full runbook on safe external access patterns.

### Quick start

The recommended path uses the `setup.sh` / `setup.ps1` installer, which runs prereq checks, pulls pre-built images from GHCR, and prints next steps.

**macOS / Linux:**

```bash
git clone https://github.com/tonylturner/rangerdanger
cd rangerdanger
./setup.sh                       # latest release
# or:  ./setup.sh --version v0.1.0
```

**Windows (PowerShell):**

```powershell
git clone https://github.com/tonylturner/rangerdanger
cd rangerdanger
.\setup.ps1                      # latest release
# or:  .\setup.ps1 -Version v0.1.0
```

**Build from source instead** (developers, contributors):

```bash
docker compose up -d --build     # builds all 14 first-party images locally
```

The first build takes several minutes while the Go simulators, Kali image, and engineering workstation are built. Subsequent runs reuse the layer cache.

**Offline / SSD install** (workshops where pulling 6+ GB per student over conference Wi-Fi is impractical):

```bash
./setup.sh --from-tarballs /Volumes/WORKSHOP_SSD
# (or: .\setup.ps1 -FromTarballs D:\WORKSHOP_SSD on Windows)
```

This expects `images-amd64.tar` / `images-arm64.tar` on the SSD; see `docs/release-plan.md` and the per-release tarballs attached to each GitHub release for staging instructions.

### Access points

| Service | URL | Credentials |
|---------|-----|-------------|
| RangerDanger UI | http://localhost:8088 | — |
| containd Web UI | http://localhost:9080 | — |
| containd SSH | `ssh -p 2222 containd@localhost` | containd / containd |
| FUXA HMI | http://localhost:8088/apps/fuxa-hmi/ | — |
| OpenPLC | http://localhost:8088/apps/openplc/ | — |

Open [http://localhost:8088/exercises](http://localhost:8088/exercises) and start with Exercise 0.

### Development

Rebuild a single service without tearing down the stack:

```bash
docker compose build backend && docker compose up -d backend
docker compose build frontend && docker compose up -d frontend
```

Run tests:

```bash
cd backend && go test ./...
cd frontend && npm run build
```

## Repository layout

```
backend/               Go + Gin API, orchestration, scenario validators
frontend/              Next.js + TypeScript + shadcn/ui dashboard
services/              Go simulators (relay, recloser, regulator, rtac, opendss)
dnp3go/                Standalone DNP3 library (separate Go module)
lab-definitions/
  substation-segmentation.yml    Topology definition
  scenarios/                     Exercise YAML files
  firewall/                      Weak and improved containd configs
proxy/                 Nginx reverse proxy config
scripts/               Dev helper scripts
docs/                  Architecture and API documentation
```

## Current status

The Substation Segmentation scenario is fully implemented and all nine exercises are playable end-to-end. Recent work:

- **containd v0.1.18 Linux shell mode** — tcpdump, bash, and standard Linux tooling available directly in the firewall SSH console
- **Traffic generator overhaul** — produces realistic cross-zone Modbus, DNP3, HTTP, and NTP traffic; Kali attack traffic removed from baseline generation
- **Exercise runner UX** — inline command blocks with Run/Copy buttons, markdown links between pages, resizable terminal panel, per-exercise shared notes, icon buttons with styled tooltips
- **PTY resize propagation** — switching between node tabs now properly resizes the remote shell via Docker exec resize and SSH window-change
- **Validator improvements** — exercise 0 now detects manually-created tcpdump captures alongside API-initiated ones

### Known gaps

- FUXA HMI screens are unconfigured (blank canvas)
- Multi-user RBAC (instructor vs student modes) not implemented
- Suricata IDS alerts not yet integrated into activity feed
- PCAP replay and per-scenario traffic recording deferred

## Documentation

- [`docs/architecture.md`](docs/architecture.md) — Full zone model, node inventory, service breakdown, and data flow
- [`docs/api-spec.md`](docs/api-spec.md) — REST API reference including workshop endpoints, scenario validators, PCAP management, and WebSocket terminal protocol
- [`docs/workshop-overview.md`](docs/workshop-overview.md) — Visitor-facing summary of the lab — exercises, what's simulated, the weak-baseline → hardened-target arc
- [`docs/security-known-issues.md`](docs/security-known-issues.md) — Triaged govulncheck/Trivy findings with rationale

## Related projects

- [containd](https://github.com/tonylturner/containd) — The ICS-aware NGFW at the heart of the lab
- [`dnp3go/`](dnp3go/) — Zero-dependency Go DNP3 library used by the field device simulators (standalone module vendored in this repo)

## License

Apache License 2.0. See [LICENSE](LICENSE).
