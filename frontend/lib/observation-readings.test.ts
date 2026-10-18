import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { createElement } from "react";
import { FEEDER_STATE, STALE_STATE, TRUTH } from "./operator-fixtures.test-util";
import { controlCentreObservations, physicalSystemObservations } from "./observation-readings";
import { sectionSupplyPath } from "./operator-supply-path";
import { FeederOneLine } from "../components/feeder-one-line";

describe("independent one-line sources", () => {
  it("pins GOOD CC observation values and text", () => {
    expect(controlCentreObservations(FEEDER_STATE.points)("feeder-breaker-position")).toEqual({
      value: "closed", condition: "good", text: "CLOSED · GOOD", shortText: "CLOSED · GOOD",
      flags: [], staleReason: null, transient: false,
    });
  });
  it("pins stale CC text, IEC flag order and the stale reason", () => {
    expect(controlCentreObservations(STALE_STATE.points)("feeder-breaker-position")).toEqual({
      value: "closed", condition: "stale", text: "CLOSED · STALE · IV NT · link_lost", shortText: "CLOSED · STALE · IV NT",
      flags: ["IV", "NT"], staleReason: "link_lost", transient: false,
    });
  });
  it("pins missing CC observation text and leaves F2 unknown", () => {
    expect(controlCentreObservations(FEEDER_STATE.points.filter((p) => p.id !== "f2-position"))("f2-position")).toEqual({
      value: undefined, condition: "unknown", text: "unknown · UNKNOWN", shortText: "unknown · UNKNOWN",
      flags: [], staleReason: undefined, transient: false,
    });
  });
  it("pins the cached CC snapshot suffix without rewriting IEC quality", () => {
    expect(controlCentreObservations(FEEDER_STATE.points, true)("feeder-breaker-position")).toEqual({
      value: "closed", condition: "good", text: "CLOSED · GOOD · cached snapshot · CC API unavailable", shortText: "CLOSED · GOOD",
      flags: [], staleReason: null, transient: false,
    });
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
    expect(html).toContain("F2 · CLOSED · GOOD · sim");
    expect(html).toContain("Simulation-observed · independent of IEC 104");
    expect(html).not.toContain("faceplate");
  });
  it("keeps null unknown and invalid measurements stale without invalidating device positions", () => {
    const field = structuredClone(TRUTH);
    field.devices["breaker-1"].measurements.current_a = null;
    field.devices["rmu-1"].measurements.valid = false;
    field.process.truth_stale = true;
    const read = physicalSystemObservations(field);
    expect(read("feeder-current")).toMatchObject({ value: null, condition: "unknown" });
    expect(read("rmu-1-section-kv").text).toContain("INVALID");
    expect(read("f2-position").condition).toBe("good");
    expect(read("rmu-1-position").condition).toBe("good");
    delete field.devices["breaker-1"];
    expect(physicalSystemObservations(field)("feeder-breaker-position").condition).toBe("unknown");
  });
  it("reads all four F2 channels from its device, never a process latch or the other view", () => {
    const field = structuredClone(TRUTH);
    // Deliberately contradictory process data cannot override the device.
    field.process.f2_tripped = true;
    const physical = physicalSystemObservations(field);
    expect(physical("f2-position")).toMatchObject({ value: "closed", condition: "good" });
    expect(physical("f2-protection-trip")).toMatchObject({ value: false, condition: "good" });
    expect(physical("f2-current")).toMatchObject({ value: 11.2, condition: "good" });
    expect(physical("f2-p-mw")).toMatchObject({ value: 0.38, condition: "good" });
    const cc = controlCentreObservations(FEEDER_STATE.points.map((p) => p.id === "f2-position" ? { ...p, value: "open" } : p));
    expect(cc("f2-position").value).toBe("open");
    expect(physical("f2-position").value).toBe("closed");
    field.devices["f2-breaker"].measurements.valid = false;
    expect(physicalSystemObservations(field)("f2-current").condition).toBe("stale");
    expect(physicalSystemObservations(field)("f2-p-mw").condition).toBe("stale");
    expect(physicalSystemObservations(field)("f2-position").condition).toBe("good");
    for (const id of ["f2-position", "f2-protection-trip", "f2-current", "f2-p-mw"]) {
      expect(physicalSystemObservations(field, true)(id).condition).toBe("stale");
    }
    delete field.devices["f2-breaker"];
    expect(physicalSystemObservations(field)("f2-position")).toMatchObject({ value: null, condition: "unknown" });
  });
  it("establishes a physical F2 path and both sources when the tie closes", () => {
    const field = structuredClone(TRUTH);
    expect(sectionSupplyPath(physicalSystemObservations(field), "S3").state).toBe("via F2");
    const cb1 = field.devices["breaker-1"];
    const tie = field.devices["rmu-2"];
    if (cb1.kind === "breaker") cb1.position = "closed";
    if (tie.kind === "rmu") tie.position = "closed";
    expect(sectionSupplyPath(physicalSystemObservations(field), "S3").state).toBe("via CB1 + F2");
    const f2 = field.devices["f2-breaker"];
    if (f2.kind === "breaker") f2.position = "open";
    if (cb1.kind === "breaker") cb1.position = "open";
    expect(sectionSupplyPath(physicalSystemObservations(field), "S3")).toMatchObject({ state: "unknown", reason: expect.stringContaining("contradicts") });
  });
});
