import { describe, expect, it } from "vitest";
import type { ReactNode } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { PackageSummary, WorkshopStatus } from "../lib/api";
import { WORKSHOP_STATUS_KEY } from "../lib/live-queries";
import { EU_PACKAGE, US_PACKAGE } from "../lib/operator-fixtures.test-util";
import { packagePolicies } from "../lib/policy-wording";
import { EuDashboard } from "./eu-overview";
import { PolicyBadge } from "./network-drawers";
import { PolicyStatusBanner } from "./policy-status-banner";
import { UsDashboard } from "./us-dashboard";
import { UsWorkshopStatus } from "./us-workshop-status";

function text(html: string): string {
  return html.replace(/<[^>]+>/g, " ").replace(/\s+/g, " ").trim();
}

function renderWith(pkg: PackageSummary | undefined, node: ReactNode, status?: WorkshopStatus): string {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  if (pkg) client.setQueryData(["packages", "active"], pkg);
  if (status) client.setQueryData(WORKSHOP_STATUS_KEY, status);
  const html = renderToStaticMarkup(<QueryClientProvider client={client}>{node}</QueryClientProvider>);
  client.clear();
  return text(html);
}

const US_TEXT = /Hardened|Weak [Bb]aseline|RTAC|substation-improved|Lab 1\.4|Apply Hardened|Reset to Weak/;

describe("PolicyStatusBanner", () => {
  const banner = (pkg: PackageSummary, activeConfig: string, expectedConfig?: string) =>
    text(
      renderToStaticMarkup(
        <PolicyStatusBanner
          activeConfig={activeConfig}
          policySource=""
          expectedConfig={expectedConfig}
          policies={packagePolicies(pkg)}
        />,
      ),
    );

  it("keeps the US wording", () => {
    expect(banner(US_PACKAGE, "weak")).toBe("Currently running Weak baseline Cross-zone attacks succeed under this config.");
    expect(banner(US_PACKAGE, "improved")).toBe(
      "Currently running Hardened reference Canned reference policy (substation-improved.json).",
    );
    expect(banner(US_PACKAGE, "improved", "weak")).toBe(
      "Policy mismatch This step expects the weak baseline , but the firewall is currently running Hardened reference . Click Reset to Weak in the policy-actions panel below to restore the baseline.",
    );
    expect(banner(US_PACKAGE, "weak", "hardened")).toMatch(
      /expects the hardened policy \(reference or your custom\) .* Click Apply Hardened \(canned reference\) or Apply Your Plan/,
    );
  });

  it("names another package's policies by their declared names, without US text", () => {
    expect(banner(EU_PACKAGE, "weak")).toBe("Currently running weak");
    expect(banner(EU_PACKAGE, "improved")).toBe("Currently running improved");
    const mismatch = banner(EU_PACKAGE, "weak", "hardened");
    expect(mismatch).toBe(
      "Policy mismatch This step expects the improved policy (or a custom policy) , but the firewall is currently running weak . Apply it from the Firewall policy menu in the Network Console.",
    );
    expect(banner(EU_PACKAGE, "custom", "weak")).toMatch(/expects the weak policy , but the firewall is currently running Your custom policy/);
    for (const out of [banner(EU_PACKAGE, "weak"), banner(EU_PACKAGE, "improved"), mismatch]) {
      expect(out).not.toMatch(US_TEXT);
    }
  });

  it("waits for the package before naming a policy", () => {
    const out = text(
      renderToStaticMarkup(<PolicyStatusBanner activeConfig="weak" policySource="" policies={undefined} />),
    );
    expect(out).toBe("Currently running Loading…");
  });
});

describe("PolicyBadge", () => {
  it("keeps the US words and shows another package's active policy by name", () => {
    expect(renderWith(US_PACKAGE, <PolicyBadge activeConfig="improved" />)).toBe("Firewall · Hardened");
    expect(renderWith(US_PACKAGE, <PolicyBadge activeConfig="weak" />)).toBe("Firewall · Weak baseline");
    expect(renderWith(EU_PACKAGE, <PolicyBadge activeConfig="improved" />)).toBe("Firewall · improved");
    expect(renderWith(EU_PACKAGE, <PolicyBadge activeConfig="custom" />)).toBe("Firewall · Custom policy");
    expect(renderWith(US_PACKAGE, <PolicyBadge activeConfig="custom" />)).toBe("Firewall · Custom policy");
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

  it("shows the declared policy name", () => {
    const out = renderWith(EU_PACKAGE, <EuDashboard title="EU" />, status);
    expect(out).toContain("Firewall Policy improved");
    expect(out).not.toMatch(US_TEXT);
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
    ).toContain("Firewall Policy Hardened");
  });
});
