"use client";

import { useState, useEffect, useCallback } from "react";
import {
  validateScenario,
  getSubstationState,
  getActiveFirewallConfig,
  applyFirewallConfig,
  sendSubstationCommand,
  executeScenarioStep,
  getSubstationAudit,
  type Scenario,
  type ValidationResult,
  type SubstationState,
  type StepExecutionResult,
  type AuditEntry,
} from "../lib/api";

type RunnerProps = {
  scenario: Scenario;
  onExit: () => void;
};

export function ScenarioRunner({ scenario, onExit }: RunnerProps) {
  const [currentStep, setCurrentStep] = useState(0);
  const [completedSteps, setCompletedSteps] = useState<Set<number>>(new Set());
  const [validation, setValidation] = useState<ValidationResult | null>(null);
  const [validating, setValidating] = useState(false);
  const [state, setState] = useState<SubstationState | null>(null);
  const [activeConfig, setActiveConfig] = useState<string | null>(null);
  const [cmdLog, setCmdLog] = useState<string[]>([]);
  const [executing, setExecuting] = useState(false);
  const [stepResult, setStepResult] = useState<StepExecutionResult | null>(null);
  const [recentAudit, setRecentAudit] = useState<AuditEntry[]>([]);

  const pollState = useCallback(async () => {
    try {
      const [s, fw, a] = await Promise.all([
        getSubstationState(),
        getActiveFirewallConfig(),
        getSubstationAudit(),
      ]);
      setState(s);
      setActiveConfig(fw.active_config);
      setRecentAudit((a.entries ?? []).slice(-5));
    } catch {
      // offline
    }
  }, []);

  useEffect(() => {
    pollState();
    const id = setInterval(pollState, 3000);
    return () => clearInterval(id);
  }, [pollState]);

  const handleValidate = async () => {
    setValidating(true);
    try {
      const result = await validateScenario(scenario.id);
      setValidation(result);
      if (result.outcome === "PASS") {
        setCompletedSteps(new Set(scenario.steps.map((_, i) => i)));
      }
    } catch {
      setValidation(null);
    } finally {
      setValidating(false);
    }
  };

  const handleExecuteStep = async (idx: number) => {
    setExecuting(true);
    setStepResult(null);
    try {
      const result = await executeScenarioStep(scenario.id, idx);
      setStepResult(result);

      // Log results
      for (const r of result.results) {
        const label = r.success ? "SUCCEEDED" : "BLOCKED";
        const msg = `[${label}] ${r.action}: ${r.impact || r.detail}`;
        setCmdLog((prev) => [msg, ...prev].slice(0, 20));
      }

      if (result.success) {
        markStepDone(idx);
      }

      setTimeout(pollState, 500);
    } catch (e) {
      setCmdLog((prev) => [`[ERROR] ${e}`, ...prev].slice(0, 20));
    } finally {
      setExecuting(false);
    }
  };

  const execCmd = async (device: string, command: string, source?: string, value?: number) => {
    try {
      const res = await sendSubstationCommand(device, command, source, value);
      const impact = res.process_impact || res.detail || "";
      const succeeded = res.result === "executed" || res.result === "success";
      const label = succeeded ? "SUCCEEDED" : "BLOCKED";
      const msg = `[${label}] ${deviceLabel(device)} — ${command}: ${impact}`;
      setCmdLog((prev) => [msg, ...prev].slice(0, 20));
      setTimeout(pollState, 500);
    } catch (e) {
      setCmdLog((prev) => [`[ERROR] ${e}`, ...prev].slice(0, 20));
    }
  };

  const markStepDone = (idx: number) => {
    setCompletedSteps((prev) => new Set([...prev, idx]));
    if (idx < scenario.steps.length - 1) {
      setCurrentStep(idx + 1);
    }
  };

  const step = scenario.steps[currentStep];
  const elec = state?.electrical;
  const progress = completedSteps.size / scenario.steps.length;
  const hasAction = !!step?.action;

  // Operational assessments
  const bkrClosed = elec?.breaker_closed ?? false;
  const rclClosed = elec?.recloser_closed ?? false;
  const critV = elec?.critical_load_voltage_v ?? 0;
  const totalKw = (elec?.general_load_kw ?? 0) + (elec?.critical_load_kw ?? 0);
  const customersServed = totalKw > 0;

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <p className="text-[10px] uppercase tracking-[0.3em] text-slate-600">Exercise Mode</p>
          <h2 className="text-lg font-bold text-white">{scenario.name}</h2>
          <p className="text-xs text-slate-500">{scenario.description}</p>
        </div>
        <button
          onClick={onExit}
          className="rounded border border-slate-700 bg-slate-800/50 px-3 py-1.5 text-xs text-slate-400 hover:text-slate-200"
        >
          Exit Exercise
        </button>
      </div>

      {/* Progress bar */}
      <div className="flex items-center gap-3">
        <div className="flex-1 h-1.5 rounded-full bg-slate-800 overflow-hidden">
          <div
            className="h-full rounded-full bg-sky-500 transition-all duration-500"
            style={{ width: `${progress * 100}%` }}
          />
        </div>
        <span className="text-[10px] text-slate-500 shrink-0">
          {completedSteps.size}/{scenario.steps.length} steps
        </span>
      </div>

      <div className="grid gap-4 lg:grid-cols-[220px_1fr]">
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

        {/* Right: Step detail + controls */}
        <div className="space-y-4">
          {/* Step instructions */}
          <div className="rounded-xl border border-slate-800 bg-slate-900/70 p-4">
            <div className="flex items-center justify-between mb-2">
              <h3 className="text-sm font-bold text-white">
                Step {currentStep + 1}: {step?.title}
              </h3>
              <div className="flex items-center gap-2">
                {hasAction && !completedSteps.has(currentStep) && (
                  <button
                    onClick={() => handleExecuteStep(currentStep)}
                    disabled={executing}
                    className="rounded border border-amber-700 bg-amber-950/40 px-3 py-1 text-[10px] font-medium text-amber-400 hover:bg-amber-900/50 disabled:opacity-50"
                  >
                    {executing ? "Executing..." : "Execute Step"}
                  </button>
                )}
                {!completedSteps.has(currentStep) && (
                  <button
                    onClick={() => markStepDone(currentStep)}
                    className="rounded border border-green-800 bg-green-950/40 px-2 py-1 text-[10px] font-medium text-green-400 hover:bg-green-900/50"
                  >
                    Mark Complete
                  </button>
                )}
              </div>
            </div>
            <p className="text-sm text-slate-300 leading-relaxed whitespace-pre-line">
              {step?.description}
            </p>

            {step?.description?.includes("curl") && (
              <div className="mt-3 rounded border border-slate-700 bg-slate-950 p-3 font-mono text-[11px] text-amber-400">
                {extractCommand(step.description)}
              </div>
            )}
          </div>

          {/* Step execution result */}
          {stepResult && (
            <div className={`rounded-lg border p-3 ${
              stepResult.success
                ? "border-green-700 bg-green-950/40"
                : "border-red-700 bg-red-950/40"
            }`}>
              <div className="text-[10px] font-bold uppercase tracking-wider text-slate-500 mb-1">
                Execution Result — {stepResult.action_type}
              </div>
              <div className="space-y-1">
                {stepResult.results.map((r, i) => (
                  <div key={i} className="flex items-start gap-2 text-xs">
                    <span className={`shrink-0 font-bold ${r.success ? "text-green-400" : "text-red-400"}`}>
                      {r.success ? "\u2713" : "\u2717"}
                    </span>
                    <span className="text-slate-300 font-medium">{r.action}:</span>
                    <span className="text-slate-400">{r.impact || r.detail}</span>
                  </div>
                ))}
              </div>
            </div>
          )}

          <div className="grid gap-4 lg:grid-cols-2">
            {/* containd segmentation + operational status */}
            <div className="rounded-xl border border-slate-800 bg-slate-900/70 p-4 space-y-3">
              {/* containd indicator — prominent */}
              <div className={`rounded-lg border px-3 py-2 ${
                activeConfig === "improved"
                  ? "border-green-800/60 bg-green-950/20"
                  : "border-red-800/60 bg-red-950/20"
              }`}>
                <div className="flex items-center justify-between">
                  <div>
                    <div className="text-[9px] font-medium uppercase tracking-wider text-slate-500">containd NGFW Policy</div>
                    <div className={`text-sm font-bold ${activeConfig === "improved" ? "text-green-400" : "text-red-400"}`}>
                      {activeConfig === "improved" ? "Hardened — RTAC-only field access" : "Weak Baseline — enterprise can reach field"}
                    </div>
                  </div>
                  {activeConfig === "weak" && isSegmentationStep(step?.title) && (
                    <button
                      onClick={async () => { await applyFirewallConfig("improved"); pollState(); }}
                      className="rounded border border-green-800 bg-green-950/40 px-2 py-1 text-[10px] text-green-400 hover:bg-green-900/50"
                    >
                      Apply Hardened
                    </button>
                  )}
                  {activeConfig === "improved" && isBaselineStep(step?.title) && (
                    <button
                      onClick={async () => { await applyFirewallConfig("weak"); pollState(); }}
                      className="rounded border border-red-800 bg-red-950/40 px-2 py-1 text-[10px] text-red-400 hover:bg-red-900/50"
                    >
                      Reset to Weak
                    </button>
                  )}
                </div>
              </div>

              {/* Operational status */}
              <div className="text-[10px] font-bold uppercase tracking-wider text-slate-500">Feeder Status</div>
              <div className="grid grid-cols-2 gap-2">
                <MiniStatus
                  label="Customer Service"
                  value={customersServed ? "Serving" : "OUTAGE"}
                  ok={customersServed}
                />
                <MiniStatus
                  label="Protection"
                  value={bkrClosed && rclClosed ? "Normal" : "DEGRADED"}
                  ok={bkrClosed && rclClosed}
                />
                <MiniStatus
                  label="Voltage"
                  value={critV === 0 ? "DEAD" : critV >= 114 && critV <= 126 ? `${critV.toFixed(0)}V OK` : `${critV.toFixed(0)}V BAD`}
                  ok={critV > 0 && critV >= 114 && critV <= 126}
                />
                <MiniStatus
                  label="Critical Load"
                  value={elec?.critical_load_energized ? "Energized" : "NO POWER"}
                  ok={elec?.critical_load_energized}
                />
              </div>
            </div>

            {/* Quick commands panel */}
            <div className="rounded-xl border border-slate-800 bg-slate-900/70 p-4">
              <div className="text-[10px] font-bold uppercase tracking-wider text-slate-500 mb-2">
                Supervisory Commands
              </div>
              <QuickCommands execCmd={execCmd} stepTitle={step?.title} />
            </div>
          </div>

          {/* Command log */}
          {cmdLog.length > 0 && (
            <div className="rounded-xl border border-slate-800 bg-slate-950 p-3 max-h-32 overflow-y-auto">
              <div className="text-[10px] font-bold uppercase tracking-wider text-slate-500 mb-1">
                Command Results
              </div>
              {cmdLog.map((msg, i) => (
                <div key={i} className={`font-mono text-[11px] ${
                  msg.startsWith("[SUCCEEDED]")
                    ? "text-green-400"
                    : msg.startsWith("[BLOCKED]")
                    ? "text-amber-400"
                    : msg.startsWith("[ERROR]")
                    ? "text-red-400"
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

          {/* Validation */}
          <div className="flex items-center gap-3">
            <button
              onClick={handleValidate}
              disabled={validating}
              className="rounded border border-sky-700 bg-sky-950/40 px-4 py-2 text-xs font-medium text-sky-400 transition-colors hover:bg-sky-900/50 disabled:opacity-50"
            >
              {validating ? "Validating..." : "Validate Scenario"}
            </button>
            {validation && (
              <span className={`text-sm font-bold ${
                validation.outcome === "PASS" ? "text-green-400" : validation.outcome === "FAIL" ? "text-red-400" : "text-yellow-400"
              }`}>
                {validation.outcome}
              </span>
            )}
          </div>

          {validation && (
            <div className={`rounded-lg border p-3 ${
              validation.outcome === "PASS"
                ? "border-green-700 bg-green-950/40"
                : validation.outcome === "FAIL"
                ? "border-red-700 bg-red-950/40"
                : "border-yellow-700 bg-yellow-950/40"
            }`}>
              <div className="space-y-1">
                {validation.checks.map((check, i) => (
                  <div key={i} className="flex items-start gap-2 text-xs">
                    <span className={`shrink-0 font-bold ${
                      check.status === "pass" ? "text-green-400" : check.status === "fail" ? "text-red-400" : "text-yellow-400"
                    }`}>
                      {check.status === "pass" ? "\u2713" : check.status === "fail" ? "\u2717" : "\u26A0"}
                    </span>
                    <span className="text-slate-300 font-medium">{check.name}:</span>
                    <span className="text-slate-400">{check.detail}</span>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function actionLabel(type: string): string {
  switch (type) {
    case "command": return "auto";
    case "check": return "verify";
    case "firewall": return "config";
    case "sequence": return "auto";
    default: return type;
  }
}

function deviceLabel(device: string): string {
  switch (device) {
    case "relay": return "Feeder Breaker (10.40.40.20)";
    case "recloser": return "Recloser (10.40.40.21)";
    case "regulator": return "Regulator (10.40.40.22)";
    default: return device;
  }
}

// Quick command buttons contextual to the current step
function QuickCommands({
  execCmd,
  stepTitle,
}: {
  execCmd: (device: string, command: string, source?: string, value?: number) => void;
  stepTitle?: string;
}) {
  const title = (stepTitle || "").toLowerCase();
  const isBreaker = title.includes("breaker") || title.includes("trip") || title.includes("relay");
  const isRecloser = title.includes("recloser") || title.includes("reclose");
  const isRegulator = title.includes("regulator") || title.includes("voltage") || title.includes("tap");

  const showAll = !isBreaker && !isRecloser && !isRegulator;

  return (
    <div className="space-y-2">
      {(showAll || isBreaker) && (
        <div>
          <div className="text-[9px] font-bold text-green-500 mb-1">Feeder Breaker 52 <span className="text-slate-600 font-normal">10.40.40.20</span></div>
          <div className="flex flex-wrap gap-1">
            <QCmd label="Trip (Open)" onClick={() => execCmd("relay", "trip", "web-ui")} variant="danger" />
            <QCmd label="Close" onClick={() => execCmd("relay", "close", "web-ui")} variant="success" />
            <QCmd label="Lockout" onClick={() => execCmd("relay", "lockout", "web-ui")} variant="warning" />
            <QCmd label="Unlock" onClick={() => execCmd("relay", "unlock", "web-ui")} />
            <QCmd label="Inject Fault" onClick={() => execCmd("relay", "inject_fault", "web-ui")} variant="danger" />
            <QCmd label="Clear Fault" onClick={() => execCmd("relay", "clear_fault", "web-ui")} />
          </div>
        </div>
      )}
      {(showAll || isRecloser) && (
        <div>
          <div className="text-[9px] font-bold text-green-500 mb-1">Recloser 79 <span className="text-slate-600 font-normal">10.40.40.21</span></div>
          <div className="flex flex-wrap gap-1">
            <QCmd label="Open" onClick={() => execCmd("recloser", "open", "web-ui")} variant="danger" />
            <QCmd label="Close" onClick={() => execCmd("recloser", "close", "web-ui")} variant="success" />
            <QCmd label="Enable Reclose" onClick={() => execCmd("recloser", "enable_reclose", "web-ui")} variant="success" />
            <QCmd label="Disable Reclose" onClick={() => execCmd("recloser", "disable_reclose", "web-ui")} variant="warning" />
            <QCmd label="Reset Lockout" onClick={() => execCmd("recloser", "reset_lockout", "web-ui")} />
          </div>
        </div>
      )}
      {(showAll || isRegulator) && (
        <div>
          <div className="text-[9px] font-bold text-green-500 mb-1">Regulator 90 <span className="text-slate-600 font-normal">10.40.40.22</span></div>
          <div className="flex flex-wrap gap-1">
            <QCmd label="Raise Tap" onClick={() => execCmd("regulator", "raise_tap", "web-ui")} />
            <QCmd label="Lower Tap" onClick={() => execCmd("regulator", "lower_tap", "web-ui")} />
            <QCmd label="Manual Mode" onClick={() => execCmd("regulator", "set_manual", "web-ui")} variant="warning" />
            <QCmd label="Auto Mode" onClick={() => execCmd("regulator", "set_auto", "web-ui")} variant="success" />
          </div>
        </div>
      )}
      <div className="text-[9px] text-slate-600 mt-1">
        Commands routed through containd NGFW. Hardened policy restricts access to RTAC only.
      </div>
    </div>
  );
}

function QCmd({ label, onClick, variant }: { label: string; onClick: () => void; variant?: "danger" | "success" | "warning" }) {
  const colors = {
    danger: "border-red-800 bg-red-950/50 text-red-400 hover:bg-red-900/50",
    success: "border-green-800 bg-green-950/50 text-green-400 hover:bg-green-900/50",
    warning: "border-yellow-800 bg-yellow-950/50 text-yellow-400 hover:bg-yellow-900/50",
  };
  const cls = variant ? colors[variant] : "border-slate-700 bg-slate-800/50 text-slate-300 hover:bg-slate-700/50";
  return (
    <button onClick={onClick} className={`rounded border px-2 py-1 text-[10px] font-medium transition-colors ${cls}`}>
      {label}
    </button>
  );
}

function MiniStatus({ label, value, ok }: { label: string; value: string; ok?: boolean }) {
  return (
    <div className={`rounded border px-2 py-1.5 ${
      ok === false ? "border-red-800 bg-red-950/30" : ok === true ? "border-green-800/50 bg-slate-900/50" : "border-slate-800 bg-slate-900/50"
    }`}>
      <div className="text-[9px] font-medium uppercase tracking-wider text-slate-500">{label}</div>
      <div className={`text-sm font-bold ${
        ok === false ? "text-red-400" : ok === true ? "text-green-400" : "text-slate-400"
      }`}>
        {value}
      </div>
    </div>
  );
}

function extractCommand(description: string): string {
  const match = description.match(/curl\s+.*$/m);
  return match ? match[0].trim() : "";
}

function isSegmentationStep(title?: string): boolean {
  if (!title) return false;
  return title.toLowerCase().includes("improve") || title.toLowerCase().includes("segmentation");
}

function isBaselineStep(title?: string): boolean {
  if (!title) return false;
  return title.toLowerCase().includes("baseline") || title.toLowerCase().includes("observe");
}
