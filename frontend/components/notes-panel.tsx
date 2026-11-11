"use client";

import Link from "next/link";
import { useCurriculumScope } from "../lib/curriculum-scope";
import { loadProgress } from "../lib/scenario-runner-storage";
import { useCurriculumStorageRevision } from "../lib/use-curriculum-storage-revision";
import { useHydrated } from "../lib/use-hydrated";

export function NotesPanel({ sourceScenario, sourceScenarioOrder, title }: {
  sourceScenario: string;
  sourceScenarioOrder?: string;
  title: string;
}) {
  const scope = useCurriculumScope();
  const hydrated = useHydrated();
  useCurriculumStorageRevision(scope);
  const notes = hydrated ? loadProgress(scope, { id: sourceScenario, steps: [] }).notes : "";
  const lab = sourceScenarioOrder ? `Lab ${sourceScenarioOrder}` : sourceScenario;
  return (
    <section className="rounded-lg border border-cyan-900/40 bg-cyan-950/10 p-3 text-xs text-slate-300" aria-label={title}>
      <h4 className="mb-2 font-semibold text-cyan-300">{title}</h4>
      {notes.trim()
        ? <p className="font-mono whitespace-pre-wrap break-words">{notes}</p>
        : <p>Nothing saved in {lab}. <Link target="_blank" rel="noopener noreferrer" className="text-cyan-300 underline" href={`/exercises/${sourceScenario}`}>Open {lab} to write your Notes.</Link></p>}
    </section>
  );
}
