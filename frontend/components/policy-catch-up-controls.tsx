"use client";

import { useState } from "react";
import { applyFirewallConfig, type CompiledPlan, type PolicySource } from "../lib/api";
import { CatchUp } from "./catch-up";
import { ApplyPlanButton } from "./apply-plan-button";

export function PolicyCatchUpControls({ activeConfig, policySource, plan, error, onApplied }: {
  activeConfig: string | null;
  policySource: PolicySource;
  plan: CompiledPlan | null;
  error?: string;
  onApplied: () => void;
}) {
  const [pending, setPending] = useState<string | null>(null);
  const [message, setMessage] = useState("");
  const [failed, setFailed] = useState(false);
  const confirmReplacement = (label: string) => policySource !== "manual-custom"
    || window.confirm(`${label} replaces your hand-written firewall policy. Export it from containd before continuing. Replace it now?`);
  const apply = async (config: string, label: string) => {
    if (!activeConfig || pending || !confirmReplacement(label)) return;
    setPending(config);
    setMessage("");
    setFailed(false);
    try {
      await applyFirewallConfig(config);
      setMessage(`${label}: applied. Check the running policy above.`);
      onApplied();
    } catch (e) {
      setFailed(true);
      setMessage(e instanceof Error ? e.message : "Could not apply policy");
    } finally {
      setPending(null);
    }
  };
  return <CatchUp id="catch-up-controls">
    <div className="flex flex-wrap gap-2">
      {activeConfig !== "improved" && <button disabled={!activeConfig || pending !== null} aria-busy={pending === "improved"}
        onClick={() => apply("improved", "Apply Hardened")}
        className="rounded-sm border border-emerald-800 bg-emerald-950/40 px-2 py-1 text-[10px] font-medium text-emerald-300 hover:bg-emerald-900/50 disabled:opacity-50">
        {pending === "improved" ? "Applying Hardened…" : "Apply Hardened"}
      </button>}
      <ApplyPlanButton plan={plan} onApplied={onApplied} disabled={!activeConfig || pending !== null}
        beforeApply={() => confirmReplacement("Apply Your Plan")}
        onBusyChange={(busy) => setPending(busy ? "plan" : null)} />
      {activeConfig && activeConfig !== "weak" && <button disabled={!activeConfig || pending !== null} aria-busy={pending === "weak"}
        onClick={() => apply("weak", "Reset to Weak")}
        className="rounded-sm border border-rose-800 bg-rose-950/40 px-2 py-1 text-[10px] font-medium text-rose-300 hover:bg-rose-900/50 disabled:opacity-50">
        {pending === "weak" ? "Resetting to Weak…" : "Reset to Weak"}
      </button>}
    </div>
    {message && <p role={failed ? "alert" : "status"} className={failed ? "mt-2 text-red-300" : "mt-2 text-slate-300"}>{message}</p>}
    {error && <p role="alert" className="text-xs text-red-400">{error}</p>}
  </CatchUp>;
}
