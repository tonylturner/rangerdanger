# CLAUDE.md

This file provides guidance to Claude Code when working with the **RangerDanger** codebase.

## Project Vision

**RangerDanger** is an OT/ICS cyber range for hands-on security training, specifically focused on **OT network segmentation**. It's an alternative to [LabShock](https://github.com/zakharb/labshock) with:

1. **Real DPI-capable segmentation** via the `containd` NGFW (not basic iptables)
2. **Cobalt Strike-inspired UI** - architecture visualization + interactive system access
3. **Dynamic container orchestration** - spin ICS/cybersecurity containers up/down for lab activities
4. **Integrated web console** - HTTPS and CLI access to all systems through the web app

### Key Differentiators from LabShock
| LabShock | RangerDanger |
|----------|--------------|
| Basic iptables/OPNsense | containd NGFW with ICS DPI (Modbus, DNP3, CIP) |
| Separate tools (Zeek, Splunk) | Unified dashboard with embedded DPI visibility |
| Static container topology | Dynamic orchestration per lab activity |
| Separate UIs per tool | Single Cobalt Strike-style command interface |

## Build and Run Commands

```bash
# Backend
cd backend && go build ./cmd/server && ./server
cd backend && go test ./...

# Frontend
cd frontend && npm install && npm run dev
cd frontend && npm run build

# Full stack (Docker Compose)
docker compose up -d --build
docker compose down

# Dev scripts
./scripts/dev-up.sh
./scripts/dev-down.sh
./scripts/seed-labs.sh
```

## Architecture

### Active Scenario: Substation Segmentation Validation Lab

Electric cooperative distribution substation segmentation lab. Students validate and improve network segmentation to protect critical substation control functions.

### Network Zones (via containd NGFW)

RangerDanger uses the `containd` firewall as the central L3 gateway. All zone traffic transits through containd for DPI and policy enforcement.

| Zone | containd Interface | Subnet | Purpose |
|------|-------------------|--------|---------|
| `enterprise_net` | eth0 (wan) | 10.10.10.0/24 | Enterprise IT, Kali attacker |
| `vendor_net` | eth1 (dmz) | 10.20.20.0/24 | Vendor jump box, engineering workstation |
| `ot_ops_net` | eth2 (lan1) | 10.30.30.0/24 | HMI, RTAC, OpenPLC |
| `field_net` | eth3 (lan2) | 10.40.40.0/24 | Relay, recloser, regulator |
| `physics_net` | (not firewalled) | 10.50.50.0/24 | Feeder physics simulation engine |

### Node Types
| Type | Zone | IP | Description |
|------|------|-----|-------------|
| `corp_workstation` | enterprise | 10.10.10.10 | Corporate desktop |
| `kali_pentest` | enterprise | 10.10.10.50 | Attacker box |
| `vendor_jumpbox` | vendor | 10.20.20.10 | Vendor remote access |
| `eng_workstation` | vendor | 10.20.20.20 | Engineering desktop |
| `fuxa_hmi` | ot_ops | 10.30.30.10 | Substation HMI (FUXA) |
| `rtac_sim` | ot_ops+field+physics | 10.30.30.20 | Supervisory controller / broker |
| `openplc` | ot_ops+field | 10.30.30.30 | Substation automation PLC |
| `relay_sim` | field | 10.40.40.20 | Feeder breaker / relay |
| `recloser_sim` | field | 10.40.40.21 | Mid-feeder recloser |
| `regulator_sim` | field | 10.40.40.22 | Voltage regulator |
| `opendss_sim` | physics | 10.50.50.20 | Feeder physics engine |

### Firewall Configs

Two firewall configs exist in `lab-definitions/firewall/`:
- **substation-weak.json** — Intentionally permissive baseline (enterprise→field ALLOWED)
- **substation-improved.json** — Target hardened state (only RTAC→field allowed)

### Simulator Services

Custom Go services in `services/` directory:
- `relay-sim` — Feeder breaker with trip/close, lockout, fault injection
- `recloser-sim` — Auto-reclose with shot counting, lockout, disable-reclose
- `regulator-sim` — Load tap changer with ±16 tap range, voltage regulation
- `rtac-sim` — Aggregator/broker polling field devices, physics engine, command forwarding
- `opendss-sim` — Simplified feeder model calculating energization and voltage

All expose `GET /api/state`, `POST /api/command`, `GET /api/audit`, `GET /api/health` on port 8080.

### containd Integration

The `containd` NGFW ([github.com/tonylturner/containd](https://github.com/tonylturner/containd)) provides:
- **Zone-based firewall** with nftables enforcement
- **ICS DPI** - Modbus/TCP function code filtering, register ranges, read-only modes
- **IT DPI** - DNS, TLS/SNI, HTTP, SSH, RDP, SMB visibility
- **Web UI + CLI** - Single management interface at :8080/:8443
- **SSH console** - Direct shell access via port 2222

containd connects to all zone networks and acts as the default gateway for each.

## Backend (Go + Gin)

### Directory Structure
```
backend/
  cmd/server/main.go       # Entry point
  internal/
    config/config.go       # Viper configuration
    db/db.go               # GORM + SQLite
    models/models.go       # LabTemplate, LabInstance, NodeDefinition, etc.
    server/server.go       # Gin router and handlers
    labs/
      loader.go            # YAML definition loader
      catalog.go           # Node type catalog
    orchestrator/
      orchestrator.go      # Docker orchestration (needs real Docker SDK)
```

### Key Models
- `LabTemplate` - Reusable lab topology definition
- `LabInstance` - Running/stopped lab derived from template
- `NodeDefinition` - Node metadata within a lab instance
- `Scenario` - Training scenario with steps
- `ScenarioRun` - Execution state for scenario
- `TelemetryPoint` - Metrics for visualization

### Environment Variables
| Variable | Default | Description |
|----------|---------|-------------|
| `OTLAB_HTTP_PORT` | 8080 | API server port |
| `OTLAB_DB_PATH` | backend/data/otlab.db | SQLite path |
| `OTLAB_LAB_DEFINITIONS_PATH` | lab-definitions | YAML definitions |

## Frontend (Next.js + TypeScript)

### Cobalt Strike-Inspired UI Goals

The UI should feel like a **command-and-control interface**:

1. **Main Dashboard** - Network topology with zone boundaries, node status (online/offline/compromised)
2. **Node Inspector** - Click a node to see details, open web UI (iframe), or launch terminal
3. **Console Drawer** - Bottom drawer with tabbed terminals to multiple systems
4. **Activity Feed** - Real-time events: connections, Modbus writes, IDS alerts, scenario progress
5. **Scenario Runner** - Step-by-step guided exercises with validation

### Directory Structure
```
frontend/
  app/
    page.tsx               # Dashboard (topology + status)
    layout.tsx             # Root layout with nav
    labs/[id]/page.tsx     # Lab detail
    console/page.tsx       # Network console (main interactive view)
  components/
    network-console.tsx    # Main Cobalt Strike-style interface
    topology-builder.tsx   # Drag-drop topology editor
    lab-detail.tsx         # Lab detail with tabs
    scenario-list.tsx      # Scenario cards
  lib/
    api.ts                 # API client
```

### Styling
- Dark theme (slate-950 backgrounds)
- Zone colors: wan=#38bdf8, dmz=#a855f7, ot_control=#f97316, ot_safety=#22c55e
- Cobalt Strike aesthetic: high-contrast, terminal-like, status indicators

## Docker Infrastructure

### Target Compose Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  Host (ports: 8088 proxy, 9080 containd UI, 2222 SSH)           │
└─────────────────────────────────────────────────────────────────┘
          │
          ▼
┌─────────────────────────────────────────────────────────────────┐
│  containd NGFW (all zones, DPI enabled)                         │
│  ├── enterprise_net (10.10.10.2) ─── corp-ws, kali, proxy       │
│  ├── vendor_net (10.20.20.2)     ─── vendor-jump, eng-ws        │
│  ├── ot_ops_net (10.30.30.2)     ─── fuxa-hmi, rtac, openplc    │
│  └── field_net (10.40.40.2)      ─── relay, recloser, regulator │
│                                                                  │
│  physics_net (10.50.50.0/24) ─── opendss-sim (not firewalled)   │
└─────────────────────────────────────────────────────────────────┘
```

All inter-zone traffic flows through containd, enabling:
- DPI visibility into control traffic between OT ops and field devices
- Policy enforcement: enterprise/vendor cannot reach field devices directly
- Segmentation validation: only RTAC can control field devices in improved config
- Attack path demonstration: weak baseline allows full access for training

### Services to Orchestrate
- `firewall` - containd NGFW gateway (always running)
- `proxy` - Nginx reverse proxy for web access
- `backend` - RangerDanger API
- `frontend` - RangerDanger UI
- `corp_ws` - Corporate workstation in enterprise zone
- `kali` - Attacker box in enterprise zone
- `vendor_jump` - Vendor remote access in vendor zone
- `eng_workstation` - Engineering desktop in vendor zone
- `fuxa_hmi` - Substation HMI in OT operations zone
- `rtac_sim` - Supervisory controller / broker (multi-homed)
- `openplc` - Substation automation PLC
- `relay_sim` / `recloser_sim` / `regulator_sim` - Field device simulators
- `opendss_sim` - Feeder physics engine

## containd Integration

**containd is a separate project** ([github.com/tonylturner/containd](https://github.com/tonylturner/containd)) - built independently and published to GHCR. RangerDanger pulls `ghcr.io/tonylturner/containd:latest` as a dependency. Only config-level changes (firewall rules, IDS rule provisioning) are made from RangerDanger; code changes happen in the containd repo.

### containd Capabilities (consumed via image)
- **Single appliance image** - `ghcr.io/tonylturner/containd:latest`
- **Multi-zone networking** - 8 interfaces (wan, dmz, lan1-lan6)
- **ICS DPI** - Modbus/TCP function code filtering, register ranges
- **IT DPI** - DNS, TLS/SNI, HTTP, SSH, RDP, SMB visibility
- **Web UI** - Management dashboard at :8080/:8443
- **SSH console** - Direct shell access via :2222
- **REST API** - `/api/v1/*` for config, status, events

### Integration Pattern
```yaml
services:
  firewall:
    image: ghcr.io/tonylturner/containd:latest
    pull_policy: always
    cap_add: [NET_ADMIN, NET_RAW]
    networks:
      enterprise_net: { ipv4_address: 10.10.10.2 }
      vendor_net: { ipv4_address: 10.20.20.2 }
      ot_ops_net: { ipv4_address: 10.30.30.2 }
      field_net: { ipv4_address: 10.40.40.2 }
    environment:
      - CONTAIND_MODE=all
      - CONTAIND_LAB_MODE=1
      - CONTAIND_JWT_SECRET=${CONTAIND_JWT_SECRET:-rangerdanger-dev}
    volumes:
      - ./data/firewall:/data
```

### API Integration Points
1. **Health**: `GET /api/v1/health` - firewall status for dashboard
2. **Events**: `GET /api/v1/events` - DPI/IDS events for activity feed
3. **Policies**: `GET/POST /api/v1/policies` - display/configure zone rules
4. **Sessions**: `GET /api/v1/sessions` - active connections for topology overlay

### Development Workflow
- containd repo: `github.com/tonylturner/containd` (separate project, do not modify locally)
- RangerDanger always pulls `ghcr.io/tonylturner/containd:latest` with `pull_policy: always`
- Only config-level changes (firewall rules, IDS rules) are made from RangerDanger

## Development Guidelines

### Go Backend
- Keep handlers thin; business logic in services
- Use context-aware functions
- Handle errors explicitly with structured logging
- Docker SDK integration needed for real container orchestration

### TypeScript Frontend
- Avoid `any` types
- Keep API types in `lib/api.ts`
- React Flow for topology visualization
- xterm.js for terminal emulation

### Docker
- Use public images only
- containd must connect to all zone networks
- Containers get zone-specific IPs with containd as gateway

## Current State & Gaps

### Implemented
- REST API with full lab CRUD and orchestration
- Frontend pages (Dashboard, Labs, Topology, Console)
- React Flow topology visualization with zone coloring and firewall rule labels
- YAML-based lab definitions (auto-loads all *.yml in lab-definitions/)
- Docker SDK integration for container lifecycle
- containd integration with JWT auth (lab mode)
- Terminal access via WebSocket (Docker exec + SSH to firewall)
- SSE event streaming from containd
- Substation segmentation scenario with 3 attack exercises
- Field device simulators (relay, recloser, regulator) with HTTP APIs
- RTAC supervisory controller aggregating device state
- Feeder physics engine with energization/voltage calculation
- Weak baseline + improved firewall configs for progressive learning
- Kali attacker in enterprise zone

### Remaining Gaps
1. **FUXA HMI screens** - One-line diagram, alarm view, segmentation impact view need configuration
2. **Scenario automation** - execute steps, validate outcomes automatically
3. **Command audit correlation UI** - cyber-to-process event mapping in frontend
4. **PCAP capture/replay** - per scenario traffic recording
5. **Multi-user RBAC** - instructor vs student modes
6. **Capbank simulator** - optional capacitor bank device
7. **IDS alerts integration** - Suricata alerts in activity feed

## Access Points

| Service | URL | Credentials |
|---------|-----|-------------|
| RangerDanger UI | http://localhost:8088 | N/A |
| containd UI | http://localhost:9080 | N/A |
| containd SSH | ssh -p 2222 containd@localhost | containd/containd |
