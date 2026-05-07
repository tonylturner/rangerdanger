"use client";

import { useQuery } from "@tanstack/react-query";
import { listScenarios, type Scenario } from "../lib/api";

export function ScenarioList({ onStartExercise }: { templateId?: string; fetchAll?: boolean; onStartExercise?: (scenario: Scenario) => void }) {
  const { data, isLoading } = useQuery({
    queryKey: ["scenarios", "substation-segmentation"],
    queryFn: () => listScenarios("substation-segmentation"),
  });

  if (isLoading) {
    return <div className="rounded-xl border border-slate-800 bg-slate-900/70 p-5">Loading scenarios...</div>;
  }

  if (!data || data.scenarios.length === 0) {
    return <div className="rounded-xl border border-slate-800 bg-slate-900/70 p-5 text-sm text-slate-300">No scenarios defined yet.</div>;
  }

  return (
    <div className="grid gap-3">
      {data?.scenarios.map((scenario) => (
        <ScenarioCard key={scenario.id} scenario={scenario} onStartExercise={onStartExercise} />
      ))}
    </div>
  );
}

function ScenarioCard({ scenario, onStartExercise }: { scenario: Scenario; onStartExercise?: (scenario: Scenario) => void }) {
  const cardText = scenario.summary || scenario.description;
  const isBonus = scenario.tags.includes("bonus");

  return (
    <article className={`rounded-lg border p-4 ${isBonus ? "border-slate-800/50 bg-slate-900/40 opacity-75" : "border-slate-800 bg-slate-900/70"}`}>
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0 flex items-start gap-3">
          {scenario.order && (
            <span className={`mt-0.5 inline-flex h-6 shrink-0 items-center justify-center rounded px-2 text-xs font-bold ${
              isBonus ? "bg-slate-800/60 text-slate-500" : "bg-sky-950/60 text-sky-400"
            }`}>
              Lab {scenario.order}
            </span>
          )}
          <div>
            <h2 className="text-sm font-bold text-white">{scenario.name}</h2>
            <p className="mt-0.5 text-xs text-slate-500">{cardText}</p>
            <div className="mt-2 flex flex-wrap gap-1.5">
              {scenario.tags.filter(t => t !== "bonus").map((tag) => (
                <span key={tag} className="rounded-full border border-slate-700 bg-slate-800/50 px-2 py-0.5 text-[10px] text-slate-400">
                  {tag}
                </span>
              ))}
              <span className="rounded-full border border-slate-700 bg-slate-800/50 px-2 py-0.5 text-[10px] text-slate-500">
                {scenario.steps.length} steps
              </span>
            </div>
          </div>
        </div>
        {onStartExercise && (
          <button
            onClick={() => onStartExercise(scenario)}
            className="shrink-0 rounded border border-sky-700 bg-sky-950/40 px-3 py-1.5 text-xs font-medium text-sky-400 transition-colors hover:bg-sky-900/50"
          >
            Start Exercise
          </button>
        )}
      </div>
    </article>
  );
}
