"use client";

// React Query reads for the operator view selection, the EU control
// centre and the instructor truth page. Every key here is range-scoped
// (lib/range.ts), so a range change drops them like any other query.

import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { getActivePackage } from "./api";
import {
  getFirewallSessions,
  getInstructorAudit,
  getInstructorState,
  getOperatorJournal,
  getOperatorState,
  killFirewallSession,
  resetInstructorDevice,
  sendOperatorCommand,
  setInstructorFault,
} from "./operator-api";
import { resolveOperatorView, type OperatorViewResolution } from "./operator-view";

// Same key and function as useActiveCurriculum, so the two share one
// request.
export function useOperatorView(): OperatorViewResolution {
  const { data, error } = useQuery({ queryKey: ["packages", "active"], queryFn: getActivePackage });
  return useMemo(() => resolveOperatorView(data, error), [data, error]);
}

export const OPERATOR_KEY = ["operator"] as const;
export const INSTRUCTOR_KEY = ["instructor"] as const;
export const SESSIONS_KEY = ["firewall-sessions"] as const;

export function useOperatorState(intervalMs: number) {
  return useQuery({ queryKey: [...OPERATOR_KEY, "state"], queryFn: getOperatorState, refetchInterval: intervalMs });
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

export function useInstructorState(intervalMs: number) {
  return useQuery({ queryKey: [...INSTRUCTOR_KEY, "state"], queryFn: getInstructorState, refetchInterval: intervalMs });
}

export function useInstructorAudit(intervalMs: number) {
  return useQuery({ queryKey: [...INSTRUCTOR_KEY, "audit"], queryFn: getInstructorAudit, refetchInterval: intervalMs });
}

export function useFirewallSessions(dport: number, intervalMs: number) {
  return useQuery({
    queryKey: [...SESSIONS_KEY, dport],
    queryFn: () => getFirewallSessions({ dport }),
    refetchInterval: intervalMs,
  });
}

function useRefreshAfter() {
  const queryClient = useQueryClient();
  return () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: INSTRUCTOR_KEY }),
      queryClient.invalidateQueries({ queryKey: OPERATOR_KEY }),
      queryClient.invalidateQueries({ queryKey: SESSIONS_KEY }),
    ]);
}

export function useInstructorFault() {
  const refresh = useRefreshAfter();
  return useMutation({ mutationFn: setInstructorFault, retry: false, onSettled: refresh });
}

export function useInstructorReset() {
  const refresh = useRefreshAfter();
  return useMutation({ mutationFn: resetInstructorDevice, retry: false, onSettled: refresh });
}

export function useSessionKill() {
  const refresh = useRefreshAfter();
  return useMutation({ mutationFn: killFirewallSession, retry: false, onSettled: refresh });
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
