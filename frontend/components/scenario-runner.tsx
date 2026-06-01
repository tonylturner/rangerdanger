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
  type PolicySource,
} from "../lib/api";
import { PolicyStatusBanner } from "./policy-status-banner";
import { getExerciseNodes, inferNodeFromDescription, NODE_LABELS, EXERCISE_NODE_MAP } from "../lib/exercise-nodes";
import { SharedTerminalPanel } from "./terminal-context";
import { NODE_UI_URLS } from "../lib/exercise-nodes";
import { Terminal as TerminalIcon, FileText, ArrowLeft, RotateCcw, Eraser, X, Lightbulb, ChevronDown, ChevronRight, AlertCircle, CircleCheck, CircleDashed, Shield, Activity, Network, BookOpen } from "lucide-react";
import {
  readRequirements,
  computeCoverage,
  summariseCoverage,
} from "../lib/requirement-coverage";
import { loadRemediationPlan } from "../lib/remediation-plan";
import { Tooltip, TooltipTrigger, TooltipContent } from "./ui/tooltip";
import { DecisionPanel } from "./decision-panel";
import { RemediationPlanBanner } from "./remediation-plan-banner";
import { MarkdownProse } from "./markdown-prose";
import { TrackPicker } from "./track-picker";
import { useFirewallTrack } from "../lib/use-firewall-track";
import {
  loadDynamicPlan,
  renderRuleTable,
  positiveValidationTests,
  negativeValidationTests,
  buildContaindConfig,
  type DynamicExercisePlan,
  type ActionSummary,
} from "../lib/remediation-to-rules";

// Parser logic lives in lib/scenario-description.ts so it can be
// unit-tested without React. See tests next to that module.
import {
  splitDescription,
  type Segment,
  type FindingsPanelItem,
} from "../lib/scenario-description";
import { decisionStorageKey } from "../lib/decision-storage";

// CommandBlock renders a single command line with copy + (optional)
// run buttons. Used in the main step body and inside HintBlock so
// commands look + behave the same in both places.
type CommandBlockProps = {
  cmd: string;
  runId: string;
  runningId: string | null;
  onRun: ((cmd: string, runId: string) => void) | null;
  // cli = a containd appliance-CLI command: copy-only (no Run), prefixed
  // with the containd# prompt and badged so it's clear the student runs
  // it in the fw-1 containd terminal, not via the lab's per-node exec.
  cli?: boolean;
};

function CommandBlock({ cmd, runId, runningId, onRun, cli }: CommandBlockProps) {
  const runnable = cli ? null : onRun;
  return (
    <div className="group relative rounded border border-slate-700 bg-slate-950 px-3 py-2 font-mono text-[11px] text-amber-400">
      <span className="pr-28 whitespace-pre-wrap">
        {cli && <span className="select-none text-sky-700">containd# </span>}
        {cmd}
      </span>
      <div className="absolute right-2 top-1.5 flex items-center gap-1.5">
        {cli && (
          <span className="select-none rounded border border-sky-800/60 bg-sky-950/40 px-1.5 py-0.5 text-[8px] font-bold uppercase tracking-wider text-sky-400">
            containd cli
          </span>
        )}
        {runnable && (
          <button
            onClick={() => runnable(cmd, runId)}
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

// DecisionBlock renders a question + dropdown for student-facing
// "what would you do here?" prompts. The selected value is persisted
// to localStorage so refreshes don't lose progress AND so later labs
// (1.3 / 1.4) can read the student's earlier decisions and tailor
// their content accordingly.
//
// Storage key: `decision:<scenario.id>:<decisionId>`. The step title
// is intentionally NOT in the key so a renamed step doesn't orphan
// the answer; uniqueness comes from the decision id chosen by the
// YAML author (e.g. "enterprise-to-field").
type DecisionBlockProps = {
  scenarioId: string;
  decisionId: string;
  options: string[];
  body: string;
  /** "<scenarioId>:<decisionId>" — initial value source if local key empty */
  defaultFrom?: string;
  /**
   * Known-correct answer for this decision. When set, the block
   * renders a green/red feedback chip after the student picks. Only
   * use on observation/factual prompts (e.g. lab 1.2 step 6 — the
   * weak baseline really did show every exposure). Leave unset on
   * judgment-call decisions (lab 1.3 verdicts) where no single
   * answer is "right."
   */
  correct?: string;
};

function DecisionBlock({ scenarioId, decisionId, options, body, defaultFrom, correct }: DecisionBlockProps) {
  const storageKey = decisionStorageKey(scenarioId, decisionId);
  const [value, setValue] = useState<string>("");
  const [inheritedFrom, setInheritedFrom] = useState<string>("");

  useEffect(() => {
    try {
      const saved = window.localStorage.getItem(storageKey);
      if (saved && options.includes(saved)) {
        setValue(saved);
        return;
      }
      // No saved value — try to inherit from the upstream decision.
      if (defaultFrom && defaultFrom.includes(":")) {
        const [srcScenario, srcId] = defaultFrom.split(":", 2);
        const srcKey = decisionStorageKey(srcScenario, srcId);
        const srcVal = window.localStorage.getItem(srcKey);
        if (srcVal && options.includes(srcVal)) {
          setValue(srcVal);
          setInheritedFrom(defaultFrom);
          // Persist the inherited value so subsequent changes are
          // tracked against THIS decision's storage key, not the
          // upstream one. The student is now committing this verdict
          // for this lab specifically.
          window.localStorage.setItem(storageKey, srcVal);
        }
      }
    } catch {
      /* localStorage unavailable — fall back to in-memory state */
    }
  }, [storageKey, options, defaultFrom]);

  const onChange = (next: string) => {
    setValue(next);
    setInheritedFrom("");   // student touched the dropdown — no longer "inherited"
    try {
      if (next) {
        window.localStorage.setItem(storageKey, next);
      } else {
        window.localStorage.removeItem(storageKey);
      }
    } catch {
      /* swallow — UI still works without persistence */
    }
  };

  const answered = value !== "";

  return (
    <div className={`rounded-lg border px-4 py-3 transition-colors ${
      answered
        ? "border-sky-800/50 bg-sky-950/20"
        : "border-slate-700 bg-slate-900/40"
    }`}>
      <div className="flex items-start gap-3">
        <div className={`mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded-full text-[10px] font-bold ${
          answered ? "bg-sky-600 text-white" : "bg-slate-700 text-slate-300"
        }`}>
          {answered ? "✓" : "?"}
        </div>
        <div className="min-w-0 flex-1">
          <div className="text-sm text-slate-200">
            <MarkdownProse>{body.replace(/^\n+|\n+$/g, "")}</MarkdownProse>
          </div>
          <div className="mt-2 flex items-center gap-2 flex-wrap">
            <label
              htmlFor={`decision-${decisionId}`}
              className="text-[10px] font-bold uppercase tracking-wider text-slate-400"
            >
              Your decision:
            </label>
            <div className="relative">
              <select
                id={`decision-${decisionId}`}
                value={value}
                onChange={(e) => onChange(e.target.value)}
                className={`appearance-none rounded border bg-slate-950 px-2 py-1 pr-7 text-[11px] font-mono cursor-pointer focus:outline-none focus:ring-1 focus:ring-cyan-700 ${
                  answered
                    ? "border-sky-700 text-sky-300"
                    : "border-slate-700 text-slate-400 hover:border-slate-600"
                }`}
                style={{ colorScheme: "dark" }}
              >
                <option value="" className="bg-slate-900 text-slate-400">Select...</option>
                {options.map((opt) => (
                  <option key={opt} value={opt} className="bg-slate-900 text-slate-200">
                    {opt}
                  </option>
                ))}
              </select>
              <ChevronDown
                className={`pointer-events-none absolute right-1.5 top-1/2 -translate-y-1/2 h-3 w-3 ${
                  answered ? "text-sky-500" : "text-slate-500"
                }`}
              />
            </div>
            {answered && correct && (
              value === correct ? (
                <span
                  className="rounded border border-emerald-700/60 bg-emerald-950/40 px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wider text-emerald-300"
                  title="Your finding matches what the baseline traffic actually shows."
                >
                  ✓ correct
                </span>
              ) : (
                <span
                  className="rounded border border-red-800/60 bg-red-950/40 px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wider text-red-300"
                  title={`The baseline capture shows this category as ${correct}. Re-check your analysis.`}
                >
                  ✗ doesn&apos;t match capture
                </span>
              )
            )}
            {inheritedFrom && (
              <span className="text-[10px] text-slate-500 italic">
                inherited from earlier lab — adjust if your design has changed
              </span>
            )}
            {answered && !inheritedFrom && (
              <button
                onClick={() => onChange("")}
                className="text-[10px] text-slate-500 hover:text-slate-300 underline underline-offset-2"
              >
                clear
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

// FindingsPanel renders read-only cards for a set of upstream
// decisions (e.g. Lab 1.3 showing the student's Lab 1.2 findings).
// Reads each id's localStorage value via the same key shape
// DecisionBlock writes. Quiet on the empty case — if the upstream
// lab wasn't done, the panel says so and links back.
type FindingsPanelProps = {
  sourceScenario: string;
  title: string;
  items: FindingsPanelItem[];
};

function FindingsPanel({ sourceScenario, title, items }: FindingsPanelProps) {
  const [values, setValues] = useState<Record<string, string>>({});
  useEffect(() => {
    try {
      const out: Record<string, string> = {};
      for (const it of items) {
        out[it.id] = window.localStorage.getItem(decisionStorageKey(sourceScenario, it.id)) ?? "";
      }
      setValues(out);
    } catch {
      /* localStorage blocked */
    }
  }, [sourceScenario, items]);

  const anySet = items.some((it) => values[it.id]);

  return (
    <div className="rounded-lg border border-cyan-900/40 bg-cyan-950/10 p-3">
      <div className="text-[10px] font-bold uppercase tracking-wider text-cyan-400 mb-2">
        {title}
      </div>
      {anySet ? (
        <div className="grid gap-1.5">
          {items.map((it) => {
            const v = values[it.id] ?? "";
            return (
              <div
                key={it.id}
                className="flex items-baseline justify-between gap-3 text-[11px]"
              >
                <span className="text-slate-300 truncate">{it.label}</span>
                <span
                  className={`font-mono shrink-0 ${
                    v ? "text-cyan-300" : "text-slate-600 italic"
                  }`}
                >
                  {v || "not recorded"}
                </span>
              </div>
            );
          })}
        </div>
      ) : (
        <div className="text-[11px] text-slate-500 italic">
          You haven&apos;t recorded findings in <a
            href={`/exercises/${sourceScenario}`}
            className="text-cyan-500 hover:text-cyan-300 underline underline-offset-2"
          >Lab {sourceScenario}</a> yet — your design verdicts below will be made without that context.
        </div>
      )}
    </div>
  );
}

// PlanCoveragePanel renders the student's Lab 1.4 plan coverage —
// the same per-requirement breakdown the DecisionPanel sticky bar
// shows, but inline at any point in a description. Used in Lab 2.4
// to surface "what did your plan close vs defer" without making the
// student manually recall their selections.
function PlanCoveragePanel({ title }: { title: string }) {
  const [snapshot, setSnapshot] = useState<{
    hasPlan: boolean;
    coverage: ReturnType<typeof computeCoverage>;
    summary: ReturnType<typeof summariseCoverage>;
  }>({
    hasPlan: false,
    coverage: [],
    summary: { total: 0, covered: 0, partial: 0, gap: 0, na: 0 },
  });

  useEffect(() => {
    const requirements = readRequirements();
    const plan = loadRemediationPlan();
    const selected = new Set(plan?.selectedActionIds ?? []);
    const coverage = computeCoverage(requirements, selected);
    const summary = summariseCoverage(coverage);
    setSnapshot({
      hasPlan: !!plan && plan.selectedActionIds.length > 0,
      coverage,
      summary,
    });
  }, []);

  if (!snapshot.hasPlan && snapshot.coverage.length === 0) {
    return (
      <div className="rounded-lg border border-slate-700 bg-slate-950 p-3 text-[11px] text-slate-500 italic">
        No remediation plan recorded yet. Visit <a
          href="/exercises/remediation-planning"
          className="text-cyan-500 hover:text-cyan-300 underline underline-offset-2"
        >Lab 1.4 (Remediation Planning)</a> to select actions and your plan coverage will appear here.
      </div>
    );
  }

  const { coverage, summary } = snapshot;
  const allClosed = summary.covered === summary.total - summary.na && summary.gap === 0 && summary.partial === 0;

  return (
    <div className="rounded-lg border border-slate-700 bg-slate-950 p-3">
      <div className="flex items-baseline justify-between mb-2">
        <div className="text-[10px] font-bold uppercase tracking-wider text-slate-500">
          {title}
        </div>
        <div className="text-[11px] font-mono">
          <span className={allClosed ? "text-emerald-400 font-bold" : "text-slate-200 font-bold"}>
            {summary.covered}
          </span>
          <span className="text-slate-500"> / {summary.total - summary.na} covered</span>
          {summary.partial > 0 && (
            <span className="text-amber-400"> · {summary.partial} partial</span>
          )}
          {summary.gap > 0 && (
            <span className="text-red-400"> · {summary.gap} gap</span>
          )}
        </div>
      </div>
      <div className="space-y-1.5">
        {coverage.map((c) => {
          if (c.status === "n/a") {
            return (
              <div key={c.req.id} className="flex items-start gap-2 text-[11px]">
                <CircleDashed className="h-3.5 w-3.5 mt-0.5 text-slate-600 shrink-0" />
                <span className="text-slate-500">
                  <span className="font-bold text-slate-400">{c.req.label}</span>
                  <span className="text-slate-600"> — {c.reason}</span>
                </span>
              </div>
            );
          }
          if (c.status === "covered") {
            return (
              <div key={c.req.id} className="flex items-start gap-2 text-[11px]">
                <CircleCheck className="h-3.5 w-3.5 mt-0.5 text-emerald-400 shrink-0" />
                <span className="text-slate-300">
                  <span className="font-bold">{c.req.label}</span>
                  <span className="text-slate-500"> — {c.req.verdict}, fully addressed by your plan</span>
                </span>
              </div>
            );
          }
          if (c.status === "partial") {
            return (
              <div key={c.req.id} className="flex items-start gap-2 text-[11px]">
                <AlertCircle className="h-3.5 w-3.5 mt-0.5 text-amber-400 shrink-0" />
                <span className="text-slate-300">
                  <span className="font-bold">{c.req.label}</span>
                  <span className="text-slate-500"> — {c.req.verdict}, partial coverage. Missing: </span>
                  <span className="font-mono text-amber-400">{c.missingActions.join(", ")}</span>
                </span>
              </div>
            );
          }
          // gap — deferred or never selected
          return (
            <div key={c.req.id} className="flex items-start gap-2 text-[11px]">
              <AlertCircle className="h-3.5 w-3.5 mt-0.5 text-red-400 shrink-0" />
              <span className="text-slate-300">
                <span className="font-bold">{c.req.label}</span>
                <span className="text-slate-500"> — {c.req.verdict}, deferred. Implementing actions: </span>
                <span className="font-mono text-red-400">{c.expectedActions.join(" or ")}</span>
              </span>
            </div>
          );
        })}
      </div>
      <div className="mt-2 text-[10px] text-slate-500 italic">
        Read-only snapshot of your <a
          href="/exercises/remediation-planning"
          className="text-cyan-500 hover:text-cyan-300 underline underline-offset-2"
        >Lab 1.4 plan</a>. Edit selections there to update.
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
  scenarioId: string;
};

// IconStamp - renders a lucide-react icon styled like the actual UI
// button the lab text is referring the student to. Used by the
// :::icon directive as a visual cue (e.g. "click this in the left
// strip" alongside a mini-Shield in the same color the real
// strip uses).
const ICON_MAP: Record<string, typeof Shield> = {
  shield: Shield,
  activity: Activity,
  network: Network,
  book: BookOpen,
  terminal: TerminalIcon,
};
const ICON_COLORS: Record<string, { text: string; border: string; bg: string }> = {
  amber:   { text: "text-amber-400",   border: "border-amber-700/60",   bg: "bg-amber-950/30" },
  cyan:    { text: "text-cyan-400",    border: "border-cyan-700/60",    bg: "bg-cyan-950/30" },
  emerald: { text: "text-emerald-400", border: "border-emerald-700/60", bg: "bg-emerald-950/30" },
  sky:     { text: "text-sky-400",     border: "border-sky-700/60",     bg: "bg-sky-950/30" },
  rose:    { text: "text-rose-400",    border: "border-rose-700/60",    bg: "bg-rose-950/30" },
  slate:   { text: "text-slate-400",   border: "border-slate-700",      bg: "bg-slate-900/60" },
};
function IconStamp({ name, color, label }: { name: string; color: string; label: string }) {
  const Icon = ICON_MAP[name] ?? Shield;
  const c = ICON_COLORS[color] ?? ICON_COLORS.amber;
  return (
    <span className={`inline-flex items-center gap-1.5 rounded border px-2 py-1 align-middle ${c.border} ${c.bg}`}>
      <Icon className={`h-3.5 w-3.5 ${c.text}`} />
      {label && <span className={`text-[11px] font-medium ${c.text}`}>{label}</span>}
    </span>
  );
}

function HintBlock({ title, body, runIdPrefix, runningId, onRun, scenarioId }: HintBlockProps) {
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
                  cli={seg.cli}
                />
              );
            }
            if (seg.type === "decision") {
              return (
                <DecisionBlock
                  key={si}
                  scenarioId={scenarioId}
                  decisionId={seg.id}
                  options={seg.options}
                  body={seg.body}
                  defaultFrom={seg.defaultFrom}
                  correct={seg.correct}
                />
              );
            }
            if (seg.type === "findingsPanel") {
              return (
                <FindingsPanel
                  key={si}
                  sourceScenario={seg.sourceScenario}
                  title={seg.title}
                  items={seg.items}
                />
              );
            }
            if (seg.type === "planCoverage") {
              return <PlanCoveragePanel key={si} title={seg.title} />;
            }
            // trackPicker / trackOnly inside a :::hint isn't a
            // pattern any lab uses today; skip silently rather than
            // render a broken sub-tree.
            if (seg.type === "trackPicker" || seg.type === "trackOnly" || seg.type === "generateTrafficButton" || seg.type === "icon") {
              return null;
            }
            if (seg.type === "hint") {
              // Nested hints aren't expected, but render them flat
              // just in case a YAML author does it.
              return (
                <HintBlock
                  key={si}
                  title={seg.title}
                  body={seg.value}
                  runIdPrefix={`${runIdPrefix}-h${si}`}
                  runningId={runningId}
                  onRun={onRun}
                  scenarioId={scenarioId}
                />
              );
            }
            return null;
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

// Scenarios whose steps actually exercise the firewall — the policy
// action buttons (Apply Hardened / Apply Your Plan / Reset to Weak)
// render here. The planning labs (1.2 baseline, 1.3 requirements,
// 1.4 plan) are intentionally excluded because policy state changes
// during their steps would not match their student-facing narrative.
const POLICY_ACTION_SCENARIOS = [
  "firewall-implementation",     // Lab 2.2
  "hardening-configurations",    // Lab 2.3
  "vendor-rdp-compromise",       // Lab 2.3-bonus
  "validation-evidence",         // Lab 2.4
];

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

    // Phase 3's generated description has to re-emit the :::guided
    // / :::technical fork itself because injectDynamicContent
    // entirely replaces the YAML for this step (the YAML edits
    // wouldn't survive otherwise). Same intent as the static
    // forks in 2.3 step 4 / 2.2 step 7 — guided students walk
    // the table to see what landed; technical students treat the
    // table as the spec they're authoring against.
    return `The six rules below are the contract for what cross-zone
traffic the substation must allow. Everything else stays denied.

${noPlan}:::guided
Click **Apply Hardened** in the side panel to push the canned
reference, or **Apply Your Plan** to push the policy built from
your Lab 1.4 picks. Then walk the rule table below and confirm
each row is present in containd's web UI
([http://localhost:9080](http://localhost:9080)) or via
\`show running-config\` in the CLI. Understanding *what* is in
the policy matters as much as *how* it gets there.
:::

:::technical
Author these rules in containd directly — your choice of the web
UI or the appliance CLI. The banner above will switch to
"Your custom policy" once your commit lands. The JSON schema and
CLI walkthrough are in the hints below.
:::

### Firewall Rules to Implement

${ruleTable}

> **Critical:** RTAC rules are source-pinned to \`10.30.30.20\`. Do **not** use a broad OT Ops subnet rule. If the HMI or OpenPLC is compromised, it must not reach field devices directly.

:::hint Creating rules via the containd CLI
The granular protocol-port rule schema (with ICS DPI fields) is best edited via JSON in the CLI rather than typed-in argument lists. From the \`fw-1\` terminal (or SSH), type \`containd cli\` to enter the appliance shell, then:

    export config > /tmp/policy.json
    shell                   # drop to bash
    vi /tmp/policy.json     # edit the firewall.rules array to add each rule above
    exit                    # back to containd CLI
    import config /tmp/policy.json
    show diff               # confirm what will change
    commit

Each rule object in \`firewall.rules\` looks like:

    {
      "id": "rtac-to-field-modbus",
      "description": "RTAC Modbus polling to field devices",
      "sourceZones": ["lan1"],
      "destZones": ["lan2"],
      "sources": ["10.30.30.20/32"],
      "protocols": [{"name": "tcp", "port": "502"}],
      "ics": {},
      "action": "ALLOW",
      "log": true
    }

Tip: \`commit confirmed 60\` commits with auto-rollback after 60 seconds unless you type \`confirm\` — useful when pushing rules and you don't want to lock yourself out.
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

After each failed attempt, check the containd event log to confirm the deny was logged. From the \`fw-1\` terminal, type \`containd cli\`, then:

    show audit

…and look for the most recent deny entries. If you don't see your test attempts, logging isn't enabled on the deny rules.

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
  // Firewall-track choice (guided | technical | null). Read here so
  // both the description renderer (for trackOnly segments) and the
  // side panel (for chip + button de-emphasis) share one source.
  const { track: firewallTrack, setTrack: setFirewallTrack } = useFirewallTrack();
  // policySource tracks how the active policy got applied — needed by
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
  // firewall-implementation — only that lab actually uses the
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
                on mismatched state — the banner always renders so
                students can always tell what's loaded. */}
            <PolicyStatusBanner
              activeConfig={activeConfig}
              policySource={policySource}
              expectedConfig={step?.expected_config}
            />
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
                          scenarioId={scenario.id}
                        />
                      );
                    }
                    if (seg.type === "decision") {
                      return (
                        <DecisionBlock
                          key={`${scenario.id}-${currentStep}-${si}`}
                          scenarioId={scenario.id}
                          decisionId={seg.id}
                          options={seg.options}
                          body={seg.body}
                          defaultFrom={seg.defaultFrom}
                          correct={seg.correct}
                        />
                      );
                    }
                    if (seg.type === "findingsPanel") {
                      return (
                        <FindingsPanel
                          key={`${scenario.id}-${currentStep}-${si}`}
                          sourceScenario={seg.sourceScenario}
                          title={seg.title}
                          items={seg.items}
                        />
                      );
                    }
                    if (seg.type === "planCoverage") {
                      return (
                        <PlanCoveragePanel
                          key={`${scenario.id}-${currentStep}-${si}`}
                          title={seg.title}
                        />
                      );
                    }
                    if (seg.type === "trackPicker") {
                      return (
                        <TrackPicker
                          key={`${scenario.id}-${currentStep}-${si}`}
                        />
                      );
                    }
                    if (seg.type === "generateTrafficButton") {
                      return (
                        <div
                          key={`${scenario.id}-${currentStep}-${si}`}
                          className="my-3"
                        >
                          <button
                            onClick={() => handleGenerateTraffic(45)}
                            disabled={generatingTraffic}
                            className="rounded border border-sky-700 bg-sky-950/50 px-4 py-2 text-xs font-medium text-sky-300 hover:bg-sky-900/60 disabled:opacity-50"
                          >
                            {generatingTraffic ? "Generating Traffic…" : "Generate Traffic (45s)"}
                          </button>
                        </div>
                      );
                    }
                    if (seg.type === "icon") {
                      return (
                        <div
                          key={`${scenario.id}-${currentStep}-${si}`}
                          className="my-2"
                        >
                          <IconStamp name={seg.name} color={seg.color} label={seg.label} />
                        </div>
                      );
                    }
                    if (seg.type === "trackOnly") {
                      // Show track-only blocks when no track is yet
                      // picked (so the student sees both perspectives
                      // before deciding) OR when the current track
                      // matches the block's track.
                      if (firewallTrack !== null && firewallTrack !== seg.track) {
                        return null;
                      }
                      // Recursively split + render the body so nested
                      // commands / hints / decisions inside a track
                      // block still work.
                      const inner = splitDescription(seg.body);
                      return (
                        <div
                          key={`${scenario.id}-${currentStep}-${si}`}
                          className={`rounded-md border-l-2 pl-3 ${
                            seg.track === "guided"
                              ? "border-emerald-700/60"
                              : "border-sky-700/60"
                          } space-y-2`}
                        >
                          <div
                            className={`text-[10px] font-bold uppercase tracking-wider ${
                              seg.track === "guided"
                                ? "text-emerald-400"
                                : "text-sky-400"
                            }`}
                          >
                            {seg.track === "guided" ? "Guided track" : "Advanced track"}
                          </div>
                          {inner.map((sub, sj) => {
                            if (sub.type === "prose") {
                              const t = sub.value.replace(/^\n+|\n+$/g, "");
                              if (!t) return null;
                              return <MarkdownProse key={sj}>{t}</MarkdownProse>;
                            }
                            if (sub.type === "cmd") {
                              const ci = bodyCmdIdx++;
                              return (
                                <CommandBlock
                                  key={sj}
                                  cmd={sub.value}
                                  runId={`body-${ci}`}
                                  runningId={autoRunning}
                                  onRun={runHandler}
                                  cli={sub.cli}
                                />
                              );
                            }
                            if (sub.type === "hint") {
                              const hi = hintIdx++;
                              return (
                                <HintBlock
                                  key={sj}
                                  title={sub.title}
                                  body={sub.value}
                                  runIdPrefix={`hint-t-${hi}`}
                                  runningId={autoRunning}
                                  onRun={runHandler}
                                  scenarioId={scenario.id}
                                />
                              );
                            }
                            // Fallback for nested decision / findings /
                            // plan-coverage / nested trackOnly: render
                            // body as prose so authors don't lose
                            // content; richer nesting can be added if
                            // a YAML actually needs it.
                            return null;
                          })}
                        </div>
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
                        cli={seg.cli}
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
            {/* containd policy actions + operational status. The
                state indicator (what is currently running) lives in
                PolicyStatusBanner at the top of the step content;
                this side-panel section is just the actions. */}
            <div className="rounded-xl border border-slate-800 bg-slate-900/70 p-4 space-y-3">
              {/* Policy action buttons — shown on the four firewall-
                  exercising scenarios (2.2 / 2.3 / 2.3-bonus / 2.4),
                  hidden in the planning labs (1.2 / 1.3 / 1.4) since
                  their steps don't actually drive policy state. */}
              {POLICY_ACTION_SCENARIOS.includes(scenario.id) && (
                <div className="rounded-lg border border-slate-800 bg-slate-950/40 px-3 py-2">
                  <div className="mb-2 flex items-center justify-between gap-2">
                    <div className="text-[9px] font-medium uppercase tracking-wider text-slate-500">
                      containd NGFW — policy actions
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
                          {firewallTrack}
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
                      Technical track — commit your policy in containd directly.
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
                        hidden — students should see it as an option
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
  const t = title.toLowerCase();
  // Match the "apply hardened policy" steps in Lab 2.3 and 2.3-bonus
  // alongside the older "improve segmentation" wording used elsewhere.
  // Without `hardened`/`harden`, Lab 2.3 step 6 ("Apply the hardened
  // policy") and Lab 2.3-bonus step 5 ("Apply hardened policy") would
  // hide the Apply Hardened button even though the step's own action
  // is { type: firewall, config: improved }.
  return t.includes("improve") || t.includes("segmentation") || t.includes("hardened") || t.includes("harden");
}

function isBaselineStep(title?: string): boolean {
  if (!title) return false;
  return title.toLowerCase().includes("baseline") || title.toLowerCase().includes("observe");
}
