"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import {
  Terminal as TerminalIcon,
  Lightbulb,
  ChevronDown,
  ChevronRight,
  AlertCircle,
  CircleCheck,
  CircleDashed,
  Shield,
  Activity,
  Network,
  BookOpen,
} from "lucide-react";
import {
  readRequirements,
  computeCoverage,
  summariseCoverage,
} from "../lib/requirement-coverage";
import { loadRemediationPlan } from "../lib/remediation-plan";
import { Tooltip, TooltipTrigger, TooltipContent } from "./ui/tooltip";
import { DecisionPanel } from "./decision-panel";
import { MarkdownProse } from "./markdown-prose";
import { TrackPicker } from "./track-picker";
import {
  splitDescription,
  type Segment,
  type FindingsPanelItem,
} from "../lib/scenario-description";
import { decisionStorageKey } from "../lib/decision-storage";
import {
  injectDynamicContent,
  PHASE3_TITLES,
  titleMatches,
} from "../lib/scenario-runner-logic";
import { type ActionSummary, type DynamicExercisePlan } from "../lib/remediation-to-rules";
import { ValidationReportPanel } from "./validation-report-panel";

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
  // copyOnly = an interactive/GUI command (remote-desktop client, sshpass
  // shell): copy button only, no Run, no badge - run it in a terminal.
  copyOnly?: boolean;
};

export function CommandBlock({ cmd, runId, runningId, onRun, cli, copyOnly }: CommandBlockProps) {
  const runnable = cli || copyOnly ? null : onRun;
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
  /** "<scenarioId>:<decisionId>" - initial value source if local key empty */
  defaultFrom?: string;
  /**
   * Known-correct answer for this decision. When set, the block
   * renders a green/red feedback chip after the student picks. Only
   * use on observation/factual prompts (e.g. lab 1.2 step 6 - the
   * weak baseline really did show every exposure). Leave unset on
   * judgment-call decisions (lab 1.3 verdicts) where no single
   * answer is "right."
   */
  correct?: string;
};

export function DecisionBlock({ scenarioId, decisionId, options, body, defaultFrom, correct }: DecisionBlockProps) {
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
      // No saved value - try to inherit from the upstream decision.
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
      /* localStorage unavailable - fall back to in-memory state */
    }
  }, [storageKey, options, defaultFrom]);

  const onChange = (next: string) => {
    setValue(next);
    setInheritedFrom("");   // student touched the dropdown - no longer "inherited"
    try {
      if (next) {
        window.localStorage.setItem(storageKey, next);
      } else {
        window.localStorage.removeItem(storageKey);
      }
    } catch {
      /* swallow - UI still works without persistence */
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
                inherited from earlier lab - adjust if your design has changed
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
// DecisionBlock writes. Quiet on the empty case - if the upstream
// lab wasn't done, the panel says so and links back.
type FindingsPanelProps = {
  sourceScenario: string;
  title: string;
  items: FindingsPanelItem[];
};

export function FindingsPanel({ sourceScenario, title, items }: FindingsPanelProps) {
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
          >Lab {sourceScenario}</a> yet - your design verdicts below will be made without that context.
        </div>
      )}
    </div>
  );
}

// PlanCoveragePanel renders the student's Lab 1.4 plan coverage -
// the same per-requirement breakdown the DecisionPanel sticky bar
// shows, but inline at any point in a description. Used in Lab 2.4
// to surface "what did your plan close vs defer" without making the
// student manually recall their selections.
export function PlanCoveragePanel({ title }: { title: string }) {
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
        No remediation plan recorded yet. Visit <Link
          href="/exercises/remediation-planning"
          className="text-cyan-500 hover:text-cyan-300 underline underline-offset-2"
        >Lab 1.4 (Remediation Planning)</Link> to select actions and your plan coverage will appear here.
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
                  <span className="text-slate-600"> - {c.reason}</span>
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
                  <span className="text-slate-500"> - {c.req.verdict}, fully addressed by your plan</span>
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
                  <span className="text-slate-500"> - {c.req.verdict}, partial coverage. Missing: </span>
                  <span className="font-mono text-amber-400">{c.missingActions.join(", ")}</span>
                </span>
              </div>
            );
          }
          // gap - deferred or never selected
          return (
            <div key={c.req.id} className="flex items-start gap-2 text-[11px]">
              <AlertCircle className="h-3.5 w-3.5 mt-0.5 text-red-400 shrink-0" />
              <span className="text-slate-300">
                <span className="font-bold">{c.req.label}</span>
                <span className="text-slate-500"> - {c.req.verdict}, deferred. Implementing actions: </span>
                <span className="font-mono text-red-400">{c.expectedActions.join(" or ")}</span>
              </span>
            </div>
          );
        })}
      </div>
      <div className="mt-2 text-[10px] text-slate-500 italic">
        Read-only snapshot of your <Link
          href="/exercises/remediation-planning"
          className="text-cyan-500 hover:text-cyan-300 underline underline-offset-2"
        >Lab 1.4 plan</Link>. Edit selections there to update.
      </div>
    </div>
  );
}

// HintBlock is a collapsible "reveal answer" panel rendered inside step
// descriptions where the YAML contains a :::hint Title / ::: fence.
// Default state is collapsed - the student has to click to see the answer.
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
export function IconStamp({ name, color, label }: { name: string; color: string; label: string }) {
  const Icon = ICON_MAP[name] ?? Shield;
  const c = ICON_COLORS[color] ?? ICON_COLORS.amber;
  return (
    <span className={`inline-flex items-center gap-1.5 rounded border px-2 py-1 align-middle ${c.border} ${c.bg}`}>
      <Icon className={`h-3.5 w-3.5 ${c.text}`} />
      {label && <span className={`text-[11px] font-medium ${c.text}`}>{label}</span>}
    </span>
  );
}

export function HintBlock({ title, body, runIdPrefix, runningId, onRun, scenarioId }: HintBlockProps) {
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
                  copyOnly={seg.copyOnly}
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
            if (seg.type === "trackPicker" || seg.type === "trackOnly" || seg.type === "generateTrafficButton" || seg.type === "validationReport" || seg.type === "icon") {
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

// Plan summary panel rendered in Exercise 3 Phase 3 - shows selected and
// unselected remediation actions as styled cards rather than markdown text.
export function RemediationPlanSummary({ plan }: { plan: DynamicExercisePlan }) {
  const [showUnselected, setShowUnselected] = useState(false);

  if (!plan.hasRemediationPlan) return null;

  return (
    <div className="space-y-3">
      <div className="rounded-lg border border-sky-800/50 bg-sky-950/20 p-4">
        <div className="text-[10px] font-bold uppercase tracking-wider text-sky-400 mb-3">
          Your Remediation Plan - {plan.selectedActions.length} actions selected
        </div>
        <div className="space-y-2">
          {plan.selectedSummary.map((a) => (
            <PlanActionCard key={a.id} action={a} selected />
          ))}
        </div>
        {plan.includeDpi && (
          <div className="mt-3 rounded border border-purple-800/40 bg-purple-950/20 px-3 py-2 text-[11px] text-purple-300">
            <span className="font-bold">ICS DPI enabled</span> - containd will filter by Modbus/DNP3 function code, not just port number
          </div>
        )}
        {plan.enableLogging && (
          <div className="mt-2 rounded border border-amber-800/40 bg-amber-950/20 px-3 py-2 text-[11px] text-amber-300">
            <span className="font-bold">Logging enabled</span> - set <code className="bg-slate-800 px-1 rounded text-[10px]">&quot;log&quot;: true</code> on DENY rules and critical ALLOW rules
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
                Not selected - {plan.unselectedSummary.length} actions deferred
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
        {selected ? "✓" : "-"}
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

export function DescriptionSegments({
  scenarioId,
  currentStep,
  stepTitle,
  stepDescription,
  dynamicPlan,
  exerciseNodes,
  firewallTrack,
  autoRunning,
  handleAutoRun,
  handleGenerateTraffic,
  generatingTraffic,
}: {
  scenarioId: string;
  currentStep: number;
  stepTitle: string;
  stepDescription: string;
  dynamicPlan: DynamicExercisePlan | null;
  exerciseNodes: string[];
  firewallTrack: "guided" | "technical" | null;
  autoRunning: string | null;
  handleAutoRun: (cmd: string, runId: string, stepDesc: string) => Promise<void>;
  handleGenerateTraffic: (durationSec?: number) => Promise<void>;
  generatingTraffic: boolean;
}) {
  const scenario = { id: scenarioId };
  const step = { title: stepTitle, description: stepDescription };
  return (
    <>
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
                    if (seg.type === "validationReport") {
                      return (
                        <div
                          key={`${scenario.id}-${currentStep}-${si}`}
                          className="my-3"
                        >
                          <ValidationReportPanel />
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
                                  copyOnly={sub.copyOnly}
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
                        copyOnly={seg.copyOnly}
                      />
                    );
                  })}
                </div>
              );
            })()}

    </>
  );
}
