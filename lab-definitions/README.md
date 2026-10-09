# Lab definitions

YAML configuration that drives the lab content: the network topology,
the firewall policy variants, and the exercises students walk
through. The backend reads these on startup; nothing here is built or
compiled. A broken package stops the backend from starting, and
`POST /api/admin/seed` returns the same error.

## Layout

| Path | Purpose |
|------|---------|
| `packages/<id>/package.yml` | A curriculum package: its topology, its scenarios directory and its capabilities |
| `packages/<id>/manifest.json` | Deployment identities and endpoints for package services, networks and UI |
| `packages/<id>/compose.source.yml` | Complete range model built from source |
| `packages/<id>/compose.release.yml` | Complete range model using published images |
| `packages/<id>/nginx.routes.conf` | Package-specific proxy locations included by the platform nginx config |
| `substation-segmentation.yml` | Lab topology of the US package - nodes, networks, IPs, container names |
| `scenarios/*.yml` | One file per exercise; numbered via the `order` field |
| `firewall/*.json` | Containd firewall policies (weak baseline + improved hardened state) |

## Packages

A package owns a topology and the scenarios students run on it. The
backend loads each `packages/*/package.yml` for its curriculum content;
deployment metadata lives in the adjacent `manifest.json` and Compose
files. The US workshop is `packages/us-dnp3-substation/package.yml`,
which points at the files in this directory where they have always lived:

```yaml
schema: 1
id: us-dnp3-substation            # slug; must match the directory name
title: "US distribution substation (DNP3)"
revision: 1                       # bump when step IDs or validator meaning change
topology: ../../substation-segmentation.yml   # relative to package.yml
scenarios: ../../scenarios                    # directory of <slug>.yml; the only scenario source
capabilities: [process.electrical, policy.containd, audit.device-control, capture.firewall]
```

- Every file (package, topology, scenario) is decoded strictly: an
  unknown field is a load error.
- `manifest.json` is the package's deployment contract. Its service keys,
  container names, topology-node IDs, network interfaces and endpoint URLs
  must agree with both complete Compose models. Compose remains the
  deployment authority.
- The root `docker-compose.yml` and `docker-compose.release.yml` own the
  persistent platform project (`rangerdanger-platform`: backend, frontend,
  proxy). A package's `compose.source.yml` or `compose.release.yml` owns the
  replaceable range project (`rangerdanger`). Run each model with the
  installation root as `RANGERDANGER_ROOT` and `--project-directory`;
  relative bind and build paths in the package files resolve from the
  installation root, not from the package directory.
- The platform owns `rangerdanger_mgmt_net`. Range Compose files reference it
  as an external network so switching a range does not remove the platform's
  management network. The other package networks belong to the range.
- Offline installation uses the release Compose files with `--pull never`
  after loading the staged images; there is no separate offline overlay.
- Paths inside a topology (`firewall_config`) resolve from the
  topology file's directory and must stay inside `lab-definitions/`.
- A topology must not list scenarios inline; `scenarios: []` is the
  only accepted value.
- Scenario IDs are slugs unique across **all** packages; a duplicate
  is a load error.
- Each package loads in its own transaction: scenarios whose file is
  gone are removed, and a package directory that disappears takes its
  scenarios with it.
- The backend serves one active package, chosen by the
  `RANGERDANGER_PACKAGE` environment variable (default
  `us-dnp3-substation`). Scenario routes answer for the active package
  only. `GET /api/packages` lists packages and marks the active one.
- Known capabilities: `process.electrical` (substation state and the
  device-command / check vocabulary for `command`, `sequence` and
  `check` actions), `policy.containd` (validators read the active
  firewall policy), `audit.device-control` (validators read the device
  audit log), `capture.firewall` (validators look for firewall
  captures).

## Adding an exercise

Create `<package scenarios dir>/<id>.yml` with the schema below. For
the US package that is `scenarios/<id>.yml`; it is exposed via
`GET /api/scenarios`.

```yaml
id: my-new-exercise         # slug; unique across every package
order: "2.5"                # quoted string; sorts lexicographically.
                            # Use the deck numbering (e.g. "1.2",
                            # "2.3-bonus"); the loader unmarshals
                            # this field as `string`.
name: "My New Exercise"
summary: "One-line summary that appears in the card view (≤120 chars)."
validator: us-my-new-exercise   # optional; a registered validator key (see below)
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
  - id: observe-normal-operations   # required slug, unique in the scenario.
                                    # Progress is stored by it: renaming it
                                    # resets students' progress for the step.
    title: "Observe normal operations"
    expected_config: weak   # see "expected_config values" below
    description: |
      Each step's description is Markdown. Code fences become
      auto-run buttons in the UI:

        mbpoll -a 1 -r 1 -c 4 -1 -t 1 10.40.40.20

      Use absolute IPs (no DNS) so commands are copy-pasteable into
      a student's terminal independent of any in-cluster name
      resolution.
  - id: trigger-the-attack
    title: "Trigger the attack"
    expected_config: weak
    description: |
      ...
```

### `expected_config` values

The loader accepts only `weak` and `hardened` (or no value). The
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
  the alias logic lives in `frontend/components/policy-status-banner.tsx`.
  Authors who add a new step with `expected_config: hardened` should
  rely on the frontend check, not on the backend's reply.)

## Validators

`GET /api/scenarios/:id/validate` runs the validator the scenario
declares with `validator: <key>`. A scenario without one has no
validation result (the route returns 404). Keys and their Go code live
in the registry in `backend/internal/server/scenario_validators.go`;
each entry also lists the package capabilities it needs. An unknown
key, or a key whose capabilities the package does not declare, is a
load error.

To add a validator, write a function in
`backend/internal/server/scenario_validate.go` following the existing
pattern, then register it:

```go
"us-my-new-exercise": {
    capabilities: []string{labs.CapabilityProcessElectrical, labs.CapabilityPolicyContaind},
    run: func(_ *Server, in validatorInput) []ValidationCheck {
        return validateMyNewExercise(in.state, in.activeConfig)
    },
},
```

Results surface in the exercise runner UI as ✓ / ✗ checks.

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

`substation-segmentation.yml` defines the US package's lab topology - the set of
nodes, which networks each one attaches to, and which container name
they map to in `packages/us-dnp3-substation/compose.source.yml` and
`packages/us-dnp3-substation/compose.release.yml`. The frontend's network console
(`/console`) is rendered from this file via the
`GET /api/workshop/graph` endpoint.

If you change network membership here, you must also change
both package Compose models to match - the YAML is the source of truth for
the UI but Docker is the source of truth for actual reachability. Update the
package manifest at the same time.

## Conventions

- Package, scenario and step IDs are slugs (lowercase letters and
  digits separated by single hyphens); the loader rejects anything
  else. Node IDs stay kebab-case too (`eng-ws-1`).
- Step descriptions favor inline code blocks the runner can auto-run,
  not block paragraphs of "type this then this then this."
- Process consequence is a first-class outcome - describe what
  happens to the breaker, the voltage, or the relay setpoints, not
  just what packets are sent.
