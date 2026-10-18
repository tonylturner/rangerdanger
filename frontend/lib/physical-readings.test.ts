import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { createElement } from "react";
import { FEEDER_STATE, TRUTH } from "./operator-fixtures.test-util";
import { controlCentreObservations, physicalSystemObservations } from "./physical-readings";
import { sectionSupplyPath } from "./operator-supply-path";
import { FeederOneLine } from "../components/feeder-one-line";
import { observedReading } from "../components/control-centre-one-line";

describe("independent one-line sources", () => {
  it("preserves every CC reading and leaves F2 unobserved", () => {
    const read = controlCentreObservations(FEEDER_STATE.points);
    for (const p of FEEDER_STATE.points) expect(read(p.id)).toEqual(observedReading(FEEDER_STATE.points, p.id));
    expect(read("f2-position").condition).toBe("unknown");
  });
  it("uses physical values, completed taps, effective blocks and no IEC flags", () => {
    const field = structuredClone(TRUTH);
    field.devices["oltc-1"].protection.picked_up = true;
    const read = physicalSystemObservations(field);
    expect(read("feeder-breaker-position").value).toBe("open");
    expect(read("oltc-1-tap").value).toEqual({ value: 1, transient: true });
    expect(read("oltc-1-blocked").value).toBe(true);
    expect(read("f2-position").value).toBe("closed");
    expect(read("feeder-current").flags).toEqual([]);
    const html = renderToStaticMarkup(createElement(FeederOneLine, { read, physical: true }));
    expect(html).toContain("CB1: OPEN · GOOD · sim");
    expect(html).toContain("F2 · closed (sim)");
    expect(html).toContain("Simulation-observed · independent of IEC 104");
    expect(html).not.toContain("faceplate");
  });
  it("keeps null unknown, invalid retained values stale, and stale F2 unknown", () => {
    const field = structuredClone(TRUTH);
    field.devices["breaker-1"].measurements.current_a = null;
    field.devices["rmu-1"].measurements.valid = false;
    field.process.truth_stale = true;
    const read = physicalSystemObservations(field);
    expect(read("feeder-current")).toMatchObject({ value: null, condition: "unknown" });
    expect(read("rmu-1-section-kv").text).toContain("INVALID");
    expect(read("f2-position").condition).toBe("unknown");
    expect(read("rmu-1-position").condition).toBe("good");
    delete field.devices["breaker-1"];
    expect(physicalSystemObservations(field)("feeder-breaker-position").condition).toBe("unknown");
  });
  it("establishes a physical F2 path and both sources when the tie closes", () => {
    const field = structuredClone(TRUTH);
    expect(sectionSupplyPath(physicalSystemObservations(field), "S3").state).toBe("via F2");
    const cb1 = field.devices["breaker-1"];
    const tie = field.devices["rmu-2"];
    if (cb1.kind === "breaker") cb1.position = "closed";
    if (tie.kind === "rmu") tie.position = "closed";
    expect(sectionSupplyPath(physicalSystemObservations(field), "S3").state).toBe("via CB1 + F2");
    field.process.f2_tripped = true;
    if (cb1.kind === "breaker") cb1.position = "open";
    expect(sectionSupplyPath(physicalSystemObservations(field), "S3")).toMatchObject({ state: "unknown", reason: expect.stringContaining("contradicts") });
  });
});
