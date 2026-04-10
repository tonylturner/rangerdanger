# API Specification

Base URL: `/api` (when accessed through the nginx proxy at `http://localhost:8088/api`)

All endpoints return JSON unless otherwise noted. Request and response bodies use `snake_case` field names.

## Health

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Liveness probe. Returns `{"status": "ok"}` |

## Admin

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/admin/seed` | Reload all YAML lab and scenario definitions from `lab-definitions/` into the database |

## Labs

Legacy endpoints for arbitrary lab templates and instances. The current workshop-focused flow uses the `/workshop/*` endpoints instead, but these remain for managing custom labs.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/labs/templates` | List lab templates |
| `POST` | `/labs/templates` | Create or update a lab template |
| `GET` | `/labs/instances` | List lab instances |
| `POST` | `/labs/instances` | Create a new instance from a template. Body: `{"template_id": "...", "name": "..."}` |
| `GET` | `/labs/instances/:id` | Get instance detail including node definitions |
| `POST` | `/labs/instances/:id/start` | Start orchestration for the instance |
| `POST` | `/labs/instances/:id/stop` | Stop containers for the instance |
| `DELETE` | `/labs/instances/:id` | Delete an instance |
| `GET` | `/labs/instances/:id/topology` | Topology JSON shaped for React Flow |
| `PATCH` | `/labs/instances/:id/topology` | Persist UI-only metadata (node positions, etc.) |
| `GET` | `/labs/instances/:id/graph` | Alternate topology graph format |
| `GET` | `/labs/instances/:id/metrics` | Telemetry points for the instance |
| `GET` | `/labs/instances/:id/events` | Scenario runs / events log |
| `GET` | `/labs/instances/:id/live-events` | SSE stream of live events |
| `GET` | `/labs/instances/:id/nodes/:nodeId/terminal` | WebSocket terminal (see Terminals section) |
| `*` | `/labs/instances/:id/nodes/:nodeId/ui/*path` | HTTP proxy to a node's web UI |

## Workshop (current exercise flow)

The workshop endpoints operate on the always-running substation lab defined in `lab-definitions/substation-segmentation.yml` and are what the exercise runner uses.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/workshop/graph` | Current topology as a graph (nodes + edges) |
| `GET` | `/workshop/status` | Status of all workshop nodes |
| `GET` | `/workshop/nodes/:nodeId/terminal` | WebSocket terminal to a node (see Terminals) |
| `POST` | `/workshop/nodes/:nodeId/exec` | Run a one-shot command on a node. Body: `{"cmd": "...", "timeout_sec": 30}`. Returns `{"stdout", "stderr", "exit_code", "duration_ms"}` |
| `POST` | `/workshop/reset` | Reset substation state (clear lockouts, restore voltage, re-enable reclose, etc.) |
| `POST` | `/workshop/test-suite` | Run all exercise validators programmatically |

## Scenarios (exercises)

Exercises are stored internally as "scenarios" for historical reasons — the user-facing term is "exercise".

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/scenarios` | List all exercises |
| `POST` | `/scenarios` | Create an exercise |
| `GET` | `/scenarios/:id` | Get exercise detail with steps |
| `POST` | `/scenarios/:id/run` | Start a scenario run against a lab instance |
| `POST` | `/scenarios/:id/steps/:stepIdx/execute` | Execute an automated action for a single step (e.g., inject_fault, apply firewall config) |
| `GET` | `/scenarios/:id/validate` | Run all validators for the exercise. Returns `{"scenario_id", "outcome", "checks": [...], "timestamp"}` |
| `GET` | `/scenario-runs/:id` | Status of a scenario run |

### Validation response

```json
{
  "scenario_id": "baseline-assessment",
  "outcome": "PASS",
  "checks": [
    {
      "name": "PCAP captured",
      "status": "pass",
      "detail": "Baseline capture file found — traffic was recorded"
    }
  ],
  "timestamp": "2026-04-10T02:41:09Z"
}
```

Check status is one of `pass`, `fail`, or `warn`. Outcome is `PASS` if all checks pass, `FAIL` otherwise.

## Substation

Proxied reads from the RTAC and control commands to individual field devices.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/substation/tags` | Flat namespace of all SCADA tags |
| `GET` | `/substation/state` | Full aggregated state with devices, electrical, comms |
| `GET` | `/substation/health` | Device health summary |
| `GET` | `/substation/audit` | Audit log of supervisory commands |
| `GET` | `/substation/network-events` | Network event log |
| `POST` | `/substation/command/:device` | Send a command to a field device. Body: `{"command": "set_tap", "value": -16, "source": "operator"}` |

## Firewall

Direct operations against the containd NGFW.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/firewall/health` | containd health status |
| `GET` | `/firewall/rules` | Currently-loaded rules |
| `GET` | `/firewall/sessions` | Active connection table |
| `GET` | `/firewall/active` | Which named configuration is currently applied |
| `GET` | `/firewall/compare` | Diff between weak and improved configs |
| `POST` | `/firewall/apply` | Apply a named configuration. Body: `{"config": "improved"}` |

## Traffic generation

Used by Exercise 0 (Baseline Traffic Analysis) to produce representative substation traffic during a capture window.

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/traffic/generate` | Start traffic generation. Body: `{"duration_sec": 50}`. Returns `{"status": "generating", "duration_sec"}` |
| `GET` | `/traffic/status` | Current traffic generation state. Returns `{"generating", "started_at", "flows_generated"}` |

## PCAP capture

Unified PCAP API. Uses the containd PCAP subsystem when available, falls back to `tcpdump` in the firewall container otherwise.

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/pcap/start` | Start a capture. Body: `{"duration_sec": 60, "name": "baseline"}`. Returns `{"status": "capturing", "file_prefix"}` |
| `POST` | `/pcap/stop` | Stop the current capture |
| `GET` | `/pcap/status` | Current capture state. Returns `{"capturing", "file_ready", "files"}` |
| `GET` | `/pcap/list` | List all capture files available on the firewall |
| `GET` | `/pcap/download` | Download the most recent capture |
| `GET` | `/pcap/download/:name` | Download a specific capture by filename |

## Nodes

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/nodes/:node_id/action` | Send a node action (restart, stop, etc.) — placeholder for orchestration hooks |

## Containd proxy

| Method | Path | Description |
|--------|------|-------------|
| `*` | `/containd/*path` | Transparent HTTP(S) proxy to the containd web UI at `http://firewall:8080/`. Used for same-origin iframe embedding |

## Terminals (WebSocket)

Terminal endpoints upgrade to a WebSocket and proxy between xterm.js clients and either a Docker `exec` session or an SSH session.

### Endpoints

- `GET /workshop/nodes/:nodeId/terminal` — Workshop node terminal
- `GET /labs/instances/:id/nodes/:nodeId/terminal` — Lab instance node terminal

### Message protocol

The WebSocket carries binary terminal data in both directions. Additionally, the client may send a JSON control message for PTY resize:

```json
{"type": "resize", "cols": 120, "rows": 30}
```

The backend detects text-mode messages starting with `{` and parses them as control messages. Non-JSON text messages and all binary messages are written straight to the shell's stdin. Binary messages from the shell's stdout are forwarded to the client.

### Shell selection

For container-local nodes (Docker exec), the backend runs:

```
sh -c "command -v bash >/dev/null 2>&1 && exec bash -il || exec sh -i"
```

This prefers bash as an interactive login shell when available, falling back to sh on minimal images. The environment includes `TERM=xterm-256color`.

For the containd firewall (node type `containd_ngfw`), the backend opens an SSH session to `rangerdanger-firewall:2222` as user `containd`, requests a PTY, starts a shell, and immediately writes `exec bash -il; clear` to upgrade the session to an interactive login bash. This is a workaround for a limitation in the containd embedded SSH server tracked in [containd#17](https://github.com/tonylturner/containd/issues/17).

### Resize propagation

On resize messages:

- Docker exec sessions call `ContainerExecResize` to resize the PTY
- SSH sessions call `session.WindowChange(rows, cols)` to send an SSH window-change request

The frontend sends a resize on initial connect, whenever the container resizes, and whenever the terminal becomes visible again (detected via `IntersectionObserver` for tab switching).

## Data contracts

Struct-level documentation lives in `backend/internal/models/models.go`. Topology, scenario steps, and firewall configs are stored as JSON strings in the database and shaped to match what the frontend expects directly — React Flow graphs for topology, ordered step arrays for scenarios, and the containd policy schema for firewall configurations.
