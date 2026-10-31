"use client";

import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError, getRange, listPackages, requestRange, type PackageSummary } from "../lib/api";
import {
  INITIAL_OBSERVATION,
  RANGE_QUERY_KEY,
  isTransitioning,
  observeRange,
  onRangeNotReady,
  rangePollInterval,
  resetRangeScopedQueries,
  type RangeStatus,
} from "../lib/range";

type RangeContextValue = {
  status: RangeStatus | undefined;
  // Bumped each time the range the page shows stops or starts serving.
  scopeKey: number;
};

const RangeContext = createContext<RangeContextValue | null>(null);

// Polls GET /api/range for the whole app. When the old range stops
// serving and again when the new one is ready, it resets every range-
// or package-scoped query and bumps scopeKey so RangeScope remounts the
// page: nothing from the old range stays on screen during or after the
// transition.
export function RangeProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();
  const range = useQuery({
    queryKey: RANGE_QUERY_KEY,
    queryFn: getRange,
    refetchInterval: (query) => rangePollInterval(query.state.data?.phase),
  });
  const observation = useRef(INITIAL_OBSERVATION);
  const [scopeKey, setScopeKey] = useState(0);

  useEffect(() => {
    if (!range.data) return;
    const { next, changed } = observeRange(observation.current, range.data);
    observation.current = next;
    if (changed) {
      void resetRangeScopedQueries(queryClient);
      setScopeKey((key) => key + 1);
    }
  }, [range.data, queryClient]);

  // A range route answering 503 means the range changed under us (for
  // example from another tab); refresh the status now unless a
  // transition is already being polled.
  useEffect(
    () =>
      onRangeNotReady(() => {
        const phase = queryClient.getQueryData<RangeStatus>(RANGE_QUERY_KEY)?.phase;
        if (phase === undefined || !isTransitioning(phase)) {
          void queryClient.invalidateQueries({ queryKey: RANGE_QUERY_KEY });
        }
      }),
    [queryClient],
  );

  return <RangeContext.Provider value={{ status: range.data, scopeKey }}>{children}</RangeContext.Provider>;
}

export function useRange(): RangeContextValue {
  const value = useContext(RangeContext);
  if (!value) throw new Error("useRange needs a RangeProvider");
  return value;
}

export function usePackages(): PackageSummary[] | undefined {
  return useQuery({ queryKey: ["packages"], queryFn: listPackages }).data;
}

// POST /api/range. The 202 body is the new status, so the provider
// sees the transition (and starts polling fast) without a round trip.
export function useRangeRequest() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: requestRange,
    onSuccess: (status) => queryClient.setQueryData(RANGE_QUERY_KEY, status),
    onError: (error) => {
      if (error instanceof ApiError && error.status === 409) {
        void queryClient.invalidateQueries({ queryKey: RANGE_QUERY_KEY });
      }
    },
  });
}
