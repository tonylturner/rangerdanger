import type { CompiledPlan, CurriculumBinding, PlanAction, PlanExpectation, PlanICS } from "./api";

export function actionName(id: string, binding: CurriculumBinding): string {
  return binding.catalog.find((a) => a.id === id)?.title ?? id;
}

export function actionEffectLabel(kind: PlanAction["kind"]): string {
  if (kind === "plan") return "planning only: no firewall change";
  if (kind === "detect") return "detects and logs or inspects; changes no verdict";
  return "L4 enforcement";
}

export function planExportFilename(plan: CompiledPlan, kind: "config" | "plan"): string {
  const hash = plan.policyHash.slice(0, 12);
  return `${plan.packageId}-r${plan.revision}-${hash}-${kind}.json`;
}

export function planExportRecord(plan: CompiledPlan): {
  packageId: string;
  revision: number;
  label: CompiledPlan["label"];
  actionIds: string[];
  policyHash: string;
} {
  return {
    packageId: plan.packageId,
    revision: plan.revision,
    label: plan.label,
    actionIds: plan.actions.filter((action) => action.chosen).map((action) => action.id),
    policyHash: plan.policyHash,
  };
}

// Keep package-authored pipes and newlines inside their markdown cells.
function cell(value: string): string {
  return value.replace(/\|/g, "\\|").replace(/\r?\n/g, " ");
}

function inspectionDetails(ics: PlanICS): string {
  const protocol = ics.protocol ? { iec104: "IEC 104", modbus: "Modbus", dnp3: "DNP3" }[ics.protocol] : "any";
  const details: string[] = [`Protocol: ${protocol}`];
  const list = (label: string, values: (string | number)[] | undefined) => {
    if (values?.length) details.push(`${label}: ${values.join(", ")}`);
  };
  const flag = (label: string, value: boolean | undefined) => {
    if (value !== undefined) details.push(`${label}: ${value ? "yes" : "no"}`);
  };
  if (ics.direction) details.push(`Direction: ${ics.direction}`);
  if (ics.mode) details.push(`Mode: ${ics.mode}`);
  flag("Read only", ics.readOnly);
  if (ics.protocol === "iec104") {
    list("Type IDs", ics.typeIds);
    list("Classes", ics.classes);
    list("Common addresses (CA)", ics.ca);
    list("Information object addresses (IOA)", ics.ioa?.map(({ start, end }) => start === end ? String(start) : `${start}–${end}`));
    list("Causes of transmission (COT)", ics.cot);
    list("Originator addresses", ics.originators);
    flag("Negative confirmation", ics.negative);
    flag("Test", ics.test);
    if (ics.selectExecute) details.push(`Select/execute: ${ics.selectExecute}`);
    list("Command states", ics.commandStates);
    list("Command qualifiers", ics.commandQualifiers);
  } else {
    list("Function codes", ics.functionCode);
    flag("Write only", ics.writeOnly);
    if (ics.protocol === "modbus") {
      if (ics.unitId !== undefined) details.push(`Unit ID: ${ics.unitId}`);
      list("Addresses", ics.addresses);
    }
  }
  return details.join("; ");
}

function tableRow(values: string[]): string {
  return `| ${values.map(cell).join(" | ")} |`;
}

export function renderRuleTable(plan: CompiledPlan, binding: CurriculumBinding): string {
  return [
    "| Rule | From | To | Port | Action | Log |",
    "|---|---|---|---|---|---|",
    ...plan.rules.flatMap(({ rule: r, origin }) => {
      const provenance = origin.kind === "observe"
        ? `observation${origin.action ? `: ${actionName(origin.action, binding)} (${origin.action})` : ""} — monitor only, not an L4 rule`
        : `${origin.kind}${origin.action ? `: ${actionName(origin.action, binding)}` : ""}${origin.kind === "deferred" ? " — Weak exposure open" : ""}`;
      const details = `**Rule details:** Origin: ${provenance}${r.ics ? `; ICS selectors: ${inspectionDetails(r.ics)}` : ""}`;
      return [tableRow([
        `${r.id}${r.description ? `: ${r.description}` : ""}`, `${r.sourceZones?.join(", ") || "any"} / ${r.sources?.join(", ") || "any"}`,
        `${r.destZones?.join(", ") || "any"} / ${r.destinations?.join(", ") || "any"}`,
        r.protocols?.map((p) => `${p.name}/${p.port || "any"}`).join(", ") || "any",
        r.ics?.mode === "monitor" ? `would ${r.action === "DENY" ? "deny" : "allow"} (monitor)` : r.action,
        r.log ? "yes" : "no",
      ]), tableRow([details, "", "", "", "", ""])];
    }),
  ].join("\n");
}

export function tupleLabel(row: PlanExpectation): string {
  return `${row.id}: ${row.from} (${row.fromIp}) → ${row.to} (${row.toIp}) TCP/${row.port}`;
}

export function renderPlanTests(plan: CompiledPlan, binding: CurriculumBinding, role: string, phase: "positive" | "negative"): string {
  const category = phase === "positive" ? "authorized" : "unauthorized";
  const rows = plan.expectations.filter((r) => r.category === category && (!r.labs?.length || r.labs.includes(role)));
  return [
    "| Tuple | Expected result | Hardened reference | Owners | Route | Note |",
    "|---|---|---|---|---|---|",
    ...rows.map((r) => `| ${[tupleLabel(r), r.planVerdict, r.hardenedVerdict, r.owners.map((id) => actionName(id, binding)).join(", ") || "invariant", r.bridged ? "bridged" : "firewall", r.note].map(cell).join(" | ")} |`),
  ].join("\n");
}
