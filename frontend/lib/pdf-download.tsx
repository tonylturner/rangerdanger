"use client";

// Browser helpers that take a react-pdf Document element, render it to
// a Blob, and trigger a file download. Dynamic-imported to keep the
// ~165KB @react-pdf/renderer runtime out of the main bundle until a
// student actually clicks a PDF button.

import React from "react";
import type { DocumentProps } from "@react-pdf/renderer";

async function renderAndDownload(
  element: React.ReactElement<DocumentProps>,
  filename: string,
): Promise<void> {
  const { pdf } = await import("@react-pdf/renderer");
  const blob = await pdf(element).toBlob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

export async function downloadExercisePDF(scenarioId: string, scenarioName: string): Promise<void> {
  const [{ listScenarios }, { ExercisePDF }] = await Promise.all([
    import("./api"),
    import("./exercise-pdf"),
  ]);
  const res = await listScenarios("substation-segmentation");
  const scenario = res.scenarios.find((s) => s.id === scenarioId);
  if (!scenario) throw new Error(`Exercise ${scenarioId} not found`);
  const safeName = scenarioName.replace(/[^a-z0-9-]+/gi, "-").toLowerCase();
  await renderAndDownload(
    <ExercisePDF scenario={scenario} />,
    `rangerdanger-${safeName}.pdf`,
  );
}

export async function downloadWorkbookPDF(): Promise<void> {
  const [{ listScenarios }, { WorkbookPDF }] = await Promise.all([
    import("./api"),
    import("./exercise-pdf"),
  ]);
  const res = await listScenarios("substation-segmentation");
  const generatedAt = new Date().toLocaleString();
  await renderAndDownload(
    <WorkbookPDF scenarios={res.scenarios} generatedAt={generatedAt} />,
    "rangerdanger-workbook.pdf",
  );
}
