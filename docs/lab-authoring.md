# Lab authoring guide

How to write a workshop lab in a package's scenarios directory
(`lab-definitions/scenarios/` for the US package). Covers
the YAML shape the runner expects, the description-body fences for
hints / decisions / findings panels, and the browser storage model
that lets a lab read what the student did in earlier labs.

The runner that consumes these YAMLs lives in
[`frontend/components/scenario-runner.tsx`](../frontend/components/scenario-runner.tsx).
If something here disagrees with the runner, the runner wins -
file an issue or PR.

## File location and naming

Each lab is one YAML file in the scenarios directory its package
names (`lab-definitions/packages/<package>/package.yml`, key
`scenarios:`). For the US package that is `lab-definitions/scenarios/`.
By convention the filename matches the scenario `id` (e.g.
`baseline-assessment.yml` contains `id: baseline-assessment`).
Scenario IDs must be unique across every package. The package layout
is described in [`lab-definitions/README.md`](../lab-definitions/README.md#packages).

Backend loads them at startup - restart `backend` after changes. Any
error in any package (an unknown field, a bad step, an unknown
validator) stops the backend from starting, so check the backend log.

## Package-owned decision catalog

Inline decision prompts belong in a step's description as
`:::decision` fences. A labor-planning step does not carry its own
catalog, team capacities or budget. Mark that step with
`action: {type: decision}`; the panel reads the budget, teams and action
catalog from the active package's `curriculum/`.

The EU package binds that directory and its exercise roles in
`lab-definitions/packages/eu-iec104-substation/package.yml`; its
`curriculum/` owns the 500-hour budget, four teams and shared action
catalog for the requirements and planning exercises. The requirements
exercise supplies the design/readiness decision prompts, and the
planning exercise has one decision step. This keeps options and
catalog mappings package-scoped instead of duplicating catalogs in
individual scenario files. The binding is declared by
[`backend/internal/labs/package.go`](../backend/internal/labs/package.go#L68-L78),
loaded and validated against requirement prompts by
[`backend/internal/labs/curriculum_load.go`](../backend/internal/labs/curriculum_load.go#L17-L24)
and [`curriculum_load.go`](../backend/internal/labs/curriculum_load.go#L101-L110),
served on `GET /api/packages` by
[`backend/internal/server/packages.go`](../backend/internal/server/packages.go#L14-L26),
and consumed by the frontend
[`DecisionPanel`](../frontend/components/decision-panel.tsx#L31-L36).

### Curriculum sections

Both packages use `curriculum: {dir: curriculum}` in `package.yml`.
The directory must be an immediate sibling with a lowercase hyphenated name;
the former `file:` binding is not accepted. Keep exercise `roles`, `activities`
and `dynamicSteps` in the package binding. Author the remaining top-level keys
once each, in these seven single-purpose section files (each under 1000 lines):

| File | Content |
| --- | --- |
| `catalog.yml` | Schema, budget, team capacities and action catalog |
| `requirements.yml` | Requirement mappings and display order |
| `readiness.yml` | Readiness answers, flags and budget adjustments |
| `coverage.yml` | Impacts, groups, planning-only IDs and display orders |
| `implementation.yml` | Phase templates, test text and empty-plan guidance |
| `nodes.yml` | Exercise node mappings and inference |
| `plan.yml` | Policy compiler and proof model |

The loader merges disjoint top-level keys, then resolves topology references in
scalar values and strictly validates the complete model. Directory order does
not change list order or prose. Duplicate keys (including across files), unknown
fields, unknown filenames, symlinks, nested directories, empty directories or
sections, and extra YAML documents are errors. Each section is one nonempty YAML
mapping. Validate authoring changes with `go -C backend test -race -count=1
-timeout 300s ./internal/labs/...` and `go -C backend run ./cmd/packagelint -root ..`.

## Package knowledge

Every `package.yml` requires `knowledge: knowledge/index.yml`. The index is
strict YAML (JSON syntax is also valid YAML) with ordered `sections`:

```yaml
sections:
  - id: telecontrol
    heading: IEC104 Telecontrol
    description: Background for this package
    icon: radio
    accent: violet
    articles:
      - id: iec104-wire-model
        title: IEC104 frames and addresses
        file: iec104-wire-model.md
```

The Markdown file contains the body only, preserved byte-for-byte. Its path is
relative to the package's `knowledge/` directory. Files must be nonempty regular
files; absolute paths, `..`, backslashes and symlinks escaping the knowledge
subtree or package are rejected, including index escapes. No shared or platform
fallback corpus exists. Unknown YAML fields and duplicate section/article IDs
are errors. IDs are lowercase hyphenated slugs, unique within their own package.
Headings, article titles and article lists must not be empty. Optional icons are
`zap`, `shield`, `radio`, `wrench`, `layers`, `target`, `book`; optional accents are
`sky`, `emerald`, `violet`, `amber`, `slate`, `rose`.

Use inline `[article](#article-id)` links within bodies and
`[reading](/knowledge#article-id)` in scenario descriptions. Both forms are
validated against the owning package, not the union of every package's articles.
Keep stable article IDs for deep links. Knowledge bodies support `:::tip`,
`:::note`, `:::warning` and `:::caution` fences, with an optional title and
a closing `:::`. Scenario-only interactive fences are not knowledge widgets. Bump the package revision when publishing curriculum
changes, including knowledge, so reader state and cache identity stay scoped.
Restart or reseed the backend after editing: requests serve the validated catalog
snapshot, not arbitrary files. A failed load or reseed publishes no new content.

## Top-level shape

```yaml
id: my-new-lab               # required slug, unique across packages; by convention the filename stem
order: "1.5"                 # required, string. Sorts in the inventory; matches workshop deck numbering.
name: "Human-readable name"  # required
summary: "One-line summary." # shown in the lab list
validator: us-my-new-lab     # optional. Registered validator key; see lab-definitions/README.md
description: |               # multi-paragraph. Shown above the steps.
  Plain prose that introduces the lab. Rendered as one plain-text
  paragraph: no Markdown, no line breaks. Use step bodies for Markdown.
nodes:                       # optional. Hint to the UI which containers
  - fw-1                     # are involved (drives the in-app terminal
  - kali-1                   # tabs and node UI shortcuts).
tags:                        # optional. Free-form labels.
  - segmentation
  - defense
steps:
  - id: step-1-do-the-thing  # required slug, unique in this scenario
    title: "Step 1 - Do the thing"
    expected_config: weak    # optional. weak | hardened
    node: fw-1               # optional. Pins which container the Run button targets.
    description: |
      ...
  - id: step-2
    title: "Step 2 - ..."
    description: |
      ...
```

The loader requires `id`, `order` and `name` on the scenario, and `id`
and `title` on each step; write a `description` for both anyway.
Unknown fields are rejected.

### Step `id`

Each step's `id` is a slug (lowercase letters and digits separated by
single hyphens) unique within its scenario. Derive it once from the
title and then leave it alone: browser progress is stored by step ID,
so renaming one resets that step for students. Retitling a step does
not require a new ID. If a change alters what a step ID means, bump
the package `revision`.

### `expected_config`

Sets the policy state the step assumes. The loader accepts only
these values (`improved` is rejected):

| Value | Meaning |
|---|---|
| `weak` | The weak-baseline `lab-definitions/firewall/substation-weak.json` is applied. Used during assessment / before-state phases. |
| `hardened` | A hardened policy is active: the canned `substation-improved.json` (backend config `improved`) or the student's own (`custom`). Used in after-state and validation phases. |

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
`docker`, `iec104cmd`, `timeout`.

Continuation lines with trailing backslash get joined automatically.

```yaml
description: |
  Run a Modbus poll to confirm the path:

      mbpoll -m tcp -a 1 -r 1 -c 4 -1 -t 0 10.40.40.20

  And check the API:

      curl -s http://10.30.30.20:8080/api/state \
        | python3 -m json.tool
```

The Run button targets the nearest preceding source marker, then the step's
`node`, then prose inference, curriculum primary and the first exercise node.
Use **per-section "From the X terminal" headers** above commands. Markers
are case-insensitive `From`, `On` or `Via`, optional `the`, then a node ID
or short name (Kali, vendor-jump, eng-ws, RTAC, RTU, CC, firewall, historian,
OpenPLC or HMI). Markdown bold/backticks are allowed around the name.
Unrelated prose does not clear the marker; a later recognized marker replaces
it. Hints and track blocks inherit the surrounding source, but markers inside
them remain local. Dynamic plan descriptions follow the same rule.
Use **From the host terminal:** for host-side commands: these are copy-only,
with no Run button. Switch back with another source marker before container
commands. Avoid combining two source headers above two separate commands.

The US lab-commands smoke test (`./scripts/lab-commands-smoke.sh`)
also prefers source headers, but maintains a separate reviewed command
inventory. Check its extraction tests and expectations as well as frontend
routing tests when changing US recipes.

The gate keeps `pipefail`, but drains terminal `| head -N` pipelines with
`sed -n '1,Np'`: visible lines are unchanged and an early closed pipe cannot
masquerade as a reader failure. Missing/corrupt PCAPs and late reader failures
still fail. A reviewed passive filter may return no matches only after a
successful read; absence is `NOT_OBSERVED`, not enforcement proof.
Every Run-button `mbpoll` read must terminate on its own (`-1`); the gate
accepts only a zero exit with valid samples and no protocol/read errors.

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
| `id` | yes | Unique within the scenario. Forms the storage name `decision:<scenario-id>:<id>` (see [Browser storage model](#browser-storage-model)). |
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
| `from` | yes | Upstream scenario id. The panel reads each item from the storage name `decision:<from>:<line-id>`. |
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
- id: choose-your-remediation-plan
  title: "Choose your remediation plan"
  description: ...
  action:
    type: decision
```

Supported `type` values:

- `decision` - labor-budgeted action picker. This is only a marker on
  the planning step; it has no inline fields. The
  [`scenario-runner`](../frontend/components/scenario-runner.tsx#L614-L617)
  renders [`DecisionPanel`](../frontend/components/decision-panel.tsx#L31-L36),
  which reads `budgetHours`, `teams` and `catalog` from the active
  package curriculum. The catalog actions, effort, roles, requirements
  and readiness mappings belong in that package's `curriculum/`.
- `check` - a state-check action with `expect: {key: value}` against the substation API. Used to confirm "the lab is in known-good state" between phases.
- `probe` - TCP reachability check from `step.node` (or a per-target `from` topology node). Set `outcome: reachable|blocked` and one or more `targets` with IPv4 `host`, TCP `port` (1–65535), and optional `note`. Every target must match the outcome. Fast connection refusal counts as reachable; a timeout counts as blocked; any other exit status is a probe error and fails the target. UDP is not supported.
- `command`, `firewall`, `sequence` - older patterns for auto-running specific commands. Most labs now prefer command blocks in the description body for the copy/Run affordance. New labs should default to description blocks. A manual step has no `action`; `type: manual` is invalid.

`DecisionPanel` automatically reads:

- 1.3 design verdicts from `decision:segmentation-requirements:*` and renders a "Your design requirements" panel + per-action requirement badges + a coverage summary.
- 1.3 readiness verdicts from `decision:segmentation-requirements:{ot-eng-availability,fw-admin-skills,vendor-change-window,risk-acceptance-authority}` and renders a "Your team readiness" panel + per-action readiness constraints.

`readiness.yml` also requires `budgetAdjustments` (an explicit list; `[]` is valid).
Each entry has `source`, `verdict`, `teamCaps` (map of team names to whole-hour
caps, including zero) and `effort` (list of `{actions, multiplier, extraHours}`).
Source/verdict pairs are unique and must name a readiness decision option.
Caps cannot exceed their team's default. Multipliers are finite and at least 1;
extra hours are nonnegative integers. An entry must have an effect. Example:

```yaml
budgetAdjustments:
  - source: fw-admin-skills
    verdict: VENDOR_LED
    teamCaps: {Firewall Admin: 80}
    effort:
      - actions: [modbus-dpi, dnp3-dpi]
        multiplier: 1
        extraHours: 20
```

Matching caps combine by **minimum**, including the base cap. Action effort is
`round(base × product(multipliers) + sum(extraHours))`, nearest whole hour,
half up, once after all adjustments. The adjusted effort then splits across
roles using the existing nearest-integer-per-role model; rounded role hours
need not sum to the action total. Overhead belongs to total action effort and
is shared across its roles, not charged only to Firewall Admin. The total
`budgetHours` envelope does not change. Matching `readinessFlags` with severity
`blocked` decide eligibility; warnings (including signoff pending) do not.
Missing answers keep defaults. `Use default budget` turns off adjustments and
blocking, not the saved answers. Its storage name is `use-default-budget` with
`true`/`false`, scoped to package and revision.

All draft readers use the shared `readiness-budget.ts` eligibility model via
`remediation-plan.ts`. They exclude blocked actions without rewriting saved
intent and display reasons. The picker retains excluded selections for removal
or later restoration. Applied-plan IDs, hashes and proof expectations remain
historical facts and are never filtered by current readiness.

If you wire a different kind of action picker, see
[`frontend/components/decision-panel.tsx`](../frontend/components/decision-panel.tsx)
for the integration pattern.

## Browser storage model

Curriculum state persists client-side in `localStorage`. Every key is
`rd:<package>:<revision>:<name>`, where `<package>` and `<revision>` are
the active package's `id` and curriculum `revision` from
`GET /api/packages`. Names:

```
exercise:<scenario-id>                   JSON {completedStepIds, notes, cmdLog}
decision:<scenario-id>:<decision-id>     decision dropdown selection
remediation-plan                         JSON {exerciseId, selectedActionIds, savedAt}
firewall-track                           guided | technical
```

Progress records step `id`s, never array positions, so reordering
steps keeps it; IDs a scenario no longer has are ignored. Bumping the
package `revision` starts every student fresh under new keys. All keys
are built in
[`frontend/lib/curriculum-storage.ts`](../frontend/lib/curriculum-storage.ts);
keys written before this scheme are not read.

The runner exposes saved data via `:::findings-panel`, `default-from`
on `:::decision`, and `:::plan-coverage`. If you need a brand-new way
to surface saved data, add a parser case to `splitDescription` and a
renderer in `frontend/components/scenario-description-blocks.tsx`,
then document it here.

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
