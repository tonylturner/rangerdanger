import type { ZoneRuleSummary } from "./api";
import { nodeZone } from "./observed-flows";

// All supported zone names
export const zones = [
  "enterprise_net", "vendor_net", "ot_ops_net", "field_net",
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
  "1881": "FUXA",
  "8080": "HTTP",
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

// Operational flows that occur entirely inside a zone and never traverse
// the firewall. The lab teaches that the firewall view is intentionally
// incomplete: HMI→RTAC and Historian→RTAC carry critical state but never
// hit the segmentation boundary, so they need host- and network-level
// protections within the zone instead of firewall policy.
//
// Source/target are node IDs as emitted by the workshop graph (e.g.
// `hmi-1`, `rtac-1`). Direction is meaningful - source initiates.
export const INTRA_ZONE_FLOWS: Array<{
  source: string;
  target: string;
  zone: string;
  protocol: string;
  description: string;
}> = [
  {
    source: "hmi-1",
    target: "rtac-1",
    zone: "ot_ops_net",
    protocol: "Modbus 502",
    description: "HMI polls RTAC tag DB ~2s - invisible to firewall",
  },
  {
    source: "historian-1",
    target: "rtac-1",
    zone: "ot_ops_net",
    protocol: "HTTP 8080",
    description: "Historian reads RTAC state ~5s - invisible to firewall",
  },
  {
    source: "openplc-1",
    target: "rtac-1",
    zone: "ot_ops_net",
    protocol: "Modbus 502",
    description: "OpenPLC tags exposed to RTAC - intra-zone",
  },
];

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
  wan: "Enterprise",
  dmz: "Vendor / DMZ",
  lan1: "OT Ops",
  lan2: "Field Devices",
  any: "any zone",
};

export function humanZoneName(z: string): string {
  return ZONE_HUMAN_NAME[z] || z;
}
// Map containd zone names to our network names
export const containdZoneToNetwork: Record<string, string[]> = {
  wan: ["enterprise_net"],
  dmz: ["vendor_net"],
  lan1: ["ot_ops_net"],
  lan2: ["field_net"],
};

// Reverse map: our network names → containd zone name
export const networkToContaindZone: Record<string, string> = {
  enterprise_net: "wan",
  enterprise: "wan",
  vendor_net: "dmz",
  vendor: "dmz",
  ot_ops_net: "lan1",
  ot_ops: "lan1",
  field_net: "lan2",
  field: "lan2",
};

// Check whether a specific host-to-host flow is blocked by the live
// firewall rules. Maps the source/target to containd zone names and
// looks up the matching rule summary. For MIXED zone pairs (like
// lan1→lan2 where RTAC is pinned as the only allowed source), the
// flow is blocked unless the source is the RTAC.
// Returns null if allowed, or a description of the blocking rule if denied.
export function getFlowBlockReason(
  sourceNodeId: string,
  targetNodeId: string,
  ruleSummaries: ZoneRuleSummary[] | undefined,
): string | null {
  if (!ruleSummaries || ruleSummaries.length === 0) return null;

  const srcZone = networkToContaindZone[nodeZone(sourceNodeId)];
  const dstZone = networkToContaindZone[nodeZone(targetNodeId)];
  if (!srcZone || !dstZone) return null;
  if (srcZone === dstZone) return null;

  const rule = ruleSummaries.find(
    (r) => r.source_zone === srcZone && r.dest_zone === dstZone,
  );

  if (!rule) return null;
  if (rule.action === "DENY") {
    const detail = rule.rule_details?.[0] || `${srcZone} → ${dstZone}: DENY`;
    return detail;
  }
  if (rule.action === "MIXED" && sourceNodeId !== "rtac-1") {
    const detail = rule.rule_details?.[0] || `${srcZone} → ${dstZone}: source not authorized`;
    return `${detail} (only RTAC is source-pinned)`;
  }
  return null;
}

// Per-zone interface metadata for the firewall→zone tooltip. Pulled
// from the lab's docker-compose layout: containd binds one interface
// per zone, and each interface is the default gateway for its subnet.
// Surfacing this in the tooltip lets students answer "which side of
// the firewall am I looking at?" without leaving the network map.
export const ZONE_INTERFACE_META: Record<
  string,
  {
    label: string;       // human zone name
    iface: string;       // containd interface name (ethN)
    role: string;        // containd zone label (wan/dmz/lan1/lan2)
    subnet: string;      // CIDR
    fwIp: string;        // firewall's interface IP in this zone
    description: string; // one-line teaching hint
  }
> = {
  enterprise_net: {
    label: "Enterprise",
    iface: "eth0",
    role: "wan",
    subnet: "10.10.10.0/24",
    fwIp: "10.10.10.2",
    description: "Corporate IT zone. Hosts: corp-ws, kali (attacker).",
  },
  vendor_net: {
    label: "Vendor / DMZ",
    iface: "eth1",
    role: "dmz",
    subnet: "10.20.20.0/24",
    fwIp: "10.20.20.2",
    description: "Vendor remote-access DMZ. Hosts: vendor-jump, eng-ws.",
  },
  ot_ops_net: {
    label: "OT Operations",
    iface: "eth2",
    role: "lan1",
    subnet: "10.30.30.0/24",
    fwIp: "10.30.30.2",
    description: "Supervisory control plane. Hosts: rtac, fuxa-hmi, openplc, historian, gps.",
  },
  field_net: {
    label: "Field Devices",
    iface: "eth3",
    role: "lan2",
    subnet: "10.40.40.0/24",
    fwIp: "10.40.40.2",
    description: "Protective devices. Hosts: relay, recloser, regulator, capbank.",
  },
};

// Build policy info for a zone from the live containd rules. The label
// favors protocol names over raw port numbers - students don't need to
// know that 502 is Modbus, they need to know "Modbus is allowed."
export type Permissiveness = "allowed" | "over" | "blocked" | "unknown";

export function getZonePolicyInfo(
  zone: string,
  ruleSummaries?: ZoneRuleSummary[],
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
      action: "ALLOW",
      permissiveness: "unknown",
    };
  }

  // Find rules that apply to this zone (as destination from firewall)
  const containdZones = Object.entries(containdZoneToNetwork)
    .filter(([_, networks]) => networks.includes(zone))
    .map(([cz]) => cz);

  const relevantRules = ruleSummaries.filter(
    (r) => containdZones.includes(r.dest_zone) || r.dest_zone === "any",
  );

  if (relevantRules.length === 0) {
    return {
      label: "No matching rules",
      details: [],
      action: "ALLOW",
      permissiveness: "unknown",
    };
  }

  // Aggregate action
  const hasAllow = relevantRules.some((r) => r.action === "ALLOW");
  const hasDeny = relevantRules.some((r) => r.action === "DENY");
  const action = hasAllow && hasDeny ? "MIXED" : hasDeny ? "DENY" : "ALLOW";

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
    action === "DENY" ? "blocked" : overPermissive ? "over" : "allowed";

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
    label = protos.length > 0 ? `MIXED · ${protos.slice(0, 2).join(" / ")}` : "MIXED";
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
