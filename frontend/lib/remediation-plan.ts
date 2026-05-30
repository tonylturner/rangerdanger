// Shared storage for the remediation-planning exercise decisions.
// Writes from the DecisionPanel component; reads from any later exercise
// that wants to display or branch on the student's plan.

const STORAGE_KEY = "rd-remediation-plan";

export type RemediationPlan = {
  exerciseId: string;
  selectedActionIds: string[];
  savedAt: string; // ISO timestamp
};

export function saveRemediationPlan(plan: RemediationPlan): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(plan));
  } catch {
    // ignore
  }
}

export function loadRemediationPlan(): RemediationPlan | null {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return null;
    return JSON.parse(raw) as RemediationPlan;
  } catch {
    return null;
  }
}

export function clearRemediationPlan(): void {
  try {
    localStorage.removeItem(STORAGE_KEY);
  } catch {
    // ignore
  }
}

// hasSelected checks whether a specific remediation action was selected.
// Used by later exercises to branch on the student's decisions.
export function hasSelected(plan: RemediationPlan | null, actionId: string): boolean {
  return !!plan && plan.selectedActionIds.includes(actionId);
}

// Mapping of remediation action ids to the later attack exercises they
// defend against. Consumed by the "Your Plan" banner in later exercises
// to call out which prior choices make the current exercise easier or
// harder.
export const ACTION_IMPACT: Record<string, { defends: string[]; note: string }> = {
  "block-enterprise-to-field": {
    defends: ["hardening-configurations"],
    note: "Blocks enterprise → field — closes the DNP3 command-injection path demonstrated in Lab 2.3",
  },
  "block-enterprise-to-ot": {
    defends: ["hardening-configurations"],
    note: "Blocks enterprise → OT Ops — removes the staging foothold for the Modbus override demonstrated in Lab 2.3",
  },
  "restrict-vendor-to-ot": {
    defends: ["vendor-rdp-compromise"],
    note: "Restricts vendor → OT to encrypted management protocols — narrows the vendor pivot lane in Lab 2.3-bonus",
  },
  "block-vendor-to-field": {
    defends: ["vendor-rdp-compromise"],
    note: "Blocks vendor → field entirely — closes the second hop of the vendor RDP/VNC kill chain in Lab 2.3-bonus",
  },
  "pin-rtac-to-field": {
    defends: ["hardening-configurations"],
    note: "Pins field access to the RTAC source IP — prevents the lateral Modbus override demonstrated in Lab 2.3",
  },
  "modbus-dpi": {
    defends: ["hardening-configurations"],
    note: "Protocol-aware Modbus filter — blocks unauthorized writes at the function-code level (Lab 2.3 DPI lesson)",
  },
  "dnp3-dpi": {
    defends: ["hardening-configurations"],
    note: "Protocol-aware DNP3 filter — blocks Direct Operate from non-RTAC sources (Lab 2.3 DPI lesson)",
  },
  "positive-validation": {
    defends: ["validation-evidence"],
    note: "Pre-planned validation testing - produces the evidence package for Lab 2.4",
  },
  "ids-deployment": {
    defends: ["validation-evidence"],
    note: "IDS sensors with ICS rule packs - adds detection alongside the firewall's prevent layer; the alert chain becomes part of the Lab 2.4 evidence package",
  },
  "ot-asset-inventory": {
    defends: ["validation-evidence"],
    note: "OT asset inventory + baseline - anchors the Lab 2.4 change-board package on a known-good device list; without it, post-change drift goes undetected",
  },
  "vendor-zta-broker": {
    defends: ["vendor-rdp-compromise"],
    note: "ZTNA/PAM broker for vendor access - closes more of the Lab 2.3-bonus kill chain than restrict-vendor-to-ot alone (no always-on path, identity-bound sessions, full audit)",
  },
};
