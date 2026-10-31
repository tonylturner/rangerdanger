import { describe, expect, it } from "vitest";
import type { Section } from "./knowledge-types";
import {
  buildSnippet,
  readingMinutes,
  searchKnowledge,
  stripMarkdownForExcerpt,
} from "./knowledge-search";

describe("knowledge search helpers", () => {
  it("drops fenced code and strips Markdown from the first paragraph", () => {
    const markdown = [
      "```ts",
      "ignoredCode()",
      "```",
      "",
      "## **First** paragraph with [a link](https://example.test).",
      "",
      "A later paragraph.",
    ].join("\n");

    expect(stripMarkdownForExcerpt(markdown)).toBe("First paragraph with a link.");
  });

  it("keeps reading estimates to at least one minute and scales with word count", () => {
    expect(readingMinutes("")).toBe(1);
    expect(readingMinutes("A short article.")).toBe(1);
    expect(readingMinutes("word ".repeat(440))).toBe(2);
  });

  it("returns matching query context and falls back to the Markdown excerpt", () => {
    const matchingBody = `Opening ${"quiet ".repeat(15)}needle phrase.`;
    const snippet = buildSnippet(matchingBody, "needle");
    expect(snippet).toContain("needle phrase");
    expect(snippet.startsWith("… ")).toBe(true);

    const fallbackBody =
      "# **Fallback** with [a link](https://example.test).\n\nSecond paragraph.";
    expect(buildSnippet(fallbackBody, "missing")).toBe("Fallback with a link.");
  });
});

describe("package-scoped knowledge search", () => {
  const us: Section[] = [{ id: "equipment", heading: "Equipment", articles: [
    { id: "rtac", title: "RTAC", body: "US controller" },
  ] }];
  const eu: Section[] = [{ id: "telecontrol", heading: "Telecontrol", articles: [
    { id: "iec104-wire-model", title: "IEC104 frames", body: "I frames carry ASDUs." },
    { id: "iec104-sbo-commands", title: "SBO", body: "IEC104 select-before-operate." },
  ] }];

  it("searches only the supplied package and preserves authored order", () => {
    expect(searchKnowledge(us, "RTAC").map((hit) => hit.article.id)).toEqual(["rtac"]);
    expect(searchKnowledge(eu, "RTAC")).toEqual([]);
    expect(searchKnowledge(eu, " IEC104 ").map((hit) => hit.article.id)).toEqual(["iec104-wire-model", "iec104-sbo-commands"]);
    expect(searchKnowledge(eu, "")).toEqual([]);
    expect(searchKnowledge([], "IEC104")).toEqual([]);
  });

  it("matches titles and bodies with snippets from the loaded snapshot", () => {
    expect(searchKnowledge(eu, "ASDU")[0]).toMatchObject({
      section: eu[0], article: eu[0].articles[0], snippet: "I frames carry ASDUs.",
    });
    expect(searchKnowledge(eu, "SBO")[0]).toMatchObject({
      article: eu[0].articles[1], snippet: "IEC104 select-before-operate.",
    });
  });
});
