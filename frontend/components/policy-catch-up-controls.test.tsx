// @vitest-environment jsdom
import { act, fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import * as api from "../lib/api";
import { EU_PLANS, US_PLANS } from "../lib/plan-fixtures.test-util";
import { PolicyCatchUpControls } from "./policy-catch-up-controls";
import { CurriculumScopeProvider, scopeOf } from "../lib/curriculum-scope";
import { fixtureCourse } from "../lib/curriculum-storage.test-util";
import { EU_PACKAGE, US_PACKAGE } from "../lib/operator-fixtures.test-util";
import { memoryStorage } from "../lib/memory-storage.test-util";
import { saveRemediationPlan } from "../lib/remediation-plan";
import { curriculumKey, REMEDIATION_PLAN } from "../lib/curriculum-storage";

const originalStorage = Object.getOwnPropertyDescriptor(window, "localStorage");
beforeEach(() => Object.defineProperty(window, "localStorage", { configurable: true, value: memoryStorage() }));
afterEach(() => {
  vi.restoreAllMocks();
  if (originalStorage) Object.defineProperty(window, "localStorage", originalStorage);
  else Reflect.deleteProperty(window, "localStorage");
});

function mount(source: api.PolicySource = "manual-custom", pkg = EU_PACKAGE, plan: api.CompiledPlan | null = EU_PLANS[0]) {
  const onApplied = vi.fn();
  const content = (current = pkg) => <CurriculumScopeProvider pkg={current}>
    <PolicyCatchUpControls activeConfig="custom" policySource={source} plan={plan} onApplied={onApplied} />
  </CurriculumScopeProvider>;
  const tree = render(content());
  fireEvent.click(tree.container.querySelector("summary")!);
  return { ...tree, onApplied, switchPackage: (next: api.PackageSummary) => tree.rerender(content(next)) };
}

describe("catch-up policy replacement", () => {
  it.each(["Apply Hardened", "Apply Your Plan", "Reset to Weak"])("cancels %s without an API request", (label) => {
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    const canned = vi.spyOn(api, "applyFirewallConfig");
    const plan = vi.spyOn(api, "applyPlan");
    const tree = mount();
    fireEvent.click(tree.getByRole("button", { name: new RegExp(`^${label}`) }));
    expect(confirm).toHaveBeenCalledWith(expect.stringContaining("hand-written firewall policy"));
    expect(canned).not.toHaveBeenCalled();
    expect(plan).not.toHaveBeenCalled();
  });

  it.each(["Apply Hardened", "Reset to Weak"])("disables all replacements while %s is pending and shows server errors", async (label) => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    let reject: (error: Error) => void = () => { throw new Error("Request not started"); };
    vi.spyOn(api, "applyFirewallConfig").mockImplementation(() => new Promise((_, fail) => { reject = fail; }));
    const tree = mount();
    fireEvent.click(tree.getByRole("button", { name: label }));
    for (const button of tree.getAllByRole("button")) expect(button.hasAttribute("disabled")).toBe(true);
    reject(new Error("409 Policy revision changed"));
    await waitFor(() => expect(tree.getByRole("alert").textContent).toBe("409 Policy revision changed"));
    expect(tree.onApplied).not.toHaveBeenCalled();
    expect(tree.getByRole("button", { name: label }).hasAttribute("disabled")).toBe(false);
  });

  it("applies an explicitly confirmed plan and retains its warnings", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    vi.spyOn(api, "applyPlan").mockResolvedValue({ applied: true, warnings: ["Review deferred controls"], plan: EU_PLANS[0] });
    const tree = mount();
    fireEvent.click(tree.getByRole("button", { name: /^Apply Your Plan/ }));
    await waitFor(() => expect(tree.onApplied).toHaveBeenCalledOnce());
    expect(tree.getByText("Review deferred controls")).toBeTruthy();
  });

  it("does not ask to replace a hand-written policy when the source is named", async () => {
    const confirm = vi.spyOn(window, "confirm");
    vi.spyOn(api, "applyFirewallConfig").mockResolvedValue({ status: "applied", active_config: "weak" });
    const tree = mount("hardened-reference");
    fireEvent.click(tree.getByRole("button", { name: "Reset to Weak" }));
    await waitFor(() => expect(tree.onApplied).toHaveBeenCalledOnce());
    expect(confirm).not.toHaveBeenCalled();
  });
});

describe("saved change request protects manual scope", () => {
  it.each([US_PACKAGE, EU_PACKAGE])("hides Hardened for any saved selection in $id, independently of compilation", (base) => {
    const pkg = { ...base, course: fixtureCourse(`lab-definitions/packages/${base.id}`) };
    const scope = scopeOf(pkg);
    const tree = mount("manual-custom", pkg, null);
    expect(tree.getByRole("button", { name: "Apply Hardened" })).toBeTruthy();
    for (const selectedActionIds of [
      [pkg.course.features.segmentationPlan!.model.catalog[0].id],
      pkg.course.features.segmentationPlan!.model.catalog.map((action) => action.id),
      (base.id === EU_PACKAGE.id ? EU_PLANS[2] : US_PLANS[2]).actions.filter((action) => action.kind === "plan").map((action) => action.id),
    ]) {
      expect(selectedActionIds.length).toBeGreaterThan(0);
      act(() => saveRemediationPlan(scope, { exerciseId: pkg.course.features.segmentationPlan!.activities.planning, selectedActionIds, savedAt: "saved" }));
      expect(tree.queryByRole("button", { name: "Apply Hardened" })).toBeNull();
      expect(tree.getByRole("button", { name: "Apply Your Plan" })).toBeTruthy();
      expect(tree.getByRole("button", { name: "Reset to Weak" })).toBeTruthy();
    }
    act(() => saveRemediationPlan(scope, { exerciseId: pkg.course.features.segmentationPlan!.activities.planning, selectedActionIds: [], savedAt: "saved" }));
    expect(tree.getByRole("button", { name: "Apply Hardened" })).toBeTruthy();
  });

  it("reacts to another tab clearing the request and isolates package revisions", () => {
    const pkg = { ...EU_PACKAGE, course: fixtureCourse("lab-definitions/packages/eu-iec104-substation") };
    const scope = scopeOf(pkg);
    const request = { exerciseId: pkg.course.features.segmentationPlan!.activities.planning, selectedActionIds: [pkg.course.features.segmentationPlan!.model.catalog[0].id], savedAt: "saved" };
    saveRemediationPlan(scope, request);
    const tree = mount("manual-custom", pkg);
    expect(tree.queryByRole("button", { name: "Apply Hardened" })).toBeNull();
    tree.switchPackage({ ...pkg, revision: pkg.revision + 1 });
    expect(tree.getByRole("button", { name: "Apply Hardened" })).toBeTruthy();
    tree.switchPackage(pkg);
    const key = curriculumKey(scope, REMEDIATION_PLAN);
    window.localStorage.setItem(key, JSON.stringify({ ...request, selectedActionIds: [] }));
    fireEvent(window, new StorageEvent("storage", { key }));
    expect(tree.getByRole("button", { name: "Apply Hardened" })).toBeTruthy();
  });
});
