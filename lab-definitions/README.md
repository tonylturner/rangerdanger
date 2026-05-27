# Lab definitions

YAML configuration that drives the lab content: the network topology,
the firewall policy variants, and the exercises students walk
through. The backend reads these on startup; nothing here is built or
compiled.

## Layout

| Path | Purpose |
|------|---------|
| `substation-segmentation.yml` | Active lab topology - nodes, networks, IPs, container names |
| `scenarios/*.yml` | One file per exercise; numbered via the `order` field |
| `firewall/*.json` | Containd firewall policies (weak baseline + improved hardened state) |

## Adding an exercise

Create `scenarios/<id>.yml` with the schema below. The backend's
loader (`backend/internal/labs/loader.go`) auto-discovers every
`*.yml` in the `scenarios/` subdirectory and exposes them via
`GET /api/scenarios`. (Top-level `*.yml` in `lab-definitions/`
itself is the lab topology template, not a scenario.)

```yaml
id: my-new-exercise         # url-safe slug; must be unique
order: "2.5"                # quoted string; sorts lexicographically.
                            # Use the deck numbering (e.g. "1.2",
                            # "2.3-bonus"); the loader unmarshals
                            # this field as `string`.
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
    expected_config: weak   # see "expected_config values" below
    description: |
      Each step's description is Markdown. Code fences become
      auto-run buttons in the UI:

        mbpoll -a 1 -r 1 -c 4 -1 -t 1 10.40.40.20

      Use absolute IPs (no DNS) so commands are copy-pasteable into
      a student's terminal independent of any in-cluster name
      resolution.
  - title: "Trigger the attack"
    expected_config: weak
    description: |
      ...
```

### `expected_config` values

The string values used in step YAMLs are `weak` and `hardened`. The
runtime semantics are:

- The backend's `/api/firewall/apply` only accepts the literal
  strings `weak` or `improved`. Applying the canned hardened
  policy via the "Apply Hardened" button sends `improved`; applying
  a student's own policy (Lab 1.4 / 2.2) sends a custom JSON to
  `/api/firewall/apply-custom`, which leaves the backend's
  `activeConfig` reading `custom`.
- For step-level `expected_config: hardened`, the frontend
  validator treats `hardened` as a UI alias matching either
  `improved` or `custom`. (Heads-up: the backend's same-named
  check in `scenario_execute.go` does **not** carry this alias —
  the alias logic lives in `frontend/components/scenario-runner.tsx`.
  Authors who add a new step with `expected_config: hardened` should
  rely on the frontend check, not on the backend's reply.)

## Validators

If your exercise has measurable success criteria (firewall state,
PCAP contents, substation telemetry), add a validator in
`backend/internal/server/scenario_validate.go` as a package-level
function matching the existing pattern:

```go
// 1. Add a top-level function. Signature is fixed.
func validateMyNewExercise(state map[string]any, audit []map[string]any, activeConfig string) []ValidationCheck {
    // ...inspect state, return pass/fail/warn checks with detail strings.
    return []ValidationCheck{
        {Name: "..."), Status: "pass", Detail: "..."},
    }
}

// 2. Wire it into the dispatch switch in handleValidateScenario:
case "my-new-exercise":
    checks = validateMyNewExercise(state, audit, activeConfig)
```

Validators are invoked from `GET /api/scenarios/:id/validate` and
surface in the exercise runner UI as ✓ / ✗ on each step.

## Firewall configs

`firewall/substation-weak.json` is the **intentionally permissive
baseline** that students start from - enterprise→field is allowed,
which is what makes most attack exercises possible.

`firewall/substation-improved.json` is the **target hardened state**
- only RTAC→field is allowed for the controlled flows, and Modbus
write function codes are denied from any source other than the
RTAC. Exercises validate against this when checking remediation.

Both files use the [containd](https://github.com/tonylturner/containd)
schema. Tested by `backend/internal/containd/firewall_config_test.go`.

## Topology

`substation-segmentation.yml` defines the lab topology - the set of
nodes, which networks each one attaches to, and which container name
they map to in `docker-compose.yml`. The frontend's network console
(`/console`) is rendered from this file via the
`GET /api/workshop/graph` endpoint.

If you change network membership here, you must also change
`docker-compose.yml` to match - the YAML is the source of truth for
the UI but Docker is the source of truth for actual reachability.
The two should never disagree.

## Conventions

- Every scenario `id` and node ID stays kebab-case
  (`my-new-exercise`, `eng-ws-1`).
- Step descriptions favor inline code blocks the runner can auto-run,
  not block paragraphs of "type this then this then this."
- Process consequence is a first-class outcome - describe what
  happens to the breaker, the voltage, or the relay setpoints, not
  just what packets are sent.
