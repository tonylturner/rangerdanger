import { afterEach, describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BUILD_INFO_KEY } from "../lib/operator-queries";
import type { BuildInfo } from "../lib/api";
import { getBuild } from "../lib/api";
import { US_PACKAGE, EU_PACKAGE } from "../lib/operator-fixtures.test-util";
import { NavSidebar } from "./nav-sidebar";

vi.mock("next/navigation", () => ({ usePathname: () => "/exercises" }));
vi.mock("./range-control", () => ({ RangeControl: () => <button>Range controls</button> }));
afterEach(() => vi.unstubAllGlobals());

function renderSidebar(pkg: typeof US_PACKAGE | typeof EU_PACKAGE, build?: BuildInfo) {
  const client = new QueryClient();
  client.setQueryData(["packages", "active"], pkg);
  if (build) client.setQueryData(BUILD_INFO_KEY, build);
  const html = renderToStaticMarkup(<QueryClientProvider client={client}><NavSidebar /></QueryClientProvider>);
  client.clear();
  return html;
}

describe("responsive navigation", () => {
  it.each([US_PACKAGE, EU_PACKAGE])("keeps $id navigation and range controls available below the desktop breakpoint", (pkg) => {
    const html = renderSidebar(pkg);
    const aside = html.match(/<aside class="([^"]+)"/)![1].split(" ");
    expect(aside).toContain("flex");
    expect(aside).not.toContain("hidden");
    expect(aside).toContain("w-full");
    expect(aside).toContain("md:w-48");
    for (const sticky of ["md:sticky", "md:top-0", "md:h-screen", "md:overflow-y-auto"]) expect(aside).toContain(sticky);
    expect(html).toContain('aria-label="Main navigation"');
    expect(html).toContain('aria-current="page"');
    for (const href of ["/", "/exercises", "/substation", "/console", "/labs", "/knowledge"]) {
      expect(html).toContain(`href="${href}"`);
    }
    expect(html).toContain(pkg === US_PACKAGE ? "Feeder HMI" : "Control Centre");
    expect(html).toContain("Range controls");
    expect(html).not.toContain("version unavailable");
    expect(html).not.toContain("source build");
  });

  it.each([
    [{ version: "v0.2.0", commit: "76ac2a1", date: "2026-11-02T10:03:05Z" }, "v0.2.0 · 76ac2a1"],
    [{ version: "v0.2.0-rc.6", commit: "0123456789abcdef", date: "2026-11-02T10:03:05Z" }, "v0.2.0-rc.6 · 0123456"],
    [{ version: "dev", commit: "abcdef0123456789", date: "unknown" }, "source build · abcdef0"],
    [{ version: "dev", commit: "none", date: "unknown" }, "source build"],
  ] satisfies [BuildInfo, string][])("shows backend build metadata as $1.version", (build, label) => {
    const html = renderSidebar(US_PACKAGE, build);
    expect(html).toContain(`title="${build.date}">${label}</div>`);
    expect(html.indexOf("containd Firewall")).toBeLessThan(html.indexOf(label));
    expect(html).toContain("md:w-48");
  });

  it("shows a muted unavailable state when build metadata failed to load", () => {
    const client = new QueryClient();
    client.setQueryData(["packages", "active"], US_PACKAGE);
    client.setQueryData(BUILD_INFO_KEY, { version: "dev", commit: "none", date: "unknown" });
    client.getQueryCache().find({ queryKey: BUILD_INFO_KEY })?.setState({
      error: new Error("backend offline"),
      status: "error",
    });
    const html = renderToStaticMarkup(<QueryClientProvider client={client}><NavSidebar /></QueryClientProvider>);
    client.clear();

    expect(html).toContain(">version unavailable</div>");
    expect(html).not.toContain("source build");
    expect(html).toContain("text-slate-500");
  });

  it("reads build metadata from /api/build", async () => {
    const build: BuildInfo = { version: "v0.2.0", commit: "76ac2a1", date: "2026-11-02T10:03:05Z" };
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(build)));
    vi.stubGlobal("fetch", fetch);

    await expect(getBuild()).resolves.toEqual(build);
    expect(fetch).toHaveBeenCalledWith("/api/build", expect.objectContaining({ cache: "no-store" }));
  });
});
