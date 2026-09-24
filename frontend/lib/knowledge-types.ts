export interface Article {
  id: string;
  title: string;
  body: string; // markdown content
}

export type IconName =
  | "zap"
  | "shield"
  | "radio"
  | "wrench"
  | "layers"
  | "target"
  | "book";

export interface Section {
  heading: string;
  description?: string;
  icon?: IconName;
  accent?: string; // tailwind-ish accent color shortcut (sky, emerald, violet, amber, slate, rose)
  articles: Article[];
}

export interface SearchHit {
  article: Article;
  section: Section;
  snippet: string;
}

export type View =
  | { kind: "landing" }
  | { kind: "category"; section: Section }
  | { kind: "article"; section: Section; article: Article };
