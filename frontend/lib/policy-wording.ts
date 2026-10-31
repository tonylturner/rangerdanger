// How the portal names a firewall policy. The names are the active
// package's package.yml `policies` (GET /api/packages). package.yml has
// no label or description for a policy, so a package's policy is shown
// by its declared name. Only the US package keeps the wording the portal
// has always used for its two policies; that text describes the US
// files (RTAC, field zones) and must never appear for another package.

import type { PackageSummary, PlanLabel, PlanProvenance, PolicySource } from "./api";
import { US_OPERATOR_VIEW } from "./operator-view";

// The policy a step's expected_config alias "hardened" names, as the
// backend's labs.HardenedPolicy. A package need not declare it.
export const HARDENED_POLICY = "improved";
export const HARDENED_ALIAS = "hardened";
// active_config after apply-plan or a direct containd commit.
export const CUSTOM_POLICY = "custom";

export type PackagePolicies = {
  // Declared names, the package's default first.
  names: string[];
  // The US package: show its own wording instead of the bare names.
  usWording: boolean;
};

export function packagePolicies(pkg: PackageSummary): PackagePolicies {
  return { names: pkg.policies, usWording: pkg.operator_view === US_OPERATOR_VIEW };
}

export type PolicyWording = { label: string; description: string };

export const US_WORKSHOP_POLICY_WORDING: Record<string, PolicyWording> = {
  weak: { label: "Weak Baseline", description: "" },
  improved: { label: "Hardened", description: "" },
};

// The wording of one policy: the US table's entry for the US package,
// otherwise the declared name and no description.
export function policyWording(
  name: string,
  policies: PackagePolicies,
  usTable: Record<string, PolicyWording>,
): PolicyWording {
  const us = policies.usWording ? usTable[name] : undefined;
  return us ?? { label: name, description: "" };
}

// The active custom config is not a named policy. Keep its display
// distinct from both the US baseline and package-declared policy names.
export function activePolicyWording(
  name: string,
  policies: PackagePolicies | undefined,
  usTable: Record<string, PolicyWording>,
  source?: PolicySource,
  plan?: PlanProvenance,
): PolicyWording {
  if (source === "plan" && plan) return { label: `Your plan — ${planLabel(plan.label)}`, description: "Applied in your plan" };
  if (name === CUSTOM_POLICY) return { label: "Custom policy", description: "" };
  if (!policies) return { label: name, description: "" };
  return policyWording(name, policies, usTable);
}

export function isHardened(activeConfig: string | null | undefined): boolean {
  return activeConfig === HARDENED_POLICY;
}

// Whether the active policy satisfies a step's expected_config, as the
// backend's test runner reads it (test_runner.go): "hardened" and
// the hardened policy itself also accept a custom policy; any other
// policy name must be the active one; no expectation always matches.
export function matchesExpected(actual: string, expected: string | undefined): boolean {
  if (!expected) return true;
  if (expected === HARDENED_ALIAS || expected === HARDENED_POLICY) {
    return actual === HARDENED_POLICY || actual === CUSTOM_POLICY;
  }
  return actual === expected;
}

export function planLabel(label: PlanLabel): string {
  return { weak: "Weak", partial: "Partial", hardened: "Hardened" }[label];
}
