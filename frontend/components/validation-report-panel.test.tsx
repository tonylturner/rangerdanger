import { afterEach, describe, expect, it, vi } from "vitest";
import { act, create, type ReactTestRenderer } from "react-test-renderer";
import { type ValidationReport } from "../lib/api";
import { ValidationReportPanel } from "./validation-report-panel";

const report: ValidationReport = {
  markdown: "Segmentation evidence remains visible.",
  summary: {
    authorized_pass: 2,
    authorized_total: 2,
    unauthorized_pass: 3,
    unauthorized_total: 3,
    result: "PASS",
  },
  active_config: "custom",
  policy_source: "manual-custom",
  pcap_path: "/captures/validation.pcap",
  policy_hash: "sha256:" + "abcdef0123456789".repeat(4),
  window_start: "2026-10-18T08:00:00-04:00",
  window_end: "2026-10-18T08:00:09-04:00",
  policy_stable: true,
};

let panel: ReactTestRenderer;
afterEach(() => {
  act(() => panel?.unmount());
  vi.unstubAllGlobals();
});

async function generate(answer: ValidationReport) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(answer))));
  act(() => { panel = create(<ValidationReportPanel />); });
  await act(async () => { await panel.root.findByType("button").props.onClick(); });
}

function text() {
  return JSON.stringify(panel.toJSON());
}

describe("validation proof metadata", () => {
  it("shows the full hash in mono and the proof window in UTC", async () => {
    await generate(report);
    const hash = panel.root.findByType("code");
    expect(hash.children).toEqual([report.policy_hash]);
    expect(hash.props.className).toContain("font-mono");
    expect(hash.props.className).toContain("break-all");
    expect(panel.root.findAllByType("time").map((time) => time.children.join(""))).toEqual([
      "2026-10-18T12:00:00.000Z",
      "2026-10-18T12:00:09.000Z",
    ]);
    expect(text()).toContain("Proof window (UTC):");
    const stable = panel.root.findAllByType("div").find((node) => node.children.includes("Policy stable during proof: "));
    expect(stable?.children).toEqual(["Policy stable during proof: ", "yes"]);
    expect(stable?.props.className).not.toMatch(/amber|red|emerald/);
    expect(text()).toContain(report.markdown);
  });

  it("warns in amber without turning a passing report into a failure", async () => {
    await generate({ ...report, policy_stable: false });
    const warning = panel.root.findAllByType("div").find((node) => node.props.className === "text-amber-300");
    expect(warning?.children).toEqual(["Policy stable during proof: ", "no"]);
    expect(text()).toContain("PASS");
    expect(text()).not.toContain("FAIL");
    expect(text()).toContain(report.markdown);
  });

  it("keeps a US report visible with an empty hash and unstable policy", async () => {
    await generate({ ...report, active_config: "improved", policy_source: "hardened-reference", policy_hash: "", policy_stable: false });
    expect(panel.root.findByType("code").children).toEqual(["(empty)"]);
    expect(text()).toContain(report.markdown);
    expect(text()).toContain("PASS");
  });

  it("preserves a backend failure independently of the stability warning", async () => {
    await generate({ ...report, summary: { ...report.summary, result: "FAIL" }, policy_stable: false });
    expect(text()).toContain("FAIL");
    expect(text()).toContain("text-amber-300");
    expect(text()).toContain(report.markdown);
  });
});
