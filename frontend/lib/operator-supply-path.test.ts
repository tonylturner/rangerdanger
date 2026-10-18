import { describe, expect, it } from "vitest";
import type { OperatorPoint } from "./operator-api";
import { BOOT_STATE, FEEDER_STATE, LOST_LINK_STATE } from "./operator-fixtures.test-util";
import { SECTION_VOLTAGE, sectionSupplyPath as readingSupplyPath, type SectionId } from "./operator-supply-path";

import { controlCentreObservations } from "./observation-readings";
const sectionSupplyPath = (points: OperatorPoint[], section: SectionId) => readingSupplyPath(controlCentreObservations(points), section);

const sections: SectionId[] = ["S1", "S2", "S3"];
function pointsWith(patch: Record<string, Partial<OperatorPoint>> = {}) {
  return FEEDER_STATE.points.map((p) => ({ ...p, ...patch[p.id] }));
}

describe("observed supply path", () => {
  it("keeps the normal F2-fed S3 path unknown despite GOOD live voltage", () => {
    expect(sectionSupplyPath(FEEDER_STATE.points, "S1").state).toBe("via CB1");
    expect(sectionSupplyPath(FEEDER_STATE.points, "S2").state).toBe("via CB1");
    expect(sectionSupplyPath(FEEDER_STATE.points, "S3")).toMatchObject({ state: "path unknown", reason: expect.stringContaining("F2 unobserved") });
  });

  it("does not claim the tie-fed S2 is supplied via CB1", () => {
    const points = pointsWith({ "rmu-1-position": { value: "open" }, "rmu-2-position": { value: "closed" } });
    expect(sectionSupplyPath(points, "S1").state).toBe("via CB1");
    for (const section of ["S2", "S3"] as const) {
      expect(sectionSupplyPath(points, section)).toMatchObject({ state: "path unknown", reason: expect.stringContaining("F2 unobserved") });
    }
  });

  it.each(sections)("notes the unobserved F2 contribution when %s also has a CB1 path", (section) => {
    const points = pointsWith({ "rmu-2-position": { value: "closed" } });
    expect(sectionSupplyPath(points, section)).toMatchObject({
      state: "via CB1", f2ContributionUnobserved: true, reason: expect.stringContaining("F2 contribution unobserved"),
    });
  });

  it.each(sections)("reports GOOD zero at %s as no supply, regardless of closed switches", (section) => {
    const points = pointsWith({ [SECTION_VOLTAGE[section]]: { value: 0 }, "rmu-2-position": { value: "closed" } });
    expect(sectionSupplyPath(points, section).state).toBe("no supply");
  });

  it.each(sections)("requires GOOD section voltage at %s", (section) => {
    const id = SECTION_VOLTAGE[section];
    for (const flag of ["iv", "nt", "sb", "bl", "ov"] as const) {
      const point = FEEDER_STATE.points.find((p) => p.id === id)!;
      expect(sectionSupplyPath(pointsWith({ [id]: { quality: { ...point.quality, [flag]: true } } }), section).state).toBe("unknown");
    }
    for (const patch of [{ value: null }, { stale_reason: "not_updated" }, { value: -1 }] satisfies Partial<OperatorPoint>[]) {
      expect(sectionSupplyPath(pointsWith({ [id]: patch }), section).state).toBe("unknown");
    }
    expect(sectionSupplyPath(FEEDER_STATE.points.filter((p) => p.id !== id), section).state).toBe("unknown");
  });

  it.each(["feeder-breaker-position", "rmu-1-position", "rmu-2-position"])("requires GOOD closed feedback for %s", (id) => {
    const closed = pointsWith({ "rmu-2-position": { value: "closed" } });
    const point = closed.find((p) => p.id === id)!;
    const patches: Partial<OperatorPoint>[] = [
      ...(["open", "intermediate", "indeterminate", null] as const).map((value) => ({ value })),
      { stale_reason: "link_lost" },
      ...(["iv", "nt", "sb", "bl"] as const).map((flag) => ({ quality: { ...point.quality, [flag]: true } })),
    ];
    for (const patch of patches) {
      const points = closed.map((p) => p.id === id ? { ...p, ...patch } : p);
      expect(sectionSupplyPath(points, "S3").state).toBe("path unknown");
    }
    expect(sectionSupplyPath(closed.filter((p) => p.id !== id), "S3").state).toBe("path unknown");
  });

  it("requires GOOD live busbar voltage, not a CB1 line-side voltage substitute", () => {
    for (const patch of [{ value: 0 }, { value: null }, { stale_reason: "measurement_invalid" }] satisfies Partial<OperatorPoint>[]) {
      for (const section of sections) {
        expect(sectionSupplyPath(pointsWith({ "oltc-1-bus-kv": patch, "rmu-2-position": { value: "closed" } }), section).state).toBe("path unknown");
      }
    }
    expect(sectionSupplyPath(FEEDER_STATE.points.filter((p) => p.id !== "oltc-1-bus-kv"), "S1").state).toBe("path unknown");
    expect(sectionSupplyPath(pointsWith({ "feeder-voltage": { value: 0 } }), "S1").state).toBe("via CB1");
  });

  it("does not let downstream feedback quality invalidate a proven upstream path", () => {
    const points = pointsWith({ "rmu-2-position": { stale_reason: "device_unreachable" } });
    expect(sectionSupplyPath(points, "S1")).toMatchObject({ state: "via CB1", f2ContributionUnobserved: false });
    expect(sectionSupplyPath(points, "S2")).toMatchObject({ state: "via CB1", f2ContributionUnobserved: false });
  });

  it.each([BOOT_STATE, LOST_LINK_STATE])("makes every section unknown without valid observations", (state) => {
    for (const section of sections) expect(sectionSupplyPath(state.points, section).state).toBe("unknown");
  });
});
