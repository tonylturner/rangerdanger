"use client";

import { useMemo } from "react";
import type { LabGraph, ZoneRuleSummary } from "../lib/api";
import type { FlowContext } from "../lib/observed-flows";
import type { ViewMode } from "../lib/network-console-data";
import { useActiveFirewall } from "../lib/live-queries";
import { buildStyledGraph } from "../lib/network-graph";

export function useStyledGraph(
  graph?: LabGraph,
  ruleSummaries?: ZoneRuleSummary[],
  viewMode: ViewMode = { policyDim: true, traffic: false, iec62443: false },
  deviceComms?: Record<string, boolean>,
  rtacOnline?: boolean,
  flowContext?: FlowContext,
  firewallOnline?: boolean,
  trafficFilter?: { zone: string; crossZoneOnly: boolean },
  highlightedTrafficPair?: string | null,
) {
  const active = useActiveFirewall(5000);
  const activePolicy = active.isError ? undefined : active.data;
  // The graph builder reads these view-mode fields individually, as before extraction.
  return useMemo(() => buildStyledGraph(
    graph,
    ruleSummaries,
    viewMode,
    deviceComms,
    rtacOnline,
    flowContext,
    firewallOnline,
    trafficFilter,
    highlightedTrafficPair,
    activePolicy,
  // eslint-disable-next-line react-hooks/exhaustive-deps
  ), [
    graph,
    activePolicy,
    ruleSummaries,
    viewMode.policyDim,
    viewMode.traffic,
    viewMode.iec62443,
    deviceComms,
    rtacOnline,
    flowContext,
    firewallOnline,
    trafficFilter,
    highlightedTrafficPair,
  ]);
}
