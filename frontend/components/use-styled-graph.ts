"use client";

import { useMemo } from "react";
import type { LabGraph, TrafficStatus, ZoneRuleSummary } from "../lib/api";
import type { ViewMode } from "../lib/network-console-data";
import { buildStyledGraph } from "../lib/network-graph";

export function useStyledGraph(
  graph?: LabGraph,
  ruleSummaries?: ZoneRuleSummary[],
  viewMode: ViewMode = { policyDim: true, traffic: false, iec62443: false },
  deviceComms?: Record<string, boolean>,
  rtacOnline?: boolean,
  trafficStatus?: TrafficStatus,
  firewallOnline?: boolean,
  trafficFilter?: { zone: string; crossZoneOnly: boolean },
  highlightedTrafficPair?: string | null,
) {
  // The graph builder reads these view-mode fields individually, as before extraction.
  return useMemo(() => buildStyledGraph(
    graph,
    ruleSummaries,
    viewMode,
    deviceComms,
    rtacOnline,
    trafficStatus,
    firewallOnline,
    trafficFilter,
    highlightedTrafficPair,
  // eslint-disable-next-line react-hooks/exhaustive-deps
  ), [
    graph,
    ruleSummaries,
    viewMode.policyDim,
    viewMode.traffic,
    viewMode.iec62443,
    deviceComms,
    rtacOnline,
    trafficStatus,
    firewallOnline,
    trafficFilter,
    highlightedTrafficPair,
  ]);
}
