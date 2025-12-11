# OT Cyber Lab Trainer

The OT Cyber Lab Trainer is a containerized industrial control system (ICS) cyber range intended for hands-on training. It orchestrates a realistic OT network, wraps it with a Go + Gin API, and offers a modern Next.js UI for scheduling, visualizing, and monitoring labs.

## Repository Layout

```
backend/   # Go + Gin API, GORM + SQLite, orchestration service
frontend/  # Next.js + TypeScript + shadcn/ui dashboard
deploy/    # Dockerfiles and docker-compose definitions
lab-definitions/
  default-lab.yml
  scenarios/
    modbus-override.yml
    vendor-rdp-compromise.yml
scripts/   # helper scripts for dev + seeding
```

## Core Features (v1)

- **Three-zone OT topology** with it_net, dmz_net, ot_control_net, and ot_safety_net networks plus an external OPNsense firewall representation.
- **Node catalog** including Engineering Workstation, PLC trainer, SIS, HMI/SCADA, Historian, OT IDS, Jump Host, and management portal.
- **Lab orchestration** via Docker networks/containers, described as LabTemplates that can be instantiated into LabInstances.
- **Scenario engine** for guided training exercises and telemetry capture.
- **Web dashboard** with React Flow topology visualization, scenario controls, and D3/Recharts metrics.

## Getting Started

1. **Install prerequisites**: Docker, Docker Compose v2, Go 1.21+, Node.js 18+, and pnpm.
2. **Install dependencies**:
   ```bash
   (cd backend && go mod tidy)   # fetch Go modules
   (cd frontend && pnpm install) # install Node deps
   ```
3. **Dev environment**:
   ```bash
   ./scripts/dev-up.sh
   ```
   This boots the backend (Gin + Air) and frontend (Next.js) against local SQLite storage. Use `./scripts/dev-down.sh` to stop everything.
4. **Seed lab definitions**:
   ```bash
   ./scripts/seed-labs.sh
   ```
   Seeds default YAML definitions into the backend via the `/api/admin/seed` endpoint.
5. **Production-style stack**:
   ```bash
   cd deploy
   docker compose up -d --build
   ```

## Frontend Experience

- **Dashboard** cards show running labs, templates, topology preview, and telemetry quick charts.
- **Labs detail** view adds topology/scenario/metrics/nodes tabs with React Flow visualizations and start/stop actions.
- **Scenario library** surfaces YAML-driven exercise steps with tags.
- **Topology Builder** lets you drag template nodes, auto-layout by zone, and push them into lab templates via the API.

## Documentation

- `docs/architecture.md` – zone model, node inventory, orchestration overview.
- `docs/api-spec.md` – REST endpoints for labs, scenarios, telemetry, and nodes.

## Stretch Goals

- Capture/replay PCAPs, instructor vs. student modes, authentication & RBAC, scenario scripting DSL, and deeper protocol coverage (CIP, DNP3, OPC UA).

> ⚠️ This project is for isolated lab environments only. Exposing these containers or networks to production systems is unsafe.
