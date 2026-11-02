// @vitest-environment jsdom
import { render } from "@testing-library/react";
import { expect, it } from "vitest";
import type { ReactElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { EU_ORACLE, PHYSICAL_PATH, ORACLE_SECTIONS, oracleFixture } from "../lib/eu-oracle.test-util";
import { controlCentreObservations, physicalSystemObservations } from "../lib/observation-readings";
import { SECTION_VOLTAGE } from "../lib/operator-supply-path";
import { FeederOneLine } from "./feeder-one-line";
import { AbnormalSummary } from "./control-centre-view";
import { PhysicalSystemScreen } from "./physical-system";

function inspect(node: ReactElement, check: (root: HTMLElement) => void) {
  const tree = render(node);
  try { check(tree.container); } finally { tree.unmount(); }
}
const hook = (root: ParentNode, attribute: string, value?: string) =>
  [...root.querySelectorAll<SVGElement>(`[${attribute}]`)].filter((node) => value === undefined || node.getAttribute(attribute) === value);

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
        expect(node.getAttribute("data-state")).toBe(live[i] ? "LIVE" : "DEAD");
        expect(node.getAttribute("data-path")).toBe(expectedPath);
        expect(node.getAttribute("data-kv")).toBe(String(kv[i]));
        const value = hook(root, "data-point", SECTION_VOLTAGE[section])[0];
        expect(value.tagName.toLowerCase()).toBe("text");
        expect(value.getAttribute("data-state")).toBe(live[i] ? "LIVE" : "DEAD");
        expect(value.textContent).toContain("kV");
        expect(node.querySelector("text")?.textContent).toContain(live[i] ? "LIVE" : "DEAD");
      }
      expect(hook(root, "data-device")).toHaveLength(5);
      for (const [i, device] of ["CB1", "RMU1", "RMU2"].entries()) {
        const deviceNode = hook(root, "data-device", device)[0];
        expect(deviceNode.getAttribute("data-position")).toBe(row.switches[i] === "C" ? "closed" : "open");
        expect(deviceNode.getAttribute("data-quality")).toBe("good");
        expect(deviceNode.getAttribute("data-off-normal")).toBe(row.switches[i] !== "CCO"[i] ? "true" : "false");
      }
      const t1 = hook(root, "data-device", "T1")[0];
      expect(t1.getAttribute("data-tap")).toBe("0");
      expect(t1.getAttribute("data-quality")).toBe("good");
      expect(t1.getAttribute("data-off-normal")).toBe("false");
      const f2 = hook(root, "data-device", "F2")[0];
      expect(f2.getAttribute("data-position")).toBe(row.f2 === "tripped" ? "open" : "closed");
      expect(f2.getAttribute("data-quality")).toBe("good");
      expect(f2.getAttribute("data-off-normal")).toBe(row.f2 === "tripped" ? "true" : "false");
      expect(hook(root, "data-f2-line")[0].getAttribute("data-state")).toBe(live[2] ? "LIVE" : "DEAD");
      const badges = hook(root, "data-condition", "asserted");
      const expectedBadges = ORACLE_SECTIONS.filter((_, i) => !live[i]).map((s) => `${s}-no-supply`);
      if (row.f2 === "tripped") expectedBadges.push("f2-protection-trip", "f2-lockout");
      expect(badges.map((n) => n.getAttribute("data-point")).sort()).toEqual(expectedBadges.sort());
      for (const badge of badges) {
        expect(badge.tagName.toLowerCase()).toBe("span");
        expect(badge.textContent).toBe(badge.getAttribute("data-state"));
        expect(badge.className).toContain("text-rose-200");
      }
      expect(hook(root, "data-point").filter((n) => n.tagName.toLowerCase() === "text").map((n) => n.getAttribute("data-point")).sort()).toEqual([
        "oltc-1-current", "oltc-1-bus-kv", "feeder-current", "feeder-voltage", "rmu-1-current", "rmu-2-current",
        "rmu-1-source-kv", "rmu-1-section-kv", "rmu-2-source-kv", "rmu-2-section-kv",
      ].sort());
      expect(hook(root, "data-point", "feeder-voltage")[0].getAttribute("data-state")).toBe(row.vt === 0 ? "DEAD" : "LIVE");
      if ("fault" in row) expect(hook(root, "data-point", "rmu-1-source-kv")[0].textContent).toContain("0.00 kV");
    });
    inspect(<AbnormalSummary state={operator} />, (root) => {
      const expected = row.f2 === "tripped" ? ["f2-protection-trip", "f2-lockout", "f2-position-off-normal"] : [];
      if (row.switches[0] === "O") expected.push("feeder-breaker-position-off-normal");
      if (row.switches[1] === "O") expected.push("rmu-1-position-off-normal");
      if (row.switches[2] === "C") expected.push("rmu-2-position-off-normal");
      row.supply.split("/").forEach((s, i) => { if (s === "D") expected.push(`${ORACLE_SECTIONS[i]}-no-supply`); });
      if ("fault" in row) expected.push("feeder-voltage-voltage-limit", "rmu-2-source-kv-voltage-limit");
      const conditions = hook(root, "data-condition");
      expect(conditions.map((c) => c.getAttribute("data-condition")).sort()).toEqual(expected.length ? expected.sort() : ["none"]);
      expect(conditions.every((c) => c.tagName.toLowerCase() === (expected.length ? "li" : "span"))).toBe(true);
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
      expect(section.getAttribute("data-state")).toBe("UNKNOWN");
      expect(section.getAttribute("data-path")).toBe("unknown");
      expect(section.getAttribute("data-kv")).toBe(condition === "unknown" ? "" : "19.9");
    }
    for (const device of ["CB1", "RMU1", "RMU2", "T1", "F2"]) {
      expect(hook(root, "data-device", device)[0].getAttribute("data-quality")).toBe(condition);
      expect(hook(root, "data-device", device)[0].getAttribute("data-off-normal")).toBe("false");
    }
    expect(hook(root, "data-device", "CB1")[0].getAttribute("data-position")).toBe(condition === "unknown" ? "unknown" : "closed");
    expect(hook(root, "data-device", "T1")[0].getAttribute("data-tap")).toBe(condition === "unknown" ? "" : "0");
    expect(hook(root, "data-point", "feeder-voltage")[0].getAttribute("data-state")).toBe("UNKNOWN");
    expect(hook(root, "data-condition", "asserted")).toHaveLength(0);
  });
});

it.each([
  ["feeder-protection-trip", true, "TRIPPED"],
  ["feeder-lockout", true, "LOCKED OUT"],
  ["f2-protection-trip", true, "TRIPPED"],
  ["f2-lockout", true, "LOCKED OUT"],
  ["rmu-1-fault-passage", true, "FAULT PASSED"],
  ["rmu-2-fault-passage", true, "FAULT PASSED"],
  ["rmu-1-earth", true, "EARTHED"],
  ["rmu-2-earth", true, "EARTHED"],
  ["oltc-1-at-lower", true, "AT LIMIT"],
  ["oltc-1-at-upper", true, "AT LIMIT"],
  ["oltc-1-blocked", true, "BLOCKED"],
  ["oltc-1-overcurrent-block", true, "BLOCKED"],
  ["plc-1-running", false, "PLC DOWN"],
  ["plc-1-input-valid", false, "PLC DOWN"],
] as const)("shows %s as %s only from usable IEC evidence", (id, value, label) => {
  const { operator } = oracleFixture(EU_ORACLE[12]);
  const point = operator.points.find((p) => p.id === id)!;
  point.value = value;
  inspect(<FeederOneLine read={controlCentreObservations(operator.points)} />, (root) => {
    const badge = hook(root, "data-point", id)[0];
    expect(badge.textContent).toBe(label);
    expect(badge.getAttribute("data-state")).toBe(label);
    expect(badge.getAttribute("aria-label")).toContain("IEC 104 observation");
  });
  for (const flag of ["iv", "nt", "sb", "bl", "ov"] as const) {
    const points = operator.points.map((p) => p.id === id ? { ...p, quality: { ...p.quality, [flag]: true } } : p);
    inspect(<FeederOneLine read={controlCentreObservations(points)} />, (root) => {
      expect(hook(root, "data-point", id)).toHaveLength(0);
    });
  }
});

it("uses independent physical protection, earth and FPI latches, never CC flags", () => {
  const { field } = oracleFixture(EU_ORACLE[12]);
  const cb1 = field.devices["breaker-1"], f2 = field.devices["f2-breaker"], rmu = field.devices["rmu-1"];
  if (cb1.kind !== "breaker" || f2.kind !== "breaker" || rmu.kind !== "rmu") throw new Error("Expected device kinds");
  cb1.protection_tripped = true;
  cb1.lockout = true;
  f2.lockout = true; // Lockout is independent of the trip indication.
  rmu.earth_closed = true;
  rmu.fault_passage = true;
  field.devices["oltc-1"].protection.picked_up = true;
  inspect(<FeederOneLine physical read={physicalSystemObservations(field)} />, (root) => {
    expect(hook(root, "data-condition", "asserted").map((n) => n.textContent).sort()).toEqual(
      ["TRIPPED", "LOCKED OUT", "LOCKED OUT", "EARTHED", "FAULT PASSED", "BLOCKED"].sort(),
    );
    expect(hook(root, "data-point", "f2-protection-trip")).toHaveLength(0);
    expect(hook(root, "data-point", "feeder-lockout")[0].getAttribute("aria-label")).toContain("sim");
  });
  inspect(<FeederOneLine physical read={physicalSystemObservations(field, true)} />, (root) => {
    expect(hook(root, "data-condition", "asserted")).toHaveLength(0);
  });
});

it("exports stale physical measurements but still GOOD physical switch positions", () => {
  const { field } = oracleFixture(EU_ORACLE[15]);
  field.process.truth_stale = true;
  inspect(<FeederOneLine physical read={physicalSystemObservations(field)} />, (root) => {
    expect(hook(root, "data-section").every((n) => n.getAttribute("data-state") === "UNKNOWN" && n.getAttribute("data-path") === "unknown")).toBe(true);
    expect(hook(root, "data-device", "CB1")[0].getAttribute("data-position")).toBe("closed");
    expect(hook(root, "data-device", "CB1")[0].getAttribute("data-quality")).toBe("good");
    expect(hook(root, "data-device", "F2")[0].getAttribute("data-position")).toBe("open");
    expect(hook(root, "data-f2-line")[0].getAttribute("data-state")).toBe("UNKNOWN");
  });
});

it.each(["intermediate", "indeterminate"] as const)("exports the received %s switch position", (value) => {
  const { operator } = oracleFixture(EU_ORACLE[12]);
  operator.points.find((p) => p.id === "rmu-1-position")!.value = value;
  inspect(<FeederOneLine read={controlCentreObservations(operator.points)} />, (root) => {
    expect(hook(root, "data-device", "RMU1")[0].getAttribute("data-position")).toBe(value);
    expect(hook(root, "data-device", "RMU1")[0].getAttribute("data-quality")).toBe("good");
    expect(hook(root, "data-device", "RMU1")[0].getAttribute("data-off-normal")).toBe("true");
  });
});

it.each([false, true])("exports MANUAL and LOCAL off-normal in physical=%s without conflating tap or position", (physical) => {
  const { field, operator } = oracleFixture(EU_ORACLE[12]);
  operator.points.find((p) => p.id === "oltc-1-auto")!.value = false;
  operator.points.find((p) => p.id === "rmu-1-remote")!.value = false;
  const t1 = field.devices["oltc-1"];
  if (t1.kind !== "oltc") throw new Error("Expected OLTC fixture");
  t1.auto = false;
  field.devices["rmu-1"].remote_enabled = false;
  const read = physical ? physicalSystemObservations(field) : controlCentreObservations(operator.points);
  inspect(<FeederOneLine read={read} physical={physical} />, (root) => {
    const transformer = hook(root, "data-device", "T1")[0];
    expect(transformer.getAttribute("data-off-normal")).toBe("true");
    expect(transformer.getAttribute("data-tap")).toBe("0");
    expect([...transformer.querySelectorAll("circle")].every((n) => n.getAttribute("stroke") === "#fbbf24" && n.getAttribute("stroke-dasharray") === null)).toBe(true);
    expect(hook(root, "data-device", "RMU1")[0].getAttribute("data-off-normal")).toBe("true");
    expect(hook(root, "data-device", "RMU1")[0].getAttribute("data-position")).toBe("closed");
    expect(hook(root, "data-device", "CB1")[0].getAttribute("data-off-normal")).toBe("false");
    expect(hook(root, "data-device", "F2")[0].getAttribute("data-off-normal")).toBe("false");
  });
});

it.each([NaN, Infinity, -1])("exports invalid numeric section kV %s as UNKNOWN, never DEAD or LIVE", (value) => {
  const { operator } = oracleFixture(EU_ORACLE[12]);
  operator.points.find((p) => p.id === "rmu-1-source-kv")!.value = value;
  inspect(<FeederOneLine read={controlCentreObservations(operator.points)} />, (root) => {
    expect(hook(root, "data-section", "S1")[0].getAttribute("data-state")).toBe("UNKNOWN");
    expect(hook(root, "data-section", "S1")[0].getAttribute("data-path")).toBe("unknown");
    expect(hook(root, "data-section", "S1")[0].getAttribute("data-kv")).toBe(Number.isFinite(value) ? String(value) : "");
  });
});
