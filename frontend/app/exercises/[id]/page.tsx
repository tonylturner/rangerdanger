"use client";

import { use } from "react";
import { useRouter } from "next/navigation";
import { ExerciseRunner } from "../../../components/exercise-runner";
import { CurriculumScopeProvider, useActiveCurriculum } from "../../../lib/curriculum-scope";

type Params = { id: string };

export default function ExerciseDetailPage({ params }: { params: Promise<Params> }) {
  // Next 15 made route params a Promise. This is a client component, so it
  // unwraps with React.use() rather than await.
  const { id } = use(params);
  const router = useRouter();
  const { pkg, scenarios, isLoading } = useActiveCurriculum();
  const scenario = scenarios?.find((s) => s.id === id);

  const handleExit = () => router.push("/exercises");

  if (isLoading) {
    return (
      <main className="mx-auto w-full max-w-6xl px-6 py-10">
        <div className="rounded-xl border border-slate-800 bg-slate-900/70 p-5 text-sm text-slate-400">
          Loading exercise…
        </div>
      </main>
    );
  }

  if (!pkg || !scenario) {
    return (
      <main className="mx-auto w-full max-w-6xl px-6 py-10">
        <div className="rounded-xl border border-amber-800 bg-amber-950/30 p-5">
          <h2 className="text-sm font-bold text-amber-300">Exercise not found</h2>
          <p className="mt-2 text-xs text-amber-400">
            No exercise with id <code className="rounded bg-slate-900 px-1 py-0.5">{id}</code> exists.
          </p>
          <button
            onClick={handleExit}
            className="mt-3 rounded border border-slate-700 bg-slate-800/50 px-3 py-1.5 text-xs text-slate-300 hover:text-slate-100"
          >
            Back to exercise list
          </button>
        </div>
      </main>
    );
  }

  return (
    <main className="mx-auto w-full max-w-6xl px-6 py-10">
      <CurriculumScopeProvider pkg={pkg}>
        <ExerciseRunner key={`${pkg.id}:${pkg.revision}:${scenario.id}`} scenario={scenario} onExit={handleExit} />
      </CurriculumScopeProvider>
    </main>
  );
}
