"use client";

import { useState, useEffect, useCallback, useMemo, useRef } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { refreshPolicyQueries } from "../lib/policy-apply";
import {
  validateScenario,
  sendSubstationCommand,
  executeScenarioStep,
  execOnNode,
  resetWorkshop,
  startTrafficGeneration,
  getTrafficStatus,
  type Scenario,
  type ValidationResult,
  type StepExecutionResult,
} from "../lib/api";
import { PolicyStatusBanner } from "./policy-status-banner";
import { getExerciseNodes, inferNodeFromDescription, NODE_LABELS } from "../lib/exercise-nodes";
import { Terminal as TerminalIcon, FileText, ArrowLeft, RotateCcw, Eraser, X } from "lucide-react";
import { Tooltip, TooltipTrigger, TooltipContent } from "./ui/tooltip";
import { DecisionPanel } from "./decision-panel";
import { RemediationPlanBanner } from "./remediation-plan-banner";
import { loadRemediationPlan, saveEmptyRemediationPlanIfMissing } from "../lib/remediation-plan";
import { useCompiledPlan } from "../lib/use-compiled-plan";
import { CompiledPlanSummary } from "./compiled-plan-summary";
import {
  clearProgress,
  loadProgress,
  loadValidation,
  saveProgress,
  saveValidation,
  initialStepIndex,
} from "../lib/scenario-runner-storage";
import { useCurriculumScope } from "../lib/curriculum-scope";
import { isRangeNotReady, logLineFor } from "../lib/range";
import { useActiveFirewall, useRefreshLive, useSubstationAudit, useSubstationState } from "../lib/live-queries";
import { useOperatorView, usePackagePolicies } from "../lib/operator-queries";
import { usFeederPolling } from "../lib/operator-view";
import { RunnerOperatorStatus } from "./runner-operator-status";
import {
  showsValidateButton,
} from "../lib/scenario-runner-logic";
import {
  DescriptionSegments,
} from "./scenario-description-blocks";
import {
  ExerciseSummary,
  MiniStatus,
  QuickCommands,
} from "./scenario-runner-widgets";
import { CommandAuditPanels, StepNavigator, TerminalPanel } from "./scenario-runner-panels";
import { splitDescription } from "../lib/scenario-description";
import { PolicyCatchUpControls } from "./policy-catch-up-controls";

type RunnerProps = {
  scenario: Scenario;
  onExit: () => void;
};


export function ScenarioRunner({ scenario, onExit }: RunnerProps) {
  const scope = useCurriculumScope();
  const queryClient = useQueryClient();
  const [saved] = useState(() => loadProgress(scope, scenario));
  const [currentStep, setCurrentStep] = useState(() => initialStepIndex(scenario, saved));
  const [completedStepIds, setCompletedStepIds] = useState<Set<string>>(() => new Set(saved.completedStepIds));
  const [skippedStepIds, setSkippedStepIds] = useState<Set<string>>(() => new Set(saved.skippedStepIds));
  const [notes, setNotes] = useState<string>(saved.notes);
  const [showSummary, setShowSummary] = useState(false);
  const [validation, setValidation] = useState<ValidationResult | null>(() => loadValidation(scope, scenario.id));
  const [validationError, setValidationError] = useState(false);
  const [validating, setValidating] = useState(false);
  // US RTAC reads only for the US operator view; other packages show
  // their own operator status (RunnerOperatorStatus).
  const operatorView = useOperatorView();
  const usFeeder = usFeederPolling(operatorView);
  // Planning surfaces are opt-in package curriculum metadata.
  const binding = scope.curriculum;
  const hasPolicyActions = !!binding?.activities[scenario.id]?.policyActions;
  const stateQuery = useSubstationState(3000, usFeeder);
  const state = stateQuery.data ?? null;
  // No feeder while the range is being replaced; the status cards wait
  // instead of reading the missing state as an outage.
  const feederWaiting = !state && isRangeNotReady(stateQuery.error);
  const activeFirewall = useActiveFirewall(3000).data;
  const activeConfig = activeFirewall?.active_config ?? null;
  // policySource tracks how the active policy got applied - needed by
  // PolicyStatusBanner to distinguish "Your custom policy (Lab 1.4
  // plan)" from "(your containd commit)". Survives page reloads
  // because it's a backend field.
  const policySource = activeFirewall?.policy_source ?? "";
  const packagePolicies = usePackagePolicies();
  const [cmdLog, setCmdLog] = useState<string[]>(saved.cmdLog);
  const [executing, setExecuting] = useState(false);
  // String IDs (e.g. "body-0", "hint-0-1") so body + hint commands can
  // each have copy/run buttons without index collisions.
  const [autoRunning, setAutoRunning] = useState<string | null>(null);
  const [resettingLab, setResettingLab] = useState(false);
  const [stepResult, setStepResult] = useState<StepExecutionResult | null>(null);
  const auditEntries = useSubstationAudit(3000, usFeeder).data?.entries;
  const recentAudit = useMemo(() => (auditEntries ?? []).slice(-5), [auditEntries]);
  const refreshLive = useRefreshLive();
  const exerciseNodes = getExerciseNodes(scenario.id, scenario.nodes, binding);
  // Every activity opting into policy actions loads the saved package plan.
  // Dynamic step text is narrower: only the binding's dynamicSteps are rewritten.
  const appliedPlan = policySource === "plan" ? activeFirewall?.plan : undefined;
  const appliedOnly = scenario.id !== binding?.roles.implementation || !!appliedPlan;
  const compiled = useCompiledPlan(scope, hasPolicyActions, appliedPlan, appliedOnly ? "applied" : "request");
  // A failed refresh must never leave an obsolete export or apply button enabled.
  const dynamicPlan = hasPolicyActions && !compiled.isError && (!appliedOnly || appliedPlan) ? compiled.data ?? null : null;
  const draft = useCompiledPlan(scope, hasPolicyActions);
  const draftPlan = hasPolicyActions && !draft.isError ? draft.data ?? null : null;
  const [showTerminalPanel, setShowTerminalPanel] = useState(false);
  const [terminalOpened, setTerminalOpened] = useState(false);
  const [activeTerminalNode, setActiveTerminalNode] = useState(exerciseNodes[0] || "");
  const [panelMode, setPanelMode] = useState<"terminal" | "ui">("terminal");
  const [panelHeight, setPanelHeight] = useState(300);
  const resizingRef = useRef(false);
  const resizeStartRef = useRef({ y: 0, h: 0 });
  const [generatingTraffic, setGeneratingTraffic] = useState(false);
  const trafficPollRef = useRef<ReturnType<typeof setInterval> | null>(null);
  useEffect(() => () => {
    if (trafficPollRef.current !== null) clearInterval(trafficPollRef.current);
  }, []);

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

  // Persist to curriculum storage on change
  useEffect(() => {
    saveProgress(scope, scenario.id, { completedStepIds: [...completedStepIds], skippedStepIds: [...skippedStepIds], notes, cmdLog, currentStepId: scenario.steps[currentStep]?.id });
  }, [scope, completedStepIds, skippedStepIds, notes, cmdLog, scenario, currentStep]);

  const resetProgress = () => {
    if (!window.confirm("Reset all exercise progress? This clears completed/skipped steps, notes, and command log. The last validation remains as a historical result.")) return;
    setCompletedStepIds(new Set());
    setSkippedStepIds(new Set());
    setNotes("");
    setCurrentStep(0);
    setShowSummary(false);
    setCmdLog([]);
    setStepResult(null);
    clearProgress(scope, scenario.id);
  };

  const handleAutoRun = async (cmd: string, runId: string, stepDesc: string, sourceNode?: string) => {
    const step = scenario.steps[currentStep];
    const nodeId = sourceNode
      || step?.node
      || inferNodeFromDescription(stepDesc, binding)
      || (binding?.nodeMap[scenario.id]?.primary)
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
      const status = result.exit_code === 0 ? "OK" : `EXIT ${result.exit_code}`;
      // Keep each completion intact: the history cap evicts old entries,
      // never diagnostic lines within the latest command's two streams.
      const output = [
        `[${status}] completed in ${result.duration_ms}ms`,
        ...(result.stdout ? [`[stdout]\n${result.stdout}`] : []),
        ...(result.stderr ? [`[stderr]\n${result.stderr}`] : []),
      ].join("\n");
      setCmdLog((prev) => [
        output,
        ...prev,
      ].slice(0, 100));
      setTimeout(refreshLive, 500);
    } catch (e) {
      setCmdLog((prev) => [logLineFor(e), ...prev].slice(0, 100));
    } finally {
      setAutoRunning(null);
    }
  };

  const handleLabReset = async () => {
    if (!window.confirm("Reset the lab environment? This deletes captures, replaces any hand-written firewall policy with the Weak baseline, and resets all field device state. Save your evidence and export your policy first.")) return;
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
      setTimeout(refreshLive, 500);
    } catch (e) {
      setCmdLog((prev) => [logLineFor(e, "Reset failed: "), ...prev].slice(0, 100));
    } finally {
      setResettingLab(false);
    }
  };

  const handleGenerateTraffic = async (durationSec = 45) => {
    setGeneratingTraffic(true);
    setCmdLog((prev) => [`[TRAFFIC] Generating ${durationSec}s of representative OT traffic...`, ...prev].slice(0, 100));
    try {
      await startTrafficGeneration(durationSec);
      setCmdLog((prev) => [`[TRAFFIC] Generation started - ${durationSec}s of package-defined scenario flows`, ...prev].slice(0, 100));
      // Poll for completion
      trafficPollRef.current = setInterval(async () => {
        try {
          const status = await getTrafficStatus();
          if (!status.generating) {
            if (trafficPollRef.current !== null) clearInterval(trafficPollRef.current);
            trafficPollRef.current = null;
            setGeneratingTraffic(false);
            setCmdLog((prev) => [`[TRAFFIC] Complete - ${status.flows_generated || 0} flows generated`, ...prev].slice(0, 100));
          }
        } catch (e) {
          if (trafficPollRef.current !== null) clearInterval(trafficPollRef.current);
          trafficPollRef.current = null;
          setGeneratingTraffic(false);
          setCmdLog((prev) => [logLineFor(e, "Traffic status unavailable; generation outcome unknown: "), ...prev].slice(0, 100));
        }
      }, 3000);
    } catch (e) {
      setCmdLog((prev) => [logLineFor(e, "Traffic generation failed: "), ...prev].slice(0, 100));
      setGeneratingTraffic(false);
    }
  };

  const changeRequest = () => {
    const plan = hasPolicyActions ? loadRemediationPlan(scope) : null;
    return plan?.selectedActionIds.length ? { packageId: scope.packageId, revision: scope.revision, actionIds: plan.selectedActionIds } : undefined;
  };

  const handleValidate = async () => {
    setValidating(true);
    setValidationError(false);
    try {
      const result = await validateScenario(scenario.package_id, scenario.id, changeRequest());
      saveValidation(scope, result);
      setValidation(result);
    } catch {
      setValidationError(true);
    } finally {
      setValidating(false);
    }
  };

  const handleExecuteStep = async (idx: number) => {
    setExecuting(true);
    setStepResult(null);
    try {
      const result = await executeScenarioStep(scenario.package_id, scenario.id, idx, changeRequest());
      setStepResult(result);

      // Log results
      for (const r of result.results) {
        const label = r.success ? "SUCCEEDED" : "NOT MET";
        const msg = `[${label}] Step ${idx + 1}: ${scenario.steps[idx]?.title} — ${r.action}: ${r.impact || r.detail}`;
        setCmdLog((prev) => [msg, ...prev].slice(0, 100));
      }

      if (result.success) {
        markStepDone(idx);
      }

      setTimeout(refreshLive, 500);
    } catch (e) {
      setCmdLog((prev) => [logLineFor(e), ...prev].slice(0, 100));
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
      const msg = `[${label}] ${NODE_LABELS[`${device}-1`] || device} - ${command}: ${impact}`;
      setCmdLog((prev) => [msg, ...prev].slice(0, 100));
      setTimeout(refreshLive, 500);
    } catch (e) {
      setCmdLog((prev) => [logLineFor(e), ...prev].slice(0, 100));
    }
  };

  const markStepDone = (idx: number) => {
    const stepId = scenario.steps[idx]?.id;
    if (
      scenario.id === scope.curriculum?.roles.planning &&
      scenario.steps[idx]?.action?.type === "decision"
    ) {
      saveEmptyRemediationPlanIfMissing(scope, scenario.id);
    }
    if (stepId !== undefined) {
      setCompletedStepIds((prev) => new Set([...prev, stepId]));
      setSkippedStepIds((prev) => new Set([...prev].filter((id) => id !== stepId)));
    }
    if (idx < scenario.steps.length - 1) {
      setCurrentStep(idx + 1);
    }
  };

  const skipStep = () => {
    const stepId = scenario.steps[currentStep]?.id;
    if (stepId === undefined) return;
    setSkippedStepIds((prev) => new Set([...prev, stepId]));
    if (currentStep < scenario.steps.length - 1) setCurrentStep(currentStep + 1);
  };

  const step = scenario.steps[currentStep];
  const elec = state?.electrical;
  const progress = completedStepIds.size / scenario.steps.length;
  const hasAction = !!step?.action && step.action.type !== "decision" && step.action.type !== "manual";
  const stepDone = step !== undefined && completedStepIds.has(step.id);
  const showValidate = showsValidateButton(scenario, binding);
  const terminalTarget = step?.node
    || (step?.description ? splitDescription(step.description).flatMap((seg) => seg.type === "cmd" && seg.sourceNode && seg.sourceNode !== "host" ? [seg.sourceNode] : [])[0] : undefined)
    || inferNodeFromDescription(step?.description || "", binding)
    || exerciseNodes[0];
  const toggleTerminal = () => {
    if (!terminalOpened) {
      if (terminalTarget && exerciseNodes.includes(terminalTarget)) setActiveTerminalNode(terminalTarget);
      setTerminalOpened(true);
    }
    setShowTerminalPanel(!showTerminalPanel);
  };

  // Operational assessments
  const bkrClosed = elec?.breaker_closed ?? false;
  const rclClosed = elec?.recloser_closed ?? false;
  const critV = elec?.critical_load_voltage_v ?? 0;
  const totalKw = (elec?.general_load_kw ?? 0) + (elec?.critical_load_kw ?? 0);
  const customersServed = totalKw > 0;

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <p className="text-[10px] uppercase tracking-[0.3em] text-slate-600">
            Lab {scenario.order}
          </p>
          <h2 className="text-lg font-bold text-white">{scenario.name}</h2>
          <p className="mt-1 text-xs text-slate-400 max-w-2xl">{scenario.description}</p>
        </div>
        <div className="flex shrink-0 flex-wrap items-center gap-2">
          {(completedStepIds.size > 0 || skippedStepIds.size > 0) && (
            <Tooltip>
              <TooltipTrigger asChild>
                <button
                  onClick={() => setShowSummary(!showSummary)}
                  aria-label={showSummary ? "Back to Exercise" : "View Summary"}
                  className="inline-flex items-center justify-center rounded-sm border border-sky-700 bg-sky-950/40 px-3 py-1.5 text-xs text-sky-400 hover:bg-sky-900/50"
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
                  onClick={toggleTerminal}
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
              <TooltipContent>{showTerminalPanel ? `Hide terminal · ${NODE_LABELS[activeTerminalNode] || activeTerminalNode}` : `Show terminal · ${NODE_LABELS[terminalOpened ? activeTerminalNode : terminalTarget] || terminalTarget}`}</TooltipContent>
            </Tooltip>
          )}
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={handleLabReset}
                disabled={resettingLab}
                aria-label="Reset Lab"
                className="inline-flex items-center justify-center rounded-sm border border-amber-800/50 bg-amber-950/30 px-3 py-1.5 text-xs text-amber-500 hover:bg-amber-900/40 disabled:opacity-50"
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
                className="inline-flex items-center justify-center rounded-sm border border-slate-700 bg-slate-800/50 px-3 py-1.5 text-xs text-slate-500 hover:text-red-400"
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
                className="inline-flex items-center justify-center rounded-sm border border-slate-700 bg-slate-800/50 px-3 py-1.5 text-xs text-slate-400 hover:text-slate-200"
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
          {completedStepIds.size}/{scenario.steps.length} steps marked complete
          {skippedStepIds.size > 0 && ` · ${skippedStepIds.size} skipped`}
        </span>
      </div>
      <p className="text-[10px] text-slate-600">
        Mark Complete is self-attested; it does not run a system check.
      </p>

      {/* ── Remediation Plan Banner (shown on later exercises) ── */}
      {!showSummary && binding && <RemediationPlanBanner currentExerciseId={scenario.id} appliedPlan={appliedPlan} />}

      {/* ── Summary View ──────────────────────────────────────── */}
      {showSummary && (
        <ExerciseSummary
          scenario={scenario}
          completedStepIds={completedStepIds}
          skippedStepIds={skippedStepIds}
          notes={notes}
          activeConfig={activeConfig}
          policySource={policySource || undefined}
          appliedPlan={appliedPlan}
        />
      )}

      {/* ── Exercise View ─────────────────────────────────────── */}
      {!showSummary && <div className="grid min-w-0 gap-4 lg:grid-cols-[220px_minmax(0,1fr)]">
        <StepNavigator
          scenario={scenario}
          completedStepIds={completedStepIds}
          skippedStepIds={skippedStepIds}
          currentStep={currentStep}
          setCurrentStep={setCurrentStep}
          setStepResult={setStepResult}
        />

        {/* Right: Step detail + controls */}
        <div className="min-w-0 space-y-4">
          {/* Step instructions */}
          <div className="rounded-xl border border-slate-800 bg-slate-900/70 p-4">
            <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
              <h3 className="text-sm font-bold text-white">
                Step {currentStep + 1}: {step?.title}
              </h3>
              <div className="flex items-center gap-2">
                {!stepDone && !skippedStepIds.has(step?.id) && (
                  <button onClick={skipStep} className="rounded-sm border border-slate-700 px-2 py-1 text-[10px] text-slate-400 hover:text-slate-200">
                    Skip Step
                  </button>
                )}
                {hasAction && (
                  <button
                    onClick={() => handleExecuteStep(currentStep)}
                    disabled={executing}
                    className="rounded-sm border border-amber-700 bg-amber-950/40 px-3 py-1 text-[10px] font-medium text-amber-400 hover:bg-amber-900/50 disabled:opacity-50"
                  >
                    {executing ? "Executing..." : stepDone ? "Run again" : "Execute Step"}
                  </button>
                )}
                {!stepDone && (
                  <button onClick={() => markStepDone(currentStep)} className="rounded-sm border border-green-800 bg-green-950/40 px-2 py-1 text-[10px] font-medium text-green-400 hover:bg-green-900/50">
                    Mark Complete
                  </button>
                )}
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
              plan={appliedPlan}
              expectedConfig={step?.expected_config}
              policies={packagePolicies}
            />
            {/* Dynamic plan summary for Exercise 3 Phase 3 */}
            {scenario.id === binding?.roles.implementation && dynamicPlan &&
              step && binding?.dynamicSteps[step.id] === "rules" && (
              <div className="mb-3">
                <CompiledPlanSummary plan={dynamicPlan} binding={binding!} appliedPlan={appliedPlan} />
              </div>
            )}
            {compiled.error && <p role="alert" className="text-xs text-red-400">{compiled.error.message}</p>}
            {step?.description && (
              <DescriptionSegments
                scenarioId={scenario.id}
                currentStep={currentStep}
                stepTitle={step.title}
                stepId={step.id}
                stepDescription={step.description}
                dynamicPlan={dynamicPlan}
                exerciseNodes={exerciseNodes}
                autoRunning={autoRunning}
                handleAutoRun={handleAutoRun}
                handleGenerateTraffic={handleGenerateTraffic}
                generatingTraffic={generatingTraffic}
                targetNode={step.node || inferNodeFromDescription(step.description, binding) || binding?.nodeMap[scenario.id]?.primary || exerciseNodes[0]}
              />
            )}

            {/* Decision panel - only for steps with action.type === "decision" */}
            {step?.action?.type === "decision" && (
              <DecisionPanel exerciseId={scenario.id} />
            )}

            {/* Shared notes - persists across all steps */}
            <div className="mt-3">
              <label htmlFor="exercise-notes" className="block text-[9px] font-bold uppercase tracking-wider text-slate-400 mb-1">
                Exercise Notes
              </label>
              <textarea
                id="exercise-notes"
                value={notes}
                onChange={(e) => setNotes(e.target.value)}
                placeholder="Document your findings, observations, or answers here. Notes persist across all steps."
                className="w-full rounded-sm border border-slate-800 bg-slate-950 px-3 py-2 font-mono text-xs text-slate-300 placeholder-slate-400 focus:border-sky-700 focus:outline-hidden resize-y min-h-[200px]"
                rows={10}
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
                Execution Result — Step {stepResult.step_index + 1}: {stepResult.step_title} — {stepResult.action_type}
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
              {hasPolicyActions && (
                <PolicyCatchUpControls
                  key={`${scope.packageId}:${scope.revision}`}
                  activeConfig={activeConfig}
                  policySource={policySource}
                  plan={draftPlan}
                  error={draft.error?.message}
                  onApplied={() => { void refreshPolicyQueries(queryClient); void refreshLive(); }}
                />
              )}

              {/* Operational status: the US feeder from the RTAC, or the
                  operator status of another package. */}
              {usFeeder ? (
                <>
                  <div className="text-[10px] font-bold uppercase tracking-wider text-slate-500">Feeder Status</div>
                  {feederWaiting ? (
                    <div className="grid grid-cols-2 gap-2">
                      {["Customer Service", "Protection", "Voltage", "Critical Load"].map((label) => (
                        <MiniStatus key={label} label={label} value="WAITING" />
                      ))}
                    </div>
                  ) : (
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
                  )}
                </>
              ) : (
                <RunnerOperatorStatus resolution={operatorView} />
              )}
            </div>

            {/* Quick commands panel (US RTAC commands) */}
            {usFeeder && (
              <div className="rounded-xl border border-slate-800 bg-slate-900/70 p-4">
                <div className="text-[10px] font-bold uppercase tracking-wider text-slate-500 mb-2">
                  Supervisory Commands
                </div>
                <QuickCommands execCmd={execCmd} stepTitle={step?.title} />
              </div>
            )}
          </div>

          <div className="whitespace-pre-wrap break-words">
            <CommandAuditPanels cmdLog={cmdLog} setCmdLog={setCmdLog} recentAudit={recentAudit} />
          </div>

          {/* Validation - only on labs whose validator checks meaningful live
              state (see showsValidateButton). Hidden on planning/analysis
              labs where the validator can only report "lab is healthy". */}
          {showValidate && (
          <div className="flex flex-wrap items-center gap-3">
            <p className="w-full text-xs text-slate-400">Checks the running system against your change request (or the full reference if none). It does not grade your written test plan or test record.</p>
            <button
              onClick={handleValidate}
              disabled={validating}
              className="rounded-sm border border-sky-700 bg-sky-950/40 px-4 py-2 text-xs font-medium text-sky-400 transition-colors hover:bg-sky-900/50 disabled:opacity-50"
            >
              {validating ? "Validating..." : "Cross-check the current system"}
            </button>
            {validation && (
              <span className={`text-sm font-bold ${
                validation.outcome === "PASS" ? "text-green-400" : validation.outcome === "FAIL" ? "text-red-400" : "text-yellow-400"
              }`}>
                Last: {validation.outcome}
              </span>
            )}
          </div>
          )}

          {showValidate && validationError && (
            <p role="alert" className="text-xs text-red-400">Validation request failed. The last recorded result, if any, is unchanged.</p>
          )}
          {showValidate && validation && (
            <div className={`rounded-lg border p-3 ${
              validation.outcome === "PASS"
                ? "border-green-700 bg-green-950/40"
                : validation.outcome === "FAIL"
                ? "border-red-700 bg-red-950/40"
                : "border-yellow-700 bg-yellow-950/40"
            }`}>
              <p className="mb-2 text-[10px] text-slate-400">
                Last validation: {validation.outcome} · {validation.timestamp}. Historical result, not proof of current lab state or step completion.
              </p>
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
      {terminalOpened && exerciseNodes.length > 0 && (
        <div hidden={!showTerminalPanel || showSummary}>
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
        </div>
      )}
    </div>
  );
}
