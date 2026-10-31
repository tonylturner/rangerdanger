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

Both packages bind `curriculum: {dir: curriculum}` through `package.yml`.
The binding's `curriculum.roles` names the seven exercises, and the
curriculum directory owns the package-level budget, teams, action catalog, requirements and
readiness mappings. In particular, `eu-segmentation-requirements.yml`
holds the design/readiness prompt fences while
`eu-remediation-planning.yml` has a single `action.type: decision` step;
neither embeds a second labor/action catalog. The package binding is
defined by [`backend/internal/labs/package.go`](../backend/internal/labs/package.go#L68-L78),
and the active package's curriculum is included by
[`backend/internal/server/packages.go`](../backend/internal/server/packages.go#L14-L26).

- `packages/eu-iec104-substation/curriculum/` is bound by
  `packages/eu-iec104-substation/package.yml`; the binding's `roles` map design,
  planning, implementation and validator roles to their served EU
  exercises. The loader validates required roles and decision options
  against the requirements exercise
  ([`curriculum_load.go`](../backend/internal/labs/curriculum_load.go#L131-L139),
  [`curriculum_load.go`](../backend/internal/labs/curriculum_load.go#L171-L191)).

The EU package serves these seven scenario files:

| Exercise | Scenario ID | Validator |
|---|---|---|
| Baseline traffic analysis | `eu-baseline-assessment` | `eu-baseline-assessment` |
| Segmentation requirements | `eu-segmentation-requirements` | — |
| Remediation planning | `eu-remediation-planning` | — |
| Firewall policy implementation | `eu-firewall-implementation` | `eu-firewall-implementation` |
| Protocol-hardened configurations | `eu-hardening-configurations` | `eu-hardening-configurations` |
| Vendor remote-access compromise | `eu-vendor-rdp-compromise` | `eu-vendor-rdp-compromise` |
| Testing and validation evidence | `eu-validation-evidence` | `eu-validation-evidence` |

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
- Deployment manifests use schema **2**: `platform` means platform ownership,
  not management identity. Exactly one platform network is management
  (`rangerdanger_mgmt_net`); backend endpoints must use that service's own
  management IP. `delivery: true` marks the fixed platform-owned desktop
  bridges. They have no zone and their nodes have no endpoints or management
  leg. Each permits only its designated desktop plus the platform proxy.
- The platform owns `rangerdanger_mgmt_net` and `rangerdanger_desk_corp`,
  `rangerdanger_desk_vendor`, `rangerdanger_desk_eng`. Range Compose files
  reference the ones they use as external; switches never remove them.
  All other package networks belong to the range. Delivery bridges are
  internal, use `10.98.1.0/28`, `10.98.2.0/28`, `10.98.3.0/28` respectively,
  with gateway `.1`, proxy `.2`, desktop `.3`. They are not teaching zones.
  nginx denies delivery source `10.98.0.0/16` for every location.
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
  Plain text: the runner shows it as one paragraph, so put Markdown,
  links and hints in step descriptions.
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

The loader accepts a declared package policy name (such as `weak` or
`improved`), or no value. It also accepts `hardened` as an alias only
when the package declares `improved`. The runtime semantics are:

- `/api/firewall/apply` accepts a declared package policy name.
  "Apply Hardened" sends `{"config":"improved"}`. "Apply Your Plan"
  instead sends `{packageId, revision, actionIds}` to
  `/api/firewall/apply-plan`; the backend compiles the policy and records
  `active_config: custom`, `policy_source: plan` and plan provenance.
  A divergent policy committed directly in containd is identified by the
  observer as `custom` / `manual-custom`, without plan provenance.
- The frontend policy banner imports `matchesExpected` from
  `frontend/lib/policy-wording.ts`: both `hardened` and `improved`
  match either `improved` or `custom`. Other names require an exact
  match. This is a display/precondition check, not proof that an arbitrary
  custom policy meets Hardened acceptance.
- The backend test runner also treats `expected_config: hardened` as
  satisfied by `improved` or `custom`; otherwise it applies `improved`
  before running that step. This is separate from action-level checks:
  in `scenario_execute.go`, a US `check` action with
  `expect.firewall_config: improved` evaluates the running L4 rules
  against the applied plan (when its provenance holds), or Hardened
  otherwise. Other `expect.firewall_config` strings are compared
  literally with the active configuration; `hardened` is not an alias
  in that action-level field. Use validators and the validation report
  for policy evidence, not the banner's name match.

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
field access is allowed only from the RTAC's pinned L4 source address.
That source pin enforces the boundary; DPI detects and logs function
codes. Exercises validate against this when checking remediation.

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
