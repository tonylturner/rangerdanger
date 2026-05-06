# Lab definitions

YAML configuration that drives the lab content: the network topology,
the firewall policy variants, and the exercises students walk
through. The backend reads these on startup; nothing here is built or
compiled.

## Layout

| Path | Purpose |
|------|---------|
| `substation-segmentation.yml` | Active lab topology — nodes, networks, IPs, container names |
| `scenarios/*.yml` | One file per exercise; numbered via the `order` field |
| `firewall/*.json` | Containd firewall policies (weak baseline + improved hardened state) |

## Adding an exercise

Create `scenarios/<id>.yml` with the schema below. The backend's
loader (`backend/internal/labs/loader.go`) auto-discovers every
`*.yml` in this directory and exposes them via
`GET /api/scenarios`.

```yaml
id: my-new-exercise         # url-safe slug; must be unique
order: 7                    # display order in the exercise list
name: "My New Exercise"
summary: "One-line summary that appears in the card view (≤120 chars)."
description: |
  Long-form prose. Rendered as Markdown in the exercise runner.
  Explain the scenario, what the student is doing, and why it
  matters operationally.
nodes:
  - kali-1                  # node IDs from substation-segmentation.yml;
  - eng-ws-1                # only those listed get a terminal in the runner
tags:
  - modbus                  # filterable in the UI
  - field-device
  - segmentation
steps:
  - title: "Observe normal operations"
    expected_config: weak   # or 'improved'; the runner can flip this
    description: |
      Each step's description is Markdown. Code fences become
      auto-run buttons in the UI:

        mbpoll -m tcp -a 1 -r 1 -c 4 -1 -t 1 10.40.40.20

      Use absolute IPs (no DNS) so commands are copy-pasteable into
      a student's terminal independent of any in-cluster name
      resolution.
  - title: "Trigger the attack"
    expected_config: weak
    description: |
      ...
```

## Validators

If your exercise has measurable success criteria (firewall state,
PCAP contents, substation telemetry), add a validator in
`backend/internal/server/scenario_validate.go`:

```go
"my-new-exercise": func(s *Server, c *gin.Context) (*ScenarioValidationResult, error) {
    // ...inspect state, return pass/fail with detail strings
}
```

Validators are invoked from
`POST /api/scenarios/:id/validate` and surface in the exercise
runner UI as ✓ / ✗ on each step.

## Firewall configs

`firewall/substation-weak.json` is the **intentionally permissive
baseline** that students start from — enterprise→field is allowed,
which is what makes most attack exercises possible.

`firewall/substation-improved.json` is the **target hardened state**
— only RTAC→field is allowed for the controlled flows, and Modbus
write function codes are denied from any source other than the
RTAC. Exercises validate against this when checking remediation.

Both files use the [containd](https://github.com/tonylturner/containd)
schema. Tested by `backend/internal/containd/firewall_config_test.go`.

## Topology

`substation-segmentation.yml` defines the lab topology — the set of
nodes, which networks each one attaches to, and which container name
they map to in `docker-compose.yml`. The frontend's network console
(`/console`) is rendered from this file via the
`GET /api/workshop/graph` endpoint.

If you change network membership here, you must also change
`docker-compose.yml` to match — the YAML is the source of truth for
the UI but Docker is the source of truth for actual reachability.
The two should never disagree.

## Conventions

- Every scenario `id` and node ID stays kebab-case
  (`my-new-exercise`, `eng-ws-1`).
- Step descriptions favor inline code blocks the runner can auto-run,
  not block paragraphs of "type this then this then this."
- Process consequence is a first-class outcome — describe what
  happens to the breaker, the voltage, or the relay setpoints, not
  just what packets are sent.
