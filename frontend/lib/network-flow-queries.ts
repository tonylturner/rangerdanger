"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import type { LabGraph, WorkshopStatus } from "./api";
import { getTrafficStatus, request } from "./api";
import type { FirewallSessionList } from "./operator-api";
import { SESSIONS_KEY, useNow, useOperatorState } from "./operator-queries";
import { createFlowContext, currentOperatorObservation } from "./observed-flows";
import { useRange } from "../components/range-context";

// One owner for all flow observations. The drawer receives this exact context;
// it does not mount its own operator, workshop or generator pollers.
export function useNetworkFlowContext(graph: LabGraph | undefined, workshop: WorkshopStatus | undefined) {
  const { status: range } = useRange();
  const now = useNow(3000);
  const providers = Object.values(graph?.liveness ?? {}).map((b) => b.provider);
  const operator = useOperatorState(3000, providers.some((p) => p === "operator-link" || p === "operator-device"));
  const traffic = useQuery({
    queryKey: ["traffic-status"], queryFn: getTrafficStatus, refetchInterval: 3000,
    enabled: graph?.operations.traffic_generation === true,
  });
  // Read the bounded, unfiltered list: dport filtering hides both incomplete
  // tuples and whether the upstream 5,000-entry limit was reached.
  const sessions = useQuery({
    queryKey: [...SESSIONS_KEY, "all"], queryFn: () => request<FirewallSessionList>("/firewall/sessions"),
    refetchInterval: 3000, enabled: providers.includes("session-presence"),
  });
  return useMemo(() => {
    if (!graph) return undefined;
    const current = range?.phase === "ready";
    const currentOperator = currentOperatorObservation(operator.data, operator.isError || operator.failureCount > 0, operator.dataUpdatedAt, range, now);
    const sessionsCurrent = current && !sessions.isError && sessions.failureCount === 0 && sessions.data && now - sessions.dataUpdatedAt < 15_000;
    return createFlowContext(graph, {
      workshop,
      traffic: graph.operations.traffic_generation ? traffic.data : undefined,
      operator: currentOperator,
      sessions: sessionsCurrent ? sessions.data : undefined,
      observedAt: currentOperator ? operator.dataUpdatedAt : undefined,
    }, now);
  }, [graph, workshop, traffic.data, operator.isError, operator.failureCount, operator.data, operator.dataUpdatedAt, sessions.isError, sessions.failureCount, sessions.data, sessions.dataUpdatedAt, now, range]);
}
