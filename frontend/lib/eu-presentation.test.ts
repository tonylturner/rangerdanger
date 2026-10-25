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
import { EU_PLANS } from "./plan-fixtures.test-util";
import { renderRuleTable, renderPlanTests, actionEffectLabel } from "./plan-presentation";
import { computeCoverage } from "./requirement-coverage";
import { injectDynamicContent, showsValidateButton } from "./scenario-runner-logic";
import { getExerciseNodes, inferNodeFromDescription } from "./exercise-nodes";

const EU = fixtureCurriculum("lab-definitions/packages/eu-iec104-substation");
describe("EU curriculum example — shared machinery, EU facts", () => {
  it("keeps the budget and teams", () => {
    expect(EU.budgetHours).toBe(500);
    expect(EU.teams).toEqual(US_CURRICULUM.teams);
  });
  it("preserves the existing catalog actions and effort estimates while allowing the new remote-access action", () => {
    expect(EU.catalog.map((a) => [a.id, a.effort_hours])).toEqual(expect.arrayContaining([
      ["block-enterprise-to-field", 40], ["block-enterprise-to-station", 30],
      ["restrict-vendor-to-station", 50], ["block-vendor-to-field", 30],
      ["pin-rtu-to-field", 40], ["modbus-dpi", 70], ["iec104-dpi", 80],
      ["improve-logging", 50], ["station-architecture-review", 30],
      ["tighten-firewall-objects", 40], ["positive-validation", 60],
      ["ids-deployment", 80], ["ot-asset-inventory", 100], ["vendor-zta-broker", 150],
    ]));
  });
  it.each(EU_PLANS)("renders server-owned $label rules without introducing ICS predicates", (plan) => {
    expect(JSON.stringify(plan.config)).not.toMatch(/"ics"\s*:/);
    expect(renderRuleTable(plan, EU)).toContain("operational-pin");
    expect(renderPlanTests(plan, EU, "hardening", "negative")).toContain("TCP/3389");
    expect(actionEffectLabel(plan.actions.find((a) => a.id === "iec104-dpi")!.kind)).toBe("planning only: no firewall change");
  });

  it("requires both enterprise and vendor field closures for the combined EU requirement", () => {
    const req = [{ id: "enterprise-to-field", label: "Enterprise/vendor field", verdict: "BLOCK" }];
    expect(computeCoverage(req, new Set(["block-enterprise-to-field"]), EU)[0]).toMatchObject({ status: "partial", missingActions: ["block-vendor-to-field"] });
    expect(computeCoverage(req, new Set(["block-enterprise-to-field", "block-vendor-to-field"]), EU)[0]).toMatchObject({ status: "covered" });
  });

  it("uses stable EU step IDs and planning-role deep links without title matching", () => {
    const plan = EU_PLANS[0];
    expect(injectDynamicContent("original", "implement-rules", plan, EU)).toContain("/exercises/eu-remediation-planning");
    expect(injectDynamicContent("original", "Phase 3", plan, EU)).toBe("original");
    expect(injectDynamicContent("original", "test-positive", plan, EU)).toContain("Plan verdict");
    expect(getExerciseNodes(EU.roles.hardening, undefined, EU)).toEqual(["kali-1"]);
    expect(inferNodeFromDescription("from the RTU", EU)).toBe("rtu-1");
    expect(inferNodeFromDescription("RTAC 10.30.30.20")).toBeNull();
  });

  it("has no planner or US node fallback without a binding; Validate follows the declared validator", () => {
    expect(getExerciseNodes("baseline-assessment")).toEqual([]);
    expect(showsValidateButton({ id: EU.roles.planning, package_id: "eu", validator: "" }, EU)).toBe(false);
    expect(showsValidateButton({ id: EU.roles.implementation, package_id: "eu", validator: "eu-check" }, EU)).toBe(true);
    // No package-ID special case: even a US-looking ID without a binding
    // cannot activate planning machinery or override a validator declaration.
    expect(showsValidateButton({ id: "baseline-assessment", package_id: "us-dnp3-substation", validator: "check" })).toBe(true);
  });
});
