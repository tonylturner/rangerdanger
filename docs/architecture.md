# Architecture Overview

RangerDanger is a containerized substation cyber range. It runs entirely inside Docker - a package-specific teaching topology plus management, physics and isolated desktop delivery bridges, a deep-packet-inspection firewall at the center, custom Go simulators for field devices, and a Next.js web application for orchestration and exercise delivery.

## Network topology

The US substation is modeled as four firewalled teaching zones plus one non-firewalled physics network and one out-of-band management network. The containd NGFW is multi-homed across the teaching zones and acts as their default gateway, forcing teaching inter-zone traffic through deep-packet inspection and policy enforcement. The inventory below describes US; EU uses station/process instead of OT-Ops/physics and adds Control Centre and Operations DMZ teaching zones. Each package's manifest defines its full attachment set. Desktop delivery bridges, described below, are outside the teaching topology.

| Zone | Subnet | containd zone | Purpose |
|------|--------|---------------|---------|
| Enterprise (`enterprise_net`) | 10.10.10.0/24 | wan | Corporate IT, attacker workstations |
| Vendor / DMZ (`vendor_net`) | 10.20.20.0/24 | dmz | Vendor remote access, engineering workstation |
| OT Operations (`ot_ops_net`) | 10.30.30.0/24 | lan1 | HMI, RTAC, OpenPLC, historian, GPS |
| Field Devices (`field_net`) | 10.40.40.0/24 | lan2 | Relay, recloser, regulator, capacitor bank |
| Management (`mgmt_net`) | 10.99.99.0/24 | lan3 | Backend ↔ firewall control plane (out of band) |
| Physics (`physics_net`) | 10.50.50.0/24 | not firewalled | OpenDSS feeder simulation |

Zone names (`wan`/`dmz`/`lan1`/`lan2`/`lan3`) are containd's logical interface names, and the firewall's kernel interfaces carry the same names. Each firewall network attachment in the range package's Compose files sets `interface_name` (`enterprise_net` → `wan`, `vendor_net` → `dmz`, `ot_ops_net` → `lan1`, `field_net` → `lan2`, `mgmt_net` → `lan3`; the EU package adds `lan4` and `lan5`), and package lint checks those names against the topology zones and the package policies. containd binds each zone through the `CONTAIND_AUTO_*_SUBNET` env, so the policy never names a device. Without the pin, Docker names the NICs `eth0`, `eth1`, ... in an order that differs between hosts; with it, `tcpdump -i lan2` on the firewall captures the field zone on every host. Compose supports `interface_name` from 2.36.0 on Engine 28.1 (API 1.49), which is why `setup.sh` refuses older Docker.

The RTAC is intentionally multi-homed on **four** networks: OT Operations (`10.30.30.20`), Field (`10.40.40.10`), Physics (`10.50.50.10`, so it can poll the OpenDSS model), and Management (`10.99.99.20`, so the backend can reach `http://rtac-sim:8080`). However, `scripts/rtac-harden.sh` actively prevents the RTAC from acting as a firewall bypass by replacing the directly-connected route to `field_net` with an indirect route via the OT-Ops firewall, disabling IP forwarding, dropping the FORWARD chain, and disabling proxy ARP. The result: **all RTAC → field traffic transits the containd firewall and is visible to its capture/policy pipeline**, with the wire-visible source IP being `10.30.30.20` (the RTAC's OT-Ops leg). The field leg exists for realism - many production RTACs have one - but is intentionally inert as a routed source.

This compensating control is why the hardened `substation-improved.json` source-pins RTAC as `10.30.30.20` (the OT-Ops IP), not `10.40.40.10` - the OT-Ops side is what the firewall sees. Students reasoning about segmentation should treat the RTAC like any other multi-homed control system: the firewall protects against lateral access *and* against an RTAC originating field traffic from the wrong leg, because the kernel-level routing pin forces every flow through the policy plane.

## Node inventory

| Node | Container | Zone(s) | IP | Role |
|------|-----------|---------|-----|------|
| Corporate workstation | `corp_ws` | enterprise + desk_corp | 10.10.10.10 | Generic IT workstation |
| Kali attacker | `kali` | enterprise | 10.10.10.50 | Offense tooling: nmap, mbpoll, dnp3poll, dnp3cmd, pymodbus |
| Vendor jump box | `vendor_jump` | vendor + desk_vendor | 10.20.20.10 | Vendor remote access |
| Engineering workstation | `eng_workstation` | vendor + desk_eng | 10.20.20.20 | Ubuntu MATE desktop with Wireshark, tshark, mbpoll, dnp3 tools |
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
| Proxy | `proxy` | mgmt + all delivery bridges | 10.99.99.3 | Nginx reverse proxy to all node UIs |

Backend and frontend attach only to `mgmt_net` (10.99.99.0/24).
The proxy retains its management leg and also attaches to three internal,
platform-owned delivery bridges. Each desktop keeps its teaching-zone leg
and replaces its management leg with exactly one delivery leg:

| Compose key / Engine name | CIDR | Gateway | Proxy | Desktop (US / EU) |
|---|---|---|---|---|
| `desk_corp_net` / `rangerdanger_desk_corp` | 10.98.1.0/28 | 10.98.1.1 | 10.98.1.2 | 10.98.1.3: corp_ws / unused |
| `desk_vendor_net` / `rangerdanger_desk_vendor` | 10.98.2.0/28 | 10.98.2.1 | 10.98.2.2 | 10.98.2.3: vendor_jump / vendor_jump |
| `desk_eng_net` / `rangerdanger_desk_eng` | 10.98.3.0/28 | 10.98.3.1 | 10.98.3.2 | 10.98.3.3: eng_workstation / eng_workstation |

Delivery bridges have no containd zone, no backend endpoint and no host
egress (`internal: true`). `set-gateway.sh` still installs the desktop's
zone-firewall default route; the connected delivery route carries replies
to the proxy. nginx resolves the unchanged desktop service names over
these bridges and forwards HTTP/WebSocket connections as before. Its
persistent server rejects socket peers in `10.98.0.0/16` with 403 before
location selection (including redirects); forwarding headers cannot grant
access. A desktop may answer the proxy, not call through it to management.

FUXA and OpenPLC retain their management legs for UI delivery. The RTAC,
containd, simulator, solver, historian and GPS management attachments are
unchanged. Kali remains enterprise-only. Removing a desktop's management
NIC does not by itself prove a deny: traffic can still follow its zone
firewall default, so acceptance probes must cover management destinations
under both weak and improved policies as well as permitted teaching flows.

### Platform and range

The lab runs as two Compose projects:

| Project | Services | Compose files | Started by |
|---|---|---|---|
| `rangerdanger-platform` | backend, frontend, proxy | `docker-compose.yml` (source) or `docker-compose.release.yml` (release) | `setup.sh` / `scripts/dev-up.sh` on the host; never stopped by a range switch |
| `rangerdanger` (the range) | everything else in the tables above | `lab-definitions/packages/<id>/compose.{source,release}.yml` | the backend only, through `POST /api/range` |

The platform owns `mgmt_net` (Engine name `rangerdanger_mgmt_net`) and all three delivery bridges; range files declare the platform networks they use as external. Teaching and physics/process networks belong to the range and keep their `rangerdanger_<key>` names. Delivery networks persist across range switches, including the corporate bridge when EU is active. One range runs at a time; switching packages (the UI toggle) stops the old range, starts the new one, installs its proxy routes (`nginx.routes.conf`) and imports its default firewall policy. Each package's `manifest.json` names which container plays which role and the endpoints the backend calls. `setup.sh` writes `RANGERDANGER_ROOT` (the install directory) to `.env`; every Compose call uses it as the project directory, and the backend mounts it read-only at the same path so the range's relative binds resolve to host paths. Teardown is by Compose project label only (`scripts/dev-down.sh`): the range with `down -v --remove-orphans`, which removes only its image-declared anonymous volumes because range models have no named volumes, and the platform without `-v`. The API is in [`api-spec.md`](api-spec.md).

### EU console boundaries

The Network console depicts the package's teaching paths, not every network attachment. Its collapsed **Infrastructure planes** explanation covers management/orchestration, internal solver-to-device process truth and isolated proxy-to-desktop delivery. These are not teaching conduits. Portal OpenPLC access is management access, not proof of the engineering-workstation-to-PLC conduit. RTU→PLC and RTU→station-clock traffic stays inside the station zone without containd; the clock path is simulated HTTP, not NTP/PTP.

Selected-flow evidence separates declared **Topology**, **Observed liveness** and **Authorization**, alongside operational purpose. Arrows identify connection initiation, not one-way information: IEC 104 and Modbus exchanges are bidirectional. Even an `ALLOW` zone-pair summary is not endpoint/port evaluation; `ALLOW`, `MIXED` and missing summaries leave flow authorization unknown, while `DENY` retains its summary evidence. EU Improved keeps source/port enforcement and adds three separate explicit-monitor IEC 104 rules for CC breaker/OLTC requests. The L4 source pin enforces; DPI detects and logs. A port allowance does not separate monitoring from commands or Modbus reads from writes; monitor matches never satisfy a BLOCK requirement. Read applied shadow evidence in containd Firewall/ICS tables, separately from TCP proof. Monitoring traverses NFQUEUE 101, adds latency and has no queue bypass: if the engine is down, the CC conduit drops. See the package [monitor evidence workflow](../lab-definitions/packages/eu-iec104-substation/knowledge/eu-ics-dpi.md) for counter lifetimes and the optional instructor-led PCAP review, which ends by discarding the candidate without commit.

The package knowledge pages hold the detailed [EU node/plane map](../lab-definitions/packages/eu-iec104-substation/knowledge/eu-range-node-map.md), [zone/conduit matrix](../lab-definitions/packages/eu-iec104-substation/knowledge/eu-zone-conduit-design.md), [authorization limits](../lab-definitions/packages/eu-iec104-substation/knowledge/eu-port-source-boundary.md) and [telecontrol roles](../lab-definitions/packages/eu-iec104-substation/knowledge/eu-telecontrol-architecture.md).

## Services

### Backend (Go + Gin)

The backend at `backend/cmd/server` exposes a Gin HTTP API, runs the range through Compose, execs into its containers through the Docker SDK, keeps the packages' scenarios in SQLite via GORM (topologies are served from the package catalog loaded at startup), and proxies to the containd firewall over REST and SSE.

Internal packages:

- `internal/config` - Viper-based configuration
- `internal/db` - GORM + SQLite setup
- `internal/models` - Scenario, the one persisted model
- `internal/labs` - package loader: the in-memory catalog (topology, default policy, scenarios) and the scenario rows
- `internal/orchestrator` - Docker SDK wrapper for exec sessions and exec resize; Compose owns container lifecycle
- `internal/containd` - REST client for containd firewall API
- `internal/server` - HTTP handlers, WebSocket terminals, exercise validators, PCAP management, traffic generation

### Frontend (Next.js + TypeScript)

Next.js 15 app at `frontend/app/` with dark-themed shadcn-style UI.

Top-level pages:

- `/` - Dashboard
- `/exercises` - Exercise library with completion tracking
- `/exercises/[id]` - Step-by-step exercise runner with inline terminals, command blocks, notes
- `/console` - Network Map with React Flow topology and per-node terminals
- `/substation` - Substation control panel (live device state, control popups)
- `/knowledge` - Reference material accompanying the exercises
- `/labs` - Workshop environment status
- `/scenarios` - Legacy redirect to `/exercises`

For the US package, the operator HMI is **FUXA**, served via the nginx proxy
at `/apps/fuxa-hmi/` rather than as a route inside the Next.js app.
For EU, `/substation` instead presents the IEC 104 **Control Centre** and a
separate management-backed **Physical System** view. A shared SCADA
view-state annunciator spans both; the compact link/current-connection
GI/current-point status precedes the CC schematic, with detailed diagnostics
collapsed below records. The current station inventory has 42 monitored
points, including four indication-only F2 points and no F2 supervisory
command. See [EU HMI/SCADA](../lab-definitions/packages/eu-iec104-substation/knowledge/eu-hmi-scada.md)
for view states, count semantics and observation-versus-physical-truth limits.

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
- `capbank-sim` - Switched capacitor bank with switch-count contact wear (DNP3 + Modbus + HTTP)
- `historian-sim` - Data historian polling the RTAC (Modbus + HTTP)
- `gps-sim` - GPS / NTP time source (NTP + HTTP)
- `rtac-sim` - Supervisory controller aggregating all field devices; runs autonomous DNP3 master polling every 5s and HTTP REST polling every 1s (read-only DNP3 outstation, Modbus, HTTP)

The RTAC's `/api/state` includes `physics` freshness (`stale`,
`consecutive_failures`, `last_good_at`, `solved_at`, `last_error`), while
`electrical.converged` and `electrical.solved_at` identify the retained last
good power-flow result. `/api/tags` exposes `physics.stale`,
`physics.consecutive_failures`, `physics.solved_at`, `physics.last_error`,
and `alarm.physics_stale`; `/api/health` includes the same `physics` object
and reports `status: degraded` while stale, otherwise `ok`.

Plus one Python sim:

- `opendss-sim` - Simplified feeder physics engine. **Python / FastAPI**, served HTTP-only on port 8080 from its own `services/opendss-sim/Dockerfile` (python:3.12-slim). Not part of the Go multi-stage build.

DNP3 outstation addresses (the five that expose DNP3): relay=1, recloser=2, regulator=3, capbank=4, rtac=10 (read-only).

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

Backend ──Compose────> range lifecycle (up / down of the range project)
        ──Docker SDK──> exec sessions, image and container checks, proxy reload
        ──REST──────> containd /api/v1/* (firewall policy, PCAP, events)
        ──WebSocket─> xterm.js clients (Docker exec, with resize forwarding)
        ──GORM──────> SQLite at /data/rangerdanger.db

Exercise runner (frontend) ──/api/scenarios/:id/validate──> backend validator
                          ──/api/workshop/nodes/:id/exec──> backend → Docker exec
                          ──/api/traffic/generate────────> backend → docker exec commands
                          ──/api/pcap/start──────────────> backend → containd PCAP API
```

## Key architectural decisions

### Multi-homed RTAC with kernel-pinned routing

The RTAC sits on four Docker networks directly (`ot_ops_net`, `field_net`, `physics_net`, `mgmt_net`; see the topology section above). This models a real substation RTAC that has a control-center-facing leg and a process-network leg. Without compensation, the kernel would forward field-bound traffic out the directly-connected `field_net` interface and bypass the firewall - exactly the "compromised RTAC bridges zones" failure mode. **`scripts/rtac-harden.sh` (run before the rtac-sim binary starts) prevents that bypass:**

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
| `RANGERDANGER_DB_PATH` | `backend/data/rangerdanger.db` (the in-container deployment overrides this to `/data/rangerdanger.db` via the platform Compose file) | SQLite path |
| `RANGERDANGER_LAB_DEFINITIONS_PATH` | `lab-definitions` | YAML source directory |
| `RANGERDANGER_ROOT` | none; the platform Compose files refuse to start without it (`setup.sh` writes it to `.env`) | Installation root: Compose project directory for ranges, mounted read-only at the same path |
| `RANGERDANGER_MODE` | `source` in `docker-compose.yml`, `release` in `docker-compose.release.yml` | Which range Compose file (`compose.source.yml` / `compose.release.yml`) the backend runs |
| `RANGERDANGER_PACKAGE` | `us-dnp3-substation` | Range package for the first start only; afterwards the recorded package (`/data/range.json`) wins. `setup.sh --package <id>` sets it |
| `CONTAIND_JWT_SECRET` | `rangerdanger-dev` | Shared secret the backend uses to mint JWTs for containd's REST API; the backend passes its value to the range's Compose so the firewall shares it |

The containd API URL and the initial firewall policy are no longer variables: they come from the active package (the firewall's `api` endpoint in `manifest.json`, and the package's default policy).

The legacy `OTLAB_*` prefix is honored as a deprecated alias - existing deployments continue to work but emit a warning at startup.

Variables read by the **containd container itself** (not the backend) and set in the US package's range Compose files:

| Variable | Value | Purpose |
|----------|-------|---------|
| `CONTAIND_SSH_SHELL_MODE` | `linux` | Drops the containd SSH user into bash instead of the appliance CLI |
| `CONTAIND_ALLOWED_ORIGINS` | `http://localhost:8088` | CORS + iframe-embedding origins for the containd web UI |
| `CONTAIND_LAB_MODE` | `1` | Lab-mode lock: auto-restores the default `containd` / `containd` admin on boot |

## Security notes

Lab networks are internal-only by default. Only the nginx proxy, containd web UI, and containd SSH bind to host ports. Containers run with minimal privileges except where simulators require `NET_ADMIN` for gateway manipulation. Default credentials are for lab use only and must not be exposed to production systems or untrusted networks.
