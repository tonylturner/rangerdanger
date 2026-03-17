"use client";

import { useState } from "react";
import { ScenarioList } from "../../components/scenario-list";
import { ScenarioRunner } from "../../components/scenario-runner";
import type { Scenario } from "../../lib/api";

export default function ScenariosPage() {
  const [activeScenario, setActiveScenario] = useState<Scenario | null>(null);

  if (activeScenario) {
    return (
      <main className="mx-auto w-full max-w-6xl px-6 py-10">
        <ScenarioRunner scenario={activeScenario} onExit={() => setActiveScenario(null)} />
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
        <ScenarioList fetchAll onStartExercise={setActiveScenario} />
      </div>
    </main>
  );
}
