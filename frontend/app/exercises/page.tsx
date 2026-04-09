"use client";

import { useState } from "react";
import { ExerciseList } from "../../components/exercise-list";
import { ExerciseRunner } from "../../components/exercise-runner";
import type { Scenario } from "../../lib/api";

export default function ExercisesPage() {
  const [activeExercise, setActiveExercise] = useState<Scenario | null>(null);

  if (activeExercise) {
    return (
      <main className="mx-auto w-full max-w-6xl px-6 py-10">
        <ExerciseRunner scenario={activeExercise} onExit={() => setActiveExercise(null)} />
      </main>
    );
  }

  return (
    <main className="mx-auto w-full max-w-4xl px-6 py-10">
      <div className="space-y-2">
        <h1 className="text-3xl font-semibold text-white">Exercises</h1>
        <p className="text-slate-400">
          Step-by-step attack and defense exercises for distribution substation segmentation validation.
        </p>
      </div>
      <div className="mt-6 space-y-4">
        <ExerciseList onStartExercise={setActiveExercise} />
      </div>
    </main>
  );
}
