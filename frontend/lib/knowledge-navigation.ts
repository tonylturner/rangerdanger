import type { Article, Section, View } from "./knowledge-types";

export function knowledgeViewForHash(hash: string, articles: Map<string, { article: Article; section: Section }>): View {
  const id = hash.replace(/^#/, "");
  if (!id) return { kind: "landing" };
  const target = articles.get(id);
  return target ? { kind: "article", ...target } : { kind: "unknown", id };
}

type HashSource = {
  location: { hash: string };
  addEventListener: (name: string, listener: () => void) => void;
  removeEventListener: (name: string, listener: () => void) => void;
};

export function watchKnowledgeHash(source: HashSource, apply: (hash: string) => void): () => void {
  const update = () => apply(source.location.hash);
  update();
  source.addEventListener("hashchange", update);
  source.addEventListener("popstate", update);
  return () => {
    source.removeEventListener("hashchange", update);
    source.removeEventListener("popstate", update);
  };
}

export function pushKnowledgeArticle(id: string, currentHash: string, history: Pick<History, "pushState">) {
  const wanted = "#" + id;
  if (currentHash !== wanted) history.pushState(null, "", wanted);
}
