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
    expect(nodeTypeLabel("rmu")).toMatch(/RMU/);
    expect(nodeTypeLabel("oltc")).toMatch(/OLTC/);
    expect(NODE_LABELS["rmu-1"]).toContain("Sectionaliser");
    expect(NODE_LABELS["rmu-2"]).toContain("Tie");
    expect(NODE_LABELS["oltc-1"]).toBe("T1 OLTC");
    expect(NODE_LABELS["plc-1"]).toBe("OpenPLC");
  });
});
