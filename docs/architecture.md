# Architecture Overview

## Zones and Networks

- **it_net** – hosts the lab management portal, jump/pentest workstation, and access tooling for trainees.
- **dmz_net** – provides a buffer network for HMI/SCADA, historian, and IDS sensors that monitor traffic toward OT.
- **ot_control_net** – core process network containing the primary PLC trainer and associated field IO simulators.
- **ot_safety_net** – safety instrumented system (SIS) running a dedicated OpenPLC instance for independent logic.
- **OPNsense Firewall** – modeled as an external VM bridging all zones via macvlan/bridge interfaces. The trainer tracks metadata (URL, API key) but does not manage lifecycle yet.

## Services

- **Backend (Go + Gin)** – REST API, LabTemplate persistence, Docker orchestration stub, scenario tracking, telemetry recording.
- **Frontend (Next.js + shadcn/ui)** – Dashboard, labs list/detail, topology builder using React Flow, metrics via Recharts/D3.
- **Lab Nodes** – Containers defined in Docker Compose: Engineering Workstation (noVNC desktop), OpenPLC instances, FUXA HMI, Historian stack (InfluxDB + Grafana), Suricata IDS, Jump Host (Kali-lite), plus optional supporting services.

## Data Flow

1. Backend loads YAML templates from `lab-definitions/` into SQLite via GORM on boot or `/api/admin/seed`.
2. Frontend consumes `/api/*` endpoints using React Query. Topology views render backend-provided JSON directly into React Flow.
3. Lab instances call into the orchestrator, which will eventually use the Docker SDK to create networks/containers. For v1 a stub records synthetic nodes.
4. Telemetry endpoints expose persisted `TelemetryPoint` rows for time-series plotting.

## Deployment

- `deploy/docker-compose.yml` defines backend, frontend, and lab node services bound to named Docker networks representing OT zones.
- Dev scripts (`scripts/dev-up.sh`, `scripts/dev-down.sh`) wrap docker-compose for quick iteration.
- Future work may package each component into its own image for Kubernetes, but Compose keeps v1 simple and reproducible.

## Security Notes

- Lab networks are internal-only by default; only the frontend/backed HTTP ports bind to localhost.
- Credentials for lab nodes (e.g., OpenPLC, OPNsense) must be changed per environment. This repository ships defaults for demo use only.
- Documented for isolated environments; do not bridge OT lab networks into production environments.
