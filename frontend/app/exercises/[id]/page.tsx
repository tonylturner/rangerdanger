"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { ExerciseRunner } from "../../../components/exercise-runner";
import { listScenarios, type Scenario } from "../../../lib/api";

type Params = { id: string };

export default function ExerciseDetailPage({ params }: { params: Params }) {
  const { id } = params;
  const router = useRouter();
  const [scenario, setScenario] = useState<Scenario | null>(null);
  const [status, setStatus] = useState<"loading" | "ready" | "not_found">("loading");

  useEffect(() => {
    let cancelled = false;
    listScenarios("substation-segmentation")
      .then((res) => {
        if (cancelled) return;
        const match = res.scenarios.find((s) => s.id === id);
        if (match) {
          setScenario(match);
          setStatus("ready");
        } else {
          setStatus("not_found");
        }
      })
      .catch(() => {
        if (!cancelled) setStatus("not_found");
      });
    return () => {
      cancelled = true;
    };
  }, [id]);

  const handleExit = () => router.push("/exercises");

  if (status === "loading") {
    return (
      <main className="mx-auto w-full max-w-6xl px-6 py-10">
        <div className="rounded-xl border border-slate-800 bg-slate-900/70 p-5 text-sm text-slate-400">
          Loading exercise…
        </div>
      </main>
    );
  }

  if (status === "not_found" || !scenario) {
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
      <ExerciseRunner scenario={scenario} onExit={handleExit} />
    </main>
  );
}
