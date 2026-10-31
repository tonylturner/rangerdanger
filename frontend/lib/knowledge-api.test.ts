import { afterEach, describe, expect, it, vi } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import { getKnowledge } from "./knowledge-api";
import { knowledgeKey } from "./knowledge-queries";
import type { Knowledge } from "./knowledge-types";

const snapshot: Knowledge = { package_id: "eu", revision: 2, sections: [] };
afterEach(() => vi.unstubAllGlobals());

describe("knowledge API", () => {
  it("uses the shared request plumbing with cancellation", async () => {
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(snapshot)));
    vi.stubGlobal("fetch", fetch);
    const signal = new AbortController().signal;
    expect(await getKnowledge("eu", 2, signal)).toEqual(snapshot);
    expect(fetch).toHaveBeenCalledWith(expect.stringContaining("/knowledge"), expect.objectContaining({ signal, cache: "no-store" }));
  });
  it("rejects package and revision mismatches", async () => {
    vi.stubGlobal("fetch", vi.fn().mockImplementation(async () => new Response(JSON.stringify(snapshot))));
    await expect(getKnowledge("us", 2)).rejects.toThrow("does not match");
    await expect(getKnowledge("eu", 3)).rejects.toThrow("does not match");
  });
  it("surfaces explicit backend errors", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response('{"error":"no active knowledge package"}', { status: 409 })));
    await expect(getKnowledge("eu", 2)).rejects.toThrow("no active knowledge package");
  });
  it("isolates late responses under the original package/revision key", async () => {
    let finish!: (response: Response) => void;
    vi.stubGlobal("fetch", vi.fn().mockImplementationOnce(() => new Promise<Response>((resolve) => { finish = resolve; })));
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const late = client.fetchQuery({ queryKey: knowledgeKey("eu", 2), queryFn: () => getKnowledge("eu", 2) });
    client.setQueryData(knowledgeKey("us", 1), { ...snapshot, package_id: "us", revision: 1 });
    finish(new Response(JSON.stringify(snapshot)));
    await late;
    expect(client.getQueryData(knowledgeKey("us", 1))).toMatchObject({ package_id: "us", revision: 1 });
    expect(client.getQueryData(knowledgeKey("eu", 2))).toEqual(snapshot);
    client.clear();
  });
});
