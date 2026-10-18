"use client";

// The matrix receives the same package flow context used by the canvas.

import { useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Tooltip, TooltipTrigger, TooltipContent } from "./ui/tooltip";
import { Activity, ArrowRight, Download, Router, Square, Zap } from "lucide-react";
import {
  getPcapDownloadUrl,
  getPcapStatus,
  startPcapCapture,
  startTrafficGeneration,
  stopPcapCapture,
  type ZoneRuleSummary,
} from "../lib/api";
import { aggregateFlows, nodeZone, nodeIp, type FlowStatus, type FlowContext } from "../lib/observed-flows";
import { getFlowAuthorization, humanZoneName } from "../lib/network-console-data";
import { FlowEvidence } from "./network-flow-evidence";

// ── Style maps ──────────────────────────────────────────────────────

const ZONE_TEXT: Record<string, string> = {
  enterprise_net: "text-red-400",
  vendor_net: "text-purple-400",
  ot_ops_net: "text-orange-400",
  field_net: "text-green-400",
};

const ZONE_DOT: Record<string, string> = {
  enterprise_net: "bg-red-500",
  vendor_net: "bg-purple-500",
  ot_ops_net: "bg-orange-500",
  field_net: "bg-green-500",
};

const PROTO_BADGE: Record<string, string> = {
  Modbus: "bg-amber-900/40 text-amber-300 border-amber-800/50",
  DNP3: "bg-rose-900/40 text-rose-300 border-rose-800/50",
  HTTP: "bg-blue-900/40 text-blue-300 border-blue-800/50",
  HTTPS: "bg-blue-900/40 text-blue-300 border-blue-800/50",
  FUXA: "bg-indigo-900/40 text-indigo-300 border-indigo-800/50",
  NTP: "bg-teal-900/40 text-teal-300 border-teal-800/50",
};

function protoBadgeClass(p: string): string {
  return PROTO_BADGE[p] || "bg-slate-800/40 text-slate-300 border-slate-700/50";
}

const STATUS_COLOR: Record<FlowStatus, { dot: string; text: string; label: string }> = {
  unknown: { dot: "bg-amber-500", text: "text-amber-400", label: "unknown" },
  active: { dot: "bg-cyan-400", text: "text-cyan-400", label: "active" },
  idle:   { dot: "bg-slate-600", text: "text-slate-500", label: "idle" },
  down:   { dot: "bg-red-500", text: "text-red-400", label: "down" },
};

// ── Component ───────────────────────────────────────────────────────

export function TrafficMatrixView({
  context,
  ruleSummaries,
  canvasEdgesOn,
  onToggleCanvasEdges,
  filter,
  onFilterChange,
  highlightedPair,
  onHighlightPair,
}: {
  context?: FlowContext;
  ruleSummaries?: ZoneRuleSummary[];
  canvasEdgesOn?: boolean;
  onToggleCanvasEdges?: () => void;
  filter?: { zone: string; crossZoneOnly: boolean };
  onFilterChange?: (f: { zone: string; crossZoneOnly: boolean }) => void;
  highlightedPair?: string | null;
  onHighlightPair?: (key: string | null) => void;
} = {}) {
  const queryClient = useQueryClient();
  const [captureDuration, setCaptureDuration] = useState(30);
  const [selectedPair, setSelectedPair] = useState<string | null>(null);

  // Filters can be controlled from the parent (so the canvas mirrors
  // them) or local-only if no parent callbacks are provided.
  const [localZone, setLocalZone] = useState("all");
  const [localCross, setLocalCross] = useState(false);
  const filterZone = filter?.zone ?? localZone;
  const crossZoneOnly = filter?.crossZoneOnly ?? localCross;
  const setFilterZone = (z: string) => {
    setLocalZone(z);
    onFilterChange?.({ zone: z, crossZoneOnly });
  };
  const setCrossZoneOnly = (v: boolean) => {
    setLocalCross(v);
    onFilterChange?.({ zone: filterZone, crossZoneOnly: v });
  };

  const { data: pcapStatus } = useQuery({
    queryKey: ["pcap-status"], queryFn: getPcapStatus, refetchInterval: 2000, enabled: !!context,
  });
  const canGenerate = context?.graph.operations.traffic_generation === true;
  const usPresentation = context?.graph.networks.some((n) => n.id === "ot_ops_net");
  const hasOperator = Object.values(context?.graph.liveness ?? {}).some((b) => b.provider === "operator-link");
  const allRows = useMemo(() => context ? aggregateFlows(context) : [], [context]);
  const rows = useMemo(() => context ? aggregateFlows(context, { zone: filterZone, crossZoneOnly }) : [], [context, filterZone, crossZoneOnly]);
  const [actionError, setActionError] = useState<string | null>(null);

  // Summary chips reflect the unfiltered totals so a student can see
  // what the filter is hiding.
  const summary = useMemo(() => {
    let active = 0, idle = 0, down = 0, unknown = 0, cross = 0;
    for (const r of allRows) {
      if (r.status === "active") active++;
      else if (r.status === "down") down++;
      else if (r.status === "unknown") unknown++;
      else idle++;
      if (r.crossZone) cross++;
    }
    return { total: allRows.length, active, idle, down, unknown, cross };
  }, [allRows]);

  const generating = canGenerate && (context?.traffic?.generating ?? false);
  const capturing = pcapStatus?.capturing ?? false;
  const pcapFiles = pcapStatus?.files ?? [];
  const pcapReady = (pcapStatus?.file_ready ?? false) && !capturing;

  const handleGenerate = async () => {
    try {
      setActionError(null);
      await startTrafficGeneration(captureDuration);
      queryClient.invalidateQueries({ queryKey: ["traffic-status"] });
    } catch (err) {
      setActionError(err instanceof Error ? err.message : "Traffic generation failed");
    }
  };

  // Combined "Generate + Capture" - fires the scenario traffic
  // generator and starts a containd PCAP capture for the selected
  // duration. Both run from the same backend handlers students hit
  // from terminals.
  const handleGenerateAndCapture = async () => {
    try {
      setActionError(null);
      await Promise.all([
        ...(canGenerate ? [startTrafficGeneration(captureDuration)] : []),
        startPcapCapture(captureDuration),
      ]);
      queryClient.invalidateQueries({ queryKey: ["traffic-status"] });
      queryClient.invalidateQueries({ queryKey: ["pcap-status"] });
    } catch (err) {
      setActionError(err instanceof Error ? err.message : "Capture failed");
    }
  };

  const handleStopCapture = async () => {
    try {
      await stopPcapCapture();
      queryClient.invalidateQueries({ queryKey: ["pcap-status"] });
    } catch (err) {
      setActionError(err instanceof Error ? err.message : "Stop capture failed");
    }
  };

  return (
    <div className="space-y-3">
      {/* Header strip - title + canvas-edges toggle + Generate button */}
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="text-[10px] font-bold uppercase tracking-wider text-slate-500">
            Traffic Matrix
          </div>
          <div className="mt-0.5 text-[11px] leading-snug text-slate-400">
            {canGenerate ? "Configured host-to-host flows for this lab. Liveness reflects RTAC device comms and the scenario traffic generator."
              : hasOperator ? "Autonomous baseline and manual exercise traffic. CC link and point quality infer liveness; sessions show transport presence only."
              : "Configured host-to-host flows for this package. Liveness reflects its declared observation providers."}
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-1.5">
          {onToggleCanvasEdges && (
            <Tooltip>
              <TooltipTrigger asChild>
                <button
                  onClick={onToggleCanvasEdges}
                  aria-label="Toggle map traffic lines"
                  className={`inline-flex h-7 w-7 items-center justify-center rounded-md border transition-colors ${
                    canvasEdgesOn
                      ? "border-cyan-700/60 bg-cyan-950/30 text-cyan-400 hover:border-cyan-600 hover:bg-cyan-900/40"
                      : "border-slate-800 bg-slate-900/70 text-slate-500 hover:border-slate-700 hover:text-slate-300"
                  }`}
                >
                  <Activity className="h-3.5 w-3.5" />
                </button>
              </TooltipTrigger>
              <TooltipContent>
                {canvasEdgesOn ? "Hide traffic lines on map" : "Show traffic lines on map"}
              </TooltipContent>
            </Tooltip>
          )}
          {canGenerate && (
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={handleGenerate}
                disabled={generating}
                aria-label="Generate Traffic"
                className="inline-flex h-7 w-7 items-center justify-center rounded-md border border-cyan-700/60 bg-cyan-950/30 text-cyan-400 transition-colors hover:border-cyan-600 hover:bg-cyan-900/40 hover:text-cyan-300 disabled:cursor-not-allowed disabled:opacity-50"
              >
                <Zap className={`h-3.5 w-3.5 ${generating ? "animate-pulse" : ""}`} />
              </button>
            </TooltipTrigger>
            <TooltipContent>
              {generating ? "Generating…" : "Generate Traffic"}
            </TooltipContent>
          </Tooltip>
          )}
        </div>
      </div>

      {hasOperator && <p className="text-[11px] text-slate-400">The process/truth plane is intentionally outside the segmentation map.</p>}
      <p className="text-xs text-slate-300">Topology is declared; liveness is observed, not authorization. Select a flow for its purpose and evidence.</p>

      {/* ── Capture & Generate controls ─────────────────────────── */}
      <div className="space-y-1.5 rounded border border-slate-800 bg-slate-900/40 p-2">
        <div className="text-[9px] font-bold uppercase tracking-wider text-slate-500">
          {canGenerate ? "Generate & Capture" : "Capture PCAP"}
        </div>
        <div className="flex items-center gap-1.5">
          <label className="text-[10px] text-slate-500">Duration:</label>
          <select
            value={captureDuration}
            onChange={(e) => setCaptureDuration(Number(e.target.value))}
            disabled={capturing || generating}
            className="rounded border border-slate-700 bg-slate-900 px-1.5 py-0.5 text-[10px] text-slate-200 disabled:opacity-50"
          >
            <option value={15}>15 sec</option>
            <option value={30}>30 sec</option>
            <option value={60}>60 sec</option>
            <option value={120}>2 min</option>
          </select>
          {!capturing ? (
            <button
              onClick={handleGenerateAndCapture}
              disabled={generating}
              className="ml-auto inline-flex items-center gap-1 rounded border border-cyan-700/60 bg-cyan-950/30 px-2 py-0.5 text-[10px] font-semibold text-cyan-300 transition-colors hover:bg-cyan-900/40 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {canGenerate ? <Zap className="h-3 w-3" fill="currentColor" /> : <Activity className="h-3 w-3" />}
              {canGenerate ? "Generate + PCAP" : "Capture PCAP"}
            </button>
          ) : (
            <button
              onClick={handleStopCapture}
              className="ml-auto inline-flex items-center gap-1 rounded border border-red-700/60 bg-red-950/30 px-2 py-0.5 text-[10px] font-semibold text-red-300 transition-colors hover:bg-red-900/40"
            >
              <Square className="h-3 w-3" fill="currentColor" />
              Stop
            </button>
          )}
        </div>

        {hasOperator && <p className="text-[10px] text-slate-400">Station-clock HTTP is unobserved and outside the IEC104/Modbus firewall capture filter.</p>}

        {/* Live activity strip */}
        {(generating || capturing) && (
          <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[10px]">
            {generating && (
              <span className="flex items-center gap-1 text-cyan-300">
                <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-cyan-400" />
                Generating · {context?.traffic?.flows_generated ?? 0} flows
              </span>
            )}
            {capturing && (
              <span className="flex items-center gap-1 text-red-300">
                <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-red-400" />
                Capturing on containd interfaces
              </span>
            )}
          </div>
        )}

        {/* PCAP downloads */}
        {pcapReady && pcapFiles.length > 0 && (
          <div className="space-y-1">
            <div className="text-[9px] uppercase tracking-wider text-slate-500">
              {pcapFiles.length} PCAP file{pcapFiles.length === 1 ? "" : "s"} ready
            </div>
            <div className="flex flex-wrap gap-1">
              {pcapFiles.map((filename) => (
                <a
                  key={filename}
                  href={getPcapDownloadUrl(filename)}
                  download
                  className="inline-flex items-center gap-1 rounded border border-green-700/60 bg-green-950/30 px-1.5 py-0.5 text-[9px] font-semibold text-green-300 transition-colors hover:bg-green-900/40"
                >
                  <Download className="h-2.5 w-2.5" />
                  {filename}
                </a>
              ))}
            </div>
          </div>
        )}
        {pcapReady && pcapFiles.length === 0 && (
          <a
            href={getPcapDownloadUrl()}
            download
            className="inline-flex items-center gap-1 rounded border border-green-700/60 bg-green-950/30 px-1.5 py-0.5 text-[9px] font-semibold text-green-300 transition-colors hover:bg-green-900/40"
          >
            <Download className="h-2.5 w-2.5" />
            Download PCAP
          </a>
        )}
      </div>

      {actionError && <p role="alert" className="text-xs text-red-300">{actionError}</p>}

      {/* ── Filters ─────────────────────────────────────────────── */}
      <div className="flex items-center gap-1.5">
        <select
          value={filterZone}
          onChange={(e) => setFilterZone(e.target.value)}
          className="rounded border border-slate-700 bg-slate-900 px-1.5 py-0.5 text-[10px] text-slate-200"
        >
          <option value="all">All zones</option>
          {context?.graph.networks.map((n) => <option key={n.id} value={n.id}>{usPresentation ? (n.id === "field_net" ? "Field" : humanZoneName(n.id)) : n.name}</option>)}
        </select>
        <button
          onClick={() => setCrossZoneOnly(!crossZoneOnly)}
          className={`rounded border px-1.5 py-0.5 text-[10px] font-semibold transition-colors ${
            crossZoneOnly
              ? "border-amber-700 bg-amber-950/30 text-amber-300"
              : "border-slate-700 bg-slate-900/40 text-slate-400 hover:text-slate-200"
          }`}
        >
          Cross-zone only
        </button>
        {(filterZone !== "all" || crossZoneOnly) && (
          <span className="ml-auto text-[10px] text-slate-500">
            {rows.length} of {allRows.length}
          </span>
        )}
      </div>

      {/* Summary chips */}
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[10px]">
        <span className="text-slate-500">Pairs:</span>
        <span className="font-bold text-slate-300">{summary.total}</span>
        <span className="text-slate-700">·</span>
        <span className="text-slate-500">Active:</span>
        <span className="font-bold text-cyan-400">{summary.active}</span>
        <span className="text-slate-700">·</span>
        <span className="text-slate-500">Idle:</span>
        <span className="font-bold text-slate-400">{summary.idle}</span>
        {summary.down > 0 && (
          <>
            <span className="text-slate-700">·</span>
            <span className="text-slate-500">Down:</span>
            <span className="font-bold text-red-400">{summary.down}</span>
          </>
        )}
        <span className="text-slate-700">·</span>
        {summary.unknown > 0 && <span className="text-amber-400">Unknown: {summary.unknown}</span>}
        <span className="text-slate-500">Cross-zone:</span>
        <span className="font-bold text-amber-400">{summary.cross}</span>
      </div>

      {/* Flow rows. One row per source-target pair with all protocols
          collapsed into a single row. */}
      <div className="max-h-[60vh] space-y-1.5 overflow-y-auto">
        {rows.map((r) => {
          const statusStyle = STATUS_COLOR[r.status];
          const sZone = nodeZone(r.source, context!.graph);
          const tZone = nodeZone(r.target, context!.graph);
          const pairKey = `${r.source}->${r.target}`;
          const isHighlighted = highlightedPair === pairKey;
          const isSelected = selectedPair === pairKey;
          const toggleSelection = () => {
            setSelectedPair(isSelected ? null : pairKey);
            onHighlightPair?.(isSelected ? null : pairKey);
          };
          const authorization = getFlowAuthorization(r.source, r.target, ruleSummaries, context!.graph);
          return (
            <div
              key={pairKey}
              className={`rounded border p-2 text-[10px] transition-colors ${
                isHighlighted || isSelected
                  ? "border-sky-500 bg-sky-950/30 ring-1 ring-sky-500/40"
                  : r.crossZone
                  ? "border-amber-900/40 bg-amber-950/10 hover:border-amber-700/60"
                  : "border-slate-800 bg-slate-900/40 hover:border-slate-700"
              }`}
              onMouseEnter={() => onHighlightPair?.(pairKey)}
              onMouseLeave={() => onHighlightPair?.(selectedPair)}
            >
              <button
                type="button"
                className="w-full rounded text-left focus-visible:outline focus-visible:outline-2 focus-visible:outline-sky-400"
                aria-label={`Inspect flow ${r.source} to ${r.target}`}
                aria-expanded={isSelected}
                onClick={toggleSelection}
              >
              <div className="flex items-center justify-between gap-2">
                <div className="flex min-w-0 items-center gap-1.5">
                  <span className={`h-1.5 w-1.5 rounded-full ${ZONE_DOT[sZone] || "bg-slate-600"}`} />
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span
                        className={`cursor-help font-medium ${ZONE_TEXT[sZone] || "text-slate-300"}`}
                      >
                        {r.source}
                      </span>
                    </TooltipTrigger>
                    <TooltipContent>
                      {nodeIp(r.source, context!.graph) || "unknown IP"}
                    </TooltipContent>
                  </Tooltip>
                  <ArrowRight className="h-3 w-3 shrink-0 text-slate-200" strokeWidth={2.5} />
                  <span className={`h-1.5 w-1.5 rounded-full ${ZONE_DOT[tZone] || "bg-slate-600"}`} />
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span
                        className={`cursor-help font-medium ${ZONE_TEXT[tZone] || "text-slate-300"}`}
                      >
                        {r.target}
                      </span>
                    </TooltipTrigger>
                    <TooltipContent>
                      {nodeIp(r.target, context!.graph) || "unknown IP"}
                    </TooltipContent>
                  </Tooltip>
                </div>
                <div className="flex shrink-0 items-center gap-1.5">
                  {r.crossZone && (
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <span className="inline-flex items-center gap-0.5 rounded border border-amber-700/60 bg-amber-950/30 px-1 py-0 text-[9px] font-bold uppercase text-amber-300">
                          <Router className="h-2.5 w-2.5" />
                          L3
                        </span>
                      </TooltipTrigger>
                      <TooltipContent>Routable</TooltipContent>
                    </Tooltip>
                  )}
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span className="flex cursor-help items-center gap-1">
                        <span
                          className={`h-1.5 w-1.5 rounded-full ${statusStyle.dot} ${
                            r.status === "active" ? "animate-pulse" : ""
                          }`}
                        />
                        <span className={`text-[9px] font-bold uppercase ${statusStyle.text}`}>
                          {statusStyle.label}
                        </span>
                      </span>
                    </TooltipTrigger>
                    <TooltipContent>
                      {r.protocols.map((f) => context?.observations[f.id]).map((o, i) => (
                        <div key={i}>{o?.evidence}: {o?.reason}</div>
                      ))}
                    </TooltipContent>
                  </Tooltip>
                </div>
              </div>
              <div className="mt-1 flex flex-wrap items-center gap-1">
                {r.protocols.map((f, i) => (
                  <span
                    key={i}
                    className={`rounded border px-1 py-0 text-[9px] ${protoBadgeClass(f.protocol)}`}
                  >
                    {f.protocol} :{f.port}
                  </span>
                ))}
                <span className="ml-auto text-[9px] text-slate-500">
                  {r.protocols[0].cadence} · {r.category}
                </span>
              </div>
              <p className="mt-1 text-xs text-slate-300">Topology: declared · Observed liveness: {r.status} · {authorization.state === "denied" ? "Authorization: denied by zone-pair summary" : authorization.state === "same-zone" ? "Authorization: outside firewall scope (same zone)" : "Authorization unknown"}</p>
              </button>
              {isSelected && <FlowEvidence context={context!} pair={r} ruleSummaries={ruleSummaries} />}
            </div>
          );
        })}
      </div>
    </div>
  );
}
