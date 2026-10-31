"use client";

import { Shield, ShieldOff, FileCheck2, FileEdit, AlertTriangle, Loader2 } from "lucide-react";
import type { PolicySource, PlanProvenance } from "../lib/api";
import {
  CUSTOM_POLICY,
  planLabel,
  HARDENED_ALIAS,
  HARDENED_POLICY,
  isHardened,
  matchesExpected,
  policyWording,
  type PackagePolicies,
  type PolicyWording,
} from "../lib/policy-wording";

/**
 * Single source of truth for "what firewall policy is the lab actually
 * running?" Used across every step of every exercise, so behavior stays
 * consistent: matched-state banner shows the policy with a color-coded
 * card; mismatched-state shows an amber warning with the action prompt
 * inline.
 *
 * Reads from /api/firewall/active (active_config + policy_source), the
 * active package's policies and the current step's expected_config to
 * decide which variant to render.
 *
 * Resting states by activeConfig + policy_source:
 *   activeConfig=the hardened policy      -> "Hardened reference" (emerald)
 *   activeConfig=another package policy   -> "Weak baseline" (rose)
 *   activeConfig=custom + plan            -> "Your plan — Weak/Partial/Hardened" (sky)
 *   activeConfig=custom + manual-custom   -> "Your custom policy
 *                                            (your containd commit)" (sky)
 *   activeConfig=custom + ""              -> "Your custom policy" (sky)
 * The quoted names are the US package's; another package's policies
 * show by their declared names, without a description.
 *
 * Mismatched (amber): the step's expected_config differs from active.
 *
 * Buttons (Apply Hardened / Apply Your Plan / Reset to Weak) live in
 * the policy-actions panel below the step in scenario-runner.tsx, not
 * here. This component is the announcement; that panel is where the
 * actions are. The
 * mismatch variant just tells the student which button to click and
 * lets them aim. Other packages have no such panel: their prompt
 * points at the Network Console policy menu.
 */

// "weak" is any package policy but the hardened one.
type Kind =
  | "weak"
  | "hardened-reference"
  | "plan"
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
    label: "",
    sub: "",
  },
  "hardened-reference": {
    border: "border-emerald-800/70",
    bg: "bg-emerald-950/40",
    dotBg: "bg-emerald-500",
    labelColor: "text-emerald-100",
    subColor: "text-emerald-300",
    iconColor: "text-emerald-300",
    label: "",
    sub: "",
  },
  "plan": {
    border: "border-sky-800/70",
    bg: "bg-sky-950/40",
    dotBg: "bg-sky-500",
    labelColor: "text-sky-100",
    subColor: "text-sky-300",
    iconColor: "text-sky-300",
    label: "Your custom policy",
    sub: "Compiled by the backend from your plan picks.",
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

// The label and sub of a package policy ("weak" and
// "hardened-reference" styles) in the US package's words.
const US_BANNER_WORDING: Record<string, PolicyWording> = {
  weak: { label: "Weak baseline", description: "Cross-zone attacks succeed under this config." },
  improved: { label: "Hardened reference", description: "Canned reference policy (substation-improved.json)." },
};

function deriveKind(activeConfig: string | null, policySource: PolicySource, policies: PackagePolicies | undefined): Kind {
  if (!activeConfig || !policies) return "unknown";
  if (activeConfig === CUSTOM_POLICY) {
    if (policySource === "plan") return "plan";
    if (policySource === "manual-custom") return "manual-custom";
    return "other-custom";
  }
  if (!policies.names.includes(activeConfig)) return "unknown";
  return isHardened(activeConfig) ? "hardened-reference" : "weak";
}

// The kind's style with a package policy's own label and sub.
function bannerStyle(kind: Kind, activeConfig: string | null, policies: PackagePolicies | undefined): Style {
  const style = STATE_STYLES[kind];
  if ((kind !== "weak" && kind !== "hardened-reference") || !activeConfig || !policies) return style;
  const wording = policyWording(activeConfig, policies, US_BANNER_WORDING);
  return { ...style, label: wording.label, sub: wording.description };
}

// What the mismatch variant asks for, and how to get there.
function mismatchText(expectedConfig: string, usWording: boolean): { need: string; action: string } {
  if (usWording) {
    return expectedConfig === "weak"
      ? {
          need: "weak baseline",
          action: "Click Reset to Weak in the policy-actions panel below to restore the baseline.",
        }
      : {
          need: "hardened policy (reference or your custom)",
          action:
            "Click Apply Hardened (canned reference) or Apply Your Plan (built from your Lab 1.4 picks) in the policy-actions panel below.",
        };
  }
  const need =
    expectedConfig === HARDENED_ALIAS || expectedConfig === HARDENED_POLICY
      ? `${HARDENED_POLICY} policy (or a custom policy)`
      : `${expectedConfig} policy`;
  return { need, action: "Apply it from the Firewall policy menu in the Network Console." };
}

function StateIcon({ kind, className }: { kind: Kind; className?: string }) {
  const cls = className ?? "h-4 w-4";
  if (kind === "weak") return <ShieldOff className={cls} />;
  if (kind === "hardened-reference") return <Shield className={cls} />;
  if (kind === "plan") return <FileCheck2 className={cls} />;
  if (kind === "manual-custom" || kind === "other-custom") return <FileEdit className={cls} />;
  return <Loader2 className={cls + " animate-spin"} />;
}

export function PolicyStatusBanner({
  activeConfig,
  policySource,
  expectedConfig,
  policies,
  plan,
}: {
  activeConfig: string | null;
  policySource: PolicySource;
  plan?: PlanProvenance;
  /** From step.expected_config in the lab YAML. Undefined for steps
      that don't pin a policy (planning labs, intros, etc.) */
  expectedConfig?: string;
  /** The active package's policies; undefined while it loads. */
  policies: PackagePolicies | undefined;
}) {
  const kind = deriveKind(activeConfig, policySource, policies);
  const base = bannerStyle(kind, activeConfig, policies);
  const style = kind === "plan" && plan ? { ...base, label: `Your plan — ${planLabel(plan.label)}` } : base;

  // If the step doesn't pin a policy, or if it does and the current
  // policy satisfies it, show the informational banner.
  const matched = matchesExpected(activeConfig ?? "", expectedConfig);

  // Mismatch variant - amber warning, action prompt inline.
  if (!matched && expectedConfig && activeConfig && policies) {
    const { need: needLabel, action: promptedAction } = mismatchText(expectedConfig, policies.usWording);

    return (
      <div className="sticky top-0 z-10 -mx-4 mb-3 border-b border-amber-700/60 bg-amber-950/60 px-4 py-2.5 backdrop-blur-xs">
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
      className={`sticky top-0 z-10 -mx-4 mb-3 border-b ${style.border} ${style.bg} px-4 py-2.5 backdrop-blur-xs`}
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
            {(kind === "plan" || kind === "manual-custom") && (
              <span className={`text-[10px] ${style.subColor}`}>
                {kind === "plan" ? "(Lab 1.4 plan)" : "(your containd commit)"}
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
