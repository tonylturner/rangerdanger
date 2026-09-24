import { describe, expect, it } from "vitest";
import {
  sanitizeText,
  softWrapCode,
  splitDescription,
} from "./exercise-pdf-text";

describe("splitDescription", () => {
  it("keeps prose around a titled hint fence in order", () => {
    const segments = splitDescription(
      "Intro prose.\n:::hint Check the result\nUse this output.\n:::\nClosing prose.",
    );

    expect(segments.map((segment) => segment.type)).toEqual([
      "prose",
      "hint",
      "prose",
    ]);
    expect(segments[0]).toEqual({ type: "prose", value: "Intro prose." });
    expect(segments[1]).toEqual({
      type: "hint",
      title: "Check the result",
      value: "Use this output.",
    });
    expect(segments[2]).toEqual({ type: "prose", value: "Closing prose." });
  });

  it("joins indented continued nmap commands but leaves unindented text as prose", () => {
    const segments = splitDescription(
      "    nmap -sn \\\n      10.40.40.20\nnmap -p 502 10.40.40.20",
    );

    expect(segments).toEqual([
      { type: "cmd", value: "nmap -sn \\\n  10.40.40.20" },
      { type: "prose", value: "nmap -p 502 10.40.40.20" },
    ]);
  });
});

describe("sanitizeText", () => {
  it("normalizes smart quotes, non-breaking spaces, and arrows but preserves em dash", () => {
    expect(sanitizeText("‘smart’ “quotes”\u00a0→ ← ↔ ⇒ ⇐ ↑ ↓ —")).toBe(
      `'smart' "quotes" -> <- <-> => <= ^ v —`,
    );
  });
});

describe("softWrapCode", () => {
  it("preserves newlines and wraps long lines at punctuation", () => {
    expect(softWrapCode("short\nalpha beta,gamma delta", 15)).toBe(
      "short\nalpha beta,\n  gamma delta",
    );
  });
});
