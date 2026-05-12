"use client";

// Self-contained "containd Segmentation" panel. Originally lived inside
// the Feeder HMI substation tab; lifted into its own file so the
// Network Map can render the exact same panel as a collapsible overlay
// without duplicating the apply logic.
//
// The component manages its own state (active config, comparison) and
// only depends on React Query for cross-component cache invalidation
// when the user applies a new config — that lets the network map's
// edge labels and the header policy badge update immediately when the
// firewall flips between weak and improved.

import { useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  getActiveFirewallConfig,
  getFirewallComparison,
  applyFirewallConfig,
  getSubstationNetworkEvents,
  type NetworkEvent,
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

        {/* Quick-set pills */}
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
        </div>
      </div>

      {/* Live containd DPI events — shows real allow/deny decisions
          on the wire so students can watch the policy enforce when
          they fire a probe from a terminal. Empty until traffic flows. */}
      <LiveEvents compact={compact} />

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

// containd reports verdicts via the (type, severity, details) tuple
// rather than a single field. "alert" events and "critical" severity
// indicate a deny; anything containing BLOCKED in the details string
// is also a deny. Everything else is inspected-and-allowed traffic.
function eventVerdict(e: NetworkEvent): "ALLOW" | "DENY" {
  if (e.type === "alert") return "DENY";
  if (e.severity === "critical") return "DENY";
  if (e.details && /blocked|denied|deny/i.test(e.details)) return "DENY";
  return "ALLOW";
}

function LiveEvents({ compact }: { compact: boolean }) {
  const [expanded, setExpanded] = useState(true);
  // 2.5s poll matches the substation Command Audit cadence and keeps
  // the strip responsive without burning bandwidth when nothing is
  // happening on the wire.
  const { data } = useQuery({
    queryKey: ["segmentation", "live-events"],
    queryFn: getSubstationNetworkEvents,
    refetchInterval: 2500,
    refetchOnWindowFocus: false,
  });

  const events = data?.events ?? [];
  const unavailable = data?.source === "unavailable";
  const denyCount = events.filter((e) => eventVerdict(e) === "DENY").length;
  const maxRows = compact ? 5 : 10;

  return (
    <div>
      <button
        onClick={() => setExpanded(!expanded)}
        className="mb-1.5 flex w-full items-center justify-between text-[9px] font-bold uppercase tracking-wider text-slate-500 hover:text-slate-300"
      >
        <span className="flex items-center gap-2">
          Live DPI Events
          {events.length > 0 && (
            <span className="rounded bg-slate-800/60 px-1.5 py-0.5 text-slate-400 normal-case tracking-normal">
              {events.length} total · {denyCount} deny
            </span>
          )}
        </span>
        <span className="text-slate-600">{expanded ? "▼" : "▶"}</span>
      </button>

      {expanded && (
        <div className={`space-y-1 overflow-y-auto rounded border border-slate-800/60 bg-slate-900/30 p-1.5 ${compact ? "max-h-32" : "max-h-56"}`}>
          {unavailable ? (
            <div className="py-2 text-center text-[10px] text-slate-600">
              containd unreachable
            </div>
          ) : events.length === 0 ? (
            <div className="py-2 text-center text-[10px] text-slate-600">
              No recent events — run a probe from a terminal to see enforcement
            </div>
          ) : (
            events.slice(0, maxRows).map((e, i) => <LiveEventRow key={`${e.id}-${i}`} e={e} />)
          )}
        </div>
      )}
    </div>
  );
}

function LiveEventRow({ e }: { e: NetworkEvent }) {
  const verdict = eventVerdict(e);
  const rowTone =
    verdict === "DENY"
      ? "border-red-900/40 bg-red-950/15"
      : "border-slate-800/60 bg-slate-900/40";
  const ts = e.timestamp
    ? new Date(e.timestamp).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" })
    : "--";

  return (
    <div className={`rounded border p-1.5 text-[10px] ${rowTone}`}>
      <div className="flex items-center gap-1.5">
        <span className="w-14 shrink-0 text-[9px] text-slate-600">{ts}</span>
        <ActionChip action={verdict} />
        <span className="font-mono text-slate-300">{e.source} → {e.dest}</span>
        {e.protocol && e.protocol !== "-" && (
          <span className="ml-auto text-[9px] text-purple-400">[{e.protocol}]</span>
        )}
      </div>
      {e.details && (
        <div className="ml-14 mt-0.5 text-[10px] leading-tight text-slate-500">
          {e.details}
        </div>
      )}
    </div>
  );
}
