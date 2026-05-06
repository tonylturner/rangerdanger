// Maps exercise scenario IDs to the container node IDs involved.
// Node IDs must match the topology in substation-segmentation.yml.

export type ExerciseNodeInfo = {
  primary: string;
  others: string[];
};

export const EXERCISE_NODE_MAP: Record<string, ExerciseNodeInfo> = {
  "baseline-assessment":       { primary: "fw-1",          others: ["eng-ws-1", "kali-1"] },
  "segmentation-requirements": { primary: "",              others: [] },
  "remediation-planning":      { primary: "",              others: [] },
  "firewall-implementation":   { primary: "fw-1",          others: ["kali-1", "eng-ws-1"] },
  "vendor-rdp-compromise":     { primary: "vendor-jump-1", others: [] },
  "modbus-override":           { primary: "kali-1",        others: [] },
  "dnp3-command-injection":    { primary: "kali-1",        others: [] },
  "validation-evidence":       { primary: "kali-1",        others: ["vendor-jump-1", "eng-ws-1"] },
  "capbank-switching-attack":  { primary: "kali-1",        others: [] },
};

export const NODE_LABELS: Record<string, string> = {
  "fw-1":              "Firewall",
  "kali-1":            "Kali",
  "vendor-jump-1":     "Vendor Jump",
  "eng-ws-1":          "Eng WS",
  "rtac-1":            "RTAC",
  "openplc-1":         "OpenPLC",
  "hmi-1":             "FUXA HMI",
  "corp-ws-1":         "Corp WS",
  "relay-1":           "Relay",
  "recloser-1":        "Recloser",
  "regulator-1":       "Regulator",
  "capbank-1":         "Capbank",
  "historian-1":       "Historian",
  "gps-1":             "GPS",
};

// Nodes that have a web UI accessible via iframe
export const NODE_UI_URLS: Record<string, string> = {
  "fw-1":              "/containd/",
  "eng-ws-1":          "/apps/eng-ws/",
  "hmi-1":             "/apps/fuxa-hmi/",
  "openplc-1":         "/apps/openplc/",
};

// Get all node IDs for an exercise, using scenario metadata or fallback map.
export function getExerciseNodes(scenarioId: string, scenarioNodes?: string[]): string[] {
  if (scenarioNodes && scenarioNodes.length > 0) {
    return scenarioNodes;
  }
  const info = EXERCISE_NODE_MAP[scenarioId];
  if (!info || !info.primary) return [];
  return [info.primary, ...info.others];
}

// Infer which node a step's CLI commands should run on from description text.
export function inferNodeFromDescription(desc: string): string | null {
  if (desc.includes("10.10.10.50") || /\bkali\b/i.test(desc)) return "kali-1";
  if (desc.includes("10.20.20.10") || /\bvendor.?jump\b/i.test(desc)) return "vendor-jump-1";
  if (desc.includes("10.20.20.20") || /\bengineering.?workstation\b/i.test(desc)) return "eng-ws-1";
  if (desc.includes("10.30.30.30") || /\bopenplc\b/i.test(desc)) return "openplc-1";
  if (desc.includes("10.30.30.20") || /\brtac\b/i.test(desc)) return "rtac-1";
  return null;
}
