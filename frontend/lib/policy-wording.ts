// Named policy wording comes exclusively from package.yml via /api/packages.

import type { PackagePolicy, PackageSummary, PlanLabel, PlanProvenance, PolicySource } from "./api";

// The policy a step's expected_config alias "hardened" names, as the
// backend's labs.HardenedPolicy. A package need not declare it.
export const HARDENED_POLICY = "improved";
export const HARDENED_ALIAS = "hardened";
// active_config after apply-plan or a direct containd commit.
export const CUSTOM_POLICY = "custom";

export type PackagePolicies = PackagePolicy[];

export function packagePolicies(pkg: PackageSummary): PackagePolicies {
  return pkg.policies;
}

export type PolicyWording = { label: string; description: string };

export function policyWording(name: string, policies: PackagePolicies): PolicyWording {
  const policy = policies.find((policy) => policy.name === name);
  return policy ? { label: policy.label, description: policy.description } : { label: name, description: "" };
}

// The active custom config is not a named policy. Keep its display
// distinct from package-declared policy names.
export function activePolicyWording(
  name: string,
  policies: PackagePolicies | undefined,
  source?: PolicySource,
  plan?: PlanProvenance,
): PolicyWording {
  if (source === "plan" && plan) return { label: `Your plan — ${planLabel(plan.label)}`, description: "Applied in your plan" };
  if (name === CUSTOM_POLICY) return { label: "Custom policy", description: "" };
  if (!policies) return { label: "Loading…", description: "" };
  return policyWording(name, policies);
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
