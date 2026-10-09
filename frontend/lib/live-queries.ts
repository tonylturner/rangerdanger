"use client";

// Live substation and firewall reads shared by the process views, the
// load simulator and the exercise runner. They go through React Query
// so the range lifecycle applies to them like every other query: a
// not-ready answer is not retried, and a range change resets them
// (lib/range.ts). Components on one page that poll the same read share
// one request at the fastest interval any of them asks for.

import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { getActiveFirewallConfig, getSubstationAudit, getSubstationNetworkEvents, getSubstationState } from "./api";

export const SUBSTATION_KEY = ["substation"] as const;
export const FIREWALL_ACTIVE_KEY = ["firewall-active"] as const;

export function useSubstationState(intervalMs: number) {
  return useQuery({ queryKey: [...SUBSTATION_KEY, "state"], queryFn: getSubstationState, refetchInterval: intervalMs });
}

export function useSubstationAudit(intervalMs: number) {
  return useQuery({ queryKey: [...SUBSTATION_KEY, "audit"], queryFn: getSubstationAudit, refetchInterval: intervalMs });
}

export function useSubstationNetworkEvents(intervalMs: number) {
  return useQuery({
    queryKey: [...SUBSTATION_KEY, "network-events"],
    queryFn: getSubstationNetworkEvents,
    refetchInterval: intervalMs,
  });
}

export function useActiveFirewall(intervalMs: number) {
  return useQuery({ queryKey: FIREWALL_ACTIVE_KEY, queryFn: getActiveFirewallConfig, refetchInterval: intervalMs });
}

// Refetches every live read now, for use right after a command or a
// policy change.
export function useRefreshLive(): () => Promise<void> {
  const queryClient = useQueryClient();
  return useCallback(async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: SUBSTATION_KEY }),
      queryClient.invalidateQueries({ queryKey: FIREWALL_ACTIVE_KEY }),
    ]);
  }, [queryClient]);
}
