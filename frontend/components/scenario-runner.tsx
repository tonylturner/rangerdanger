"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import Link from "next/link";
import {
  validateScenario,
  getSubstationState,
  getActiveFirewallConfig,
  applyFirewallConfig,
  applyCustomFirewallConfig,
  sendSubstationCommand,
  executeScenarioStep,
  getSubstationAudit,
  execOnNode,
  resetWorkshop,
  startTrafficGeneration,
  getTrafficStatus,
  startPcapCapture,
  getPcapStatus,
  getPcapDownloadUrl,
  type Scenario,
  type ValidationResult,
  type SubstationState,
  type StepExecutionResult,
  type AuditEntry,
  type PolicySource,
} from "../lib/api";
import { PolicyStatusBanner } from "./policy-status-banner";
import { getExerciseNodes, inferNodeFromDescription, NODE_LABELS, EXERCISE_NODE_MAP } from "../lib/exercise-nodes";
import { Terminal as TerminalIcon, FileText, ArrowLeft, RotateCcw, Eraser, X } from "lucide-react";
import { Tooltip, TooltipTrigger, TooltipContent } from "./ui/tooltip";
import { DecisionPanel } from "./decision-panel";
import { RemediationPlanBanner } from "./remediation-plan-banner";
import { useFirewallTrack } from "../lib/use-firewall-track";
import {
  loadDynamicPlan,
  buildContaindConfig,
  type DynamicExercisePlan,
} from "../lib/remediation-to-rules";
import {
  loadSaved,
  saveToDisk,
  storageKey,
} from "../lib/scenario-runner-storage";
import {
  POLICY_ACTION_SCENARIOS,
  VALIDATE_BUTTON_SCENARIOS,
  PHASE3_TITLES,
  titleMatches,
  deviceLabel,
  isSegmentationStep,
  isBaselineStep,
} from "../lib/scenario-runner-logic";
import {
  DescriptionSegments,
  RemediationPlanSummary,
} from "./scenario-description-blocks";
import {
  ExerciseSummary,
  MiniStatus,
  QuickCommands,
} from "./scenario-runner-widgets";
import { CommandAuditPanels, StepNavigator, TerminalPanel } from "./scenario-runner-panels";

type RunnerProps = {
  scenario: Scenario;
  onExit: () => void;
};


export function ScenarioRunner({ scenario, onExit }: RunnerProps) {
  const saved = loadSaved(scenario.id);
  const [currentStep, setCurrentStep] = useState(0);
  const [completedSteps, setCompletedSteps] = useState<Set<number>>(new Set(saved.completedSteps));
  const [notes, setNotes] = useState<string>(saved.notes);
  const [showSummary, setShowSummary] = useState(false);
  const [validation, setValidation] = useState<ValidationResult | null>(null);
  const [validating, setValidating] = useState(false);
  const [state, setState] = useState<SubstationState | null>(null);
  const [activeConfig, setActiveConfig] = useState<string | null>(null);
  // Firewall-track choice (guided | technical | null). Read here so
  // both the description renderer (for trackOnly segments) and the
  // side panel (for chip + button de-emphasis) share one source.
  const { track: firewallTrack, setTrack: setFirewallTrack } = useFirewallTrack();
  // policySource tracks how the active policy got applied - needed by
  // PolicyStatusBanner to distinguish "Your custom policy (Lab 1.4
  // plan)" from "(your containd commit)". Survives page reloads
  // because it's a backend field.
  const [policySource, setPolicySource] = useState<PolicySource>("");
  const [cmdLog, setCmdLog] = useState<string[]>(saved.cmdLog || []);
  const [executing, setExecuting] = useState(false);
  // String IDs (e.g. "body-0", "hint-0-1") so body + hint commands can
  // each have copy/run buttons without index collisions.
  const [autoRunning, setAutoRunning] = useState<string | null>(null);
  const [resettingLab, setResettingLab] = useState(false);
  const [stepResult, setStepResult] = useState<StepExecutionResult | null>(null);
  const [recentAudit, setRecentAudit] = useState<AuditEntry[]>([]);
  const exerciseNodes = getExerciseNodes(scenario.id, scenario.nodes);
  // Load the saved Lab 1.4 remediation plan on every firewall lab,
  // not just firewall-implementation. The plan drives the side-
  // panel Apply Your Plan button on Lab 2.3 / 2.3-bonus / 2.4 too,
  // so leaving it null on those labs left the button stuck in the
  // disabled-with-tooltip state even when a plan existed (Codex
  // review on #74). The injectDynamicContent path stays scoped to
  // firewall-implementation - only that lab actually uses the
  // plan to rewrite step descriptions.
  const [dynamicPlan, setDynamicPlan] = useState<DynamicExercisePlan | null>(() =>
    POLICY_ACTION_SCENARIOS.includes(scenario.id) ? loadDynamicPlan() : null
  );
  useEffect(() => {
    if (!POLICY_ACTION_SCENARIOS.includes(scenario.id)) return;
    setDynamicPlan(loadDynamicPlan());
    const onFocus = () => setDynamicPlan(loadDynamicPlan());
    document.addEventListener("visibilitychange", onFocus);
    return () => document.removeEventListener("visibilitychange", onFocus);
  }, [scenario.id]);
  const [showTerminalPanel, setShowTerminalPanel] = useState(false);
  const [activeTerminalNode, setActiveTerminalNode] = useState(exerciseNodes[0] || "");
  const [panelMode, setPanelMode] = useState<"terminal" | "ui">("terminal");
  const [panelHeight, setPanelHeight] = useState(300);
  const resizingRef = useRef(false);
  const resizeStartRef = useRef({ y: 0, h: 0 });
  const [generatingTraffic, setGeneratingTraffic] = useState(false);
  const [capturing, setCapturing] = useState(false);

  // Panel resize handlers
  const onResizeStart = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    resizingRef.current = true;
    resizeStartRef.current = { y: e.clientY, h: panelHeight };
    const onMove = (ev: MouseEvent) => {
      if (!resizingRef.current) return;
      const delta = resizeStartRef.current.y - ev.clientY;
      const next = Math.max(200, Math.min(800, resizeStartRef.current.h + delta));
      setPanelHeight(next);
    };
    const onUp = () => {
      resizingRef.current = false;
      document.removeEventListener("mousemove", onMove);
      document.removeEventListener("mouseup", onUp);
    };
    document.addEventListener("mousemove", onMove);
    document.addEventListener("mouseup", onUp);
  }, [panelHeight]);

  // Persist to localStorage on change
  useEffect(() => {
    saveToDisk(scenario.id, { completedSteps: [...completedSteps], notes, cmdLog });
  }, [completedSteps, notes, cmdLog, scenario.id]);

  const resetProgress = () => {
    if (!window.confirm("Reset all exercise progress? This clears completed steps, notes, and command log.")) return;
    setCompletedSteps(new Set());
    setNotes("");
    setCurrentStep(0);
    setCmdLog([]);
    setValidation(null);
    setStepResult(null);
    try { localStorage.removeItem(storageKey(scenario.id)); } catch { /* ignore */ }
  };

  const handleAutoRun = async (cmd: string, runId: string, stepDesc: string) => {
    const step = scenario.steps[currentStep];
    const nodeId = step?.node
      || inferNodeFromDescription(stepDesc)
      || (EXERCISE_NODE_MAP[scenario.id]?.primary)
      || exerciseNodes[0];

    if (!nodeId) {
      setCmdLog((prev) => [`[ERROR] No target node for command - open terminal and run manually`, ...prev].slice(0, 100));
      return;
    }

    setAutoRunning(runId);
    const nodeLabel = NODE_LABELS[nodeId] || nodeId;
    setCmdLog((prev) => [`[RUN on ${nodeLabel}] ${cmd}`, ...prev].slice(0, 100));

    try {
      const result = await execOnNode(nodeId, cmd, 30);
      const lines = (result.stdout || result.stderr || "").split("\n").filter(Boolean);
      const output = lines.length > 20
        ? [...lines.slice(0, 18), `... (${lines.length - 18} more lines)`]
        : lines;
      const status = result.exit_code === 0 ? "OK" : `EXIT ${result.exit_code}`;
      setCmdLog((prev) => [
        `[${status}] completed in ${result.duration_ms}ms`,
        ...output.map((l) => "  " + l),
        ...prev,
      ].slice(0, 100));
      setTimeout(pollState, 500);
    } catch (e) {
      setCmdLog((prev) => [`[ERROR] ${e}`, ...prev].slice(0, 100));
    } finally {
      setAutoRunning(null);
    }
  };

  const handleLabReset = async () => {
    if (!window.confirm("Reset the lab environment? This restores the weak baseline firewall config and resets all field device state.")) return;
    setResettingLab(true);
    setCmdLog((prev) => [`[RESET] Restoring lab to default state...`, ...prev].slice(0, 100));
    try {
      const result = await resetWorkshop();
      const actionLines = result.actions.map(
        (a) => `  ${a.success ? "\u2713" : "\u2717"} ${a.action}: ${a.detail}`
      );
      setCmdLog((prev) => [
        `[RESET] ${result.success ? "Lab restored" : "Reset had errors"} (${result.actions.length} actions)`,
        ...actionLines,
        ...prev,
      ].slice(0, 100));
      setTimeout(pollState, 500);
    } catch (e) {
      setCmdLog((prev) => [`[ERROR] Reset failed: ${e}`, ...prev].slice(0, 100));
    } finally {
      setResettingLab(false);
    }
  };

  const handleGenerateTraffic = async (durationSec = 45) => {
    setGeneratingTraffic(true);
    setCmdLog((prev) => [`[TRAFFIC] Generating ${durationSec}s of representative OT traffic...`, ...prev].slice(0, 100));
    try {
      await startTrafficGeneration(durationSec);
      setCmdLog((prev) => [`[TRAFFIC] Generation started - ${durationSec}s of Modbus, DNP3, HTTP, NTP flows`, ...prev].slice(0, 100));
      // Poll for completion
      const pollId = setInterval(async () => {
        try {
          const status = await getTrafficStatus();
          if (!status.generating) {
            clearInterval(pollId);
            setGeneratingTraffic(false);
            setCmdLog((prev) => [`[TRAFFIC] Complete - ${status.flows_generated || 0} flows generated`, ...prev].slice(0, 100));
          }
        } catch { clearInterval(pollId); setGeneratingTraffic(false); }
      }, 3000);
    } catch (e) {
      setCmdLog((prev) => [`[ERROR] Traffic generation failed: ${e}`, ...prev].slice(0, 100));
      setGeneratingTraffic(false);
    }
  };

  const handleStartCapture = async (durationSec = 60, name = "baseline") => {
    setCapturing(true);
    setCmdLog((prev) => [`[CAPTURE] Starting ${durationSec}s packet capture on firewall...`, ...prev].slice(0, 100));
    try {
      await startPcapCapture(durationSec, name);
      setCmdLog((prev) => [`[CAPTURE] Recording on all firewall interfaces`, ...prev].slice(0, 100));
      const pollId = setInterval(async () => {
        try {
          const status = await getPcapStatus();
          if (!status.capturing) {
            clearInterval(pollId);
            setCapturing(false);
            const url = getPcapDownloadUrl();
            setCmdLog((prev) => [`[CAPTURE] Complete - download at ${url}`, ...prev].slice(0, 100));
          }
        } catch { clearInterval(pollId); setCapturing(false); }
      }, 3000);
    } catch (e) {
      setCmdLog((prev) => [`[ERROR] Capture failed: ${e}`, ...prev].slice(0, 100));
      setCapturing(false);
    }
  };

  const pollState = useCallback(async () => {
    try {
      const [s, fw, a] = await Promise.all([
        getSubstationState(),
        getActiveFirewallConfig(),
        getSubstationAudit(),
      ]);
      setState(s);
      setActiveConfig(fw.active_config);
      setPolicySource(fw.policy_source ?? "");
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
      const msg = `[${label}] ${deviceLabel(device)} - ${command}: ${impact}`;
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
          <p className="text-[10px] uppercase tracking-[0.3em] text-slate-600">
            Lab {scenario.order ?? ""}
          </p>
          <h2 className="text-lg font-bold text-white">{scenario.name}</h2>
          <p className="mt-1 text-xs text-slate-400 max-w-2xl">{scenario.description}</p>
        </div>
        <div className="flex items-center gap-2">
          {completedSteps.size > 0 && (
            <Tooltip>
              <TooltipTrigger asChild>
                <button
                  onClick={() => setShowSummary(!showSummary)}
                  aria-label={showSummary ? "Back to Exercise" : "View Summary"}
                  className="inline-flex items-center justify-center rounded border border-sky-700 bg-sky-950/40 px-3 py-1.5 text-xs text-sky-400 hover:bg-sky-900/50"
                >
                  {showSummary ? <ArrowLeft className="h-4 w-4" /> : <FileText className="h-4 w-4" />}
                </button>
              </TooltipTrigger>
              <TooltipContent>{showSummary ? "Back to Exercise" : "View Summary"}</TooltipContent>
            </Tooltip>
          )}
          {exerciseNodes.length > 0 && (
            <Tooltip>
              <TooltipTrigger asChild>
                <button
                  onClick={() => setShowTerminalPanel(!showTerminalPanel)}
                  aria-label={showTerminalPanel ? "Hide terminal" : "Show terminal"}
                  className={`inline-flex items-center justify-center rounded border px-3 py-1.5 text-xs transition-all ${
                    showTerminalPanel
                      ? "border-cyan-400 bg-cyan-950/60 text-cyan-300 ring-2 ring-cyan-400/60 ring-offset-0"
                      : "border-slate-700 bg-slate-800/50 text-slate-400 hover:text-cyan-400 hover:border-slate-600"
                  }`}
                >
                  <TerminalIcon className="h-4 w-4" />
                </button>
              </TooltipTrigger>
              <TooltipContent>{showTerminalPanel ? "Hide terminal" : "Show terminal"}</TooltipContent>
            </Tooltip>
          )}
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={handleLabReset}
                disabled={resettingLab}
                aria-label="Reset Lab"
                className="inline-flex items-center justify-center rounded border border-amber-800/50 bg-amber-950/30 px-3 py-1.5 text-xs text-amber-500 hover:bg-amber-900/40 disabled:opacity-50"
              >
                <RotateCcw className={`h-4 w-4 ${resettingLab ? "animate-spin" : ""}`} />
              </button>
            </TooltipTrigger>
            <TooltipContent>{resettingLab ? "Resetting lab..." : "Reset Lab"}</TooltipContent>
          </Tooltip>
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={resetProgress}
                aria-label="Reset Progress"
                className="inline-flex items-center justify-center rounded border border-slate-700 bg-slate-800/50 px-3 py-1.5 text-xs text-slate-500 hover:text-red-400"
              >
                <Eraser className="h-4 w-4" />
              </button>
            </TooltipTrigger>
            <TooltipContent>Reset Progress</TooltipContent>
          </Tooltip>
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={onExit}
                aria-label="Exit Exercise"
                className="inline-flex items-center justify-center rounded border border-slate-700 bg-slate-800/50 px-3 py-1.5 text-xs text-slate-400 hover:text-slate-200"
              >
                <X className="h-4 w-4" />
              </button>
            </TooltipTrigger>
            <TooltipContent>Exit Exercise</TooltipContent>
          </Tooltip>
        </div>
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

      {/* ── Remediation Plan Banner (shown on later exercises) ── */}
      {!showSummary && <RemediationPlanBanner currentExerciseId={scenario.id} />}

      {/* ── Summary View ──────────────────────────────────────── */}
      {showSummary && (
        <ExerciseSummary
          scenario={scenario}
          completedSteps={completedSteps}
          notes={notes}
          activeConfig={activeConfig}
        />
      )}

      {/* ── Exercise View ─────────────────────────────────────── */}
      {!showSummary && <div className="grid gap-4 lg:grid-cols-[220px_1fr]">
        <StepNavigator
          scenario={scenario}
          completedSteps={completedSteps}
          currentStep={currentStep}
          setCurrentStep={setCurrentStep}
          setStepResult={setStepResult}
        />

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
                {!completedSteps.has(currentStep) && (() => {
                  // No force-pick: Lab 2.2 defaults to the Guided track
                  // (see useFirewallTrack), so step 1 advances freely.
                  // Students switch to Advanced from the picker / side
                  // panel anytime; the choice persists to localStorage
                  // so later firewall labs inherit it.
                  const trackGate = false;
                  return (
                    <button
                      onClick={() => markStepDone(currentStep)}
                      disabled={trackGate}
                      title={trackGate ? "Pick a track above to continue." : undefined}
                      className={
                        trackGate
                          ? "cursor-not-allowed rounded border border-slate-700 bg-slate-900/40 px-2 py-1 text-[10px] font-medium text-slate-500"
                          : "rounded border border-green-800 bg-green-950/40 px-2 py-1 text-[10px] font-medium text-green-400 hover:bg-green-900/50"
                      }
                    >
                      Mark Complete
                    </button>
                  );
                })()}
              </div>
            </div>
            {/* PolicyStatusBanner: sticky-at-top per-step indicator of
                what firewall policy is actually running. Renders an
                informational variant when the active policy satisfies
                the step's expected_config; switches to an amber warning
                with the action prompt when it doesn't. Replaces the
                older inline "Config mismatch" notice that only fired
                on mismatched state - the banner always renders so
                students can always tell what's loaded. */}
            <PolicyStatusBanner
              activeConfig={activeConfig}
              policySource={policySource}
              expectedConfig={step?.expected_config}
            />
            {/* Per-step track chip: on the firewall labs (after the
                picker on 2.2 step 1), show which path the student is on
                and let them switch inline without hunting for the side
                panel. */}
            {POLICY_ACTION_SCENARIOS.includes(scenario.id) &&
              !(scenario.id === "firewall-implementation" && currentStep === 0) && (
              <div className="mb-3 flex items-center gap-2 text-[10px]">
                <span className="font-medium uppercase tracking-wider text-slate-500">
                  Track
                </span>
                <span
                  className={`rounded px-2 py-0.5 font-bold uppercase tracking-wider ${
                    firewallTrack === "technical"
                      ? "bg-sky-950/60 text-sky-300"
                      : "bg-emerald-950/60 text-emerald-300"
                  }`}
                >
                  {firewallTrack === "technical" ? "Advanced" : "Guided"}
                </span>
                <button
                  onClick={() =>
                    setFirewallTrack(
                      firewallTrack === "technical" ? "guided" : "technical",
                    )
                  }
                  className="text-slate-500 underline transition-colors hover:text-slate-300"
                >
                  switch to {firewallTrack === "technical" ? "Guided" : "Advanced"}
                </button>
              </div>
            )}
            {/* Dynamic plan summary for Exercise 3 Phase 3 */}
            {scenario.id === "firewall-implementation" && dynamicPlan?.hasRemediationPlan &&
              step && titleMatches(step.title, PHASE3_TITLES) && (
              <div className="mb-3">
                <RemediationPlanSummary plan={dynamicPlan} />
              </div>
            )}
            {step?.description && (
              <DescriptionSegments
                scenarioId={scenario.id}
                currentStep={currentStep}
                stepTitle={step.title}
                stepDescription={step.description}
                dynamicPlan={dynamicPlan}
                exerciseNodes={exerciseNodes}
                firewallTrack={firewallTrack}
                autoRunning={autoRunning}
                handleAutoRun={handleAutoRun}
                handleGenerateTraffic={handleGenerateTraffic}
                generatingTraffic={generatingTraffic}
              />
            )}

            {/* Quick action buttons for traffic/capture steps */}
            {step?.description && (step.description.includes("traffic/generate") || step.description.includes("representative traffic")) && (
              <div className="mt-2">
                <button
                  onClick={() => handleGenerateTraffic(45)}
                  disabled={generatingTraffic}
                  className="rounded border border-sky-800/60 bg-sky-950/40 px-3 py-1.5 text-[10px] font-medium text-sky-400 hover:bg-sky-900/50 disabled:opacity-50"
                >
                  {generatingTraffic ? "Generating Traffic..." : "Generate Traffic (45s)"}
                </button>
              </div>
            )}
            {step?.description && (step.description.includes("pcap/start") || step.description.includes("packet capture")) && !step.description.includes("representative traffic") && (
              <div className="mt-2 flex items-center gap-2">
                <button
                  onClick={() => handleStartCapture(60, "baseline")}
                  disabled={capturing}
                  className="rounded border border-amber-800/60 bg-amber-950/40 px-3 py-1.5 text-[10px] font-medium text-amber-400 hover:bg-amber-900/50 disabled:opacity-50"
                >
                  {capturing ? "Capturing..." : "Start 60s Capture"}
                </button>
                {!capturing && (
                  <a
                    href={getPcapDownloadUrl()}
                    className="text-[10px] text-slate-500 hover:text-sky-400"
                    download
                  >
                    Download Last Capture
                  </a>
                )}
              </div>
            )}

            {/* Decision panel - only for steps with action.type === "decision" */}
            {step?.action?.type === "decision" && (
              <DecisionPanel exerciseId={scenario.id} action={step.action} />
            )}

            {/* Shared notes - persists across all steps */}
            <div className="mt-3">
              <label className="block text-[9px] font-bold uppercase tracking-wider text-slate-600 mb-1">
                Exercise Notes
              </label>
              <textarea
                value={notes}
                onChange={(e) => setNotes(e.target.value)}
                placeholder="Document your findings, observations, or answers here. Notes persist across all steps."
                className="w-full rounded border border-slate-800 bg-slate-950 px-3 py-2 text-xs text-slate-300 placeholder-slate-700 focus:border-sky-700 focus:outline-none resize-y min-h-[60px]"
                rows={3}
              />
            </div>
          </div>

          {/* Step execution result */}
          {stepResult && (
            <div className={`rounded-lg border p-3 ${
              stepResult.success
                ? "border-green-700 bg-green-950/40"
                : "border-red-700 bg-red-950/40"
            }`}>
              <div className="text-[10px] font-bold uppercase tracking-wider text-slate-500 mb-1">
                Execution Result - {stepResult.action_type}
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
            {/* containd policy actions + operational status. The
                state indicator (what is currently running) lives in
                PolicyStatusBanner at the top of the step content;
                this lower controls section is just the actions. */}
            <div className="rounded-xl border border-slate-800 bg-slate-900/70 p-4 space-y-3">
              {/* Policy action buttons - shown on the four firewall-
                  exercising scenarios (2.2 / 2.3 / 2.3-bonus / 2.4),
                  hidden in the planning labs (1.2 / 1.3 / 1.4) since
                  their steps don't actually drive policy state. */}
              {POLICY_ACTION_SCENARIOS.includes(scenario.id) && (
                <div className="rounded-lg border border-slate-800 bg-slate-950/40 px-3 py-2">
                  <div className="mb-2 flex items-center justify-between gap-2">
                    <div className="text-[9px] font-medium uppercase tracking-wider text-slate-500">
                      containd NGFW - policy actions
                    </div>
                    {firewallTrack && (
                      <div className="flex items-center gap-1.5 text-[9px]">
                        <span
                          className={`rounded px-1.5 py-0.5 font-bold uppercase tracking-wider ${
                            firewallTrack === "guided"
                              ? "bg-emerald-950/60 text-emerald-300"
                              : "bg-sky-950/60 text-sky-300"
                          }`}
                        >
                          {firewallTrack === "technical" ? "Advanced" : "Guided"}
                        </span>
                        <button
                          onClick={() =>
                            setFirewallTrack(
                              firewallTrack === "guided" ? "technical" : "guided",
                            )
                          }
                          className="text-slate-500 underline hover:text-slate-300"
                        >
                          switch
                        </button>
                      </div>
                    )}
                  </div>
                  {firewallTrack === "technical" && (
                    <div className="mb-2 text-[10px] italic text-slate-500">
                      Advanced track - commit your policy in containd directly.
                      Buttons below are a guided fallback.
                    </div>
                  )}
                  <div className="flex flex-wrap gap-2">
                    {/* Apply Hardened: load the canned reference. */}
                    {activeConfig !== "improved" && (
                      <button
                        onClick={async () => { await applyFirewallConfig("improved"); pollState(); }}
                        className={
                          firewallTrack === "technical"
                            ? "rounded border border-emerald-900/40 bg-emerald-950/20 px-1.5 py-0.5 text-[9px] text-emerald-400/70 hover:bg-emerald-900/40"
                            : "rounded border border-emerald-800 bg-emerald-950/40 px-2 py-1 text-[10px] font-medium text-emerald-300 hover:bg-emerald-900/50"
                        }
                      >
                        Apply Hardened
                      </button>
                    )}
                    {/* Apply Your Plan: push the policy built from the
                        student's Lab 1.4 picks. Disabled-with-tooltip
                        when no plan exists, rather than silently
                        hidden - students should see it as an option
                        and learn what unlocks it. */}
                    {dynamicPlan?.hasRemediationPlan ? (
                      <button
                        onClick={async () => {
                          const config = buildContaindConfig(dynamicPlan);
                          await applyCustomFirewallConfig(config);
                          pollState();
                          setCmdLog((prev) => [`[APPLIED] Your remediation plan config pushed to containd`, ...prev].slice(0, 100));
                        }}
                        className={
                          firewallTrack === "technical"
                            ? "rounded border border-sky-900/40 bg-sky-950/20 px-1.5 py-0.5 text-[9px] text-sky-400/70 hover:bg-sky-900/40"
                            : "rounded border border-sky-700 bg-sky-950/40 px-2 py-1 text-[10px] font-medium text-sky-300 hover:bg-sky-900/50"
                        }
                      >
                        Apply Your Plan
                      </button>
                    ) : (
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <button
                            disabled
                            className="cursor-not-allowed rounded border border-slate-700/60 bg-slate-900/40 px-2 py-1 text-[10px] font-medium text-slate-500"
                          >
                            Apply Your Plan
                          </button>
                        </TooltipTrigger>
                        <TooltipContent>
                          Complete Lab 1.4 (Remediation Planning) first.
                          The system builds this policy from your plan
                          picks.
                        </TooltipContent>
                      </Tooltip>
                    )}
                    {/* Reset to Weak: available from any non-weak
                        state so students can re-attempt an exercise
                        from the baseline regardless of how they
                        landed on the current policy. */}
                    {activeConfig !== "weak" && activeConfig && (
                      <button
                        onClick={async () => { await applyFirewallConfig("weak"); pollState(); }}
                        className={
                          firewallTrack === "technical"
                            ? "rounded border border-rose-900/40 bg-rose-950/20 px-1.5 py-0.5 text-[9px] text-rose-400/70 hover:bg-rose-900/40"
                            : "rounded border border-rose-800 bg-rose-950/40 px-2 py-1 text-[10px] font-medium text-rose-300 hover:bg-rose-900/50"
                        }
                      >
                        Reset to Weak
                      </button>
                    )}
                  </div>
                </div>
              )}

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

          <CommandAuditPanels cmdLog={cmdLog} setCmdLog={setCmdLog} recentAudit={recentAudit} />

          {/* Validation - only on labs whose validator checks meaningful live
              state (see VALIDATE_BUTTON_SCENARIOS). Hidden on planning/analysis
              labs where the validator can only report "lab is healthy". */}
          {VALIDATE_BUTTON_SCENARIOS.includes(scenario.id) && (
          <div className="flex items-center gap-3">
            <button
              onClick={handleValidate}
              disabled={validating}
              className="rounded border border-sky-700 bg-sky-950/40 px-4 py-2 text-xs font-medium text-sky-400 transition-colors hover:bg-sky-900/50 disabled:opacity-50"
            >
              {validating ? "Validating..." : "Validate Exercise"}
            </button>
            {validation && (
              <span className={`text-sm font-bold ${
                validation.outcome === "PASS" ? "text-green-400" : validation.outcome === "FAIL" ? "text-red-400" : "text-yellow-400"
              }`}>
                {validation.outcome}
              </span>
            )}
          </div>
          )}

          {VALIDATE_BUTTON_SCENARIOS.includes(scenario.id) && validation && (
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
      </div>}

      {/* ── Node Terminal/UI Panel ─────────────────────────────── */}
      {showTerminalPanel && exerciseNodes.length > 0 && !showSummary && (
        <TerminalPanel
          exerciseNodes={exerciseNodes}
          activeTerminalNode={activeTerminalNode}
          setActiveTerminalNode={setActiveTerminalNode}
          panelMode={panelMode}
          setPanelMode={setPanelMode}
          panelHeight={panelHeight}
          setPanelHeight={setPanelHeight}
          onResizeStart={onResizeStart}
        />
      )}
    </div>
  );
}
