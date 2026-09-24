import {
  renderRuleTable,
  positiveValidationTests,
  negativeValidationTests,
  type DynamicExercisePlan,
} from "./remediation-to-rules";

// Step title fragments used to identify dynamic phases in firewall-implementation.
export const PHASE3_TITLES = ["create minimal", "phase 3"];
export const PHASE5_TITLES = ["validate allowed", "phase 5"];
export const PHASE6_TITLES = ["validate blocked", "phase 6"];

// Scenarios whose steps actually exercise the firewall - the policy
// action buttons (Apply Hardened / Apply Your Plan / Reset to Weak)
// render here. The planning labs (1.2 baseline, 1.3 requirements,
// 1.4 plan) are intentionally excluded because policy state changes
// during their steps would not match their student-facing narrative.
export const POLICY_ACTION_SCENARIOS = [
  "firewall-implementation",     // Lab 2.2
  "hardening-configurations",    // Lab 2.3
  "vendor-rdp-compromise",       // Lab 2.3-bonus
  "validation-evidence",         // Lab 2.4
];

// Scenarios where the "Validate Exercise" button is meaningful - i.e. the
// per-scenario validator checks live state that genuinely reflects what the
// student did (policy applied, attack blocked, operations preserved). The
// planning/analysis labs (1.2 baseline, 1.3 requirements, 1.4 plan) are
// excluded: their "work" is the decision blocks and findings, not a grid-state
// check, so a validator there can only report "is the lab healthy" - a near-
// always-PASS that misleads. The button is hidden on those.
export const VALIDATE_BUTTON_SCENARIOS = [
  "firewall-implementation",     // Lab 2.2
  "hardening-configurations",    // Lab 2.3
  "vendor-rdp-compromise",       // Lab 2.3-bonus
  "validation-evidence",         // Lab 2.4
];

export function titleMatches(title: string, fragments: string[]): boolean {
  const lower = title.toLowerCase();
  return fragments.some((f) => lower.includes(f));
}

// For firewall-implementation, generate full step descriptions from the
// student's remediation plan. Uses step titles (stable) rather than
// regex matching inside description text (fragile).
export function injectDynamicContent(
  desc: string,
  stepTitle: string,
  plan: DynamicExercisePlan | null,
): string {
  if (!plan) return desc;

  if (titleMatches(stepTitle, PHASE3_TITLES)) {
    const ruleTable = renderRuleTable(plan);

    const noPlan = !plan.hasRemediationPlan
      ? `You have not completed a remediation plan yet. The rules below are the minimum baseline - RTAC and GPS access to field devices. Consider going back to the [Remediation Planning](/exercises/remediation-planning) exercise to build a plan that drives additional rules.\n\n`
      : "";

    // Phase 3's generated description has to re-emit the :::guided
    // / :::technical fork itself because injectDynamicContent
    // entirely replaces the YAML for this step (the YAML edits
    // wouldn't survive otherwise). Same intent as the static
    // forks in 2.3 step 4 / 2.2 step 7 - guided students walk
    // the table to see what landed; technical students treat the
    // table as the spec they're authoring against.
    return `The six rules below are the contract for what cross-zone
traffic the substation must allow. Everything else stays denied.

${noPlan}:::guided
Click **Apply Hardened** in the **containd NGFW - policy actions** panel below this step to push the canned
reference, or **Apply Your Plan** to push the policy built from
your Lab 1.4 picks. Then walk the rule table below and confirm
each row is present in containd's web UI
([http://localhost:9080](http://localhost:9080)) or via
\`show running-config\` in the CLI. Understanding *what* is in
the policy matters as much as *how* it gets there.
:::

:::technical
Author these rules in containd directly - your choice of the web
UI or the appliance CLI. The banner above will switch to
"Your custom policy" once your commit lands. The JSON schema and
CLI walkthrough are in the hints below.
:::

### Firewall Rules to Implement

${ruleTable}

> **Critical:** RTAC rules are source-pinned to \`10.30.30.20\`. Do **not** use a broad OT Ops subnet rule. If the HMI or OpenPLC is compromised, it must not reach field devices directly.

:::hint Creating rules via the containd CLI
The granular protocol-port rule schema (with ICS DPI fields) is best edited via JSON in the CLI rather than typed-in argument lists. From the \`fw-1\` terminal, type \`containd cli\` to enter the appliance shell, then:

    export config > /tmp/policy.json
    shell                   # drop to bash
    vi /tmp/policy.json     # edit the firewall.rules array to add each rule above
    exit                    # back to containd CLI
    import config /tmp/policy.json
    show diff               # confirm what will change
    commit

Each rule object in \`firewall.rules\` looks like:

    {
      "id": "rtac-to-field-modbus",
      "description": "RTAC Modbus polling to field devices",
      "sourceZones": ["lan1"],
      "destZones": ["lan2"],
      "sources": ["10.30.30.20/32"],
      "protocols": [{"name": "tcp", "port": "502"}],
      "ics": {"protocol": "modbus", "functionCode": [1, 2, 3, 4, 5, 6]},
      "action": "ALLOW",
      "log": true
    }

Tip: \`commit confirmed 60\` commits with auto-rollback after 60 seconds unless you type \`confirm\` - useful when pushing rules and you don't want to lock yourself out.
:::`;
  }

  if (titleMatches(stepTitle, PHASE5_TITLES)) {
    const tests = positiveValidationTests(plan);
    return `Your rules are in place. Now verify that operations still work. If any of these tests fail, you have a rule that is too restrictive or missing.

**Positive validation commands (based on your plan):**

${tests.join("\n\n")}

If any test fails, check your rules. The most common mistake is forgetting to allow a required flow, or pinning the source too narrowly on the GPS rule.`;
  }

  if (titleMatches(stepTitle, PHASE6_TITLES)) {
    const tests = negativeValidationTests(plan);
    return `Now verify that unauthorized traffic is denied. Every test below should fail with a timeout or connection refused.

**Negative validation commands (based on your plan):**

${tests.join("\n\n")}

After each failed attempt, check the containd event log to confirm the deny was logged. From the \`fw-1\` terminal, type \`containd cli\`, then:

    show audit

…and look for the most recent deny entries. If you don't see your test attempts, logging isn't enabled on the deny rules.

If any test succeeds when it should fail, you have a rule that is too permissive. Review your policy and tighten it.`;
  }

  return desc;
}
export function actionLabel(type: string): string {
  switch (type) {
    case "command": return "auto";
    case "check": return "verify";
    case "firewall": return "config";
    case "sequence": return "auto";
    default: return type;
  }
}

export function deviceLabel(device: string): string {
  switch (device) {
    case "relay": return "Feeder Breaker (10.40.40.20)";
    case "recloser": return "Recloser (10.40.40.21)";
    case "regulator": return "Regulator (10.40.40.22)";
    default: return device;
  }
}

export function isSegmentationStep(title?: string): boolean {
  if (!title) return false;
  const t = title.toLowerCase();
  // Match the "apply hardened policy" steps in Lab 2.3 and 2.3-bonus
  // alongside the older "improve segmentation" wording used elsewhere.
  // Without `hardened`/`harden`, Lab 2.3 step 6 ("Apply the hardened
  // policy") and Lab 2.3-bonus step 5 ("Apply hardened policy") would
  // hide the Apply Hardened button even though the step's own action
  // is { type: firewall, config: improved }.
  return t.includes("improve") || t.includes("segmentation") || t.includes("hardened") || t.includes("harden");
}

export function isBaselineStep(title?: string): boolean {
  if (!title) return false;
  return title.toLowerCase().includes("baseline") || title.toLowerCase().includes("observe");
}
