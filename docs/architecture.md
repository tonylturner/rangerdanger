# Architecture Overview

RangerDanger is a containerized substation cyber range. It runs entirely inside Docker - five virtual networks representing zones of an electric distribution substation, a deep-packet-inspection firewall at the center, custom Go simulators for field devices, and a Next.js web application for orchestration and exercise delivery.

## Network topology

The substation is modeled as four firewalled zones plus one non-firewalled physics network and one out-of-band management network. The containd NGFW is multi-homed across all four firewalled zones and acts as the default gateway for each, forcing all inter-zone traffic through deep-packet inspection and policy enforcement.

| Zone | Subnet | containd zone | Purpose |
|------|--------|---------------|---------|
| Enterprise (`enterprise_net`) | 10.10.10.0/24 | wan | Corporate IT, attacker workstations |
| Vendor / DMZ (`vendor_net`) | 10.20.20.0/24 | dmz | Vendor remote access, engineering workstation |
| OT Operations (`ot_ops_net`) | 10.30.30.0/24 | lan1 | HMI, RTAC, OpenPLC, historian, GPS |
| Field Devices (`field_net`) | 10.40.40.0/24 | lan2 | Relay, recloser, regulator, capacitor bank |
| Management (`mgmt_net`) | 10.99.99.0/24 | lan3 | Backend ↔ firewall control plane (out of band) |
| Physics (`physics_net`) | 10.50.50.0/24 | not firewalled | OpenDSS feeder simulation |

Zone names (`wan`/`dmz`/`lan1`/`lan2`/`lan3`) are containd's logical interface names. They auto-bind to whatever `ethN` Docker assigns at boot via the `CONTAIND_AUTO_*_SUBNET` env in `docker-compose.yml`. **`ethN` ordering is not stable across hosts** - Docker assigns `ethN` by alphabetical network name, not by compose declaration order - so policy and tooling reference zone names rather than `eth` indices. The `eth0`/`eth1`/`...` mapping is a runtime artifact of the host you're on; never rely on it.

The RTAC is intentionally multi-homed on **four** networks: OT Operations (`10.30.30.20`), Field (`10.40.40.10`), Physics (`10.50.50.10`, so it can poll the OpenDSS model), and Management (`10.99.99.20`, so the backend can reach `http://rtac-sim:8080`). However, `scripts/rtac-harden.sh` actively prevents the RTAC from acting as a firewall bypass by replacing the directly-connected route to `field_net` with an indirect route via the OT-Ops firewall, disabling IP forwarding, dropping the FORWARD chain, and disabling proxy ARP. The result: **all RTAC → field traffic transits the containd firewall and is visible to its capture/policy pipeline**, with the wire-visible source IP being `10.30.30.20` (the RTAC's OT-Ops leg). The field leg exists for realism - many production RTACs have one - but is intentionally inert as a routed source.

This compensating control is why the hardened `substation-improved.json` source-pins RTAC as `10.30.30.20` (the OT-Ops IP), not `10.40.40.10` - the OT-Ops side is what the firewall sees. Students reasoning about segmentation should treat the RTAC like any other multi-homed control system: the firewall protects against lateral access *and* against an RTAC originating field traffic from the wrong leg, because the kernel-level routing pin forces every flow through the policy plane.

## Node inventory

| Node | Container | Zone(s) | IP | Role |
|------|-----------|---------|-----|------|
| Corporate workstation | `corp_ws` | enterprise | 10.10.10.10 | Generic IT workstation |
| Kali attacker | `kali` | enterprise | 10.10.10.50 | Offense tooling: nmap, mbpoll, dnp3poll, dnp3cmd, pymodbus |
| Vendor jump box | `vendor_jump` | vendor | 10.20.20.10 | Vendor remote access |
| Engineering workstation | `eng_workstation` | vendor | 10.20.20.20 | Ubuntu MATE desktop with Wireshark, tshark, mbpoll, dnp3 tools |
| FUXA HMI | `fuxa_hmi` | ot_ops | 10.30.30.10 | Operator HMI (SCADA) |
| RTAC | `rtac_sim` | ot_ops + field + physics + mgmt | 10.30.30.20 (also 10.40.40.10 on field_net pinned via the OT-Ops firewall, 10.50.50.10 on physics_net for OpenDSS polling, 10.99.99.20 on mgmt_net so the backend can reach it) | Supervisory controller / protocol broker |
| OpenPLC | `openplc` | ot_ops | 10.30.30.30 | Substation automation PLC (Modbus client of the RTAC) |
| Historian | `historian_sim` | ot_ops | 10.30.30.40 | Data historian polling the RTAC |
| GPS clock | `gps_sim` | ot_ops | 10.30.30.50 | Time source for SOE timestamping |
| Relay | `relay_sim` | field | 10.40.40.20 | Feeder breaker with trip/close/lockout |
| Recloser | `recloser_sim` | field | 10.40.40.21 | Auto-reclose with shot counting |
| Regulator | `regulator_sim` | field | 10.40.40.22 | Load tap changer (±16 positions) |
| Capacitor bank | `capbank_sim` | field | 10.40.40.23 | Var support |
| OpenDSS | `opendss_sim` | physics | 10.50.50.20 | Feeder physics engine |
| Firewall | `firewall` | all (incl. mgmt) | .2 on each zone | containd NGFW |
| Backend | `backend` | mgmt | 10.99.99.10 | RangerDanger API |
| Frontend | `frontend` | mgmt | 10.99.99.11 | Next.js UI |
| Proxy | `proxy` | mgmt | 10.99.99.3 | Nginx reverse proxy to all node UIs |

The platform services (backend, frontend, proxy) live on `mgmt_net` (10.99.99.0/24) only. User-facing lab nodes that expose UIs (corp_ws, vendor_jump, eng_workstation, fuxa_hmi, openplc) get a `mgmt_net` leg in addition to their lab zone so the proxy can reach them; `rtac_sim` likewise gets a mgmt leg (10.99.99.20) so the backend can reach `http://rtac-sim:8080`. The firewall (containd) sits on every zone.

## Services

### Backend (Go + Gin)

The backend at `backend/cmd/server` exposes a Gin HTTP API, manages container orchestration through the Docker SDK, persists lab state in SQLite via GORM, and proxies to the containd firewall over REST and SSE.

Internal packages:

- `internal/config` - Viper-based configuration
- `internal/db` - GORM + SQLite setup
- `internal/models` - LabTemplate, LabInstance, NodeDefinition, Scenario, ScenarioRun, TelemetryPoint
- `internal/labs` - YAML definition loader and node type catalog
- `internal/orchestrator` - Docker SDK wrapper (container lifecycle, exec sessions, exec resize)
- `internal/containd` - REST client for containd firewall API
- `internal/server` - HTTP handlers, WebSocket terminals, exercise validators, PCAP management, traffic generation

### Frontend (Next.js + TypeScript)

Next.js 14 app at `frontend/app/` with dark-themed shadcn-style UI.

Top-level pages:

- `/` - Dashboard
- `/exercises` - Exercise library with completion tracking
- `/exercises/[id]` - Step-by-step exercise runner with inline terminals, command blocks, notes
- `/console` - Network Map with React Flow topology and per-node terminals
- `/substation` - Substation control panel (live device state, control popups)
- `/knowledge` - Reference material accompanying the exercises
- `/labs` / `/labs/[id]` - Lab template and instance management
- `/scenarios` - Legacy redirect to `/exercises`

The operator HMI itself is **FUXA**, served via the nginx proxy at
`/apps/fuxa-hmi/` rather than as a route inside the Next.js app.

Key components:

- `terminal-context.tsx` / `terminal-inner.tsx` - Shared xterm.js terminal manager with PTY resize propagation
- `scenario-runner.tsx` - Exercise runner with inline command blocks, shared notes, styled tooltips
- `network-console.tsx` - Cobalt Strike-style network map
- `substation-panel.tsx` - Live substation device state with command popups
- `ui/tooltip.tsx` / `ui/button.tsx` - shadcn primitives

### Field device simulators (Go)

Most simulators under `services/` are Go binaries built from a shared multi-stage `services/Dockerfile` and share a common pattern: single in-memory state exposed over three protocols simultaneously.

- **HTTP REST** on port 8080 - `GET /api/state`, `POST /api/command`, `GET /api/audit`, `GET /api/health`
- **Modbus TCP** on port 502 - hand-written outstation supporting FC1/3/4 read and FC5/6 write
- **DNP3 TCP** on port 20000 - outstation via the [dnp3go](https://github.com/tonylturner/dnp3go) library (standalone Go module at the repo root), supporting Read (FC01), Direct Operate (FC05), Select/Operate (FC03/04)

Field-device simulators:

- `relay-sim` - Feeder breaker with trip/close, lockout, fault injection (DNP3 + Modbus + HTTP)
- `recloser-sim` - Auto-reclose with shot counting, lockout, disable-reclose (DNP3 + Modbus + HTTP)
- `regulator-sim` - Load tap changer with ±16 tap range (DNP3 + Modbus + HTTP)
- `capbank-sim` - Switched capacitor bank with switch-count contact wear (Modbus + HTTP; no DNP3 outstation)
- `historian-sim` - Data historian polling the RTAC (Modbus + HTTP)
- `gps-sim` - GPS / NTP time source (NTP + HTTP)
- `rtac-sim` - Supervisory controller aggregating all field devices; runs autonomous DNP3 master polling every 5s and HTTP REST polling every 1s (read-only DNP3 outstation, Modbus, HTTP)

Plus one Python sim:

- `opendss-sim` - Simplified feeder physics engine. **Python / FastAPI**, served HTTP-only on port 8080 from its own `services/opendss-sim/Dockerfile` (python:3.12-slim). Not part of the Go multi-stage build.

DNP3 outstation addresses (the four that expose DNP3): relay=1, recloser=2, regulator=3, rtac=10 (read-only).

### Containd NGFW

[containd](https://github.com/tonylturner/containd) is pulled as a container image (`ghcr.io/tonylturner/containd:latest`) and configured via JSON policy files in `lab-definitions/firewall/`. RangerDanger doesn't modify containd source - only configuration.

Capabilities used:

- Zone-based firewall with nftables backend
- ICS DPI - Modbus function code filtering, DNP3 protocol awareness
- IT DPI - DNS, TLS/SNI, HTTP, SSH, RDP, SMB visibility
- Web UI on :8080, SSH console on :2222
- REST API at `/api/v1/*` for policy, status, events, PCAP management
- SSE events stream
- Linux shell mode (`CONTAIND_SSH_SHELL_MODE=linux`) giving students a real bash shell with `tcpdump` and standard Linux tools

Two firewall configs ship with the lab:

- **substation-weak.json** - Intentionally permissive baseline; enterprise and vendor zones have broad access to OT and field devices on Modbus/DNP3/HTTP. Enables attack success in exercises 2-4.
- **substation-improved.json** - Hardened policy. Enterprise denied all ICS access. Vendor restricted to SSH/HTTPS only. Only the RTAC (source-pinned to `10.30.30.20`, the OT-Ops leg the firewall actually sees) can reach field devices on Modbus/DNP3/HTTP. ICS DPI rules limit Modbus to function codes 1-6. GPS time sync pinned to `10.30.30.50`.

## Data flow

```
Browser ──HTTP──> Nginx proxy ──┬──> /apps/*      → webtop/HMI/OpenPLC containers
                                ├──> /containd/*  → containd web UI (proxied, iframe-safe)
                                ├──> /api/*       → backend
                                └──> /            → frontend (Next.js)

Backend ──Docker SDK──> container lifecycle + exec sessions
        ──REST──────> containd /api/v1/* (firewall policy, PCAP, events)
        ──WebSocket─> xterm.js clients (Docker exec or SSH, with resize forwarding)
        ──GORM──────> SQLite at /data/rangerdanger.db

Exercise runner (frontend) ──/api/scenarios/:id/validate──> backend validator
                          ──/api/workshop/nodes/:id/exec──> backend → Docker exec
                          ──/api/traffic/generate────────> backend → docker exec commands
                          ──/api/pcap/start──────────────> backend → containd PCAP API
```

## Key architectural decisions

### Multi-homed RTAC with kernel-pinned routing

The RTAC sits on two Docker networks (`ot_ops_net` + `field_net`) directly. This models a real substation RTAC that has a control-center-facing leg and a process-network leg. Without compensation, the kernel would forward field-bound traffic out the directly-connected `field_net` interface and bypass the firewall - exactly the "compromised RTAC bridges zones" failure mode. **`scripts/rtac-harden.sh` (run before the rtac-sim binary starts) prevents that bypass:**

1. `net.ipv4.ip_forward = 0` and per-interface forwarding disabled.
2. Proxy ARP and ICMP redirects disabled, strict reverse-path filter on.
3. `iptables -P FORWARD DROP` on the RTAC.
4. The directly-connected route for `10.40.40.0/24` is **replaced** with a route via the OT-Ops firewall (`10.30.30.2`), so all RTAC→field traffic transits containd.
5. Inbound L3 traffic on the field interface is dropped (the leg still ARPs but rejects new connections).

Outcome: the firewall sees every RTAC→field flow with source IP `10.30.30.20` (the OT-Ops leg). The hardened policy (`substation-improved.json`) source-pins exactly that IP, so a compromised non-RTAC OT host cannot impersonate the RTAC, and the RTAC itself cannot smuggle traffic via its field leg. Students designing segmentation should treat this kernel pin as a compensating control rather than a property of the firewall - the firewall *enforces* the pin, the kernel *creates* it.

`scripts/rtac-route-monitor.sh` re-applies the route replacement if Docker reconciliation drops it (e.g., after a network reattach), so the pin stays durable across container lifecycle events.

### Traffic generator

`POST /api/traffic/generate` spawns containerized `docker exec` commands that send realistic OT traffic from every relevant source - Modbus reads from RTAC to field devices, Modbus reads from HMI and historian to RTAC, NTP from GPS to field devices, HTTP from eng-ws to RTAC and OpenPLC, and so on. This produces the baseline traffic that exercise 0 captures and analyzes. Attack traffic is deliberately excluded from the generator - only normal operational flows.

### PCAP capture

Two paths: containd's native PCAP API (`POST /api/pcap/start` in the backend proxies to containd) or direct `tcpdump` from the firewall terminal. Lab 1.2 (Baseline Traffic Analysis) uses the direct `tcpdump` path so students see the tool in action. The validator checks for the capture file via Docker exec into the firewall container, covering both paths.

### Shared terminal state

The frontend uses a React context (`TerminalProvider`) plus a hidden-mount pattern in `SharedTerminalPanel` - all exercise node terminals are mounted simultaneously and toggled with `display:none/block`. When switching tabs, xterm's size can get confused, so `terminal-inner.tsx` uses an `IntersectionObserver` to detect visibility changes and re-fit, propagating the new size to the server via a JSON resize message (`{"type":"resize","cols":N,"rows":N}`). The backend maps this to `ContainerExecResize` on the Docker exec session, keeping the remote bash's `stty size` in sync. (SSH-based terminals were removed; every terminal - including the firewall - goes through Docker exec.)

## Environment variables

Backend-read variables (`backend/internal/config`):

| Variable | Default | Purpose |
|----------|---------|---------|
| `RANGERDANGER_HTTP_PORT` | 8080 | Backend listen port |
| `RANGERDANGER_DB_PATH` | `backend/data/rangerdanger.db` (the in-container deployment overrides this to `/data/rangerdanger.db` via `docker-compose.yml`) | SQLite path |
| `RANGERDANGER_LAB_DEFINITIONS_PATH` | `lab-definitions` | YAML source directory |
| `RANGERDANGER_CONTAIND_API_URL` | `http://firewall:8080` | containd HTTP base URL |
| `RANGERDANGER_CONTAIND_CONFIG_PATH` | `lab-definitions/firewall/substation-weak.json` | Initial firewall config |
| `CONTAIND_JWT_SECRET` | `rangerdanger-dev` | Shared secret the backend uses to mint JWTs for containd's REST API |

The legacy `OTLAB_*` prefix is honored as a deprecated alias - existing deployments continue to work but emit a warning at startup.

Variables read by the **containd container itself** (not the backend) and set in `docker-compose.yml`:

| Variable | Value | Purpose |
|----------|-------|---------|
| `CONTAIND_SSH_SHELL_MODE` | `linux` | Drops the containd SSH user into bash instead of the appliance CLI |
| `CONTAIND_ALLOWED_ORIGINS` | `http://localhost:8088` | CORS + iframe-embedding origins for the containd web UI |
| `CONTAIND_LAB_MODE` | `1` | Lab-mode lock: auto-restores the default `containd` / `containd` admin on boot |

## Security notes

Lab networks are internal-only by default. Only the nginx proxy, containd web UI, and containd SSH bind to host ports. Containers run with minimal privileges except where simulators require `NET_ADMIN` for gateway manipulation. Default credentials are for lab use only and must not be exposed to production systems or untrusted networks.
