// @vitest-environment jsdom
import { act, fireEvent, render as rtlRender, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderToStaticMarkup } from "react-dom/server";
import { EU_PACKAGE, US_PACKAGE } from "../lib/operator-fixtures.test-util";
import { FIREWALL_ACTIVE_KEY, NETWORK_EVENTS_KEY } from "../lib/live-queries";
import type { PolicyComparison } from "../lib/api";
import { FIREWALL_COMPARISON_KEY, SegmentationView } from "./segmentation-view";

const comparison: PolicyComparison = {
  weak_config: "weak.json",
  improved_config: "improved.json",
  diffs: [
    {
      zone_pair: "Enterprise → Station",
      weak_rule: "Weak IEC104 exposure",
      improved_rule: "Other sources denied",
      weak_action: "ALLOW",
      improved_action: "DENY",
      change: "tightened",
    },
  ],
  summary: "1 rule(s) tightened",
};

function renderText(
  pkg: typeof EU_PACKAGE | typeof US_PACKAGE,
  activeConfig: string | null,
  withComparison = true,
): string {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData(["packages", "active"], pkg);
  client.setQueryData(FIREWALL_ACTIVE_KEY, { active_config: activeConfig, policy_source: "manual-custom" });
  client.setQueryData(NETWORK_EVENTS_KEY, { events: [], source: "containd" });
  if (withComparison) client.setQueryData(FIREWALL_COMPARISON_KEY, comparison);
  const html = renderToStaticMarkup(
    <QueryClientProvider client={client}>
      <SegmentationView />
    </QueryClientProvider>,
  );
  client.clear();
  return html.replace(/<[^>]+>/g, " ").replace(/\s+/g, " ").trim();
}

describe("SegmentationView", () => {
  it("follows named active policies, retains custom fallback, and keeps a manual comparison sticky", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    client.setQueryData(["packages", "active"], US_PACKAGE);
    client.setQueryData(FIREWALL_ACTIVE_KEY, { active_config: "weak" });
    client.setQueryData(NETWORK_EVENTS_KEY, { events: [], source: "containd" });
    client.setQueryData(FIREWALL_COMPARISON_KEY, comparison);
    const tree = rtlRender(<QueryClientProvider client={client}><SegmentationView /></QueryClientProvider>);
    try {
      const selected = () => (screen.getByRole("combobox") as HTMLSelectElement).value;
      const active = async (active_config: string) => act(async () => {
        client.setQueryData(FIREWALL_ACTIVE_KEY, { active_config });
        await new Promise((resolve) => setTimeout(resolve, 0));
      });
      expect(selected()).toBe("weak");
      await active("improved");
      expect(selected()).toBe("improved");
      await active("custom");
      expect(selected()).toBe("improved");
      fireEvent.change(screen.getByRole("combobox"), { target: { value: "weak" } });
      await active("improved");
      expect(selected()).toBe("weak");
      await active("custom");
      expect(selected()).toBe("weak");
      await active("improved");
      vi.stubGlobal("fetch", vi.fn(async (url: string) => {
        if (url.endsWith("/firewall/apply")) return new Response(JSON.stringify({ status: "ok", active_config: "weak" }));
        throw new TypeError("refresh unavailable");
      }));
      fireEvent.click(screen.getByRole("button", { name: /Apply/ }));
      await waitFor(() => expect(client.getMutationCache().getAll().at(-1)?.state.status).toBe("success"));
      // A successful apply must not snap the inspection back to stale cache.
      expect(selected()).toBe("weak");
      await active("weak");
      await active("improved");
      expect(selected()).toBe("improved");
    } finally {
      tree.unmount();
      client.clear();
      vi.unstubAllGlobals();
    }
  });

  it.each([EU_PACKAGE, US_PACKAGE])("labels the mixed event stream by $id's active policy, without implying DPI is enabled", (pkg) => {
    for (const policy of ["weak", "improved", "custom"]) {
      const html = renderText(pkg, policy);
      const label = policy === "custom" ? "Custom policy"
        : pkg === US_PACKAGE ? (policy === "weak" ? "Weak Baseline" : "Hardened Segmentation") : policy;
      expect(html).toContain(`Live Firewall Events · ${label}`);
      expect(html).not.toContain("Live DPI Events");
    }
    expect(renderText(pkg, null)).toContain("Live Firewall Events · Policy loading…");
  });

  it("uses EU-specific named policy descriptions", () => {
    const weak = renderText(EU_PACKAGE, "weak");
    expect(weak).toContain(
      "Enterprise, vendor, and Control Centre sources can reach the RTU over IEC 104",
    );
    expect(weak).toContain("any station-zone host can reach the feeder breaker over Modbus TCP");
    expect(weak).not.toContain("RTAC");

    const improved = renderText(EU_PACKAGE, "improved");
    expect(improved).toContain("Only the Control Centre (10.60.60.20/32)");
    expect(improved).toContain("TCP/2404");
    expect(improved).toContain("TCP/502");
  });

  it("shows a custom active policy without baseline metadata or an active diff claim", () => {
    const html = renderText(US_PACKAGE, "custom");
    expect(html).toContain("Active Policy Custom policy");
    expect(html).not.toContain("All zones can reach field devices directly");
    expect(html).not.toMatch(/\d+ changes? vs active/);
    expect(html).toContain("Named policy comparison");
    expect(html).toContain("Enterprise → Station");
    expect(html).toContain("Apply");
  });

  it("keeps policy controls visible while comparison is loading", () => {
    vi.stubGlobal("fetch", vi.fn(() => new Promise(() => {})));
    try {
      const html = renderText(US_PACKAGE, "weak", false);
      expect(html).toContain("Loading named policy comparison");
      expect(html).toContain("Apply");
    } finally {
      vi.unstubAllGlobals();
    }
  });
});
