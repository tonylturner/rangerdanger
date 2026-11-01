// @vitest-environment jsdom
import { fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { Scenario, CurriculumBinding, CompiledPlan } from "../lib/api";
import { memoryStorage } from "../lib/memory-storage.test-util";
import { loadProgress, saveProgress } from "../lib/scenario-runner-storage";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { resolve } from "node:path";
import { fixtureCurriculum, US_CURRICULUM } from "../lib/curriculum-storage.test-util";
import { EU_PLANS } from "../lib/plan-fixtures.test-util";
import * as api from "../lib/api";
import { ScenarioRunner } from "./scenario-runner";

const { scope, compiled } = vi.hoisted(() => ({
  scope: { packageId: "us-dnp3-substation", revision: 1, curriculum: undefined as CurriculumBinding | undefined },
  compiled: { data: undefined as CompiledPlan | undefined },
}));
vi.mock("../lib/use-compiled-plan", () => ({ useCompiledPlan: () => compiled }));
vi.mock("../lib/curriculum-scope", () => ({ useCurriculumScope: () => scope }));
vi.mock("../lib/use-firewall-track", () => ({ useFirewallTrack: () => ({ track: "guided", setTrack: vi.fn() }) }));
vi.mock("../lib/operator-queries", () => ({
  useOperatorView: () => ({ status: "loading" }), usePackagePolicies: () => undefined,
}));
vi.mock("../lib/live-queries", async (importOriginal) => ({
  ...await importOriginal<typeof import("../lib/live-queries")>(),
  useActiveFirewall: () => ({}), useSubstationState: () => ({}),
  useSubstationAudit: () => ({}), useRefreshLive: () => vi.fn(),
}));
vi.mock("./runner-operator-status", () => ({ RunnerOperatorStatus: () => null }));

vi.mock("./scenario-runner-panels", () => ({
  CommandAuditPanels: () => null, StepNavigator: () => null, TerminalPanel: () => null,
}));
vi.mock("./ui/tooltip", () => ({
  Tooltip: ({ children }: { children: ReactNode }) => children,
  TooltipTrigger: ({ children }: { children: ReactNode }) => children,
  TooltipContent: () => null,
}));

const scenario: Scenario = {
  id: "reset-fixture", order: "1.2", name: "Reset fixture", description: "Test",
  package_id: "fixture", lab_template_id: "fixture", validator: "", tags: [], nodes: [],
  steps: [
    { id: "first", title: "First instruction", description: "Start here", action: { type: "decision" } },
    { id: "second", title: "Second instruction", description: "Continue", action: { type: "manual" } },
    { id: "third", title: "Third instruction", description: "Run this", action: { type: "probe" } },
  ],
};

const originalStorage = Object.getOwnPropertyDescriptor(window, "localStorage");
afterEach(() => {
  scope.curriculum = undefined;
  compiled.data = undefined;
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  if (originalStorage) Object.defineProperty(window, "localStorage", originalStorage);
  else Reflect.deleteProperty(window, "localStorage");
});

describe("Reset Progress", () => {
  it.each(["us-dnp3-substation", "eu-iec104-substation"])(
    "returns %s from Summary to the first usable step and preserves other records", async (packageId) => {
      scope.packageId = packageId;
      Object.defineProperty(window, "localStorage", { configurable: true, value: memoryStorage() });
      vi.spyOn(window, "confirm").mockReturnValue(true);
      const client = new QueryClient();
      saveProgress(scope, scenario.id, { completedStepIds: ["first", "second"], notes: "old", cmdLog: ["old"] });
      saveProgress(scope, "another", { completedStepIds: ["kept"], notes: "keep", cmdLog: [] });
      const tree = render(<QueryClientProvider client={client}><ScenarioRunner scenario={scenario} onExit={() => {}} /></QueryClientProvider>);
      const click = (label: string) => fireEvent.click(tree.getByRole("button", { name: label }));
      try {
        expect(tree.queryByRole("button", { name: "Execute Step" })).toBeNull();
        expect(tree.container.textContent).toContain("Mark Complete is self-attested; it does not run a system check.");
        click("View Summary");
        expect(tree.container.textContent).toContain("Steps marked complete");
        expect(tree.container.textContent).toContain("Mark Complete is self-attested; it does not run a system check.");
        click("Reset Progress");
        expect(tree.container.textContent).not.toContain("Exercise Summary");
        expect(tree.container.innerHTML).toContain("First instruction");
        expect(tree.queryByRole("button", { name: "Execute Step" })).toBeNull();
        expect((tree.getByRole("textbox") as HTMLTextAreaElement).value).toBe("");
        expect(loadProgress(scope, scenario)).toEqual({ completedStepIds: [], notes: "", cmdLog: [] });
        expect(loadProgress(scope, { id: "another", steps: [{ id: "kept", title: "", description: "" }] }).notes).toBe("keep");
        fireEvent.click(tree.getByRole("button", { name: /Mark Complete/ }));
        expect(tree.container.innerHTML).toContain("Second instruction");
        expect(tree.queryByRole("button", { name: "Execute Step" })).toBeNull();
        fireEvent.click(tree.getByRole("button", { name: /Mark Complete/ }));
        expect(tree.container.innerHTML).toContain("Third instruction");
        await waitFor(() => expect(tree.getByRole("button", { name: "Execute Step" })).toBeTruthy());
      } finally {
        client.clear();
      }
    },
  );
});

const { load } = createRequire(import.meta.url)("js-yaml") as { load: (text: string) => unknown };
const EU = fixtureCurriculum("lab-definitions/packages/eu-iec104-substation");
function authoredScenario(name: string, eu = true): Scenario {
  const directory = eu ? "lab-definitions/packages/eu-iec104-substation/scenarios" : "lab-definitions/scenarios";
  return load(readFileSync(resolve(__dirname, "../..", directory, `${name}.yml`), "utf8")) as Scenario;
}

function renderStep(authored: Scenario, stepId: string, binding: CurriculumBinding) {
  scope.packageId = binding === EU ? "eu-iec104-substation" : "us-dnp3-substation";
  scope.curriculum = binding;
  Object.defineProperty(window, "localStorage", { configurable: true, value: memoryStorage() });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const step = authored.steps.find((s) => s.id === stepId);
  if (!step) throw new Error(`Missing authored step ${stepId}`);
  const tree = render(<QueryClientProvider client={client}><ScenarioRunner scenario={{ ...authored, steps: [step] }} onExit={() => {}} /></QueryClientProvider>);
  return { ...tree, close: () => { tree.unmount(); client.clear(); } };
}

describe("authored description → Run → node exec", () => {
  it("keeps the host reconnect copy-only and runs the CC reconnect on cc-1", async () => {
    const exec = vi.spyOn(api, "execOnNode").mockResolvedValue({ exit_code: 0, stdout: "ok", stderr: "", duration_ms: 1 });
    const tree = renderStep(authoredScenario("eu-firewall-implementation"), "phase-4-enable-logging-and-visibility", EU);
    try {
      const host = tree.getByText("curl -fsS -X POST http://localhost:8088/api/operator/reconnect").parentElement!;
      expect(host.querySelectorAll("button")).toHaveLength(1);
      expect(host.textContent).not.toContain("Run");
      expect(tree.getAllByRole("button", { name: "Run" })).toHaveLength(1);
      fireEvent.click(tree.getByRole("button", { name: "Run" }));
      await waitFor(() => expect(exec).toHaveBeenCalledWith("cc-1", "wget -qO- --post-data '' http://10.99.99.21:8080/api/reconnect", 30));
    } finally { tree.close(); }
  });

  it.each([false, true])("routes both positive checks with dynamic plan=%s", async (dynamic) => {
    if (dynamic) compiled.data = EU_PLANS[1];
    const exec = vi.spyOn(api, "execOnNode").mockResolvedValue({ exit_code: 0, stdout: "ok", stderr: "", duration_ms: 1 });
    const tree = renderStep(authoredScenario("eu-firewall-implementation"), "test-positive", EU);
    try {
      for (const [index, node, url] of [[0, "kali-1", "http://10.70.70.40:8080/api/points"], [1, "eng-ws-1", "http://10.30.30.30:8080/login"]] as const) {
        fireEvent.click(tree.getAllByRole("button", { name: "Run" })[index]);
        await waitFor(() => expect(exec).toHaveBeenCalledWith(node, `curl -fsS --max-time 3 ${url}`, 30));
        await waitFor(() => expect(tree.queryByRole("button", { name: "Running..." })).toBeNull());
      }
    } finally { tree.close(); }
  });

  it("overrides the vendor retest step's Kali node only for the field write", async () => {
    const exec = vi.spyOn(api, "execOnNode").mockResolvedValue({ exit_code: 0, stdout: "ok", stderr: "", duration_ms: 1 });
    const tree = renderStep(authoredScenario("eu-vendor-rdp-compromise"), "re-attempt-the-pivot", EU);
    try {
      fireEvent.click(tree.getAllByRole("button", { name: "Run" })[0]);
      await waitFor(() => expect(exec).toHaveBeenCalledWith("kali-1", "nmap -Pn -p 3389,5900 10.20.20.10 --max-retries 1 -T4", 30));
      await waitFor(() => expect(tree.queryByRole("button", { name: "Running..." })).toBeNull());
      fireEvent.click(tree.getAllByRole("button", { name: "Run" })[1]);
      await waitFor(() => expect(exec).toHaveBeenCalledWith("vendor-jump-1", "mbpoll -m tcp -a 1 -t 4 -0 -r 0 -1 10.40.40.21 1", 30));
    } finally { tree.close(); }
  });

  it("runs the authored bounded capture on the firewall", async () => {
    const exec = vi.spyOn(api, "execOnNode").mockResolvedValue({ exit_code: 0, stdout: "ok", stderr: "", duration_ms: 1 });
    const tree = renderStep(authoredScenario("eu-baseline-assessment"), "capture-traffic-at-the-firewall", EU);
    try {
      fireEvent.click(tree.getByRole("button", { name: "Run" }));
      await waitFor(() => expect(exec).toHaveBeenCalledWith("fw-1", "tcpdump -i any -nn 'not host 127.0.0.1' -G 12 -W 1 -w /data/captures/baseline.pcap", 30));
    } finally { tree.close(); }
  });

  it.each([
    ["explicit step node", "A plain description", "cc-1", ["kali-1"], "cc-1"],
    ["prose inference", "Inspect RTU telemetry", undefined, ["kali-1"], "rtu-1"],
    ["curriculum primary", "A plain description", undefined, ["cc-1"], "kali-1"],
    ["first exercise node", "A plain description", undefined, ["cc-1"], "cc-1"],
  ] as const)("preserves %s fallback without a marker", async (label, description, node, nodes, expected) => {
    const exec = vi.spyOn(api, "execOnNode").mockResolvedValue({ exit_code: 0, stdout: "ok", stderr: "", duration_ms: 1 });
    const authored = { ...scenario, id: label === "first exercise node" ? "unknown" : "eu-firewall-implementation", nodes: [...nodes], steps: [{ id: "fallback", title: "Fallback", node, description: `${description}\n    curl endpoint` }] };
    const tree = renderStep(authored, "fallback", EU);
    try {
      fireEvent.click(tree.getByRole("button", { name: "Run" }));
      await waitFor(() => expect(exec).toHaveBeenCalledWith(expected, "curl endpoint", 30));
    } finally { tree.close(); }
  });

  it("passes inherited and overridden sources through track commands and hints", async () => {
    const exec = vi.spyOn(api, "execOnNode").mockResolvedValue({ exit_code: 0, stdout: "ok", stderr: "", duration_ms: 1 });
    const authored = { ...scenario, nodes: ["kali-1"], steps: [{ id: "nested", title: "Nested", node: "rtu-1", description: "From the cc-1 terminal:\n:::guided\n    wget inherited\n:::\n:::hint More\nFrom eng-ws:\n    curl override\n:::" }] };
    const tree = renderStep(authored, "nested", EU);
    try {
      fireEvent.click(tree.getByRole("button", { name: "Run" }));
      await waitFor(() => expect(exec).toHaveBeenCalledWith("cc-1", "wget inherited", 30));
      await waitFor(() => expect(tree.queryByRole("button", { name: "Running..." })).toBeNull());
      fireEvent.click(tree.getByRole("button", { name: /More/ }));
      fireEvent.click(tree.getAllByRole("button", { name: "Run" })[1]);
      await waitFor(() => expect(exec).toHaveBeenCalledWith("eng-ws-1", "curl override", 30));
    } finally { tree.close(); }
  });

  it("uses US multi-source markers inside a real hint, including a wrapped header", async () => {
    const exec = vi.spyOn(api, "execOnNode").mockResolvedValue({ exit_code: 0, stdout: "ok", stderr: "", duration_ms: 1 });
    const authored = authoredScenario("validation-evidence", false);
    const step = authored.steps.find((s) => s.description.includes("Advanced: run the positive tests by hand"))!;
    const tree = renderStep(authored, step.id, US_CURRICULUM);
    try {
      fireEvent.click(tree.getByRole("button", { name: /Advanced: run the positive tests by hand/ }));
      for (const [cmd, node] of [["mbpoll -a 1 -r 1 -c 4 -1 -t 0 10.40.40.20", "rtac-1"], ["curl -ks https://10.30.30.20/ -o /dev/null -w '%{http_code}\\n'", "vendor-jump-1"]]) {
        const block = tree.getByText(cmd).parentElement!;
        fireEvent.click(Array.from(block.querySelectorAll("button")).find((b) => b.textContent === "Run")!);
        await waitFor(() => expect(exec).toHaveBeenCalledWith(node, cmd, 30));
        await waitFor(() => expect(tree.queryByRole("button", { name: "Running..." })).toBeNull());
      }
    } finally { tree.close(); }
  });
});

describe("validation is separate from self-attested progress", () => {
  it.each(["us-dnp3-substation", "eu-iec104-substation"])("PASS preserves untouched steps in %s", async (packageId) => {
    scope.packageId = packageId;
    Object.defineProperty(window, "localStorage", { configurable: true, value: memoryStorage() });
    saveProgress(scope, scenario.id, { completedStepIds: ["first"], notes: "kept", cmdLog: [] });
    vi.spyOn(api, "validateScenario").mockResolvedValue({
      scenario_id: scenario.id, outcome: "PASS", timestamp: "2026-11-01T00:00:00Z",
      checks: [{ name: "Current state", status: "pass", detail: "Bounded check passed" }],
    });
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const tree = render(<QueryClientProvider client={client}><ScenarioRunner scenario={{ ...scenario, validator: "state" }} onExit={() => {}} /></QueryClientProvider>);
    try {
      fireEvent.click(tree.getByRole("button", { name: "Validate Exercise" }));
      await waitFor(() => expect(tree.container.textContent).toContain("PASS"));
      expect(tree.container.textContent).toContain("Bounded check passed");
      expect(loadProgress(scope, scenario).completedStepIds).toEqual(["first"]);
      fireEvent.click(tree.getByRole("button", { name: "View Summary" }));
      expect(tree.container.textContent).toContain("1/3 steps marked complete");
    } finally { tree.unmount(); client.clear(); }
  });
});
