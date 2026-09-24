"use client";

import React, { useMemo, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { applyFirewallConfig, ZoneRuleSummary } from "../lib/api";
import {
  INTRA_ZONE_FLOWS,
  ViewMode,
  containdZoneToNetwork,
  humanizeSummary,
  humanZoneName,
} from "../lib/network-console-data";
import { zoneColors } from "../lib/zone-colors";
import { Tooltip, TooltipTrigger, TooltipContent } from "./ui/tooltip";
import { SegmentationView } from "./segmentation-view";
import { TrafficMatrixView } from "./traffic-matrix-view";
import {
  Activity,
  ChevronDown,
  ChevronLeft,
  Layers,
  ScrollText,
  Shield,
} from "lucide-react";

export function InfoRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between rounded-lg border border-slate-800 bg-slate-900 px-3 py-2">
      <span className="text-slate-400">{label}</span>
      <span className="text-slate-100">{value}</span>
    </div>
  );
}

export function StatusDot({
  ok,
  label,
  tooltipTitle,
  tooltipBody,
}: {
  ok: boolean;
  label: string;
  tooltipTitle?: string;
  tooltipBody?: string;
}) {
  const dot = (
    <span className="flex cursor-help items-center gap-1.5 text-slate-400">
      <span className={`h-2 w-2 rounded-full ${ok ? "bg-green-500" : "bg-red-500"}`} />
      {label}
    </span>
  );
  if (!tooltipTitle && !tooltipBody) return dot;
  return (
    <Tooltip>
      <TooltipTrigger asChild>{dot}</TooltipTrigger>
      <TooltipContent className="max-w-[260px]">
        {tooltipTitle && (
          <div className="mb-0.5 text-[10px] font-bold uppercase tracking-wider text-sky-300">
            {tooltipTitle}
          </div>
        )}
        {tooltipBody && <div className="text-[11px] leading-snug">{tooltipBody}</div>}
      </TooltipContent>
    </Tooltip>
  );
}

// Policy badge + quick-switch dropdown in the header. Styled as a
// rounded rectangle (matching the drawer button shapes) that doubles
// as a dropdown trigger. Clicking reveals a small menu of available
// policies so the student can switch without opening the drawer.
export function PolicyBadge({ activeConfig }: { activeConfig?: string }) {
  const [open, setOpen] = useState(false);
  const [applying, setApplying] = useState(false);
  const queryClient = useQueryClient();

  const hardened = activeConfig === "improved";

  const policies = [
    { id: "weak", label: "Weak Baseline", desc: "All zones allowed - over-permissive" },
    { id: "improved", label: "Hardened", desc: "Deny-default, RTAC-only field access" },
  ];

  const handleApply = async (config: string) => {
    if (config === activeConfig || applying) return;
    setApplying(true);
    setOpen(false);
    try {
      await applyFirewallConfig(config as "weak" | "improved");
      queryClient.invalidateQueries({ queryKey: ["firewall-rules"] });
      queryClient.invalidateQueries({ queryKey: ["workshop", "status"] });
      // SegmentationView (the left drawer) subscribes to this; without
      // invalidating it here, flipping policy from the badge left
      // the drawer's Active Policy banner stuck on the prior value
      // until its own 5s refetch caught up.
      queryClient.invalidateQueries({ queryKey: ["firewall-active"] });
    } catch {
      // swallow
    } finally {
      setApplying(false);
    }
  };

  return (
    <div className="relative">
      <button
        onClick={() => setOpen(!open)}
        className={`inline-flex items-center gap-1.5 rounded-lg border px-3 py-1.5 text-[11px] font-bold uppercase tracking-wider transition-colors ${
          hardened
            ? "border-green-600/60 bg-green-950/40 text-green-300 hover:bg-green-950/60"
            : "border-red-600/60 bg-red-950/40 text-red-300 hover:bg-red-950/60"
        }`}
      >
        {hardened ? (
          <Shield className="h-3 w-3" />
        ) : (
          // eslint-disable-next-line @next/next/no-img-element
          <img
            src="/rook-forward-aggressive-transparent-web.png"
            alt=""
            className="h-4 w-4"
          />
        )}
        <span>Firewall · {hardened ? "Hardened" : "Weak baseline"}</span>
        <ChevronDown className={`h-3 w-3 transition-transform ${open ? "rotate-180" : ""}`} />
      </button>

      {open && (
        <>
          {/* Click-outside backdrop */}
          <div
            className="fixed inset-0 z-40"
            onClick={() => setOpen(false)}
          />
          <div className="absolute right-0 top-full z-50 mt-1 w-56 rounded-lg border border-slate-700 bg-slate-900/95 py-1 shadow-2xl backdrop-blur">
            <div className="px-3 py-1.5 text-[9px] font-bold uppercase tracking-wider text-slate-500">
              Select Policy
            </div>
            {policies.map((p) => {
              const isActive = p.id === activeConfig;
              return (
                <button
                  key={p.id}
                  onClick={() => handleApply(p.id)}
                  disabled={isActive || applying}
                  className={`flex w-full items-start gap-2 px-3 py-2 text-left transition-colors ${
                    isActive
                      ? "bg-slate-800/60"
                      : "hover:bg-slate-800/40"
                  } disabled:cursor-default`}
                >
                  <span
                    className={`mt-0.5 h-2 w-2 shrink-0 rounded-full ${
                      isActive
                        ? p.id === "improved" ? "bg-green-400" : "bg-red-400"
                        : "bg-slate-600"
                    }`}
                  />
                  <div className="min-w-0">
                    <div className="text-[11px] font-semibold text-slate-200">
                      {p.label}
                      {isActive && (
                        <span className="ml-1.5 text-[9px] font-bold uppercase text-slate-500">
                          active
                        </span>
                      )}
                    </div>
                    <div className="text-[10px] leading-snug text-slate-500">
                      {p.desc}
                    </div>
                  </div>
                </button>
              );
            })}
          </div>
        </>
      )}
    </div>
  );
}

// Left drawer menu attached to the left edge of the React Flow
// canvas. The strip is always 36px wide and shows a stack of icon
// buttons - one per registered drawer. Clicking any icon opens that
// drawer's content in a 420px panel that sits next to the strip.
// Clicking the header chevron collapses back to strip-only.
//
// Drawers mount whenever Policy view is on; both icons are always
// visible. The Traffic Matrix drawer owns the canvas "traffic edges"
// toggle as an internal header control, so the top toolbar no longer
// has a separate "Traffic view" button.
type DrawerKey = "segmentation" | "traffic";
const DRAWER_META: Record<
  DrawerKey,
  { label: string; Icon: typeof Shield; iconColor: string }
> = {
  segmentation: {
    label: "Segmentation Policy",
    Icon: Shield,
    iconColor: "text-amber-400",
  },
  traffic: {
    label: "Traffic Matrix",
    Icon: Activity,
    iconColor: "text-cyan-400",
  },
};
const DRAWER_ORDER: DrawerKey[] = ["segmentation", "traffic"];

export function LeftDrawerMenu({
  active,
  onChange,
  trafficEdgesOn,
  onToggleTrafficEdges,
  trafficFilter,
  onTrafficFilterChange,
  highlightedTrafficPair,
  onHighlightTrafficPair,
}: {
  active: DrawerKey | null;
  onChange: (id: DrawerKey | null) => void;
  trafficEdgesOn: boolean;
  onToggleTrafficEdges: () => void;
  trafficFilter: { zone: string; crossZoneOnly: boolean };
  onTrafficFilterChange: (f: { zone: string; crossZoneOnly: boolean }) => void;
  highlightedTrafficPair: string | null;
  onHighlightTrafficPair: (key: string | null) => void;
}) {
  const activeMeta = active ? DRAWER_META[active] : null;

  const renderContent = () => {
    if (active === "segmentation") return <SegmentationView />;
    if (active === "traffic") {
      return (
        <TrafficMatrixView
          canvasEdgesOn={trafficEdgesOn}
          onToggleCanvasEdges={onToggleTrafficEdges}
          filter={trafficFilter}
          onFilterChange={onTrafficFilterChange}
          highlightedPair={highlightedTrafficPair}
          onHighlightPair={onHighlightTrafficPair}
        />
      );
    }
    return null;
  };

  return (
    <>
      {/* Always-visible icon strip on the left edge */}
      <div className="absolute left-0 top-0 z-30 flex h-full w-9 flex-col items-center gap-1 border-r border-slate-700 bg-slate-900/95 py-3 backdrop-blur">
        {DRAWER_ORDER.map((id) => {
          const { label, Icon, iconColor } = DRAWER_META[id];
          const isActive = active === id;
          return (
            <Tooltip key={id}>
              <TooltipTrigger asChild>
                <button
                  onClick={() => onChange(isActive ? null : id)}
                  aria-label={label}
                  className={`flex h-7 w-7 items-center justify-center rounded-md border transition-colors ${
                    isActive
                      ? "border-slate-600 bg-slate-800"
                      : "border-transparent hover:border-slate-700 hover:bg-slate-800"
                  }`}
                >
                  <Icon className={`h-4 w-4 ${iconColor}`} />
                </button>
              </TooltipTrigger>
              <TooltipContent side="right">{label}</TooltipContent>
            </Tooltip>
          );
        })}
      </div>

      {/* Expanded drawer panel sits flush against the right edge of
          the strip. Same dimensions and chrome regardless of which
          drawer is active. */}
      {activeMeta && (
        <aside className="absolute left-9 top-0 z-30 flex h-full w-[420px] flex-col border-r border-slate-700 bg-slate-950/95 shadow-2xl backdrop-blur">
          <div className="flex items-center justify-between border-b border-slate-800 px-3 py-2.5">
            <div className="flex items-center gap-2">
              <activeMeta.Icon className={`h-4 w-4 ${activeMeta.iconColor}`} />
              <span className="text-[11px] font-bold uppercase tracking-wider text-slate-200">
                {activeMeta.label}
              </span>
            </div>
            <button
              onClick={() => onChange(null)}
              title="Minimize"
              className="rounded p-1 text-slate-400 transition-colors hover:bg-slate-800 hover:text-white"
            >
              <ChevronLeft className="h-4 w-4" />
            </button>
          </div>
          <div className="flex-1 overflow-y-auto p-3">{renderContent()}</div>
        </aside>
      )}
    </>
  );
}

// Floating toolbar above the React Flow canvas. Each toggle is an
// icon-only button (matching the exercise page header style) so the
// strip stays compact even when the segmentation drawer is open.
// Position is shifted past the drawer's collapsed strip so the "VIEW"
// label is never covered.
export function ViewModeToolbar({
  viewMode,
  onChange,
}: {
  viewMode: ViewMode;
  onChange: (m: ViewMode) => void;
}) {
  const IconToggle = ({
    on,
    onClick,
    label,
    Icon,
    activeBorder,
    activeBg,
    activeText,
  }: {
    on: boolean;
    onClick: () => void;
    label: string;
    Icon: React.ComponentType<{ className?: string }>;
    activeBorder: string;
    activeBg: string;
    activeText: string;
  }) => (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          onClick={onClick}
          aria-label={label}
          className={`inline-flex h-7 w-7 items-center justify-center rounded-md border transition-colors ${
            on
              ? `${activeBorder} ${activeBg} ${activeText}`
              : "border-slate-800 bg-slate-900/70 text-slate-500 hover:border-slate-700 hover:text-slate-300"
          }`}
        >
          <Icon className="h-3.5 w-3.5" />
        </button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );

  return (
    <div className="absolute left-14 top-3 z-20 flex items-center gap-2 rounded-lg border border-slate-800 bg-slate-900/85 px-2.5 py-1.5 backdrop-blur">
      <span className="pl-0.5 pr-1 text-[9px] font-semibold uppercase tracking-[0.2em] text-slate-500">
        View
      </span>
      <IconToggle
        on={viewMode.policyDim}
        onClick={() => onChange({ ...viewMode, policyDim: !viewMode.policyDim })}
        label="Policy"
        Icon={ScrollText}
        activeBorder="border-green-700/60"
        activeBg="bg-green-950/30"
        activeText="text-green-400"
      />
      <IconToggle
        on={viewMode.iec62443}
        onClick={() => onChange({ ...viewMode, iec62443: !viewMode.iec62443 })}
        label="Purdue Model View"
        Icon={Layers}
        activeBorder="border-green-700/60"
        activeBg="bg-green-950/30"
        activeText="text-green-400"
      />
    </div>
  );
}

// Right-panel section showing what flows the selected node participates
// in. Two distinct sections so the student can see the teaching point
// at a glance:
//
//   • Intra-zone (firewall-blind): hard-coded peer flows like HMI→RTAC.
//     Important context that the firewall view is incomplete.
//
//   • Cross-zone (firewall-governed): rules from containd, framed as
//     "outbound from this zone" or "inbound to this zone" so the
//     student can correlate the rule list to the selected node.
//
// Note: containd policy is enforced at the zone level, not per host
// IP. We say so explicitly so students don't think a per-node ACL is
// being shown.
export function ActiveFlowsPanel({
  nodeId,
  nodeZone,
  ruleSummaries,
  multiHomedZones,
}: {
  nodeId: string;
  nodeZone?: string;
  ruleSummaries?: ZoneRuleSummary[];
  multiHomedZones: string[];
}) {
  // Cross-zone rules where this node's zone is either source or dest.
  // We split into outbound (this zone is the source) and inbound (this
  // zone is the destination) so the user sees the rule from the
  // selected node's perspective.
  const { outbound, inbound } = useMemo(() => {
    if (!ruleSummaries || !nodeZone) return { outbound: [], inbound: [] };
    const containdZones = Object.entries(containdZoneToNetwork)
      .filter(([_, networks]) => networks.includes(nodeZone))
      .map(([cz]) => cz);
    const out: ZoneRuleSummary[] = [];
    const inb: ZoneRuleSummary[] = [];
    ruleSummaries.forEach((r) => {
      if (r.source_zone === r.dest_zone) return;
      const isSrc = containdZones.includes(r.source_zone);
      const isDst = containdZones.includes(r.dest_zone);
      if (isSrc && !isDst) out.push(r);
      else if (isDst && !isSrc) inb.push(r);
    });
    return { outbound: out, inbound: inb };
  }, [ruleSummaries, nodeZone]);

  // Intra-zone flows that name this node as source or target.
  const intraFlows = useMemo(() => {
    return INTRA_ZONE_FLOWS.filter((f) => f.source === nodeId || f.target === nodeId);
  }, [nodeId]);

  const hasContent =
    outbound.length > 0 ||
    inbound.length > 0 ||
    intraFlows.length > 0 ||
    multiHomedZones.length > 0;
  if (!hasContent) return null;

  const renderRule = (
    r: ZoneRuleSummary,
    direction: "out" | "in",
    key: number,
  ) => {
    const peerZone = direction === "out" ? r.dest_zone : r.source_zone;
    const protos = humanizeSummary(r.summary);
    const protoLabel =
      protos.length === 0
        ? r.summary || "all"
        : protos.length <= 3
        ? protos.join(" / ")
        : `${protos.length} flows`;
    const actionColor =
      r.action === "DENY"
        ? "text-red-400"
        : r.action === "MIXED"
        ? "text-amber-400"
        : "text-green-400";
    const arrow = direction === "out" ? "→" : "←";
    return (
      <li key={key} className="leading-tight">
        <div className="flex items-baseline gap-1.5 text-[11px]">
          <span className={`font-bold ${actionColor}`}>
            {r.action === "DENY" ? "✕" : r.action === "MIXED" ? "⚠" : "✓"}
          </span>
          <span className="font-mono text-slate-500">{arrow}</span>
          <span className="text-slate-200">{humanZoneName(peerZone)}</span>
          <span className="text-slate-500">·</span>
          <span className="text-slate-300">{protoLabel}</span>
        </div>
      </li>
    );
  };

  return (
    <div className="rounded-lg border border-slate-800 bg-slate-900 px-3 py-2.5">
      <div className="mb-2 text-[10px] font-semibold uppercase tracking-wider text-slate-400">
        Communications
      </div>

      {multiHomedZones.length > 0 && (
        <div className="mb-2 rounded border border-amber-700/40 bg-amber-950/20 px-2 py-1.5">
          <div className="text-[9px] font-bold uppercase tracking-wider text-amber-400">
            Multi-homed
          </div>
          <div className="mt-0.5 text-[11px] text-slate-200">
            Also has interface in{" "}
            {multiHomedZones.map((z) => (
              <span
                key={z}
                className="font-mono"
                style={{ color: zoneColors[z]?.text || "#94a3b8" }}
              >
                {z.replace(/_net$/, "")}{" "}
              </span>
            ))}
          </div>
          <div className="mt-1 text-[10px] text-slate-400">
            Cross-zone traffic still routes through the firewall.
          </div>
        </div>
      )}

      {intraFlows.length > 0 && (
        <div className="mb-3">
          <div className="mb-1 flex items-center gap-1.5 text-[9px] font-bold uppercase tracking-wider text-amber-400">
            <span className="h-1.5 w-1.5 rounded-full bg-amber-400" />
            Same zone · firewall-blind
          </div>
          <ul className="space-y-1">
            {intraFlows.map((f, i) => {
              const isSource = f.source === nodeId;
              const peer = isSource ? f.target : f.source;
              return (
                <li key={i} className="text-[11px] leading-tight">
                  <span className="font-mono text-slate-500">
                    {isSource ? "→" : "←"}
                  </span>{" "}
                  <span className="text-slate-200">{peer}</span>{" "}
                  <span className="text-slate-500">· {f.protocol}</span>
                </li>
              );
            })}
          </ul>
          <div className="mt-1 text-[10px] text-slate-500 italic">
            These flows never reach the firewall. Protect with host or
            network controls inside the zone.
          </div>
        </div>
      )}

      {(outbound.length > 0 || inbound.length > 0) && (
        <div>
          <div className="mb-1 flex items-center gap-1.5 text-[9px] font-bold uppercase tracking-wider text-sky-400">
            <span className="h-1.5 w-1.5 rounded-full bg-sky-400" />
            Cross-zone · firewall-governed
          </div>
          {outbound.length > 0 && (
            <div className="mb-1.5">
              <div className="mb-0.5 text-[9px] uppercase tracking-wider text-slate-500">
                Outbound from {nodeZone ? humanZoneName(nodeZone) : "this zone"}
              </div>
              <ul className="space-y-1">
                {outbound.slice(0, 6).map((r, i) => renderRule(r, "out", i))}
              </ul>
            </div>
          )}
          {inbound.length > 0 && (
            <div>
              <div className="mb-0.5 text-[9px] uppercase tracking-wider text-slate-500">
                Inbound to {nodeZone ? humanZoneName(nodeZone) : "this zone"}
              </div>
              <ul className="space-y-1">
                {inbound.slice(0, 6).map((r, i) => renderRule(r, "in", i))}
              </ul>
            </div>
          )}
          <div className="mt-1.5 text-[10px] text-slate-500 italic">
            Policy is enforced per zone, not per host. This list shows
            the rules that govern any flow between {nodeZone ? humanZoneName(nodeZone) : "this zone"} and another zone.
          </div>
        </div>
      )}
    </div>
  );
}
