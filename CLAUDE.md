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

| Zone | containd Interface | Subnet | Purpose | Firewall Policy |
|------|-------------------|--------|---------|-----------------|
| `it_net` | eth0 (wan) | 10.10.10.0/24 | IT/Management, Pentest | SSH/HTTP/S/RDP to DMZ |
| `dmz_net` | eth1 (dmz) | 10.20.20.0/24 | HMI View, EWS, Jump Box, Historian, IDS | Modbus R/O to OT zones |
| `ot_control_net` | eth2 (lan1) | 10.30.30.0/24 | HMI Control, Process PLCs | Modbus R/W internal |
| `ot_safety_net` | eth3 (lan2) | 10.40.40.0/24 | Safety PLCs (SIS) | **READ ONLY** (DPI enforced) |

### Node Types (ICS Containers)
| Type | Image | Zone | IP | Description |
|------|-------|------|-----|-------------|
| `kali_pentest` | kalilinux/kali-rolling | it_net | 10.10.10.50 | Penetration testing box |
| `hmi_view` | frangoteam/fuxa:latest | dmz/ot_ctrl/ot_safety | 10.20.20.10 | Read-only HMI (firewall enforced) |
| `ews` | linuxserver/webtop:ubuntu-mate | dmz_net | 10.20.20.50 | Engineering Workstation (noVNC) |
| `ubuntu_jumpbox` | linuxserver/webtop:ubuntu-xfce | dmz_net | 10.20.20.60 | Jump host (sole gateway to OT Control) |
| `hmi_control` | frangoteam/fuxa:latest | ot_control_net | 10.30.30.11 | Full-control HMI |
| `plc_trainer` | tuttas/openplc_v3:latest | ot_control_net | 10.30.30.20-22 | Process PLCs (OpenPLC) |
| `sis_plc` | tuttas/openplc_v3:latest | ot_safety_net | 10.40.40.20 | Safety PLC (read-only access) |
| `historian` | influxdb:2 | dmz_net | - | Time-series database |
| `ot_ids` | jasonish/suricata:latest | dmz_net | - | OT IDS sensor |

### Firewall Rules (ICS DPI)

The containd firewall enforces zone-based policies with Modbus function code filtering:

| Rule | Source | Destination | Policy |
|------|--------|-------------|--------|
| IT to DMZ | wan (IT) | dmz | Allow SSH, HTTP/S, RDP, VNC |
| Jumpbox to OT | dmz (10.20.20.60) | lan1 (OT Control) | Allow Modbus R/W |
| HMI View to OT | dmz (10.30.30.10) | lan1 (OT Control) | Allow Modbus READ only (FC 1-4) |
| HMI View to Safety | dmz (10.40.40.10) | lan2 (OT Safety) | Allow Modbus READ only (FC 1-4) |
| HMI Control internal | lan1 | lan1 | Allow Modbus R/W |
| HMI Control to Safety | lan1 (10.30.30.11) | lan2 | Allow Modbus READ only (FC 1-4) |
| **Block writes to Safety** | any | lan2 (OT Safety) | **DENY Modbus FC 5,6,15,16,22,23** |

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
│  Host (ports: 8088 proxy, 9080 containd UI, 2222 SSH)           │
└─────────────────────────────────────────────────────────────────┘
          │
          ▼
┌─────────────────────────────────────────────────────────────────┐
│  containd NGFW (all zones, ICS DPI enabled)                     │
│  ├── it_net (10.10.10.2)   ─── proxy, frontend, backend, kali   │
│  ├── dmz_net (10.20.20.2)  ─── hmi_view, ews, jumpbox, ids      │
│  ├── ot_control (10.30.30.2) ─── hmi_control, plc_process/comp  │
│  └── ot_safety (10.40.40.2)  ─── plc_safety (READ ONLY)         │
└─────────────────────────────────────────────────────────────────┘
```

All inter-zone traffic flows through containd, enabling:
- DPI visibility into Modbus traffic between HMI and PLCs
- Policy enforcement: **Block Modbus writes to safety zone**
- One-way monitoring: HMI View can read safety PLC but not control
- Jump box as sole gateway from DMZ to OT Control

### Services to Orchestrate
- `firewall` - containd NGFW gateway (always running)
- `proxy` - Nginx reverse proxy for web access
- `backend` - RangerDanger API
- `frontend` - RangerDanger UI
- `hmi_view` - Read-only HMI in DMZ (multi-homed to all OT zones)
- `hmi_control` - Full-control HMI in OT Control zone only
- `ews` - Engineering workstation in DMZ
- `kali_pentest` - Penetration testing box in IT
- `ubuntu_jumpbox` - Jump host in DMZ (sole gateway to OT)
- `plc_*` - OpenPLC containers for process control
- `plc_safety` - Safety PLC (read-only access enforced)

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
    image: containd:local  # or ghcr.io/tturner/containd:latest
    cap_add: [NET_ADMIN, NET_RAW]
    networks:
      it_net: { ipv4_address: 10.10.10.2 }
      dmz_net: { ipv4_address: 10.20.20.2 }
      ot_control_net: { ipv4_address: 10.30.30.2 }
      ot_safety_net: { ipv4_address: 10.40.40.2 }
    environment:
      - CONTAIND_MODE=all
      - CONTAIND_AUTO_WAN_SUBNET=10.10.10.0/24
      - CONTAIND_AUTO_DMZ_SUBNET=10.20.20.0/24
      - CONTAIND_AUTO_LAN1_SUBNET=10.30.30.0/24
      - CONTAIND_AUTO_LAN2_SUBNET=10.40.40.0/24
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
- REST API with full lab CRUD and orchestration
- Frontend pages (Dashboard, Labs, Topology, Console)
- React Flow topology visualization with zone coloring and firewall rule labels
- YAML-based lab definitions
- Docker SDK integration for container lifecycle
- containd integration with ICS DPI (Modbus function code filtering)
- Terminal access via WebSocket (Docker exec + SSH to firewall)
- SSE event streaming from containd
- Split HMI architecture (view-only in DMZ, full-control in OT)
- Safety zone one-way traffic (read-only enforced by DPI)
- Ubuntu jump box as sole gateway to OT Control
- Kali pentest box in IT network

### Remaining Gaps
1. **Scenario automation** - execute steps, validate outcomes automatically
2. **PCAP capture/replay** - per scenario traffic recording
3. **Multi-user RBAC** - instructor vs student modes
4. **VPN client setup** - OpenVPN/WireGuard profiles on jump box
5. **IDS alerts integration** - Suricata alerts in activity feed

## Access Points

| Service | URL | Credentials |
|---------|-----|-------------|
| RangerDanger UI | http://localhost:8088 | N/A |
| HMI View | http://localhost:8088/apps/hmi/ | N/A |
| HMI Control | http://localhost:8088/apps/hmi-control/ | N/A |
| Engineering WS | http://localhost:8088/apps/ews/ | N/A |
| Jump Box | http://localhost:8088/apps/jumpbox/ | N/A |
| containd UI | http://localhost:9080 | N/A |
| containd SSH | ssh -p 2222 containd@localhost | containd/containd |
| OpenPLC (PLC-101) | http://localhost:8088/apps/plc/ | openplc/openplc |
