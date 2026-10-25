"use client";

import { useState } from "react";

import { runValidationReport, type ValidationReport, type PlanExpectation } from "../lib/api";
import { tupleLabel } from "../lib/plan-presentation";
import { isRangeNotReady } from "../lib/range";
import { MarkdownProse } from "./markdown-prose";

// ValidationReportPanel - rendered inline in Lab 2.4 via the
// :::validation-report directive. The button runs the positive/negative
// segmentation test matrix against the CURRENTLY ACTIVE policy (the
// backend never re-applies, so it validates what the student built),
// captures PCAP, and renders the resulting markdown evidence report.
export function ValidationReportPanel({ expectations = [] }: { expectations?: PlanExpectation[] }) {
  const [loading, setLoading] = useState(false);
  const [report, setReport] = useState<ValidationReport | null>(null);
  const [error, setError] = useState<Error | null>(null);

  const generate = async () => {
    setLoading(true);
    setError(null);
    try {
      setReport(await runValidationReport());
    } catch (e) {
      setError(e instanceof Error ? e : new Error("Failed to generate report"));
    } finally {
      setLoading(false);
    }
  };

  const pass = report?.acceptance.pass;
  const tuple = (id: string) => {
    const row = expectations.find((r) => r.id === id);
    return row ? tupleLabel(row) : id;
  };

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
            assembles the change-board evidence - the work an operator would
            do by hand after a segmentation change.
          </div>
        </div>
        <button
          onClick={generate}
          disabled={loading}
          className="shrink-0 rounded-sm border border-sky-700 bg-sky-950/50 px-4 py-2 text-xs font-medium text-sky-300 hover:bg-sky-900/60 disabled:opacity-50"
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
          Probing the package proof flows and capturing PCAP - this takes a few seconds.
        </div>
      )}
      {error && (
        <div className={`mt-3 text-xs ${isRangeNotReady(error) ? "text-slate-400" : "text-red-400"}`}>{error.message}</div>
      )}

      {report && !loading && (
        <div className="mt-4">
          <div
            className={`mb-3 inline-flex items-center gap-2 rounded px-2 py-1 text-xs font-bold uppercase tracking-wider ${
              pass
                ? "bg-emerald-950/60 text-emerald-300"
                : "bg-red-950/60 text-red-300"
            }`}
          >
            Hardened acceptance: {pass ? "PASS" : "FAIL"}
            <span className="font-normal normal-case text-slate-400">
              authorized {report.summary.authorized_pass}/
              {report.summary.authorized_total} · unauthorized{" "}
              {report.summary.unauthorized_pass}/
              {report.summary.unauthorized_total}
            </span>
          </div>
          <div className="mb-3 text-xs text-slate-300">
            <p>Applied plan proof: {report.planProof.applicable ? report.planProof.pass ? "PASS" : "FAIL" : "not applicable (active policy is not a plan)"}</p>
            {report.planProof.applicable && <p>Plan proof hash: <span className="break-all font-mono">{report.planProof.policyHash}</span>. Hash stable: {report.planProof.hashStable ? "yes" : "no"}; skipped: {report.planProof.skipped}</p>}
            <p>Hardened acceptance skipped: {report.acceptance.skipped}</p>
            <div className="mt-2 overflow-x-auto">
              <table className="w-full text-left text-xs">
                <caption className="text-left">Observed results against your applied plan, with Hardened as comparison</caption>
                <thead><tr><th>Tuple</th><th>Observed</th><th>Plan expected / result</th><th>Hardened expected / result</th></tr></thead>
                <tbody>{report.acceptance.rows.map((r) => {
                  const proof = report.planProof.rows.find((p) => p.id === r.id);
                  return <tr key={r.id}><td>{tuple(r.id)}</td><td>{r.observed}</td>
                    <td>{proof ? `${proof.expected} / ${proof.pass ? "PASS" : "FAIL"}` : "not applicable"}</td>
                    <td>{r.expected} / {r.pass ? "PASS" : "FAIL"}</td></tr>;
                })}</tbody>
              </table>
            </div>
            <h4 className="mt-2 font-semibold">Prevention gaps against Hardened</h4>
            {report.acceptance.preventionGaps.length ? <ul>{report.acceptance.preventionGaps.map((id) => <li key={id}>{tuple(id)}</li>)}</ul> : <p>None</p>}
          </div>
          <div className="mb-3 space-y-1 text-xs text-slate-400">
            <div>
              Policy hash:{" "}
              <code className="break-all font-mono text-slate-300">
                {report.policy_hash || "(empty)"}
              </code>
            </div>
            <div>
              Proof window (UTC):{" "}
              <time dateTime={report.window_start}>
                {new Date(report.window_start).toISOString()}
              </time>
              {" — "}
              <time dateTime={report.window_end}>
                {new Date(report.window_end).toISOString()}
              </time>
            </div>
            <div className={report.policy_stable ? "" : "text-amber-300"}>
              Policy stable during proof: {report.policy_stable ? "yes" : "no"}
            </div>
          </div>
          <div className="max-h-112 overflow-auto rounded-lg border border-slate-800 bg-slate-950/60 p-4">
            <MarkdownProse>{report.markdown}</MarkdownProse>
          </div>
        </div>
      )}
    </div>
  );
}
