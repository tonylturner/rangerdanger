"use client";

import { createContext, useContext, useMemo, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { getActivePackage, listScenarios, type PackageSummary, type Scenario } from "./api";
import type { CurriculumScope } from "./curriculum-storage";

// The active package and its scenarios. Query keys carry the package ID
// so a different package never reuses another's cached scenarios.
export function useActiveCurriculum(): {
  pkg: PackageSummary | undefined;
  scenarios: Scenario[] | undefined;
  isLoading: boolean;
} {
  const pkg = useQuery({ queryKey: ["packages", "active"], queryFn: getActivePackage });
  const scenarios = useQuery({
    queryKey: ["scenarios", pkg.data?.id],
    queryFn: () => {
      if (!pkg.data) throw new Error("No active package");
      return listScenarios(pkg.data.id);
    },
    enabled: pkg.data !== undefined,
  });
  return {
    pkg: pkg.data,
    scenarios: scenarios.data?.scenarios,
    isLoading: pkg.isPending || (pkg.data !== undefined && scenarios.isPending),
  };
}

export function scopeOf(pkg: Pick<PackageSummary, "id" | "revision" | "course">): CurriculumScope {
  return { packageId: pkg.id, revision: pkg.revision, course: pkg.course };
}

const CurriculumScopeContext = createContext<CurriculumScope | null>(null);

// Provides the storage scope to everything rendered inside an exercise.
export function CurriculumScopeProvider({ pkg, children }: { pkg: PackageSummary; children: ReactNode }) {
  const { id, revision, course } = pkg;
  const scope = useMemo(() => scopeOf({ id, revision, course }), [id, revision, course]);
  return <CurriculumScopeContext.Provider value={scope}>{children}</CurriculumScopeContext.Provider>;
}

export function useCurriculumScope(): CurriculumScope {
  const scope = useContext(CurriculumScopeContext);
  if (!scope) throw new Error("useCurriculumScope needs a CurriculumScopeProvider");
  return scope;
}
