# Architecture Overview

RangerDanger is a containerized substation cyber range. It runs entirely inside Docker — five virtual networks representing zones of an electric distribution substation, a deep-packet-inspection firewall at the center, custom Go simulators for field devices, and a Next.js web application for orchestration and exercise delivery.

## Network topology

The substation is modeled as four firewalled zones plus one non-firewalled physics network. The containd NGFW is multi-homed across all four zones and acts as the default gateway for each, forcing all inter-zone traffic through deep-packet inspection and policy enforcement.

| Zone | Subnet | containd iface | Purpose |
|------|--------|----------------|---------|
| Enterprise (`enterprise_net`) | 10.10.10.0/24 | eth0 (wan) | Corporate IT, attacker workstations |
| Vendor / DMZ (`vendor_net`) | 10.20.20.0/24 | eth1 (dmz) | Vendor remote access, engineering workstation |
| OT Operations (`ot_ops_net`) | 10.30.30.0/24 | eth2 (lan1) | HMI, RTAC, OpenPLC, historian, GPS |
| Field Devices (`field_net`) | 10.40.40.0/24 | eth3 (lan2) | Relay, recloser, regulator, capacitor bank |
| Physics (`physics_net`) | 10.50.50.0/24 | not firewalled | OpenDSS feeder simulation |

The RTAC is intentionally multi-homed on OT Operations, Field, and Physics networks. Its Modbus/DNP3 polling of field devices happens directly on `field_net` and does **not** transit the firewall. This is a realistic substation architecture detail students must understand when designing segmentation rules.

## Node inventory

| Node | Container | Zone(s) | IP | Role |
|------|-----------|---------|-----|------|
| Corporate workstation | `corp_ws` | enterprise | 10.10.10.10 | Generic IT workstation |
| Kali attacker | `kali` | enterprise | 10.10.10.50 | Offense tooling: nmap, mbpoll, dnp3poll, dnp3cmd, pymodbus |
| Vendor jump box | `vendor_jump` | vendor | 10.20.20.10 | Vendor remote access |
| Engineering workstation | `eng_workstation` | vendor | 10.20.20.20 | Ubuntu MATE desktop with Wireshark, tshark, mbpoll, dnp3 tools |
| FUXA HMI | `fuxa_hmi` | ot_ops | 10.30.30.10 | Operator HMI (SCADA) |
| RTAC | `rtac_sim` | ot_ops + field + physics | 10.30.30.20 / 10.40.40.10 / 10.50.50.10 | Supervisory controller / protocol broker |
| OpenPLC | `openplc` | ot_ops + field | 10.30.30.30 / 10.40.40.30 | Substation automation PLC |
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

The platform services (backend, frontend, proxy) live on `mgmt_net` (10.99.99.0/24) only. User-facing lab nodes that expose UIs (corp_ws, vendor_jump, eng_workstation, fuxa_hmi, openplc) get a `mgmt_net` leg in addition to their lab zone so the proxy can reach them; the firewall (containd) sits on every zone.

## Services

### Backend (Go + Gin)

The backend at `backend/cmd/server` exposes a Gin HTTP API, manages container orchestration through the Docker SDK, persists lab state in SQLite via GORM, and proxies to the containd firewall over REST and SSE.

Internal packages:

- `internal/config` — Viper-based configuration
- `internal/db` — GORM + SQLite setup
- `internal/models` — LabTemplate, LabInstance, NodeDefinition, Scenario, ScenarioRun, TelemetryPoint
- `internal/labs` — YAML definition loader and node type catalog
- `internal/orchestrator` — Docker SDK wrapper (container lifecycle, exec sessions, exec resize)
- `internal/containd` — REST client for containd firewall API
- `internal/server` — HTTP handlers, WebSocket terminals, exercise validators, PCAP management, traffic generation

### Frontend (Next.js + TypeScript)

Next.js 14 app at `frontend/app/` with dark-themed shadcn-style UI.

Top-level pages:

- `/` — Dashboard
- `/exercises` — Exercise library with completion tracking
- `/exercises/[id]` — Step-by-step exercise runner with inline terminals, command blocks, notes
- `/console` — Network Map with React Flow topology and per-node terminals
- `/hmi` — SCADA one-line diagram with click-to-control device popups
- `/substation` — Alternate substation control panel
- `/labs` / `/labs/[id]` — Lab template and instance management
- `/topology` — Topology builder

Key components:

- `terminal-context.tsx` / `terminal-inner.tsx` — Shared xterm.js terminal manager with PTY resize propagation
- `scenario-runner.tsx` — Exercise runner with inline command blocks, shared notes, styled tooltips
- `network-console.tsx` — Cobalt Strike-style network map
- `advanced-hmi.tsx` — SVG substation one-line with animated power flow
- `ui/tooltip.tsx` / `ui/button.tsx` — shadcn primitives

### Field device simulators (Go)

Custom services under `services/` sharing a common pattern: single in-memory state exposed over three protocols simultaneously.

- **HTTP REST** on port 8080 — `GET /api/state`, `POST /api/command`, `GET /api/audit`, `GET /api/health`
- **Modbus TCP** on port 502 — hand-written outstation supporting FC1/2/3/4 read and FC5/6 write
- **DNP3 TCP** on port 20000 — outstation via the [dnp3go](https://github.com/tonylturner/dnp3go) library (standalone Go module at the repo root), supporting Read (FC01), Direct Operate (FC05), Select/Operate (FC03/04)

Simulators:

- `relay-sim` — Feeder breaker with trip/close, lockout, fault injection
- `recloser-sim` — Auto-reclose with shot counting, lockout, disable-reclose
- `regulator-sim` — Load tap changer with ±16 tap range
- `rtac-sim` — Supervisory controller aggregating all field devices; runs autonomous DNP3 master polling every 5s and HTTP REST polling every 1s
- `opendss-sim` — Simplified feeder model calculating load energization and voltage

DNP3 outstation addresses: relay=1, recloser=2, regulator=3, rtac=10 (read-only).

### Containd NGFW

[containd](https://github.com/tonylturner/containd) is pulled as a container image (`ghcr.io/tonylturner/containd:latest`) and configured via JSON policy files in `lab-definitions/firewall/`. RangerDanger doesn't modify containd source — only configuration.

Capabilities used:

- Zone-based firewall with nftables backend
- ICS DPI — Modbus function code filtering, DNP3 protocol awareness
- IT DPI — DNS, TLS/SNI, HTTP, SSH, RDP, SMB visibility
- Web UI on :8080, SSH console on :2222
- REST API at `/api/v1/*` for policy, status, events, PCAP management
- SSE events stream
- Linux shell mode (`CONTAIND_SSH_SHELL_MODE=linux`) giving students a real bash shell with `tcpdump` and standard Linux tools

Two firewall configs ship with the lab:

- **substation-weak.json** — Intentionally permissive baseline; enterprise and vendor zones have broad access to OT and field devices on Modbus/DNP3/HTTP. Enables attack success in exercises 2-4.
- **substation-improved.json** — Hardened policy. Enterprise denied all ICS access. Vendor restricted to SSH/HTTPS only. Only the RTAC (pinned to 10.40.40.10) can reach field devices on Modbus/DNP3/HTTP. ICS DPI rules limit Modbus to function codes 1-6. GPS time sync pinned to 10.30.30.50.

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

### Multi-homed RTAC

The RTAC sits on three Docker networks (ot_ops, field, physics) directly. This models a real substation RTAC that has a WAN interface back to the control center and a local process interface to field devices. The consequence for segmentation: **the firewall cannot see RTAC → field device traffic** because it stays on `field_net`. Students must understand this when writing segmentation requirements — the firewall protects against lateral access, not against a compromised RTAC.

### Traffic generator

`POST /api/traffic/generate` spawns containerized `docker exec` commands that send realistic OT traffic from every relevant source — Modbus reads from RTAC to field devices, Modbus reads from HMI and historian to RTAC, NTP from GPS to field devices, HTTP from eng-ws to RTAC and OpenPLC, and so on. This produces the baseline traffic that exercise 0 captures and analyzes. Attack traffic is deliberately excluded from the generator — only normal operational flows.

### PCAP capture

Two paths: containd's native PCAP API (`POST /api/pcap/start` in the backend proxies to containd) or direct `tcpdump` from a student SSH session on the firewall. Exercise 0 uses the direct `tcpdump` path so students see the tool in action. The exercise validator checks for the capture file via Docker exec into the firewall container, covering both paths.

### Shared terminal state

The frontend uses a React context (`TerminalProvider`) plus a hidden-mount pattern in `SharedTerminalPanel` — all exercise node terminals are mounted simultaneously and toggled with `display:none/block`. When switching tabs, xterm's size can get confused, so `terminal-inner.tsx` uses an `IntersectionObserver` to detect visibility changes and re-fit, propagating the new size to the server via a JSON resize message (`{"type":"resize","cols":N,"rows":N}`). The backend maps this to `ContainerExecResize` (Docker exec) or `session.WindowChange` (SSH), keeping the remote bash's `stty size` in sync.

## Environment variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `OTLAB_HTTP_PORT` | 8080 | Backend listen port |
| `OTLAB_DB_PATH` | /data/rangerdanger.db | SQLite path |
| `OTLAB_LAB_DEFINITIONS_PATH` | lab-definitions | YAML source directory |
| `CONTAIND_API_URL` | http://firewall:8080 | containd REST endpoint |
| `CONTAIND_JWT_SECRET` | rangerdanger-dev | containd auth shared secret |
| `CONTAIND_SSH_SHELL_MODE` | linux | Sets containd SSH to drop into bash |
| `CONTAIND_ALLOWED_ORIGINS` | http://localhost:8088 | CORS + iframe-embedding origins |

## Security notes

Lab networks are internal-only by default. Only the nginx proxy, containd web UI, and containd SSH bind to host ports. Containers run with minimal privileges except where simulators require `NET_ADMIN` for gateway manipulation. Default credentials are for lab use only and must not be exposed to production systems or untrusted networks.
