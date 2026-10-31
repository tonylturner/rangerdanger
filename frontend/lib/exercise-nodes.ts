// Resolve exercise nodes from scenario metadata or the active package binding.

import type { CurriculumBinding } from "./api";

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
  "cc-1":              "Control Centre",
  "rtu-1":             "IEC 104 RTU",
  "breaker-1":         "Feeder Breaker",
  "f2-breaker":        "F2 Feeder Breaker",
  "rmu-1":             "RMU1 Sectionaliser",
  "rmu-2":             "RMU2 Tie",
  "oltc-1":            "T1 OLTC",
  "plc-1":             "OpenPLC",
};

// Nodes that have a web UI accessible via iframe
export const NODE_UI_URLS: Record<string, string> = {
  "fw-1":              "/containd/",
  "eng-ws-1":          "/apps/eng-ws/",
  "hmi-1":             "/apps/fuxa-hmi/",
  "openplc-1":         "/apps/openplc/",
};

// Get all node IDs for an exercise, using scenario metadata or fallback map.
export function getExerciseNodes(scenarioId: string, scenarioNodes?: string[], curriculum?: CurriculumBinding): string[] {
  if (scenarioNodes && scenarioNodes.length > 0) {
    return scenarioNodes;
  }
  const info = curriculum?.nodeMap[scenarioId];
  if (!info || !info.primary) return [];
  return [info.primary, ...info.others];
}

// Infer which node a step's CLI commands should run on from description text.
export function inferNodeFromDescription(desc: string, curriculum?: CurriculumBinding): string | null {
  return curriculum?.nodeInference.find((n) => desc.includes(n.ip) || new RegExp(n.pattern, "i").test(desc))?.node ?? null;
}
