# Lab authoring guide

How to write a workshop lab in `lab-definitions/scenarios/`. Covers
the YAML shape the runner expects, the description-body fences for
hints / decisions / findings panels, and the localStorage model
that lets a lab read what the student did in earlier labs.

The runner that consumes these YAMLs lives in
[`frontend/components/scenario-runner.tsx`](../frontend/components/scenario-runner.tsx).
If something here disagrees with the runner, the runner wins -
file an issue or PR.

## File location and naming

Each lab is one YAML file under `lab-definitions/scenarios/`. The
filename matches the scenario `id` (e.g. `baseline-assessment.yml`
contains `id: baseline-assessment`).

Backend loads them at startup - restart `backend` after changes.

## Top-level shape

```yaml
id: my-new-lab               # required, kebab-case, must match filename stem
order: "1.5"                 # required, string. Sorts in the inventory; matches workshop deck numbering.
name: "Human-readable name"  # required
summary: "One-line summary." # shown in the lab list
description: |               # multi-paragraph. Shown above the steps.
  Longer prose that introduces the lab. Markdown allowed.
nodes:                       # optional. Hint to the UI which containers
  - fw-1                     # are involved (drives the in-app terminal
  - kali-1                   # tabs and node UI shortcuts).
tags:                        # optional. Free-form labels.
  - segmentation
  - defense
steps:
  - title: "Step 1 - Do the thing"
    expected_config: weak    # optional. weak | improved | hardened
    node: fw-1               # optional. Pins which container the Run button targets.
    description: |
      ...
  - title: "Step 2 - ..."
    description: |
      ...
```

Required fields: `id`, `order`, `name`, `description`, `steps`.
Each step requires `title` and `description`.

### `expected_config`

Sets the policy state the step assumes. Values:

| Value | Meaning |
|---|---|
| `weak` | The weak-baseline `lab-definitions/firewall/substation-weak.json` is applied. Used during assessment / before-state phases. |
| `improved` (alias `hardened`) | The hardened policy `substation-improved.json` is applied. Used after-state and validation phases. |

If a student's running policy doesn't match `expected_config`, the
runner shows a config-mismatch banner with a one-click reset.

Steps that don't depend on a specific policy state can omit it.

### `node`

Pins the Run button to a specific container. Useful when the
description text doesn't make the source obvious (e.g. all
commands run from the firewall but the prose talks about other
hosts). The UI also infers a node from prose keywords if `node`
isn't set; explicit is more reliable.

Valid values are node IDs from
[`lab-definitions/substation-segmentation.yml`](../lab-definitions/substation-segmentation.yml):
`fw-1`, `kali-1`, `vendor-jump-1`, `eng-ws-1`, `corp-ws-1`,
`hmi-1`, `rtac-1`, `openplc-1`, `historian-1`, `gps-1`, `relay-1`,
`recloser-1`, `regulator-1`, `capbank-1`.

## Description body - fences and special blocks

The step `description` is markdown with several runner-specific
extensions. They're parsed in `splitDescription()` and rendered
as React components.

### Command blocks

Any indented line that begins with one of these tools becomes a
**command block** with copy + Run buttons:

`nmap`, `mbpoll`, `dnp3poll`, `dnp3cmd`, `curl`, `tshark`,
`tcpdump`, `nc`, `telnet`, `ssh`, `wget`, `ls`, `grep`, `cat`,
`docker`.

Continuation lines with trailing backslash get joined automatically.

```yaml
description: |
  Run a Modbus poll to confirm the path:

      mbpoll -m tcp -a 1 -r 1 -c 5 -1 -t 1 10.40.40.20

  And check the API:

      curl -s http://10.30.30.20:8080/api/state \
        | python3 -m json.tool
```

The Run button targets the step's `node`, or falls back to prose
inference (look for "from the kali terminal", "on the firewall", etc.).
For multi-source steps, use **per-section "From X terminal" headers**
above each command - the runner picks the nearest one walking up.

The lab-commands smoke test (`./scripts/lab-commands-smoke.sh`)
extracts these the same way and runs each. If a command in your
lab YAML doesn't run cleanly, CI fails.

### Hints - `:::hint`

Collapsible "reveal answer" panel. Default state is collapsed.

```yaml
:::hint Reveal expected answer
The hardened substation lands at:

1. Enterprise → Field on Modbus/DNP3: BLOCK
2. ...
:::
```

Hint bodies support all the same fences (commands, decisions,
findings panels), so a hint can demonstrate the right answer with
runnable commands inside it.

### Decisions - `:::decision`

Inline question with a dropdown that the student commits to. The
choice persists to `localStorage` so refreshes don't lose it AND
so later labs can read it.

```yaml
:::decision id=enterprise-to-field options=BLOCK,RESTRICT,ALLOW correct=BLOCK
**Enterprise → Field** on Modbus 502 / DNP3 20000 - what's your design verdict?
:::
```

Attributes:

| Attribute | Required | Purpose |
|---|---|---|
| `id` | yes | Unique within the scenario. Forms the storage key as `decision:<scenario-id>:<id>`. |
| `options` | no | Comma-separated list of dropdown choices. Default: `BLOCK,BLOCK and LOG,RESTRICT,ALLOW`. Quote values that contain spaces (e.g. `options="BLOCK,BLOCK and LOG,RESTRICT,ALLOW"`). |
| `default-from` | no | `<source-scenario-id>:<source-decision-id>`. On first render with no local value, copies the upstream value so a downstream lab can pre-fill from an earlier one. The student can still adjust. |
| `correct` | no | The "right" answer. When set, the dropdown renders a green ✓ chip when the student's pick matches, red ✗ otherwise. Use this **only on observation / factual prompts** (e.g. "what did you see in the capture?"), not on judgment-call design decisions. |

Body is markdown - short prose, the dropdown renders below.

The dropdown is dark-themed and uses `colorScheme: dark` so the OS
picker matches the lab UI on supporting browsers.

### Findings panels - `:::findings-panel`

Read-only panel that surfaces upstream decisions as context. Used
when a downstream lab needs to show the student's earlier work
without re-prompting them.

```yaml
:::findings-panel from=baseline-assessment title="Findings from your assessment (Lab 1.2)"
enterprise-to-field: Enterprise → Field on Modbus/DNP3
enterprise-to-ot: Enterprise → OT Operations on ICS ports
vendor-to-ot: Vendor DMZ → OT Operations on Modbus/HMI control
non-rtac-to-field: Non-RTAC OT hosts → Field devices
unauth-modbus-writes: Unauthorized Modbus writes / DNP3 Direct Operate
:::
```

Attributes:

| Attribute | Required | Purpose |
|---|---|---|
| `from` | yes | Upstream scenario id. The panel reads each item from `decision:<from>:<line-id>`. |
| `title` | no | Header text. Defaults to `"Inherited findings"`. |

Body lines: one per item, format `<decision-id>: <human label>`.
The component reads each id's localStorage value and shows a
read-only summary. If the upstream lab wasn't done, the panel
shows a "you haven't done lab X yet" link.

Multiple panels in one step are fine - Lab 1.3 step 1 shows both
"Passive capture findings" and "Active probe result" stacked.

### Plan coverage - `:::plan-coverage`

Renders the same per-requirement coverage view that Lab 1.4's
DecisionPanel shows in its sticky-bottom bar, but inline at any
point in a description. Reads the saved remediation plan from
`localStorage` and computes coverage against the Lab 1.3 design
verdicts. Each requirement appears as **fully addressed**,
**partial**, **deferred**, or **n/a**.

```yaml
:::plan-coverage title="Coverage of your committed Lab 1.3 design"
:::
```

Body is ignored - the panel reads from `localStorage`, no
author input needed. Useful in summary / reflection steps where
the lab wants to surface "what your plan closed vs deferred"
without making the student manually recall their selections.

| Attribute | Required | Purpose |
|---|---|---|
| `title` | no | Panel header. Defaults to `"Your plan coverage"`. |

If the student hasn't completed Lab 1.4, the panel shows a
"visit Lab 1.4 to select actions" link instead of an empty
table.

## Step-level `action` field

Some steps have a structured action that renders a custom UI
component instead of (or in addition to) the description prose.

```yaml
- title: "Choose your remediation plan"
  description: ...
  action:
    type: decision
    budget_hours: 500
    roles:
      - { name: OT Engineer, capacity_hours: 120 }
      - { name: Firewall Admin, capacity_hours: 140 }
    actions:
      - id: block-enterprise-to-field
        title: "Block enterprise → field direct access"
        why: |
          Closes the DNP3 command injection attack surface.
        effort_hours: 40
        roles: [Firewall Admin, Security Analyst]
        tags: [enterprise-field]
      ...
```

Supported `type` values:

- `decision` - labor-budgeted action picker (Lab 1.4 pattern). Renders the [`DecisionPanel`](../frontend/components/decision-panel.tsx) component with budget meters, per-role utilization, and per-action requirement / readiness overlays.
- `check` - a state-check action with `expect: {key: value}` against the substation API. Used to confirm "the lab is in known-good state" between phases.
- `command`, `firewall`, `sequence`, `manual` - older patterns for auto-running specific commands. Most labs now prefer command blocks in the description body for the copy/Run affordance. New labs should default to description blocks.

`DecisionPanel` automatically reads:

- 1.3 design verdicts from `decision:segmentation-requirements:*` and renders a "Your design requirements" panel + per-action requirement badges + a coverage summary.
- 1.3 readiness verdicts from `decision:segmentation-requirements:{ot-eng-availability,fw-admin-skills,vendor-change-window,risk-acceptance-authority}` and renders a "Your team readiness" panel + per-action readiness overlays.

If you wire a different kind of action picker, see
[`frontend/components/decision-panel.tsx`](../frontend/components/decision-panel.tsx)
for the integration pattern.

## localStorage model

Decisions and the remediation plan persist client-side. Keys:

```
decision:<scenario-id>:<decision-id>     decision dropdown selections
remediationPlan                          current plan (action ids + saved-at)
```

The runner doesn't expose these as authoring primitives - you read
them indirectly via `:::findings-panel` and the `default-from`
attribute on `:::decision`. If you need a brand-new way to surface
saved data, add a parser case to `splitDescription` and a renderer
component, then document it here.

## Authoring checklist

Before opening a PR with a new or modified lab:

1. **Bring the stack up** and click through the lab end-to-end. The Run buttons should fire from the right container under the right policy.
2. **Run the lab-commands smoke** - catches typo'd IPs, missing tools, wrong source containers, broken policies. Same gate CI runs.
   ```sh
   ./scripts/lab-commands-smoke.sh <your-scenario-id>
   ```
3. **Run the firewall traffic smoke** if you changed policy state expectations or rule shape.
   ```sh
   ./scripts/firewall-smoke.sh
   ```
4. **Update `lab-definitions/substation-segmentation.yml`** if you added a new container the lab needs.
5. **Update `EXERCISE_NODE_MAP`** in [`frontend/lib/exercise-nodes.ts`](../frontend/lib/exercise-nodes.ts) so the in-app terminal tabs and node UI shortcuts surface the right hosts.
6. **Update `CHANGELOG.md`** under `[Unreleased]` for any user-visible change.

## Patterns that work well

- **Active engagement per step.** Pure reading steps lose students fast. Even a single `:::decision` or a Run button makes the step feel like work.
- **Inheritance over re-input.** When Lab N+1 needs Lab N's verdicts, use `:::findings-panel` plus `default-from` on the new decisions instead of asking the student to re-fill the same dropdowns.
- **`correct=` only on observations, not on judgment.** Quiz-style chips on factual prompts ("did you see X in the capture?") teach. Quiz chips on design choices ("what would you BLOCK?") feel like the workshop is policing the student's design.
- **Pair passive analysis with active probing.** Capture says "I don't see traffic on this path." Active probe says "but the path's open." That gap is the teaching moment - see Lab 1.2 step 7 for the pattern.

## Patterns to avoid

- **Hardcoded "you documented X" lists** that don't match what the student actually captured. If the student fills in dropdowns in Lab N, downstream labs should `findings-panel` from those keys, not re-state expected answers in prose.
- **Mixing decision-language and observation-language in the same step.** Be explicit which one a step is about. Lab 1.2 = observe, Lab 1.3 = decide design, Lab 1.4 = decide implementation.
- **Long-running tcpdump / tshark in capture mode** as the only command in a step. The Run button fires a one-shot exec; capture loops just sit until the student kills them. Use bounded captures (`-c N`, `-G secs -W 1`) or split the capture into a separate background-managed action.

## Smoke gate cheat sheet

| Failure | Likely cause |
|---|---|
| Lab-commands smoke says `TOOL NOT INSTALLED` | The container the lab targets doesn't have that tool. Add the tool to the container's Dockerfile, or pick a different source via per-section "From X terminal" header. |
| Lab-commands smoke says `DOCKER EXEC FAILED` | Container isn't running, or `node:` points at the wrong topology id. |
| Firewall smoke says `expect=allow actual=deny` (3+s timeout) | The policy doesn't allow the flow you're claiming. Double-check `expected_config` and the relevant `lab-definitions/firewall/substation-*.json`. |
| Firewall smoke says `expect=deny actual=allow` (sub-second) | The policy lets the flow through. Either the rule is too permissive or your test expectation is wrong. |
