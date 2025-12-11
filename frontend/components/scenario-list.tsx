"use client";

import { useQuery } from "@tanstack/react-query";
import { listScenarios } from "../lib/api";

export function ScenarioList({ templateId, fetchAll = false }: { templateId?: string; fetchAll?: boolean }) {
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
    <div className="grid gap-4">
      {data?.scenarios.map((scenario) => (
        <article key={scenario.id} className="rounded-2xl border border-slate-800 bg-slate-900/70 p-5">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h2 className="text-xl font-semibold text-white">{scenario.name}</h2>
              <p className="text-sm text-slate-400">{scenario.description}</p>
            </div>
            <div className="flex flex-wrap gap-2 text-xs uppercase tracking-wide text-amber-300">
              {scenario.tags.map((tag) => (
                <span key={tag} className="rounded-full border border-amber-400/40 px-2 py-1">
                  {tag}
                </span>
              ))}
            </div>
          </div>
          <ol className="mt-4 list-decimal space-y-2 pl-6 text-sm text-slate-200">
            {scenario.steps.map((step) => (
              <li key={step.title}>
                <span className="font-semibold text-white">{step.title}:</span> {step.description}
              </li>
            ))}
          </ol>
        </article>
      ))}
    </div>
  );
}
