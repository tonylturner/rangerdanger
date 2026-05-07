"use client";

import { useState, useEffect, useCallback, useRef } from "react";
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
} from "../lib/api";
import { getExerciseNodes, inferNodeFromDescription, NODE_LABELS, EXERCISE_NODE_MAP } from "../lib/exercise-nodes";
import { SharedTerminalPanel } from "./terminal-context";
import { NODE_UI_URLS } from "../lib/exercise-nodes";
import { Terminal as TerminalIcon, FileText, ArrowLeft, RotateCcw, Eraser, X, Lightbulb, ChevronDown, ChevronRight } from "lucide-react";
import { Tooltip, TooltipTrigger, TooltipContent } from "./ui/tooltip";
import { DecisionPanel } from "./decision-panel";
import { RemediationPlanBanner } from "./remediation-plan-banner";
import { MarkdownProse } from "./markdown-prose";
import {
  loadDynamicPlan,
  renderRuleTable,
  positiveValidationTests,
  negativeValidationTests,
  buildContaindConfig,
  type DynamicExercisePlan,
  type ActionSummary,
} from "../lib/remediation-to-rules";

const CMD_TOOL_RE = /^(nmap|mbpoll|dnp3poll|dnp3cmd|curl|tshark|tcpdump|nc|telnet|ssh|wget|ls|grep|cat|docker)\s/;

// :::hint Title  ...content...  :::
// Opens a collapsible hint panel in the rendered description. Anything
// between the opening and closing fence is captured as the hint body.
const HINT_OPEN_RE = /^:::hint(?:\s+(.+))?$/;
const HINT_CLOSE_RE = /^:::$/;

type Segment =
  | { type: "prose"; value: string }
  | { type: "cmd"; value: string }
  | { type: "hint"; title: string; value: string };

/** Split description into interleaved prose, command, and hint segments. */
function splitDescription(text: string): Segment[] {
  const result: Segment[] = [];
  const lines = text.split("\n");
  let prose: string[] = [];
  let i = 0;

  const flushProse = () => {
    if (prose.length > 0) {
      result.push({ type: "prose", value: prose.join("\n") });
      prose = [];
    }
  };

  while (i < lines.length) {
    const trimmed = lines[i].trim();
    const hintOpen = HINT_OPEN_RE.exec(trimmed);
    if (hintOpen) {
      flushProse();
      const title = hintOpen[1]?.trim() || "Reveal answer";
      const body: string[] = [];
      i++;
      while (i < lines.length && !HINT_CLOSE_RE.test(lines[i].trim())) {
        body.push(lines[i]);
        i++;
      }
      // Skip the closing fence
      if (i < lines.length) i++;
      result.push({ type: "hint", title, value: body.join("\n") });
      continue;
    }
    // A line is a command block only if it is indented (has leading
    // whitespace) AND starts with a recognized tool prefix. The indentation
    // requirement distinguishes a code snippet from a prose sentence that
    // happens to begin with a tool name, e.g. "tshark uses display filters".
    const isIndented = /^\s+\S/.test(lines[i]);
    if (isIndented && CMD_TOOL_RE.test(trimmed)) {
      flushProse();
      let cmd = trimmed;
      while (cmd.endsWith("\\") && i + 1 < lines.length) {
        i++;
        cmd += "\n  " + lines[i].trim();
      }
      result.push({ type: "cmd", value: cmd });
    } else {
      prose.push(lines[i]);
    }
    i++;
  }
  flushProse();
  return result;
}

// CommandBlock renders a single command line with copy + (optional)
// run buttons. Used in the main step body and inside HintBlock so
// commands look + behave the same in both places.
type CommandBlockProps = {
  cmd: string;
  runId: string;
  runningId: string | null;
  onRun: ((cmd: string, runId: string) => void) | null;
};

function CommandBlock({ cmd, runId, runningId, onRun }: CommandBlockProps) {
  return (
    <div className="group relative rounded border border-slate-700 bg-slate-950 px-3 py-2 font-mono text-[11px] text-amber-400">
      <span className="pr-24 whitespace-pre-wrap">{cmd}</span>
      <div className="absolute right-2 top-1.5 flex items-center gap-1.5">
        {onRun && (
          <button
            onClick={() => onRun(cmd, runId)}
            disabled={runningId !== null}
            className="rounded border border-green-800/60 bg-green-950/40 px-2 py-0.5 text-[9px] font-bold text-green-400 hover:bg-green-900/50 disabled:opacity-40 transition-colors"
          >
            {runningId === runId ? "Running..." : "Run"}
          </button>
        )}
        <button
          onClick={() => navigator.clipboard?.writeText(cmd)}
          className="text-[9px] text-slate-600 opacity-0 group-hover:opacity-100 transition-opacity hover:text-slate-400"
        >
          copy
        </button>
      </div>
    </div>
  );
}

// HintBlock is a collapsible "reveal answer" panel rendered inside step
// descriptions where the YAML contains a :::hint Title / ::: fence.
// Default state is collapsed — the student has to click to see the answer.
//
// Hint bodies are split the same way as step descriptions, so commands
// inside hints get the same copy + Run buttons (long tshark / nmap /
// curl invocations are particularly painful to select-and-copy without
// the affordance).
type HintBlockProps = {
  title: string;
  body: string;
  runIdPrefix: string;
  runningId: string | null;
  onRun: ((cmd: string, runId: string) => void) | null;
};

function HintBlock({ title, body, runIdPrefix, runningId, onRun }: HintBlockProps) {
  const [open, setOpen] = useState(false);
  const segments = splitDescription(body.replace(/^\n+|\n+$/g, ""));
  let cmdIdx = 0;
  return (
    <div className="rounded-lg border border-amber-900/60 bg-amber-950/20">
      <button
        onClick={() => setOpen(!open)}
        className="flex w-full items-center gap-2 px-3 py-2 text-left hover:bg-amber-950/40 transition-colors"
      >
        {open ? (
          <ChevronDown className="h-3.5 w-3.5 text-amber-500 shrink-0" />
        ) : (
          <ChevronRight className="h-3.5 w-3.5 text-amber-500 shrink-0" />
        )}
        <Lightbulb className="h-3.5 w-3.5 text-amber-500 shrink-0" />
        <span className="text-[11px] font-bold uppercase tracking-wider text-amber-400">
          {title}
        </span>
        {!open && (
          <span className="ml-auto text-[10px] text-amber-700 italic">
            click to reveal
          </span>
        )}
      </button>
      {open && (
        <div className="border-t border-amber-900/40 px-4 py-3 space-y-2">
          {segments.map((seg, si) => {
            if (seg.type === "prose") {
              const trimmed = seg.value.replace(/^\n+|\n+$/g, "");
              if (!trimmed) return null;
              return <MarkdownProse key={si}>{trimmed}</MarkdownProse>;
            }
            if (seg.type === "cmd") {
              const id = `${runIdPrefix}-${cmdIdx++}`;
              return (
                <CommandBlock
                  key={si}
                  cmd={seg.value}
                  runId={id}
                  runningId={runningId}
                  onRun={onRun}
                />
              );
            }
            // Nested hints aren't expected, but render them flat just
            // in case a YAML author does it — same context propagated.
            return (
              <HintBlock
                key={si}
                title={seg.title}
                body={seg.value}
                runIdPrefix={`${runIdPrefix}-h${si}`}
                runningId={runningId}
                onRun={onRun}
              />
            );
          })}
        </div>
      )}
    </div>
  );
}

// Plan summary panel rendered in Exercise 3 Phase 3 — shows selected and
// unselected remediation actions as styled cards rather than markdown text.
function RemediationPlanSummary({ plan }: { plan: DynamicExercisePlan }) {
  const [showUnselected, setShowUnselected] = useState(false);

  if (!plan.hasRemediationPlan) return null;

  return (
    <div className="space-y-3">
      <div className="rounded-lg border border-sky-800/50 bg-sky-950/20 p-4">
        <div className="text-[10px] font-bold uppercase tracking-wider text-sky-400 mb-3">
          Your Remediation Plan — {plan.selectedActions.length} actions selected
        </div>
        <div className="space-y-2">
          {plan.selectedSummary.map((a) => (
            <PlanActionCard key={a.id} action={a} selected />
          ))}
        </div>
        {plan.includeDpi && (
          <div className="mt-3 rounded border border-purple-800/40 bg-purple-950/20 px-3 py-2 text-[11px] text-purple-300">
            <span className="font-bold">ICS DPI enabled</span> — containd will filter by Modbus/DNP3 function code, not just port number
          </div>
        )}
        {plan.enableLogging && (
          <div className="mt-2 rounded border border-amber-800/40 bg-amber-950/20 px-3 py-2 text-[11px] text-amber-300">
            <span className="font-bold">Logging enabled</span> — set <code className="bg-slate-800 px-1 rounded text-[10px]">&quot;log&quot;: true</code> on DENY rules and critical ALLOW rules
          </div>
        )}
      </div>

      {plan.unselectedSummary.length > 0 && (
        <div className="rounded-lg border border-slate-800 bg-slate-900/30">
          <button
            onClick={() => setShowUnselected(!showUnselected)}
            className="flex w-full items-center justify-between px-4 py-2.5 hover:bg-slate-900/50 transition-colors"
          >
            <div className="flex items-center gap-2">
              <span className="text-[10px] font-bold uppercase tracking-wider text-slate-500">
                Not selected — {plan.unselectedSummary.length} actions deferred
              </span>
            </div>
            <span className="text-[10px] text-slate-600">{showUnselected ? "hide" : "show"}</span>
          </button>
          {showUnselected && (
            <div className="border-t border-slate-800 p-4 space-y-2">
              <div className="text-[10px] text-slate-500 mb-2">
                These actions were not in your plan. The attack paths they address remain open.
              </div>
              {plan.unselectedSummary.map((a) => (
                <PlanActionCard key={a.id} action={a} selected={false} />
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function PlanActionCard({ action, selected }: { action: ActionSummary; selected: boolean }) {
  return (
    <div className={`flex items-start gap-2.5 rounded border px-3 py-2 ${
      selected
        ? "border-sky-800/40 bg-sky-950/30"
        : "border-slate-800/40 bg-slate-950/30 opacity-60"
    }`}>
      <div className={`mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded text-[10px] font-bold ${
        selected ? "bg-sky-600 text-white" : "bg-slate-800 text-slate-500"
      }`}>
        {selected ? "✓" : "—"}
      </div>
      <div className="min-w-0">
        <div className={`text-[11px] font-bold ${selected ? "text-sky-300" : "text-slate-500"}`}>
          {action.title}
        </div>
        <div className="text-[10px] text-slate-500 mt-0.5">{action.description}</div>
        {selected && action.hasRules && (
          <div className="text-[9px] text-sky-600 mt-0.5">→ produces firewall rules below</div>
        )}
        {selected && !action.hasRules && (
          <div className="text-[9px] text-slate-600 mt-0.5">configuration / process change (no firewall rule)</div>
        )}
      </div>
    </div>
  );
}

// Step title fragments used to identify dynamic phases in firewall-implementation.
const PHASE3_TITLES = ["create minimal", "phase 3"];
const PHASE5_TITLES = ["validate allowed", "phase 5"];
const PHASE6_TITLES = ["validate blocked", "phase 6"];

function titleMatches(title: string, fragments: string[]): boolean {
  const lower = title.toLowerCase();
  return fragments.some((f) => lower.includes(f));
}

// For firewall-implementation, generate full step descriptions from the
// student's remediation plan. Uses step titles (stable) rather than
// regex matching inside description text (fragile).
function injectDynamicContent(
  desc: string,
  stepTitle: string,
  plan: DynamicExercisePlan | null,
): string {
  if (!plan) return desc;

  if (titleMatches(stepTitle, PHASE3_TITLES)) {
    const ruleTable = renderRuleTable(plan);

    const noPlan = !plan.hasRemediationPlan
      ? `You have not completed a remediation plan yet. The rules below are the minimum baseline — RTAC and GPS access to field devices. Consider going back to the [Remediation Planning](/exercises/remediation-planning) exercise to build a plan that drives additional rules.\n\n`
      : "";

    return `Now create the firewall rules your remediation plan requires. Everything not explicitly allowed stays denied.

${noPlan}### Firewall Rules to Implement

${ruleTable}

> **Critical:** RTAC rules are source-pinned to \`10.30.30.20\`. Do **not** use a broad OT Ops subnet rule. If the HMI or OpenPLC is compromised, it must not reach field devices directly.

Create these rules in the containd firewall. Use either the web UI or the CLI.

:::hint Creating rules via the CLI
From the firewall terminal, you can push rules via the containd API:

    curl -X POST http://localhost:8080/api/v1/policies \\
      -H 'Content-Type: application/json' \\
      -d '{"src":"10.30.30.20/32","dst":"10.40.40.0/24","proto":"tcp","dport":502,"action":"allow","description":"RTAC to field Modbus"}'

Repeat for each rule, changing the source, destination, port, and description as shown in the table above.
:::`;
  }

  if (titleMatches(stepTitle, PHASE5_TITLES)) {
    const tests = positiveValidationTests(plan);
    return `Your rules are in place. Now verify that operations still work. If any of these tests fail, you have a rule that is too restrictive or missing.

**Positive validation commands (based on your plan):**

${tests.join("\n\n")}

If any test fails, check your rules. The most common mistake is forgetting to allow a required flow, or pinning the source too narrowly on the GPS rule.`;
  }

  if (titleMatches(stepTitle, PHASE6_TITLES)) {
    const tests = negativeValidationTests(plan);
    return `Now verify that unauthorized traffic is denied. Every test below should fail with a timeout or connection refused.

**Negative validation commands (based on your plan):**

${tests.join("\n\n")}

After each failed attempt, check the containd event log to confirm the deny was logged:

    curl -s http://localhost:8080/api/v1/events?limit=5 | python3 -m json.tool

If any test succeeds when it should fail, you have a rule that is too permissive. Review your policy and tighten it.`;
  }

  return desc;
}

type RunnerProps = {
  scenario: Scenario;
  onExit: () => void;
};

// ── LocalStorage persistence helpers ──────────────────────────────
function storageKey(scenarioId: string) { return `rd-exercise-${scenarioId}`; }

type SavedState = {
  completedSteps: number[];
  notes: string;
  cmdLog: string[];
};

function loadSaved(scenarioId: string): SavedState {
  try {
    const raw = localStorage.getItem(storageKey(scenarioId));
    if (raw) {
      const parsed = JSON.parse(raw);
      // Migrate from per-step notes (Record<number, string>) to single shared note
      if (parsed.notes && typeof parsed.notes === "object" && !Array.isArray(parsed.notes)) {
        const merged = Object.values(parsed.notes as Record<string, string>).filter(Boolean).join("\n\n");
        return { ...parsed, notes: merged };
      }
      return parsed;
    }
  } catch { /* ignore */ }
  return { completedSteps: [], notes: "", cmdLog: [] };
}

function saveToDisk(scenarioId: string, state: SavedState) {
  try { localStorage.setItem(storageKey(scenarioId), JSON.stringify(state)); } catch { /* ignore */ }
}

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
  const [cmdLog, setCmdLog] = useState<string[]>(saved.cmdLog || []);
  const [executing, setExecuting] = useState(false);
  // String IDs (e.g. "body-0", "hint-0-1") so body + hint commands can
  // each have copy/run buttons without index collisions.
  const [autoRunning, setAutoRunning] = useState<string | null>(null);
  const [resettingLab, setResettingLab] = useState(false);
  const [stepResult, setStepResult] = useState<StepExecutionResult | null>(null);
  const [recentAudit, setRecentAudit] = useState<AuditEntry[]>([]);
  const exerciseNodes = getExerciseNodes(scenario.id, scenario.nodes);
  const [dynamicPlan, setDynamicPlan] = useState<DynamicExercisePlan | null>(() =>
    scenario.id === "firewall-implementation" ? loadDynamicPlan() : null
  );
  useEffect(() => {
    if (scenario.id !== "firewall-implementation") return;
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
      setCmdLog((prev) => [`[ERROR] No target node for command — open terminal and run manually`, ...prev].slice(0, 100));
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
      setCmdLog((prev) => [`[TRAFFIC] Generation started — ${durationSec}s of Modbus, DNP3, HTTP, NTP flows`, ...prev].slice(0, 100));
      // Poll for completion
      const pollId = setInterval(async () => {
        try {
          const status = await getTrafficStatus();
          if (!status.generating) {
            clearInterval(pollId);
            setGeneratingTraffic(false);
            setCmdLog((prev) => [`[TRAFFIC] Complete — ${status.flows_generated || 0} flows generated`, ...prev].slice(0, 100));
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
            setCmdLog((prev) => [`[CAPTURE] Complete — download at ${url}`, ...prev].slice(0, 100));
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
            {/* Config mismatch warning */}
            {step?.expected_config && activeConfig && (() => {
              const expected = step.expected_config;
              const actual = activeConfig;
              const match = expected === "weak"
                ? actual === "weak"
                : expected === "hardened"
                ? actual === "improved" || actual === "custom"
                : true;
              if (match) return null;
              const needLabel = expected === "weak" ? "weak baseline" : "hardened (improved or custom)";
              const hasLabel = actual === "weak" ? "weak baseline" : actual;
              const action = expected === "weak"
                ? "Use the Reset to Weak button to restore the weak baseline."
                : "Use the Apply Hardened or Apply Your Plan button to apply a hardened policy.";
              return (
                <div className="rounded-lg border border-amber-800/50 bg-amber-950/20 px-4 py-3 mb-3">
                  <div className="text-[11px] text-amber-400">
                    <span className="font-bold">Config mismatch:</span> This step expects the <span className="font-bold">{needLabel}</span> config,
                    but the firewall is currently on <span className="font-bold">{hasLabel}</span>. {action}
                  </div>
                </div>
              );
            })()}
            {/* Dynamic plan summary for Exercise 3 Phase 3 */}
            {scenario.id === "firewall-implementation" && dynamicPlan?.hasRemediationPlan &&
              step && titleMatches(step.title, PHASE3_TITLES) && (
              <div className="mb-3">
                <RemediationPlanSummary plan={dynamicPlan} />
              </div>
            )}
            {/* Description with inline command blocks and hint panels */}
            {step?.description && (() => {
              const effectiveDesc = scenario.id === "firewall-implementation" && dynamicPlan
                ? injectDynamicContent(step.description, step.title, dynamicPlan)
                : step.description;
              const segments = splitDescription(effectiveDesc);
              let bodyCmdIdx = 0;
              let hintIdx = 0;
              const runHandler = exerciseNodes.length > 0
                ? (cmd: string, runId: string) => handleAutoRun(cmd, runId, step.description)
                : null;
              return (
                <div className="space-y-2">
                  {segments.map((seg, si) => {
                    if (seg.type === "prose") {
                      const trimmed = seg.value.replace(/^\n+|\n+$/g, "");
                      if (!trimmed) return null;
                      return (
                        <div key={si} className="space-y-2">
                          <MarkdownProse>{trimmed}</MarkdownProse>
                        </div>
                      );
                    }
                    if (seg.type === "hint") {
                      const hi = hintIdx++;
                      return (
                        <HintBlock
                          key={`${scenario.id}-${currentStep}-${si}`}
                          title={seg.title}
                          body={seg.value}
                          runIdPrefix={`hint-${hi}`}
                          runningId={autoRunning}
                          onRun={runHandler}
                        />
                      );
                    }
                    const ci = bodyCmdIdx++;
                    return (
                      <CommandBlock
                        key={si}
                        cmd={seg.value}
                        runId={`body-${ci}`}
                        runningId={autoRunning}
                        onRun={runHandler}
                      />
                    );
                  })}
                </div>
              );
            })()}

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

            {/* Decision panel — only for steps with action.type === "decision" */}
            {step?.action?.type === "decision" && (
              <DecisionPanel exerciseId={scenario.id} action={step.action} />
            )}

            {/* Shared notes — persists across all steps */}
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
                  {scenario.id === "firewall-implementation" && dynamicPlan?.hasRemediationPlan && (
                    <button
                      onClick={async () => {
                        const config = buildContaindConfig(dynamicPlan);
                        await applyCustomFirewallConfig(config);
                        pollState();
                        setCmdLog((prev) => [`[APPLIED] Your remediation plan config pushed to containd`, ...prev].slice(0, 100));
                      }}
                      className="rounded border border-sky-700 bg-sky-950/40 px-2 py-1 text-[10px] text-sky-400 hover:bg-sky-900/50"
                    >
                      Apply Your Plan
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

          {/* Command log — persistent, scrollable */}
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

          {/* Validation */}
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
      </div>}

      {/* ── Node Terminal/UI Panel ─────────────────────────────── */}
      {showTerminalPanel && exerciseNodes.length > 0 && !showSummary && (
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
      )}
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

// ── Exercise Summary Component ───────────────────────────────────

function ExerciseSummary({
  scenario,
  completedSteps,
  notes,
  activeConfig,
}: {
  scenario: Scenario;
  completedSteps: Set<number>;
  notes: string;
  activeConfig: string | null;
}) {
  const completionPct = Math.round((completedSteps.size / scenario.steps.length) * 100);
  const timestamp = new Date().toLocaleString();

  const summaryText = [
    `Lab ${scenario.order ?? ""}: ${scenario.name}`,
    `Completed: ${completedSteps.size}/${scenario.steps.length} steps (${completionPct}%)`,
    `Firewall Config: ${activeConfig || "unknown"}`,
    `Date: ${timestamp}`,
    "",
    ...scenario.steps.map((s, i) => {
      const done = completedSteps.has(i) ? "[x]" : "[ ]";
      return `${done} Step ${i + 1}: ${s.title}`;
    }),
    ...(notes ? ["", "Notes:", notes] : []),
  ].join("\n");

  return (
    <div className="space-y-4">
      <div className="rounded-xl border border-slate-800 bg-slate-900/70 p-5">
        <div className="flex items-center justify-between mb-4">
          <h3 className="text-sm font-bold text-white">Exercise Summary</h3>
          <div className="flex items-center gap-2">
            <button
              onClick={() => navigator.clipboard?.writeText(summaryText)}
              className="rounded border border-slate-700 bg-slate-800/50 px-3 py-1 text-[10px] text-slate-400 hover:text-slate-200"
            >
              Copy to Clipboard
            </button>
          </div>
        </div>

        <div className="grid gap-3 md:grid-cols-3 mb-4">
          <div className="rounded border border-slate-800 bg-slate-950 p-3">
            <div className="text-[9px] uppercase tracking-wider text-slate-600">Completion</div>
            <div className={`text-lg font-bold ${completionPct === 100 ? "text-green-400" : "text-sky-400"}`}>
              {completionPct}%
            </div>
          </div>
          <div className="rounded border border-slate-800 bg-slate-950 p-3">
            <div className="text-[9px] uppercase tracking-wider text-slate-600">Firewall Config</div>
            <div className={`text-lg font-bold ${activeConfig === "improved" ? "text-green-400" : "text-red-400"}`}>
              {activeConfig || "—"}
            </div>
          </div>
          <div className="rounded border border-slate-800 bg-slate-950 p-3">
            <div className="text-[9px] uppercase tracking-wider text-slate-600">Notes</div>
            <div className="text-lg font-bold text-slate-300">
              {notes ? `${notes.split("\n").length} lines` : "—"}
            </div>
          </div>
        </div>

        <div className="space-y-2">
          {scenario.steps.map((s, i) => {
            const done = completedSteps.has(i);
            return (
              <div key={i} className={`rounded border p-3 ${done ? "border-green-800/50 bg-green-950/10" : "border-slate-800 bg-slate-950/50"}`}>
                <div className="flex items-center gap-2">
                  <span className={`text-xs font-bold ${done ? "text-green-400" : "text-slate-600"}`}>
                    {done ? "\u2713" : "\u2022"}
                  </span>
                  <span className={`text-xs font-medium ${done ? "text-slate-300" : "text-slate-500"}`}>
                    Step {i + 1}: {s.title}
                  </span>
                </div>
              </div>
            );
          })}
          {notes && (
            <div className="rounded border border-slate-800 bg-slate-950/50 p-3 mt-2">
              <div className="text-[9px] font-bold uppercase tracking-wider text-slate-600 mb-1">Exercise Notes</div>
              <div className="text-xs text-slate-400 whitespace-pre-line">{notes}</div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function isSegmentationStep(title?: string): boolean {
  if (!title) return false;
  return title.toLowerCase().includes("improve") || title.toLowerCase().includes("segmentation");
}

function isBaselineStep(title?: string): boolean {
  if (!title) return false;
  return title.toLowerCase().includes("baseline") || title.toLowerCase().includes("observe");
}
