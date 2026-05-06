"use client";

// Traffic Matrix view rendered inside the Network Map's left drawer.
//
// Shares its data source with the canvas Traffic view: both consume
// the same OBSERVED_FLOWS table and the same resolveLiveness() helper
// so the matrix and the map are guaranteed to be consistent. The
// matrix is "what flows the lab is configured to produce" with each
// row stamped active/idle/down by live runtime telemetry from
// /workshop/status (RTAC online + device_comms) and /traffic/status
// (scenario generator state).
//
// The Generate Traffic button calls the same /api/traffic/generate
// backend endpoint that students invoke from the lab terminals — the
// backend handler `docker exec`s real curl commands inside eng-ws and
// vendor-jump, so flows produced here are identical to flows produced
// by hand. Nothing in this drawer reaches around containd or invents
// its own protocol path.

import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Tooltip, TooltipTrigger, TooltipContent } from "./ui/tooltip";
import { Activity, ArrowRight, Download, Router, Square, Zap } from "lucide-react";
import {
  getPcapDownloadUrl,
  getPcapStatus,
  getTrafficStatus,
  getWorkshopStatus,
  startPcapCapture,
  startTrafficGeneration,
  stopPcapCapture,
} from "../lib/api";
import {
  OBSERVED_FLOWS,
  resolveLiveness,
  isCrossZone,
  nodeZone,
  nodeIp,
  type FlowStatus,
  type ObservedFlow,
} from "../lib/observed-flows";

// ── Style maps ──────────────────────────────────────────────────────

const ZONE_TEXT: Record<string, string> = {
  enterprise: "text-red-400",
  vendor: "text-purple-400",
  ot_ops: "text-orange-400",
  field: "text-green-400",
};

const ZONE_DOT: Record<string, string> = {
  enterprise: "bg-red-500",
  vendor: "bg-purple-500",
  ot_ops: "bg-orange-500",
  field: "bg-green-500",
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

// Format an ISO timestamp as a compact, defensible UTC string. The
// backend/ref is ISO so we can still feed it to tools later; this is
// purely a display helper. We also prepend a short relative marker
// when the stamp is recent so the student can gauge freshness at a
// glance without doing arithmetic on the wall-clock time.
function formatLastSeen(iso: string | undefined): string | null {
  if (!iso) return null;
  const ms = new Date(iso).getTime();
  if (!Number.isFinite(ms)) return iso;
  const pad = (n: number) => n.toString().padStart(2, "0");
  const d = new Date(ms);
  const abs = `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())} ${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}:${pad(d.getUTCSeconds())} UTC`;

  const diff = Math.max(0, Date.now() - ms);
  if (diff < 300_000) {
    const rel =
      diff < 1500 ? "now"
      : diff < 60_000 ? `${Math.round(diff / 1000)}s ago`
      : `${Math.round(diff / 60_000)}m ago`;
    return `${rel} · ${abs}`;
  }
  return abs;
}

const STATUS_COLOR: Record<FlowStatus, { dot: string; text: string; label: string }> = {
  active: { dot: "bg-cyan-400", text: "text-cyan-400", label: "active" },
  idle:   { dot: "bg-slate-600", text: "text-slate-500", label: "idle" },
  down:   { dot: "bg-red-500", text: "text-red-400", label: "down" },
};

// ── Aggregation ─────────────────────────────────────────────────────

type Row = {
  source: string;
  target: string;
  protocols: ObservedFlow[];
  status: FlowStatus;
  category: ObservedFlow["category"];
  crossZone: boolean;
};

function aggregate(
  flows: ObservedFlow[],
  deviceComms: Record<string, boolean> | undefined,
  rtacOnline: boolean,
  trafficStatus: ReturnType<typeof getTrafficStatus> extends Promise<infer T> ? T | undefined : undefined,
): Row[] {
  const groups = new Map<string, Row>();
  for (const f of flows) {
    const key = `${f.source}->${f.target}`;
    if (!groups.has(key)) {
      groups.set(key, {
        source: f.source,
        target: f.target,
        protocols: [],
        status: "idle",
        category: f.category,
        crossZone: isCrossZone(f.source, f.target),
      });
    }
    groups.get(key)!.protocols.push(f);
  }
  // Compute aggregated status per pair: active if any constituent is
  // active; down if all are down; otherwise idle.
  for (const row of groups.values()) {
    const statuses = row.protocols.map((f) =>
      resolveLiveness(f, deviceComms, rtacOnline, trafficStatus),
    );
    row.status = statuses.includes("active")
      ? "active"
      : statuses.every((s) => s === "down")
      ? "down"
      : "idle";
  }
  // Sort: active first, then idle, then down; cross-zone above
  // intra-zone within each tier so the security-relevant rows float up.
  const order: Record<FlowStatus, number> = { active: 0, idle: 1, down: 2 };
  return Array.from(groups.values()).sort((a, b) => {
    if (order[a.status] !== order[b.status]) return order[a.status] - order[b.status];
    if (a.crossZone !== b.crossZone) return a.crossZone ? -1 : 1;
    return a.source.localeCompare(b.source);
  });
}

// ── Component ───────────────────────────────────────────────────────

export function TrafficMatrixView({
  canvasEdgesOn,
  onToggleCanvasEdges,
  filter,
  onFilterChange,
  highlightedPair,
  onHighlightPair,
}: {
  canvasEdgesOn?: boolean;
  onToggleCanvasEdges?: () => void;
  filter?: { zone: string; crossZoneOnly: boolean };
  onFilterChange?: (f: { zone: string; crossZoneOnly: boolean }) => void;
  highlightedPair?: string | null;
  onHighlightPair?: (key: string | null) => void;
} = {}) {
  const queryClient = useQueryClient();
  const [captureDuration, setCaptureDuration] = useState(30);

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

  // Same React Query keys as the rest of the app so we share cache
  // and so invalidation from this drawer (after Generate Traffic)
  // refreshes the canvas Traffic edges in the same render cycle.
  const { data: workshopStatus } = useQuery({
    queryKey: ["workshop", "status"],
    queryFn: getWorkshopStatus,
    refetchInterval: 5000,
  });
  const { data: trafficStatus } = useQuery({
    queryKey: ["traffic-status"],
    queryFn: getTrafficStatus,
    refetchInterval: 3000,
  });
  const { data: pcapStatus } = useQuery({
    queryKey: ["pcap-status"],
    queryFn: getPcapStatus,
    refetchInterval: 2000,
  });

  const allRows = useMemo(
    () => aggregate(OBSERVED_FLOWS, workshopStatus?.device_comms, workshopStatus?.rtac_online ?? false, trafficStatus),
    [workshopStatus, trafficStatus],
  );

  // Apply filter UI to the aggregated rows. The summary stats below
  // still reflect the unfiltered totals so the user can see what's
  // being hidden.
  const rows = useMemo(() => {
    return allRows.filter((r) => {
      if (crossZoneOnly && !r.crossZone) return false;
      if (filterZone !== "all") {
        const sZone = nodeZone(r.source);
        const tZone = nodeZone(r.target);
        if (sZone !== filterZone && tZone !== filterZone) return false;
      }
      return true;
    });
  }, [allRows, crossZoneOnly, filterZone]);

  // Track per-pair last-seen-active timestamps. We use a ref so the
  // map persists across renders without itself triggering renders;
  // each poll cycle (driven by the React Query refetch interval)
  // updates entries for any pair that's currently active. Tooltips
  // read the ref at hover time and get the latest stamp.
  const lastSeenRef = useRef<Map<string, string>>(new Map());
  useEffect(() => {
    const now = new Date().toISOString();
    for (const r of rows) {
      if (r.status === "active") {
        lastSeenRef.current.set(`${r.source}->${r.target}`, now);
      }
    }
  }, [rows]);

  // Summary chips reflect the unfiltered totals so a student can see
  // what the filter is hiding.
  const summary = useMemo(() => {
    let active = 0, idle = 0, down = 0, cross = 0;
    for (const r of allRows) {
      if (r.status === "active") active++;
      else if (r.status === "down") down++;
      else idle++;
      if (r.crossZone) cross++;
    }
    return { total: allRows.length, active, idle, down, cross };
  }, [allRows]);

  const generating = trafficStatus?.generating ?? false;
  const capturing = pcapStatus?.capturing ?? false;
  const pcapFiles = pcapStatus?.files ?? [];
  const pcapReady = (pcapStatus?.file_ready ?? false) && !capturing;

  const handleGenerate = async () => {
    try {
      await startTrafficGeneration(captureDuration);
      queryClient.invalidateQueries({ queryKey: ["traffic-status"] });
    } catch (err) {
      console.error("startTrafficGeneration failed:", err);
    }
  };

  // Combined "Generate + Capture" — fires the scenario traffic
  // generator and starts a containd PCAP capture for the selected
  // duration. Both run from the same backend handlers students hit
  // from terminals.
  const handleGenerateAndCapture = async () => {
    try {
      await Promise.all([
        startTrafficGeneration(captureDuration),
        startPcapCapture(captureDuration),
      ]);
      queryClient.invalidateQueries({ queryKey: ["traffic-status"] });
      queryClient.invalidateQueries({ queryKey: ["pcap-status"] });
    } catch (err) {
      console.error("Generate + capture failed:", err);
    }
  };

  const handleStopCapture = async () => {
    try {
      await stopPcapCapture();
      queryClient.invalidateQueries({ queryKey: ["pcap-status"] });
    } catch (err) {
      console.error("stopPcapCapture failed:", err);
    }
  };

  return (
    <div className="space-y-3">
      {/* Header strip — title + canvas-edges toggle + Generate button */}
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="text-[10px] font-bold uppercase tracking-wider text-slate-500">
            Traffic Matrix
          </div>
          <div className="mt-0.5 text-[11px] leading-snug text-slate-400">
            Configured host-to-host flows for this lab. Liveness reflects RTAC device comms and the scenario traffic generator.
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
        </div>
      </div>

      {/* ── Capture & Generate controls ─────────────────────────── */}
      <div className="space-y-1.5 rounded border border-slate-800 bg-slate-900/40 p-2">
        <div className="text-[9px] font-bold uppercase tracking-wider text-slate-500">
          Generate & Capture
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
              <Zap className="h-3 w-3" fill="currentColor" />
              Generate + PCAP
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

        {/* Live activity strip */}
        {(generating || capturing) && (
          <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[10px]">
            {generating && (
              <span className="flex items-center gap-1 text-cyan-300">
                <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-cyan-400" />
                Generating · {trafficStatus?.flows_generated ?? 0} flows
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

      {/* ── Filters ─────────────────────────────────────────────── */}
      <div className="flex items-center gap-1.5">
        <select
          value={filterZone}
          onChange={(e) => setFilterZone(e.target.value)}
          className="rounded border border-slate-700 bg-slate-900 px-1.5 py-0.5 text-[10px] text-slate-200"
        >
          <option value="all">All zones</option>
          <option value="enterprise">Enterprise</option>
          <option value="vendor">Vendor / DMZ</option>
          <option value="ot_ops">OT Ops</option>
          <option value="field">Field</option>
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
        <span className="text-slate-500">Cross-zone:</span>
        <span className="font-bold text-amber-400">{summary.cross}</span>
      </div>

      {/* Flow rows. One row per source-target pair with all protocols
          collapsed into a single row. */}
      <div className="max-h-[60vh] space-y-1.5 overflow-y-auto">
        {rows.map((r) => {
          const statusStyle = STATUS_COLOR[r.status];
          const sZone = nodeZone(r.source);
          const tZone = nodeZone(r.target);
          const pairKey = `${r.source}->${r.target}`;
          const isHighlighted = highlightedPair === pairKey;
          return (
            <div
              key={pairKey}
              className={`cursor-pointer rounded border p-2 text-[10px] transition-colors ${
                isHighlighted
                  ? "border-sky-500 bg-sky-950/30 ring-1 ring-sky-500/40"
                  : r.crossZone
                  ? "border-amber-900/40 bg-amber-950/10 hover:border-amber-700/60"
                  : "border-slate-800 bg-slate-900/40 hover:border-slate-700"
              }`}
              onMouseEnter={() => onHighlightPair?.(pairKey)}
              onMouseLeave={() => onHighlightPair?.(null)}
              onClick={() =>
                onHighlightPair?.(isHighlighted ? null : pairKey)
              }
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
                      {nodeIp(r.source) || "unknown IP"}
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
                      {nodeIp(r.target) || "unknown IP"}
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
                      {(() => {
                        const seen = formatLastSeen(
                          lastSeenRef.current.get(`${r.source}->${r.target}`),
                        );
                        if (r.status === "active") {
                          return seen ? `Last seen: ${seen}` : "Last seen: now";
                        }
                        if (r.status === "down") {
                          return seen ? `Down · last seen ${seen}` : "Down · never seen";
                        }
                        return seen ? `Idle · last seen ${seen}` : "Idle · never seen";
                      })()}
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
            </div>
          );
        })}
      </div>
    </div>
  );
}
