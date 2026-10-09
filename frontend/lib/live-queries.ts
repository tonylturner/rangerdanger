"use client";

// Live substation, firewall and workshop-status reads shared by the
// process views, the load simulator, the overviews, the network console
// and the exercise runner. They go through React Query so the range
// lifecycle applies to them like every other query: a
// not-ready answer is not retried, and a range change resets them
// (lib/range.ts). Components on one page that poll the same read share
// one request at the fastest interval any of them asks for.

import { useCallback } from "react";
import { queryOptions, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  getActiveFirewallConfig,
  getSubstationAudit,
  getSubstationNetworkEvents,
  getSubstationState,
  getWorkshopStatus,
} from "./api";

export const SUBSTATION_KEY = ["substation"] as const;
export const FIREWALL_ACTIVE_KEY = ["firewall-active"] as const;
// GET /api/workshop/status: the one key for it, so a policy apply that
// invalidates it refreshes every view that shows it.
export const WORKSHOP_STATUS_KEY = ["workshop", "status"] as const;

// The US RTAC reads. enabled is false wherever the active package's
// operator view is not the US one (lib/operator-view.ts): those ranges
// have no RTAC, so polling it would only produce errors.
export function substationStateQuery(intervalMs: number, enabled = true) {
  return queryOptions({
    queryKey: [...SUBSTATION_KEY, "state"],
    queryFn: getSubstationState,
    refetchInterval: intervalMs,
    enabled,
  });
}

export function substationAuditQuery(intervalMs: number, enabled = true) {
  return queryOptions({
    queryKey: [...SUBSTATION_KEY, "audit"],
    queryFn: getSubstationAudit,
    refetchInterval: intervalMs,
    enabled,
  });
}

export function useSubstationState(intervalMs: number, enabled = true) {
  return useQuery(substationStateQuery(intervalMs, enabled));
}

export function useSubstationAudit(intervalMs: number, enabled = true) {
  return useQuery(substationAuditQuery(intervalMs, enabled));
}

export function useSubstationNetworkEvents(intervalMs: number) {
  return useQuery({
    queryKey: [...SUBSTATION_KEY, "network-events"],
    queryFn: getSubstationNetworkEvents,
    refetchInterval: intervalMs,
  });
}

export function useWorkshopStatus(intervalMs: number) {
  return useQuery({ queryKey: WORKSHOP_STATUS_KEY, queryFn: getWorkshopStatus, refetchInterval: intervalMs });
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
