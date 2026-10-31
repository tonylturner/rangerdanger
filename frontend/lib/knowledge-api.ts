import { request } from "./api";
import type { Knowledge } from "./knowledge-types";

export function assertKnowledgeIdentity(data: Knowledge, packageId: string, revision: number): Knowledge {
  if (data.package_id !== packageId || data.revision !== revision) {
    throw new Error("Knowledge response does not match the active package revision");
  }
  return data;
}

export async function getKnowledge(packageId: string, revision: number, signal?: AbortSignal): Promise<Knowledge> {
  const data = await request<Knowledge>("/knowledge", { signal });
  return assertKnowledgeIdentity(data, packageId, revision);
}
