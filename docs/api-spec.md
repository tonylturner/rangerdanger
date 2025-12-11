# API Spec (v1)

Base URL: `/api`

## Labs

- `GET /labs/templates` → `{ "templates": LabTemplate[] }`
- `POST /labs/templates` → create/update template; accepts full LabTemplate JSON.
- `POST /labs/instances` `{ "template_id": string, "name": string }` → `202 Accepted` with new LabInstance.
- `GET /labs/instances` → `{ "instances": LabInstance[] }`.
- `GET /labs/instances/:id` → LabInstance + nodes.
- `POST /labs/instances/:id/start|stop` → set desired state (orchestration hook).
- `DELETE /labs/instances/:id` → tears down instance.
- `GET /labs/instances/:id/topology` → `{"topology": {...}}` parsed JSON ready for React Flow.
- `PATCH /labs/instances/:id/topology` → stores UI-only metadata (positions/layout) into instance runtime_config.
- `GET /labs/instances/:id/metrics` → returns `{ "metrics": TelemetryPoint[] }` filtered by instance.
- `GET /labs/instances/:id/events` → returns scenario runs/events for the instance.

## Nodes

- `POST /nodes/:node_id/action` `{ "action": "restart" }` – placeholder for orchestration commands.

## Scenarios

- `GET /scenarios?lab_template_id=` – list metadata globally or filter by template.
- `GET /scenarios/:id` – fetch scenario detail.
- `POST /scenarios` – create scenario (linked to template id, tags, steps JSON arrays).
- `POST /scenarios/:id/run` `{ "lab_instance_id": "..." }` – create ScenarioRun record and kick off automation (stub today).
- `GET /scenario-runs/:id` – status + events log.

## Admin

- `POST /admin/seed` – load `lab-definitions/*.yml` into persistence.

### Data Contracts

See `backend/internal/models/models.go` for struct-level documentation. The `topology` JSON is intentionally generic so the frontend can feed it directly to React Flow.
