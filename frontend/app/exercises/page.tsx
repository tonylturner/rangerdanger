"use client";

import { useCallback, useState } from "react";
import { useRouter } from "next/navigation";
import { BookOpen } from "lucide-react";
import { ExerciseList } from "../../components/exercise-list";
import { Tooltip, TooltipTrigger, TooltipContent } from "../../components/ui/tooltip";
import { Toast, type ToastState } from "../../components/ui/toast";
import { downloadExercisePDF, downloadWorkbookPDF } from "../../lib/pdf-download";
import type { Scenario } from "../../lib/api";

export default function ExercisesPage() {
  const router = useRouter();
  const [toast, setToast] = useState<ToastState | null>(null);
  const [workbookExporting, setWorkbookExporting] = useState(false);

  const handleStart = (scenario: Scenario) => {
    router.push(`/exercises/${encodeURIComponent(scenario.id)}`);
  };

  const handleExportExercise = useCallback(async (scenario: Scenario) => {
    const safeName = scenario.name.replace(/[^a-z0-9-]+/gi, "-").toLowerCase();
    const filename = `rangerdanger-${safeName}.pdf`;
    setToast({
      kind: "loading",
      title: `Generating PDF: ${scenario.name}`,
      detail: "Rendering exercise content…",
    });
    try {
      await downloadExercisePDF(scenario.id, scenario.name);
      setToast({
        kind: "success",
        title: "Download started",
        detail: filename,
      });
    } catch (err) {
      console.error("Exercise PDF export failed:", err);
      setToast({
        kind: "error",
        title: "PDF export failed",
        detail: err instanceof Error ? err.message : String(err),
      });
    }
  }, []);

  const handleExportWorkbook = useCallback(async () => {
    if (workbookExporting) return;
    setWorkbookExporting(true);
    setToast({
      kind: "loading",
      title: "Generating workbook PDF",
      detail: "Rendering all exercises — this may take a few seconds…",
    });
    try {
      await downloadWorkbookPDF();
      setToast({
        kind: "success",
        title: "Workbook download started",
        detail: "rangerdanger-workbook.pdf",
      });
    } catch (err) {
      console.error("Workbook PDF export failed:", err);
      setToast({
        kind: "error",
        title: "Workbook export failed",
        detail: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setWorkbookExporting(false);
    }
  }, [workbookExporting]);

  return (
    <main className="mx-auto w-full max-w-4xl px-6 py-10">
      <div className="flex items-start justify-between gap-4">
        <div className="space-y-2">
          <h1 className="text-3xl font-semibold text-white">Exercises</h1>
          <p className="text-slate-400">
            Step-by-step attack and defense exercises for distribution substation segmentation validation.
          </p>
        </div>
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              onClick={handleExportWorkbook}
              disabled={workbookExporting}
              aria-label="Export full workbook as PDF"
              className="inline-flex shrink-0 items-center gap-2 rounded border border-amber-700/60 bg-amber-950/30 px-3 py-2 text-sm font-medium text-amber-300 transition-colors hover:bg-amber-900/40 hover:border-amber-600 disabled:opacity-50"
            >
              <BookOpen className={`h-4 w-4 ${workbookExporting ? "animate-pulse" : ""}`} />
              <span>{workbookExporting ? "Generating…" : "Workbook PDF"}</span>
            </button>
          </TooltipTrigger>
          <TooltipContent>
            {workbookExporting ? "Generating workbook…" : "Export full lab workbook as PDF"}
          </TooltipContent>
        </Tooltip>
      </div>
      <div className="mt-6 space-y-4">
        <ExerciseList onStartExercise={handleStart} onExportExercise={handleExportExercise} />
      </div>
      <Toast toast={toast} onDismiss={() => setToast(null)} />
    </main>
  );
}
