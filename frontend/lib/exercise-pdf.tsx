"use client";

// PDF generation for exercise content.
//
// Uses @react-pdf/renderer which produces selectable-text PDFs (not
// rasterized images). The markdown-to-PDF converter walks the marked
// lexer output and emits react-pdf primitives (View, Text) for each
// token type. It also respects the same segment split that the
// scenario runner uses (prose / command / hint) so commands render
// as monospace inset boxes and hint fences render as labeled sections.

import React from "react";
import {
  Document,
  Page,
  Text,
  View,
  Link,
  StyleSheet,
  Font,
} from "@react-pdf/renderer";
import { marked } from "marked";
import type { Scenario, ScenarioStep } from "./api";
import { workbookSection } from "./workbook-sections";

// Register real TTF fonts. The PDF built-in Helvetica/Courier fonts
// have known glyph metric issues in @react-pdf/renderer that surface
// as `unsupported number: -1.66e+21` layout errors when long content
// hits certain wrap conditions. Roboto from the Google Fonts CDN has
// full Unicode coverage and reliable metrics. Hyphen disabled because
// our content has many long unbreakable identifiers (URLs, IPs, JSON
// payloads) that the default hyphenator chokes on.
Font.register({
  family: "Roboto",
  fonts: [
    { src: "https://cdn.jsdelivr.net/npm/@fontsource/roboto@5.0.13/files/roboto-latin-400-normal.woff", fontWeight: 400 },
    { src: "https://cdn.jsdelivr.net/npm/@fontsource/roboto@5.0.13/files/roboto-latin-700-normal.woff", fontWeight: 700 },
    { src: "https://cdn.jsdelivr.net/npm/@fontsource/roboto@5.0.13/files/roboto-latin-400-italic.woff", fontWeight: 400, fontStyle: "italic" },
  ],
});
Font.register({
  family: "RobotoMono",
  fonts: [
    { src: "https://cdn.jsdelivr.net/npm/@fontsource/roboto-mono@5.0.12/files/roboto-mono-latin-400-normal.woff", fontWeight: 400 },
    { src: "https://cdn.jsdelivr.net/npm/@fontsource/roboto-mono@5.0.12/files/roboto-mono-latin-700-normal.woff", fontWeight: 700 },
  ],
});
Font.registerHyphenationCallback((word) => [word]);

// Brand palette — picked to harmonize with the web app's slate/sky theme
// while staying readable on white paper. Dark slate accents, sky blue
// for highlights, amber for code, cyan for blockquotes, warm amber for
// hints. White body so the PDF prints cleanly.
const C = {
  ink: "#0f172a",       // slate-900 — headings
  body: "#1e293b",      // slate-800 — body text
  muted: "#64748b",     // slate-500 — secondary
  faint: "#94a3b8",     // slate-400 — eyebrow
  border: "#e2e8f0",    // slate-200 — rules
  surface: "#f8fafc",   // slate-50 — table header
  accent: "#0369a1",    // sky-700 — primary accent
  accentLight: "#0ea5e9", // sky-500
  codeBg: "#0f172a",    // slate-900 — code panel
  codeFg: "#fbbf24",    // amber-400 — code text
  inlineCodeFg: "#b45309", // amber-700
  inlineCodeBg: "#fef3c7", // amber-100
  hintBorder: "#f59e0b",   // amber-500
  hintBg: "#fffbeb",       // amber-50
  hintLabel: "#92400e",    // amber-800
  quoteBorder: "#06b6d4",  // cyan-500
  quoteBg: "#ecfeff",      // cyan-50
  quoteText: "#164e63",    // cyan-900
};

const styles = StyleSheet.create({
  // ── pages ──────────────────────────────────────────────────────────
  page: {
    paddingTop: 0,
    paddingBottom: 60,
    paddingHorizontal: 0,
    fontFamily: "Roboto",
    fontSize: 10.5,
    color: C.body,
    lineHeight: 1.5,
  },
  pageBody: {
    paddingHorizontal: 54,
    paddingTop: 24,
  },
  pageNumber: {
    position: "absolute",
    bottom: 28,
    left: 0,
    right: 0,
    textAlign: "center",
    fontSize: 8.5,
    color: C.faint,
    fontFamily: "Roboto",
  },
  pageFooterBrand: {
    position: "absolute",
    bottom: 28,
    left: 54,
    fontSize: 8.5,
    color: C.faint,
    fontFamily: "Roboto",
    letterSpacing: 0.6,
  },

  // ── cover (workbook front page) ────────────────────────────────────
  coverBanner: {
    backgroundColor: C.ink,
    paddingTop: 72,
    paddingBottom: 32,
    paddingHorizontal: 54,
  },
  coverEyebrow: {
    fontSize: 9,
    color: C.accentLight,
    letterSpacing: 2,
    marginBottom: 8,
  },
  coverTitle: {
    fontSize: 34,
    fontWeight: 700,
    color: "#ffffff",
    lineHeight: 1.15,
    marginBottom: 12,
  },
  coverSubtitle: {
    fontSize: 13,
    color: "#cbd5e1",
    lineHeight: 1.4,
  },
  coverMetaSection: {
    paddingHorizontal: 54,
    paddingTop: 32,
  },
  coverMetaRow: {
    flexDirection: "row",
    marginBottom: 14,
  },
  coverMetaCol: {
    flexGrow: 1,
    flexShrink: 1,
    flexBasis: 0,
  },
  coverMetaLabel: {
    fontSize: 8,
    color: C.faint,
    letterSpacing: 1.4,
    marginBottom: 2,
  },
  coverMetaValue: {
    fontSize: 11,
    color: C.body,
  },
  coverIntroHeading: {
    fontSize: 14,
    fontWeight: 700,
    color: C.ink,
    marginTop: 24,
    marginBottom: 8,
  },
  tocLink: {
    textDecoration: "none",
    color: C.ink,
  },
  tocRow: {
    flexDirection: "row",
    alignItems: "center",
    paddingVertical: 5,
    borderBottomWidth: 0.5,
    borderBottomColor: C.border,
  },
  tocLabBadge: {
    width: 44,
    fontSize: 9,
    fontWeight: 700,
    color: C.accent,
    letterSpacing: 0.4,
  },
  tocTitle: {
    flexGrow: 1,
    flexShrink: 1,
    fontSize: 11,
    color: C.ink,
  },
  tocExerciseNum: {
    fontSize: 8.5,
    color: C.muted,
    letterSpacing: 0.6,
  },

  // ── exercise header page ───────────────────────────────────────────
  exerciseEyebrow: {
    fontSize: 9,
    fontWeight: 700,
    color: C.accent,
    letterSpacing: 1.6,
    marginBottom: 6,
  },
  exerciseTitle: {
    fontSize: 24,
    fontWeight: 700,
    color: C.ink,
    lineHeight: 1.15,
    marginBottom: 14,
  },
  exerciseTitleRule: {
    height: 3,
    width: 48,
    backgroundColor: C.accent,
    marginTop: 0,
    marginBottom: 16,
  },
  exerciseSummary: {
    fontSize: 11,
    fontStyle: "italic",
    color: C.muted,
    marginBottom: 18,
    lineHeight: 1.55,
  },

  // ── step page ──────────────────────────────────────────────────────
  stepHeaderBand: {
    backgroundColor: C.ink,
    paddingVertical: 14,
    paddingHorizontal: 54,
  },
  stepHeaderEyebrow: {
    fontSize: 8.5,
    color: C.accentLight,
    letterSpacing: 1.6,
    marginBottom: 4,
  },
  stepHeaderTitle: {
    fontSize: 16,
    fontWeight: 700,
    color: "#ffffff",
    lineHeight: 1.2,
  },

  // ── prose ──────────────────────────────────────────────────────────
  para: {
    marginVertical: 4,
    fontSize: 10.5,
    color: C.body,
    lineHeight: 1.55,
  },
  h1: {
    fontSize: 15,
    fontWeight: 700,
    color: C.ink,
    lineHeight: 1.25,
    marginTop: 14,
    marginBottom: 6,
  },
  h2: {
    fontSize: 13,
    fontWeight: 700,
    color: C.ink,
    lineHeight: 1.25,
    marginTop: 12,
    marginBottom: 6,
  },
  h3: {
    fontSize: 10.5,
    fontWeight: 700,
    color: C.accent,
    marginTop: 10,
    marginBottom: 3,
    letterSpacing: 0.6,
  },
  bold: {
    fontWeight: 700,
  },
  italic: {
    fontStyle: "italic",
  },
  hr: {
    borderBottomWidth: 0.5,
    borderBottomColor: C.border,
    marginVertical: 10,
  },

  // ── lists ──────────────────────────────────────────────────────────
  listBlock: {
    marginVertical: 4,
    paddingLeft: 6,
  },
  listItem: {
    marginVertical: 1,
  },
  listText: {
    fontSize: 10.5,
    color: C.body,
    lineHeight: 1.55,
  },

  // ── code ───────────────────────────────────────────────────────────
  codeBlock: {
    backgroundColor: C.codeBg,
    borderRadius: 3,
    paddingVertical: 8,
    paddingHorizontal: 12,
    marginVertical: 8,
  },
  codeText: {
    fontFamily: "RobotoMono",
    fontSize: 9,
    color: C.codeFg,
    lineHeight: 1.5,
  },
  inlineCode: {
    fontFamily: "RobotoMono",
    fontSize: 9.5,
    color: C.inlineCodeFg,
    backgroundColor: C.inlineCodeBg,
  },

  // ── blockquote ─────────────────────────────────────────────────────
  blockquote: {
    borderLeftWidth: 3,
    borderLeftColor: C.quoteBorder,
    backgroundColor: C.quoteBg,
    paddingVertical: 8,
    paddingHorizontal: 12,
    marginVertical: 8,
  },
  blockquoteText: {
    fontSize: 10.5,
    color: C.quoteText,
    lineHeight: 1.55,
  },

  // ── hint admonition ────────────────────────────────────────────────
  hintBlock: {
    borderLeftWidth: 3,
    borderLeftColor: C.hintBorder,
    backgroundColor: C.hintBg,
    paddingVertical: 10,
    paddingHorizontal: 12,
    marginVertical: 8,
    borderRadius: 2,
  },
  hintLabel: {
    fontSize: 8,
    fontWeight: 700,
    color: C.hintLabel,
    letterSpacing: 1.2,
    marginBottom: 6,
  },

  // ── table ──────────────────────────────────────────────────────────
  table: {
    marginVertical: 8,
    borderWidth: 0.5,
    borderColor: C.border,
    borderRadius: 2,
  },
  tableHeaderRow: {
    flexDirection: "row",
    backgroundColor: C.surface,
    borderBottomWidth: 0.5,
    borderBottomColor: C.border,
  },
  tableRow: {
    flexDirection: "row",
    borderBottomWidth: 0.5,
    borderBottomColor: C.border,
  },
  tableRowAlt: {
    flexDirection: "row",
    borderBottomWidth: 0.5,
    borderBottomColor: C.border,
    backgroundColor: "#fafbfc",
  },
  tableHeaderCell: {
    flexGrow: 1,
    flexShrink: 1,
    flexBasis: 0,
    paddingVertical: 6,
    paddingHorizontal: 8,
    fontSize: 9.5,
    fontWeight: 700,
    color: C.ink,
  },
  tableCell: {
    flexGrow: 1,
    flexShrink: 1,
    flexBasis: 0,
    paddingVertical: 6,
    paddingHorizontal: 8,
    fontSize: 9.5,
    color: C.body,
    lineHeight: 1.45,
  },

  // ── tags ───────────────────────────────────────────────────────────
  tagRow: {
    flexDirection: "row",
    flexWrap: "wrap",
    marginTop: 4,
  },
  tag: {
    fontSize: 7.5,
    fontWeight: 700,
    backgroundColor: C.surface,
    color: C.accent,
    borderWidth: 0.5,
    borderColor: C.border,
    paddingVertical: 2,
    paddingHorizontal: 6,
    marginRight: 5,
    marginBottom: 4,
    borderRadius: 2,
    letterSpacing: 0.6,
  },
});

// ── Text sanitization ──────────────────────────────────────────────
//
// Roboto (registered above) has full Latin Extended + symbol coverage,
// so we don't need to strip Unicode anymore. We only normalize a few
// chars that look bad in print or trip layout: non-breaking space to
// regular space, smart quotes to straight (matches the rest of the
// PDF's typography). Em-dashes, arrows, bullets, multiplication signs
// etc. render fine in Roboto and are kept as-is.
function sanitizeText(s: string): string {
  return s
    .replace(/[\u2018\u2019\u201A\u2032]/g, "'")
    .replace(/[\u201C\u201D\u201E\u2033]/g, '"')
    .replace(/\u00A0/g, " ")
    // Arrows aren't in the Roboto-latin subset we ship — render them as
    // ASCII so they don't fall back to a notdef glyph that looks like
    // an apostrophe.
    .replace(/\u2192/g, "->")
    .replace(/\u2190/g, "<-")
    .replace(/\u2194/g, "<->")
    .replace(/\u21D2/g, "=>")
    .replace(/\u21D0/g, "<=")
    .replace(/\u2191/g, "^")
    .replace(/\u2193/g, "v");
}

// Long unbreakable runs in command/code blocks (URLs, JSON payloads,
// quote-bound filter strings) can't be word-wrapped by react-pdf because
// they have no whitespace breakpoints. Hard-wrap long lines at natural
// punctuation by inserting a real newline + continuation indent. We
// can't use \u200B (zero-width space) here because that codepoint is
// outside the Roboto-latin font subset and would render as notdef.
function softWrapCode(s: string, maxLen = 78): string {
  const wrap = (line: string): string => {
    if (line.length <= maxLen) return line;
    // Find a breakpoint near maxLen at a sensible character.
    const breakChars = /[\s,=]/;
    let cut = -1;
    for (let i = Math.min(maxLen, line.length - 1); i > maxLen / 2; i--) {
      if (breakChars.test(line[i])) {
        cut = i + 1;
        break;
      }
    }
    if (cut < 0) cut = maxLen;
    const head = line.slice(0, cut).replace(/\s+$/, "");
    const tail = line.slice(cut).replace(/^\s+/, "");
    return head + "\n  " + wrap(tail);
  };
  return s.split("\n").map(wrap).join("\n");
}

// ── Segment splitter (same logic as scenario-runner) ────────────────

const CMD_TOOL_RE =
  /^(nmap|mbpoll|dnp3poll|dnp3cmd|curl|tshark|tcpdump|nc|telnet|ssh|wget|ls|grep|cat|docker)\s/;
const HINT_OPEN_RE = /^:::hint(?:\s+(.+))?$/;
const HINT_CLOSE_RE = /^:::$/;

type Segment =
  | { type: "prose"; value: string }
  | { type: "cmd"; value: string }
  | { type: "hint"; title: string; value: string };

function splitDescription(text: string): Segment[] {
  const result: Segment[] = [];
  const lines = text.split("\n");
  let prose: string[] = [];
  let i = 0;

  const flushProse = () => {
    if (prose.length > 0) {
      result.push({ type: "prose", value: prose.join("\n") });
      prose = [];
    }
  };

  while (i < lines.length) {
    const trimmed = lines[i].trim();
    const hintOpen = HINT_OPEN_RE.exec(trimmed);
    if (hintOpen) {
      flushProse();
      const title = hintOpen[1]?.trim() || "Reveal answer";
      const body: string[] = [];
      i++;
      while (i < lines.length && !HINT_CLOSE_RE.test(lines[i].trim())) {
        body.push(lines[i]);
        i++;
      }
      if (i < lines.length) i++;
      result.push({ type: "hint", title, value: body.join("\n") });
      continue;
    }
    const isIndented = /^\s+\S/.test(lines[i]);
    if (isIndented && CMD_TOOL_RE.test(trimmed)) {
      flushProse();
      let cmd = trimmed;
      while (cmd.endsWith("\\") && i + 1 < lines.length) {
        i++;
        cmd += "\n  " + lines[i].trim();
      }
      result.push({ type: "cmd", value: cmd });
    } else {
      prose.push(lines[i]);
    }
    i++;
  }
  flushProse();
  return result;
}

// ── Markdown token walker ──────────────────────────────────────────

// marked's type exports moved between versions, so we use a loose token
// shape and discriminate on the `type` field at runtime. The only
// properties we rely on are widely supported: text, tokens, depth,
// items, ordered, start, header, rows, raw.
type MdToken = {
  type: string;
  text?: string;
  tokens?: MdToken[];
  depth?: number;
  items?: MdToken[];
  ordered?: boolean;
  start?: number;
  header?: { text: string }[];
  rows?: { text: string }[][];
  raw?: string;
};

// react-pdf Text nodes can contain mixed children. For inline formatting
// (bold, italic, inline code, links) we walk the marked inline tokens
// and emit an array of Text children with appropriate styles.
function renderInline(tokens: MdToken[], keyPrefix = ""): React.ReactNode[] {
  return tokens.map((token, i) => {
    const key = `${keyPrefix}-${i}`;
    switch (token.type) {
      case "text":
        if (token.tokens && token.tokens.length > 0) {
          return <Text key={key}>{renderInline(token.tokens, key)}</Text>;
        }
        return <Text key={key}>{sanitizeText(token.text || "")}</Text>;
      case "strong":
        return (
          <Text key={key} style={styles.bold}>
            {renderInline(token.tokens || [], key)}
          </Text>
        );
      case "em":
        return (
          <Text key={key} style={styles.italic}>
            {renderInline(token.tokens || [], key)}
          </Text>
        );
      case "codespan":
        return (
          <Text key={key} style={styles.inlineCode}>
            {sanitizeText(token.text || "")}
          </Text>
        );
      case "link":
        // Render as plain text (no live links in PDF)
        return (
          <Text key={key}>
            {renderInline(token.tokens || [], key)}
          </Text>
        );
      case "del":
        return (
          <Text key={key} style={{ textDecoration: "line-through" }}>
            {renderInline(token.tokens || [], key)}
          </Text>
        );
      case "br":
        return <Text key={key}>{"\n"}</Text>;
      case "escape":
        return <Text key={key}>{sanitizeText(token.text || "")}</Text>;
      default:
        if (token.text) return <Text key={key}>{sanitizeText(token.text)}</Text>;
        return null;
    }
  });
}

// Walk an already-parsed array of marked block tokens. Used both as the
// entry point (after lexing a markdown string) and recursively for nested
// content like list items and blockquotes — feeding raw text back through
// `marked.lexer()` would cause infinite recursion on nested lists because
// `item.raw` for a list token re-lexes to the same list.
function renderBlockTokens(tokens: MdToken[], keyPrefix = ""): React.ReactNode[] {
  return tokens
    .map((token, i): React.ReactElement | null => {
      const key = `${keyPrefix}-${i}`;
      switch (token.type) {
        case "heading": {
          const depth = token.depth || 3;
          const style = depth === 1 ? styles.h1 : depth === 2 ? styles.h2 : styles.h3;
          return (
            <Text key={key} style={style}>
              {renderInline(token.tokens || [], key)}
            </Text>
          );
        }
        case "paragraph": {
          return (
            <Text key={key} style={styles.para}>
              {renderInline(token.tokens || [], key)}
            </Text>
          );
        }
        case "blockquote": {
          // wrap=false prevents react-pdf from splitting the View
          // across pages, which produced empty leading boxes when the
          // content overflowed. The visual difference is acceptable
          // because individual blockquotes are small.
          return (
            <View key={key} style={styles.blockquote} wrap={false}>
              {renderBlockTokens(token.tokens || [], key)}
            </View>
          );
        }
        case "list": {
          const items = token.items || [];
          const start = token.start || 1;
          return (
            <View key={key} style={styles.listBlock}>
              {items.map((item, j) => {
                const marker = token.ordered ? `${start + j}.  ` : "•  ";
                const itemKey = `${key}-${j}`;
                const itemTokens = item.tokens || [];
                const firstBlock = itemTokens[0];
                const inline =
                  firstBlock && (firstBlock.type === "paragraph" || firstBlock.type === "text")
                    ? renderInline(firstBlock.tokens || [], itemKey)
                    : item.text
                    ? [<Text key={`${itemKey}-t`}>{sanitizeText(item.text)}</Text>]
                    : [];
                const rest = firstBlock ? itemTokens.slice(1) : itemTokens;
                return (
                  <View key={itemKey} style={styles.listItem}>
                    <Text style={styles.listText}>
                      <Text>{marker}</Text>
                      {inline}
                    </Text>
                    {rest.length > 0 ? renderBlockTokens(rest, itemKey) : null}
                  </View>
                );
              })}
            </View>
          );
        }
        case "code": {
          return (
            <View key={key} style={styles.codeBlock} wrap={false}>
              <Text style={styles.codeText}>
                {softWrapCode(sanitizeText(token.text || ""))}
              </Text>
            </View>
          );
        }
        case "hr":
          return <View key={key} style={styles.hr} />;
        case "table": {
          // Each cell carries both `text` (raw markdown) and `tokens`
          // (parsed inline tokens). We render the tokens so **bold**,
          // `code`, and links display properly instead of literal
          // markdown markers. Now that each step gets its own Page,
          // flexbox table rows lay out reliably.
          type Cell = { text?: string; tokens?: MdToken[] };
          const header = (token.header || []) as Cell[];
          const rows = (token.rows || []) as Cell[][];
          return (
            <View key={key} style={styles.table}>
              {header.length > 0 ? (
                <View style={styles.tableHeaderRow}>
                  {header.map((cell, h) => (
                    <Text key={`${key}-h-${h}`} style={styles.tableHeaderCell}>
                      {cell.tokens && cell.tokens.length > 0
                        ? renderInline(cell.tokens, `${key}-h-${h}`)
                        : sanitizeText(cell.text || "")}
                    </Text>
                  ))}
                </View>
              ) : null}
              {rows.map((row, r) => (
                <View
                  key={`${key}-r-${r}`}
                  style={r % 2 === 1 ? styles.tableRowAlt : styles.tableRow}
                >
                  {row.map((cell, c) => (
                    <Text key={`${key}-r-${r}-c-${c}`} style={styles.tableCell}>
                      {cell.tokens && cell.tokens.length > 0
                        ? renderInline(cell.tokens, `${key}-r-${r}-c-${c}`)
                        : sanitizeText(cell.text || "")}
                    </Text>
                  ))}
                </View>
              ))}
            </View>
          );
        }
        case "space":
          return null;
        default:
          if ("text" in token) {
            return (
              <Text key={key} style={styles.para}>
                {sanitizeText((token as { text: string }).text)}
              </Text>
            );
          }
          return null;
      }
    })
    .filter((x): x is React.ReactElement => x !== null);
}

// Lex a markdown string once and walk the resulting block tokens.
function renderMarkdown(markdown: string, keyPrefix = ""): React.ReactNode[] {
  if (!markdown || !markdown.trim()) return [];
  const tokens = marked.lexer(markdown, { gfm: true }) as unknown as MdToken[];
  return renderBlockTokens(tokens, keyPrefix);
}

// Render a single step (title + description segments).
function StepBlock({ step, index }: { step: ScenarioStep; index: number }) {
  const segments = step.description ? splitDescription(step.description) : [];
  return (
    <>
      <View style={styles.stepHeaderBand}>
        <Text style={styles.stepHeaderEyebrow}>STEP {index + 1}</Text>
        <Text style={styles.stepHeaderTitle}>{sanitizeText(step.title)}</Text>
      </View>
      <View style={styles.pageBody} wrap>
        {segments.map((seg, si) => {
          if (seg.type === "prose") {
            const trimmed = seg.value.replace(/^\n+|\n+$/g, "");
            return <View key={si}>{renderMarkdown(trimmed, `s${index}-${si}`)}</View>;
          }
          if (seg.type === "cmd") {
            return (
              <View key={si} style={styles.codeBlock} wrap={false}>
                <Text style={styles.codeText}>
                  {softWrapCode(sanitizeText(seg.value))}
                </Text>
              </View>
            );
          }
          // hint — these can be long enough to span multiple pages
          // (the Critical Conduits hint has three tables and would
          // exceed a page), so we have to allow wrap. The empty
          // leading box artifact is the cost of admitting the hint
          // can be a multi-page admonition.
          return (
            <View key={si} style={styles.hintBlock}>
              <Text style={styles.hintLabel}>
                HINT — {sanitizeText(seg.title.toUpperCase())}
              </Text>
              {renderMarkdown(
                seg.value.replace(/^\n+|\n+$/g, ""),
                `s${index}-${si}-hint`,
              )}
            </View>
          );
        })}
      </View>
    </>
  );
}

// Header content for an exercise (everything except the per-step body).
// `anchorId` becomes a #-target so TOC entries can link to this header.
function ExerciseHeader({
  scenario,
  exerciseNumber,
  anchorId,
}: {
  scenario: Scenario;
  exerciseNumber: number;
  anchorId?: string;
}) {
  const lab = workbookSection(scenario.id);
  const eyebrow = lab
    ? `LAB ${lab}  ·  EXERCISE ${exerciseNumber}`
    : `EXERCISE ${exerciseNumber}`;
  return (
    <View style={styles.pageBody} id={anchorId} wrap>
      <Text style={styles.exerciseEyebrow}>{eyebrow}</Text>
      <Text style={styles.exerciseTitle}>{sanitizeText(scenario.name)}</Text>
      <View style={styles.exerciseTitleRule} />
      {scenario.summary ? (
        <Text style={styles.exerciseSummary}>{sanitizeText(scenario.summary)}</Text>
      ) : null}
      {scenario.description ? (
        <View>
          {renderMarkdown(scenario.description, `exdesc-${scenario.id}`)}
        </View>
      ) : null}
      {scenario.tags && scenario.tags.length > 0 ? (
        <View style={styles.tagRow}>
          {scenario.tags.map((tag) => (
            <Text key={tag} style={styles.tag}>
              {sanitizeText(tag)}
            </Text>
          ))}
        </View>
      ) : null}
    </View>
  );
}

// ── Exported Document components ──────────────────────────────────

export function ExercisePDF({ scenario }: { scenario: Scenario }) {
  const lab = workbookSection(scenario.id);
  const labPrefix = lab ? `LAB ${lab}  ·  ` : "";
  const footer = `${labPrefix}${sanitizeText(scenario.name).toUpperCase()}`;
  const exerciseBookmarkTitle = lab
    ? `Lab ${lab} — ${sanitizeText(scenario.name)}`
    : sanitizeText(scenario.name);
  return (
    <Document title={scenario.name} author="RangerDanger">
      <Page
        size="LETTER"
        style={styles.page}
        bookmark={{ title: exerciseBookmarkTitle, fit: true }}
        wrap
      >
        <ExerciseHeader
          scenario={scenario}
          exerciseNumber={scenario.order ?? 0}
          anchorId={`ex-${scenario.id}`}
        />
        <Text style={styles.pageFooterBrand} fixed>{footer}</Text>
        <Text
          style={styles.pageNumber}
          render={({ pageNumber, totalPages }) => `${pageNumber} / ${totalPages}`}
          fixed
        />
      </Page>
      {scenario.steps.map((step, i) => (
        <Page
          key={i}
          size="LETTER"
          style={styles.page}
          bookmark={{
            title: `   • Step ${i + 1}: ${sanitizeText(step.title)}`,
            fit: true,
          }}
          wrap
        >
          <StepBlock step={step} index={i} />
          <Text style={styles.pageFooterBrand} fixed>{footer}</Text>
          <Text
            style={styles.pageNumber}
            render={({ pageNumber, totalPages }) => `${pageNumber} / ${totalPages}`}
            fixed
          />
        </Page>
      ))}
    </Document>
  );
}

export function WorkbookPDF({
  scenarios,
  generatedAt,
}: {
  scenarios: Scenario[];
  generatedAt: string;
}) {
  const sorted = [...scenarios].sort((a, b) => (a.order ?? 99) - (b.order ?? 99));
  return (
    <Document title="RangerDanger Substation Segmentation Workbook" author="RangerDanger">
      {/* Cover page */}
      <Page
        size="LETTER"
        style={styles.page}
        bookmark={{ title: "Cover & Contents", fit: true }}
      >
        <View style={styles.coverBanner}>
          <Text style={styles.coverEyebrow}>RANGERDANGER  /  OT CYBER RANGE</Text>
          <Text style={styles.coverTitle}>Substation Segmentation</Text>
          <Text style={styles.coverSubtitle}>
            Hands-on workbook — capture, analyze, harden, validate
          </Text>
        </View>
        <View style={styles.coverMetaSection}>
          <View style={styles.coverMetaRow}>
            <View style={styles.coverMetaCol}>
              <Text style={styles.coverMetaLabel}>SCENARIO</Text>
              <Text style={styles.coverMetaValue}>
                Electric distribution substation
              </Text>
            </View>
            <View style={styles.coverMetaCol}>
              <Text style={styles.coverMetaLabel}>EXERCISES</Text>
              <Text style={styles.coverMetaValue}>{sorted.length} in this workbook</Text>
            </View>
            <View style={styles.coverMetaCol}>
              <Text style={styles.coverMetaLabel}>GENERATED</Text>
              <Text style={styles.coverMetaValue}>{sanitizeText(generatedAt)}</Text>
            </View>
          </View>

          <Text style={styles.coverIntroHeading}>Contents</Text>
          {sorted.map((s, ei) => {
            const lab = workbookSection(s.id);
            return (
              <Link
                key={s.id}
                src={`#ex-${s.id}`}
                style={styles.tocLink}
              >
                <View style={styles.tocRow}>
                  <Text style={styles.tocLabBadge}>{lab ? lab : "—"}</Text>
                  <Text style={styles.tocTitle}>{sanitizeText(s.name)}</Text>
                  <Text style={styles.tocExerciseNum}>EX {ei}</Text>
                </View>
              </Link>
            );
          })}

          <Text style={styles.coverIntroHeading}>Introduction</Text>
          <Text style={styles.para}>
            This workbook contains every exercise for the RangerDanger OT
            segmentation lab. Lab numbers (1.2, 1.3, 2.3, ...) align with
            the workshop courseware modules so each printed page can be
            traced back to the slide deck used in instructor-led delivery.
          </Text>
          <Text style={styles.para}>
            The lab is sequential: capture and analyze baseline traffic,
            identify required communication flows and exposures, plan
            remediation under real-world constraints, design segmentation
            requirements, execute attacks against the weak baseline, then
            validate that the hardened policy closes the attack paths
            without breaking operations.
          </Text>
          <Text style={styles.para}>
            The web UI runner includes interactive terminals, one-click
            command execution, and collapsible hints. This PDF mirrors the
            full exercise content for offline reading and printing.
          </Text>
        </View>
        <Text style={styles.pageFooterBrand} fixed>
          RANGERDANGER WORKBOOK
        </Text>
        <Text
          style={styles.pageNumber}
          render={({ pageNumber, totalPages }) => `${pageNumber} / ${totalPages}`}
          fixed
        />
      </Page>

      {/* One page per exercise header, then one page per step. The
          @react-pdf bookmark type is flat (no `parent` field), so we
          fake a two-level outline by indenting step titles with a
          leading bullet. PDF readers display the bookmarks in document
          order, which produces the right visual grouping. */}
      {sorted.flatMap((s, ei) => {
        const lab = workbookSection(s.id);
        const labPrefix = lab ? `LAB ${lab}  ·  ` : "";
        const footer = `${labPrefix}${sanitizeText(s.name).toUpperCase()}`;
        const exerciseBookmarkTitle = lab
          ? `Lab ${lab} — ${sanitizeText(s.name)}`
          : sanitizeText(s.name);
        return [
          <Page
            key={`${s.id}-h`}
            size="LETTER"
            style={styles.page}
            bookmark={{ title: exerciseBookmarkTitle, fit: true }}
            wrap
          >
            <ExerciseHeader
              scenario={s}
              exerciseNumber={ei}
              anchorId={`ex-${s.id}`}
            />
            <Text style={styles.pageFooterBrand} fixed>{footer}</Text>
            <Text
              style={styles.pageNumber}
              render={({ pageNumber, totalPages }) =>
                `${pageNumber} / ${totalPages}`
              }
              fixed
            />
          </Page>,
          ...s.steps.map((step, i) => (
            <Page
              key={`${s.id}-s${i}`}
              size="LETTER"
              style={styles.page}
              bookmark={{
                title: `   • Step ${i + 1}: ${sanitizeText(step.title)}`,
                fit: true,
              }}
              wrap
            >
              <StepBlock step={step} index={i} />
              <Text style={styles.pageFooterBrand} fixed>{footer}</Text>
              <Text
                style={styles.pageNumber}
                render={({ pageNumber, totalPages }) =>
                  `${pageNumber} / ${totalPages}`
                }
                fixed
              />
            </Page>
          )),
        ];
      })}
    </Document>
  );
}
