"use client";

import { useQuery } from "@tanstack/react-query";
import { listScenarios, type Scenario } from "../lib/api";

// Maps exercise IDs to workshop workbook sections
const WORKBOOK_SECTION: Record<string, string> = {
  "baseline-assessment":       "1.2",
  "segmentation-requirements": "1.3",
  "vendor-rdp-compromise":     "2.3",
  "modbus-override":           "2.3",
  "dnp3-command-injection":    "2.3",
  "validation-evidence":       "2.4",
};

// Read completion from localStorage
function getCompletionPct(exerciseId: string, totalSteps: number): number {
  if (totalSteps === 0) return 0;
  try {
    const raw = localStorage.getItem(`rd-exercise-${exerciseId}`);
    if (!raw) return 0;
    const saved = JSON.parse(raw);
    const completed = saved.completedSteps?.length || 0;
    return Math.round((completed / totalSteps) * 100);
  } catch {
    return 0;
  }
}

export function ExerciseList({ onStartExercise }: { onStartExercise?: (scenario: Scenario) => void }) {
  const { data, isLoading } = useQuery({
    queryKey: ["scenarios", "substation-segmentation"],
    queryFn: () => listScenarios("substation-segmentation"),
  });

  if (isLoading) {
    return <div className="rounded-xl border border-slate-800 bg-slate-900/70 p-5">Loading exercises...</div>;
  }

  if (!data || data.scenarios.length === 0) {
    return <div className="rounded-xl border border-slate-800 bg-slate-900/70 p-5 text-sm text-slate-300">No exercises defined yet.</div>;
  }

  return (
    <div className="grid gap-3">
      {data?.scenarios.map((exercise, index) => (
        <ExerciseCard key={exercise.id} exercise={exercise} index={index} onStartExercise={onStartExercise} />
      ))}
    </div>
  );
}

function ExerciseCard({ exercise, index, onStartExercise }: { exercise: Scenario; index: number; onStartExercise?: (scenario: Scenario) => void }) {
  const cardText = exercise.summary || exercise.description;
  const isBonus = exercise.tags.includes("bonus");
  const pct = getCompletionPct(exercise.id, exercise.steps.length);
  const section = WORKBOOK_SECTION[exercise.id];

  return (
    <article className={`rounded-lg border p-4 ${isBonus ? "border-slate-800/50 bg-slate-900/40 opacity-75" : "border-slate-800 bg-slate-900/70"}`}>
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0 flex items-start gap-3">
          {/* Completion badge */}
          <div className="mt-0.5 flex h-8 w-8 shrink-0 flex-col items-center justify-center rounded" style={{
            background: pct === 100
              ? "rgba(34,197,94,0.15)"
              : pct > 0
              ? "rgba(14,165,233,0.15)"
              : isBonus
              ? "rgba(51,65,85,0.3)"
              : "rgba(14,165,233,0.08)",
            border: pct === 100
              ? "1px solid rgba(34,197,94,0.4)"
              : pct > 0
              ? "1px solid rgba(14,165,233,0.3)"
              : "1px solid rgba(51,65,85,0.3)",
          }}>
            <span className={`text-[10px] font-bold leading-none ${
              pct === 100 ? "text-green-400" : pct > 0 ? "text-sky-400" : isBonus ? "text-slate-600" : "text-slate-500"
            }`}>
              {pct}%
            </span>
          </div>

          <div>
            <h2 className="text-sm font-bold text-white">
              Exercise {index}: {exercise.name}
            </h2>
            <p className="mt-0.5 text-xs text-slate-500">{cardText}</p>
            <div className="mt-2 flex flex-wrap gap-1.5">
              {section && (
                <span className="rounded-full border border-amber-700/50 bg-amber-950/30 px-2 py-0.5 text-[10px] font-bold text-amber-400">
                  Lab {section}
                </span>
              )}
              {exercise.tags.filter(t => t !== "bonus").map((tag) => (
                <span key={tag} className="rounded-full border border-slate-700 bg-slate-800/50 px-2 py-0.5 text-[10px] text-slate-400">
                  {tag}
                </span>
              ))}
              <span className="rounded-full border border-slate-700 bg-slate-800/50 px-2 py-0.5 text-[10px] text-slate-500">
                {exercise.steps.length} steps
              </span>
            </div>
          </div>
        </div>
        {onStartExercise && (
          <button
            onClick={() => onStartExercise(exercise)}
            className="shrink-0 rounded border border-sky-700 bg-sky-950/40 px-3 py-1.5 text-xs font-medium text-sky-400 transition-colors hover:bg-sky-900/50"
          >
            {pct > 0 && pct < 100 ? "Continue" : pct === 100 ? "Review" : "Start"}
          </button>
        )}
      </div>
    </article>
  );
}
