// @vitest-environment jsdom
import { act, fireEvent, render, type RenderResult } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { memoryStorage } from "../lib/memory-storage.test-util";
import { sendLabControl } from "../lib/api";
import { LoadSimulator } from "./load-simulator";

const { telemetry } = vi.hoisted(() => ({
  telemetry: { data: { electrical: { general_load_kw: 500, critical_load_kw: 200, power_factor: 0.93 } } },
}));
const originalSessionStorage = Object.getOwnPropertyDescriptor(window, "sessionStorage");
vi.mock("../lib/live-queries", () => ({ useSubstationState: () => telemetry }));
vi.mock("../lib/api", () => ({ sendLabControl: vi.fn() }));

let tree: RenderResult;
const button = (label: string) => [...tree.container.querySelectorAll("button")].find((b) => b.textContent?.includes(label))!;
const confirmReset = () => [...tree.container.querySelectorAll('[role="dialog"] button')].find((b) => b.textContent?.trim() === "Reset")!;
const slider = (label = "GENERAL LOAD") => tree.container.querySelector<HTMLInputElement>(`input[aria-label="${label}"]`)!;
const change = (value: number, label?: string) => fireEvent.change(slider(label), { target: { value: String(value) } });
const flushMicrotasks = () => act(async () => {
  for (let i = 0; i < 10; i += 1) await Promise.resolve();
});
const reset = async () => {
  fireEvent.click(button("Reset lab"));
  fireEvent.click(confirmReset());
  await flushMicrotasks();
  expect(tree.container.querySelector('[role="dialog"]')).toBeNull();
};

beforeEach(() => {
  telemetry.data.electrical = { general_load_kw: 500, critical_load_kw: 200, power_factor: 0.93 };
  vi.useFakeTimers();
  vi.mocked(sendLabControl).mockReset().mockResolvedValue({ result: "ok" });
  const storage = memoryStorage();
  vi.stubGlobal("localStorage", memoryStorage());
  vi.stubGlobal("sessionStorage", storage);
  Object.defineProperty(window, "sessionStorage", { configurable: true, value: storage });
  tree = render(<LoadSimulator />);
  fireEvent.click(tree.getByLabelText("Expand"));
});
afterEach(() => {
  tree.unmount();
  vi.useRealTimers();
  vi.unstubAllGlobals();
  if (originalSessionStorage) Object.defineProperty(window, "sessionStorage", originalSessionStorage);
});

describe("Load Simulator", () => {
  it("restores session expansion on mount without expanding the server snapshot", () => {
    tree.unmount();
    window.sessionStorage.setItem("loadsim-expanded", "1");
    expect(renderToStaticMarkup(<LoadSimulator />)).not.toContain("Fine Tune");
    tree = render(<LoadSimulator />);
    expect(slider().value).toBe("71");
    expect(tree.getByLabelText("Collapse")).toBeTruthy();
    fireEvent.click(tree.getByLabelText("Collapse"));
    expect(window.sessionStorage.getItem("loadsim-expanded")).toBe("0");
  });

  it("follows idle telemetry before painting but retains an engaged override", () => {
    expect(slider().value).toBe("71");
    telemetry.data.electrical = { general_load_kw: 353.5, critical_load_kw: 150, power_factor: 0.85 };
    tree.rerender(<LoadSimulator />);
    expect(slider().value).toBe("50");
    expect(slider("CRITICAL LOAD").value).toBe("50");
    expect(slider("POWER FACTOR").value).toBe("85");
    change(80);
    telemetry.data.electrical = { general_load_kw: 0, critical_load_kw: 0, power_factor: 1 };
    tree.rerender(<LoadSimulator />);
    expect(slider().value).toBe("80");
    expect(slider("CRITICAL LOAD").value).toBe("50");
    expect(slider("POWER FACTOR").value).toBe("85");
  });

  it("starts a preset at the latest idle setpoints and timestamps the click", async () => {
    const frames: FrameRequestCallback[] = [];
    vi.stubGlobal("requestAnimationFrame", (frame: FrameRequestCallback) => frames.push(frame));
    vi.stubGlobal("cancelAnimationFrame", vi.fn());
    vi.stubGlobal("performance", { now: () => 0 });
    vi.setSystemTime(new Date("2026-10-28T12:00:00Z"));
    telemetry.data.electrical = { general_load_kw: 353.5, critical_load_kw: 150, power_factor: 0.85 };
    tree.rerender(<LoadSimulator />);
    fireEvent.click(button("Overnight"));
    expect(tree.container.textContent).toContain("Preset: Overnight");
    act(() => frames.shift()!(0));
    expect(slider().value).toBe("50");
    expect(slider("POWER FACTOR").value).toBe("85");
    await act(async () => { vi.advanceTimersByTime(2000); });
    const auditLine = () => [...tree.container.querySelectorAll("div")].find((n) => n.textContent?.startsWith("Last change: "))!;
    expect(auditLine().textContent).toContain("2s ago");
    fireEvent.click(button("Overnight"));
    expect(tree.container.textContent).toContain("Steady state");
    expect(auditLine().textContent).toContain("0s ago");
  });

  it("resets all sliders even when cached telemetry has not changed", async () => {
    change(90);
    change(80, "CRITICAL LOAD");
    change(85, "POWER FACTOR");
    expect(slider().value).toBe("90");
    await reset();
    expect(slider().value).toBe("71");
    expect(slider("CRITICAL LOAD").value).toBe("67");
    expect(slider("POWER FACTOR").value).toBe("93");
    await act(async () => { vi.advanceTimersByTime(500); });
    const calls = vi.mocked(sendLabControl).mock.calls.map(([body]) => body);
    expect(calls.at(-1)).toMatchObject({ active: false, audit_command: "reset_lab" });
    expect(calls.filter((body) => body.active === true)).toHaveLength(1);
    telemetry.data.electrical = { general_load_kw: 495, critical_load_kw: 198, power_factor: 0.92 };
    tree.rerender(<LoadSimulator />);
    expect(slider().value).toBe("70");
    expect(slider("CRITICAL LOAD").value).toBe("66");
  });

  it.each(["ArrowRight", "Home", "End", "PageDown"])("audits keyboard %s changes exactly once", async (key) => {
    change(65);
    fireEvent.keyUp(slider(), { key });
    fireEvent.blur(slider());
    await act(async () => {});
    const audits = vi.mocked(sendLabControl).mock.calls.filter(([body]) => body.audit_command === "set_load");
    expect(audits).toHaveLength(1);
    expect(audits[0][0]).toMatchObject({ audit_target: "general_load", audit_detail: "460 kW" });
  });

  it("does not claim a successful reset or change sliders after a failed reset", async () => {
    change(90);
    await act(async () => {});
    vi.mocked(sendLabControl).mockRejectedValueOnce(new Error("reset unavailable"));
    fireEvent.click(button("Reset lab"));
    fireEvent.click(confirmReset());
    await flushMicrotasks();
    expect(tree.container.querySelector('[role="alert"]')?.textContent).toContain("reset unavailable");
    expect(slider().value).toBe("90");
    expect(tree.container.querySelector('[role="dialog"]')).not.toBeNull();
    expect(tree.container.innerHTML).not.toContain("Reset to baseline");
  });

  it("waits for an in-flight override before resetting and drops queued writes on unmount", async () => {
    let finish!: (value: { result: string }) => void;
    vi.mocked(sendLabControl).mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
    change(80);
    await act(async () => {});
    fireEvent.keyUp(slider(), { key: "ArrowRight" });
    fireEvent.click(button("Reset lab"));
    fireEvent.click(confirmReset());
    expect(sendLabControl).toHaveBeenCalledTimes(1);
    tree.unmount();
    await act(async () => {
      finish({ result: "ok" });
      for (let i = 0; i < 10; i += 1) await Promise.resolve();
    });
    expect(sendLabControl).toHaveBeenCalledTimes(1);
  });
});
