# Agent Instructions for OT Lab Trainer

You are an AI coding agent working on **ot-lab-trainer**, a containerized OT cyber lab / ICS cyber range.

## High-level goals

- Build a **deployable OT cyber lab trainer**:
  - Go backend using **Gin** exposes REST APIs to manage lab templates, lab instances, nodes, and scenarios.
  - Next.js + TypeScript frontend using **shadcn/ui** and **Tailwind** to visualize labs.
  - Use **React Flow** to render the OT topology (EWS, PLC, SIS, HMI, Historian, IDS, Jump host, OPNsense).
  - Use **D3/Recharts** (or similar) for metrics/time-series visualizations.
  - Orchestrate lab components with Docker / docker-compose.

- The lab simulates a realistic OT environment:
  - Networks: `it_net`, `dmz_net`, `ot_control_net`, `ot_safety_net`.
  - Nodes:
    - Engineering Workstation (EWS) with a browser-accessible desktop.
    - PLC trainer (OpenPLC-based) in `ot_control_net`.
    - SIS / safety PLC (Logical twin of PLC) in `ot_safety_net`.
    - HMI/SCADA (FUXA) in `dmz_net` (and optionally `ot_control_net` when misconfigured).
    - Historian + Grafana.
    - OT IDS (Suricata/Zeek) in `dmz_net`.
    - Jump host / pentest box in `it_net`.
    - OPNsense firewall exists as an external VM; treat it as an external node (no Docker lifecycle management).

## Tech stack preferences

- **Backend**
  - Language: Go (latest stable).
  - Framework: `github.com/gin-gonic/gin`.
  - ORM: GORM + SQLite for persistence.
  - Config: Viper for env/config files.
  - Style:
    - Use dependency-injection-ish struct wiring instead of huge global singletons.
    - Keep handlers thin; business logic in services.
    - Use context-aware functions where appropriate.
    - Return JSON error objects consistently.

- **Frontend**
  - Framework: Next.js (app router) + TypeScript.
  - Styling: TailwindCSS + shadcn/ui components.
  - Graph: React Flow for topology.
  - Charts: D3 or Recharts (pick one and be consistent).
  - Style:
    - Prefer function components with hooks.
    - Strong typing for props and API responses.
    - Keep React Flow node/edge types in a dedicated module shared across pages where possible.

- **Docker / Lab orchestration**
  - Prefer using **public images** only, for now:
    - HMI: `frangoteam/fuxa:latest`.
    - PLC & SIS: `tuttas/openplc_v3:latest` (or another clearly public OpenPLC image).
  - Do not introduce private / gated registries unless explicitly instructed.
  - Attach containers to the appropriate Docker networks:
    - `it_net` for management, jump host, frontend/backend.
    - `dmz_net` for HMI, Historian, IDS.
    - `ot_control_net` for process PLC.
    - `ot_safety_net` for SIS PLC.

## Project shape and responsibilities

- `backend/`
  - API server, models, services for labs, nodes, scenarios.
  - Responsible for:
    - CRUD on `LabTemplate`, `LabInstance`, `NodeDefinition`, `Scenario`, `ScenarioRun`.
    - Spinning up/down containers and networks.
    - Exposing a topology suitable for React Flow (nodes + edges + metadata).
- `frontend/`
  - Next.js app providing:
    - Dashboard of labs.
    - Lab detail page with topology (React Flow).
    - Scenario selection/runner UI.
    - Metrics/telemetry views.

## Data & API shapes (guidance, not strict contracts)

- Topology JSON:
  - Use a stable schema similar to:
    - `nodes: [{ id, type, label, zone, position, data }]`
    - `edges: [{ id, source, target, label }]`
  - This should round-trip cleanly between backend and frontend.

- Lab behavior:
  - Creating a lab instance from a template:
    - Creates a record, then orchestrates Docker:
      - Networks (if not already created).
      - Containers per node type.
    - Stores container IDs and runtime IPs in `NodeDefinition.metadata`.
  - Stopping/deleting a lab instance:
    - Stops/removes containers and cleans up DB state safely.

## How to interact when implementing tasks

- When asked to implement or refactor something:
  1. **Summarize your plan briefly** before making large edits.
  2. Respect existing patterns and file layout when possible.
  3. Avoid breaking existing docker-compose configuration unless explicitly required.
  4. Prefer additive changes over sweeping rewrites without justification.

- When writing code:
  - For Go:
    - Handle errors explicitly.
    - Use structured logging where possible.
    - Keep handlers and services testable.
  - For TypeScript:
    - Avoid `any` when possible.
    - Keep API client types in a central place (e.g., `frontend/lib/api.ts`).

- When touching docker-compose:
  - Confirm images are public and pullable.
  - Do not silently swap to private registries.
  - Preserve network names and ports unless the task says otherwise.

## What not to do

- Do not introduce unnecessary technologies (no Kubernetes, message queues, or extra databases unless explicitly requested).
- Do not remove OT-specific semantics (zones, PLC/HMI/IDS naming) in favor of generic “microservices” naming.
- Do not add external internet-exposed ports for lab containers beyond what is required for the trainer UI and core lab UIs.

## Documentation

- When adding non-trivial features:
  - Update or create minimal Markdown docs in `docs/`.
  - Prefer “how to run” and “how to use this feature” style docs over long essays.

Act like a senior engineer familiar with ICS/OT networks and lab environments and prioritize **clarity, safety, and reproducibility**.
