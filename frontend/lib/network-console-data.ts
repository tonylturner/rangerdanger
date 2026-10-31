import type { LabGraph, TopologyNetwork, ZoneRuleSummary } from "./api";
import { nodeZone } from "./observed-flows";

// All supported zone names
export const zones = [
  "enterprise_net", "vendor_net", "ot_ops_net", "field_net",
  // EU IEC 104 package zones
  "station_net", "control_centre_net", "ops_dmz_net", "lan5",
  // Legacy zone names
  "wan", "dmz", "ot_control", "ot_safety", "it_workstations",
  "it_net", "dmz_net", "ot_control_net", "ot_safety_net"
] as const;

// Map well-known TCP/UDP ports to the protocol students recognize from
// the workshop. The ICS-relevant ports (Modbus, DNP3) are first since
// they're the load-bearing labels in this lab.
export const PORT_PROTOCOL: Record<string, string> = {
  "502": "Modbus",
  "20000": "DNP3",
  "2404": "IEC 104",
  "1881": "FUXA",
  "8080": "HTTP",
  "8081": "HTTP ingest",
  "8443": "HTTPS",
  "80": "HTTP",
  "443": "HTTPS",
  "22": "SSH",
  "23": "Telnet",
  "3389": "RDP",
  "445": "SMB",
  "123": "NTP",
  "53": "DNS",
};

export function humanizeSummary(summary: string): string[] {
  // containd's summary string is "502, 20000, 8080" or "8080 +4 more".
  // Strip the "+N more" tail (we surface that in the tooltip) and look up
  // each port. Anything we don't recognize is shown as "tcp/<port>".
  const cleaned = summary.replace(/\s*\+\d+\s*more\s*$/i, "").trim();
  if (!cleaned) return [];
  return cleaned
    .split(/[,\s]+/)
    .filter(Boolean)
    .map((tok) => PORT_PROTOCOL[tok] || `tcp/${tok}`);
}

// View modes that toggle overlays on the map without changing the
// underlying topology. Each toggle is independent so a student can
// stack policy + traffic to see "what's allowed AND what's flowing".
export type ViewMode = {
  policyDim: boolean; // color edges by firewall action
  traffic: boolean;   // overlay observed host-to-host flows
  iec62443: boolean;  // Purdue Model horizontal-band layout (legacy field name kept for state-shape stability)
};

// Map containd's internal zone names back to a human-friendly label.
// Used by the inspector so the student doesn't see "lan1 → lan2" but
// "OT Ops → Field Devices".
export const ZONE_HUMAN_NAME: Record<string, string> = {
  enterprise_net: "Enterprise",
  vendor_net: "Vendor / DMZ",
  ot_ops_net: "OT Ops",
  field_net: "Field Devices",
  station_net: "Station",
  control_centre_net: "Control Centre",
  ops_dmz_net: "Operations DMZ",
  lan4: "Control Centre",
  lan5: "Operations DMZ",
  wan: "Enterprise",
  dmz: "Vendor / DMZ",
  lan1: "OT Ops",
  lan2: "Field Devices",
  any: "any zone",
};

export function humanZoneName(z: string): string {
  return ZONE_HUMAN_NAME[z] || z;
}
export type FlowAuthorization = {
  state: "unknown" | "denied" | "same-zone";
  text: string;
};

// A zone summary is not an endpoint/port policy evaluation. In particular,
// MIXED cannot establish a source pin, even for the traditional US RTAC.
export function getFlowAuthorization(
  sourceNodeId: string,
  targetNodeId: string,
  ruleSummaries: ZoneRuleSummary[] | undefined,
  graph: LabGraph,
): FlowAuthorization {
  const unknown: FlowAuthorization = { state: "unknown", text: "Authorization unknown — no matching zone-pair summary or endpoint/port evaluation." };
  const srcZone = graph.networks.find((n) => n.id === nodeZone(sourceNodeId, graph))?.zone;
  const dstZone = graph.networks.find((n) => n.id === nodeZone(targetNodeId, graph))?.zone;
  if (!srcZone || !dstZone) return unknown;
  if (srcZone === dstZone) return { state: "same-zone", text: "Same zone — does not traverse containd; firewall policy does not establish authorization." };

  const rule = ruleSummaries?.find(
    (r) => r.source_zone === srcZone && r.dest_zone === dstZone,
  );

  if (!rule) return unknown;
  if (rule.action === "DENY") {
    const detail = rule.rule_details?.[0] || `${srcZone} → ${dstZone}: DENY`;
    return { state: "denied", text: `Denied by zone-pair summary — ${detail}` };
  }
  return { state: "unknown", text: `Authorization unknown — ${rule.action} zone-pair summary, not an endpoint/port evaluation. ${rule.rule_details.join("; ")}` };
}

// Build policy info for a zone from the live containd rules. The label
// favors protocol names over raw port numbers - students don't need to
// know that 502 is Modbus, they need to know "Modbus is allowed."
export type Permissiveness = "allowed" | "over" | "blocked" | "unknown";

export function getZonePolicyInfo(
  zone: string,
  ruleSummaries: ZoneRuleSummary[] | undefined,
  networks: TopologyNetwork[],
): {
  label: string;
  details: string[];
  action: string;
  permissiveness: Permissiveness;
} {
  if (!ruleSummaries || ruleSummaries.length === 0) {
    return {
      label: "No policy data",
      details: [],
      action: "UNKNOWN",
      permissiveness: "unknown",
    };
  }

  // Find rules that apply to this zone (as destination from firewall)
  const containdZones = networks.filter((n) => n.id === zone).map((n) => n.zone);

  const relevantRules = ruleSummaries.filter(
    (r) => containdZones.includes(r.dest_zone) || r.dest_zone === "any",
  );

  if (relevantRules.length === 0) {
    return {
      label: "No inbound rules",
      details: [],
      action: "UNKNOWN",
      permissiveness: "unknown",
    };
  }

  // Aggregate action
  const hasAllow = relevantRules.some((r) => r.action === "ALLOW");
  const hasDeny = relevantRules.some((r) => r.action === "DENY");
  const action = relevantRules.some((r) => r.action === "MIXED") || (hasAllow && hasDeny) ? "MIXED" : hasDeny ? "DENY" : "ALLOW";

  // Detect over-permissive: any allow rule whose detail starts with
  // "WEAK:" - that's the lab definition's convention for "this rule
  // should be tightened in the improved policy". When the active
  // config is the weak baseline, those rules are still in effect and
  // should read visually as warnings.
  const overPermissive = relevantRules.some(
    (r) =>
      r.action !== "DENY" &&
      r.rule_details.some((d) => d.trim().toUpperCase().startsWith("WEAK:")),
  );
  const permissiveness: Permissiveness =
    action === "DENY" ? "blocked" : action === "MIXED" ? "unknown" : overPermissive ? "over" : "allowed";

  // Structured details for tooltip
  const details = relevantRules.map((r) => {
    const src = r.source_zone || "any";
    const dst = r.dest_zone || "any";
    const desc = r.rule_details.length > 0 ? r.rule_details[0] : r.summary;
    return `${r.action} ${src}→${dst}: ${desc}`;
  });

  // Collect unique protocol names across all relevant ALLOW rules.
  const protoSet = new Set<string>();
  relevantRules
    .filter((r) => r.action === "ALLOW")
    .forEach((r) => humanizeSummary(r.summary).forEach((p) => protoSet.add(p)));
  const protos = Array.from(protoSet);

  // Build a label tuned to teach the student what kind of conduit this
  // is. Compact and unambiguous - never "+1" / "+3".
  let label: string;
  if (action === "DENY") {
    label = "BLOCKED";
  } else if (action === "MIXED") {
    label = "MIXED · authorization unknown";
  } else if (protos.length === 0) {
    label = "ALLOW";
  } else if (protos.length === 1) {
    label = protos[0];
  } else if (protos.length <= 3) {
    label = protos.join(" / ");
  } else {
    // 4+ protocols - say so explicitly instead of dumping a +N tail.
    label = `Multiple flows (${protos.length})`;
  }

  return { label, details, action, permissiveness };
}

// Map a workshop graph node id to the runtime telemetry source that
// drives its status dot. Nodes not in the map don't get a dot - we
// only render dots for nodes whose health we actually probe.
export function deriveNodeHealth(
  nodeId: string,
  workshopOnline: { firewall?: boolean; rtac?: boolean; deviceComms?: Record<string, boolean> },
): { health?: "ok" | "down"; healthSource?: string } {
  if (nodeId === "fw-1") {
    return {
      health: workshopOnline.firewall ? "ok" : "down",
      healthSource: "containd /api/v1/health",
    };
  }
  if (nodeId === "rtac-1") {
    return {
      health: workshopOnline.rtac ? "ok" : "down",
      healthSource: "RTAC /api/state",
    };
  }
  const deviceMap: Record<string, string> = {
    "relay-1": "relay",
    "recloser-1": "recloser",
    "regulator-1": "regulator",
    "capbank-1": "capbank",
  };
  const dev = deviceMap[nodeId];
  if (dev) {
    const ok = workshopOnline.deviceComms?.[dev];
    return {
      health: ok ? "ok" : "down",
      healthSource: `RTAC device_comms[${dev}]`,
    };
  }
  return {};
}
