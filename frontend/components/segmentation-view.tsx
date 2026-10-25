"use client";

// Self-contained "containd Segmentation" panel. Originally lived inside
// the Feeder HMI substation tab; lifted into its own file so the
// Network Map can render the exact same panel as a collapsible overlay
// without duplicating the apply logic.
//
// Active policy and package comparison use React Query so the range
// lifecycle clears them when one package is replaced by another.

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ApiError,
  getActiveFirewallConfig,
  getFirewallComparison,
  type NetworkEvent,
} from "../lib/api";
import { FIREWALL_ACTIVE_KEY, useSubstationNetworkEvents } from "../lib/live-queries";
import { useOperatorView, usePackagePolicies } from "../lib/operator-queries";
import { policyApplyErrorText, useApplyPolicy } from "../lib/policy-apply";
import {
  HARDENED_POLICY,
  isHardened,
  activePolicyWording,
  policyWording,
  type PackagePolicies,
  type PolicyWording,
} from "../lib/policy-wording";
import { errorMessage } from "../lib/utils";
import { EU_OPERATOR_VIEW, isView } from "../lib/operator-view";

export const FIREWALL_COMPARISON_KEY = ["firewall-comparison"] as const;

// The policies are the active package's (package.yml `policies`). The
// US package keeps its own wording; any other package's policies show
// by their declared names, without a description.
const US_DRAWER_WORDING: Record<string, PolicyWording> = {
  weak: {
    label: "Weak Baseline",
    description:
      "All zones can reach field devices directly. Attackers have clear paths to breakers and regulators.",
  },
  improved: {
    label: "Hardened Segmentation",
    description: "Only the RTAC can reach field devices. Enterprise and vendor zones are blocked.",
  },
};
const US_PILL_WORDING: Record<string, PolicyWording> = {
  weak: { label: "Weak", description: "" },
  improved: { label: "Hardened", description: "" },
};
const EU_DRAWER_WORDING: Record<string, PolicyWording> = {
  weak: {
    label: "weak",
    description:
      "Enterprise, vendor, and Control Centre sources can reach the RTU over IEC 104; any station-zone host can reach the feeder breaker over Modbus TCP.",
  },
  improved: {
    label: "improved",
    description:
      "Only the Control Centre (10.60.60.20/32) can reach the RTU (10.30.30.20/32) over TCP/2404, and only the RTU (10.30.30.20/32) can reach the feeder breaker (10.40.40.20/32) over TCP/502.",
  },
};

// The columns of PolicyComparison.diffs per named policy pair.
type ComparisonFields = {
  actionField: "weak_action" | "improved_action";
  ruleField: "weak_rule" | "improved_rule";
};
const COMPARISON_FIELDS: Record<string, ComparisonFields> = {
  weak: { actionField: "weak_action", ruleField: "weak_rule" },
  improved: { actionField: "improved_action", ruleField: "improved_rule" },
};

type PolicyMeta = PolicyWording & {
  id: string;
  pill: string;
  toneActive: string;
  toneInactive: string;
};

function policyMetas(policies: PackagePolicies, euWording: boolean): PolicyMeta[] {
  return policies.names.map((id) => ({
    id,
    ...(euWording
      ? EU_DRAWER_WORDING[id] ?? { label: id, description: "" }
      : policyWording(id, policies, US_DRAWER_WORDING)),
    pill: policyWording(id, policies, US_PILL_WORDING).label,
    toneActive: isHardened(id)
      ? "border-green-700/60 bg-green-950/15 text-green-400"
      : "border-red-700/60 bg-red-950/15 text-red-400",
    toneInactive: isHardened(id)
      ? "border-slate-700 text-slate-400 hover:border-green-700 hover:text-green-400"
      : "border-slate-700 text-slate-400 hover:border-red-700 hover:text-red-400",
  }));
}

// The active config may be custom, not one of the named comparison
// columns. Never substitute the baseline's metadata for it.
function activePolicyMeta(
  metas: PolicyMeta[],
  activeConfig: string | null,
  policies: PackagePolicies,
): PolicyMeta | undefined {
  const declared = metas.find((p) => p.id === activeConfig);
  if (declared || !activeConfig) return declared ?? metas[0];
  const wording = activePolicyWording(activeConfig, policies, US_DRAWER_WORDING);
  return { id: activeConfig, ...wording, pill: wording.label, toneActive: "", toneInactive: "" };
}

export function SegmentationView({ compact = false }: { compact?: boolean }) {
  const packagePolicies = usePackagePolicies();
  const operatorView = useOperatorView();
  const euWording = isView(operatorView, EU_OPERATOR_VIEW);
  const apply = useApplyPolicy();
  const applying = apply.isPending;
  // Which policy the evaluation panel is previewing. Defaults to
  // whatever is currently applied so the panel opens "self-consistent".
  // Track whether the user has manually changed the selection so we
  // don't keep snapping back to the active config when they're trying
  // to inspect a different one.
  const [selectedPolicyId, setSelectedPolicyId] = useState<string>(HARDENED_POLICY);
  const [userPickedPolicy, setUserPickedPolicy] = useState(false);

  // Active firewall config - shared via React Query so this drawer
  // and the top-right PolicyBadge stay in sync whichever entry point
  // the student uses to flip policy. Polling every 5s also picks up
  // manual containd commits the policy-observer flips to
  // "manual-custom" within its grace window.
  const { data: activeData } = useQuery({
    queryKey: FIREWALL_ACTIVE_KEY,
    queryFn: getActiveFirewallConfig,
    refetchInterval: 5000,
    staleTime: 2000,
  });
  const activeConfig = activeData?.active_config ?? null;
  const policies = useMemo(
    () => (packagePolicies ? policyMetas(packagePolicies, euWording) : []),
    [packagePolicies, euWording],
  );
  const comparisonQuery = useQuery({
    queryKey: FIREWALL_COMPARISON_KEY,
    queryFn: getFirewallComparison,
    retry: false,
  });

  useEffect(() => {
    if (!userPickedPolicy && policies.some((p) => p.id === activeConfig)) {
      setSelectedPolicyId(activeConfig as string);
    }
    // userPickedPolicy intentionally excluded so the initial sync
    // happens but a manual pick stays sticky.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeConfig, policies]);

  // The apply refreshes every firewall view (lib/policy-apply.ts); on
  // success the inspector re-syncs with the policy now active.
  const handleApply = (config: string) => {
    apply.mutate(config, {
      onSuccess: () => {
        setSelectedPolicyId(config);
        setUserPickedPolicy(false);
      },
    });
  };
  const failedMeta = policies.find((p) => p.id === apply.variables);
  const applyFailure = apply.error
    ? policyApplyErrorText(failedMeta?.label ?? apply.variables ?? "", apply.error)
    : null;

  // Build the evaluation rows for the currently-selected policy. Each
  // row reads the chosen policy's action/rule columns from the diff
  // and tags whether it differs from what's currently active - that
  // tag drives the "would change if applied" highlight.
  const selectedMeta = policies.find((p) => p.id === selectedPolicyId) ?? policies[0];
  const activeMeta = packagePolicies ? activePolicyMeta(policies, activeConfig, packagePolicies) : undefined;
  const comparison = comparisonQuery.data;
  const selectedFields = selectedMeta ? COMPARISON_FIELDS[selectedMeta.id] : undefined;
  const activeFields = activeMeta ? COMPARISON_FIELDS[activeMeta.id] : undefined;
  const evaluation = useMemo(() => {
    if (!comparison || !selectedFields) return [];
    return comparison.diffs.map((d) => {
      const action = d[selectedFields.actionField];
      const rule = d[selectedFields.ruleField];
      const activeAction = activeFields ? d[activeFields.actionField] : undefined;
      const differsFromActive = activeAction !== undefined && action !== activeAction;
      return { zonePair: d.zone_pair, action, rule, differsFromActive };
    });
  }, [comparison, selectedFields, activeFields]);
  const changeCount = evaluation.filter((r) => r.differsFromActive).length;

  if (!packagePolicies || !selectedMeta || !activeMeta) {
    return (
      <div className="py-6 text-center text-sm text-slate-500">
        Loading policy controls…
      </div>
    );
  }

  const isSelectedActive = selectedMeta.id === activeConfig;
  const activeHardened = isHardened(activeConfig);
  const activeTone = activeHardened
    ? "border-green-700/60 bg-green-950/15 text-green-400"
    : activeConfig === "weak"
    ? "border-red-700/60 bg-red-950/15 text-red-400"
    : "border-slate-700 bg-slate-900/50 text-slate-300";
  const activeTextTone = activeHardened
    ? "text-green-400"
    : activeConfig === "weak"
    ? "text-red-400"
    : "text-slate-300";
  const comparisonUnsupported =
    comparisonQuery.error instanceof ApiError && comparisonQuery.error.status === 404;

  return (
    <div className="space-y-3">
      {/* Active policy banner */}
      <div className={`rounded-lg border p-2.5 ${activeTone}`}>
        <div className="text-[9px] font-bold uppercase tracking-wider text-slate-500">
          containd NGFW · Active Policy
        </div>
        <div className={`mt-0.5 text-sm font-bold ${activeTextTone}`}>
          {activeMeta.label}
        </div>
        {!compact && activeMeta.description && (
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
            value={selectedMeta.id}
            onChange={(e) => {
              setSelectedPolicyId(e.target.value);
              setUserPickedPolicy(true);
            }}
            className="flex-1 rounded-sm border border-slate-700 bg-slate-900 px-2 py-1 text-[11px] text-slate-200 focus:border-slate-500 focus:outline-hidden"
          >
            {policies.map((p) => (
              <option key={p.id} value={p.id}>
                {p.label}
                {p.id === activeConfig ? " · active" : ""}
              </option>
            ))}
          </select>
          <button
            onClick={() => handleApply(selectedMeta.id)}
            disabled={applying || isSelectedActive}
            className="rounded-sm border border-sky-700 bg-sky-950/40 px-2.5 py-1 text-[10px] font-bold uppercase tracking-wider text-sky-300 transition-colors hover:bg-sky-900/60 disabled:cursor-not-allowed disabled:opacity-40"
          >
            {applying ? "Applying…" : isSelectedActive ? "Active" : "Apply"}
          </button>
        </div>

        {/* Quick-set pills */}
        <div className="flex items-center gap-1">
          {policies.map((p) => (
            <button
              key={p.id}
              onClick={() => handleApply(p.id)}
              disabled={applying || activeConfig === p.id}
              className={`rounded border px-1.5 py-0.5 text-[9px] font-bold uppercase tracking-wider transition-colors disabled:opacity-50 ${
                activeConfig === p.id ? p.toneActive : p.toneInactive
              }`}
              title={`Apply ${p.label}`}
            >
              {p.pill}
            </button>
          ))}
        </div>
        {applyFailure && (
          <div role="alert" className="text-[10px] leading-snug text-red-400">
            {applyFailure}
          </div>
        )}
      </div>

      {/* Live containd DPI events - shows real allow/deny decisions
          on the wire so students can watch the policy enforce when
          they fire a probe from a terminal. Empty under the weak
          baseline by design (no rules have log:true). */}
      <LiveEvents compact={compact} activeConfig={activeConfig} activePolicyLabel={activeConfig ? activeMeta.label : "Policy loading…"} usWording={packagePolicies.usWording} />

      {/* Dynamic policy evaluation - driven by the dropdown selection.
          Rows that differ from the currently-active policy get a
          "would change" tag so the user can see what applying the
          selected policy would actually do. */}
      {!compact && (
        <div>
          <div className="mb-1.5 text-[9px] font-bold uppercase tracking-wider text-slate-500">
            Named policy comparison
          </div>
          <div className="mb-2 text-[10px] leading-snug text-slate-600">
            Named-policy preview is separate from live sessions and firewall enforcement.
          </div>
          {comparisonQuery.isPending && (
            <div className="text-[10px] leading-snug text-slate-500">
              Loading named policy comparison…
            </div>
          )}
          {comparisonUnsupported && (
            <div className="text-[10px] leading-snug text-slate-500">
              Named policy comparison is not available for this package.
            </div>
          )}
          {comparisonQuery.isError && !comparisonUnsupported && (
            <div className="text-[10px] leading-snug text-red-400">
              Policy comparison unavailable: {errorMessage(comparisonQuery.error)}
            </div>
          )}
          {comparison && selectedFields && (
            <>
              <div className="mb-1.5 flex items-center justify-between">
                <div className="text-[9px] font-bold uppercase tracking-wider text-slate-500">
                  Preview · {selectedMeta.label}
                </div>
                {activeFields && !isSelectedActive && (
                  <span className="rounded-sm bg-amber-950/30 px-1.5 py-0.5 text-[9px] font-bold uppercase text-amber-400">
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
            </>
          )}
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
    <span className={`rounded-sm border px-1.5 py-0.5 text-[9px] font-bold ${cls}`}>
      {action}
    </span>
  );
}

// eventVerdict returns the kernel-side disposition for firewall.rule.hit
// events ONLY. IDS anomalies are NOT treated as DENY here even though
// they're suspicious - anomalies are alerts, not drops, and conflating
// them inflates the deny counter + paints amber-categorized rows red.
// Operators read "N deny" as "the firewall actually stopped N packets";
// anomalies belong in the separate `ids` count alongside the amber
// row tone. (Review on PR #47 caught the original conflation.)
//
function eventVerdict(e: NetworkEvent): "ALLOW" | "DENY" {
  if (e.attributes?.action === "DENY") return "DENY";
  return "ALLOW";
}

// Classify an event for visual chip + label purposes. Three kinds reach
// this strip; each gets a distinct look so an operator can tell at a
// glance whether they're looking at "policy made a verdict" vs "DPI
// decoded a protocol message" vs "IDS noticed something off."
type EventCategory = "rule" | "dpi" | "ids" | "other";
function eventCategory(e: NetworkEvent): EventCategory {
  if (e.kind === "firewall.rule.hit") return "rule";
  if (e.kind === "anomaly") return "ids";
  // Modbus/DNP3 decoder events come through with kind="request" or
  // "response"; lumping both as "dpi" keeps the chip set tight.
  if (e.kind === "request" || e.kind === "response") return "dpi";
  return "other";
}

// Compose a one-line identity for each event kind. firewall.rule.hit
// uses ruleId; "request" uses the function code so students can see
// "FC=3 (read)" vs "FC=6 (write)"; "anomaly" uses the anomaly_type.
function eventLabel(e: NetworkEvent): string {
  const a = e.attributes ?? {};
  switch (eventCategory(e)) {
    case "rule":
      return a.ruleId ?? "(unnamed rule)";
    case "dpi": {
      const fc = a.function_code;
      const dir = e.kind === "response" ? "resp" : "req";
      const proto = a.proto ?? e.transport ?? "";
      return fc !== undefined ? `${proto} FC=${fc} (${dir})` : `${proto} ${dir}`;
    }
    case "ids":
      return a.anomaly_type ?? a.message ?? "IDS anomaly";
    default:
      return e.kind ?? "event";
  }
}

function eventEndpoints(e: NetworkEvent): { src: string; dst: string } {
  return {
    src: e.srcIp ?? "?",
    dst: e.dstIp ?? "?",
  };
}

function LiveEvents({
  compact,
  activeConfig,
  activePolicyLabel,
  usWording,
}: {
  compact: boolean;
  activeConfig: string | null;
  activePolicyLabel: string;
  usWording: boolean;
}) {
  const [expanded, setExpanded] = useState(true);
  // 2.5s poll matches the substation Command Audit cadence and keeps
  // the strip responsive without burning bandwidth when nothing is
  // happening on the wire.
  const { data } = useSubstationNetworkEvents(2500, { refetchOnWindowFocus: false });

  const events = data?.events ?? [];
  const unavailable = data?.source === "unavailable";
  const denyCount = events.filter((e) => eventVerdict(e) === "DENY").length;
  // Per-kind counts give operators a quick sense of what's flowing:
  // many rule.hit ALLOW + zero ids = normal RTAC chatter; ids > 0 or
  // rule.hit DENY > 0 means something is being noticed.
  const ruleCount = events.filter((e) => eventCategory(e) === "rule").length;
  const dpiCount = events.filter((e) => eventCategory(e) === "dpi").length;
  const idsCount = events.filter((e) => eventCategory(e) === "ids").length;
  const maxRows = compact ? 5 : 10;

  return (
    <div>
      <button
        onClick={() => setExpanded(!expanded)}
        className="mb-1.5 flex w-full items-center justify-between text-[9px] font-bold uppercase tracking-wider text-slate-500 hover:text-slate-300"
      >
        <span className="flex items-center gap-2">
          Live Firewall Events · {activePolicyLabel}
          {events.length > 0 && (
            <span className="rounded-sm bg-slate-800/60 px-1.5 py-0.5 text-slate-400 normal-case tracking-normal">
              {ruleCount} rule · {dpiCount} dpi · {idsCount} ids · {denyCount} deny
            </span>
          )}
        </span>
        <span className="text-slate-600">{expanded ? "▼" : "▶"}</span>
      </button>

      {expanded && (
        <div className={`space-y-1 overflow-y-auto rounded-sm border border-slate-800/60 bg-slate-900/30 p-1.5 ${compact ? "max-h-32" : "max-h-56"}`}>
          {unavailable ? (
            <div className="py-2 text-center text-[10px] text-slate-600">
              containd unreachable
            </div>
          ) : events.length === 0 ? (
            <EmptyEventsHint compact={compact} activeConfig={activeConfig} usWording={usWording} />
          ) : (
            events.slice(0, maxRows).map((e, i) => <LiveEventRow key={`${e.id}-${i}`} e={e} />)
          )}
        </div>
      )}
    </div>
  );
}

// EmptyEventsHint - policy-aware empty-state message for the Live Firewall
// Events panel. Under the weak baseline the panel is silent by design
// (no shipped rule has log:true - mirrors a real misconfigured
// firewall and is part of the segmentation lesson); under hardened
// the panel is "empty because no probe has been fired yet"; outside
// those two there's not enough context to be more specific. The two
// specific hints describe the US policy files and tools, so another
// package gets the generic one.
function EmptyEventsHint({
  compact,
  activeConfig,
  usWording,
}: {
  compact: boolean;
  activeConfig: string | null;
  usWording: boolean;
}) {
  if (usWording && activeConfig === "weak") {
    return (
      <div className="space-y-1 py-2 px-1 text-[10px] leading-snug text-slate-500">
        <div className="font-medium text-slate-400">
          Weak baseline has no logged rules.
        </div>
        <div>
          No rule in the weak config has <span className="font-mono text-amber-400">log: true</span>,
          so containd emits zero firewall.rule.hit events while it&apos;s active.
          {!compact && " That silence IS part of the weak-baseline lesson - real misconfigured firewalls also lack observability."}
        </div>
        <div>
          To see this panel light up, <span className="text-slate-300">Apply Hardened</span> in the
          controls above - the hardened config has logging on every cross-zone rule.
        </div>
      </div>
    );
  }
  if (usWording && activeConfig === "improved") {
    return (
      <div className="py-2 px-1 text-[10px] leading-snug text-slate-500">
        Hardened policy is active and rules have <span className="font-mono text-amber-400">log: true</span>,
        but no traffic has matched yet. Run a probe from a terminal (e.g.{" "}
        <span className="font-mono text-slate-300">mbpoll</span> or{" "}
        <span className="font-mono text-slate-300">dnp3poll</span> from kali) to see enforcement.
      </div>
    );
  }
  return (
    <div className="py-2 text-center text-[10px] text-slate-600">
      No recent events - run a probe from a terminal to see enforcement
    </div>
  );
}

function LiveEventRow({ e }: { e: NetworkEvent }) {
  const verdict = eventVerdict(e);
  const category = eventCategory(e);
  const { src, dst } = eventEndpoints(e);
  const label = eventLabel(e);
  const port = e.dstPort ?? null;
  const proto = e.protocol || e.transport || "";

  // Row tone is dominated by DENY (red) since that's the highest-value
  // signal for the operator. ALLOWs use the standard slate background;
  // anomaly events are toned amber so they stand out without screaming.
  const rowTone =
    verdict === "DENY"
      ? "border-red-900/40 bg-red-950/15"
      : category === "ids"
      ? "border-amber-900/40 bg-amber-950/10"
      : "border-slate-800/60 bg-slate-900/40";
  const ts = e.timestamp
    ? new Date(e.timestamp).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" })
    : "--";

  return (
    <div className={`rounded-sm border p-1.5 text-[10px] ${rowTone}`}>
      <div className="flex items-center gap-1.5">
        <span className="w-14 shrink-0 text-[9px] text-slate-600">{ts}</span>
        {/* For firewall.rule.hit rows show the ALLOW/DENY action chip;
            for dpi/ids rows show a kind chip in its own color so the
            three event sources are visually distinct in a mixed list. */}
        {category === "rule" ? (
          <ActionChip action={verdict} />
        ) : (
          <KindChip category={category} />
        )}
        <span className="font-mono text-slate-300">
          {src} → {dst}
          {port ? `:${port}` : ""}
        </span>
        <span className="ml-auto truncate text-[9px] text-slate-400">{label}</span>
        {proto && proto !== "-" && (
          <span className="text-[9px] text-purple-400">[{proto}]</span>
        )}
      </div>
    </div>
  );
}

// Small colored chip for non-firewall.rule.hit event categories so the
// strip remains scannable when DPI decode + IDS anomaly rows are
// interleaved with policy verdicts. Mirrors ActionChip's footprint.
function KindChip({ category }: { category: EventCategory }) {
  const meta =
    category === "dpi"
      ? { label: "DPI", cls: "text-sky-400 border-sky-800/40" }
      : category === "ids"
      ? { label: "IDS", cls: "text-amber-400 border-amber-800/40" }
      : { label: "EVT", cls: "text-slate-400 border-slate-700/40" };
  return (
    <span
      className={`rounded-sm border px-1.5 py-0.5 text-[9px] font-bold ${meta.cls}`}
    >
      {meta.label}
    </span>
  );
}
