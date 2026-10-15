import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import KnowledgePage from "../app/knowledge/page";
import { renderArticleBody } from "./knowledge-reader";
import { knowledgeKey } from "../lib/knowledge-queries";
import { knowledgeViewForHash, pushKnowledgeArticle, watchKnowledgeHash } from "../lib/knowledge-navigation";
import type { Knowledge, Section } from "../lib/knowledge-types";

const sections: Section[] = [{ id: "eu", heading: "EU telecontrol", articles: [{ id: "iec104", title: "IEC104 points", body: "Only EU data" }] }];
const snapshot: Knowledge = { package_id: "eu", revision: 1, sections };
function render(data?: Knowledge, error?: Error, active = "eu") {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, retryOnMount: false } } });
  client.setQueryData(["packages", "active"], { id: active, revision: 1 });
  if (data) client.setQueryData(knowledgeKey(active, 1), data);
  if (error) client.getQueryCache().build(client, { queryKey: knowledgeKey(active, 1) }).setState({ status: "error", error });
  const html = renderToStaticMarkup(<QueryClientProvider client={client}><KnowledgePage /></QueryClientProvider>);
  client.clear();
  return html;
}

describe("package knowledge reader", () => {
  it("loads without static US content", () => {
    expect(render()).toContain("Loading package knowledge");
    expect(render()).not.toContain("RTAC");
  });
  it("derives layout and count from the loaded package", () => {
    const html = render(snapshot);
    expect(html).toContain("EU telecontrol");
    expect(html).toContain("IEC104 points");
    expect(html).toContain("1 articles");
    expect(html).not.toContain("RTAC");
  });
  it("shows errors, empty content and mismatched cached identity explicitly", () => {
    expect(render(undefined, new Error("offline"))).toContain("Knowledge unavailable: offline");
    expect(render({ ...snapshot, sections: [] })).toContain("no knowledge articles");
    expect(render(snapshot, undefined, "us")).toContain("does not match");
  });
  it("preserves admonitions and both forms of article links", () => {
    const html = renderToStaticMarkup(<>{renderArticleBody(":::warning Evidence\nNever retry blindly.\n:::\n\n[local](#iec104) [deep](/knowledge#iec104)", (href, text) => <a href={href}>{text}</a>)}</>);
    expect(html).toContain("WARNING");
    expect(html).toContain("Never retry blindly");
    expect(html).toContain('href="#iec104"');
    expect(html).toContain('href="/knowledge#iec104"');
  });
  it("resolves loaded deep links and reports stale hashes in another package", () => {
    const articles = new Map([["iec104", { section: sections[0], article: sections[0].articles[0] }]]);
    expect(knowledgeViewForHash("#iec104", articles).kind).toBe("article");
    expect(knowledgeViewForHash("", articles)).toEqual({ kind: "landing" });
    expect(knowledgeViewForHash("#dnp3", articles)).toEqual({ kind: "unknown", id: "dnp3" });
  });
  it("reapplies hashes after load and follows Back/Forward without duplicating entries", () => {
    const source = new (class extends EventTarget { location = { hash: "#iec104" }; })();
    const hashes: string[] = [];
    const cleanup = watchKnowledgeHash(source, (hash) => hashes.push(hash));
    source.location.hash = "";
    source.dispatchEvent(new Event("popstate"));
    source.location.hash = "#iec104";
    source.dispatchEvent(new Event("hashchange"));
    expect(hashes).toEqual(["#iec104", "", "#iec104"]);
    const pushed: unknown[] = [];
    const history = { pushState: (_: unknown, _unused: string, url?: string | URL | null) => { pushed.push(url); } };
    pushKnowledgeArticle("iec104", "#iec104", history);
    pushKnowledgeArticle("other", "#iec104", history);
    expect(pushed).toEqual(["#other"]);
    cleanup();
    source.dispatchEvent(new Event("popstate"));
    expect(hashes).toHaveLength(3);
  });
});
