// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { DeviceAudit, ProcessControls } from "../lib/operator-api";
import { AUDIT } from "../lib/operator-fixtures.test-util";
import { AuditTable, PhysicalAudit } from "./physical-system-audit";

const controls: ProcessControls = { fault_section: "none", load_multiplier: 1, source_pu: 1, profile_enabled: true };
const processAudit: DeviceAudit = {
  generation: 123, oldest_seq: 1, latest_seq: 1,
  entries: [{
    seq: 1, at: "2026-11-11T07:00:00Z", operation: "fault_s1", result: "accepted",
    before: controls, after: { ...controls, fault_section: "S1" },
    context: { peer: "10.99.99.10:1234", protocol: "lab", zone: "unknown" },
  }],
};
const clients: QueryClient[] = [];
afterEach(() => {
  cleanup();
  clients.splice(0).forEach((client) => client.clear());
  vi.unstubAllGlobals();
});

describe("process audit table", () => {
  it("shows all process-owned controls before → after, outcome and attribution without inventing device truth", () => {
    const audit = structuredClone(processAudit);
    audit.entries[0].operation = "fault_s2";
    audit.entries[0].after = { fault_section: "S2", load_multiplier: 1.5, source_pu: 0.95, profile_enabled: false };
    const tree = render(<AuditTable audit={audit} />);
    const row = within(tree.getAllByRole("row")[1]);
    for (const text of [
      "fault_s2", "accepted", "lab · unknown · 10.99.99.10:1234",
      "Fault none → S2 · Load 1 → 1.5 · Source 1 → 0.95 p.u. · Profile enabled → disabled",
    ]) expect(row.getByRole("cell", { name: text })).toBeDefined();
    expect(tree.container.textContent).not.toMatch(/undefined|NaN|lockout|f2_tripped/);
  });

  it.each([
    ["clear_fault", "accepted", "no_op"],
    ["unknown", "rejected", "unknown_action"],
  ])("shows unchanged controls and reason for %s/%s", (operation, result, detail) => {
    const entry = { ...processAudit.entries[0], operation, result, detail, after: controls };
    const tree = render(<AuditTable audit={{ ...processAudit, entries: [entry] }} />);
    expect(tree.getByRole("cell", { name: result })).toBeDefined();
    expect(tree.getByRole("cell", {
      name: `Fault none → none · Load 1 → 1 · Source 1 → 1 p.u. · Profile enabled → enabled · ${detail}`,
    })).toBeDefined();
    expect(tree.getByRole("cell", { name: "lab · unknown · 10.99.99.10:1234" })).toBeDefined();
  });

  it("retains device position, rejection and first-solve evidence", () => {
    const tree = render(<AuditTable audit={AUDIT} />);
    expect(tree.getByRole("cell", { name: "closed → open" })).toBeDefined();
    expect(tree.getByRole("cell", { name: "lockout" })).toBeDefined();
    expect(tree.getByRole("cell", { name: "revision 5 · solve 113 · 0 A" })).toBeDefined();
  });

  it("shows the newest 50 process entries first without mutating the audit", () => {
    const entries = Array.from({ length: 55 }, (_, i) => ({ ...processAudit.entries[0], seq: i + 1 }));
    const tree = render(<AuditTable audit={{ ...processAudit, entries, latest_seq: 55 }} />);
    const rows = tree.getAllByRole("row").slice(1);
    expect(rows).toHaveLength(50);
    expect(rows[0].children[0].textContent).toBe("55");
    expect(rows[49].children[0].textContent).toBe("6");
    expect(entries[0].seq).toBe(1);
  });

  it("shows an empty process audit without claiming device entries", () => {
    const tree = render(<AuditTable audit={{ ...processAudit, entries: [], oldest_seq: null, latest_seq: null }} />);
    expect(tree.getByText("No audit entries yet.")).toBeDefined();
  });
});

it("selects process, keeps its separate query across refreshes and retains its last answer on failure", async () => {
  const fetchMock = vi.fn().mockImplementation(async (url: string) =>
    new Response(JSON.stringify(url.endsWith("device=process") ? processAudit : AUDIT)));
  vi.stubGlobal("fetch", fetchMock);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  clients.push(client);
  const tree = render(<QueryClientProvider client={client}><PhysicalAudit /></QueryClientProvider>);
  await waitFor(() => expect(tree.getByRole("cell", { name: "close" })).toBeDefined());
  const select = tree.getByRole("combobox", { name: "Audit device" }) as HTMLSelectElement;
  fireEvent.change(select, { target: { value: "process" } });
  await waitFor(() => expect(tree.getByRole("cell", { name: "fault_s1" })).toBeDefined());
  expect(fetchMock.mock.calls.some(([url]) => url.endsWith("/field/audit?after=0&device=process"))).toBe(true);
  expect(tree.queryByRole("cell", { name: "close" })).toBeNull();
  expect(client.getQueryData(["field", "audit", "breaker-1"])).toEqual(AUDIT);
  expect(client.getQueryData(["field", "audit", "process"])).toEqual(processAudit);
  expect(vi.mocked(fetch).mock.calls.every(([, init]) => init?.method === undefined)).toBe(true);
  act(() => { client.setQueryData(["field", "audit", "process"], structuredClone(processAudit)); });
  expect(select.value).toBe("process");
  fetchMock.mockRejectedValue(new Error("process offline"));
  await act(async () => { await client.refetchQueries({ queryKey: ["field", "audit", "process"] }); });
  await waitFor(() => expect(tree.getByRole("alert").textContent).toContain("process offline"));
  expect(tree.getByRole("alert").textContent).toContain("Showing the last answer.");
  expect(tree.getByRole("cell", { name: "fault_s1" })).toBeDefined();
});
