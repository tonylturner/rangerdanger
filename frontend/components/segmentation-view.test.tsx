// @vitest-environment jsdom
import { act, fireEvent, render as rtlRender, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderToStaticMarkup } from "react-dom/server";
import { EU_PACKAGE, US_PACKAGE } from "../lib/operator-fixtures.test-util";
import { FIREWALL_ACTIVE_KEY, NETWORK_EVENTS_KEY } from "../lib/live-queries";
import type { NetworkEventsResponse, PolicyComparison } from "../lib/api";
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
  networkEvents: NetworkEventsResponse = { events: [], source: "containd" },
): string {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData(["packages", "active"], pkg);
  client.setQueryData(FIREWALL_ACTIVE_KEY, { active_config: activeConfig, policy_source: "manual-custom" });
  client.setQueryData(NETWORK_EVENTS_KEY, networkEvents);
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
  it("separates monitor decisions from actual denials in a mixed event stream", () => {
    const events: NetworkEventsResponse["events"] = [
      { id: "drop", timestamp: "", kind: "ics.decision", attributes: { mode: "monitor", decision: "would_drop", action: "DENY", ruleId: "observe-breaker", reason: "rule" } },
      { id: "allow", timestamp: "", kind: "ics.decision", attributes: { mode: "monitor", decision: "would_allow", action: "ALLOW", ruleId: "observe-oltc", reason: "rule" } },
      { id: "deny", timestamp: "", kind: "firewall.rule.hit", attributes: { action: "DENY", ruleId: "source-pin-deny" } },
      { id: "admit", timestamp: "", kind: "firewall.rule.hit", attributes: { action: "ALLOW", ruleId: "source-pin-allow" } },
      { id: "ids", timestamp: "", kind: "anomaly", attributes: { action: "DENY", anomaly_type: "protocol anomaly" } },
    ];
    const html = renderText(EU_PACKAGE, "improved", true, { events, source: "containd" }).split("Named policy comparison")[0];
    expect(html).toContain("2 rule · 2 dpi · 1 ids · 1 blocked");
    expect(html).toContain("would deny (monitor)");
    expect(html).toContain("would allow (monitor)");
    expect(html).toContain("observe-breaker");
    expect(html).toContain("observe-oltc");
    expect(html.match(/\bDENY\b/g)).toHaveLength(1);
    expect(html.match(/\bALLOW\b/g)).toHaveLength(1);
    expect(html).not.toContain("would have blocked");
  });

  it("renders RESET as blocked with DENY tone, without counting non-rule actions", () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(["packages", "active"], EU_PACKAGE);
    client.setQueryData(FIREWALL_ACTIVE_KEY, { active_config: "custom" });
    const events: NetworkEventsResponse = { source: "containd", events: [
      { id: "reset", timestamp: "", kind: "firewall.rule.hit", attributes: { action: "RESET", ruleId: "reset-rule" } },
      { id: "deny", timestamp: "", kind: "firewall.rule.hit", attributes: { action: "DENY", ruleId: "deny-rule" } },
      { id: "ids", timestamp: "", kind: "anomaly", attributes: { action: "RESET" } },
      { id: "shadow", timestamp: "", kind: "ics.decision", attributes: { mode: "monitor", decision: "would_drop", action: "DENY" } },
    ] };
    client.setQueryData(NETWORK_EVENTS_KEY, events);
    const tree = rtlRender(<QueryClientProvider client={client}><SegmentationView /></QueryClientProvider>);
    try {
      expect(screen.getByText("2 rule · 1 dpi · 1 ids · 2 blocked")).toBeTruthy();
      for (const action of ["RESET", "DENY"]) {
        const chip = screen.getByText(action);
        expect(chip.className).toContain("text-red-400");
        expect(chip.closest("div.rounded-sm")?.className).toContain("bg-red-950/15");
      }
      expect(screen.getAllByText("RESET")).toHaveLength(1);
      expect(screen.getByText("would deny (monitor)")).toBeTruthy();
    } finally {
      tree.unmount();
      client.clear();
    }
  });

  it("never invents a verdict or counts a monitor-only stream as denied commands", () => {
    const html = renderText(EU_PACKAGE, "improved", true, {
      source: "containd",
      events: [
        { id: "shadow", timestamp: "", kind: "ics.decision", attributes: { mode: "monitor", decision: "would_drop", action: "DENY" } },
        { id: "unknown", timestamp: "", kind: "ics.decision", attributes: { mode: "monitor", action: "DENY" } },
      ],
    }).split("Named policy comparison")[0];
    expect(html).toContain("0 rule · 2 dpi · 0 ids · 0 blocked");
    expect(html).toContain("monitor observation");
    expect(html).not.toMatch(/\b(ALLOW|DENY)\b/);
  });

  it("shows evidence limits rather than a normal empty-state hint on engine failure", () => {
    const html = renderText(EU_PACKAGE, "improved", true, {
      events: [], source: "containd", historyTruncated: true, engineError: "engine down",
    });
    expect(html).toContain("Event history is truncated; this is not a complete record.");
    expect(html).toContain("DPI engine error: engine down");
    expect(html).not.toContain("No recent events");
  });

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
        : pkg.policies.find((p) => p.name === policy)!.label;
      expect(html).toContain(`Live Firewall Events · ${label}`);
      expect(html).not.toContain("Live DPI Events");
    }
    expect(renderText(pkg, null)).toContain("Live Firewall Events · Policy loading…");
  });

  it.each([EU_PACKAGE, US_PACKAGE])("uses $id's package labels and descriptions", (pkg) => {
    for (const policy of pkg.policies) {
      const html = renderText(pkg, policy.name);
      expect(html).toContain(`Active Policy ${policy.label} ${policy.description}`);
      expect(html).not.toContain("has no logged rules");
      expect(html).toContain("No recent events");
    }
  });

  it("takes comparison controls and active wording from package data, not a name table", () => {
    const pkg = {
      ...EU_PACKAGE,
      policies: EU_PACKAGE.policies.map((policy) => ({
        ...policy, label: `Authored ${policy.name}`, description: `Description for ${policy.name}`,
      })),
    };
    const html = renderText(pkg, "improved");
    expect(html).toContain("Active Policy Authored improved Description for improved");
    expect(html).toContain("Inspect Policy Authored weak Authored improved · active");
    expect(html).toContain("Live Firewall Events · Authored improved");
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
      expect(html).toContain("Inspect Policy Weak Baseline · active Hardened Segmentation");
      expect(html).toContain("Active Weak Baseline Hardened Segmentation");
    } finally {
      vi.unstubAllGlobals();
    }
  });
});
