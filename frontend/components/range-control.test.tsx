// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderToStaticMarkup } from "react-dom/server";
import { US_PACKAGE, EU_PACKAGE } from "../lib/operator-fixtures.test-util";
import { ApiError, type PackageSummary } from "../lib/api";
import { RANGE_QUERY_KEY, RangeNotReadyError, type RangePhase, type RangeStatus } from "../lib/range";
import { RangeBannerView } from "./range-banner";
import { RangeConfirmDialog } from "./range-confirm-dialog";
import { RangeControl, RangeControlView, currentPackageId, rangeRequestErrorText } from "./range-control";
import { RangeProvider } from "./range-context";

const PACKAGES: PackageSummary[] = [
  { course: US_PACKAGE.course, id: "us-dnp3-substation", title: "US DNP3 Substation", revision: 3, active: true, operator_view: "us-dnp3-substation", policies: US_PACKAGE.policies },
  { course: EU_PACKAGE.course, id: "eu-iec104-substation", title: "EU IEC 104 Substation", revision: 1, active: false, operator_view: "eu-iec104", policies: EU_PACKAGE.policies },
];

function status(phase: RangePhase, extra: Partial<RangeStatus> = {}): RangeStatus {
  return {
    generation: 2,
    phase,
    package: "us-dnp3-substation",
    target: "",
    mode: "source",
    error: "",
    updated_at: "2026-10-09T00:00:00Z",
    ...extra,
  };
}

// Markup without tags, so assertions read like what a student sees.
function text(html: string): string {
  return html.replace(/<[^>]+>/g, " ").replace(/\s+/g, " ").trim();
}

function control(s: RangeStatus | undefined, opts: { expanded?: boolean; requestError?: string; pending?: boolean } = {}) {
  return renderToStaticMarkup(
    <RangeControlView
      status={s}
      packages={PACKAGES}
      expanded={opts.expanded ?? false}
      pending={opts.pending ?? false}
      requestError={opts.requestError ?? null}
      onToggle={() => {}}
      onSelect={() => {}}
    />,
  );
}

describe("RangeControlView", () => {
  it("waits for the first status", () => {
    expect(text(control(undefined))).toContain("Checking the range");
  });

  it("offers Start range when nothing runs, for the default package", () => {
    const out = text(control(status("none", { package: "" })));
    expect(out).toContain("US DNP3 Substation");
    expect(out).toContain("Not running");
    expect(out).toContain("Start range");
  });

  it("lists restart for the active package and switch for the others when ready", () => {
    const html = control(status("ready"), { expanded: true });
    const out = text(html);
    expect(out).toContain("Ready");
    expect(out).toMatch(/US DNP3 Substation Restart range/);
    expect(out).toMatch(/EU IEC 104 Substation Switch range/);
    expect(html).toContain('aria-expanded="true"');
  });

  it("shows the current step and hides the package list during a transition", () => {
    const html = control(status("starting", { package: "eu-iec104-substation" }), { expanded: true });
    const out = text(html);
    expect(out).toContain("EU IEC 104 Substation");
    expect(out).toContain("Step 3 of 4");
    expect(html).toMatch(/aria-current="step"[^>]*>.*Starting lab containers/);
    expect(out).not.toContain("Switch range");
  });

  it("names the target during preflight", () => {
    const out = text(control(status("preflight", { target: "eu-iec104-substation" })));
    expect(out).toContain("EU IEC 104 Substation");
    expect(out).toContain("Step 1 of 4");
  });

  it("shows the failure with Retry", () => {
    const out = text(control(status("failed", { error: "up: container rtac_sim is unhealthy" })));
    expect(out).toContain("Failed");
    expect(out).toContain("up: container rtac_sim is unhealthy");
    expect(out).toContain("Retry");
  });

  it("reports a preflight failure without claiming the range broke", () => {
    const out = text(control(status("ready", { error: "missing images: rangerdanger-iec104_rtu" })));
    expect(out).toContain("The last change did not start: missing images: rangerdanger-iec104_rtu");
    expect(out).toContain("The running range was not touched");
  });

  it("shows a rejected request", () => {
    expect(text(control(status("ready"), { requestError: "Another range change is already running." }))).toContain(
      "Another range change is already running.",
    );
  });
});

describe("RangeControl", () => {
  const clients: QueryClient[] = [];
  afterEach(() => {
    clients.forEach((client) => client.clear());
    clients.length = 0;
  });

  it.each([
    ["Restart range", "Restart US DNP3 Substation?"],
    ["Switch range", "Switch the range to EU IEC 104 Substation?"],
  ])("portals the %s confirmation outside a sticky sidebar", (action, title) => {
    const client = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    clients.push(client);
    client.setQueryData(RANGE_QUERY_KEY, status("ready"));
    client.setQueryData(["packages"], PACKAGES);
    const tree = render(
      <QueryClientProvider client={client}>
        <RangeProvider>
          <aside style={{ position: "sticky", top: 0 }}>
            <RangeControl />
          </aside>
        </RangeProvider>
      </QueryClientProvider>,
    );
    const sidebar = tree.container.querySelector("aside")!;
    fireEvent.click(screen.getByRole("button", { expanded: false }));
    fireEvent.click(screen.getByRole("button", { name: new RegExp(action) }));

    const dialog = screen.getByRole("dialog", { name: title });
    expect(sidebar.contains(dialog)).toBe(false);
    expect(dialog.parentElement?.parentElement).toBe(document.body);
    expect(dialog.getAttribute("aria-modal")).toBe("true");
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Cancel" }));

    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});

describe("RangeConfirmDialog", () => {
  function dialog(kind: "restart" | "switch") {
    render(
      <RangeConfirmDialog
        kind={kind}
        packageTitle="EU IEC 104 Substation"
        pending={false}
        error={null}
        onConfirm={() => {}}
        onCancel={() => {}}
      />,
    );
    return screen.getByRole("dialog").outerHTML;
  }

  it("states the consequences of a switch before it runs", () => {
    const html = dialog("switch");
    const out = text(html);
    expect(html).toContain('role="dialog"');
    expect(out).toContain("Switch the range to EU IEC 104 Substation?");
    expect(out).toContain("Every lab container restarts");
    expect(out).toContain("Open terminals close.");
    expect(out).toContain("The firewall policy resets to the EU IEC 104 Substation default");
    expect(out).toContain("progress is kept for each package");
    expect(out).toMatch(/Cancel Switch range$/);
  });

  it("titles a restart as a restart", () => {
    expect(text(dialog("restart"))).toMatch(/^Restart EU IEC 104 Substation\?.*Restart range$/);
  });

  it("keeps a pending request disabled and ignores Escape", () => {
    const onCancel = vi.fn();
    const onConfirm = vi.fn();
    render(
      <RangeConfirmDialog
        kind="restart"
        packageTitle="EU IEC 104 Substation"
        pending
        error={null}
        onConfirm={onConfirm}
        onCancel={onCancel}
      />,
    );
    for (const button of screen.getAllByRole("button")) {
      expect((button as HTMLButtonElement).disabled).toBe(true);
      fireEvent.click(button);
    }
    fireEvent.keyDown(window, { key: "Escape" });
    expect(onCancel).not.toHaveBeenCalled();
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it("shows request errors and keeps both actions available", () => {
    const onCancel = vi.fn();
    const onConfirm = vi.fn();
    render(
      <RangeConfirmDialog
        kind="switch"
        packageTitle="EU IEC 104 Substation"
        pending={false}
        error="Another range change is already running."
        onConfirm={onConfirm}
        onCancel={onCancel}
      />,
    );
    expect(screen.getByRole("dialog").textContent).toContain("Another range change is already running.");
    fireEvent.click(screen.getByRole("button", { name: "Switch range" }));
    expect(onConfirm).toHaveBeenCalledOnce();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onCancel).toHaveBeenCalledOnce();
  });
});

describe("RangeBannerView", () => {
  const banner = (s: RangeStatus) => text(renderToStaticMarkup(<RangeBannerView status={s} packages={PACKAGES} />));

  it("is silent while the range is ready", () => {
    expect(banner(status("ready"))).toBe("");
  });

  it("explains a transition calmly with its step", () => {
    const out = banner(status("configuring", { package: "eu-iec104-substation" }));
    expect(out).toContain("Bringing up EU IEC 104 Substation");
    expect(out).toContain("Step 4 of 4: Configuring routes and firewall policy.");
    expect(out).toContain("Pages reconnect on their own");
  });

  it("points at the control when nothing runs or the start failed", () => {
    expect(banner(status("none", { package: "" }))).toContain("No range is running.");
    const failed = banner(status("failed", { error: "interrupted" }));
    expect(failed).toContain("US DNP3 Substation did not start");
    expect(failed).toContain("interrupted");
  });
});

describe("helpers", () => {
  it("picks the package the range runs or would run", () => {
    expect(currentPackageId(status("none", { package: "" }), PACKAGES)).toBe("us-dnp3-substation");
    expect(currentPackageId(status("preflight", { target: "eu-iec104-substation" }), PACKAGES)).toBe("eu-iec104-substation");
    expect(currentPackageId(status("failed", { package: "eu-iec104-substation" }), PACKAGES)).toBe("eu-iec104-substation");
  });

  it("words request failures for students", () => {
    expect(rangeRequestErrorText(new ApiError(409, "busy"))).toBe("Another range change is already running.");
    expect(rangeRequestErrorText(new ApiError(422, "Run setup --add-package eu, then refresh.", [], "package_not_installed")))
      .toBe("Run setup --add-package eu, then refresh.");
    expect(rangeRequestErrorText(new ApiError(422, "", [], "package_not_installed"))).toBe("Request failed: 422");
    expect(rangeRequestErrorText(new ApiError(400, "unknown package \"x\""))).toBe('unknown package "x"');
    expect(rangeRequestErrorText(new ApiError(500, ""))).toBe("Request failed: 500");
    expect(rangeRequestErrorText(new RangeNotReadyError("starting"))).toMatch(/being replaced/);
  });
});
