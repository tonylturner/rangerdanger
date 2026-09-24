"use client";

import React, { useCallback, useEffect, useMemo, useState } from "react";
import {
  ArrowLeft,
  BookOpen,
  ChevronRight,
  Clock,
  Home,
  Layers,
  Radio,
  Search,
  Shield,
  Target,
  Wrench,
  Zap,
} from "lucide-react";
import Markdown from "marked-react";
import { sections } from "../../lib/knowledge-content";
import { buildSnippet, readingMinutes, stripMarkdownForExcerpt } from "../../lib/knowledge-search";
import type { Article, IconName, SearchHit, Section, View } from "../../lib/knowledge-types";

/* ------------------------------------------------------------------ */
/*  Helpers                                                            */
/* ------------------------------------------------------------------ */

/**
 * Per-section accent palette. Each color name maps to a bundle of
 * Tailwind class strings used by the landing tiles, article cards,
 * search results, and the article reading view's section header.
 *
 * Design intent: each section reads as visually distinct from the
 * landing page (you can see at a glance which tile is which), and
 * that same color identity follows the article through category and
 * reading views.
 */
const ACCENT_CLASSES: Record<
  string,
  {
    ring: string;        // hover-state border
    chip: string;        // small section pill background
    chipText: string;
    iconBg: string;      // 40x40 icon container background
    iconText: string;
    cardBg: string;      // landing-tile background (subtle accent tint)
    cardBorder: string;  // landing-tile resting border
    leftBar: string;     // 4px left bar color for article cards
    topBar: string;      // wide top bar inside article reading view
    headerWash: string;  // soft wash behind the article header
  }
> = {
  sky: {
    ring: "hover:border-sky-500",
    chip: "bg-sky-900/60",
    chipText: "text-sky-100",
    iconBg: "bg-sky-500/25",
    iconText: "text-sky-200",
    cardBg: "bg-gradient-to-br from-sky-950/60 via-slate-900/80 to-slate-900/60",
    cardBorder: "border-sky-800/60",
    leftBar: "border-l-sky-500",
    topBar: "bg-sky-500",
    headerWash: "bg-gradient-to-b from-sky-950/40 to-transparent",
  },
  emerald: {
    ring: "hover:border-emerald-500",
    chip: "bg-emerald-900/60",
    chipText: "text-emerald-100",
    iconBg: "bg-emerald-500/25",
    iconText: "text-emerald-200",
    cardBg: "bg-gradient-to-br from-emerald-950/60 via-slate-900/80 to-slate-900/60",
    cardBorder: "border-emerald-800/60",
    leftBar: "border-l-emerald-500",
    topBar: "bg-emerald-500",
    headerWash: "bg-gradient-to-b from-emerald-950/40 to-transparent",
  },
  violet: {
    ring: "hover:border-violet-500",
    chip: "bg-violet-900/60",
    chipText: "text-violet-100",
    iconBg: "bg-violet-500/25",
    iconText: "text-violet-200",
    cardBg: "bg-gradient-to-br from-violet-950/60 via-slate-900/80 to-slate-900/60",
    cardBorder: "border-violet-800/60",
    leftBar: "border-l-violet-500",
    topBar: "bg-violet-500",
    headerWash: "bg-gradient-to-b from-violet-950/40 to-transparent",
  },
  amber: {
    ring: "hover:border-amber-500",
    chip: "bg-amber-900/60",
    chipText: "text-amber-100",
    iconBg: "bg-amber-500/25",
    iconText: "text-amber-200",
    cardBg: "bg-gradient-to-br from-amber-950/60 via-slate-900/80 to-slate-900/60",
    cardBorder: "border-amber-800/60",
    leftBar: "border-l-amber-500",
    topBar: "bg-amber-500",
    headerWash: "bg-gradient-to-b from-amber-950/40 to-transparent",
  },
  slate: {
    ring: "hover:border-slate-500",
    chip: "bg-slate-700/60",
    chipText: "text-slate-100",
    iconBg: "bg-slate-500/25",
    iconText: "text-slate-200",
    cardBg: "bg-gradient-to-br from-slate-800/60 via-slate-900/80 to-slate-900/60",
    cardBorder: "border-slate-700/60",
    leftBar: "border-l-slate-400",
    topBar: "bg-slate-400",
    headerWash: "bg-gradient-to-b from-slate-800/40 to-transparent",
  },
  rose: {
    ring: "hover:border-rose-500",
    chip: "bg-rose-900/60",
    chipText: "text-rose-100",
    iconBg: "bg-rose-500/25",
    iconText: "text-rose-200",
    cardBg: "bg-gradient-to-br from-rose-950/60 via-slate-900/80 to-slate-900/60",
    cardBorder: "border-rose-800/60",
    leftBar: "border-l-rose-500",
    topBar: "bg-rose-500",
    headerWash: "bg-gradient-to-b from-rose-950/40 to-transparent",
  },
};

const accentOf = (s: Section) => ACCENT_CLASSES[s.accent || "slate"] || ACCENT_CLASSES.slate;

function SectionIcon({ name, className }: { name?: IconName; className?: string }) {
  const cls = className || "h-5 w-5";
  switch (name) {
    case "zap":     return <Zap     className={cls} />;
    case "shield":  return <Shield  className={cls} />;
    case "radio":   return <Radio   className={cls} />;
    case "wrench":  return <Wrench  className={cls} />;
    case "layers":  return <Layers  className={cls} />;
    case "target":  return <Target  className={cls} />;
    default:        return <BookOpen className={cls} />;
  }
}

/* ------------------------------------------------------------------ */
/*  Admonitions + cross-link routing                                   */
/* ------------------------------------------------------------------ */

const ADMONITION_STYLES: Record<
  string,
  { border: string; bg: string; label: string; labelColor: string }
> = {
  tip:     { border: "border-l-emerald-500", bg: "bg-emerald-950/30", label: "TIP",     labelColor: "text-emerald-300" },
  note:    { border: "border-l-sky-500",     bg: "bg-sky-950/30",     label: "NOTE",    labelColor: "text-sky-300" },
  warning: { border: "border-l-amber-500",   bg: "bg-amber-950/30",   label: "WARNING", labelColor: "text-amber-300" },
  caution: { border: "border-l-rose-500",    bg: "bg-rose-950/30",    label: "CAUTION", labelColor: "text-rose-300" },
};

function Admonition({
  kind,
  title,
  children,
}: {
  kind: string;
  title?: string;
  children: React.ReactNode;
}) {
  const s = ADMONITION_STYLES[kind] || ADMONITION_STYLES.note;
  return (
    <div
      className={`my-5 overflow-hidden rounded-r-md border border-l-4 border-slate-800 ${s.border} ${s.bg} px-4 py-3`}
    >
      <div
        className={`mb-1.5 text-[10px] font-bold uppercase tracking-wider ${s.labelColor}`}
      >
        {s.label}
        {title ? ` · ${title}` : ""}
      </div>
      <div className="text-sm leading-relaxed text-slate-300">{children}</div>
    </div>
  );
}

/**
 * Split the article body on `:::tip|note|warning|caution [Title]\n…\n:::`
 * fences and render each segment via marked-react with internal-link routing
 * for `#article-id` href patterns.
 */
function renderArticleBody(
  body: string,
  linkRenderer: (href: string, text: React.ReactNode) => React.ReactElement,
): React.ReactNode[] {
  const segments: React.ReactNode[] = [];
  const re = /^:::(tip|note|warning|caution)(?:[ \t]+(.+))?\n([\s\S]+?)\n:::[ \t]*$/gm;
  let lastIdx = 0;
  let key = 0;
  let m: RegExpExecArray | null;
  while ((m = re.exec(body)) !== null) {
    if (m.index > lastIdx) {
      const md = body.slice(lastIdx, m.index);
      if (md.trim()) {
        segments.push(
          <Markdown key={key++} renderer={{ link: linkRenderer }}>
            {md}
          </Markdown>,
        );
      }
    }
    segments.push(
      <Admonition key={key++} kind={m[1]} title={m[2]}>
        <Markdown renderer={{ link: linkRenderer }}>{m[3]}</Markdown>
      </Admonition>,
    );
    lastIdx = m.index + m[0].length;
  }
  if (lastIdx < body.length) {
    const md = body.slice(lastIdx);
    if (md.trim()) {
      segments.push(
        <Markdown key={key++} renderer={{ link: linkRenderer }}>
          {md}
        </Markdown>,
      );
    }
  }
  return segments;
}

/* ------------------------------------------------------------------ */
/*  Component                                                          */
/* ------------------------------------------------------------------ */

export default function KnowledgePage() {
  const [search, setSearch] = useState("");
  const [view, setView] = useState<View>({ kind: "landing" });

  const totalArticles = useMemo(
    () => sections.reduce((n, s) => n + s.articles.length, 0),
    [],
  );

  const articlesById = useMemo(() => {
    const m = new Map<string, { article: Article; section: Section }>();
    sections.forEach((s) =>
      s.articles.forEach((a) => m.set(a.id, { article: a, section: s })),
    );
    return m;
  }, []);

  const goArticle = (s: Section, a: Article) =>
    setView({ kind: "article", section: s, article: a });

  const linkRenderer = useCallback(
    (href: string, text: React.ReactNode): React.ReactElement => {
      // Internal cross-link: href like #article-id
      if (href.startsWith("#")) {
        const target = articlesById.get(href.slice(1));
        if (target) {
          return (
            <a
              href={href}
              onClick={(e) => {
                e.preventDefault();
                goArticle(target.section, target.article);
              }}
              className="text-sky-400 underline-offset-2 hover:underline"
            >
              {text}
            </a>
          );
        }
      }
      // External link: open in new tab
      return (
        <a
          href={href}
          target="_blank"
          rel="noopener noreferrer"
          className="text-sky-400 underline-offset-2 hover:underline"
        >
          {text}
        </a>
      );
    },
    [articlesById],
  );

  const hits: SearchHit[] = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return [];
    const out: SearchHit[] = [];
    for (const section of sections) {
      for (const article of section.articles) {
        const inTitle = article.title.toLowerCase().includes(q);
        const inBody = article.body.toLowerCase().includes(q);
        if (inTitle || inBody) {
          out.push({
            article,
            section,
            snippet: inBody
              ? buildSnippet(article.body, q)
              : stripMarkdownForExcerpt(article.body).slice(0, 180),
          });
        }
      }
    }
    return out;
  }, [search]);

  const goLanding = () => {
    setView({ kind: "landing" });
    setSearch("");
    if (typeof window !== "undefined" && window.location.hash) {
      history.replaceState(null, "", window.location.pathname);
    }
  };
  const goCategory = (s: Section) => setView({ kind: "category", section: s });

  // Deep-link support: read `#article-id` on mount and whenever the browser
  // history advances (back / forward, or hashchange). Lab YAMLs can use
  // [link](/knowledge#article-id) and clicking jumps straight into the article.
  //
  // Empty hash means "landing view." This is what lets Back from article B
  // restore article A (which the article-effect pushed earlier), and Back
  // again restore landing (popstate fires with empty hash).
  useEffect(() => {
    const apply = () => {
      if (typeof window === "undefined") return;
      const hash = window.location.hash.replace(/^#/, "");
      if (!hash) {
        setView({ kind: "landing" });
        return;
      }
      const target = articlesById.get(hash);
      if (target) {
        setView({
          kind: "article",
          section: target.section,
          article: target.article,
        });
      }
    };
    apply();
    window.addEventListener("hashchange", apply);
    window.addEventListener("popstate", apply);
    return () => {
      window.removeEventListener("hashchange", apply);
      window.removeEventListener("popstate", apply);
    };
  }, [articlesById]);

  // Whenever view changes to article-mode, mirror the article id into the URL.
  // Use pushState (not replaceState) so each article navigation gets its own
  // history entry and the browser Back button actually walks back through the
  // article-A -> article-B chain rather than skipping A. The guard prevents
  // an infinite loop when popstate / hashchange set the view from an existing
  // matching URL - we only push when the hash needs to change.
  //
  // Hash stripping on landing is handled by goLanding() explicitly so this
  // effect stays single-purpose.
  useEffect(() => {
    if (typeof window === "undefined") return;
    if (view.kind === "article") {
      const wanted = "#" + view.article.id;
      if (window.location.hash !== wanted) {
        history.pushState(null, "", wanted);
      }
    }
  }, [view]);

  const isSearching = search.trim().length > 0;

  return (
    <main className="flex h-[calc(100vh-0px)] flex-col overflow-hidden bg-slate-950">
      {/* Header */}
      <header className="flex shrink-0 items-center gap-3 border-b border-slate-800 bg-slate-950/80 px-6 py-4">
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img
          src="/rook-quarter-turn-wink-transparent-web.png"
          alt="Rook"
          className="h-9 w-9 shrink-0"
        />
        <button
          onClick={goLanding}
          className="flex items-center gap-2 text-left transition-opacity hover:opacity-80"
          aria-label="Back to knowledge home"
        >
          <BookOpen className="h-5 w-5 text-sky-400" />
          <h1 className="text-lg font-semibold text-slate-100">
            Knowledge Base
          </h1>
        </button>
        <span className="text-xs text-slate-500">{totalArticles} articles</span>

        <div className="ml-auto flex items-center gap-2">
          {(view.kind !== "landing" || isSearching) && (
            <button
              onClick={goLanding}
              className="flex items-center gap-1.5 rounded-md border border-slate-700 bg-slate-900 px-2.5 py-1 text-xs text-slate-300 transition-colors hover:bg-slate-800 hover:text-slate-100"
            >
              <Home className="h-3.5 w-3.5" />
              Home
            </button>
          )}
        </div>
      </header>

      {/* Search bar */}
      <div className="shrink-0 border-b border-slate-800 bg-slate-950/60 px-6 py-3">
        <div className="relative mx-auto max-w-2xl">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-500" />
          <input
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search across all articles (titles and bodies)…"
            className="w-full rounded-lg border border-slate-700 bg-slate-900 py-2 pl-10 pr-4 text-sm text-slate-200 placeholder:text-slate-600 focus:border-sky-600 focus:outline-none focus:ring-1 focus:ring-sky-600"
          />
          {isSearching && (
            <button
              onClick={() => setSearch("")}
              className="absolute right-2 top-1/2 -translate-y-1/2 rounded px-2 py-0.5 text-xs text-slate-500 hover:text-slate-300"
              aria-label="Clear search"
            >
              clear
            </button>
          )}
        </div>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-y-auto px-6 py-8">
        <div className="mx-auto max-w-5xl">
          {/* SEARCH RESULTS - take precedence over any nav view */}
          {isSearching ? (
            <div>
              <h2 className="mb-1 text-sm font-semibold text-slate-200">
                {hits.length === 0
                  ? "No results"
                  : hits.length === 1
                    ? "1 result"
                    : `${hits.length} results`}{" "}
                <span className="text-slate-500">
                  for &ldquo;{search.trim()}&rdquo;
                </span>
              </h2>
              <p className="mb-6 text-xs text-slate-500">
                Searches every article title and body. Click a result to jump
                into the article.
              </p>

              {hits.length === 0 ? (
                <p className="py-10 text-center text-sm text-slate-500">
                  Try a different query, or{" "}
                  <button
                    onClick={() => setSearch("")}
                    className="text-sky-400 hover:underline"
                  >
                    clear the search
                  </button>{" "}
                  to browse topics.
                </p>
              ) : (
                <ul className="space-y-3">
                  {hits.map((hit) => {
                    const acc = accentOf(hit.section);
                    return (
                      <li key={hit.article.id}>
                        <button
                          onClick={() => {
                            setSearch("");
                            goArticle(hit.section, hit.article);
                          }}
                          className={`block w-full rounded-lg border border-l-4 ${acc.cardBorder} ${acc.leftBar} bg-slate-900/60 p-4 text-left transition-colors ${acc.ring}`}
                        >
                          <div className="mb-1 flex items-center gap-2">
                            <span
                              className={`inline-flex items-center gap-1 rounded-full ${acc.chip} px-2 py-0.5 text-[10px] font-medium uppercase tracking-wider ${acc.chipText}`}
                            >
                              <SectionIcon
                                name={hit.section.icon}
                                className="h-3 w-3"
                              />
                              {hit.section.heading}
                            </span>
                            <span className="flex items-center gap-1 text-[10px] text-slate-600">
                              <Clock className="h-3 w-3" />
                              {readingMinutes(hit.article.body)} min
                            </span>
                          </div>
                          <h3 className="text-sm font-semibold text-slate-100">
                            {hit.article.title}
                          </h3>
                          <p className="mt-1 line-clamp-2 text-xs text-slate-400">
                            {hit.snippet}
                          </p>
                        </button>
                      </li>
                    );
                  })}
                </ul>
              )}
            </div>
          ) : view.kind === "landing" ? (
            /* LANDING: hero + topic tiles */
            <div>
              <div className="mb-8">
                <h2 className="text-2xl font-semibold text-slate-100">
                  Explore the knowledge base
                </h2>
                <p className="mt-2 max-w-2xl text-sm leading-relaxed text-slate-400">
                  Background reading for the workshop labs. Pick a topic to
                  browse, or use the search bar above to jump straight to a
                  specific article. Each article is short enough to read in a
                  few minutes during an exercise.
                </p>
              </div>

              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
                {sections.map((section) => {
                  const acc = accentOf(section);
                  return (
                    <button
                      key={section.heading}
                      onClick={() => goCategory(section)}
                      className={`group relative flex flex-col rounded-xl border ${acc.cardBorder} ${acc.cardBg} p-5 text-left transition-all ${acc.ring} hover:shadow-lg hover:shadow-slate-950/50`}
                    >
                      <div
                        className={`mb-4 flex h-10 w-10 items-center justify-center rounded-lg ${acc.iconBg} ${acc.iconText}`}
                      >
                        <SectionIcon name={section.icon} className="h-5 w-5" />
                      </div>
                      <h3 className="text-base font-semibold text-slate-100">
                        {section.heading}
                      </h3>
                      <p className="mt-2 line-clamp-3 text-xs leading-relaxed text-slate-400">
                        {section.description || ""}
                      </p>
                      <div className="mt-4 flex items-center justify-between text-[11px]">
                        <span className="text-slate-500">
                          {section.articles.length} article
                          {section.articles.length === 1 ? "" : "s"}
                        </span>
                        <span className="flex items-center gap-1 text-slate-500 group-hover:text-slate-300">
                          Browse <ChevronRight className="h-3 w-3" />
                        </span>
                      </div>
                    </button>
                  );
                })}
              </div>

              <div className="mt-10">
                <h3 className="mb-3 text-xs font-semibold uppercase tracking-widest text-amber-400/80">
                  All articles, A–Z
                </h3>
                <ul className="columns-1 gap-x-8 sm:columns-2 lg:columns-3">
                  {[...sections]
                    .flatMap((section) =>
                      section.articles.map((article) => ({ article, section })),
                    )
                    .sort((a, b) => a.article.title.localeCompare(b.article.title))
                    .map(({ article, section }) => (
                      <li
                        key={article.id}
                        className="mb-2 break-inside-avoid text-xs leading-snug"
                      >
                        <button
                          onClick={() => goArticle(section, article)}
                          className="text-left text-slate-400 transition-colors hover:text-sky-300"
                        >
                          {article.title}
                        </button>
                      </li>
                    ))}
                </ul>
              </div>
            </div>
          ) : view.kind === "category" ? (
            /* CATEGORY: one section's articles, card-style */
            <div>
              <button
                onClick={goLanding}
                className="mb-4 flex items-center gap-1 text-xs text-slate-500 transition-colors hover:text-slate-300"
              >
                <ArrowLeft className="h-3 w-3" />
                All topics
              </button>
              <div className="mb-6 flex items-start gap-4">
                <div
                  className={`flex h-12 w-12 shrink-0 items-center justify-center rounded-lg ${accentOf(view.section).iconBg} ${accentOf(view.section).iconText}`}
                >
                  <SectionIcon name={view.section.icon} className="h-6 w-6" />
                </div>
                <div className="min-w-0">
                  <h2 className="text-2xl font-semibold text-slate-100">
                    {view.section.heading}
                  </h2>
                  <p className="mt-2 max-w-2xl text-sm leading-relaxed text-slate-400">
                    {view.section.description}
                  </p>
                  <p className="mt-3 text-[11px] text-slate-500">
                    {view.section.articles.length} article
                    {view.section.articles.length === 1 ? "" : "s"}
                  </p>
                </div>
              </div>

              <ul className="space-y-3">
                {view.section.articles.map((article) => {
                  const acc = accentOf(view.section);
                  return (
                    <li key={article.id}>
                      <button
                        onClick={() => goArticle(view.section, article)}
                        className={`block w-full rounded-lg border border-l-4 ${acc.cardBorder} ${acc.leftBar} bg-slate-900/60 p-4 text-left transition-colors ${acc.ring}`}
                      >
                        <div className="flex items-start justify-between gap-3">
                          <h3 className="text-sm font-semibold text-slate-100">
                            {article.title}
                          </h3>
                          <span className="flex shrink-0 items-center gap-1 text-[10px] text-slate-500">
                            <Clock className="h-3 w-3" />
                            {readingMinutes(article.body)} min
                          </span>
                        </div>
                        <p className="mt-1 line-clamp-2 text-xs text-slate-400">
                          {stripMarkdownForExcerpt(article.body).slice(0, 200)}
                        </p>
                      </button>
                    </li>
                  );
                })}
              </ul>
            </div>
          ) : (
            /* ARTICLE: clean reading view */
            <div>
              <nav className="mb-4 flex items-center gap-1.5 text-xs text-slate-500">
                <button
                  onClick={goLanding}
                  className="hover:text-slate-300"
                >
                  Knowledge
                </button>
                <ChevronRight className="h-3 w-3" />
                <button
                  onClick={() => goCategory(view.section)}
                  className="hover:text-slate-300"
                >
                  {view.section.heading}
                </button>
              </nav>

              <article className="mx-auto max-w-3xl">
                {/* Color identifier for the section: 3-px top accent bar
                    plus a soft wash behind the header zone. */}
                <div className="-mx-2 mb-6 overflow-hidden rounded-lg">
                  <div className={`h-1 w-full ${accentOf(view.section).topBar}`} />
                  <div
                    className={`${accentOf(view.section).headerWash} px-4 pb-5 pt-4`}
                  >
                    <div className="mb-3 flex items-center gap-2">
                      <span
                        className={`inline-flex items-center gap-1.5 rounded-full ${accentOf(view.section).chip} px-2.5 py-1 text-[10px] font-semibold uppercase tracking-wider ${accentOf(view.section).chipText}`}
                      >
                        <SectionIcon
                          name={view.section.icon}
                          className="h-3 w-3"
                        />
                        {view.section.heading}
                      </span>
                      <span className="flex items-center gap-1 text-[10px] text-slate-500">
                        <Clock className="h-3 w-3" />
                        {readingMinutes(view.article.body)} min read
                      </span>
                    </div>
                    <h2 className="text-3xl font-semibold leading-tight text-slate-100">
                      {view.article.title}
                    </h2>
                  </div>
                </div>
                <div className="knowledge-article text-[15px] leading-relaxed text-slate-300">
                  {renderArticleBody(view.article.body, linkRenderer)}
                </div>

                {view.section.articles.length > 1 && (
                  <div className="mt-12 border-t border-slate-800 pt-6">
                    <h3 className="mb-3 text-xs font-semibold uppercase tracking-widest text-amber-400/80">
                      More in {view.section.heading}
                    </h3>
                    <ul className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                      {view.section.articles
                        .filter((a) => a.id !== view.article.id)
                        .slice(0, 6)
                        .map((other) => (
                          <li key={other.id}>
                            <button
                              onClick={() => goArticle(view.section, other)}
                              className="flex w-full items-center gap-2 rounded-md border border-slate-800 bg-slate-900/60 px-3 py-2 text-left text-xs text-slate-300 transition-colors hover:border-slate-700 hover:text-slate-100"
                            >
                              <ChevronRight className="h-3 w-3 shrink-0 text-slate-600" />
                              <span className="truncate">{other.title}</span>
                            </button>
                          </li>
                        ))}
                    </ul>
                  </div>
                )}
              </article>
            </div>
          )}
        </div>
      </div>
    </main>
  );
}
