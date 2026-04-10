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
    defends: ["dnp3-command-injection"],
    note: "Blocks enterprise → field DNP3 path — closes the DNP3 command injection attack vector",
  },
  "block-enterprise-to-ot": {
    defends: ["modbus-override"],
    note: "Blocks enterprise → OT Ops — removes the staging foothold for Modbus override",
  },
  "restrict-vendor-to-ot": {
    defends: ["vendor-rdp-compromise"],
    note: "Restricts vendor → OT to encrypted management protocols — closes vendor pivot path",
  },
  "block-vendor-to-field": {
    defends: ["vendor-rdp-compromise"],
    note: "Blocks vendor → field entirely — kills the direct Modbus path from the vendor DMZ",
  },
  "pin-rtac-to-field": {
    defends: ["modbus-override"],
    note: "Pins field access to RTAC source IP — prevents lateral compromised-OT-host attacks",
  },
  "modbus-dpi": {
    defends: ["modbus-override"],
    note: "Protocol-aware Modbus filter — blocks unauthorized writes at the function-code level",
  },
  "dnp3-dpi": {
    defends: ["dnp3-command-injection"],
    note: "Protocol-aware DNP3 filter — blocks Direct Operate from non-RTAC sources",
  },
  "positive-validation": {
    defends: ["validation-evidence"],
    note: "Pre-planned validation testing — produces the evidence package for Exercise 6",
  },
};
