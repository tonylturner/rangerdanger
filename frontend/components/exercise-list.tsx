"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { FileText, Star } from "lucide-react";
import { Tooltip, TooltipTrigger, TooltipContent } from "./ui/tooltip";
import { listScenarios, type Scenario } from "../lib/api";
import { WORKBOOK_SECTION } from "../lib/workbook-sections";

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

export function ExerciseList({
  onStartExercise,
  onExportExercise,
}: {
  onStartExercise?: (scenario: Scenario) => void;
  onExportExercise?: (scenario: Scenario) => void | Promise<void>;
}) {
  const [showBonus, setShowBonus] = useState(true);
  const { data, isLoading } = useQuery({
    queryKey: ["scenarios", "substation-segmentation"],
    queryFn: () => listScenarios("substation-segmentation"),
  });

  if (isLoading) {
    return <div className="rounded-xl border border-slate-800 bg-slate-900/70 p-5">Loading exercises...</div>;
  }

  if (!data || data.scenarios.length === 0) {
    return (
      <div className="flex flex-col items-center gap-3 rounded-xl border border-slate-800 bg-slate-900/70 px-5 py-10 text-center">
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img
          src="/rook-quarter-turn-wink-transparent-web.png"
          alt=""
          className="h-28 w-28 opacity-90"
        />
        <div className="text-sm font-medium text-slate-300">No exercises loaded.</div>
        <div className="text-xs text-slate-500">Check that the lab definitions are mounted and the backend has started.</div>
      </div>
    );
  }

  const exercises = data.scenarios.filter((ex) => showBonus || !ex.tags.includes("bonus"));
  const bonusCount = data.scenarios.filter((ex) => ex.tags.includes("bonus")).length;

  return (
    <div className="space-y-3">
      {bonusCount > 0 && (
        <div className="flex items-center justify-end">
          <button
            onClick={() => setShowBonus(!showBonus)}
            className={`flex items-center gap-1.5 rounded-full border px-3 py-1 text-[11px] font-medium transition-colors ${
              showBonus
                ? "border-purple-700/50 bg-purple-950/30 text-purple-400"
                : "border-slate-700 bg-slate-800/50 text-slate-500 hover:text-slate-300"
            }`}
          >
            <Star className="h-3 w-3" />
            {showBonus ? `Bonus labs shown (${bonusCount})` : `Show bonus labs (${bonusCount})`}
          </button>
        </div>
      )}
      <div className="grid gap-3">
        {exercises.map((exercise) => (
          <ExerciseCard
            key={exercise.id}
            exercise={exercise}
            onStartExercise={onStartExercise}
            onExportExercise={onExportExercise}
          />
        ))}
      </div>
    </div>
  );
}

function ExerciseCard({
  exercise,
  onStartExercise,
  onExportExercise,
}: {
  exercise: Scenario;
  onStartExercise?: (scenario: Scenario) => void;
  onExportExercise?: (scenario: Scenario) => void | Promise<void>;
}) {
  const cardText = exercise.summary || exercise.description;
  const isBonus = exercise.tags.includes("bonus");
  const pct = getCompletionPct(exercise.id, exercise.steps.length);
  const section = WORKBOOK_SECTION[exercise.id];

  const handleExportPDF = (e: React.MouseEvent) => {
    e.stopPropagation();
    onExportExercise?.(exercise);
  };

  return (
    <article className={`rounded-lg border p-4 ${isBonus ? "border-purple-800/30 bg-purple-950/10" : "border-slate-800 bg-slate-900/70"}`}>
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0 flex items-start gap-3">
          <div className="mt-0.5 flex h-8 w-8 shrink-0 flex-col items-center justify-center rounded" style={{
            background: pct === 100
              ? "rgba(34,197,94,0.15)"
              : pct > 0
              ? "rgba(14,165,233,0.15)"
              : isBonus
              ? "rgba(147,51,234,0.1)"
              : "rgba(14,165,233,0.08)",
            border: pct === 100
              ? "1px solid rgba(34,197,94,0.4)"
              : pct > 0
              ? "1px solid rgba(14,165,233,0.3)"
              : isBonus
              ? "1px solid rgba(147,51,234,0.25)"
              : "1px solid rgba(51,65,85,0.3)",
          }}>
            <span className={`text-[10px] font-bold leading-none ${
              pct === 100 ? "text-green-400" : pct > 0 ? "text-sky-400" : isBonus ? "text-purple-500" : "text-slate-500"
            }`}>
              {pct}%
            </span>
          </div>

          <div>
            <h2 className="text-sm font-bold text-white">
              Exercise {exercise.order ?? ""}: {exercise.name}
            </h2>
            <p className="mt-0.5 text-xs text-slate-500">{cardText}</p>
            <div className="mt-2 flex flex-wrap gap-1.5">
              {section && (
                <span className="rounded-full border border-amber-700/50 bg-amber-950/30 px-2 py-0.5 text-[10px] font-bold text-amber-400">
                  Lab {section}
                </span>
              )}
              {isBonus && (
                <span className="rounded-full border border-purple-700/50 bg-purple-950/30 px-2 py-0.5 text-[10px] font-bold text-purple-400 flex items-center gap-1">
                  <Star className="h-2.5 w-2.5" />
                  Bonus
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
        <div className="flex shrink-0 items-center gap-2">
          {onExportExercise && (
            <Tooltip>
              <TooltipTrigger asChild>
                <button
                  onClick={handleExportPDF}
                  aria-label="Export exercise as PDF"
                  className="inline-flex items-center justify-center rounded border border-slate-700 bg-slate-800/50 px-2 py-1.5 text-slate-400 transition-colors hover:text-sky-400 hover:border-slate-600"
                >
                  <FileText className="h-4 w-4" />
                </button>
              </TooltipTrigger>
              <TooltipContent>Export as PDF</TooltipContent>
            </Tooltip>
          )}
          {onStartExercise && (
            <button
              onClick={() => onStartExercise(exercise)}
              className={`rounded border px-3 py-1.5 text-xs font-medium transition-colors ${
                isBonus
                  ? "border-purple-700/50 bg-purple-950/30 text-purple-400 hover:bg-purple-900/40"
                  : "border-sky-700 bg-sky-950/40 text-sky-400 hover:bg-sky-900/50"
              }`}
            >
              {pct > 0 && pct < 100 ? "Continue" : pct === 100 ? "Review" : "Start"}
            </button>
          )}
        </div>
      </div>
    </article>
  );
}
