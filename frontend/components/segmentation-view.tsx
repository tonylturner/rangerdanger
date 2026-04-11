"use client";

// Self-contained "containd Segmentation" panel. Originally lived inside
// the Feeder HMI substation tab; lifted into its own file so the
// Network Map can render the exact same panel as a collapsible overlay
// without duplicating the apply/test logic.
//
// The component manages its own state (active config, comparison, test
// result) and only depends on React Query for cross-component cache
// invalidation when the user applies a new config — that lets the
// network map's edge labels and the header policy badge update
// immediately when the firewall flips between weak and improved.

import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  getActiveFirewallConfig,
  getFirewallComparison,
  applyFirewallConfig,
  executeScenarioStep,
  type PolicyComparison,
} from "../lib/api";

export function SegmentationView({ compact = false }: { compact?: boolean }) {
  const queryClient = useQueryClient();
  const [activeConfig, setActiveConfig] = useState<string | null>(null);
  const [comparison, setComparison] = useState<PolicyComparison | null>(null);
  const [applying, setApplying] = useState(false);
  const [lastApply, setLastApply] = useState(0);
  const [testResult, setTestResult] = useState<{ blocked: boolean; detail: string } | null>(null);
  const [testing, setTesting] = useState(false);

  useEffect(() => {
    getActiveFirewallConfig().then((r) => setActiveConfig(r.active_config)).catch(() => {});
    getFirewallComparison().then(setComparison).catch(() => {});
  }, [lastApply]);

  const handleApply = async (config: "weak" | "improved") => {
    setApplying(true);
    try {
      const res = await applyFirewallConfig(config);
      setActiveConfig(res.active_config);
      setLastApply((c) => c + 1);
      setTestResult(null);
      // Network map edges + header policy badge subscribe to these
      // queries; invalidating triggers an immediate refetch so the
      // canvas reflects the new config without a manual reload.
      queryClient.invalidateQueries({ queryKey: ["firewall-rules"] });
      queryClient.invalidateQueries({ queryKey: ["workshop", "status"] });
    } catch {
      // swallow — UI state stays as-is
    } finally {
      setApplying(false);
    }
  };

  const handleTestConfig = async () => {
    setTesting(true);
    setTestResult(null);
    try {
      const res = await executeScenarioStep("enterprise-to-breaker", 5);
      const blocked = !res.success;
      setTestResult({
        blocked,
        detail:
          res.results?.[0]?.detail ||
          (blocked ? "Command blocked by containd" : "Command reached field device"),
      });
      // If the trip command actually succeeded (weak baseline), restore
      // the breaker so the lab is left in a known state.
      if (!blocked) {
        await executeScenarioStep("enterprise-to-breaker", 3);
      }
    } catch (e) {
      setTestResult({ blocked: false, detail: `Test error: ${e}` });
    } finally {
      setTesting(false);
    }
  };

  if (!comparison) {
    return (
      <div className="py-6 text-center text-sm text-slate-500">
        Loading policy comparison…
      </div>
    );
  }

  return (
    <div className="space-y-3">
      {/* containd active policy banner. In compact mode the buttons
          stack under the title so the text never gets squeezed into a
          vertical column when the panel is narrow. */}
      <div
        className={`rounded-lg border p-2.5 ${
          activeConfig === "improved"
            ? "border-green-700/60 bg-green-950/15"
            : "border-red-700/60 bg-red-950/15"
        }`}
      >
        <div className="text-[9px] font-bold uppercase tracking-wider text-slate-500">
          containd NGFW · Active Policy
        </div>
        <div
          className={`mt-0.5 text-sm font-bold ${
            activeConfig === "improved" ? "text-green-400" : "text-red-400"
          }`}
        >
          {activeConfig === "improved"
            ? "Hardened Segmentation"
            : "Weak Baseline (vulnerable)"}
        </div>
        {!compact && (
          <div className="mt-1 text-[11px] leading-snug text-slate-500">
            {activeConfig === "improved"
              ? "Only the RTAC can reach field devices. Enterprise and vendor zones are blocked."
              : "All zones can reach field devices directly. Attackers have clear paths to breakers and regulators."}
          </div>
        )}
        <div className="mt-2 flex flex-wrap gap-1.5">
          <button
            onClick={() => handleApply("weak")}
            disabled={applying || activeConfig === "weak"}
            className={`rounded border px-2 py-1 text-[10px] font-medium transition-colors disabled:opacity-40 ${
              activeConfig === "weak"
                ? "border-red-700/60 bg-red-950/30 text-red-400"
                : "border-slate-600 text-slate-400 hover:border-red-700 hover:text-red-400"
            }`}
          >
            {activeConfig === "weak" ? "Weak (active)" : "Reset to Weak"}
          </button>
          <button
            onClick={() => handleApply("improved")}
            disabled={applying || activeConfig === "improved"}
            className={`rounded border px-2 py-1 text-[10px] font-medium transition-colors disabled:opacity-40 ${
              activeConfig === "improved"
                ? "border-green-700/60 bg-green-950/30 text-green-400"
                : "border-slate-600 text-slate-400 hover:border-green-700 hover:text-green-400"
            }`}
          >
            {activeConfig === "improved" ? "Hardened (active)" : "Apply Hardened"}
          </button>
          <button
            onClick={handleTestConfig}
            disabled={testing}
            className="rounded border border-sky-700 bg-sky-950/30 px-2 py-1 text-[10px] font-medium text-sky-400 transition-colors hover:bg-sky-900/30 disabled:opacity-40"
          >
            {testing ? "Testing…" : "Test"}
          </button>
        </div>
      </div>

      {/* Test result */}
      {testResult && (
        <div
          className={`rounded border p-2.5 text-[11px] ${
            testResult.blocked
              ? "border-green-800/60 bg-green-950/20 text-green-400"
              : "border-red-800/60 bg-red-950/20 text-red-400"
          }`}
        >
          <span className="font-bold">{testResult.blocked ? "BLOCKED" : "ALLOWED"}</span>
          <span className="ml-2 text-slate-400">{testResult.detail}</span>
          {!testResult.blocked && activeConfig === "improved" && (
            <div className="mt-1 text-yellow-400">
              Warning: command should be blocked with hardened policy
            </div>
          )}
          {testResult.blocked && activeConfig === "improved" && (
            <div className="mt-1">Enterprise→field traffic correctly blocked by containd NGFW</div>
          )}
        </div>
      )}

      {/* Zone-pair comparison — hidden in compact mode to keep the box small */}
      {!compact && (
        <div className="space-y-1.5">
          {comparison.diffs.map((d, i) => {
            const tightened = d.change === "tightened" || d.change === "added";
            return (
              <div
                key={i}
                className={`rounded border p-2.5 text-[11px] ${
                  tightened
                    ? "border-green-900/40 bg-green-950/10"
                    : "border-slate-800/60 bg-slate-900/30"
                }`}
              >
                <div className="flex items-center justify-between">
                  <span className="font-medium text-slate-300">{d.zone_pair}</span>
                  <div className="flex items-center gap-2">
                    <ActionChip action={d.weak_action} label="Weak" />
                    {d.improved_action !== d.weak_action && (
                      <>
                        <span className="text-slate-600">→</span>
                        <ActionChip action={d.improved_action} label="Hardened" />
                      </>
                    )}
                    {tightened && (
                      <span className="text-[9px] font-bold uppercase text-green-400">
                        tightened
                      </span>
                    )}
                  </div>
                </div>
                {tightened && d.improved_rule && (
                  <div className="mt-1.5 text-[10px] text-slate-500">{d.improved_rule}</div>
                )}
              </div>
            );
          })}
        </div>
      )}

      {!compact && (
        <div className="border-t border-slate-800/40 pt-2 text-[10px] text-slate-600">
          The hardened policy ensures only the RTAC (10.40.40.10) can send control
          commands to field devices. Enterprise and vendor zones are blocked from
          direct field device access.
        </div>
      )}
    </div>
  );
}

function ActionChip({ action }: { action: string; label?: string }) {
  if (!action) return null;
  const cls =
    action === "ALLOW"
      ? "text-green-400 border-green-800/40"
      : action === "DENY"
      ? "text-red-400 border-red-800/40"
      : "text-yellow-400 border-yellow-800/40";
  return (
    <span className={`rounded border px-1.5 py-0.5 text-[9px] font-bold ${cls}`}>
      {action}
    </span>
  );
}
