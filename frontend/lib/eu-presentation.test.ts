import { describe, expect, it } from "vitest";
import { NODE_LABELS } from "./exercise-nodes";
import { humanZoneName, humanizeSummary, zones } from "./network-console-data";
import { nodeTypeLabel } from "./node-types";
import { zoneColors } from "./zone-colors";

// The network map and runner vocabulary the EU IEC 104 package needs.
describe("EU presentation vocabulary", () => {
  it("labels IEC 104 on port 2404", () => {
    expect(humanizeSummary("2404, 502")).toEqual(["IEC 104", "Modbus"]);
  });

  it("colours the lan4 control-centre zone and keeps the EU zones on the map", () => {
    expect(zoneColors.lan4).toEqual(zoneColors.control_centre_net);
    expect(zoneColors.station_net).toEqual(zoneColors.lan1);
    expect(zones).toContain("station_net");
    expect(zones).toContain("control_centre_net");
    expect(zones).toContain("ops_dmz_net");
    expect(zones).toContain("lan5");
    expect(zoneColors.lan5).toEqual(zoneColors.ops_dmz_net);
    expect(humanZoneName("lan5")).toBe("Operations DMZ");
    expect(humanizeSummary("8081")).toEqual(["HTTP ingest"]);
  });

  it("names the EU node types and nodes", () => {
    expect(nodeTypeLabel("control_centre")).toMatch(/Control centre/);
    expect(nodeTypeLabel("iec104_rtu")).toMatch(/IEC 104 RTU/);
    expect(nodeTypeLabel("feeder_breaker")).toMatch(/Feeder breaker/);
    expect(nodeTypeLabel("rtac_sim")).toBe("rtac_sim");
    expect(NODE_LABELS["cc-1"]).toBe("Control Centre");
    expect(NODE_LABELS["rtu-1"]).toBe("IEC 104 RTU");
    expect(NODE_LABELS["breaker-1"]).toBe("Feeder Breaker");
    expect(NODE_LABELS["f2-breaker"]).toBe("F2 Feeder Breaker");
    expect(nodeTypeLabel("rmu")).toMatch(/RMU/);
    expect(nodeTypeLabel("oltc")).toMatch(/OLTC/);
    expect(NODE_LABELS["rmu-1"]).toContain("Sectionaliser");
    expect(NODE_LABELS["rmu-2"]).toContain("Tie");
    expect(NODE_LABELS["oltc-1"]).toBe("T1 OLTC");
    expect(NODE_LABELS["plc-1"]).toBe("OpenPLC");
  });
});

import { fixtureCurriculum, US_CURRICULUM } from "./curriculum-storage.test-util";
import { buildContaindConfig, buildDynamicPlan, loadDynamicPlan } from "./remediation-to-rules";
import { actionEffectLabel } from "./remediation-plan";
import { computeCoverage } from "./requirement-coverage";
import { injectDynamicContent, showsValidateButton } from "./scenario-runner-logic";
import { getExerciseNodes, inferNodeFromDescription } from "./exercise-nodes";
import type { CurriculumPolicyRule } from "./api";

const EU = fixtureCurriculum("lab-definitions/packages/eu-iec104-substation");
function euPlan(ids: string[], binding = EU) {
  return buildDynamicPlan({ exerciseId: binding.roles.planning, selectedActionIds: ids, savedAt: "" }, binding);
}
function euConfig(ids: string[], binding = EU) {
  return buildContaindConfig(euPlan(ids, binding)) as {
    interfaces: { name: string }[];
    firewall: { defaultAction: string; rules: CurriculumPolicyRule[] };
  };
}

describe("EU curriculum example — shared machinery, EU facts", () => {
  const ids = EU.catalog.slice(0, 8).map(a => a.id);
  it("keeps the frozen catalog, budget and teams", () => {
    expect(EU.budgetHours).toBe(500);
    expect(EU.teams).toEqual(US_CURRICULUM.teams);
    expect(EU.catalog.map(a => [a.id, a.effort_hours])).toEqual([
      ["block-enterprise-to-field", 40], ["block-enterprise-to-station", 30],
      ["restrict-vendor-to-station", 50], ["block-vendor-to-field", 30],
      ["pin-rtu-to-field", 40], ["modbus-dpi", 70], ["iec104-dpi", 80],
      ["improve-logging", 50], ["station-architecture-review", 30],
      ["tighten-firewall-objects", 40], ["positive-validation", 60],
      ["ids-deployment", 80], ["ot-asset-inventory", 100], ["vendor-zta-broker", 150],
    ]);
  });

  it("all 256 EU combinations preserve six pinned conduits and forbid every ics key", () => {
    for (let mask = 0; mask < 256; mask++) {
      const selected = ids.filter((_, i) => mask & (1 << i));
      const config = euConfig(selected);
      const wire = JSON.stringify(config);
      expect(config.firewall.defaultAction).toBe("DENY");
      expect(config.interfaces.map(i => i.name)).toEqual(["wan", "dmz", "lan1", "lan2", "lan4", "lan5", "lan3"]);
      expect(wire).not.toMatch(/"ics"\s*:/);
      expect(wire).not.toMatch(/RTAC|DNP3|20000|gps-to-field/i);
      expect(config.firewall.rules.slice(0, 6)).toMatchObject([
        { id: "control-centre-to-rtu-iec104", sources: ["10.60.60.20/32"], destinations: ["10.30.30.20/32"] },
        { id: "rtu-to-feeder-modbus", sources: ["10.30.30.20/32"], destinations: ["10.40.40.20/32", "10.40.40.24/32", "10.40.40.21/32", "10.40.40.22/32", "10.40.40.23/32"] },
        { id: "plc-to-oltc-modbus", sources: ["10.30.30.30/32"], destinations: ["10.40.40.23/32"] },
        { id: "control-centre-to-historian-ingest", protocols: [{ name: "tcp", port: "8081" }] },
        { id: "enterprise-to-historian-read", protocols: [{ name: "tcp", port: "8080" }] },
        { id: "engineering-to-plc-web", sources: ["10.20.20.20/32"], destinations: ["10.30.30.30/32"] },
      ]);
      for (const id of ["block-enterprise-to-field", "block-enterprise-to-station", "restrict-vendor-to-station", "block-vendor-to-field", "pin-rtu-to-field"]) {
        expect(config.firewall.rules.some(r => r.id === `weak-${id}`), `${id} deferred in ${mask}`).toBe(!selected.includes(id));
        expect(config.firewall.rules.some(r => r.id === `deny-${id}`), `${id} selected in ${mask}`).toBe(selected.includes(id));
      }
    }
  });

  it("requires both enterprise and vendor field closures for the combined EU requirement", () => {
    const req = [{ id: "enterprise-to-field", label: "Enterprise/vendor field", verdict: "BLOCK" }];
    expect(computeCoverage(req, new Set(["block-enterprise-to-field"]), EU)[0]).toMatchObject({ status: "partial", missingActions: ["block-vendor-to-field"] });
    expect(computeCoverage(req, new Set(["block-enterprise-to-field", "block-vendor-to-field"]), EU)[0]).toMatchObject({ status: "covered" });
  });

  it("keeps semantic and conceptual selections honest in summary and coverage", () => {
    const plan = euPlan(["iec104-dpi", "modbus-dpi", "ids-deployment"]);
    expect(plan.selectedSummary[0]).toMatchObject({ hasRules: false, description: expect.stringContaining("planned, not enforceable in this range yet") });
    expect(plan.selectedSummary[2]).toMatchObject({ hasRules: false, description: expect.stringContaining("planning-only") });
    expect(actionEffectLabel(EU, "iec104-dpi")).toBe("planned, not enforceable in this range yet");
    const coverage = computeCoverage([{ id: "unauth-control", label: "Control", verdict: "BLOCK" }], new Set(plan.selectedActions), EU);
    expect(coverage[0]).toMatchObject({ status: "n/a", reason: "planned, not enforceable in this range yet" });
    expect(euConfig(["ids-deployment"])).toEqual(euConfig([]));
    const allowed = { ...EU, icsRules: "allowed" as const };
    expect(euConfig(["iec104-dpi"], allowed).firewall.rules[0].ics).toEqual({ protocol: "iec104", readOnly: true, mode: "enforce" });
    expect(euConfig(["modbus-dpi"], allowed).firewall.rules[1].ics).toEqual({ protocol: "modbus", functionCode: [3, 4, 6, 16] });
  });

  it("uses stable EU step IDs and planning-role deep links without title matching", () => {
    const plan = buildDynamicPlan(null, EU);
    expect(injectDynamicContent("original", "implement-rules", plan)).toContain("/exercises/eu-remediation-planning");
    expect(injectDynamicContent("original", "Phase 3", plan)).toBe("original");
    expect(injectDynamicContent("original", "test-positive", plan)).toContain("CC→RTU:2404");
    expect(getExerciseNodes(EU.roles.hardening, undefined, EU)).toEqual(["kali-1"]);
    expect(inferNodeFromDescription("from the RTU", EU)).toBe("rtu-1");
    expect(inferNodeFromDescription("RTAC 10.30.30.20")).toBeNull();
  });

  it("has no planner or US node fallback without a binding; Validate follows the declared validator", () => {
    expect(loadDynamicPlan({ packageId: "unbound", revision: 1 })).toBeNull();
    expect(getExerciseNodes("baseline-assessment")).toEqual([]);
    expect(showsValidateButton({ id: EU.roles.planning, package_id: "eu", validator: "" }, EU)).toBe(false);
    expect(showsValidateButton({ id: EU.roles.implementation, package_id: "eu", validator: "eu-check" }, EU)).toBe(true);
    // No package-ID special case: even a US-looking ID without a binding
    // cannot activate planning machinery or override a validator declaration.
    expect(showsValidateButton({ id: "baseline-assessment", package_id: "us-dnp3-substation", validator: "check" })).toBe(true);
  });
});
