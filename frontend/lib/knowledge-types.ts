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
  id: string;
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
  | { kind: "unknown"; id: string }
  | { kind: "category"; section: Section }
  | { kind: "article"; section: Section; article: Article };

export interface Knowledge {
  package_id: string;
  revision: number;
  sections: Section[];
}
