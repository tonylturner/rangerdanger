import { describe, expect, it } from "vitest";
import type { ReactNode } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import DashboardPage from "../app/page";
import InstructorPage from "../app/instructor/page";
import WorkshopPage from "../app/labs/page";
import SubstationPage from "../app/substation/page";
import type { PackageSummary } from "../lib/api";
import { EU_PACKAGE, US_PACKAGE } from "../lib/operator-fixtures.test-util";
import { resolveOperatorView } from "../lib/operator-view";
import { navLinksFor } from "./nav-sidebar";

// Markup without tags, so assertions read like what a student sees.
function text(html: string): string {
  return html.replace(/<[^>]+>/g, " ").replace(/\s+/g, " ").trim();
}

// Renders a page with the active package already known, as the
// backend reported it.
function renderWith(pkg: PackageSummary | undefined, page: ReactNode): string {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  if (pkg) client.setQueryData(["packages", "active"], pkg);
  const html = renderToStaticMarkup(<QueryClientProvider client={client}>{page}</QueryClientProvider>);
  client.clear();
  return text(html);
}

const UNKNOWN: PackageSummary = { ...EU_PACKAGE, id: "jp-iec61850", operator_view: "jp-iec61850" };

describe("/substation operator view dispatch", () => {
  it("renders today's US feeder page for the US view", () => {
    const out = renderWith(US_PACKAGE, <SubstationPage />);
    expect(out).toContain("Feeder 101 — Distribution Substation");
    expect(out).toContain("Substation Process View");
    expect(out).toContain("Live feeder state from RTAC via OpenDSS physics engine");
    expect(out).not.toContain("Control Centre");
  });

  it("renders the control centre for the EU view", () => {
    const out = renderWith(EU_PACKAGE, <SubstationPage />);
    expect(out).toContain("Control Centre");
    expect(out).toContain("IEC 60870-5-104");
    expect(out).not.toContain("RTAC");
  });

  it("shows an error panel for an unknown view, never another package's page", () => {
    const out = renderWith(UNKNOWN, <SubstationPage />);
    expect(out).toContain("Operator view unavailable");
    expect(out).toContain("declares the operator view &quot;jp-iec61850&quot;, which this portal does not know");
    expect(out).not.toContain("Substation Process View");
    expect(out).not.toContain("Control Centre");
  });

  it("renders nothing package-specific while the package loads", () => {
    const out = renderWith(undefined, <SubstationPage />);
    expect(out).toBe("Loading the active package…");
  });
});

describe("overview and workshop status dispatch", () => {
  it("keeps the US overview and status pages", () => {
    const overview = renderWith(US_PACKAGE, <DashboardPage />);
    expect(overview).toContain("Substation Segmentation Lab");
    expect(overview).toContain("RTAC Controller");
    const status = renderWith(US_PACKAGE, <WorkshopPage />);
    expect(status).toContain("Distribution Substation Lab");
    expect(status).toContain("RTAC Controller");
  });

  it("shows the control centre instead of the RTAC for EU", () => {
    const overview = renderWith(EU_PACKAGE, <DashboardPage />);
    expect(overview).toContain("EU IEC 104 Substation");
    expect(overview).toContain("Control Centre Link");
    expect(overview).not.toContain("RTAC");
    const status = renderWith(EU_PACKAGE, <WorkshopPage />);
    expect(status).toContain("Control Centre Link");
    expect(status).not.toContain("RTAC");
  });

  it("refuses an unknown view on every dispatched page", () => {
    for (const page of [<DashboardPage key="d" />, <WorkshopPage key="w" />, <InstructorPage key="i" />]) {
      expect(renderWith(UNKNOWN, page)).toContain("Operator view unavailable");
    }
  });
});

describe("/instructor", () => {
  it("is the out-of-band truth page for EU only", () => {
    expect(renderWith(EU_PACKAGE, <InstructorPage />)).toContain("Instructor truth — out of band");
    const us = renderWith(US_PACKAGE, <InstructorPage />);
    expect(us).toContain("has no instructor truth page");
    expect(us).not.toContain("out of band");
  });
});

describe("navLinksFor", () => {
  it("labels the EU operator screen Control Centre and adds the instructor link", () => {
    const eu = navLinksFor(resolveOperatorView(EU_PACKAGE, null));
    expect(eu.find((l) => l.href === "/substation")?.label).toBe("Control Centre");
    expect(eu.map((l) => l.href)).toContain("/instructor");
  });

  it("keeps the US sidebar as it was", () => {
    expect(navLinksFor(resolveOperatorView(US_PACKAGE, null))).toEqual([
      { href: "/", label: "Overview" },
      { href: "/exercises", label: "Exercises" },
      { href: "/substation", label: "Feeder HMI" },
      { href: "/console", label: "Network Map" },
      { href: "/labs", label: "Workshop Status" },
      { href: "/knowledge", label: "Knowledge" },
    ]);
  });
});
