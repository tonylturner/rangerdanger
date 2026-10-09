"use client";

// Applying one of the package's named policies from the network console
// (the header policy menu and the segmentation drawer): the request, the
// queries it refreshes, and what the student reads when it fails.

import { useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { applyFirewallConfig } from "./api";
import { FIREWALL_ACTIVE_KEY, WORKSHOP_STATUS_KEY } from "./live-queries";
import { isRangeNotReady } from "./range";
import { errorMessage } from "./utils";

// Everything that shows the firewall state: the canvas reads the rules,
// the header badge and the overviews the workshop status, the drawer
// the active policy.
// Refreshing all three keeps every entry point in step whichever one
// applied the policy; without firewall-active, the drawer's Active
// Policy banner stayed on the prior value until its own 5s refetch.
const POLICY_QUERY_KEYS = [["firewall-rules"], WORKSHOP_STATUS_KEY, FIREWALL_ACTIVE_KEY] as const;

export async function applyPackagePolicy(client: QueryClient, name: string): Promise<void> {
  await applyFirewallConfig(name);
  for (const queryKey of POLICY_QUERY_KEYS) {
    void client.invalidateQueries({ queryKey: [...queryKey] });
  }
}

// Never retried: a second apply is a second containd commit.
export function useApplyPolicy() {
  const client = useQueryClient();
  return useMutation({ mutationFn: (name: string) => applyPackagePolicy(client, name), retry: false });
}

// label is how the policy is shown where it was picked.
export function policyApplyErrorText(label: string, error: unknown): string {
  if (isRangeNotReady(error)) return error.message;
  return `Could not apply ${label}: ${errorMessage(error)}`;
}
