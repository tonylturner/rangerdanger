import { expect, it } from "vitest";
import { act, create, type ReactTestInstance, type ReactTestRenderer } from "react-test-renderer";
import type { ReactElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { EU_ORACLE, PHYSICAL_PATH, ORACLE_SECTIONS, oracleFixture } from "../lib/eu-oracle.test-util";
import { controlCentreObservations, physicalSystemObservations } from "../lib/observation-readings";
import { SECTION_VOLTAGE } from "../lib/operator-supply-path";
import { FeederOneLine } from "./feeder-one-line";
import { AbnormalSummary } from "./control-centre-view";
import { PhysicalSystemScreen } from "./physical-system";

function inspect(node: ReactElement, check: (root: ReactTestInstance) => void) {
  let tree!: ReactTestRenderer;
  act(() => { tree = create(node); });
  try { check(tree.root); } finally { act(() => tree.unmount()); }
}
const hook = (root: ReactTestInstance, attribute: string, value?: string) =>
  root.findAll((n) => typeof n.type === "string" && (value === undefined ? n.props[attribute] !== undefined : n.props[attribute] === value));

it.each(EU_ORACLE.flatMap((row) => [false, true].map((physical) => ({ ...row, physical, row }))))(
  "$switches / F2 $f2 / $supply / VT $vt / physical=$physical: exposes oracle states and values through the exact DOM contract",
  ({ row, physical }) => {
    const { field, operator, kv, live } = oracleFixture(row);
    const read = physical ? physicalSystemObservations(field) : controlCentreObservations(operator.points);
    inspect(<FeederOneLine read={read} physical={physical} />, (root) => {
      expect(hook(root, "data-section")).toHaveLength(3);
      for (const [i, section] of ORACLE_SECTIONS.entries()) {
        const node = hook(root, "data-section", section)[0];
        const expectedPath = PHYSICAL_PATH[row.supply.split("/")[i] as keyof typeof PHYSICAL_PATH];
        expect(node.props).toMatchObject({
          "data-state": live[i] ? "LIVE" : "DEAD", "data-path": expectedPath, "data-kv": kv[i],
        });
        const value = hook(root, "data-point", SECTION_VOLTAGE[section])[0];
        expect(value.type).toBe("text");
        expect(value.props["data-state"]).toBe(live[i] ? "LIVE" : "DEAD");
        expect(value.children.join("")).toContain("kV");
        expect(node.findAllByType("text")[0].children.join("")).toContain(live[i] ? "LIVE" : "DEAD");
      }
      expect(hook(root, "data-device")).toHaveLength(5);
      for (const [i, device] of ["CB1", "RMU1", "RMU2"].entries()) {
        expect(hook(root, "data-device", device)[0].props).toMatchObject({
          "data-position": row.switches[i] === "C" ? "closed" : "open", "data-quality": "good",
        });
      }
      expect(hook(root, "data-device", "T1")[0].props).toMatchObject({ "data-tap": 0, "data-quality": "good" });
      expect(hook(root, "data-device", "F2")[0].props).toMatchObject({ "data-position": row.f2 === "tripped" ? "open" : "closed", "data-quality": "good" });
      expect(hook(root, "data-f2-line")[0].props["data-state"]).toBe(live[2] ? "LIVE" : "DEAD");
      expect(hook(root, "data-point").map((n) => n.props["data-point"]).sort()).toEqual([
        "oltc-1-current", "oltc-1-bus-kv", "feeder-current", "feeder-voltage", "rmu-1-current", "rmu-2-current",
        "rmu-1-source-kv", "rmu-1-section-kv", "rmu-2-source-kv", "rmu-2-section-kv",
      ].sort());
      expect(hook(root, "data-point", "feeder-voltage")[0].props["data-state"]).toBe(row.vt === 0 ? "DEAD" : "LIVE");
      if ("fault" in row) expect(hook(root, "data-point", "rmu-1-source-kv")[0].children.join("")).toContain("0.00 kV");
    });
    inspect(<AbnormalSummary state={operator} />, (root) => {
      const expected = row.f2 === "tripped" ? ["f2-protection-trip"] : [];
      if (row.switches[0] === "O") expected.push("feeder-breaker-position-off-normal");
      if (row.switches[1] === "O") expected.push("rmu-1-position-off-normal");
      if (row.switches[2] === "C") expected.push("rmu-2-position-off-normal");
      row.supply.split("/").forEach((s, i) => { if (s === "D") expected.push(`${ORACLE_SECTIONS[i]}-no-supply`); });
      if ("fault" in row) expected.push("feeder-voltage-voltage-limit", "rmu-2-source-kv-voltage-limit");
      const conditions = hook(root, "data-condition");
      expect(conditions.map((c) => c.props["data-condition"]).sort()).toEqual(expected.length ? expected.sort() : ["none"]);
      expect(conditions.every((c) => c.type === (expected.length ? "li" : "span"))).toBe(true);
    });
    const physicalPage = renderToStaticMarkup(<PhysicalSystemScreen field={field} stateError={null} operator={operator} now={0} />);
    expect(physicalPage).toContain('aria-label="Physical System state"');
    expect(physicalPage.includes("No active physical abnormalities")).toBe(row.switches === "CCO" && row.f2 === "normal" && !("fault" in row));
  },
);

it.each(["stale", "unknown"] as const)("exports CC %s states without turning retained data into GOOD evidence", (condition) => {
  const { operator } = oracleFixture(EU_ORACLE[12]);
  operator.points = operator.points.map((p) => condition === "unknown" ? { ...p, value: null } : { ...p, quality: { ...p.quality, nt: true } });
  inspect(<FeederOneLine read={controlCentreObservations(operator.points)} />, (root) => {
    for (const section of hook(root, "data-section")) {
      expect(section.props).toMatchObject({ "data-state": "UNKNOWN", "data-path": "unknown", "data-kv": condition === "unknown" ? "" : 19.9 });
    }
    for (const device of ["CB1", "RMU1", "RMU2", "T1", "F2"]) expect(hook(root, "data-device", device)[0].props["data-quality"]).toBe(condition);
    expect(hook(root, "data-device", "CB1")[0].props["data-position"]).toBe(condition === "unknown" ? "unknown" : "closed");
    expect(hook(root, "data-device", "T1")[0].props["data-tap"]).toBe(condition === "unknown" ? "" : 0);
    expect(hook(root, "data-point", "feeder-voltage")[0].props["data-state"]).toBe("UNKNOWN");
  });
});

it("exports stale physical measurements but still GOOD physical switch positions", () => {
  const { field } = oracleFixture(EU_ORACLE[15]);
  field.process.truth_stale = true;
  inspect(<FeederOneLine physical read={physicalSystemObservations(field)} />, (root) => {
    expect(hook(root, "data-section").every((n) => n.props["data-state"] === "UNKNOWN" && n.props["data-path"] === "unknown")).toBe(true);
    expect(hook(root, "data-device", "CB1")[0].props).toMatchObject({ "data-position": "closed", "data-quality": "good" });
    expect(hook(root, "data-device", "F2")[0].props["data-position"]).toBe("open");
    expect(hook(root, "data-f2-line")[0].props["data-state"]).toBe("UNKNOWN");
  });
});

it.each(["intermediate", "indeterminate"] as const)("exports the received %s switch position", (value) => {
  const { operator } = oracleFixture(EU_ORACLE[12]);
  operator.points.find((p) => p.id === "rmu-1-position")!.value = value;
  inspect(<FeederOneLine read={controlCentreObservations(operator.points)} />, (root) => {
    expect(hook(root, "data-device", "RMU1")[0].props).toMatchObject({ "data-position": value, "data-quality": "good" });
  });
});

it.each([NaN, Infinity, -1])("exports invalid numeric section kV %s as UNKNOWN, never DEAD or LIVE", (value) => {
  const { operator } = oracleFixture(EU_ORACLE[12]);
  operator.points.find((p) => p.id === "rmu-1-source-kv")!.value = value;
  inspect(<FeederOneLine read={controlCentreObservations(operator.points)} />, (root) => {
    expect(hook(root, "data-section", "S1")[0].props).toMatchObject({
      "data-state": "UNKNOWN", "data-path": "unknown", "data-kv": Number.isFinite(value) ? value : "",
    });
  });
});
