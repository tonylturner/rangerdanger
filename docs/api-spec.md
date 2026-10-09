# API Specification

Base URL: `/api` (when accessed through the nginx proxy at `http://localhost:8088/api`)

All endpoints return JSON unless otherwise noted. Most request and response bodies use `snake_case` field names. Exceptions include `GET /pcap/list`, which returns `sizeBytes` and `createdAt`, and PCAP downloads, which return binary data.

**Range-bound routes.** Routes that reach the range (its containers, its firewall or its services) are served only while a range generation serves: phase `ready`, or `preflight` while the previous range is still untouched (see [Range](#range)). Otherwise they answer `503 {"error":"range not ready","phase":"<phase>"}`. When a switch starts stopping the range, in-flight range-bound requests, terminal sessions and SSE streams are cancelled and the switch waits up to 15 s for them to return; a request that loses its range mid-flight may answer `503 {"error":"range is stopping"}`. The range-bound routes are: all of `/firewall/*`, `/substation/*`, `/pcap/*`, `/traffic/*`, `/workshop/*` and `/containd/*`; `GET /scenarios/:id/validate` and `POST /scenarios/:id/steps/:stepIdx/execute`. Everything else is served whatever the range is doing.

**Identities.** Range-bound routes take every container, address and endpoint of the range from the serving package's manifest: nodes (`:nodeId`, probe sources) resolve by topology node, the firewall and RTAC by role and named endpoint. A node the manifest does not know answers `404`. The canned policies (`/firewall/apply`, `/firewall/compare`), workshop reset, test suite, validation report and traffic generation follow the package's workshop recipe; only `us-dnp3-substation` has one, and other packages answer `404 {"error":"package <id> has no workshop recipe"}`.

## Health and build info

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Liveness probe. Returns `{"status": "ok"}` |
| `GET` | `/build` | Build metadata for this binary. Returns `{"version", "commit", "date"}`. Values are injected at build time via ldflags (see `backend/internal/version`); for an unstamped local build they're `"dev"` / `"none"` / `"unknown"`. Note: this is **not** at `/version` because the nginx proxy reserves `/api/version` for FUXA's own HMI version endpoint. |

## Admin

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/admin/seed` | Reload every package from `lab-definitions/`: the package list, each package's topology and default policy (used from the next range start; the serving range keeps the package it started with), and the scenario rows in the database. The active package (see `GET /packages`) must still exist. A load error answers `500` and keeps the previous packages. |

## Range

The range is the one Compose project (`rangerdanger`) that runs the active package's containers; the platform (backend, frontend, proxy) runs separately and keeps serving while the range switches. The backend never starts a range on its own: setup, or the UI, asks for one with `POST /range`.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/range` | Lifecycle status, `200` with the status body below. |
| `POST` | `/range` | Start a switch to a package. Body `{"package":"<id>"}`; the body or the field may be omitted, which restarts the active package. Returns `202` with the status body (phase `preflight`). `409 {"error","phase"}` while a switch or the startup reconcile runs; `400 {"error"}` for an unknown package or a malformed body (unknown fields are rejected). Poll `GET /range` until `ready` or `failed`. |
| `GET` | `/packages` | Curriculum packages: `[{"id","title","revision","active"}]`. `active` marks the range's recorded package, or `RANGERDANGER_PACKAGE` before any range was started. Scenario, workshop and seed routes serve the same package. |

Status body:

```json
{"generation": 3, "phase": "ready", "package": "us-dnp3-substation", "target": "",
 "mode": "release", "error": "", "updated_at": "2026-10-09T14:30:00Z"}
```

| Field | Meaning |
|-------|---------|
| `generation` | Increments with every switch that gets past preflight. Range-bound work belongs to one generation; results that land after it stopped are dropped. |
| `phase` | `none` (no range was ever started), `preflight`, `stopping`, `starting`, `configuring`, `ready`, `failed`. |
| `package` | The recorded package. From `stopping` on it is the new package; in `preflight` it is still the old one. Empty in `none`. |
| `target` | The requested package, only in `preflight`; otherwise empty. |
| `mode` | `source` or `release`: which Compose file of the package runs (`RANGERDANGER_MODE`). |
| `error` | Why the last switch failed. A failed preflight leaves the phase where it was (for example `ready`, with the old range still serving) and sets `error`; a failure from `stopping` on removes the attempted range and sets phase `failed`. The previous package is not restored automatically; retry or switch. |
| `updated_at` | When the status last changed; `null` in phase `none`. |

A switch runs these phases, one switch at a time:

1. `preflight` (old range untouched and still served): the package exists, its manifest loads, `docker compose config` succeeds for the mode's Compose file, every image it needs is already present locally (nothing is pulled or built), and its proxy routes and default firewall policy exist.
2. `stopping`: in-flight range-bound work drains, then the old range is removed by its Compose project label together with its anonymous volumes (`down -v`; range models declare no named volumes), and the removal is verified: no container or network with the label, and none of the volumes its containers mounted, remains.
3. `starting`: `compose up --wait` with no build and no pull (healthchecks have 300 s).
4. `configuring`: the package's proxy routes are installed (`nginx -t`, then reload; a rejected file is rolled back), the firewall is waited for, and the package's default policy is imported. Every range start imports it, so a student's policy does not survive a switch.
5. `ready`.

On backend start, a range recorded as `ready` is adopted when every container of its manifest exists and the proxy runs its routes (no restart, no policy import); otherwise the phase becomes `failed` with `range missing: ...`. A switch that was cut short (`stopping`, `starting`, `configuring`) is torn down and recorded as `failed` with `interrupted during <phase>`.

## Workshop

The workshop endpoints operate on the serving range, with nodes from the topology of the package that range started with, and are what the exercise runner uses.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/workshop/graph` | Current topology as a graph (nodes + edges); zones and per-network `interface_ips` come from the manifest |
| `GET` | `/workshop/status` | Status of all workshop nodes |
| `GET` | `/workshop/nodes/:nodeId/terminal` | WebSocket terminal to a node (see Terminals) |
| `POST` | `/workshop/nodes/:nodeId/exec` | Run a one-shot command on a node. Body: `{"command": "...", "timeout_sec": 30}`. The first token of `command` must be in the backend allowlist: `nmap, mbpoll, dnp3poll, dnp3cmd, curl, tshark, tcpdump, nc, ping, traceroute, wget, cat, ls, ip, ss, netstat` - anything else returns `403 {"error":"command not allowed"}`. On success returns `{"stdout", "stderr", "exit_code", "duration_ms"}`. |
| `POST` | `/workshop/reset` | Reset substation state (clear lockouts, restore voltage, re-enable reclose, etc.) |
| `POST` | `/workshop/test-suite` | Run all exercise validators programmatically |

## Scenarios (exercises)

Exercises are stored internally as "scenarios" for historical reasons - the user-facing term is "exercise".

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/scenarios` | List the active package's exercises |
| `GET` | `/scenarios/:id` | Get exercise detail with steps |
| `POST` | `/scenarios/:id/steps/:stepIdx/execute` | Execute an automated action for a single step (e.g., inject_fault, apply firewall config) |
| `GET` | `/scenarios/:id/validate` | Run all validators for the exercise. Returns `{"scenario_id", "outcome", "checks": [...], "timestamp"}` |

### Validation response

```json
{
  "scenario_id": "baseline-assessment",
  "outcome": "PASS",
  "checks": [
    {
      "name": "PCAP captured",
      "status": "pass",
      "detail": "Baseline capture file found - traffic was recorded"
    }
  ],
  "timestamp": "2026-04-10T02:41:09Z"
}
```

Check status is one of `pass`, `fail`, or `warn`. Outcome is `PASS` if every check is `pass`, `FAIL` if any check is `fail`, and `PARTIAL` if at least one check is `warn` and none are `fail`.

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
| `POST` | `/substation/lab-control` | Proxy a Load Simulator override (`active`, `general_load_pct`, `critical_load_pct`, `power_factor`) and optional audit entry (`audit_command`, `audit_target`, `audit_detail`, `source`) to RTAC `/api/lab-control`. |

## Firewall

Direct operations against the containd NGFW.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/firewall/health` | containd management health, as containd sends it: `{"status":"ok","component","build","time"}`. |
| `GET` | `/firewall/rules` | Currently-loaded rules |
| `GET` | `/firewall/flows` | containd engine flow table (proxied from containd `GET /api/v1/flows`). Optional `?limit=` 1..5000, default 200; anything else is `400`. Returns `{"flows":[{"flowId","firstSeen","lastSeen","srcIp","dstIp","srcPort","dstPort","transport","application","eventCount","avDetected","avBlocked"}]}`; containd omits empty `srcIp`/`dstIp`/ports/`transport`/`application` and false `av*` fields. `503` with `{"flows":[],"error":"..."}` when containd is unreachable or errors. |
| `GET` | `/firewall/active` | Which named configuration is currently applied. Returns `{"active_config":"weak"\|"improved"\|"custom", "policy_source":"weak"\|"hardened-reference"\|"plan-custom"\|"manual-custom"\|""}`. `policy_source` distinguishes a button-applied policy (`"plan-custom"` = "Apply Your Plan" from Lab 1.4 picks) from a manually-committed one (`"manual-custom"` is set by the backend's policy observer, which runs with each range generation, when it detects a policy committed directly in containd). A new range starts with `active_config` from the package's default policy and an empty `policy_source`. |
| `GET` | `/firewall/compare` | Diff between weak and improved configs |
| `POST` | `/firewall/apply` | Apply a named configuration. Body: `{"config": "improved"}` (or `"weak"`). Returns `{"status":"applied","active_config":"weak"\|"improved"}` and includes `"warnings": [...]` when containd returned any. Sets server-side `policy_source` to `"weak"` (for weak) or `"hardened-reference"` (for improved). |
| `POST` | `/firewall/apply-custom` | Apply a raw JSON config produced by the student during Lab 1.4 (Remediation Planning) or Lab 2.2 (Firewall Implementation). Body is the full containd policy JSON (max 512 KB). The backend validates structure, posts to containd's `candidate → commit` flow, and on success returns `{"status":"applied","active_config":"custom","policy_source":"plan-custom"}` plus `"warnings": [...]` when containd returned any. The `policy_source` tag lets the UI label the active policy as derived from the student's Lab 1.4 plan rather than a manual containd commit. |
| `POST` | `/firewall/validation-report` | No request body. Runs the segmentation validation matrix against the currently active policy and captures a short PCAP without applying a policy. Returns the Markdown report and summary (`authorized_pass`, `authorized_total`, `unauthorized_pass`, `unauthorized_total`, `skipped`, `result`), plus `active_config`, `policy_source`, and `pcap_path`. |

## Traffic generation

Used by Lab 1.2 (Baseline Traffic Analysis) to produce representative substation traffic during a capture window.

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/traffic/generate` | Start traffic generation. Body: `{"duration_sec": 50}` (defaults to `30` when omitted or `<= 0`). Returns `{"status": "generating", "duration_sec"}` |
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

## Containd proxy

| Method | Path | Description |
|--------|------|-------------|
| `*` | `/containd/*path` | Transparent HTTP(S) proxy to the containd web UI at the range manifest's firewall `api` endpoint (`http://10.99.99.2:8080/` for the US package). Used for same-origin iframe embedding |

## Terminals (WebSocket)

Terminal endpoints upgrade to a WebSocket and proxy between xterm.js clients and a Docker `exec` session. SSH-based terminals were removed; every node terminal - including the firewall - now goes through Docker exec.

### Endpoints

- `GET /workshop/nodes/:nodeId/terminal` - Workshop node terminal

### Message protocol

The WebSocket carries binary terminal data in both directions. Additionally, the client may send a JSON control message for PTY resize:

```json
{"type": "resize", "cols": 120, "rows": 30}
```

The backend detects text-mode messages starting with `{` and parses them as control messages. Non-JSON text messages and all binary messages are written straight to the shell's stdin. Binary messages from the shell's stdout are forwarded to the client.

### Shell selection

Most nodes get a generic bash/sh selector:

```
sh -c "command -v bash >/dev/null 2>&1 && exec bash -il || exec sh -i"
```

This prefers bash as an interactive login shell when available, falling
back to sh on minimal images. The environment includes
`TERM=xterm-256color`.

**The firewall node is different.** Its terminal first lands in the
containd appliance CLI (matching what `ssh containd@localhost -p 2222`
gives you), and only drops into bash after the CLI exits:

```
sh -c "containd cli; exec bash -il || exec sh -i"
```

So when you open the `fw-1` terminal you'll see the `containd# `
prompt first. Type `shell` (or `exit`) to drop into the underlying
Linux bash (containd runs `CONTAIND_SSH_SHELL_MODE=linux`, so the
bash drop-in carries the same tools as the SSH path). The host port
mapping `127.0.0.1:2222:2222` still exposes the SSH path for users
who want it (`ssh -p 2222 containd@localhost`, password `containd`).

### Resize propagation

The backend calls `ContainerExecResize` on the Docker exec session for every resize message. The frontend sends a resize on initial connect, whenever the container resizes, and whenever the terminal becomes visible again (detected via `IntersectionObserver` for tab switching).

## Data contracts

Struct-level documentation lives in `backend/internal/models/models.go`. Topology, scenario steps, and firewall configs are stored as JSON strings in the database and shaped to match what the frontend expects directly - React Flow graphs for topology, ordered step arrays for scenarios, and the containd policy schema for firewall configurations.
