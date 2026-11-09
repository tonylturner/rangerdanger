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
import { loadSavedRemediationPlan, saveRemediationPlan } from "../lib/remediation-plan";
import { EU_PLANS } from "../lib/plan-fixtures.test-util";
import * as api from "../lib/api";
import { ScenarioRunner } from "./scenario-runner";

const { scope, compiled } = vi.hoisted(() => ({
  scope: { packageId: "us-dnp3-substation", revision: 1, curriculum: undefined as CurriculumBinding | undefined },
  compiled: { data: undefined as CompiledPlan | undefined },
}));
vi.mock("../lib/use-compiled-plan", () => ({ useCompiledPlan: () => compiled }));
vi.mock("../lib/curriculum-scope", () => ({
  useCurriculumScope: () => scope,
  useActiveCurriculum: () => ({ scenarios: [] }),
}));
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

vi.mock("./scenario-runner-panels", async (importOriginal) => ({
  ...await importOriginal<typeof import("./scenario-runner-panels")>(),
  StepNavigator: () => null, TerminalPanel: () => null,
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
const EU = fixtureCurriculum("lab-definitions/packages/eu-iec104-substation");

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
      saveProgress(scope, scenario.id, { skippedStepIds: [], completedStepIds: ["first", "second"], notes: "old", cmdLog: ["old"] });
      saveProgress(scope, "another", { skippedStepIds: [], completedStepIds: ["kept"], notes: "keep", cmdLog: [] });
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
        expect(loadProgress(scope, scenario)).toEqual({ skippedStepIds: [], completedStepIds: [], notes: "", cmdLog: [] });
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

describe("empty remediation plan completion", () => {
  it.each([
    ["US", US_CURRICULUM, "us-dnp3-substation"],
    ["EU", EU, "eu-iec104-substation"],
  ] as const)("records an empty %s plan when its decision step is completed", (_, binding, packageId) => {
    scope.packageId = packageId;
    scope.revision = 1;
    scope.curriculum = binding;
    Object.defineProperty(window, "localStorage", { configurable: true, value: memoryStorage() });
    const planningScenario: Scenario = {
      ...scenario,
      id: binding.roles.planning,
      steps: [{ ...scenario.steps[0], action: { type: "decision" } }],
    };
    const client = new QueryClient();
    const tree = render(<QueryClientProvider client={client}><ScenarioRunner scenario={planningScenario} onExit={() => {}} /></QueryClientProvider>);
    try {
      expect(loadSavedRemediationPlan(scope)).toBeNull();
      fireEvent.click(tree.getByRole("button", { name: "Mark Complete" }));
      expect(loadSavedRemediationPlan(scope)).toEqual({
        exerciseId: planningScenario.id,
        selectedActionIds: [],
        savedAt: expect.any(String),
      });
    } finally {
      client.clear();
    }
  });

  it("does not replace a saved plan when the decision step is completed", () => {
    scope.packageId = "us-dnp3-substation";
    scope.revision = 1;
    scope.curriculum = US_CURRICULUM;
    Object.defineProperty(window, "localStorage", { configurable: true, value: memoryStorage() });
    const plan = {
      exerciseId: US_CURRICULUM.roles.planning,
      selectedActionIds: ["existing-action"],
      savedAt: "existing timestamp",
    };
    saveRemediationPlan(scope, plan);
    const planningScenario: Scenario = {
      ...scenario,
      id: US_CURRICULUM.roles.planning,
      steps: [{ ...scenario.steps[0], action: { type: "decision" } }],
    };
    const client = new QueryClient();
    const tree = render(<QueryClientProvider client={client}><ScenarioRunner scenario={planningScenario} onExit={() => {}} /></QueryClientProvider>);
    try {
      fireEvent.click(tree.getByRole("button", { name: "Mark Complete" }));
      expect(loadSavedRemediationPlan(scope)).toEqual(plan);
    } finally {
      client.clear();
    }
  });
});

const { load } = createRequire(import.meta.url)("js-yaml") as { load: (text: string) => unknown };
function authoredScenario(name: string, eu = true): Scenario {
  const directory = eu ? "lab-definitions/packages/eu-iec104-substation/scenarios" : "lab-definitions/scenarios";
  return load(readFileSync(resolve(__dirname, "../..", directory, `${name}.yml`), "utf8")) as Scenario;
}

function renderStep(authored: Scenario, stepId: string, binding: CurriculumBinding, cmdLog: string[] = []) {
  scope.packageId = binding === EU ? "eu-iec104-substation" : "us-dnp3-substation";
  scope.curriculum = binding;
  Object.defineProperty(window, "localStorage", { configurable: true, value: memoryStorage() });
  saveProgress(scope, authored.id, { completedStepIds: [], skippedStepIds: [], notes: "", cmdLog });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const step = authored.steps.find((s) => s.id === stepId);
  if (!step) throw new Error(`Missing authored step ${stepId}`);
  const tree = render(<QueryClientProvider client={client}><ScenarioRunner scenario={{ ...authored, steps: [step] }} onExit={() => {}} /></QueryClientProvider>);
  return { ...tree, close: () => { tree.unmount(); client.clear(); } };
}

describe("Execute Step command log", () => {
  it.each([US_CURRICULUM, EU])("logs failed checks as amber NOT MET in $roles.implementation", async (binding) => {
    const authored: Scenario = {
      ...scenario,
      steps: [{ id: "check", title: "Check breaker", description: "Read device state", action: { type: "check" } }],
    };
    const execute = vi.spyOn(api, "executeScenarioStep").mockResolvedValue({
      step_index: 0, step_title: "Check breaker", action_type: "check", success: false,
      results: [{ action: "breaker_closed", success: false, detail: "Expected false; observed true" }],
      timestamp: "2026-11-07T00:00:00Z",
    });
    const tree = renderStep(authored, "check", binding);
    try {
      fireEvent.click(tree.getByRole("button", { name: "Execute Step" }));
      const line = await tree.findByText(/^\[NOT MET\] breaker_closed: Expected false; observed true$/);
      expect(execute).toHaveBeenCalledWith(authored.id, 0);
      expect(line.classList.contains("text-amber-400")).toBe(true);
      expect(line.textContent).not.toContain("BLOCKED");
      expect(loadProgress(scope, authored).cmdLog).toEqual([line.textContent]);
      expect(loadProgress(scope, authored).completedStepIds).toEqual([]);
    } finally { tree.close(); }
  });
});

describe("authored description → Run → node exec", () => {
  it.each([US_CURRICULUM, EU])("logs package-neutral scenario generation", async (binding) => {
    vi.spyOn(api, "startTrafficGeneration").mockResolvedValue({
      status: "started", duration_sec: 45, run_id: "fixture", started_at: "2026-11-04T00:00:00Z",
    });
    vi.spyOn(globalThis, "setInterval").mockReturnValue(0 as unknown as ReturnType<typeof setInterval>);
    const authored = { ...scenario, steps: [{ ...scenario.steps[0], description: "Generate representative traffic." }] };
    const tree = renderStep(authored, "first", binding);
    try {
      fireEvent.click(tree.getByRole("button", { name: "Generate Traffic (45s)" }));
      await waitFor(() => expect(tree.container.textContent).toContain("45s of package-defined scenario flows"));
      expect(tree.container.textContent).not.toContain("Modbus, DNP3, HTTP, NTP");
    } finally { tree.close(); }
  });

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

describe("manual evidence surfaces", () => {
  it.each([false, true])("has no one-click evidence controls on hardening or validation (EU=%s)", (eu) => {
    const binding = eu ? EU : US_CURRICULUM;
    for (const role of ["hardening", "validation"] as const) {
      const authored = authoredScenario(binding.roles[role], eu);
      for (const step of authored.steps) {
        const tree = renderStep(authored, step.id, binding);
        try {
          expect(tree.queryByRole("button", { name: /Generate Validation Report/i })).toBeNull();
          expect(tree.queryByRole("link", { name: /Download markdown/i })).toBeNull();
          expect(tree.container.textContent).not.toContain(":::validation-report");
        } finally { tree.close(); }
      }
    }
  });
});

describe("validation is separate from self-attested progress", () => {
  it.each(["us-dnp3-substation", "eu-iec104-substation"])("PASS preserves untouched steps in %s", async (packageId) => {
    scope.packageId = packageId;
    Object.defineProperty(window, "localStorage", { configurable: true, value: memoryStorage() });
    saveProgress(scope, scenario.id, { skippedStepIds: [], completedStepIds: ["first"], notes: "kept", cmdLog: [] });
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

// Captured exec response bodies from the EU walkthrough (both streams retained).
const walkthroughResults = [
  {
    "stdout": "{\"event\":\"result\",\"at\":\"2026-11-02T05:22:39.609Z\",\"op\":\"dc\",\"status\":\"ok\"}\n",
    "stderr": "2026-11-02T05:22:38.742Z iec104cmd connecting to 10.30.30.20:2404 oa=1 ca=1\n2026-11-02T05:22:38.742Z iec104cmd link opened\n2026-11-02T05:22:38.742Z iec104cmd link startdt-con\n2026-11-02T05:22:38.742Z iec104cmd link started\n2026-11-02T05:22:38.742Z iec104cmd SELECT ioa=3001 val=1\n2026-11-02T05:22:38.743Z iec104cmd rx ASDU type=C_DC_NA_1(46) cot=actcon(7) neg=0 test=0 oa=1 ca=1 objects=1\n2026-11-02T05:22:38.743Z iec104cmd rx   ioa=3001 dcs=1(OFF) qu=0 se=SELECT\n2026-11-02T05:22:38.743Z iec104cmd select confirmed\n2026-11-02T05:22:38.743Z iec104cmd EXECUTE ioa=3001 val=1\n2026-11-02T05:22:39.097Z iec104cmd rx ASDU type=C_DC_NA_1(46) cot=actcon(7) neg=0 test=0 oa=1 ca=1 objects=1\n2026-11-02T05:22:39.097Z iec104cmd rx   ioa=3001 dcs=1(OFF) qu=0 se=EXECUTE\n2026-11-02T05:22:39.097Z iec104cmd execute confirmed\n2026-11-02T05:22:39.609Z iec104cmd rx ASDU type=M_DP_TB_1(31) cot=spont(3) neg=0 test=0 oa=0 ca=1 objects=1\n2026-11-02T05:22:39.609Z iec104cmd rx   ioa=1001 value=OFF q=GOOD time=2026-11-02T05:22:39.607Z\n2026-11-02T05:22:39.609Z iec104cmd rx ASDU type=M_ME_NC_1(13) cot=spont(3) neg=0 test=0 oa=0 ca=1 objects=1\n2026-11-02T05:22:39.609Z iec104cmd rx   ioa=2001 value=33.593 q=IV\n2026-11-02T05:22:39.609Z iec104cmd rx ASDU type=M_ME_NC_1(13) cot=spont(3) neg=0 test=0 oa=0 ca=1 objects=1\n2026-11-02T05:22:39.609Z iec104cmd rx   ioa=2002 value=20.500 q=IV\n2026-11-02T05:22:39.609Z iec104cmd rx ASDU type=M_ME_NC_1(13) cot=spont(3) neg=0 test=0 oa=0 ca=1 objects=1\n2026-11-02T05:22:39.609Z iec104cmd rx   ioa=2003 value=0.000 q=IV\n2026-11-02T05:22:39.609Z iec104cmd rx ASDU type=M_ME_NC_1(13) cot=spont(3) neg=0 test=0 oa=0 ca=1 objects=1\n2026-11-02T05:22:39.609Z iec104cmd rx   ioa=2004 value=0.000 q=IV\n2026-11-02T05:22:39.609Z iec104cmd rx ASDU type=M_ME_NC_1(13) cot=spont(3) neg=0 test=0 oa=0 ca=1 objects=1\n2026-11-02T05:22:39.609Z iec104cmd rx   ioa=2005 value=0.000 q=IV\n2026-11-02T05:22:39.609Z iec104cmd rx ASDU type=M_DP_TB_1(31) cot=retrem(11) neg=0 test=0 oa=0 ca=1 objects=1\n2026-11-02T05:22:39.609Z iec104cmd rx   ioa=1001 value=OFF q=GOOD time=2026-11-02T05:22:39.607Z\n2026-11-02T05:22:39.609Z iec104cmd rx ASDU type=C_DC_NA_1(46) cot=actterm(10) neg=0 test=0 oa=1 ca=1 objects=1\n2026-11-02T05:22:39.609Z iec104cmd rx   ioa=3001 dcs=1(OFF) qu=0 se=EXECUTE\n2026-11-02T05:22:39.609Z iec104cmd execute terminated (ACT-TERM) negative=0\n2026-11-02T05:22:39.610Z iec104cmd link closed\n",
    "exit_code": 0,
    "duration_ms": 898
  },
  {
    "stdout": "mbpoll 1.0-0 - ModBus(R) Master Simulator\nCopyright (c) 2015-2023 Pascal JEAN, https://github.com/epsilonrt/mbpoll\nThis program comes with ABSOLUTELY NO WARRANTY.\nThis is free software, and you are welcome to redistribute it\nunder certain conditions; type 'mbpoll -w' for details.\n\n",
    "stderr": "mbpoll: Connection failed: Connection timed out.\n",
    "exit_code": 1,
    "duration_ms": 523
  }
];

describe("Run output evidence", () => {
  it.each([EU, US_CURRICULUM])("retains both real command streams in $roles.implementation", async (binding) => {
    const authored = { ...scenario, nodes: ["kali-1"], steps: [{ id: "run", title: "Run evidence", description: "From Kali:\n    curl fixture" }] };
    const exec = vi.spyOn(api, "execOnNode");
    const tree = renderStep(authored, "run", binding);
    try {
      for (const result of walkthroughResults) {
        exec.mockResolvedValueOnce(result);
        fireEvent.click(tree.getByRole("button", { name: "Run" }));
        await waitFor(() => expect(loadProgress(scope, authored).cmdLog[0]).toContain(result.stderr));
        const entry = loadProgress(scope, authored).cmdLog[0];
        expect(entry).toBe(`[${result.exit_code === 0 ? "OK" : `EXIT ${result.exit_code}`}] completed in ${result.duration_ms}ms\n[stdout]\n${result.stdout}\n[stderr]\n${result.stderr}`);
        const displayed = tree.getByText((_, element) => element?.textContent === entry);
        expect(displayed.closest(".whitespace-pre-wrap")).not.toBeNull();
        await waitFor(() => expect(tree.queryByRole("button", { name: "Running..." })).toBeNull());
      }
    } finally { tree.close(); }
  });

  it.each([
    { stdout: Array.from({ length: 150 }, (_, i) => `line ${i}`).join("\n"), stderr: "last diagnostic" },
    { stdout: "stdout only\n\nfinal line\n", stderr: "" },
    { stdout: "", stderr: "stderr only\n" },
    { stdout: "", stderr: "" },
  ])("keeps a complete result and caps history at 100 entries %#", async (streams) => {
    const authored = { ...scenario, nodes: ["kali-1"], steps: [{ id: "run", title: "Run evidence", description: "From Kali:\n    curl fixture" }] };
    vi.spyOn(api, "execOnNode").mockResolvedValue({ ...streams, exit_code: 0, duration_ms: 1 });
    const tree = renderStep(authored, "run", EU, Array.from({ length: 100 }, (_, i) => `old ${i}`));
    try {
      fireEvent.click(tree.getByRole("button", { name: "Run" }));
      await waitFor(() => expect(loadProgress(scope, authored).cmdLog[0]).toContain("[OK]"));
      const log = loadProgress(scope, authored).cmdLog;
      expect(log).toHaveLength(100);
      expect(log[0]).toBe(["[OK] completed in 1ms", ...(streams.stdout ? [`[stdout]\n${streams.stdout}`] : []), ...(streams.stderr ? [`[stderr]\n${streams.stderr}`] : [])].join("\n"));
      expect(log[1]).toContain("[RUN on Kali]");
      expect(log.at(-1)).toBe("old 97");
      expect(tree.container.textContent).not.toContain("more lines");
    } finally { tree.close(); }
  });
});
