"use client";

import { useEffect, useRef, useState } from "react";
import type { AuditEntry, NetworkEvent } from "../lib/api";

export function CommandAuditView({ entries, networkEvents }: { entries: AuditEntry[]; networkEvents: NetworkEvent[] }) {
  const [showDPI, setShowDPI] = useState(networkEvents.length > 0);
  const dpiSeenRef = useRef(false);
  useEffect(() => {
    if (networkEvents.length > 0 && !dpiSeenRef.current) {
      dpiSeenRef.current = true;
      setShowDPI(true);
    }
  }, [networkEvents.length]);

  if (entries.length === 0 && networkEvents.length === 0) {
    return <div className="py-8 text-center text-sm text-slate-500">No commands recorded yet. Run an exercise to see the audit trail.</div>;
  }

  // Zone labels that make sense for substation context
  const zoneLabel: Record<string, string> = {
    enterprise: "Enterprise",
    vendor: "Vendor",
    ot_ops: "OT Ops",
    field: "Field",
    operator: "Operator",
    "lab-control": "Lab Control",
    auto: "Auto",
  };

  const zoneBorder: Record<string, string> = {
    enterprise: "border-l-red-500",
    vendor: "border-l-purple-500",
    ot_ops: "border-l-orange-500",
    field: "border-l-green-500",
    operator: "border-l-sky-500",
    "lab-control": "border-l-[#c6f24e]",
    auto: "border-l-amber-500",
  };

  return (
    <div className="space-y-3">
      {/* Controls */}
      <div className="flex items-center justify-between">
        <span className="text-[10px] text-slate-500">
          Who sent commands, from which zone, and what happened to the feeder.
        </span>
        {networkEvents.length > 0 && (
          <button
            onClick={() => setShowDPI(!showDPI)}
            className={`rounded border px-2 py-1 text-[10px] font-medium ${
              showDPI ? "border-purple-700 bg-purple-950/30 text-purple-400" : "border-slate-700 bg-slate-800/40 text-slate-500"
            }`}
          >
            {showDPI ? "Hide" : "Show"} DPI ({networkEvents.length})
          </button>
        )}
      </div>

      <div className="max-h-[500px] overflow-y-auto space-y-1">
        {entries.map((e, i) => {
              const zone = e.source_zone || "unknown";
              const wasAttack = zone === "enterprise" || zone === "vendor";
              const succeeded = e.result === "executed";
              const harmful = e.process_impact?.includes("de-energized") || e.process_impact?.includes("DISABLED") || e.process_impact?.includes("OPENED") || e.process_impact?.includes("LOCKED");

              return (
                <div key={`a-${e.timestamp}-${e.command}-${i}`} className={`rounded border-l-2 border border-slate-800/60 bg-slate-900/40 p-2 text-xs ${zoneBorder[zone] || "border-l-slate-500"}`}>
                  <div className="flex items-center gap-2">
                    <span className="text-slate-600 w-14 shrink-0 text-[10px]">
                      {e.timestamp ? new Date(e.timestamp).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" }) : "--"}
                    </span>
                    <span className="text-[10px] font-bold text-slate-400">
                      {zoneLabel[zone] || zone}
                    </span>
                    <span className="text-slate-600">sent</span>
                    <span className="text-amber-400 font-medium">{e.command}</span>
                    <span className="text-slate-600">to</span>
                    <span className="text-slate-300 font-medium">{e.target}</span>
                    <span className="ml-auto">
                      {succeeded ? (
                        harmful ? (
                          <span className="text-red-400 font-bold">SUCCEEDED</span>
                        ) : (
                          <span className="text-green-400 font-bold">OK</span>
                        )
                      ) : (
                        <span className="text-yellow-400 font-bold">BLOCKED</span>
                      )}
                    </span>
                  </div>
                  {/* Operational consequence - always prominent */}
                  {e.process_impact && e.process_impact !== "command executed" && (
                    <div className={`mt-1 ml-14 rounded px-2 py-1 text-[11px] font-medium ${
                      succeeded && harmful
                        ? "bg-red-950/30 text-red-300 border border-red-900/40"
                        : succeeded
                        ? "bg-green-950/20 text-green-300 border border-green-900/30"
                        : "bg-slate-800/40 text-slate-400"
                    }`}>
                      {wasAttack && succeeded && harmful ? "Attack impact: " : ""}
                      {e.process_impact}
                    </div>
                  )}
                </div>
              );
            })}

        {showDPI && networkEvents.map((e, i) => (
          <div key={`n-${e.id ?? i}`} className="rounded border border-purple-900/30 bg-purple-950/10 p-2 text-xs">
            <div className="flex items-center gap-2">
              <span className="text-slate-600 w-14 shrink-0 text-[10px]">
                {e.timestamp ? new Date(e.timestamp).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" }) : "--"}
              </span>
              <span className="text-[10px] font-bold text-purple-400">DPI</span>
              <span className="text-slate-500">{e.source} → {e.dest}</span>
              {e.protocol && e.protocol !== "-" && (
                <span className="text-purple-400 text-[10px]">[{e.protocol}]</span>
              )}
            </div>
            {e.details && <div className="mt-0.5 ml-14 text-slate-500 text-[10px]">{e.details}</div>}
          </div>
        ))}
      </div>
    </div>
  );
}
