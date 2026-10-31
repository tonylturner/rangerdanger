// @vitest-environment jsdom
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { type ValidationReport } from "../lib/api";
import { US_PLANS } from "../lib/plan-fixtures.test-util";
import { ValidationReportPanel } from "./validation-report-panel";

const report: ValidationReport = {
  planProof: { applicable: false, pass: false, policyHash: "", hashStable: false, skipped: 0, rows: [] },
  acceptance: { pass: true, skipped: 0, rows: [], preventionGaps: [] },
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

afterEach(() => {
  vi.unstubAllGlobals();
});

async function generate(answer: ValidationReport) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(answer))));
  render(<ValidationReportPanel expectations={US_PLANS[0].expectations} />);
  fireEvent.click(screen.getByRole("button", { name: "Generate Validation Report" }));
  await screen.findByText(answer.markdown);
}

function text() {
  return document.body.textContent ?? "";
}

describe("validation proof metadata", () => {
  it("shows the full hash in mono and the proof window in UTC", async () => {
    await generate(report);
    const hash = document.querySelector("code")!;
    expect(hash.textContent).toBe(report.policy_hash);
    expect(hash.className).toContain("font-mono");
    expect(hash.className).toContain("break-all");
    expect([...document.querySelectorAll("time")].map((time) => time.textContent)).toEqual([
      "2026-10-18T12:00:00.000Z",
      "2026-10-18T12:00:09.000Z",
    ]);
    expect(text()).toContain("Proof window (UTC):");
    const stable = [...document.querySelectorAll("div")].find((node) => node.textContent?.startsWith("Policy stable during proof: "));
    expect(stable?.textContent).toBe("Policy stable during proof: yes");
    expect(stable?.className).not.toMatch(/amber|red|emerald/);
    expect(text()).toContain(report.markdown);
  });

  it("warns in amber without turning a passing report into a failure", async () => {
    await generate({ ...report, policy_stable: false });
    const warning = [...document.querySelectorAll("div")].find((node) => node.className === "text-amber-300");
    expect(warning?.textContent).toBe("Policy stable during proof: no");
    expect(text()).toContain("PASS");
    expect(text()).not.toContain("FAIL");
    expect(text()).toContain(report.markdown);
  });

  it("keeps a US report visible with an empty hash and unstable policy", async () => {
    await generate({ ...report, active_config: "improved", policy_source: "hardened-reference", policy_hash: "", policy_stable: false });
    expect(document.querySelector("code")?.textContent).toBe("(empty)");
    expect(text()).toContain(report.markdown);
    expect(text()).toContain("PASS");
  });

  it("preserves a backend failure independently of the stability warning", async () => {
    await generate({ ...report, acceptance: { ...report.acceptance, pass: false }, policy_stable: false });
    expect(text()).toContain("FAIL");
    expect(document.querySelector(".text-amber-300")).not.toBeNull();
    expect(text()).toContain(report.markdown);
  });
  it("separates a passing Weak plan proof from failed Hardened acceptance and lists tuple gaps", async () => {
    await generate({ ...report,
      planProof: { applicable: true, pass: true, policyHash: "applied-hash", hashStable: true, skipped: 0, rows: [{ id: "U12", expected: "allow", observed: "allow", pass: true }] },
      acceptance: { pass: false, skipped: 0, rows: [{ id: "U12", expected: "deny", observed: "allow", pass: false }], preventionGaps: ["U12"] },
    });
    const out = text();
    expect(out).toContain("Applied plan proof: ");
    expect(out).toContain("applied-hash");
    expect(out).toContain("Hardened acceptance: ");
    expect(out).toContain("allow / PASS");
    expect(out).toContain("deny");
    expect(out).toContain("Prevention gaps against Hardened");
    expect(out).toContain("U12: kali-1");
    expect(out).toContain("10.10.10.50");
    expect(out).toContain("10.20.20.10");
    expect(out).toContain("TCP/3389");
  });
  it("shows skipped and unstable proof failures without calling a manual policy a plan", async () => {
    await generate({ ...report, acceptance: { pass: false, skipped: 1, rows: [{ id: "U1", expected: "deny", observed: "skipped", pass: false }], preventionGaps: [] } });
    expect(text()).toContain("not applicable (active policy is not a plan)");
    expect(text()).toContain("skipped");
    expect(text()).toContain("FAIL");
  });

});
