import { describe, expect, it } from "vitest";
import type { ReactNode } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { PackageSummary, WorkshopStatus, PlanProvenance } from "../lib/api";
import { FIREWALL_ACTIVE_KEY, WORKSHOP_STATUS_KEY } from "../lib/live-queries";
import { EU_PACKAGE, US_PACKAGE } from "../lib/operator-fixtures.test-util";
import { packagePolicies } from "../lib/policy-wording";
import { EuDashboard, EuWorkshopStatus } from "./eu-overview";
import { PolicyBadge } from "./network-drawers";
import { PolicyStatusBanner } from "./policy-status-banner";
import { UsDashboard } from "./us-dashboard";
import { UsWorkshopStatus } from "./us-workshop-status";

function text(html: string): string {
  return html.replace(/<[^>]+>/g, " ").replace(/\s+/g, " ").trim();
}

function renderWith(pkg: PackageSummary | undefined, node: ReactNode, status?: WorkshopStatus, plan?: PlanProvenance): string {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  if (pkg) client.setQueryData(["packages", "active"], pkg);
  if (status) client.setQueryData(WORKSHOP_STATUS_KEY, status);
  if (plan) client.setQueryData(FIREWALL_ACTIVE_KEY, { active_config: "custom", policy_source: "plan", plan });
  const html = renderToStaticMarkup(<QueryClientProvider client={client}>{node}</QueryClientProvider>);
  client.clear();
  return text(html);
}

it.each([US_PACKAGE, EU_PACKAGE])("propagates $id's authored wording to every status surface", (pkg) => {
  const authored = { ...pkg, policies: [{ name: "weak", label: "Training baseline", description: "Package-owned teaching text." }] };
  const status: WorkshopStatus = {
    workshop_id: pkg.id, workshop_name: pkg.title, firewall_online: true,
    firewall_config: "weak", scenario_count: 1,
  };
  const banner = text(renderToStaticMarkup(
    <PolicyStatusBanner course={authored.course} activeConfig="weak" policySource="" policies={packagePolicies(authored)} hasCatchUpControls={false} />,
  ));
  expect(banner).toContain("Training baseline Package-owned teaching text.");
  for (const node of [
    <PolicyBadge key="badge" activeConfig="weak" />,
    <UsDashboard key="us-dashboard" />,
    <UsWorkshopStatus key="us-workshop" />,
    <EuDashboard key="eu-dashboard" title="EU" />,
    <EuWorkshopStatus key="eu-workshop" title="EU" />,
  ]) {
    expect(renderWith(authored, node, status)).toContain("Training baseline");
  }
});

describe("PolicyStatusBanner", () => {
  it.each(["weak", "partial", "hardened"] as const)("shows the %s label for a custom plan in both packages", (label) => {
    for (const pkg of [US_PACKAGE, EU_PACKAGE]) {
      const plan = { packageId: pkg.id, revision: pkg.revision, label, actionIds: [], policyHash: "abc", appliedAt: "now" };
      const out = text(renderToStaticMarkup(<PolicyStatusBanner course={pkg.course} activeConfig="custom" policySource="plan" plan={plan} policies={packagePolicies(pkg)} hasCatchUpControls={false} />));
      expect(out).toContain(`Your plan — ${label[0].toUpperCase() + label.slice(1)}`);
      expect(out).not.toContain("Hardened reference");
      expect(out).toContain("(Lab 1.4 plan)");
    }
  });

  it("derives the planning lab label from the binding, not a fixed number", () => {
    const course = structuredClone(US_PACKAGE.course);
    const planning = course.features.segmentationPlan!.activities.planning;
    course.modules.flatMap((module) => module.activities).find((a) => a.scenario === planning)!.label = "B";
    const out = text(renderToStaticMarkup(<PolicyStatusBanner course={course} activeConfig="custom" policySource="plan" policies={packagePolicies(US_PACKAGE)} hasCatchUpControls={false} />));
    expect(out).toContain("(Lab B plan)");
    expect(out).not.toContain("1.4");
  });

  const banner = (pkg: PackageSummary, activeConfig: string, expectedConfig?: string, hasCatchUpControls = true) =>
    text(
      renderToStaticMarkup(
        <PolicyStatusBanner
          course={pkg.course}
          activeConfig={activeConfig}
          policySource=""
          expectedConfig={expectedConfig}
          policies={packagePolicies(pkg)}
          hasCatchUpControls={hasCatchUpControls}
        />,
      ),
    );

  it.each([US_PACKAGE, EU_PACKAGE])("uses $id's labels and the shared policy-actions instruction", (pkg) => {
    expect(banner(pkg, "weak")).toBe("Currently running Weak Baseline Cross-zone attacks succeed under this policy.");
    expect(banner(pkg, "improved")).toBe("Currently running Hardened Segmentation The reference segmentation policy for this range.");
    expect(banner(pkg, "improved", "weak")).toContain("Restore the baseline in containd and read it back. Optional catch-up — for less technical students or teams behind schedule: use Reset to Weak below. It replaces typing rules, not your own observations, operator check or sign-off.");
    expect(banner(pkg, "weak", "hardened")).toContain("Hardened Segmentation (reference or your custom policy)");
    expect(banner(pkg, "weak", "hardened")).toContain("Author, review, commit and read back in containd");
    expect(banner(pkg, "weak", "hardened")).toContain("Optional catch-up — for less technical students or teams behind schedule: use the catch-up controls below. These replace typing rules, not your own observations, operator check or sign-off.");
    expect(banner(pkg, "weak", "hardened")).not.toContain("Apply Hardened");
    expect(banner(pkg, "weak", "improved")).not.toContain("Network Console");
    expect(banner(pkg, "custom", "weak")).toContain("expects the Weak Baseline");
  });

  it.each([US_PACKAGE, EU_PACKAGE])("omits catch-up instructions for $id when its controls are absent", (pkg) => {
    const weakMismatch = banner(pkg, "improved", "weak", false);
    expect(weakMismatch).toContain("Restore the baseline in containd and read it back.");
    expect(weakMismatch).not.toContain("Optional catch-up");
    expect(weakMismatch).not.toContain("below");

    const hardenedMismatch = banner(pkg, "weak", "hardened", false);
    expect(hardenedMismatch).toContain("Author, review, commit and read back in containd.");
    expect(hardenedMismatch).not.toContain("Optional catch-up");
    expect(hardenedMismatch).not.toContain("below");
  });

  it("waits for the package before naming a policy", () => {
    const out = text(
      renderToStaticMarkup(<PolicyStatusBanner course={undefined} activeConfig="weak" policySource="" policies={undefined} hasCatchUpControls={false} />),
    );
    expect(out).toBe("Currently running Loading…");
  });
});

describe("PolicyBadge", () => {
  it("uses the active endpoint's plan label instead of a stale named-policy prop", () => {
    const plan = { packageId: US_PACKAGE.id, revision: US_PACKAGE.revision, label: "weak" as const, actionIds: [], policyHash: "abc", appliedAt: "now" };
    expect(renderWith(US_PACKAGE, <PolicyBadge activeConfig="improved" />, undefined, plan)).toContain("Your plan — Weak");
  });

  it.each([US_PACKAGE, EU_PACKAGE])("uses $id's package labels and distinguishes custom", (pkg) => {
    expect(renderWith(pkg, <PolicyBadge activeConfig="improved" />)).toBe("Firewall · Hardened Segmentation");
    expect(renderWith(pkg, <PolicyBadge activeConfig="weak" />)).toBe("Firewall · Weak Baseline");
    expect(renderWith(pkg, <PolicyBadge activeConfig="custom" />)).toBe("Firewall · Custom policy");
  });

  it("waits for the package", () => {
    expect(renderWith(undefined, <PolicyBadge activeConfig="weak" />)).toBe("");
  });
});

describe("EuDashboard firewall card", () => {
  const status: WorkshopStatus = {
    workshop_id: "eu",
    workshop_name: "EU",
    firewall_online: true,
    firewall_config: "improved",
    scenario_count: 1,
    control_centre: { link: "started", gi: "complete" },
  };

  it("shows the package label on the dashboard and workshop status", () => {
    const out = renderWith(EU_PACKAGE, <EuDashboard title="EU" />, status);
    expect(out).toContain("Firewall Policy Hardened Segmentation");
    expect(renderWith(EU_PACKAGE, <EuWorkshopStatus title="EU" />, status)).toContain("Hardened Segmentation");
  });
});

describe("US firewall status wording", () => {
  const status: WorkshopStatus = {
    workshop_id: "us",
    workshop_name: "US",
    firewall_online: true,
    firewall_config: "custom",
    scenario_count: 1,
  };

  it("names a custom policy instead of calling it the weak baseline", () => {
    const dashboard = renderWith(US_PACKAGE, <UsDashboard />, status);
    const workshop = renderWith(US_PACKAGE, <UsWorkshopStatus />, status);
    expect(dashboard).toContain("Firewall Policy Custom policy");
    expect(workshop).toContain("Custom policy");
    expect(dashboard).not.toContain("Weak Baseline");
    expect(workshop).not.toContain("Weak Baseline");
  });

  it("keeps exact weak and improved labels", () => {
    expect(
      renderWith(US_PACKAGE, <UsDashboard />, { ...status, firewall_config: "weak" }),
    ).toContain("Firewall Policy Weak Baseline");
    expect(
      renderWith(US_PACKAGE, <UsDashboard />, { ...status, firewall_config: "improved" }),
    ).toContain("Firewall Policy Hardened Segmentation");
  });
});
