"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { compilePlan, type PlanProvenance } from "./api";
import type { CurriculumScope } from "./curriculum-storage";
import { loadRemediationPlan } from "./remediation-plan";
import { useCurriculumStorageRevision } from "./use-curriculum-storage-revision";

export function useCompiledPlan(scope: CurriculumScope, enabled: boolean, appliedPlan?: PlanProvenance, target: "draft" | "applied" | "request" = "draft") {
  useCurriculumStorageRevision(scope);
  const [, refreshStorage] = useState(0);
  useEffect(() => {
    const refresh = () => refreshStorage((n) => n + 1);
    document.addEventListener("visibilitychange", refresh);
    window.addEventListener("focus", refresh);
    return () => {
      document.removeEventListener("visibilitychange", refresh);
      window.removeEventListener("focus", refresh);
    };
  }, [scope]);
  const actionIds = loadRemediationPlan(scope)?.selectedActionIds ?? [];
  const appliedOnly = target === "applied";
  // No effective request means the same full-reference target used by the server.
  const fullReference = target === "request" && actionIds.length === 0;
  const ids = appliedOnly ? appliedPlan?.actionIds ?? []
    : fullReference ? scope.course?.features.segmentationPlan?.model?.catalog.map((action) => action.id) ?? [] : actionIds;
  return useQuery({
    queryKey: ["compiled-plan", scope.packageId, scope.revision, ids, target, appliedOnly ? appliedPlan?.policyHash : undefined],
    queryFn: async () => {
      const plan = await compilePlan({ packageId: scope.packageId, revision: scope.revision, actionIds: ids });
      if (appliedOnly && plan.policyHash !== appliedPlan?.policyHash) throw new Error("Compiled plan no longer matches the applied plan hash. Refresh the active policy before judging results.");
      return fullReference ? {
        ...plan,
        expectations: plan.expectations.map((row) => ({ ...row, planVerdict: row.hardenedVerdict })),
      } : plan;
    },
    enabled: enabled && !!scope.course?.features.segmentationPlan?.model && (!appliedOnly || !!appliedPlan),
    retry: false,
  });
}
