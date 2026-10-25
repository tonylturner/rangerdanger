import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, create, type ReactTestRenderer } from "react-test-renderer";
import { memoryStorage } from "../lib/memory-storage.test-util";
import { sendLabControl } from "../lib/api";
import { LoadSimulator } from "./load-simulator";

const { telemetry } = vi.hoisted(() => ({
  telemetry: { data: { electrical: { general_load_kw: 500, critical_load_kw: 200, power_factor: 0.93 } } },
}));
vi.mock("../lib/live-queries", () => ({ useSubstationState: () => telemetry }));
vi.mock("../lib/api", () => ({ sendLabControl: vi.fn() }));

let tree: ReactTestRenderer;
const button = (label: string) => tree.root.findAllByType("button").find((b) => b.children.includes(label))!;
const slider = (label = "GENERAL LOAD") => tree.root.findByProps({ "aria-label": label });
const change = (value: number, label?: string) => act(() => slider(label).props.onChange({ target: { value: String(value) } }));
const reset = async () => {
  act(() => button("Reset lab").props.onClick());
  await act(async () => { await button("Reset").props.onClick(); });
};

beforeEach(() => {
  telemetry.data.electrical = { general_load_kw: 500, critical_load_kw: 200, power_factor: 0.93 };
  vi.useFakeTimers();
  vi.mocked(sendLabControl).mockReset().mockResolvedValue({ result: "ok" });
  vi.stubGlobal("sessionStorage", memoryStorage());
  vi.stubGlobal("window", { addEventListener: vi.fn(), removeEventListener: vi.fn() });
  act(() => { tree = create(<LoadSimulator />); });
  act(() => tree.root.findByProps({ "aria-label": "Expand" }).props.onClick());
});
afterEach(() => {
  act(() => tree.unmount());
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("Load Simulator", () => {
  it("resets all sliders even when cached telemetry has not changed", async () => {
    change(90);
    change(80, "CRITICAL LOAD");
    change(85, "POWER FACTOR");
    expect(slider().props.value).toBe(90);
    await reset();
    expect(slider().props.value).toBe(71);
    expect(slider("CRITICAL LOAD").props.value).toBe(67);
    expect(slider("POWER FACTOR").props.value).toBe(93);
    await act(async () => { vi.advanceTimersByTime(500); });
    const calls = vi.mocked(sendLabControl).mock.calls.map(([body]) => body);
    expect(calls.at(-1)).toMatchObject({ active: false, audit_command: "reset_lab" });
    expect(calls.filter((body) => body.active === true)).toHaveLength(1);
    telemetry.data.electrical = { general_load_kw: 495, critical_load_kw: 198, power_factor: 0.92 };
    act(() => tree.update(<LoadSimulator />));
    expect(slider().props.value).toBe(70);
    expect(slider("CRITICAL LOAD").props.value).toBe(66);
  });

  it.each(["ArrowRight", "Home", "End", "PageDown"])("audits keyboard %s changes exactly once", async (key) => {
    change(65);
    act(() => slider().props.onKeyUp({ key }));
    act(() => slider().props.onBlur());
    await act(async () => {});
    const audits = vi.mocked(sendLabControl).mock.calls.filter(([body]) => body.audit_command === "set_load");
    expect(audits).toHaveLength(1);
    expect(audits[0][0]).toMatchObject({ audit_target: "general_load", audit_detail: "460 kW" });
  });

  it("does not claim a successful reset or change sliders after a failed reset", async () => {
    change(90);
    await act(async () => {});
    vi.mocked(sendLabControl).mockRejectedValueOnce(new Error("reset unavailable"));
    await reset();
    expect(slider().props.value).toBe(90);
    expect(tree.root.findByProps({ role: "alert" }).children).toContain("reset unavailable");
    expect(tree.root.findByProps({ role: "dialog" })).toBeDefined();
    expect(JSON.stringify(tree.toJSON())).not.toContain("Reset to baseline");
  });

  it("waits for an in-flight override before resetting and drops queued writes on unmount", async () => {
    let finish!: (value: { result: string }) => void;
    vi.mocked(sendLabControl).mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
    change(80);
    await act(async () => {});
    act(() => slider().props.onKeyUp({ key: "ArrowRight" }));
    act(() => button("Reset lab").props.onClick());
    let resetting!: Promise<void>;
    act(() => { resetting = button("Reset").props.onClick(); });
    expect(sendLabControl).toHaveBeenCalledTimes(1);
    act(() => tree.unmount());
    await act(async () => { finish({ result: "ok" }); await resetting; });
    expect(sendLabControl).toHaveBeenCalledTimes(1);
  });
});
