// @vitest-environment jsdom
import { act, render } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import TerminalInner from "./terminal-inner";

const { fit, refresh } = vi.hoisted(() => ({ fit: vi.fn(), refresh: vi.fn() }));
vi.mock("@xterm/xterm", () => ({
  Terminal: class {
    cols = 80;
    rows = 24;
    loadAddon() {}
    open() {}
    writeln() {}
    onData() {}
    dispose() {}
    refresh = refresh;
  },
}));
vi.mock("@xterm/addon-fit", () => ({ FitAddon: class { fit = fit; } }));

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

it("refits and resizes the remote PTY on container changes without a window resize", () => {
  let resize: ResizeObserverCallback = () => {};
  const observe = vi.fn(), disconnect = vi.fn(), send = vi.fn();
  vi.stubGlobal("ResizeObserver", class {
    constructor(callback: ResizeObserverCallback) { resize = callback; }
    observe = observe;
    disconnect = disconnect;
  });
  vi.stubGlobal("IntersectionObserver", class {
    observe() {}
    disconnect() {}
  });
  vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => { callback(0); return 1; });
  vi.stubGlobal("WebSocket", class {
    static OPEN = 1;
    readyState = 1;
    send = send;
    close() {}
  });
  let height = 240;
  vi.spyOn(HTMLElement.prototype, "offsetWidth", "get").mockReturnValue(400);
  vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockImplementation(() => height);
  const tree = render(<TerminalInner nodeId="rtu" />);
  expect(observe).toHaveBeenCalledWith(tree.container.firstElementChild);
  fit.mockClear();

  // ResizeObserver is the real terminal's response to the inspector flexing.
  const resizeContainer = () => act(() => resize([], {} as ResizeObserver));
  height = 500;
  resizeContainer();
  expect(fit).toHaveBeenCalledTimes(1);
  expect(send).toHaveBeenLastCalledWith(JSON.stringify({ type: "resize", cols: 80, rows: 24 }));
  expect(refresh).toHaveBeenLastCalledWith(0, 23);
  height = 0;
  resizeContainer();
  expect(fit).toHaveBeenCalledTimes(1);
  height = 320;
  resizeContainer();
  expect(fit).toHaveBeenCalledTimes(2);
  tree.unmount();
  expect(disconnect).toHaveBeenCalledTimes(1);
});
