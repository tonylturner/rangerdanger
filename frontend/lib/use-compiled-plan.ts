"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { compilePlan, type PlanProvenance } from "./api";
import type { CurriculumScope } from "./curriculum-storage";
import { loadRemediationPlan } from "./remediation-plan";
import { useCurriculumStorageRevision } from "./use-curriculum-storage-revision";

export function useCompiledPlan(scope: CurriculumScope, enabled: boolean, appliedPlan?: PlanProvenance, appliedOnly = false) {
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
  const ids = appliedOnly ? appliedPlan?.actionIds ?? [] : actionIds;
  return useQuery({
    queryKey: ["compiled-plan", scope.packageId, scope.revision, ids, appliedOnly ? appliedPlan?.policyHash : "draft"],
    queryFn: async () => {
      const plan = await compilePlan({ packageId: scope.packageId, revision: scope.revision, actionIds: ids });
      if (appliedOnly && plan.policyHash !== appliedPlan?.policyHash) throw new Error("Compiled plan no longer matches the applied plan hash. Refresh the active policy before judging results.");
      return plan;
    },
    enabled: enabled && !!scope.curriculum && (!appliedOnly || !!appliedPlan),
    retry: false,
  });
}
