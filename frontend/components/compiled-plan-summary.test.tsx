import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { US_CURRICULUM } from "../lib/curriculum-storage.test-util";
import { US_PLANS } from "../lib/plan-fixtures.test-util";
import { CompiledPlanSummary } from "./compiled-plan-summary";

describe("compiled plan exports", () => {
  it("keeps config export pure and exports selected action provenance", () => {
    const plan = US_PLANS[1];
    const html = renderToStaticMarkup(<CompiledPlanSummary plan={plan} binding={US_CURRICULUM} />);
    const links = [...html.matchAll(/<a[^>]*download="([^"]+)"[^>]*href="data:application\/json;charset=utf-8,([^"]+)"[^>]*>(.*?)<\/a>/g)];

    expect(html).toContain(`Revision ${plan.revision} · Policy ${plan.policyHash.slice(0, 12)}`);
    expect(links).toHaveLength(2);
    expect(links[0][1]).toBe(`${plan.packageId}-r${plan.revision}-${plan.policyHash.slice(0, 12)}-config.json`);
    expect(JSON.parse(decodeURIComponent(links[0][2]))).toEqual(plan.config);
    expect(links[1][1]).toBe(`${plan.packageId}-r${plan.revision}-${plan.policyHash.slice(0, 12)}-plan.json`);
    expect(links[1][3]).toContain("Export plan record");
    expect(JSON.parse(decodeURIComponent(links[1][2]))).toEqual({
      packageId: plan.packageId,
      revision: plan.revision,
      label: plan.label,
      actionIds: plan.actions.filter((action) => action.chosen).map((action) => action.id),
      policyHash: plan.policyHash,
    });
  });
});
