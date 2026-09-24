import { describe, expect, it } from "vitest";
import { sections } from "./knowledge-content";
import {
  buildSnippet,
  readingMinutes,
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

describe("knowledge content", () => {
  it("retains the six unique section headings and original article order", () => {
    const headings = sections.map((section) => section.heading);
    expect(headings).toEqual([
      "Substation Equipment & Operations",
      "Network Segmentation Concepts",
      "Protocols & Communication",
      "Command References & Lab Tools",
      "Lab Internals",
      "ICS Threats & Operational Practice",
    ]);
    expect(new Set(headings).size).toBe(6);

    const articleIds = sections.flatMap((section) =>
      section.articles.map((article) => article.id),
    );
    expect(new Set(articleIds).size).toBe(articleIds.length);
    expect(articleIds).toEqual([
      "distribution-substation",
      "rtac",
      "protective-relays",
      "reclosers",
      "voltage-regulators",
      "capacitor-banks",
      "substation-physics",
      "hmi-scada-fuxa",
      "plc-ladder-openplc",
      "power-factor-reactive-power",
      "ot-segmentation-overview",
      "segmentation-approaches",
      "purdue-model",
      "iec-62443-zones-conduits",
      "iec-62443-security-levels",
      "nerc-cip-segmentation",
      "esp-eap-erc",
      "ics-dpi",
      "default-deny",
      "vendor-remote-access",
      "modbus-tcp",
      "dnp3",
      "ntp-ot",
      "iec-61850-goose",
      "opc-ua",
      "tool-mbpoll",
      "tool-dnp3",
      "tool-tshark",
      "tool-tcpdump",
      "tool-curl",
      "tool-nmap",
      "lab-architecture",
      "weak-vs-hardened",
      "traffic-matrix",
      "plan-coverage-pipeline",
      "live-dpi-events-strip",
      "how-containd-enforces-policy",
      "ot-kill-chain",
      "living-off-the-land-ot",
      "change-management-firewall-rules",
      "outage-costs-saidi-saifi",
    ]);
  });
});
