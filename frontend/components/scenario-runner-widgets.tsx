"use client";

import { type Scenario } from "../lib/api";

// Quick command buttons contextual to the current step
export function QuickCommands({
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

export function MiniStatus({ label, value, ok }: { label: string; value: string; ok?: boolean }) {
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

export function ExerciseSummary({
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
              {activeConfig || "-"}
            </div>
          </div>
          <div className="rounded border border-slate-800 bg-slate-950 p-3">
            <div className="text-[9px] uppercase tracking-wider text-slate-600">Notes</div>
            <div className="text-lg font-bold text-slate-300">
              {notes ? `${notes.split("\n").length} lines` : "-"}
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
