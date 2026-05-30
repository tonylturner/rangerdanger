"use client";

import { Shield, ShieldOff, FileCheck2, FileEdit, AlertTriangle, Loader2 } from "lucide-react";
import type { PolicySource } from "../lib/api";

/**
 * Single source of truth for "what firewall policy is the lab actually
 * running?" Used across every step of every exercise, so behavior stays
 * consistent: matched-state banner shows the policy with a color-coded
 * card; mismatched-state shows an amber warning with the action prompt
 * inline.
 *
 * Reads from /api/firewall/active (active_config + policy_source) and
 * the current step's expected_config to decide which variant to render.
 *
 * Three resting states by activeConfig + policy_source:
 *   activeConfig=weak                     -> "Weak baseline" (rose)
 *   activeConfig=improved                 -> "Hardened reference" (emerald)
 *   activeConfig=custom + plan-custom     -> "Your custom policy
 *                                            (Lab 1.4 plan)" (sky)
 *   activeConfig=custom + manual-custom   -> "Your custom policy
 *                                            (your containd commit)" (sky)
 *   activeConfig=custom + ""              -> "Your custom policy" (sky)
 *
 * Mismatched (amber): the step's expected_config differs from active.
 *
 * Buttons (Apply Hardened / Apply Your Plan / Reset to Weak) live in
 * the side panel of scenario-runner.tsx, not here. This component is
 * the announcement; the side panel is where the actions are. The
 * mismatch variant just tells the student which button to click and
 * lets them aim.
 */

type Kind =
  | "weak"
  | "hardened-reference"
  | "plan-custom"
  | "manual-custom"
  | "other-custom"
  | "unknown";

interface Style {
  border: string;
  bg: string;
  dotBg: string;
  labelColor: string;
  subColor: string;
  iconColor: string;
  label: string;
  sub: string;
}

const STATE_STYLES: Record<Kind, Style> = {
  weak: {
    border: "border-rose-800/70",
    bg: "bg-rose-950/40",
    dotBg: "bg-rose-500",
    labelColor: "text-rose-100",
    subColor: "text-rose-300",
    iconColor: "text-rose-300",
    label: "Weak baseline",
    sub: "Cross-zone attacks succeed under this config.",
  },
  "hardened-reference": {
    border: "border-emerald-800/70",
    bg: "bg-emerald-950/40",
    dotBg: "bg-emerald-500",
    labelColor: "text-emerald-100",
    subColor: "text-emerald-300",
    iconColor: "text-emerald-300",
    label: "Hardened reference",
    sub: "Canned reference policy (substation-improved.json).",
  },
  "plan-custom": {
    border: "border-sky-800/70",
    bg: "bg-sky-950/40",
    dotBg: "bg-sky-500",
    labelColor: "text-sky-100",
    subColor: "text-sky-300",
    iconColor: "text-sky-300",
    label: "Your custom policy",
    sub: "Built from your Lab 1.4 plan picks.",
  },
  "manual-custom": {
    border: "border-sky-800/70",
    bg: "bg-sky-950/40",
    dotBg: "bg-sky-500",
    labelColor: "text-sky-100",
    subColor: "text-sky-300",
    iconColor: "text-sky-300",
    label: "Your custom policy",
    sub: "Committed by you in containd's CLI or web UI.",
  },
  "other-custom": {
    border: "border-sky-800/70",
    bg: "bg-sky-950/40",
    dotBg: "bg-sky-500",
    labelColor: "text-sky-100",
    subColor: "text-sky-300",
    iconColor: "text-sky-300",
    label: "Your custom policy",
    sub: "Custom policy active.",
  },
  unknown: {
    border: "border-slate-700/60",
    bg: "bg-slate-900/60",
    dotBg: "bg-slate-500",
    labelColor: "text-slate-300",
    subColor: "text-slate-500",
    iconColor: "text-slate-400",
    label: "Loading…",
    sub: "",
  },
};

function deriveKind(activeConfig: string | null, policySource: PolicySource): Kind {
  if (!activeConfig) return "unknown";
  if (activeConfig === "weak") return "weak";
  if (activeConfig === "improved") return "hardened-reference";
  if (activeConfig === "custom") {
    if (policySource === "plan-custom") return "plan-custom";
    if (policySource === "manual-custom") return "manual-custom";
    return "other-custom";
  }
  return "unknown";
}

function StateIcon({ kind, className }: { kind: Kind; className?: string }) {
  const cls = className ?? "h-4 w-4";
  if (kind === "weak") return <ShieldOff className={cls} />;
  if (kind === "hardened-reference") return <Shield className={cls} />;
  if (kind === "plan-custom") return <FileCheck2 className={cls} />;
  if (kind === "manual-custom" || kind === "other-custom") return <FileEdit className={cls} />;
  return <Loader2 className={cls + " animate-spin"} />;
}

/**
 * matchesExpected — mirror of the alias logic in scenario_execute.go:332
 * and frontend/components/scenario-runner.tsx:1219-1223. The YAML uses
 * "hardened" as the alias for the backend's "improved" or "custom"
 * states, so any of those three count as a match for an expected
 * "hardened" step.
 */
function matchesExpected(actual: string, expected: string | undefined): boolean {
  if (!expected) return true;
  if (expected === "weak") return actual === "weak";
  if (expected === "hardened" || expected === "improved") {
    return actual === "improved" || actual === "custom";
  }
  return true;
}

export function PolicyStatusBanner({
  activeConfig,
  policySource,
  expectedConfig,
}: {
  activeConfig: string | null;
  policySource: PolicySource;
  /** From step.expected_config in the lab YAML. Undefined for steps
      that don't pin a policy (planning labs, intros, etc.) */
  expectedConfig?: string;
}) {
  const kind = deriveKind(activeConfig, policySource);
  const style = STATE_STYLES[kind];

  // If the step doesn't pin a policy, or if it does and the current
  // policy satisfies it, show the informational banner.
  const matched = matchesExpected(activeConfig ?? "", expectedConfig);

  // Mismatch variant — amber warning, action prompt inline.
  if (!matched && expectedConfig && activeConfig) {
    const needLabel =
      expectedConfig === "weak"
        ? "weak baseline"
        : "hardened policy (reference or your custom)";
    const promptedAction =
      expectedConfig === "weak"
        ? "Click Reset to Weak in the side panel to restore the baseline."
        : "Click Apply Hardened (canned reference) or Apply Your Plan (built from your Lab 1.4 picks) in the side panel.";

    return (
      <div className="sticky top-0 z-10 -mx-4 mb-3 border-b border-amber-700/60 bg-amber-950/60 px-4 py-2.5 backdrop-blur-sm">
        <div className="flex items-start gap-2.5">
          <AlertTriangle className="h-4 w-4 shrink-0 text-amber-300 mt-0.5" />
          <div className="min-w-0 flex-1">
            <div className="text-[10px] font-bold uppercase tracking-wider text-amber-300">
              Policy mismatch
            </div>
            <div className="mt-0.5 text-xs text-amber-100">
              This step expects the <span className="font-semibold">{needLabel}</span>,
              but the firewall is currently running{" "}
              <span className="font-semibold">{style.label}</span>.
            </div>
            <div className="mt-1 text-xs text-amber-200/80">{promptedAction}</div>
          </div>
        </div>
      </div>
    );
  }

  // Matched / informational variant.
  return (
    <div
      className={`sticky top-0 z-10 -mx-4 mb-3 border-b ${style.border} ${style.bg} px-4 py-2.5 backdrop-blur-sm`}
    >
      <div className="flex items-center gap-2.5">
        <span className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-md ${style.bg} ${style.iconColor}`}>
          <StateIcon kind={kind} className="h-4 w-4" />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex items-baseline gap-2">
            <span className="text-[10px] font-bold uppercase tracking-wider text-slate-400">
              Currently running
            </span>
            <span className={`text-sm font-semibold ${style.labelColor}`}>
              {style.label}
            </span>
            {(kind === "plan-custom" || kind === "manual-custom") && (
              <span className={`text-[10px] ${style.subColor}`}>
                {kind === "plan-custom" ? "(Lab 1.4 plan)" : "(your containd commit)"}
              </span>
            )}
          </div>
          {style.sub && (
            <div className={`mt-0.5 text-[11px] ${style.subColor}`}>{style.sub}</div>
          )}
        </div>
      </div>
    </div>
  );
}
