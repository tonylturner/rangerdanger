"use client";

import { useRouter } from "next/navigation";
import { ExerciseList } from "../../components/exercise-list";
import type { Scenario } from "../../lib/api";

export default function ExercisesPage() {
  const router = useRouter();

  const handleStart = (scenario: Scenario) => {
    router.push(`/exercises/${encodeURIComponent(scenario.id)}`);
  };

  return (
    <main className="mx-auto w-full max-w-4xl px-6 py-10">
      <div className="space-y-2">
        <h1 className="text-3xl font-semibold text-white">Exercises</h1>
        <p className="text-slate-400">
          Step-by-step attack and defense exercises for distribution substation segmentation validation.
        </p>
      </div>
      <div className="mt-6 space-y-4">
        <ExerciseList onStartExercise={handleStart} />
      </div>
    </main>
  );
}
