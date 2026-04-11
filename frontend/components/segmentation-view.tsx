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

import { useEffect, useMemo, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  getActiveFirewallConfig,
  getFirewallComparison,
  applyFirewallConfig,
  executeScenarioStep,
  type PolicyComparison,
} from "../lib/api";

// Catalogue of policies the drawer knows about. The backend currently
// only validates `weak` and `improved`, but the frontend treats this
// list as the source of truth for the dropdown so adding a new
// policy is one entry here plus a matching JSON in the lab-definitions
// folder. `actionField` tells the evaluation renderer which column of
// PolicyComparison.diffs to read from for this policy.
type PolicyId = "weak" | "improved";
type PolicyMeta = {
  id: PolicyId;
  label: string;
  description: string;
  actionField: "weak_action" | "improved_action";
  ruleField: "weak_rule" | "improved_rule";
  toneActive: string;
  toneInactive: string;
};
const POLICIES: PolicyMeta[] = [
  {
    id: "weak",
    label: "Weak Baseline",
    description:
      "All zones can reach field devices directly. Attackers have clear paths to breakers and regulators.",
    actionField: "weak_action",
    ruleField: "weak_rule",
    toneActive: "border-red-700/60 bg-red-950/15 text-red-400",
    toneInactive: "border-slate-700 text-slate-400 hover:border-red-700 hover:text-red-400",
  },
  {
    id: "improved",
    label: "Hardened Segmentation",
    description:
      "Only the RTAC can reach field devices. Enterprise and vendor zones are blocked.",
    actionField: "improved_action",
    ruleField: "improved_rule",
    toneActive: "border-green-700/60 bg-green-950/15 text-green-400",
    toneInactive: "border-slate-700 text-slate-400 hover:border-green-700 hover:text-green-400",
  },
];
function policyMeta(id: string | null | undefined): PolicyMeta {
  return POLICIES.find((p) => p.id === id) || POLICIES[0];
}

export function SegmentationView({ compact = false }: { compact?: boolean }) {
  const queryClient = useQueryClient();
  const [activeConfig, setActiveConfig] = useState<string | null>(null);
  const [comparison, setComparison] = useState<PolicyComparison | null>(null);
  const [applying, setApplying] = useState(false);
  const [lastApply, setLastApply] = useState(0);
  const [testResult, setTestResult] = useState<{ blocked: boolean; detail: string } | null>(null);
  const [testing, setTesting] = useState(false);
  // Which policy the evaluation panel is previewing. Defaults to
  // whatever is currently applied so the panel opens "self-consistent".
  // Track whether the user has manually changed the selection so we
  // don't keep snapping back to the active config when they're trying
  // to inspect a different one.
  const [selectedPolicyId, setSelectedPolicyId] = useState<PolicyId>("improved");
  const [userPickedPolicy, setUserPickedPolicy] = useState(false);

  useEffect(() => {
    getActiveFirewallConfig()
      .then((r) => {
        setActiveConfig(r.active_config);
        if (!userPickedPolicy && (r.active_config === "weak" || r.active_config === "improved")) {
          setSelectedPolicyId(r.active_config);
        }
      })
      .catch(() => {});
    getFirewallComparison().then(setComparison).catch(() => {});
    // userPickedPolicy intentionally excluded so the initial load
    // sets it but a manual pick is sticky.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lastApply]);

  const handleApply = async (config: "weak" | "improved") => {
    setApplying(true);
    try {
      const res = await applyFirewallConfig(config);
      setActiveConfig(res.active_config);
      setSelectedPolicyId(config);
      setUserPickedPolicy(false); // re-sync with active after apply
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

  // Scenario step references for the segmentation Test button.
  // `dnp3-command-injection` is the workshop scenario that ships a
  // ready-made enterprise→field probe. The test step fires a DNP3
  // disable_reclose from kali (10.10.10.50); the restore step runs
  // the full sequence (clear_fault → enable_reclose → reset_lockout
  // → close) so we leave the lab in a known-good state if the test
  // actually went through (weak baseline).
  //
  // The backend `executeCommand` checks the source string against
  // its allowlist when activeConfig === "improved": only RTAC and
  // "operator" pass, everything else returns Success=false with a
  // BLOCKED detail. That's how this test reports allow vs deny.
  const TEST_SCENARIO = "dnp3-command-injection";
  const TEST_STEP_INDEX = 7;     // "Re-test DNP3 attack"
  const RESTORE_STEP_INDEX = 5;  // "Restore operations"

  const handleTestConfig = async () => {
    setTesting(true);
    setTestResult(null);
    try {
      const res = await executeScenarioStep(TEST_SCENARIO, TEST_STEP_INDEX);
      const blocked = !res.success;
      setTestResult({
        blocked,
        detail:
          res.results?.[0]?.detail ||
          (blocked ? "Command blocked by containd" : "Command reached field device"),
      });
      // If the attack succeeded (weak baseline), restore the recloser
      // so the lab is left in a known state.
      if (!blocked) {
        await executeScenarioStep(TEST_SCENARIO, RESTORE_STEP_INDEX);
      }
    } catch (e) {
      setTestResult({ blocked: false, detail: `Test error: ${e}` });
    } finally {
      setTesting(false);
    }
  };

  // Build the evaluation rows for the currently-selected policy. Each
  // row reads the chosen policy's action/rule columns from the diff
  // and tags whether it differs from what's currently active — that
  // tag drives the "would change if applied" highlight.
  const selectedMeta = policyMeta(selectedPolicyId);
  const activeMeta = policyMeta(activeConfig);
  const evaluation = useMemo(() => {
    if (!comparison) return [];
    return comparison.diffs.map((d) => {
      const action = d[selectedMeta.actionField];
      const rule = d[selectedMeta.ruleField];
      const activeAction = d[activeMeta.actionField];
      const differsFromActive = action !== activeAction;
      return { zonePair: d.zone_pair, action, rule, activeAction, differsFromActive };
    });
  }, [comparison, selectedMeta, activeMeta]);
  const changeCount = evaluation.filter((r) => r.differsFromActive).length;

  if (!comparison) {
    return (
      <div className="py-6 text-center text-sm text-slate-500">
        Loading policy comparison…
      </div>
    );
  }

  const isSelectedActive = selectedPolicyId === activeConfig;

  return (
    <div className="space-y-3">
      {/* Active policy banner */}
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
          {activeMeta.label}
        </div>
        {!compact && (
          <div className="mt-1 text-[11px] leading-snug text-slate-500">
            {activeMeta.description}
          </div>
        )}
      </div>

      {/* Inspect / Apply controls */}
      <div className="space-y-1.5">
        <div className="text-[9px] font-bold uppercase tracking-wider text-slate-500">
          Inspect Policy
        </div>
        <div className="flex items-center gap-1.5">
          <select
            value={selectedPolicyId}
            onChange={(e) => {
              setSelectedPolicyId(e.target.value as PolicyId);
              setUserPickedPolicy(true);
            }}
            className="flex-1 rounded border border-slate-700 bg-slate-900 px-2 py-1 text-[11px] text-slate-200 focus:border-slate-500 focus:outline-none"
          >
            {POLICIES.map((p) => (
              <option key={p.id} value={p.id}>
                {p.label}
                {p.id === activeConfig ? " · active" : ""}
              </option>
            ))}
          </select>
          <button
            onClick={() => handleApply(selectedPolicyId)}
            disabled={applying || isSelectedActive}
            className="rounded border border-sky-700 bg-sky-950/40 px-2.5 py-1 text-[10px] font-bold uppercase tracking-wider text-sky-300 transition-colors hover:bg-sky-900/60 disabled:cursor-not-allowed disabled:opacity-40"
          >
            {applying ? "Applying…" : isSelectedActive ? "Active" : "Apply"}
          </button>
        </div>

        {/* Quick-set pills + test */}
        <div className="flex items-center gap-1">
          {POLICIES.map((p) => (
            <button
              key={p.id}
              onClick={() => handleApply(p.id)}
              disabled={applying || activeConfig === p.id}
              className={`rounded border px-1.5 py-0.5 text-[9px] font-bold uppercase tracking-wider transition-colors disabled:opacity-50 ${
                activeConfig === p.id ? p.toneActive : p.toneInactive
              }`}
              title={`Apply ${p.label}`}
            >
              {p.id === "weak" ? "Weak" : "Hardened"}
            </button>
          ))}
          <button
            onClick={handleTestConfig}
            disabled={testing}
            className="ml-auto rounded border border-sky-700 bg-sky-950/30 px-1.5 py-0.5 text-[9px] font-bold uppercase tracking-wider text-sky-400 transition-colors hover:bg-sky-900/30 disabled:opacity-40"
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
          <div className="flex items-start gap-2">
            {!testResult.blocked && (
              // Aggressive raccoon for the "attack got through"
              // moment — visual reinforcement that this is bad.
              // eslint-disable-next-line @next/next/no-img-element
              <img
                src="/rook-forward-aggressive-transparent-web.png"
                alt=""
                className="h-7 w-7 shrink-0"
              />
            )}
            <div className="min-w-0 flex-1">
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
          </div>
        </div>
      )}

      {/* Dynamic policy evaluation — driven by the dropdown selection.
          Rows that differ from the currently-active policy get a
          "would change" tag so the user can see what applying the
          selected policy would actually do. */}
      {!compact && (
        <div>
          <div className="mb-1.5 flex items-center justify-between">
            <div className="text-[9px] font-bold uppercase tracking-wider text-slate-500">
              Evaluation · {selectedMeta.label}
            </div>
            {!isSelectedActive && (
              <span className="rounded bg-amber-950/30 px-1.5 py-0.5 text-[9px] font-bold uppercase text-amber-400">
                {changeCount} change{changeCount === 1 ? "" : "s"} vs active
              </span>
            )}
          </div>
          <div className="space-y-1.5">
            {evaluation.map((r, i) => (
              <div
                key={i}
                className={`rounded border p-2 text-[11px] ${
                  r.differsFromActive
                    ? "border-amber-900/40 bg-amber-950/10"
                    : "border-slate-800/60 bg-slate-900/30"
                }`}
              >
                <div className="flex items-center justify-between gap-2">
                  <span className="font-medium text-slate-300">{r.zonePair}</span>
                  <div className="flex items-center gap-1.5">
                    <ActionChip action={r.action} />
                    {r.differsFromActive && (
                      <span className="text-[9px] font-bold uppercase text-amber-400">
                        → would change
                      </span>
                    )}
                  </div>
                </div>
                {r.rule && (
                  <div className="mt-1 text-[10px] leading-snug text-slate-500">{r.rule}</div>
                )}
              </div>
            ))}
          </div>
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
