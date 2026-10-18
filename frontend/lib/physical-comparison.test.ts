import { describe, expect, it } from "vitest";
import type { OperatorPoint } from "./operator-api";
import { FEEDER_STATE, TRUTH } from "./operator-fixtures.test-util";
import { agreementText, comparisonCounts, physicalComparison, scadaQualityText } from "./physical-comparison";

function compare(id: string, patch: Partial<OperatorPoint> = {}) {
  const operator = structuredClone(FEEDER_STATE);
  Object.assign(operator.points.find((p) => p.id === id)!, patch);
  return physicalComparison(TRUTH, operator).find((r) => r.id === id)!;
}
describe("physical comparison precedence", () => {
  it("compares all independently mapped points, including measurement sides", () => {
    const rows = physicalComparison(TRUTH, FEEDER_STATE);
    expect(rows).toHaveLength(35);
    expect(rows.filter((r) => r.primary)).toHaveLength(6);
    expect(rows.find((r) => r.id === "rmu-2-source-kv")?.label).toContain("S3 side");
    expect(rows.find((r) => r.id === "rmu-1-source-kv")?.label).toContain("S1 side");
    expect(rows.some((r) => r.id.startsWith("plc"))).toBe(false);
  });
  it("does not match stale but equal values and identifies each hop", () => {
    for (const [reason, detail] of [["link_lost", "CC↔RTU link lost"], ["device_unreachable", "RTU cannot reach device"], ["heartbeat", "heartbeat"], ["not_updated", "not_updated"]] as const) {
      expect(compare("feeder-breaker-position", { value: "open", quality: { ...FEEDER_STATE.points[0].quality, iv: true, nt: true }, stale_reason: reason })).toMatchObject({ status: "STALE", detail: expect.stringContaining(detail) });
    }
  });
  it("distinguishes invalid, other quality flags and cached snapshots", () => {
    for (const flag of ["iv", "sb", "bl"] as const) expect(compare("feeder-breaker-position", { quality: { ...FEEDER_STATE.points[0].quality, [flag]: true } }).status).toBe("INVALID");
    expect(compare("feeder-breaker-position", { quality: { ...FEEDER_STATE.points[0].quality, nt: true } }).status).toBe("STALE");
    expect(physicalComparison(TRUTH, FEEDER_STATE, true).find((r) => r.id === "rmu-2-position")?.status).toBe("STALE");
  });
  it("uses each float's configured deadband, never a universal threshold", () => {
    expect(compare("oltc-1-bus-kv", { value: 20.12, reporting: { cyclic_ms: 10000, deadband: .1 } }).status).toBe("MATCH");
    expect(compare("oltc-1-bus-kv", { value: 20.12, reporting: { cyclic_ms: 10000, deadband: .01 } }).status).toBe("MISMATCH");
    expect(compare("oltc-1-bus-kv", { reporting: null }).status).toBe("UNKNOWN");
  });
  it("shows awaiting CC snapshot for a change later than rendered_at", () => {
    const field = structuredClone(TRUTH);
    field.devices["breaker-1"].changed_at = "2026-10-09T12:00:30Z";
    expect(physicalComparison(field, FEEDER_STATE)[0]).toMatchObject({ status: "UNKNOWN", detail: "awaiting CC snapshot" });
  });
  it("makes absent, null, invalid and stale physical values unknown before evaluating CC quality", () => {
    expect(physicalComparison(TRUTH, undefined).every((r) => r.status === "UNKNOWN")).toBe(true);
    expect(compare("feeder-current", { value: null }).status).toBe("UNKNOWN");
    const field = structuredClone(TRUTH);
    field.devices["breaker-1"].measurements.current_a = null;
    expect(physicalComparison(field, FEEDER_STATE).find((r) => r.id === "feeder-current")).toMatchObject({ status: "UNKNOWN", physical: { value: null } });
    field.process.truth_stale = true;
    expect(physicalComparison(field, FEEDER_STATE).find((r) => r.id === "oltc-1-bus-kv")?.status).toBe("UNKNOWN");
    delete field.devices["oltc-1"];
    expect(physicalComparison(field, FEEDER_STATE).find((r) => r.id === "oltc-1-tap")?.status).toBe("UNKNOWN");
  });
  it("compares completed tap rather than target, with motion evidence", () => {
    expect(compare("oltc-1-tap", { value: { value: 1, transient: true } })).toMatchObject({ status: "MATCH", detail: expect.stringContaining("field moving; completed tap shown") });
    expect(compare("oltc-1-tap", { value: { value: 2, transient: false } }).status).toBe("MISMATCH");
  });
  it("keeps usable SCADA quality separate from physical agreement", () => {
    expect(scadaQualityText(FEEDER_STATE)).toBe("42/42 usable received points");
    const counts = comparisonCounts(physicalComparison(TRUTH, FEEDER_STATE));
    expect(counts.MISMATCH).toBeGreaterThan(0);
    expect(agreementText(counts)).toContain("of 35");
    expect(scadaQualityText(FEEDER_STATE, true)).toContain("0/42");
  });
});
