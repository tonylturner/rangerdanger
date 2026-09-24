"use client";

import type { Dispatch, MouseEvent, SetStateAction } from "react";
import { type AuditEntry, type Scenario } from "../lib/api";
import { NODE_LABELS, NODE_UI_URLS } from "../lib/exercise-nodes";
import { SharedTerminalPanel } from "./terminal-context";
import { actionLabel } from "../lib/scenario-runner-logic";

export function StepNavigator({
  scenario,
  completedSteps,
  currentStep,
  setCurrentStep,
  setStepResult,
}: {
  scenario: Scenario;
  completedSteps: Set<number>;
  currentStep: number;
  setCurrentStep: Dispatch<SetStateAction<number>>;
  setStepResult: Dispatch<SetStateAction<import("../lib/api").StepExecutionResult | null>>;
}) {
  return (
    <>
  {/* Left: Step navigator */}
  <div className="space-y-1">
    {scenario.steps.map((s, i) => {
      const done = completedSteps.has(i);
      const active = i === currentStep;
      return (
        <button
          key={i}
          onClick={() => { setCurrentStep(i); setStepResult(null); }}
          className={`w-full rounded-lg border px-3 py-2 text-left text-xs transition-colors ${
            active
              ? "border-sky-700 bg-sky-950/40 text-sky-300"
              : done
              ? "border-green-800 bg-green-950/20 text-green-400"
              : "border-slate-800 bg-slate-900/50 text-slate-400 hover:bg-slate-900"
          }`}
        >
          <div className="flex items-center gap-2">
            <span className={`flex h-5 w-5 shrink-0 items-center justify-center rounded-full text-[10px] font-bold ${
              done ? "bg-green-900 text-green-400" : active ? "bg-sky-900 text-sky-400" : "bg-slate-800 text-slate-500"
            }`}>
              {done ? "\u2713" : i + 1}
            </span>
            <span className="truncate font-medium">{s.title}</span>
            {s.action && (
              <span className="ml-auto text-[8px] text-slate-600 uppercase">{actionLabel(s.action.type)}</span>
            )}
          </div>
        </button>
      );
    })}
  </div>
    </>
  );
}

export function CommandAuditPanels({
  cmdLog,
  setCmdLog,
  recentAudit,
}: {
  cmdLog: string[];
  setCmdLog: Dispatch<SetStateAction<string[]>>;
  recentAudit: AuditEntry[];
}) {
  return (
    <>
    {/* Command log - persistent, scrollable */}
    {cmdLog.length > 0 && (
      <div className="rounded-xl border border-slate-800 bg-slate-950 p-3 max-h-64 overflow-y-auto">
        <div className="flex items-center justify-between mb-1">
          <div className="text-[10px] font-bold uppercase tracking-wider text-slate-500">
            Command Log ({cmdLog.length} entries)
          </div>
          <button
            onClick={() => setCmdLog([])}
            className="text-[9px] text-slate-600 hover:text-slate-400"
          >
            clear
          </button>
        </div>
        {cmdLog.map((msg, i) => (
          <div key={i} className={`font-mono text-[11px] ${
            msg.startsWith("[SUCCEEDED]") || msg.startsWith("[OK]")
              ? "text-green-400"
              : msg.startsWith("[BLOCKED]") || msg.startsWith("[RESET]")
              ? "text-amber-400"
              : msg.startsWith("[ERROR]") || msg.startsWith("[EXIT")
              ? "text-red-400"
              : msg.startsWith("[RUN")
              ? "text-sky-400"
              : msg.startsWith("  ")
              ? "text-slate-500"
              : "text-slate-400"
          }`}>
            {msg}
          </div>
        ))}
      </div>
    )}

    {/* Live audit trail */}
    {recentAudit.length > 0 && (
      <div className="rounded-xl border border-slate-800 bg-slate-900/70 p-3">
        <div className="text-[10px] font-bold uppercase tracking-wider text-slate-500 mb-1">
          Recent Activity (via containd)
        </div>
        <div className="space-y-1">
          {recentAudit.map((e, i) => {
            const zone = e.source_zone || "unknown";
            const succeeded = e.result === "executed";
            const harmful = e.process_impact?.includes("de-energized") || e.process_impact?.includes("DISABLED") || e.process_impact?.includes("OPENED");
            return (
              <div key={i} className="flex items-center gap-2 text-[11px]">
                <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${
                  zone === "enterprise" ? "bg-red-500"
                  : zone === "vendor" ? "bg-purple-500"
                  : zone === "ot_ops" ? "bg-orange-500"
                  : zone === "operator" ? "bg-sky-500"
                  : "bg-slate-500"
                }`} />
                <span className="text-slate-500">{zone}</span>
                <span className="text-amber-400">{e.command}</span>
                <span className="text-slate-600">→</span>
                <span className="text-slate-300">{e.target}</span>
                <span className={`ml-auto font-bold text-[10px] ${
                  succeeded && harmful ? "text-red-400" : succeeded ? "text-green-400" : "text-yellow-400"
                }`}>
                  {succeeded ? (harmful ? "ATTACK" : "OK") : "BLOCKED"}
                </span>
              </div>
            );
          })}
        </div>
      </div>
    )}

    </>
  );
}

export function TerminalPanel({
  exerciseNodes,
  activeTerminalNode,
  setActiveTerminalNode,
  panelMode,
  setPanelMode,
  panelHeight,
  setPanelHeight,
  onResizeStart,
}: {
  exerciseNodes: string[];
  activeTerminalNode: string;
  setActiveTerminalNode: Dispatch<SetStateAction<string>>;
  panelMode: "terminal" | "ui";
  setPanelMode: Dispatch<SetStateAction<"terminal" | "ui">>;
  panelHeight: number;
  setPanelHeight: Dispatch<SetStateAction<number>>;
  onResizeStart: (event: MouseEvent) => void;
}) {
  return (
  <div className="rounded-xl border border-slate-800 bg-slate-900/70 overflow-hidden">
    {/* Resize handle */}
    <div
      onMouseDown={onResizeStart}
      className="h-1.5 cursor-ns-resize flex items-center justify-center hover:bg-slate-700/50 transition-colors group"
    >
      <div className="w-8 h-0.5 rounded-full bg-slate-700 group-hover:bg-slate-500" />
    </div>
    {/* Node switcher + Terminal/UI toggle */}
    <div className="flex items-center border-b border-slate-800 px-2 py-1 gap-1">
      {exerciseNodes.map((nodeId) => (
        <button
          key={nodeId}
          onClick={() => { setActiveTerminalNode(nodeId); setPanelMode("terminal"); }}
          className={`rounded px-2 py-1 text-[10px] font-medium transition-colors ${
            activeTerminalNode === nodeId
              ? "bg-cyan-950/60 text-cyan-400 border border-cyan-800/50"
              : "text-slate-500 hover:text-slate-300 border border-transparent"
          }`}
        >
          {NODE_LABELS[nodeId] || nodeId}
        </button>
      ))}
      <div className="ml-auto flex items-center gap-1">
        <button
          onClick={() => { setPanelMode("terminal"); if (panelHeight > 400) setPanelHeight(300); }}
          className={`rounded px-2 py-1 text-[9px] font-medium ${
            panelMode === "terminal" ? "bg-slate-800 text-slate-200" : "text-slate-500 hover:text-slate-300"
          }`}
        >
          Terminal
        </button>
        {NODE_UI_URLS[activeTerminalNode] && (
          <button
            onClick={() => { setPanelMode("ui"); if (panelHeight < 400) setPanelHeight(500); }}
            className={`rounded px-2 py-1 text-[9px] font-medium ${
              panelMode === "ui" ? "bg-slate-800 text-slate-200" : "text-slate-500 hover:text-slate-300"
            }`}
          >
            UI
          </button>
        )}
      </div>
    </div>

    {/* Content */}
    <div className="relative" style={{ height: panelHeight }}>
      {panelMode === "terminal" && (
        <SharedTerminalPanel
          nodes={exerciseNodes}
          activeNode={activeTerminalNode}
          height={panelHeight}
        />
      )}
      {panelMode === "ui" && NODE_UI_URLS[activeTerminalNode] && (
        <iframe
          title={`${NODE_LABELS[activeTerminalNode] || activeTerminalNode} UI`}
          src={NODE_UI_URLS[activeTerminalNode]}
          className="h-full w-full border-0"
        />
      )}
    </div>
  </div>

  );
}
