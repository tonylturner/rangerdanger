import { describe, expect, it } from "vitest";
import type { ReactNode } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "../lib/api";
import type { CommandResult } from "../lib/operator-api";
import { commandErrorText } from "../lib/operator-format";
import {
  AUDIT,
  FRESH_STATE,
  JOURNAL,
  EMPTY_JOURNAL,
  NOW,
  SESSIONS,
  STALE_STATE,
  TRUTH,
  commandResult,
} from "../lib/operator-fixtures.test-util";
import { RangeNotReadyError } from "../lib/range";
import { CommandPanelView, type CommandPanelState } from "./control-centre-command";
import { ControlCentreScreen } from "./control-centre-view";
import { SessionControls, SessionsTable } from "./instructor-sessions";
import { AuditTable, OperatorContrast, TruthCards } from "./instructor-view";
import { ControlCentreMiniStatus } from "./runner-operator-status";

function text(html: string): string {
  return html.replace(/<[^>]+>/g, " ").replace(/\s+/g, " ").trim();
}

function render(node: ReactNode): string {
  const client = new QueryClient();
  const html = renderToStaticMarkup(<QueryClientProvider client={client}>{node}</QueryClientProvider>);
  client.clear();
  return html;
}

function screen(props: Partial<Parameters<typeof ControlCentreScreen>[0]>): string {
  return render(
    <ControlCentreScreen
      state={FRESH_STATE}
      stateError={null}
      stateUpdatedAt={NOW - 1000}
      journal={JOURNAL}
      now={NOW}
      {...props}
    />,
  );
}

const NO_JOURNAL = { ...JOURNAL, entries: [] };

describe("ControlCentreScreen", () => {
  it("shows a started link and four good points", () => {
    const html = screen({ journal: NO_JOURNAL });
    const out = text(html);
    expect(out).toContain("STARTED");
    expect(out).toContain("COMPLETE");
    expect(out).toContain("4/4 good");
    expect(out).toContain("CLOSED");
    expect(out).toContain("28.9 A");
    expect(out).toContain("19.94 kV");
    expect(out).not.toContain("STALE");
    expect(html.match(/data-condition="good"/g)).toHaveLength(4);
  });

  it("keeps every last value on a cut link and flags it stale with reason and age", () => {
    const html = screen({ state: STALE_STATE, journal: NO_JOURNAL });
    const out = text(html);
    expect(out).toContain("IEC 104 LINK CONNECTING: telemetry is stale");
    expect(html.match(/data-condition="stale"/g)).toHaveLength(4);
    // Last values, not zero.
    expect(out).toContain("CLOSED");
    expect(out).toContain("NORMAL");
    expect(out).toContain("28.9 A");
    expect(out).toContain("19.94 kV");
    expect(out).not.toContain("0.0 A");
    expect(out).not.toContain("0.00 kV");
    expect(out.match(/last known value/g)).toHaveLength(4);
    expect(out.match(/IV NT/g)).toHaveLength(4);
    expect(out.match(/IEC 104 link to the RTU lost/g)).toHaveLength(4);
    // Times are kept and the age grows: received 12:00:00.150, now 12:00:30.
    expect(out).toContain("12:00:00.150");
    expect(out).toContain("29 s");
    expect(out).toContain("0/4 good · 4 STALE");
    // The one-line says unknown rather than open or dead.
    expect(out).toContain("⚠ STALE · last known");
    expect(html).toContain('aria-label="Feeder one-line: breaker CLOSED ⚠ STALE"');
  });

  it("marks the API as unavailable but keeps the last answer on screen", () => {
    const out = text(screen({ stateError: new ApiError(502, "control centre unreachable") }));
    expect(out).toContain("Control centre API unavailable");
    expect(out).toContain("Showing the last answer");
    expect(out).toContain("CLOSED");
  });

  it("waits while the range is not ready", () => {
    const out = text(screen({ state: undefined, stateError: new RangeNotReadyError("starting") }));
    expect(out).toContain("The control centre shows once the range is ready.");
    expect(out).not.toContain("API unavailable");
  });

  it("shows the journal newest first", () => {
    const out = text(screen({}));
    expect(out.indexOf("Feeder breaker position STALE")).toBeLessThan(out.indexOf("Link down #3: nothing received from the RTU for t1 (t1_timeout)"));
    expect(out).toContain("cache epoch a1b2c3d4");
    expect(out).toContain("seq 1–4");
  });

  it("shows an empty journal, whose sequence bounds are null", () => {
    const out = text(screen({ journal: EMPTY_JOURNAL }));
    expect(out).toContain("empty");
    expect(out).toContain("No journal entries yet.");
  });

  it("shows no local endpoint: lib60870 cannot report it", () => {
    expect(text(screen({}))).not.toContain("CC (local)");
  });
});

function panel(state: CommandPanelState, linkState: "started" | "connecting" = "started"): string {
  return renderToStaticMarkup(
    <CommandPanelView
      linkState={linkState}
      position={FRESH_STATE.points[0]}
      state={state}
      onArm={() => {}}
      onExecute={() => {}}
      onCancel={() => {}}
    />,
  );
}

describe("CommandPanelView", () => {
  const result = (status: CommandResult["status"]) => text(panel({ phase: "result", operation: "open", result: commandResult(status) }));

  it("renders a completed command with every phase", () => {
    const out = result("completed");
    expect(out).toContain("OPEN COMPLETED");
    expect(out).toMatch(/Select ✓ CONFIRMED 12:00:01\.000/);
    expect(out).toMatch(/Execute ✓ CONFIRMED 12:00:01\.100/);
    expect(out).toMatch(/Terminated ✓ ACT-TERM 12:00:01\.700/);
    expect(out).toMatch(/Feedback ✓ MATCHED · OPEN 12:00:01\.600/);
  });

  it("renders a rejected command", () => {
    const out = result("rejected");
    expect(out).toContain("OPEN REJECTED");
    expect(out).toMatch(/Execute ✗ NEGATIVE/);
    expect(out).toContain("NO ACT-TERM");
    expect(out).toContain("execute rejected (negative ACT-CON)");
  });

  it("renders a failed command", () => {
    const out = result("failed");
    expect(out).toContain("OPEN FAILED");
    expect(out).toContain("Execute not sent");
    expect(out).toContain("link lost before the execute was sent");
  });

  it("renders an unknown outcome", () => {
    const out = result("unknown");
    expect(out).toContain("OPEN UNKNOWN");
    expect(out).toMatch(/Execute \? TIMEOUT/);
    expect(out).toContain("It is not retried");
  });

  it("renders a refused request", () => {
    const out = text(panel({ phase: "error", operation: "close", message: commandErrorText(new ApiError(409, "busy")) }));
    expect(out).toContain("CLOSE NOT DONE");
    expect(out).toContain("Another command is still in progress");
  });

  it("asks before select-before-operate and disables control without a started link", () => {
    expect(text(panel({ phase: "armed", operation: "close" }))).toContain("Select then execute CLOSE on the feeder breaker?");
    const down = panel({ phase: "idle" }, "connecting");
    expect(down.match(/disabled=""/g)).toHaveLength(2);
    expect(text(down)).toContain("commands cannot be sent");
    expect(panel({ phase: "idle" })).not.toContain('disabled=""');
  });
});

describe("instructor truth", () => {
  it("shows device truth and solver output", () => {
    const out = text(renderToStaticMarkup(<TruthCards truth={TRUTH} />));
    expect(out).toMatch(/Position OPEN/);
    expect(out).toMatch(/Protection tripped YES/);
    expect(out).toMatch(/Breaker in model OPEN/);
    expect(out).toMatch(/Truth revision \/ generation 5 \/ 3141592/);
  });

  it("contrasts truth with the stale operator view", () => {
    const out = text(renderToStaticMarkup(<OperatorContrast truth={TRUTH} operator={STALE_STATE} />));
    expect(out).toContain("Breaker position OPEN CLOSED ⚠ STALE");
    expect(out).toContain("Protection trip TRIPPED NORMAL ⚠ STALE");
  });

  it("lists 2404 sessions, marks the control centre's and offers a kill per row", () => {
    const html = renderToStaticMarkup(<SessionsTable sessions={SESSIONS.sessions} pending={false} onKill={() => {}} />);
    const out = text(html);
    expect(out).toContain("tcp 10.60.60.20:40312 → 10.30.30.20:2404 CONTROL CENTRE ESTABLISHED 431999 s");
    expect(out).toContain("tcp 10.10.10.50:51000 → 10.30.30.20:2404 ESTABLISHED 431980 s");
    expect(out.match(/CONTROL CENTRE/g)).toHaveLength(1);
    expect(out.match(/Kill/g)).toHaveLength(2);
  });

  it("cannot kill a listed session without both ports", () => {
    const partial = { ...SESSIONS.sessions[1], sport: null, state: null };
    const html = renderToStaticMarkup(<SessionsTable sessions={[partial]} pending={false} onKill={() => {}} />);
    expect(text(html)).toContain("tcp 10.10.10.50:? → 10.30.30.20:2404 —");
    expect(html.match(/disabled=""/g)).toHaveLength(1);
  });

  it("previews the control centre's session the cut would delete", () => {
    expect(text(render(<SessionControls sessions={SESSIONS.sessions} sessionsError={null} />))).toContain(
      "Cut control-centre session tcp 10.60.60.20:40312 → 10.30.30.20:2404",
    );
    expect(text(render(<SessionControls sessions={[SESSIONS.sessions[1]]} sessionsError={null} />))).toContain(
      "containd lists no tcp 10.60.60.20 → 10.30.30.20:2404 session.",
    );
  });

  it("shows the device audit newest first", () => {
    const out = text(renderToStaticMarkup(<AuditTable audit={AUDIT} />));
    expect(out.indexOf("first_solve")).toBeLessThan(out.indexOf("local_protection_trip"));
    expect(out).toContain("local_protection_trip accepted local-protection · field · protection closed → open");
    expect(out).toContain("revision 5 · solve 113 · 0 A");
    expect(out).toContain("close rejected modbus · station · 10.30.30.20:41000 lockout");
  });
});

describe("ControlCentreMiniStatus", () => {
  it("summarises the control centre in the exercise runner", () => {
    expect(text(renderToStaticMarkup(<ControlCentreMiniStatus state={FRESH_STATE} waiting={false} />))).toContain("4/4 good");
    const out = text(renderToStaticMarkup(<ControlCentreMiniStatus state={STALE_STATE} waiting={false} />));
    expect(out).toContain("CONNECTING");
    expect(out).toContain("CLOSED ⚠ STALE");
  });
});
