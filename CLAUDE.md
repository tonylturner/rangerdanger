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

### Network Zones (via containd NGFW)

RangerDanger uses the `containd` firewall as the central L3 gateway. All zone traffic transits through containd for DPI and policy enforcement.

| Zone | containd Interface | Subnet | Purpose |
|------|-------------------|--------|---------|
| `wan` | eth0 | 192.168.240.0/24 | External/management access |
| `dmz` | eth1 | 192.168.241.0/24 | HMI, Historian, IDS sensors |
| `ot_control` | eth2 | 192.168.242.0/24 | Process PLCs, field I/O |
| `ot_safety` | eth3 | 192.168.243.0/24 | Safety PLCs (SIS) |
| `it_workstations` | eth4 | 192.168.244.0/24 | Engineering WS, Jump hosts |

### Node Types (ICS Containers)
| Type | Image | Zone | Description |
|------|-------|------|-------------|
| `ews` | linuxserver/webtop:ubuntu-mate | it_workstations | Engineering Workstation (noVNC) |
| `jump_host` | kalilinux/kali-rolling | it_workstations | Pentest tooling |
| `plc_trainer` | tuttas/openplc_v3:latest | ot_control | Process PLC (OpenPLC) |
| `sis_plc` | tuttas/openplc_v3:latest | ot_safety | Safety PLC |
| `hmi_scada` | frangoteam/fuxa:latest | dmz | HMI/SCADA (FUXA) |
| `historian` | influxdb:2 | dmz | Time-series database |
| `grafana` | grafana/grafana | dmz | Metrics visualization |
| `ot_ids` | jasonish/suricata:latest | dmz | OT IDS sensor |

### containd Integration

The `containd` NGFW (from `/Users/tturner/Documents/GitHub/containd`) provides:
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
│  Host (exposed ports: 8088 proxy, 8080 containd UI)             │
└─────────────────────────────────────────────────────────────────┘
          │
          ▼
┌─────────────────────────────────────────────────────────────────┐
│  containd NGFW (all zones attached)                             │
│  ├── wan (eth0)  ─── proxy, frontend, backend                   │
│  ├── dmz (eth1)  ─── hmi, historian, grafana, ids               │
│  ├── ot_control (eth2) ─── plc_trainer                          │
│  ├── ot_safety (eth3)  ─── sis_plc                              │
│  └── it_workstations (eth4) ─── ews, jump_host                  │
└─────────────────────────────────────────────────────────────────┘
```

All inter-zone traffic flows through containd, enabling:
- DPI visibility into Modbus traffic between HMI and PLCs
- Policy enforcement (e.g., block Modbus writes from DMZ)
- IDS alerts on suspicious OT activity

### Services to Orchestrate
- `containd` - NGFW gateway (always running)
- `proxy` - Nginx reverse proxy for web access
- `backend` - RangerDanger API
- `frontend` - RangerDanger UI
- ICS containers - dynamically started/stopped per lab

## containd Integration

**containd is a separate project** - built independently and published to a container registry (e.g., `ghcr.io/tturner/containd:latest`). RangerDanger pulls it as a standard Docker image dependency.

### containd Capabilities (consumed via image)
- **Single appliance image** - `ghcr.io/tturner/containd:latest`
- **Multi-zone networking** - 8 interfaces (wan, dmz, lan1-lan6)
- **ICS DPI** - Modbus/TCP function code filtering, register ranges
- **IT DPI** - DNS, TLS/SNI, HTTP, SSH, RDP, SMB visibility
- **Web UI** - Management dashboard at :8080/:8443
- **SSH console** - Direct shell access via :2222
- **REST API** - `/api/v1/*` for config, status, events

### Integration Pattern
```yaml
# In RangerDanger's docker-compose.yml
services:
  firewall:
    image: ghcr.io/tturner/containd:latest
    cap_add: [NET_ADMIN, NET_RAW]
    networks:
      wan: { ipv4_address: 192.168.240.2 }
      dmz: { ipv4_address: 192.168.241.2 }
      ot_control: { ipv4_address: 192.168.242.2 }
      ot_safety: { ipv4_address: 192.168.243.2 }
      it_workstations: { ipv4_address: 192.168.244.2 }
    environment:
      - CONTAIND_MODE=all
    volumes:
      - ./data/firewall:/data
```

### API Integration Points
1. **Health**: `GET /api/v1/health` - firewall status for dashboard
2. **Events**: `GET /api/v1/events` - DPI/IDS events for activity feed
3. **Policies**: `GET/POST /api/v1/policies` - display/configure zone rules
4. **Sessions**: `GET /api/v1/sessions` - active connections for topology overlay

### Development Workflow
- containd repo: `/Users/tturner/Documents/GitHub/containd`
- Build & push: `docker build -f Dockerfile.mgmt -t ghcr.io/tturner/containd:dev . && docker push ...`
- RangerDanger pulls tagged images; no source dependency

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
- REST API scaffolding
- Frontend pages (Dashboard, Labs, Topology, Console)
- React Flow topology visualization
- YAML-based lab definitions

### Critical Gaps to Address
1. **Rename to RangerDanger** - project name, branding, package names
2. **Docker SDK integration** - real container start/stop/destroy
3. **containd integration** - pull image from registry, wire zones through NGFW as gateway
4. **Terminal access** - xterm.js WebSocket to container shells (via Docker exec or SSH)
5. **Unified event stream** - poll containd API for DPI/IDS events, merge with lab events
6. **Scenario automation** - execute steps, validate outcomes

### Nice-to-Have
- PCAP capture/replay per scenario
- Instructor vs student modes
- Multi-user RBAC
- containd ICS policy templates for common scenarios

## Rename Checklist

When renaming from rangerrocks → RangerDanger:
- [ ] `go.mod` module path
- [ ] Docker image names
- [ ] Frontend package.json name
- [ ] UI branding (logo, title, nav)
- [ ] docker-compose service names
- [ ] GitHub repo name (if applicable)
- [ ] CLAUDE.md and agents.md references
