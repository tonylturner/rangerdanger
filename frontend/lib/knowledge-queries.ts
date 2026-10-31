"use client";

import { useQuery } from "@tanstack/react-query";
import { getActivePackage } from "./api";
import { assertKnowledgeIdentity, getKnowledge } from "./knowledge-api";

export const knowledgeKey = (packageId: string, revision: number) => ["knowledge", packageId, revision] as const;

export function useKnowledge() {
  // Share package identity with the rest of the portal without fetching labs.
  const active = useQuery({ queryKey: ["packages", "active"], queryFn: getActivePackage });
  const pkg = active.data;
  const knowledge = useQuery({
    queryKey: knowledgeKey(pkg?.id ?? "", pkg?.revision ?? 0),
    queryFn: ({ signal }) => getKnowledge(pkg!.id, pkg!.revision, signal),
    enabled: !!pkg && !active.error,
  });
  let data = knowledge.data;
  let error = active.error ?? knowledge.error;
  // Also check cached data: old/in-flight responses never become another
  // package's reader, even during a lifecycle transition.
  if (data && pkg) {
    try { data = assertKnowledgeIdentity(data, pkg.id, pkg.revision); }
    catch (cause) { error = cause as Error; data = undefined; }
  }
  return { data: error ? undefined : data, error };
}
