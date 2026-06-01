"use client";

import { useState } from "react";

import { runValidationReport, type ValidationReport } from "../lib/api";
import { MarkdownProse } from "./markdown-prose";

// ValidationReportPanel — rendered inline in Lab 2.4 via the
// :::validation-report directive. The button runs the positive/negative
// segmentation test matrix against the CURRENTLY ACTIVE policy (the
// backend never re-applies, so it validates what the student built),
// captures PCAP, and renders the resulting markdown evidence report.
export function ValidationReportPanel() {
  const [loading, setLoading] = useState(false);
  const [report, setReport] = useState<ValidationReport | null>(null);
  const [error, setError] = useState<string | null>(null);

  const generate = async () => {
    setLoading(true);
    setError(null);
    try {
      setReport(await runValidationReport());
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to generate report");
    } finally {
      setLoading(false);
    }
  };

  const pass = report?.summary.result === "PASS";

  return (
    <div className="my-4 rounded-xl border border-slate-700 bg-slate-900/50 p-4">
      <div className="flex items-center justify-between gap-3">
        <div>
          <div className="text-sm font-semibold text-slate-200">
            Validation report
          </div>
          <div className="text-xs text-slate-400">
            Runs the full authorized + unauthorized test matrix against the
            policy you have active, captures PCAP at the firewall, and
            assembles the change-board evidence — the work an operator would
            do by hand after a segmentation change.
          </div>
        </div>
        <button
          onClick={generate}
          disabled={loading}
          className="shrink-0 rounded border border-sky-700 bg-sky-950/50 px-4 py-2 text-xs font-medium text-sky-300 hover:bg-sky-900/60 disabled:opacity-50"
        >
          {loading
            ? "Running tests…"
            : report
              ? "Re-run report"
              : "Generate Validation Report"}
        </button>
      </div>

      {loading && (
        <div className="mt-3 text-xs italic text-slate-500">
          Probing ~19 flows and capturing PCAP — this takes a few seconds.
        </div>
      )}
      {error && <div className="mt-3 text-xs text-red-400">{error}</div>}

      {report && !loading && (
        <div className="mt-4">
          <div
            className={`mb-3 inline-flex items-center gap-2 rounded px-2 py-1 text-xs font-bold uppercase tracking-wider ${
              pass
                ? "bg-emerald-950/60 text-emerald-300"
                : "bg-red-950/60 text-red-300"
            }`}
          >
            {report.summary.result}
            <span className="font-normal normal-case text-slate-400">
              authorized {report.summary.authorized_pass}/
              {report.summary.authorized_total} · unauthorized{" "}
              {report.summary.unauthorized_pass}/
              {report.summary.unauthorized_total}
            </span>
          </div>
          <div className="max-h-[28rem] overflow-auto rounded-lg border border-slate-800 bg-slate-950/60 p-4">
            <MarkdownProse>{report.markdown}</MarkdownProse>
          </div>
        </div>
      )}
    </div>
  );
}
