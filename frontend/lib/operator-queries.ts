"use client";

// React Query reads for the active package (operator view, policies),
// the EU control centre and physical feeder panel. Every key here
// is range-scoped (lib/range.ts), so a range change drops them like
// any other query.

import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { getActivePackage } from "./api";
import {
  getFieldAudit,
  getFieldState,
  getOperatorJournal,
  getOperatorState,
  sendOperatorCommand,
} from "./operator-api";
import { resolveOperatorView, type OperatorViewResolution } from "./operator-view";
import { packagePolicies, type PackagePolicies } from "./policy-wording";

// Same key and function as useActiveCurriculum, so the two share one
// request.
function useActivePackage() {
  return useQuery({ queryKey: ["packages", "active"], queryFn: getActivePackage });
}

export function useOperatorView(): OperatorViewResolution {
  const { data, error } = useActivePackage();
  return useMemo(() => resolveOperatorView(data, error), [data, error]);
}

// The active package's firewall policies; undefined until it is known.
export function usePackagePolicies(): PackagePolicies | undefined {
  const { data } = useActivePackage();
  return useMemo(() => (data ? packagePolicies(data) : undefined), [data]);
}

export const OPERATOR_KEY = ["operator"] as const;
export const FIELD_KEY = ["field"] as const;
export const SESSIONS_KEY = ["firewall-sessions"] as const;

export function useOperatorState(intervalMs: number, enabled = true) {
  return useQuery({ queryKey: [...OPERATOR_KEY, "state"], queryFn: getOperatorState, refetchInterval: intervalMs, enabled });
}

export function useOperatorJournal(intervalMs: number) {
  return useQuery({ queryKey: [...OPERATOR_KEY, "journal"], queryFn: getOperatorJournal, refetchInterval: intervalMs });
}

// Never retried: a second execute is a second operation.
export function useOperatorCommand() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: sendOperatorCommand,
    retry: false,
    onSettled: () => queryClient.invalidateQueries({ queryKey: OPERATOR_KEY }),
  });
}

export function useFieldState(intervalMs: number) {
  return useQuery({ queryKey: [...FIELD_KEY, "state"], queryFn: getFieldState, refetchInterval: intervalMs });
}

export function useFieldAudit(intervalMs: number) {
  return useQuery({ queryKey: [...FIELD_KEY, "audit"], queryFn: getFieldAudit, refetchInterval: intervalMs });
}

// Wall-clock time, ticking, for sample ages.
export function useNow(intervalMs: number): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(id);
  }, [intervalMs]);
  return now;
}
