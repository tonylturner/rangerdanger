import { describe, expect, it } from "vitest";
import { EU_PACKAGE, US_PACKAGE } from "./operator-fixtures.test-util";
import {
  networkMapHeading,
  operatorNavLabel,
  operatorViewProblem,
  resolveOperatorView,
  usFeederPolling,
} from "./operator-view";

describe("resolveOperatorView", () => {
  it("dispatches on the backend operator_view of the active package", () => {
    expect(resolveOperatorView(US_PACKAGE, null)).toEqual({ kind: "known", view: "us-dnp3-substation", pkg: US_PACKAGE });
    expect(resolveOperatorView(EU_PACKAGE, null)).toEqual({ kind: "known", view: "eu-iec104", pkg: EU_PACKAGE });
  });

  it("reports an unknown view instead of falling back to another", () => {
    const res = resolveOperatorView({ ...EU_PACKAGE, operator_view: "eu-iec61850" }, null);
    expect(res).toEqual({ kind: "unknown", packageId: "eu-iec104-substation", value: "eu-iec61850" });
    expect(operatorViewProblem(res)).toContain('"eu-iec61850"');
  });

  it("treats a package without an operator view as unknown", () => {
    const pkg = { ...US_PACKAGE } as Partial<typeof US_PACKAGE>;
    delete pkg.operator_view;
    const res = resolveOperatorView(pkg as typeof US_PACKAGE, null);
    expect(res.kind).toBe("unknown");
    expect(operatorViewProblem(res)).toBe("Package us-dnp3-substation declares no operator view.");
  });

  it("is loading until the package arrives and an error when it cannot be read", () => {
    expect(resolveOperatorView(undefined, null)).toEqual({ kind: "loading" });
    const res = resolveOperatorView(undefined, new Error("boom"));
    expect(res).toEqual({ kind: "error", message: "boom" });
    expect(operatorViewProblem(res)).toContain("boom");
    expect(operatorViewProblem({ kind: "loading" })).toBeNull();
  });

  it("keeps a known package over a later refetch error", () => {
    expect(resolveOperatorView(EU_PACKAGE, new Error("blip")).kind).toBe("known");
  });
});

describe("usFeederPolling", () => {
  it("polls the US RTAC only for the US view", () => {
    expect(usFeederPolling(resolveOperatorView(US_PACKAGE, null))).toBe(true);
    expect(usFeederPolling(resolveOperatorView(EU_PACKAGE, null))).toBe(false);
    expect(usFeederPolling({ kind: "loading" })).toBe(false);
    expect(usFeederPolling({ kind: "unknown", packageId: "x", value: "y" })).toBe(false);
    expect(usFeederPolling({ kind: "error", message: "e" })).toBe(false);
  });
});

describe("operatorNavLabel", () => {
  it("names the EU screen Control Centre and keeps Feeder HMI otherwise", () => {
    expect(operatorNavLabel(resolveOperatorView(EU_PACKAGE, null))).toBe("Control Centre");
    expect(operatorNavLabel(resolveOperatorView(US_PACKAGE, null))).toBe("Feeder HMI");
  });
});

describe("networkMapHeading", () => {
  it("keeps the US substation's words and names any other package by its title", () => {
    expect(networkMapHeading(resolveOperatorView(US_PACKAGE, null))).toEqual({
      title: "Substation Network Map",
      subtitle: "Distribution co-op feeder topology. Click a node to inspect, open UI, or terminal.",
    });
    expect(networkMapHeading(resolveOperatorView(EU_PACKAGE, null))).toEqual({
      title: "Network Map",
      subtitle: "EU IEC 104 Substation topology. Click a node to inspect, open UI, or terminal.",
    });
  });

  it("stays neutral until the package is known", () => {
    expect(networkMapHeading(resolveOperatorView(undefined, null))).toEqual({
      title: "Network Map",
      subtitle: "Click a node to inspect, open UI, or terminal.",
    });
  });
});
