// Maps exercise scenario IDs to the container node IDs involved.
// Used as fallback when YAML `nodes` field isn't set on the scenario.

export type ExerciseNodeInfo = {
  primary: string;
  others: string[];
};

export const EXERCISE_NODE_MAP: Record<string, ExerciseNodeInfo> = {
  "baseline-assessment":       { primary: "fw-1",         others: ["eng_workstation", "kali"] },
  "segmentation-requirements": { primary: "",             others: [] },
  "vendor-rdp-compromise":     { primary: "vendor_jump",  others: [] },
  "modbus-override":           { primary: "kali",         others: [] },
  "dnp3-command-injection":    { primary: "kali",         others: [] },
  "validation-evidence":       { primary: "kali",         others: ["vendor_jump", "eng_workstation"] },
};

export const NODE_LABELS: Record<string, string> = {
  "fw-1":              "Firewall (containd)",
  "kali":              "Kali (10.10.10.50)",
  "vendor_jump":       "Vendor Jump (10.20.20.10)",
  "eng_workstation":   "Eng WS (10.20.20.20)",
  "rtac_sim":          "RTAC (10.30.30.20)",
  "openplc":           "OpenPLC (10.30.30.30)",
  "fuxa_hmi":          "FUXA HMI (10.30.30.10)",
  "corp_ws":           "Corp WS (10.10.10.10)",
  "relay_sim":         "Relay (10.40.40.20)",
  "recloser_sim":      "Recloser (10.40.40.21)",
  "regulator_sim":     "Regulator (10.40.40.22)",
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
  if (desc.includes("10.10.10.50") || /\bkali\b/i.test(desc)) return "kali";
  if (desc.includes("10.20.20.10") || /\bvendor.?jump\b/i.test(desc)) return "vendor_jump";
  if (desc.includes("10.20.20.20") || /\bengineering.?workstation\b/i.test(desc)) return "eng_workstation";
  if (desc.includes("10.30.30.30") || /\bopenplc\b/i.test(desc)) return "openplc";
  if (desc.includes("10.30.30.20") || /\brtac\b/i.test(desc)) return "rtac_sim";
  return null;
}
