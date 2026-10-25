import type { CompiledPlan, CurriculumBinding, PlanAction, PlanExpectation } from "./api";

export function actionName(id: string, binding: CurriculumBinding): string {
  return binding.catalog.find((a) => a.id === id)?.title ?? id;
}

export function actionEffectLabel(kind: PlanAction["kind"]): string {
  if (kind === "plan") return "planning only: no firewall change";
  if (kind === "detect") return "detects and logs or inspects; changes no verdict";
  return "L4 enforcement";
}

// Keep package-authored pipes and newlines inside their markdown cells.
function cell(value: string): string {
  return value.replace(/\|/g, "\\|").replace(/\r?\n/g, " ");
}

export function renderRuleTable(plan: CompiledPlan, binding: CurriculumBinding): string {
  return [
    "| # | Rule | Source zones / addresses | Destination zones / addresses | Protocol / port | Verdict | Origin | Log | Inspection |",
    "|---|---|---|---|---|---|---|---|---|",
    ...plan.rules.map(({ rule: r, origin }, i) => [
      i + 1, `${r.id}${r.description ? `: ${r.description}` : ""}`, `${r.sourceZones?.join(", ") || "any"} / ${r.sources?.join(", ") || "any"}`,
      `${r.destZones?.join(", ") || "any"} / ${r.destinations?.join(", ") || "any"}`,
      r.protocols?.map((p) => `${p.name}/${p.port || "any"}`).join(", ") || "any", r.action,
      `${origin.kind}${origin.action ? `: ${actionName(origin.action, binding)}` : ""}${origin.kind === "deferred" ? " — Weak exposure open" : ""}`,
      r.log ? "yes" : "no", r.ics ? JSON.stringify(r.ics) : "none",
    ].map((v) => cell(String(v))).join(" | ")).map((row) => `| ${row} |`),
  ].join("\n");
}

export function tupleLabel(row: PlanExpectation): string {
  return `${row.id}: ${row.from} (${row.fromIp}) → ${row.to} (${row.toIp}) TCP/${row.port}`;
}

export function renderPlanTests(plan: CompiledPlan, binding: CurriculumBinding, role: string, phase: "positive" | "negative"): string {
  const category = phase === "positive" ? "authorized" : "unauthorized";
  const rows = plan.expectations.filter((r) => r.category === category && (!r.labs?.length || r.labs.includes(role)));
  return [
    "| Tuple | Plan verdict | Hardened verdict | Owners | Route | Note |",
    "|---|---|---|---|---|---|",
    ...rows.map((r) => `| ${[tupleLabel(r), r.planVerdict, r.hardenedVerdict, r.owners.map((id) => actionName(id, binding)).join(", ") || "invariant", r.bridged ? "bridged" : "firewall", r.note].map(cell).join(" | ")} |`),
  ].join("\n");
}
