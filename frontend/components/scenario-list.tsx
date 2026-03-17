"use client";

import { useQuery } from "@tanstack/react-query";
import { listScenarios, type Scenario } from "../lib/api";

export function ScenarioList({ templateId, fetchAll = false, onStartExercise }: { templateId?: string; fetchAll?: boolean; onStartExercise?: (scenario: Scenario) => void }) {
  const enabled = fetchAll || Boolean(templateId);
  const { data, isLoading } = useQuery({
    queryKey: ["scenarios", fetchAll ? "all" : templateId ?? "all"],
    queryFn: () => listScenarios(fetchAll ? undefined : templateId),
    enabled
  });

  if (!enabled) {
    return <div className="rounded-xl border border-slate-800 bg-slate-900/70 p-5">Select a template to load scenarios.</div>;
  }

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
  return (
    <article className="rounded-lg border border-slate-800 bg-slate-900/70 p-4">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h2 className="text-sm font-bold text-white">{scenario.name}</h2>
          <p className="mt-0.5 text-xs text-slate-500">{scenario.description}</p>
          <div className="mt-2 flex flex-wrap gap-1.5">
            {scenario.tags.map((tag) => (
              <span key={tag} className="rounded-full border border-slate-700 bg-slate-800/50 px-2 py-0.5 text-[10px] text-slate-400">
                {tag}
              </span>
            ))}
            <span className="rounded-full border border-slate-700 bg-slate-800/50 px-2 py-0.5 text-[10px] text-slate-500">
              {scenario.steps.length} steps
            </span>
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
