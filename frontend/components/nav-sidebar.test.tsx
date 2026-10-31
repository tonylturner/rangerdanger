import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { US_PACKAGE, EU_PACKAGE } from "../lib/operator-fixtures.test-util";
import { NavSidebar } from "./nav-sidebar";

vi.mock("next/navigation", () => ({ usePathname: () => "/exercises" }));
vi.mock("./range-control", () => ({ RangeControl: () => <button>Range controls</button> }));

describe("responsive navigation", () => {
  it.each([US_PACKAGE, EU_PACKAGE])("keeps $id navigation and range controls available below the desktop breakpoint", (pkg) => {
    const client = new QueryClient();
    client.setQueryData(["packages", "active"], pkg);
    const html = renderToStaticMarkup(<QueryClientProvider client={client}><NavSidebar /></QueryClientProvider>);
    client.clear();
    const aside = html.match(/<aside class="([^"]+)"/)![1].split(" ");
    expect(aside).toContain("flex");
    expect(aside).not.toContain("hidden");
    expect(aside).toContain("w-full");
    expect(aside).toContain("md:w-48");
    expect(html).toContain('aria-label="Main navigation"');
    expect(html).toContain('aria-current="page"');
    for (const href of ["/", "/exercises", "/substation", "/console", "/labs", "/knowledge"]) {
      expect(html).toContain(`href="${href}"`);
    }
    expect(html).toContain(pkg === US_PACKAGE ? "Feeder HMI" : "Control Centre");
    expect(html).toContain("Range controls");
  });
});
